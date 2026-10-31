package ktf

import (
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

type arenaState struct {
	Base                            uint32
	Limit, Cursor, HighWater, Freed uint64
	Free                            []arenaBlockState
}
type arenaBlockState struct{ Start, End uint64 }

func captureArenaState(arena *wipicrt.GuestArena) (arenaState, error) {
	if arena == nil || len(arena.Free) > 1<<20 {
		return arenaState{}, fmt.Errorf("KTF arena is missing or exceeds the free-block limit")
	}
	saved := arenaState{Base: arena.Base, Limit: arena.Limit, Cursor: arena.Cursor, HighWater: arena.HighWater, Freed: arena.Freed}
	for _, block := range arena.Free {
		saved.Free = append(saved.Free, arenaBlockState{Start: block.Start, End: block.End})
	}
	if _, err := restoreArenaState(saved, arena.Base, arena.Limit-uint64(arena.Base)); err != nil {
		return arenaState{}, err
	}
	return saved, nil
}

func restoreArenaState(saved arenaState, base uint32, size uint64) (*wipicrt.GuestArena, error) {
	end := uint64(base) + size
	if size == 0 || end > 1<<32 || saved.Base != base || saved.Limit != end || saved.Cursor < uint64(base) || saved.Cursor > saved.HighWater || saved.HighWater > end || saved.Cursor%wipicrt.ArenaAlignment != 0 || saved.HighWater%wipicrt.ArenaAlignment != 0 || len(saved.Free) > 1<<20 {
		return nil, fmt.Errorf("KTF arena state has invalid bounds or policy")
	}
	var freed uint64
	for i, block := range saved.Free {
		if block.Start < uint64(base) || block.Start >= block.End || block.End >= saved.Cursor || block.Start%wipicrt.ArenaAlignment != 0 || block.End%wipicrt.ArenaAlignment != 0 || i > 0 && saved.Free[i-1].End >= block.Start {
			return nil, fmt.Errorf("KTF arena free list is not bounded and coalesced")
		}
		freed += block.End - block.Start
	}
	if freed != saved.Freed {
		return nil, fmt.Errorf("KTF arena free-byte count differs from its blocks")
	}
	arena := &wipicrt.GuestArena{Base: base, Limit: end, Cursor: saved.Cursor, HighWater: saved.HighWater, Freed: freed}
	for _, block := range saved.Free {
		arena.Free = append(arena.Free, wipicrt.ArenaBlock{Start: block.Start, End: block.End})
	}
	return arena, nil
}

func (runtime *initializationRuntime) heapCodec() jvm.HeapCodec {
	return jvm.HeapCodec{
		CaptureArray: func(storage jvm.ArrayStorage) (jvm.HeapExternalPayload, error) {
			array, ok := storage.(*guestArrayStorage)
			if !ok || array.runtime != runtime {
				return jvm.HeapExternalPayload{}, fmt.Errorf("KTF heap contains an unknown array storage")
			}
			data := make([]byte, 4)
			binary.LittleEndian.PutUint32(data, array.elements)
			return jvm.HeapExternalPayload{Kind: "ktf-array-v1", Data: data}, nil
		},
		RestoreArray: func(component jvm.Type, length int, external jvm.HeapExternalPayload) (jvm.ArrayStorage, error) {
			if external.Kind != "ktf-array-v1" || len(external.Data) != 4 || len(external.References) != 0 || length < 0 || uint64(length) > uint64(maxJavaArrayElements) {
				return nil, fmt.Errorf("KTF heap array record is invalid")
			}
			elements := binary.LittleEndian.Uint32(external.Data)
			const headerSize = javaInstanceSize + javaInstanceHeader + javaArrayLengthSize
			if elements < headerSize || elements&3 != 0 {
				return nil, fmt.Errorf("KTF heap array address is invalid")
			}
			address := elements - headerSize
			size, err := aotArrayElementBytes(component)
			if err != nil {
				return nil, err
			}
			total := uint64(headerSize) + uint64(length)*uint64(size)
			if total > wipicrt.MaxPlatformAllocation {
				return nil, fmt.Errorf("KTF heap array exceeds allocation limit")
			}
			if err := runtime.client.Core().Memory().ValidateRange(address, total, armcore.PermissionReadWrite); err != nil {
				return nil, err
			}
			header, err := runtime.ReadAOTWords(address, headerSize/4, "restored array header")
			if err != nil {
				return nil, err
			}
			if header[0] != address+javaInstanceSize || header[3] != uint32(length) {
				return nil, fmt.Errorf("KTF heap array shape differs from its guest header")
			}
			class, err := runtime.readAOTClass(header[1])
			if err != nil {
				return nil, err
			}
			if class.Name != "["+component.Descriptor() {
				return nil, fmt.Errorf("KTF heap array component differs from its guest class")
			}
			return runtime.newGuestArrayStorage(address, component, length)
		},
	}
}

package ktf

import (
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type arenaState struct {
	Base                            uint32
	Limit, Cursor, HighWater, Freed uint64
	Free                            []arenaBlockState
}
type arenaBlockState struct{ Start, End uint64 }

func (arena *guestArena) captureState() (arenaState, error) {
	if arena == nil || len(arena.free) > 1<<20 {
		return arenaState{}, fmt.Errorf("KTF arena is missing or exceeds the free-block limit")
	}
	saved := arenaState{Base: arena.base, Limit: arena.limit, Cursor: arena.cursor, HighWater: arena.highWater, Freed: arena.freed}
	for _, block := range arena.free {
		saved.Free = append(saved.Free, arenaBlockState{Start: block.start, End: block.end})
	}
	if _, err := restoreArenaState(saved, arena.base, arena.limit-uint64(arena.base)); err != nil {
		return arenaState{}, err
	}
	return saved, nil
}

func restoreArenaState(saved arenaState, base uint32, size uint64) (*guestArena, error) {
	end := uint64(base) + size
	if size == 0 || end > 1<<32 || saved.Base != base || saved.Limit != end || saved.Cursor < uint64(base) || saved.Cursor > saved.HighWater || saved.HighWater > end || saved.Cursor%arenaAlignment != 0 || saved.HighWater%arenaAlignment != 0 || len(saved.Free) > 1<<20 {
		return nil, fmt.Errorf("KTF arena state has invalid bounds or policy")
	}
	var freed uint64
	for i, block := range saved.Free {
		if block.Start < uint64(base) || block.Start >= block.End || block.End >= saved.Cursor || block.Start%arenaAlignment != 0 || block.End%arenaAlignment != 0 || i > 0 && saved.Free[i-1].End >= block.Start {
			return nil, fmt.Errorf("KTF arena free list is not bounded and coalesced")
		}
		freed += block.End - block.Start
	}
	if freed != saved.Freed {
		return nil, fmt.Errorf("KTF arena free-byte count differs from its blocks")
	}
	arena := &guestArena{base: base, limit: end, cursor: saved.Cursor, highWater: saved.HighWater, freed: freed}
	for _, block := range saved.Free {
		arena.free = append(arena.free, arenaBlock{start: block.Start, end: block.End})
	}
	return arena, nil
}

// rebaseArenaShadow starts debug diagnostics at the restored instant. Historical
// free-block evidence belongs to the old runtime and does not change guest state.
func (runtime *initializationRuntime) rebaseArenaShadow() {
	runtime.installArenaShadow()
	if runtime.arena.recordReleased == nil {
		return
	}
	for _, block := range runtime.arena.free {
		runtime.arena.recordReleased(uint32(block.start), block.end-block.start)
	}
	if runtime.arena.cursor < runtime.arena.highWater {
		runtime.arena.recordReleased(uint32(runtime.arena.cursor), runtime.arena.highWater-runtime.arena.cursor)
	}
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
			if total > maxPlatformAllocation {
				return nil, fmt.Errorf("KTF heap array exceeds allocation limit")
			}
			if err := runtime.client.core.Memory().ValidateRange(address, total, armcore.PermissionReadWrite); err != nil {
				return nil, err
			}
			header, err := runtime.readAOTWords(address, headerSize/4, "restored array header")
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

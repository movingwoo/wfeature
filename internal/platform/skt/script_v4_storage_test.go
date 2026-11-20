package skt

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func runWideStorageTest(t *testing.T, resources []sgsvm.Resource, call func(*sgsvm.WideVM)) {
	t.Helper()
	vm := sgsvm.NewWide(&sgsvm.Program{
		Wide:      &sgsvm.WideProgram{Variables: [][]int32{{1, 2, 3, 4}}},
		Resources: resources,
	}, nil)
	call(vm)
}

func testWideStorageCall(t *testing.T, s *ScriptSession, vm *sgsvm.WideVM, id uint16, args ...int32) {
	t.Helper()
	for _, a := range args {
		if err := vm.Push(a); err != nil {
			t.Fatal(err)
		}
	}
	handled, err := s.wideStorage(id, vm)
	if !handled || err != nil {
		t.Fatalf("service %#x: handled %t, error %v", id, handled, err)
	}
}

func testWideStorageResult(t *testing.T, s *ScriptSession, vm *sgsvm.WideVM, want int32, id uint16, args ...int32) {
	t.Helper()
	testWideStorageCall(t, s, vm, id, args...)
	out, err := vm.Args(1)
	if err != nil || out[0] != want {
		t.Fatalf("service %#x result %v, want %d: %v", id, out, want, err)
	}
}

func TestWideFilePersistenceAndSeed(t *testing.T) {
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &ScriptSession{options: ScriptOptions{SaveStore: store}, archive: &Archive{Entries: map[string][]byte{"slot": []byte("abc")}}}
	res := []sgsvm.Resource{{Data: []byte("slot\x00")}, {Data: []byte("zzzz"), Mutable: true}, {Data: []byte("../slot\x00")}}
	runWideStorageTest(t, res, func(vm *sgsvm.WideVM) {
		testWideStorageResult(t, s, vm, -1, 0x63, 2, 3)
		testWideStorageResult(t, s, vm, 0, 0x63, 0, 1)
		testWideStorageResult(t, s, vm, 3, 0x6a, 0, 1, 0, 4)
		if !bytes.Equal(vm.Resources[1].Data, []byte("abcz")) {
			t.Fatal(vm.Resources[1].Data)
		}
		testWideStorageResult(t, s, vm, 1, 0x66, 0, -2, 2)
		testWideStorageResult(t, s, vm, 2, 0x68, 0, 1, 2, 2)
		testWideStorageResult(t, s, vm, 1, 0x64, 0)
	})
	fresh := &ScriptSession{options: s.options, archive: s.archive}
	runWideStorageTest(t, res, func(vm *sgsvm.WideVM) {
		testWideStorageResult(t, fresh, vm, 0, 0x63, 0, 3)
		testWideStorageResult(t, fresh, vm, 3, 0x6a, 0, 1, 0, 4)
		if !bytes.Equal(vm.Resources[1].Data[:3], []byte("acz")) {
			t.Fatal(vm.Resources[1].Data)
		}
		testWideStorageResult(t, fresh, vm, 1, 0x64, 0)
		testWideStorageResult(t, fresh, vm, 1, 0x65, 0)
		testWideStorageResult(t, fresh, vm, -1, 0x6b, 0)
	})
}

func TestWideMediaAllocationAndSound(t *testing.T) {
	sink := &scriptNoteSink{AudioSink: backend.NewRecordingSink(nil)}
	s := &ScriptSession{audio: backend.NewAudio(sink)}
	file := scriptOneNoteSMAF()
	sound := append([]byte{2, 0}, file...)
	res := []sgsvm.Resource{{Data: []byte{2, 4}, Mutable: true}, {Data: sound, Kind: 2}}
	runWideStorageTest(t, res, func(vm *sgsvm.WideVM) {
		testWideStorageResult(t, s, vm, 1, 0x9e, 0, 5)
		testWideStorageResult(t, s, vm, 6, 0x9d, 0)
		testWideStorageResult(t, s, vm, 0, 0x9e, 1, 20)
		testWideStorageCall(t, s, vm, 0xc6, 0, 2, 0x5a, 3)
		if !bytes.Equal(vm.Resources[0].Data, []byte{2, 4, 0x5a, 0x5a, 0x5a, 0}) {
			t.Fatal(vm.Resources[0].Data)
		}
		testWideStorageCall(t, s, vm, 0x82, 1)
		s.audio.Advance(25 * time.Millisecond)
		if sink.on != 1 {
			t.Fatalf("notes %d", sink.on)
		}
		testWideStorageCall(t, s, vm, 0x83)
		if s.audio.Playing(s.sound) {
			t.Fatal("sound still playing")
		}
	})
}

func TestWideNVAndByteAccess(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	seed := make([]byte, 64)
	binary.LittleEndian.PutUint32(seed, 0x78563412)
	s := &ScriptSession{options: ScriptOptions{SaveStore: store}, archive: &Archive{Entries: map[string][]byte{"usernv.sys": seed}}}
	runWideStorageTest(t, []sgsvm.Resource{{Data: make([]byte, 8), Mutable: true}}, func(vm *sgsvm.WideVM) {
		ref := int32(0)
		testWideStorageCall(t, s, vm, 0x93, ref, 1)
		testWideStorageResult(t, s, vm, 0x34, 0xaa, ref, 1)
		testWideStorageCall(t, s, vm, 0xa9, ref, 1, 0xfe)
		testWideStorageCall(t, s, vm, 0x94, ref, 1)
		got, found := store.LoadSave("nv/data")
		if !found || len(got) != 64 || binary.LittleEndian.Uint32(got) != 0x7856fe12 {
			t.Fatal(got)
		}
	})
}

func TestWideMemorySpansAndMutability(t *testing.T) {
	s := &ScriptSession{}
	resources := []sgsvm.Resource{{Data: []byte{0x12, 0x34, 0x56}}, {Data: make([]byte, 6), Mutable: true}}
	runWideStorageTest(t, resources, func(vm *sgsvm.WideVM) {
		testWideStorageCall(t, s, vm, 0xab, 0, 3, 0, 3)
		testWideStorageCall(t, s, vm, 0xac, 0, 3, 1, 3)
		if !bytes.Equal(vm.Resources[0].Data, vm.Resources[1].Data) {
			t.Fatal(vm.Resources)
		}
		testWideStorageResult(t, s, vm, 1, 0x9e, 1, 6)
		testWideStorageCall(t, s, vm, 0xbd, 0, 1, 1, 2)
		if !bytes.Equal(vm.Resources[1].Data[:4], []byte{0, 0x12, 0x34, 0x56}) {
			t.Fatal(vm.Resources[1].Data)
		}
		before := bytes.Clone(vm.Resources[1].Data)
		// Copying an out-of-range source must leave all destination bytes intact.
		for _, arg := range []int32{1, 0, 0, 2, 2} {
			if err := vm.Push(arg); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.wideStorage(0xbf, vm); err == nil {
			t.Fatal("out-of-range copy accepted")
		}
		if !bytes.Equal(before, vm.Resources[1].Data) {
			t.Fatal("partial copy")
		}
		testWideStorageResult(t, s, vm, 0, 0x9e, 1, 0x7fffffff)
	})
}

func TestWideSparseZeroLengthFileWrite(t *testing.T) {
	s := &ScriptSession{}
	runWideStorageTest(t, []sgsvm.Resource{{Data: []byte("slot\x00")}, {Mutable: true}}, func(vm *sgsvm.WideVM) {
		testWideStorageResult(t, s, vm, 0, 0x63, 0, 2)
		testWideStorageResult(t, s, vm, 1, 0x66, 0, 4096, 1)
		testWideStorageResult(t, s, vm, 0, 0x68, 0, 1, 0, 0)
		if got := len(*s.wideFiles[0].data); got != 0 {
			t.Fatalf("empty write grew file to %d", got)
		}
	})
}

func TestWideMediaRefusesGrowthBeyondRetainedHeap(t *testing.T) {
	runWideStorageTest(t, []sgsvm.Resource{{Mutable: true}, {Mutable: true}}, func(vm *sgsvm.WideVM) {
		vm.Resources[0].Data = make([]byte, 2, wideHeapLimit)
		s := &ScriptSession{}
		testWideStorageResult(t, s, vm, 0, 0x9e, 1, 4)
		if len(vm.Resources[1].Data) != 0 {
			t.Fatal("failed allocation resized guest media")
		}
	})
}

type wideFileFailStore struct {
	backend.SaveStore
	fail bool
}

func (store *wideFileFailStore) StoreSave(name string, data []byte) error {
	if store.fail {
		return fmt.Errorf("test store unavailable")
	}
	return store.SaveStore.StoreSave(name, data)
}

func TestWideFileFailedWriteRetainsPositionAndData(t *testing.T) {
	base, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &wideFileFailStore{SaveStore: base}
	s := &ScriptSession{options: ScriptOptions{SaveStore: store}}
	runWideStorageTest(t, []sgsvm.Resource{{Data: []byte("slot\x00")}, {Data: []byte("ab")}}, func(vm *sgsvm.WideVM) {
		testWideStorageResult(t, s, vm, 0, 0x63, 0, 2)
		store.fail = true
		for _, a := range []int32{0, 1, 0, 2} {
			if err := vm.Push(a); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.wideStorage(0x68, vm); err == nil {
			t.Fatal("failed save reported success")
		}
		h := s.wideFiles[0]
		if len(*h.data) != 0 || h.position != 0 {
			t.Fatal("failed write changed guest state")
		}
		store.fail = false
		testWideStorageResult(t, s, vm, 2, 0x68, 0, 1, 0, 2)
		testWideStorageResult(t, s, vm, 1, 0x64, 0)
		testWideStorageResult(t, s, vm, 0, 0x63, 0, 3)
		if !bytes.Equal(*s.wideFiles[0].data, []byte("ab")) {
			t.Fatal("retry wrote wrong contents")
		}
	})
}

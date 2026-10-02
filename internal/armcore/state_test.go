package armcore

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func TestStateMemoryRangeValidationIsBoundedAndReadOnly(t *testing.T) {
	core := NewCore(CoreOptions{})
	for _, address := range []uint32{0x1000, 0x3000} {
		if err := core.Memory().Map(address, 0x1000, PermissionRead); err != nil {
			t.Fatal(err)
		}
	}
	var group sync.WaitGroup
	for _, address := range []uint32{0x1000, 0x3000} {
		group.Go(func() {
			for i := 0; i < 100; i++ {
				if err := core.Memory().ValidateRange(address, 16, PermissionRead); err != nil {
					t.Error(err)
				}
			}
		})
	}
	group.Wait()
	if err := core.Memory().ValidateRange(0x1000, 16, PermissionWrite); !errors.Is(err, ErrPermission) {
		t.Fatalf("write validation=%v", err)
	}
	if err := core.Memory().ValidateRange(0x1000, ^uint64(0), PermissionRead); !errors.Is(err, ErrAddressOverflow) {
		t.Fatalf("overflow validation=%v", err)
	}
	if err := core.Memory().ValidateRange(0x1000, 0x3000, PermissionRead); !errors.Is(err, ErrUnmapped) {
		t.Fatalf("gap validation=%v", err)
	}
	saved, err := core.CaptureState()
	if err != nil || len(saved.Memory.Pages) != 0 {
		t.Fatalf("validation committed memory: pages=%d, %v", len(saved.Memory.Pages), err)
	}
}

func TestCoreStateRestoresSparseMemoryPermissionsAndPrivateWords(t *testing.T) {
	options := CoreOptions{MaxSteps: 37, Quantum: 11}
	core := NewCore(options)
	for _, region := range []struct {
		address    uint32
		size       uint64
		permission Permission
	}{
		{0x1003, 7, PermissionReadExecute}, {0x2000, 1 << 30, PermissionReadWrite},
		{0xfffffff8, 8, PermissionReadWrite},
	} {
		if err := core.Memory().Map(region.address, region.size, region.permission); err != nil {
			t.Fatal(err)
		}
	}
	if err := core.Memory().Load(0x1003, []byte{1, 2, 3, 4, 5, 6, 7}); err != nil {
		t.Fatal(err)
	}
	if err := core.Memory().Write(0x2000, []byte{7, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := core.RegisterThreadLocalWord(0x2000); err != nil {
		t.Fatal(err)
	}
	// Defaults are registered once; the backing byte and each thread's word
	// may all differ by the time a checkpoint is requested.
	if err := core.Memory().Write(0x2000, []byte{9, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := core.Memory().Write(0xfffffffc, []byte{11, 12, 13, 14}); err != nil {
		t.Fatal(err)
	}
	root := NewThread(NewContext())
	root.SetStepBudget(19)
	if err := core.SetThreadLocalWord(root, 0x2000, 75); err != nil {
		t.Fatal(err)
	}
	state, err := core.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	thread, err := core.CaptureRootThread(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Memory.Pages) != 3 {
		t.Fatalf("snapshot committed sparse zeros: pages=%d", len(state.Memory.Pages))
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CoreState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewCoreFromState(decoded, options)
	if err != nil {
		t.Fatal(err)
	}
	freshThread, err := fresh.RestoreRootThread(thread, 19)
	if err != nil {
		t.Fatal(err)
	}
	if word, err := fresh.ThreadLocalWord(freshThread, 0x2000); err != nil || word != 75 {
		t.Fatalf("private word = %d, %v", word, err)
	}
	if word, err := fresh.ThreadLocalWord(NewThread(NewContext()), 0x2000); err != nil || word != 7 {
		t.Fatalf("default word = %d, %v", word, err)
	}
	if err := fresh.Memory().Write(0x1003, []byte{99}); !errors.Is(err, ErrPermission) {
		t.Fatalf("read-only mapping = %v", err)
	}
	if err := fresh.Memory().Read(0x1002, make([]byte, 1)); !errors.Is(err, ErrUnmapped) {
		t.Fatalf("unmapped page prefix = %v", err)
	}
	for address, want := range map[uint32][]byte{0x1003: {1, 2, 3, 4, 5, 6, 7}, 0x2000: {9, 0, 0, 0}, 0xfffffffc: {11, 12, 13, 14}, 0x30000000: {0, 0, 0, 0}} {
		got := make([]byte, len(want))
		if err := fresh.Memory().Read(address, got); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("memory at %#x = %x, %v; want %x", address, got, err, want)
		}
	}
	decoded.Memory.Pages[0].Data[3] = 99
	state.Memory.Pages[0].Data[3] = 88
	for _, memory := range []*Memory{core.Memory(), fresh.Memory()} {
		var got [1]byte
		if err := memory.Read(0x1003, got[:]); err != nil || got[0] != 1 {
			t.Fatal("snapshot bytes alias live memory")
		}
	}
	if _, err := fresh.RestoreRootThread(thread, 20); err == nil {
		t.Fatal("restored thread accepted a different instruction budget")
	}
}

func TestCoreStateRejectsInvalidMemoryBeforeAdoption(t *testing.T) {
	core := NewCore(CoreOptions{})
	if err := core.Memory().Map(0x1000, 4096, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := core.Memory().Write(0x1000, []byte{42}); err != nil {
		t.Fatal(err)
	}
	state, err := core.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*CoreState)
	}{
		{"version", func(s *CoreState) { s.Version++ }},
		{"execution policy", func(s *CoreState) { s.MaxSteps++ }},
		{"mapping overflow", func(s *CoreState) { s.Memory.Mappings[0].Size = ^uint64(0) }},
		{"permission", func(s *CoreState) { s.Memory.Mappings[0].Permission = 0 }},
		{"page length", func(s *CoreState) { s.Memory.Pages[0].Data = []byte{1} }},
		{"page alignment", func(s *CoreState) { s.Memory.Pages[0].Address++ }},
		{"unmapped page", func(s *CoreState) { s.Memory.Pages[0].Address = 0x9000 }},
		{"duplicate page", func(s *CoreState) { s.Memory.Pages = append(s.Memory.Pages, s.Memory.Pages[0]) }},
		{"unmapped private default", func(s *CoreState) { s.Memory.ThreadLocal = []LocalWordState{{Address: 0x9000, Value: 3}} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			var bad CoreState
			if err := json.Unmarshal(data, &bad); err != nil {
				t.Fatal(err)
			}
			test.change(&bad)
			if restored, err := NewCoreFromState(bad, CoreOptions{}); err == nil || restored != nil {
				t.Fatalf("invalid state adopted: core=%v error=%v", restored, err)
			}
		})
	}
}

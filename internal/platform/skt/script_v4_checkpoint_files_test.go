package skt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestWideFileCheckpointRebindsAliasesAtTheirCursors(t *testing.T) {
	res := []sgsvm.Resource{{Data: []byte("slot")}, {Data: []byte("ab"), Mutable: true}}
	store, _ := backend.NewMemorySaveStore(nil)
	s := &ScriptSession{options: ScriptOptions{SaveStore: store}}
	runWideStorageTest(t, res, func(vm *sgsvm.WideVM) {
		testWideStorageResult(t, s, vm, 0, 0x63, 0, 2)
		testWideStorageResult(t, s, vm, 2, 0x68, 0, 1, 0, 2)
		testWideStorageResult(t, s, vm, 1, 0x63, 0, 3)
		testWideStorageResult(t, s, vm, 1, 0x66, 1, 1, 1)
	})
	saved := s.captureWideFiles()
	if saved[0] != (wideFileCheckpoint{Name: "slot", Position: 2}) || saved[1] != (wideFileCheckpoint{Name: "slot", Position: 1, ReadOnly: true}) {
		t.Fatalf("captured handles = %+v", saved[:2])
	}
	live, _ := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "files/slot", Data: backend.EncodeSaveRecords([][]byte{[]byte("WXYZ")})}})
	if err := s.restoreWideFiles(saved, live); err != nil {
		t.Fatal(err)
	}
	s.options.SaveStore = live
	runWideStorageTest(t, res, func(vm *sgsvm.WideVM) {
		testWideStorageResult(t, s, vm, -1, 0x68, 1, 1, 0, 1)
		testWideStorageResult(t, s, vm, 1, 0x6a, 1, 1, 0, 1)
		if vm.Resources[1].Data[0] != 'X' {
			t.Fatalf("restored reader read %q", vm.Resources[1].Data)
		}
		testWideStorageResult(t, s, vm, 1, 0x68, 0, 1, 1, 1)
		testWideStorageResult(t, s, vm, 1, 0x6a, 1, 1, 1, 1)
		if vm.Resources[1].Data[1] != 'b' {
			t.Fatal("restored reader missed a write through its alias")
		}
	})
	if raw, _ := live.LoadSave("files/slot"); !bytes.Equal(raw, backend.EncodeSaveRecords([][]byte{[]byte("WXbZ")})) {
		t.Fatalf("live file = %x", raw)
	}
}

func TestWideFileCheckpointMissingFile(t *testing.T) {
	for _, create := range []bool{false, true} {
		name := "write kept handle"
		if create {
			name = "create new alias"
		}
		t.Run(name, func(t *testing.T) {
			tombstone := backend.EncodeSaveRecords(nil)
			store, _ := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "files/slot", Data: tombstone}})
			s := &ScriptSession{archive: &Archive{Entries: map[string][]byte{"slot": []byte("seed")}}, options: ScriptOptions{SaveStore: store}}
			saved := [15]wideFileCheckpoint{{Name: "slot", Position: 2}, {Name: "slot", ReadOnly: true}}
			if err := s.restoreWideFiles(saved, store); err != nil {
				t.Fatal(err)
			}
			runWideStorageTest(t, []sgsvm.Resource{{Data: []byte("slot")}, {Data: []byte("a"), Mutable: true}}, func(vm *sgsvm.WideVM) {
				testWideStorageResult(t, s, vm, -1, 0x6b, 0)
				testWideStorageResult(t, s, vm, -1, 0x63, 0, 3)
				testWideStorageResult(t, s, vm, 0, 0x6a, 1, 1, 0, 1)
				if data, _ := store.LoadSave("files/slot"); !bytes.Equal(data, tombstone) {
					t.Fatalf("restore changed the deletion record to %x", data)
				}
				if create {
					testWideStorageResult(t, s, vm, 2, 0x63, 0, 2)
					testWideStorageResult(t, s, vm, 1, 0x6b, 0)
					testWideStorageResult(t, s, vm, 1, 0x68, 2, 1, 0, 1)
				}
				testWideStorageResult(t, s, vm, 1, 0x68, 0, 1, 0, 1)
				testWideStorageResult(t, s, vm, 1, 0x6b, 0)
				testWideStorageResult(t, s, vm, 1, 0x66, 1, 2, 1)
				testWideStorageResult(t, s, vm, 1, 0x6a, 1, 1, 0, 1)
			})
			want := []byte{0, 0, 'a'}
			if create {
				want[0] = 'a'
			}
			if raw, _ := store.LoadSave("files/slot"); !bytes.Equal(raw, backend.EncodeSaveRecords([][]byte{want})) {
				t.Fatalf("stored file = %x, want sparse %q", raw, want)
			}
		})
	}
}

func TestWideFileCheckpointRejectsInvalidAndUnreadableFilesAtomically(t *testing.T) {
	kept := []byte("old")
	s := &ScriptSession{wideFiles: [15]wideFileHandle{{name: "old", data: &kept}}}
	for _, invalid := range []wideFileCheckpoint{
		{Position: 1}, {ReadOnly: true}, {Name: "../slot"}, {Name: "./slot"}, {Name: "Slot"},
		{Name: "a:b"}, {Name: "a\nb"}, {Name: "a\\b"}, {Name: strings.Repeat("a", 507)},
		{Name: "slot", Position: -1}, {Name: "slot", Position: wideFileLimit + 1},
	} {
		if err := validateWideFiles([15]wideFileCheckpoint{invalid}); err == nil {
			t.Fatalf("accepted invalid handle %+v", invalid)
		}
	}
	saved := [15]wideFileCheckpoint{{Name: "empty"}, {Name: "bad"}}
	store, _ := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "files/empty", Data: backend.EncodeSaveRecords([][]byte{{}})}, {Key: "files/bad", Data: []byte{0}}})
	if err := s.restoreWideFiles(saved, store); err == nil || s.wideFiles[0].data != &kept || s.options.SaveStore != nil {
		t.Fatalf("failed restore changed session: err %v, handles %+v", err, s.wideFiles[:2])
	}
	saved[1] = wideFileCheckpoint{Name: "seed"}
	s.archive = &Archive{Entries: map[string][]byte{"seed": nil}}
	if err := s.restoreWideFiles(saved, store); err != nil {
		t.Fatal(err)
	}
	s.options.SaveStore = store
	runWideStorageTest(t, []sgsvm.Resource{{Data: []byte("empty")}, {Data: []byte("seed")}}, func(vm *sgsvm.WideVM) {
		testWideStorageResult(t, s, vm, 1, 0x6b, 0)
		testWideStorageResult(t, s, vm, 1, 0x6b, 1)
	})
}

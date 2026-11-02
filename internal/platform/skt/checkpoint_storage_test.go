package skt

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestJavaCheckpointSaveCacheCopiesShareAnAllocationBudget(t *testing.T) {
	for _, records := range []bool{false, true} {
		t.Run(map[bool]string{false: "files", true: "records"}[records], func(t *testing.T) {
			memory, _ := backend.NewMemorySaveStore(nil)
			store := &javaCheckpointFailStore{MemorySaveStore: memory}
			runtime := &Runtime{Archive: &Archive{Entries: map[string][]byte{}}}
			heap := newCheckpointHeap(runtime, time.Now())
			reads := 2
			if records {
				_ = memory.StoreSave(rmsIndexKey, []byte("same"))
				_ = memory.StoreSave("rms/same", backend.EncodeSaveRecords([][]byte{{1, 2, 3, 4, 5, 6, 7, 8}}))
				runtime.rmsState = &rmsState{stores: make(map[string]*recordStore)}
				for range 8 {
					heap.stores = append(heap.stores, &recordStore{name: "same", open: 1})
				}
			} else {
				// Handles on one path share its one file, so a copy is made per
				// path rather than per handle: eight paths overrun the budget,
				// after the removal list and the five paths read before it ran
				// out.
				for index := range 8 {
					name := fmt.Sprintf("file%d", index)
					_ = memory.StoreSave("fs/"+name, make([]byte, 16))
					heap.files = append(heap.files, &xFileData{name: name, open: true})
				}
				reads = 6
			}
			if err := heap.rebuildCheckpointSavesWithin(store, 64); !errors.Is(err, backend.ErrCheckpointSaveRead) {
				t.Fatalf("unbounded reconstruction: %v", err)
			}
			if store.writes != 0 || store.reads > reads {
				t.Fatalf("cache rebuild wrote or reread saves: %d/%d", store.writes, store.reads)
			}
		})
	}
}

// Eight handles on one path rebuild one shared copy: the budget a single copy
// fits is enough, the path is read once, and every handle reads the same file.
func TestJavaCheckpointHandlesOnOnePathShareOneCopy(t *testing.T) {
	memory, _ := backend.NewMemorySaveStore(nil)
	store := &javaCheckpointFailStore{MemorySaveStore: memory}
	_ = memory.StoreSave("fs/same", make([]byte, 16))
	runtime := &Runtime{Archive: &Archive{Entries: map[string][]byte{}}}
	heap := newCheckpointHeap(runtime, time.Now())
	for _, name := range []string{"same", "/same", "./same", "same", "same", "same", "same", "same"} {
		heap.files = append(heap.files, &xFileData{name: name, open: true})
	}
	if err := heap.rebuildCheckpointSavesWithin(store, 40); err != nil {
		t.Fatal(err)
	}
	if store.writes != 0 || store.reads > 2 {
		t.Fatalf("cache rebuild wrote or reread saves: %d/%d", store.writes, store.reads)
	}
	for _, file := range heap.files {
		if file.node == nil || file.node != heap.files[0].node || len(file.node.data) != 16 {
			t.Fatal("handles on one path were not rebuilt onto one file")
		}
	}
}

// A write through a second handle on the fixture's file lands in the one file
// both handles share, so its close stores both writes, and a quick save or a
// quick load after it has no older buffer to put over them.
func TestJavaCheckpointFlushPreservesLaterWrite(t *testing.T) {
	for _, load := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "load"}[load], func(t *testing.T) {
			store, _ := backend.NewMemorySaveStore(nil)
			source := startJavaCheckpoint(t, store)
			slot, err := source.CaptureCheckpointWithSession(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = source.VM.InvokeStatic("CheckpointMIDlet", "writeCurrent", "(I)V", jvm.IntValue(4)); err != nil {
				t.Fatal(err)
			}
			second := &jvm.Object{ClassName: skvm.XFileClass}
			receiver := jvm.ReferenceValue(second)
			if _, err = source.initXFileName(source.VM, []jvm.Value{receiver, jvm.ReferenceValue(source.VM.NewString("checkpoint")), jvm.IntValue(3)}); err != nil {
				t.Fatal(err)
			}
			if _, err = source.xFileSeek(source.VM, []jvm.Value{receiver, jvm.IntValue(1), jvm.IntValue(0)}); err != nil {
				t.Fatal(err)
			}
			if _, err = source.xFileWrite(source.VM, []jvm.Value{receiver, jvm.ReferenceValue(jvm.NewByteArray([]byte{9})), jvm.IntValue(0), jvm.IntValue(1)}); err != nil {
				t.Fatal(err)
			}
			if _, err = source.xFileClose(source.VM, []jvm.Value{receiver}); err != nil {
				t.Fatal(err)
			}
			before, _ := store.LoadSave("fs/checkpoint")
			if !bytes.Equal(before, []byte{4, 9, 3}) {
				t.Fatalf("the second handle's close stored %v, want both handles' writes", before)
			}
			if load {
				prepared, prepErr := PrepareJavaCheckpoint(javaCheckpointJAR, slot, Options{})
				if prepErr != nil {
					t.Fatal(prepErr)
				}
				defer prepared.Discard()
				restored, loadErr := prepared.Commit(t.Context(), source, store, newTestFramebuffer(t, 32, 24), nil)
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				defer restored.transition("test cleanup", StateDestroyed)
				if _, err = restored.VM.InvokeStatic("CheckpointMIDlet", "readCurrent", "()V"); err != nil {
					t.Fatal(err)
				}
				if got := checkpointJavaInt(t, restored, "readValue"); got != 404 {
					t.Fatalf("restored session read %d, want the later record and file 404", got)
				}
			} else if _, err = source.CaptureCheckpointWithSession(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			after, _ := store.LoadSave("fs/checkpoint")
			if !bytes.Equal(before, after) {
				t.Fatalf("checkpoint changed the later save: before=%v after=%v", before, after)
			}
		})
	}
}

// Handles on one path share its file, so the step before a checkpoint stores
// one current file per path — never an older handle's bytes over a newer
// write, a removal or a write a refused store left dirty — and a store that
// refuses it refuses the step with the file still dirty in the session.
func TestCheckpointFlushStoresTheCurrentFileOfEveryPath(t *testing.T) {
	for _, scenario := range []string{"aliases", "later removal", "failed later write", "open after failed write", "failed stream flush", "refused"} {
		t.Run(scenario, func(t *testing.T) {
			memory, _ := backend.NewMemorySaveStore(nil)
			store := &javaCheckpointFailStore{MemorySaveStore: memory}
			runtime := startJavaCheckpoint(t, store)
			if _, err := runtime.CaptureCheckpointWithSession(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			open := func(name string) jvm.Value {
				t.Helper()
				object := jvm.ReferenceValue(&jvm.Object{ClassName: skvm.XFileClass})
				if _, err := runtime.initXFileName(runtime.VM, []jvm.Value{object, jvm.ReferenceValue(runtime.VM.NewString(name)), jvm.IntValue(3)}); err != nil {
					t.Fatal(err)
				}
				return object
			}
			write := func(file jvm.Value, at int32, value byte) {
				t.Helper()
				if _, err := runtime.xFileSeek(runtime.VM, []jvm.Value{file, jvm.IntValue(at), jvm.IntValue(0)}); err != nil {
					t.Fatal(err)
				}
				if _, err := runtime.xFileWrite(runtime.VM, []jvm.Value{file, jvm.ReferenceValue(jvm.NewByteArray([]byte{value})), jvm.IntValue(0), jvm.IntValue(1)}); err != nil {
					t.Fatal(err)
				}
			}
			first, second := open("/checkpoint"), open("checkpoint")
			write(first, 0, 4)
			write(second, 1, 9)
			want := []byte{4, 9, 3}
			switch scenario {
			case "later removal":
				if _, err := runtime.xFileUnlink(runtime.VM, []jvm.Value{jvm.ReferenceValue(runtime.VM.NewString("checkpoint"))}); err != nil {
					t.Fatal(err)
				}
				want = nil
			case "failed later write", "open after failed write", "failed stream flush":
				store.failWrite = true
				if scenario == "failed stream flush" {
					handle, _ := xFileArgument([]jvm.Value{second})
					if err := runtime.persistWIPIFileStream(handle); err == nil {
						t.Fatal("stream flush unexpectedly succeeded")
					}
				} else if _, err := runtime.xFileFlush(runtime.VM, []jvm.Value{second}); err != nil {
					t.Fatal(err)
				}
				store.failWrite = false
				if scenario == "open after failed write" {
					write(open("/checkpoint"), 2, 7)
					want = []byte{4, 9, 7}
				}
			}
			write(open("another"), 0, 7)
			if scenario == "refused" {
				store.failWrite = true
				before, _ := memory.SnapshotSaves()
				if _, err := runtime.flushCheckpointSaves(t.Context()); !errors.Is(err, backend.ErrCheckpointSaveWrite) {
					t.Fatalf("refused flush: %v", err)
				}
				after, _ := memory.SnapshotSaves()
				handle, _ := xFileArgument([]jvm.Value{first})
				if !reflect.DeepEqual(before, after) || !handle.node.dirty {
					t.Fatal("a refused flush changed the saves or dropped the dirty file")
				}
				store.failWrite = false
			}
			if _, err := runtime.flushCheckpointSaves(t.Context()); err != nil {
				t.Fatal(err)
			}
			if data, _ := memory.LoadSave("fs/checkpoint"); !bytes.Equal(data, want) {
				t.Fatalf("flush stored %v for the shared path, want %v", data, want)
			}
			if data, _ := memory.LoadSave("fs/another"); !bytes.Equal(data, []byte{7}) {
				t.Fatalf("flush stored %v for the unrelated path", data)
			}
			if list, _ := memory.LoadSave(xFileRemovedKey); (scenario == "later removal") != bytes.Equal(list, []byte("checkpoint")) {
				t.Fatalf("removal list after the flush = %q", list)
			}
			runtime.fileMu.Lock()
			dirty := runtime.dirtyFileKeysLocked()
			runtime.fileMu.Unlock()
			if len(dirty) != 0 {
				t.Fatalf("a successful flush left %v dirty", dirty)
			}
		})
	}
}

func TestRMSCloseRetriesOnlyIssuedWrites(t *testing.T) {
	memory, _ := backend.NewMemorySaveStore(nil)
	boundary := &javaCheckpointFailStore{MemorySaveStore: memory}
	runtime := startJavaCheckpoint(t, boundary)
	boundary.failWrite = true
	value, err := runtime.rmsOpenRecordStore(runtime.VM, []jvm.Value{jvm.ReferenceValue(runtime.VM.NewString("fresh")), jvm.IntValue(1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.rmsAddRecord(runtime.VM, []jvm.Value{value, jvm.ReferenceValue(jvm.NewByteArray([]byte{9})), jvm.IntValue(0), jvm.IntValue(1)}); err != nil {
		t.Fatal(err)
	}
	boundary.failWrite = false
	if _, err = runtime.rmsCloseRecordStore(runtime.VM, []jvm.Value{value}); err != nil {
		t.Fatal(err)
	}
	index, _ := memory.LoadSave(rmsIndexKey)
	encoded, _ := memory.LoadSave("rms/fresh")
	records, err := backend.DecodeSaveRecords(encoded)
	if err != nil || !bytes.Contains(index, []byte("fresh")) || len(records) != 1 || !bytes.Equal(records[0], []byte{9}) || len(runtime.pendingSaves) != 0 {
		t.Fatalf("close lost issued writes: index=%q records=%v error=%v", index, records, err)
	}
}

func TestJavaCheckpointCloseKeepsLaterRMSDeletion(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	source := startJavaCheckpoint(t, store)
	slot, err := source.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := source.VM.StaticField("CheckpointMIDlet", "store", "Ljavax/microedition/rms/RecordStore;")
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.Reference()
	if _, err = source.VM.InvokeVirtual(object, "closeRecordStore", "()V"); err != nil {
		t.Fatal(err)
	}
	if _, err = source.rmsDeleteRecordStore(source.VM, []jvm.Value{jvm.ReferenceValue(source.VM.NewString("checkpoint"))}); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareJavaCheckpoint(javaCheckpointJAR, slot, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	restored, err := prepared.Commit(t.Context(), source, store, newTestFramebuffer(t, 32, 24), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.transition("test cleanup", StateDestroyed)
	index, _ := store.LoadSave(rmsIndexKey)
	if len(index) != 0 {
		t.Fatalf("deletion already lost at commit: %q", index)
	}
	value, err = restored.VM.StaticField("CheckpointMIDlet", "store", "Ljavax/microedition/rms/RecordStore;")
	if err != nil {
		t.Fatal(err)
	}
	object, _ = value.Reference()
	if _, err = restored.VM.InvokeVirtual(object, "closeRecordStore", "()V"); err != nil {
		t.Fatal(err)
	}
	index, _ = store.LoadSave(rmsIndexKey)
	if len(index) != 0 {
		t.Fatalf("closing unchanged restored handle recreated deleted RMS: %q", index)
	}
	// A subsequent real mutation through the restored open handle may create
	// the store again, with only the current records.
	preparedAgain, err := PrepareJavaCheckpoint(javaCheckpointJAR, slot, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer preparedAgain.Discard()
	mutating, err := preparedAgain.Commit(t.Context(), restored, store, newTestFramebuffer(t, 32, 24), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mutating.transition("test cleanup", StateDestroyed)
	value, err = mutating.VM.StaticField("CheckpointMIDlet", "store", "Ljavax/microedition/rms/RecordStore;")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mutating.rmsAddRecord(mutating.VM, []jvm.Value{value, jvm.ReferenceValue(jvm.NewByteArray([]byte{7})), jvm.IntValue(0), jvm.IntValue(1)}); err != nil {
		t.Fatal(err)
	}
	index, _ = store.LoadSave(rmsIndexKey)
	encoded, _ := store.LoadSave("rms/checkpoint")
	records, err := backend.DecodeSaveRecords(encoded)
	if err != nil || string(index) != "checkpoint" || len(records) != 1 || !bytes.Equal(records[0], []byte{7}) {
		t.Fatalf("new mutation failed to create current records: index=%q records=%v error=%v", index, records, err)
	}
}

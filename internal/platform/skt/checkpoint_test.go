package skt

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

//go:embed testdata/checkpoint.jar
var javaCheckpointJAR []byte

func checkpointJavaInt(t *testing.T, runtime *Runtime, field string) int32 {
	t.Helper()
	value, err := runtime.VM.StaticField("CheckpointMIDlet", field, "I")
	if err != nil {
		t.Fatal(err)
	}
	n, err := value.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func startJavaCheckpoint(t *testing.T, store backend.SaveStore) *Runtime {
	t.Helper()
	archive, err := Open(javaCheckpointJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24), SaveStore: store, Speed: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.transition("test cleanup", StateDestroyed) })
	deadline := time.Now().Add(time.Second)
	for checkpointJavaInt(t, runtime, "before") != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if checkpointJavaInt(t, runtime, "before") != 1 || checkpointJavaInt(t, runtime, "value") != 0 {
		t.Fatal("fixture did not initialize")
	}
	return runtime
}

func TestJavaCheckpointRestoresWorkersOverCurrentSaves(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	runtime := startJavaCheckpoint(t, store)
	saved, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if data, ok := store.LoadSave("fs/checkpoint"); !ok || !bytes.Equal(data, []byte{1, 2, 3}) {
		t.Fatalf("pending file was not flushed: %v", data)
	}
	prepared, err := PrepareJavaCheckpoint(javaCheckpointJAR, saved, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if calls, _ := prepared.PreparationStoreCalls(); calls != 0 {
		t.Fatal("detached validation accessed saves")
	}
	if err = store.StoreSave("rms/checkpoint", backend.EncodeSaveRecords([][]byte{{9}})); err != nil {
		t.Fatal(err)
	}
	if err = store.StoreSave("fs/checkpoint", []byte{9, 8, 7}); err != nil {
		t.Fatal(err)
	}
	restored, err := prepared.Commit(t.Context(), runtime, store, newTestFramebuffer(t, 32, 24), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.transition("test cleanup", StateDestroyed)
	if checkpointJavaInt(t, restored, "starts") != 1 || checkpointJavaInt(t, restored, "destroys") != 0 || checkpointJavaInt(t, restored, "before") != 1 || checkpointJavaInt(t, restored, "after") != 0 {
		t.Fatal("restoration replayed startup, shutdown or worker prefix")
	}
	fileValue, _ := restored.VM.StaticField("CheckpointMIDlet", "file", "Lcom/xce/io/XFile;")
	fileObject, _ := fileValue.Reference()
	if fileObject.Native.(*xFileData).cursor != 3 {
		t.Fatal("file cursor was not restored")
	}
	if _, err = restored.VM.InvokeStatic("CheckpointMIDlet", "readCurrent", "()V"); err != nil {
		t.Fatal(err)
	}
	if got := checkpointJavaInt(t, restored, "readValue"); got != 909 {
		t.Fatalf("restored caches read %d, want current RMS/file 909", got)
	}
	threadValue, _ := restored.VM.StaticField("CheckpointMIDlet", "worker", "Ljava/lang/Thread;")
	thread, _ := threadValue.Reference()
	if _, err = restored.VM.InvokeVirtual(thread, "interrupt", "()V"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for checkpointJavaInt(t, restored, "after") != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if checkpointJavaInt(t, restored, "before") != 1 || checkpointJavaInt(t, restored, "after") != 1 {
		t.Fatal("restored sleep did not complete exactly once")
	}
	if _, err = restored.VM.InvokeStatic("CheckpointMIDlet", "writeCurrent", "(I)V", jvm.IntValue(6)); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.CaptureCheckpointWithSession(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if data, _ := store.LoadSave("fs/checkpoint"); !bytes.Equal(data, []byte{6, 8, 7}) {
		t.Fatalf("continued write did not use current file: %v", data)
	}
}

type javaCheckpointFailStore struct {
	*backend.MemorySaveStore
	failRead, failWrite bool
	reads, writes       int
}

func (s *javaCheckpointFailStore) ReadSave(key string) ([]byte, bool, error) {
	s.reads++
	if s.failRead {
		return nil, false, errors.New("test read refusal")
	}
	data, ok := s.LoadSave(key)
	return data, ok, nil
}
func (s *javaCheckpointFailStore) StoreSave(key string, data []byte) error {
	s.writes++
	if s.failWrite {
		return errors.New("test write refusal")
	}
	return s.MemorySaveStore.StoreSave(key, data)
}

func TestJavaCheckpointFailedStorageKeepsSourceAndIssuedWrites(t *testing.T) {
	memory, _ := backend.NewMemorySaveStore(nil)
	store := &javaCheckpointFailStore{MemorySaveStore: memory}
	runtime := startJavaCheckpoint(t, store)
	saved, err := runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store.failWrite = true
	if _, err = runtime.VM.InvokeStatic("CheckpointMIDlet", "writeCurrent", "(I)V", jvm.IntValue(4)); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.CaptureCheckpointWithSession(t.Context(), nil); !errors.Is(err, backend.ErrCheckpointSaveWrite) {
		t.Fatalf("capture write failure: %v", err)
	}
	if runtime.State() != StateActive || len(runtime.pendingSaves) == 0 {
		t.Fatal("failed capture discarded source or pending writes")
	}
	prepared, err := PrepareJavaCheckpoint(javaCheckpointJAR, saved, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	store.failRead = true
	writes := store.writes
	if _, err = prepared.Commit(t.Context(), runtime, store, newTestFramebuffer(t, 32, 24), nil); !errors.Is(err, backend.ErrCheckpointSaveRead) {
		t.Fatalf("load read failure: %v", err)
	}
	if runtime.State() != StateActive || store.writes != writes {
		t.Fatal("failed read displaced source or attempted writes")
	}
	store.failRead = false
	if _, err = prepared.Commit(t.Context(), runtime, store, newTestFramebuffer(t, 32, 24), nil); !errors.Is(err, backend.ErrCheckpointSaveWrite) {
		t.Fatalf("load write failure: %v", err)
	}
	store.failWrite = false
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = prepared.Commit(canceled, runtime, store, newTestFramebuffer(t, 32, 24), nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	restored, err := prepared.Commit(t.Context(), runtime, store, newTestFramebuffer(t, 32, 24), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.transition("test cleanup", StateDestroyed)
	if _, err = restored.VM.InvokeStatic("CheckpointMIDlet", "readCurrent", "()V"); err != nil {
		t.Fatal(err)
	}
	if checkpointJavaInt(t, restored, "readValue") != 404 {
		t.Fatal("load lost the source's already-issued writes")
	}
}

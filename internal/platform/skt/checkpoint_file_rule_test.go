package skt

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// previousXFileRecord is the "skt.file" payload as the build before the shared
// file state wrote it: provenance, mode, open state and cursor, and no bytes.
// It is spelled out here rather than taken from checkpointXFile so that a
// later change to that record cannot quietly change what this test feeds the
// restore.
type previousXFileRecord struct {
	Name, Archive, Entry string
	Mode                 int32
	Cursor               int
	Open, Writable       bool
}

// rewriteFilePayloadsInPreviousForm re-encodes every file payload of a Java
// checkpoint in the previous build's record form and answers how many it
// rewrote.
func rewriteFilePayloadsInPreviousForm(t *testing.T, checkpoint *backend.Checkpoint) int {
	t.Helper()
	var saved javaCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		t.Fatal(err)
	}
	rewritten := 0
	for index, payload := range saved.Threads.Heap.Payloads {
		if payload.Kind != "external" || payload.ExternalKind != "skt.file" {
			continue
		}
		var previous previousXFileRecord
		if err := backend.DecodeCheckpointRecord(payload.Data, &previous); err != nil {
			t.Fatal(err)
		}
		data, err := backend.EncodeCheckpointRecord(previous)
		if err != nil {
			t.Fatal(err)
		}
		saved.Threads.Heap.Payloads[index].Data = data
		rewritten++
	}
	encoded, err := backend.EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint.Runtime = encoded
	return rewritten
}

// readWIPIHandle reads one byte through a File handle the stream registry
// holds, from that handle's own position.
func readWIPIHandle(t *testing.T, runtime *Runtime, handle *xFileData) int32 {
	t.Helper()
	buffer := jvm.NewByteArray(make([]byte, 1))
	receiver := jvm.ReferenceValue(&jvm.Object{ClassName: wipi.FileClass, Native: handle})
	value, err := runtime.xFileRead(runtime.VM, []jvm.Value{receiver, jvm.ReferenceValue(buffer), jvm.IntValue(0), jvm.IntValue(1)})
	if err != nil {
		t.Fatal(err)
	}
	if count := storeReviewInt(t, value); count != 1 {
		return count
	}
	data, _ := jvm.ByteArraySnapshot(buffer)
	return int32(data[0])
}

// A checkpoint whose file handles are in the previous build's form restores
// onto the shared file state: every handle on one path — the title's XFile,
// a WIPI File and the input stream it opened — is rebuilt from the current
// save onto one file, keeps its own cursor, and sees the others' writes; an
// empty file an earlier build's unlink left stays a present empty file; and the
// restored session's writes are stored when it ends.
func TestJavaCheckpointInThePreviousFileFormRestoresOntoSharedFiles(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	if err := store.StoreSave("fs/gone", nil); err != nil {
		t.Fatal(err)
	}
	source := startJavaCheckpoint(t, store)
	for _, open := range []struct {
		name string
		mode int32
	}{{"checkpoint", wipiFileReadWrite}, {"gone", wipiFileReadOnly}} {
		file, err := source.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;I)V", jvm.ReferenceValue(source.VM.NewString(open.name)), jvm.IntValue(open.mode))
		if err != nil {
			t.Fatalf("open %q: %v", open.name, err)
		}
		if _, err := source.VM.InvokeVirtual(file, "openInputStream", "()Ljava/io/InputStream;"); err != nil {
			t.Fatal(err)
		}
	}
	slot, err := source.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture's own XFile, and each File with the input stream it opened.
	if rewritten := rewriteFilePayloadsInPreviousForm(t, &slot); rewritten < 5 {
		t.Fatalf("rewrote %d file payloads, want the fixture's XFile and two File/stream pairs", rewritten)
	}
	prepared, err := PrepareJavaCheckpoint(javaCheckpointJAR, slot, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	if err := store.StoreSave("fs/checkpoint", []byte{9, 8, 7}); err != nil {
		t.Fatal(err)
	}
	restored, err := prepared.Commit(t.Context(), source, store, newTestFramebuffer(t, 32, 24), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.transition("test cleanup", StateDestroyed)

	if _, err := restored.VM.InvokeStatic("CheckpointMIDlet", "readCurrent", "()V"); err != nil {
		t.Fatal(err)
	}
	if got := checkpointJavaInt(t, restored, "readValue"); got != 109 {
		t.Fatalf("restored XFile read %d, want record 1 and the current file's 9", got)
	}
	var shared, gone *xFileData
	var sharedStream *jvm.Object
	for stream, handle := range restored.wipiFileStreams {
		switch handle.name {
		case "checkpoint":
			shared, sharedStream = handle, stream
		case "gone":
			gone = handle
		}
	}
	if shared == nil || gone == nil {
		t.Fatal("restored stream registry lost a File")
	}
	if value := storeReviewInt(t, wipiStreamCall(t, restored, sharedStream, "read", "()I")); value != 9 {
		t.Fatalf("restored input stream read %d, want the current file's first byte", value)
	}
	// The title writes through its XFile; the File restored beside it reads
	// that write from its own position, before anything is flushed.
	if _, err := restored.VM.InvokeStatic("CheckpointMIDlet", "writeCurrent", "(I)V", jvm.IntValue(6)); err != nil {
		t.Fatal(err)
	}
	if value := readWIPIHandle(t, restored, shared); value != 6 {
		t.Fatalf("restored File read %d after the XFile's write, want 6", value)
	}
	if value := storeReviewInt(t, wipiStreamCall(t, restored, sharedStream, "read", "()I")); value != 8 {
		t.Fatalf("restored input stream lost its own position: read %d, want 8", value)
	}
	if value := readWIPIHandle(t, restored, gone); value != -1 {
		t.Fatalf("a File on an earlier build's empty file read %d, want the end", value)
	}
	if !ruleExists(t, restored, "gone") || ruleSize(t, restored, "gone") != 0 {
		t.Fatal("an earlier build's empty file is not a present empty file after the restore")
	}
	if err := restored.Destroy(true); err != nil {
		t.Fatal(err)
	}
	if data, _ := store.LoadSave("fs/checkpoint"); !bytes.Equal(data, []byte{6, 8, 7}) {
		t.Fatalf("the restored session's write was not stored at its end: %v", data)
	}
}

// A quick load does not make a file again because a restored handle had it
// open for writing: the saves as they are when the load commits decide, so a
// removal made after the quick save stays, and a write through the restored
// handle is what brings the file back.
func TestJavaCheckpointRestoredWritableHandleKeepsALaterRemoval(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	source := startJavaCheckpoint(t, store)
	file, err := source.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;I)V", jvm.ReferenceValue(source.VM.NewString("made")), jvm.IntValue(wipiFileWrite))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.VM.InvokeVirtual(file, "openOutputStream", "()Ljava/io/OutputStream;"); err != nil {
		t.Fatal(err)
	}
	slot, err := source.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSaves(map[string][]byte{"fs/made": nil, "fs/.removed": []byte("made")}); err != nil {
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
	if ruleExists(t, restored, "made") {
		t.Fatal("the restored writable handle made the removed file again")
	}
	var output *jvm.Object
	for stream, handle := range restored.wipiFileStreams {
		if handle.name == "made" && stream.ClassName == wipiFileOutputStreamClass {
			output = stream
		}
	}
	if output == nil {
		t.Fatal("the restored stream registry lost the output stream")
	}
	wipiStreamCall(t, restored, output, "write", "(I)V", jvm.IntValue(5))
	if !ruleExists(t, restored, "made") || ruleSize(t, restored, "made") != 1 {
		t.Fatal("a write through the restored handle did not bring the file back")
	}
	if err := restored.Destroy(true); err != nil {
		t.Fatal(err)
	}
	if data, _ := store.LoadSave("fs/made"); !bytes.Equal(data, []byte{5}) {
		t.Fatalf("the file brought back holds %v, want the write", data)
	}
	if list, _ := store.LoadSave("fs/.removed"); len(list) != 0 {
		t.Fatalf("removal list after the file came back = %q", list)
	}
}

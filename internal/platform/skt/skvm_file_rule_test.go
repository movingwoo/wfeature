package skt

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// These tests hold the SKT file layer to the save rule KTF, LGT and SKT share:
// a write or a truncation is seen at once by every later read of the path; what
// a title wrote is stored by its flush or close and in any case when the
// session ends; a truncating open empties the stored file at the open; a
// removed file is absent now and in later sessions, until it is written again;
// and nothing else writes saves. Each rule is checked through XFile, the WIPI
// File and FileSystem classes, and the com.xce.io streams.

// fileRuleStore is a memory save store that records every key written to it
// and can be told to refuse writes. A batch records each of its keys.
type fileRuleStore struct {
	*backend.MemorySaveStore
	mu      sync.Mutex
	refuse  bool
	writes  []string
	batches [][]string
}

func newFileRuleStore(t *testing.T, entries map[string][]byte) *fileRuleStore {
	t.Helper()
	memory, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, data := range entries {
		if err := memory.StoreSave(key, data); err != nil {
			t.Fatal(err)
		}
	}
	return &fileRuleStore{MemorySaveStore: memory}
}

func (store *fileRuleStore) StoreSave(name string, data []byte) error {
	return store.StoreSaves(map[string][]byte{name: data})
}

func (store *fileRuleStore) StoreSaves(entries map[string][]byte) error {
	store.mu.Lock()
	refuse := store.refuse
	batch := make([]string, 0, len(entries))
	for name := range entries {
		store.writes = append(store.writes, name)
		batch = append(batch, name)
	}
	slices.Sort(batch)
	store.batches = append(store.batches, batch)
	store.mu.Unlock()
	if refuse {
		return errors.New("save write refused")
	}
	return store.MemorySaveStore.StoreSaves(entries)
}

func (store *fileRuleStore) setRefuse(refuse bool) {
	store.mu.Lock()
	store.refuse = refuse
	store.mu.Unlock()
}

// written answers the keys written since the last call, sorted, and forgets
// them.
func (store *fileRuleStore) written() []string {
	store.mu.Lock()
	defer store.mu.Unlock()
	keys := slices.Clone(store.writes)
	store.writes = nil
	slices.Sort(keys)
	return keys
}

// takeBatches answers the writes since the last call, one sorted list of keys
// per call that reached the store, and forgets them.
func (store *fileRuleStore) takeBatches() [][]string {
	store.mu.Lock()
	defer store.mu.Unlock()
	batches := store.batches
	store.batches = nil
	return batches
}

func (store *fileRuleStore) removalList() []string {
	data, _ := store.LoadSave("fs/.removed")
	var names []string
	for _, name := range strings.Split(string(data), "\n") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// fileRuleRuntime starts a session over store, with packaged installed as
// the archive's own files.
func fileRuleRuntime(t *testing.T, store backend.SaveStore, packaged map[string][]byte) *Runtime {
	t.Helper()
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range packaged {
		archive.Entries[name] = data
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3), SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	return runtime
}

func ruleText(runtime *Runtime, text string) jvm.Value {
	return jvm.ReferenceValue(runtime.VM.NewString(text))
}

func ruleBytes(data ...byte) jvm.Value {
	return jvm.ReferenceValue(jvm.NewByteArray(data))
}

func ruleNew(t *testing.T, runtime *Runtime, class, descriptor string, arguments ...jvm.Value) *jvm.Object {
	t.Helper()
	object, err := runtime.VM.NewObject(class, descriptor, arguments...)
	if err != nil {
		t.Fatalf("new %s%s: %v", class, descriptor, err)
	}
	return object
}

func ruleXFile(t *testing.T, runtime *Runtime, name string, mode int32) *jvm.Object {
	t.Helper()
	return ruleNew(t, runtime, skvm.XFileClass, "(Ljava/lang/String;I)V", ruleText(runtime, name), jvm.IntValue(mode))
}

func ruleXWrite(t *testing.T, runtime *Runtime, file *jvm.Object, data ...byte) {
	t.Helper()
	written := storeReviewInt(t, wipiStreamCall(t, runtime, file, "write", "([BII)I", ruleBytes(data...), jvm.IntValue(0), jvm.IntValue(int32(len(data)))))
	if written != int32(len(data)) {
		t.Fatalf("XFile.write = %d, want %d", written, len(data))
	}
}

// ruleXRead reads up to limit bytes from the handle's position.
func ruleXRead(t *testing.T, runtime *Runtime, file *jvm.Object, limit int) []byte {
	t.Helper()
	buffer := jvm.NewByteArray(make([]byte, limit))
	count := storeReviewInt(t, wipiStreamCall(t, runtime, file, "read", "([BII)I", jvm.ReferenceValue(buffer), jvm.IntValue(0), jvm.IntValue(int32(limit))))
	data, err := jvm.ByteArraySnapshot(buffer)
	if err != nil {
		t.Fatal(err)
	}
	return data[:max(count, 0)]
}

func ruleStatic(t *testing.T, runtime *Runtime, class, method, descriptor string, arguments ...jvm.Value) int32 {
	t.Helper()
	value, err := runtime.VM.InvokeStatic(class, method, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s.%s%s: %v", class, method, descriptor, err)
	}
	return storeReviewInt(t, value)
}

func ruleRemove(t *testing.T, runtime *Runtime, name string) {
	t.Helper()
	if _, err := runtime.VM.InvokeStatic(wipi.FileSystemClass, "remove", "(Ljava/lang/String;)V", ruleText(runtime, name)); err != nil {
		t.Fatalf("FileSystem.remove(%q): %v", name, err)
	}
}

func ruleExists(t *testing.T, runtime *Runtime, name string) bool {
	t.Helper()
	return ruleStatic(t, runtime, skvm.XFileClass, "exists", "(Ljava/lang/String;)Z", ruleText(runtime, name)) != 0
}

func ruleSize(t *testing.T, runtime *Runtime, name string) int32 {
	t.Helper()
	return ruleStatic(t, runtime, skvm.XFileClass, "filesize", "(Ljava/lang/String;)I", ruleText(runtime, name))
}

func ruleUnlink(t *testing.T, runtime *Runtime, name string) int32 {
	t.Helper()
	return ruleStatic(t, runtime, skvm.XFileClass, "unlink", "(Ljava/lang/String;)I", ruleText(runtime, name))
}

// ruleFileSystem answers what FileSystem.exists, isFile and sizeOf say of a
// name.
func ruleFileSystem(t *testing.T, runtime *Runtime, name string) (exists, isFile bool, size int32) {
	t.Helper()
	exists = ruleStatic(t, runtime, wipi.FileSystemClass, "exists", "(Ljava/lang/String;)Z", ruleText(runtime, name)) != 0
	isFile = ruleStatic(t, runtime, wipi.FileSystemClass, "isFile", "(Ljava/lang/String;)Z", ruleText(runtime, name)) != 0
	size = ruleStatic(t, runtime, wipi.FileSystemClass, "sizeOf", "(Ljava/lang/String;)I", ruleText(runtime, name))
	return exists, isFile, size
}

func ruleStoreHolds(t *testing.T, store *fileRuleStore, key string, want []byte) {
	t.Helper()
	data, ok := store.LoadSave(key)
	if !ok || !bytes.Equal(data, want) {
		t.Fatalf("store %s = %v (present %v), want %v", key, data, ok, want)
	}
}

// --- Rule 1: visibility ---

// A write through one XFile is what every later read of the path answers:
// a handle opened before it, a new open under another spelling of the path,
// exists and filesize. Nothing is stored for it until a flush.
func TestXFileWriteIsSeenAtOnceByEveryRead(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/slot": {1, 2, 3}})
	runtime := fileRuleRuntime(t, store, nil)
	early := ruleXFile(t, runtime, "slot", xFileRead)
	writer := ruleXFile(t, runtime, "/slot", xFileWrite)
	ruleXWrite(t, runtime, writer, 9, 9, 9, 9)
	if size := ruleSize(t, runtime, "slot"); size != 4 {
		t.Fatalf("filesize after an unflushed write = %d, want 4", size)
	}
	if got := ruleXRead(t, runtime, early, 8); !bytes.Equal(got, []byte{9, 9, 9, 9}) {
		t.Fatalf("a handle opened before the write read %v", got)
	}
	if got := ruleXRead(t, runtime, ruleXFile(t, runtime, "slot", xFileRead), 8); !bytes.Equal(got, []byte{9, 9, 9, 9}) {
		t.Fatalf("a new open read %v", got)
	}

	// A path with nothing behind it exists from the open for writing, empty,
	// and then holds what is written.
	fresh := ruleXFile(t, runtime, "fresh", xFileWrite)
	if !ruleExists(t, runtime, "fresh") || ruleSize(t, runtime, "fresh") != 0 {
		t.Fatal("opening a missing path for writing did not make an empty file")
	}
	ruleXWrite(t, runtime, fresh, 5)
	if !ruleExists(t, runtime, "fresh") || ruleSize(t, runtime, "fresh") != 1 {
		t.Fatal("a written path is not visible to exists and filesize")
	}
	if got := ruleXRead(t, runtime, ruleXFile(t, runtime, "fresh", xFileRead), 4); !bytes.Equal(got, []byte{5}) {
		t.Fatalf("read-only open of a written path read %v", got)
	}
	if written := store.written(); len(written) != 0 {
		t.Fatalf("writes reached the store before any flush: %v", written)
	}

	// A flush stores the path as every handle sees it.
	wipiStreamCall(t, runtime, writer, "flush", "()V")
	ruleStoreHolds(t, store, "fs/slot", []byte{9, 9, 9, 9})
}

// The WIPI File surface reads the same shared file: FileSystem's three
// questions, sizeOf on another File, an input stream opened before the write,
// and a truncating open, all without a close.
func TestWIPIFileWriteIsSeenAtOnceByEveryRead(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/save": {1, 2}})
	runtime := fileRuleRuntime(t, store, nil)
	early := wipiStreamFile(t, runtime, "save", wipiFileReadOnly)
	input := wipiOpenStream(t, runtime, early, "openInputStream", jvm.InputStreamClass)
	writer := wipiStreamFile(t, runtime, "save", wipiFileReadWrite)
	wipiStreamCall(t, runtime, writer, "write", "([B)I", ruleBytes(7, 7, 7))

	if exists, isFile, size := ruleFileSystem(t, runtime, "save"); !exists || !isFile || size != 3 {
		t.Fatalf("FileSystem after an unclosed write: exists=%v isFile=%v sizeOf=%d, want true true 3", exists, isFile, size)
	}
	if size := storeReviewInt(t, wipiStreamCall(t, runtime, early, "sizeOf", "()I")); size != 3 {
		t.Fatalf("File.sizeOf on a handle opened before the write = %d, want 3", size)
	}
	if value := storeReviewInt(t, wipiStreamCall(t, runtime, input, "read", "()I")); value != 7 {
		t.Fatalf("input stream opened before the write read %d, want 7", value)
	}
	reader := wipiStreamFile(t, runtime, "save", wipiFileReadOnly)
	buffer := jvm.NewByteArray(make([]byte, 8))
	if count := storeReviewInt(t, wipiStreamCall(t, runtime, reader, "read", "([B)I", jvm.ReferenceValue(buffer))); count != 3 {
		t.Fatalf("a new READ_ONLY open read %d bytes, want 3", count)
	}

	fresh := wipiStreamFile(t, runtime, "fresh", wipiFileWrite)
	if exists, isFile, size := ruleFileSystem(t, runtime, "fresh"); !exists || !isFile || size != 0 {
		t.Fatalf("WRITE open of a missing path: exists=%v isFile=%v sizeOf=%d, want an empty file", exists, isFile, size)
	}
	wipiStreamCall(t, runtime, fresh, "write", "([B)I", ruleBytes(1))
	if exists, isFile, size := ruleFileSystem(t, runtime, "fresh"); !exists || !isFile || size != 1 {
		t.Fatalf("FileSystem after a first write: exists=%v isFile=%v sizeOf=%d", exists, isFile, size)
	}

	// The truncation is seen by every reader at once, the open stream included.
	wipiStreamFile(t, runtime, "save", wipiFileWriteTrunc)
	if _, _, size := ruleFileSystem(t, runtime, "save"); size != 0 {
		t.Fatalf("FileSystem.sizeOf after WRITE_TRUNC = %d, want 0", size)
	}
	if size := storeReviewInt(t, wipiStreamCall(t, runtime, early, "sizeOf", "()I")); size != 0 {
		t.Fatalf("File.sizeOf after another handle's WRITE_TRUNC = %d, want 0", size)
	}
	if value := storeReviewInt(t, wipiStreamCall(t, runtime, input, "read", "()I")); value != -1 {
		t.Fatalf("input stream after WRITE_TRUNC read %d, want the end", value)
	}
}

// What a FileOutputStream writes is what a FileInputStream opened later
// reads, and what the appending form adds is seen by the stream already open.
func TestXCEStreamWriteIsSeenAtOnceByEveryRead(t *testing.T) {
	store := newFileRuleStore(t, nil)
	runtime := fileRuleRuntime(t, store, nil)
	output := ruleNew(t, runtime, skvm.FileOutputStreamClass, "(Ljava/lang/String;)V", ruleText(runtime, "stream"))
	wipiStreamCall(t, runtime, output, "write", "([BII)V", ruleBytes(4, 5), jvm.IntValue(0), jvm.IntValue(2))
	if size := ruleSize(t, runtime, "stream"); size != 2 {
		t.Fatalf("filesize after an unclosed stream write = %d, want 2", size)
	}
	input := ruleNew(t, runtime, skvm.FileInputStreamClass, "(Ljava/lang/String;)V", ruleText(runtime, "stream"))
	for _, want := range []int32{4, 5, -1} {
		if value := storeReviewInt(t, wipiStreamCall(t, runtime, input, "read", "()I")); value != want {
			t.Fatalf("FileInputStream.read = %d, want %d", value, want)
		}
	}
	appender := ruleNew(t, runtime, skvm.FileOutputStreamClass, "(Ljava/lang/String;Z)V", ruleText(runtime, "stream"), jvm.IntValue(1))
	wipiStreamCall(t, runtime, appender, "write", "(I)V", jvm.IntValue(6))
	if value := storeReviewInt(t, wipiStreamCall(t, runtime, input, "read", "()I")); value != 6 {
		t.Fatalf("an open FileInputStream read %d after an append, want 6", value)
	}
}

// --- Rule 2: durability ---

// A write nobody flushed or closed is stored when the session ends, however it
// ends, and a write the store refused is kept for that end.
func TestXFileWritesAreStoredWhenTheSessionEnds(t *testing.T) {
	for _, ending := range []struct {
		name  string
		state LifecycleState
		end   func(*testing.T, *Runtime)
	}{
		{"destroy", StateDestroyed, func(t *testing.T, runtime *Runtime) {
			if err := runtime.Destroy(true); err != nil {
				t.Fatal(err)
			}
		}},
		{"exit", StateDestroyed, func(t *testing.T, runtime *Runtime) {
			if err := runtime.exit(0); err != nil {
				t.Fatal(err)
			}
			_ = runtime.RunPending()
		}},
		{"notifyDestroyed", StateDestroyed, func(t *testing.T, runtime *Runtime) {
			if _, err := runtime.notifyDestroyed(runtime.VM, []jvm.Value{jvm.ReferenceValue(runtime.MIDlet)}); err != nil {
				t.Fatal(err)
			}
			_ = runtime.RunPending()
		}},
		{"failure", StateError, func(t *testing.T, runtime *Runtime) {
			_ = runtime.fail("test failure", errors.New("test failure"))
		}},
	} {
		t.Run(ending.name, func(t *testing.T) {
			store := newFileRuleStore(t, nil)
			runtime := fileRuleRuntime(t, store, nil)
			ruleXWrite(t, runtime, ruleXFile(t, runtime, "keep", xFileWrite), 1, 2, 3)
			ending.end(t, runtime)
			if state := runtime.State(); state != ending.state {
				t.Fatalf("state = %s, want %s", state, ending.state)
			}
			ruleStoreHolds(t, store, "fs/keep", []byte{1, 2, 3})
		})
	}
	t.Run("refused flush", func(t *testing.T) {
		store := newFileRuleStore(t, nil)
		runtime := fileRuleRuntime(t, store, nil)
		file := ruleXFile(t, runtime, "keep", xFileWrite)
		ruleXWrite(t, runtime, file, 4, 2)
		store.setRefuse(true)
		wipiStreamCall(t, runtime, file, "flush", "()V")
		store.setRefuse(false)
		if err := runtime.Destroy(true); err != nil {
			t.Fatal(err)
		}
		ruleStoreHolds(t, store, "fs/keep", []byte{4, 2})
	})
}

// The WIPI File and its output stream are stored at the end of the session
// without a close or a flush.
func TestWIPIFileWritesAreStoredWhenTheSessionEnds(t *testing.T) {
	store := newFileRuleStore(t, nil)
	runtime := fileRuleRuntime(t, store, nil)
	file := wipiStreamFile(t, runtime, "file", wipiFileReadWrite)
	wipiStreamCall(t, runtime, file, "write", "([B)I", ruleBytes(8, 1))
	stream := wipiOpenStream(t, runtime, wipiStreamFile(t, runtime, "stream", wipiFileWrite), "openDataOutputStream", "java/io/DataOutputStream")
	wipiStreamCall(t, runtime, stream, "writeShort", "(I)V", jvm.IntValue(0x1234))
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	ruleStoreHolds(t, store, "fs/file", []byte{8, 1})
	ruleStoreHolds(t, store, "fs/stream", []byte{0x12, 0x34})
}

// A FileOutputStream nobody closed is stored when the session ends.
func TestXCEStreamWritesAreStoredWhenTheSessionEnds(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/stream": {9, 9, 9}})
	runtime := fileRuleRuntime(t, store, nil)
	output := ruleNew(t, runtime, skvm.FileOutputStreamClass, "(Ljava/lang/String;)V", ruleText(runtime, "stream"))
	wipiStreamCall(t, runtime, output, "write", "([BII)V", ruleBytes(3, 1), jvm.IntValue(0), jvm.IntValue(2))
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	ruleStoreHolds(t, store, "fs/stream", []byte{3, 1})
}

// --- Rule 3: truncation ---

// XFile's own modes never truncate, and the truncation a FileOutputStream
// makes is in the store at once, as an empty file that still exists, and is
// seen by an XFile already open on it.
func TestXFileSeesATruncationAtOnceAndTheStoreHoldsIt(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/trunc": {1, 2, 3}})
	runtime := fileRuleRuntime(t, store, nil)
	early := ruleXFile(t, runtime, "trunc", xFileRead|xFileWrite)
	if size := ruleSize(t, runtime, "trunc"); size != 3 {
		t.Fatalf("READ_WRITE open changed the file to %d bytes", size)
	}
	if written := store.written(); len(written) != 0 {
		t.Fatalf("a non-truncating open stored %v", written)
	}
	ruleNew(t, runtime, skvm.FileOutputStreamClass, "(Ljava/lang/String;)V", ruleText(runtime, "trunc"))
	ruleStoreHolds(t, store, "fs/trunc", []byte{})
	if !ruleExists(t, runtime, "trunc") || ruleSize(t, runtime, "trunc") != 0 {
		t.Fatal("a truncated file is not an existing empty file")
	}
	if available := storeReviewInt(t, wipiStreamCall(t, runtime, early, "available", "()I")); available != 0 {
		t.Fatalf("an XFile open before the truncation still has %d bytes", available)
	}
	if list := store.removalList(); len(list) != 0 {
		t.Fatalf("a truncation wrote a removal list: %v", list)
	}
}

// WRITE_TRUNC stores the empty file at the open, for an existing file and for
// a missing one, before any write or close.
func TestWIPIWriteTruncStoresTheEmptyFileAtTheOpen(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/save": {1, 2, 3}})
	runtime := fileRuleRuntime(t, store, nil)
	wipiStreamFile(t, runtime, "save", wipiFileWriteTrunc)
	ruleStoreHolds(t, store, "fs/save", []byte{})
	wipiStreamFile(t, runtime, "new", wipiFileWriteTrunc)
	ruleStoreHolds(t, store, "fs/new", []byte{})
	if exists, isFile, size := ruleFileSystem(t, runtime, "new"); !exists || !isFile || size != 0 {
		t.Fatalf("WRITE_TRUNC of a missing path: exists=%v isFile=%v sizeOf=%d, want an empty file", exists, isFile, size)
	}
}

// FileOutputStream(String) and (String, false) truncate: the stored file is
// empty at once, an input stream already open on it sees the end, and a stream
// closed with nothing written leaves an empty file rather than a removed one,
// in this session and the next.
func TestXCEFileOutputStreamTruncatesWithoutRemoving(t *testing.T) {
	for _, form := range []struct {
		descriptor string
		extra      []jvm.Value
	}{
		{"(Ljava/lang/String;)V", nil},
		{"(Ljava/lang/String;Z)V", []jvm.Value{jvm.IntValue(0)}},
	} {
		t.Run(form.descriptor, func(t *testing.T) {
			store := newFileRuleStore(t, map[string][]byte{"fs/s": {1, 2, 3, 4}})
			runtime := fileRuleRuntime(t, store, nil)
			input := ruleNew(t, runtime, skvm.FileInputStreamClass, "(Ljava/lang/String;)V", ruleText(runtime, "s"))
			output := ruleNew(t, runtime, skvm.FileOutputStreamClass, form.descriptor, append([]jvm.Value{ruleText(runtime, "s")}, form.extra...)...)
			ruleStoreHolds(t, store, "fs/s", []byte{})
			if value := storeReviewInt(t, wipiStreamCall(t, runtime, input, "read", "()I")); value != -1 {
				t.Fatalf("an open FileInputStream read %d after the truncation, want the end", value)
			}
			wipiStreamCall(t, runtime, output, "close", "()V")
			if !ruleExists(t, runtime, "s") || ruleSize(t, runtime, "s") != 0 {
				t.Fatal("the truncated file does not exist as an empty file")
			}
			if list := store.removalList(); len(list) != 0 {
				t.Fatalf("the truncation went through removal: %v", list)
			}
			if err := runtime.Destroy(true); err != nil {
				t.Fatal(err)
			}
			next := fileRuleRuntime(t, store, nil)
			if !ruleExists(t, next, "s") || ruleSize(t, next, "s") != 0 {
				t.Fatal("the next session does not find the empty file")
			}
		})
	}
}

// --- Rule 4: removal ---

// XFile.unlink makes the file absent to every later read — exists, filesize,
// a read-only open and a handle already open — and keeps it absent in the
// next session, packaged files included. Writing the path brings it back;
// unlinking a name that never existed stores nothing.
func TestXFileUnlinkRemovesTheFile(t *testing.T) {
	packaged := map[string][]byte{"pkg": {4, 5}}
	store := newFileRuleStore(t, map[string][]byte{"fs/slot": {1, 2, 3}, "fs/open": {6}})
	runtime := fileRuleRuntime(t, store, packaged)
	held := ruleXFile(t, runtime, "open", xFileRead)
	for _, name := range []string{"slot", "/pkg", "open"} {
		if result := ruleUnlink(t, runtime, name); result != 0 {
			t.Fatalf("unlink(%q) = %d, want 0", name, result)
		}
	}
	for _, name := range []string{"slot", "pkg", "open"} {
		if ruleExists(t, runtime, name) || ruleSize(t, runtime, name) != -1 {
			t.Fatalf("%q still exists after unlink", name)
		}
		if _, err := runtime.VM.NewObject(skvm.XFileClass, "(Ljava/lang/String;I)V", ruleText(runtime, name), jvm.IntValue(xFileRead)); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
			t.Fatalf("read-only open of removed %q: %v, want IOException", name, err)
		}
	}
	if got := ruleXRead(t, runtime, held, 4); len(got) != 0 {
		t.Fatalf("a handle open on a removed file read %v", got)
	}
	ruleStoreHolds(t, store, "fs/slot", []byte{})
	if list := store.removalList(); !slices.Equal(list, []string{"open", "pkg", "slot"}) {
		t.Fatalf("removal list = %v", list)
	}

	store.written()
	if result := ruleUnlink(t, runtime, "never"); result != 0 {
		t.Fatalf("unlink of a missing name = %d, want 0", result)
	}
	if written := store.written(); len(written) != 0 || ruleExists(t, runtime, "never") {
		t.Fatalf("unlinking a missing name stored %v", written)
	}
	// A file that never reached the store leaves nothing to hide.
	ruleXWrite(t, runtime, ruleXFile(t, runtime, "scratch", xFileWrite), 1)
	if result := ruleUnlink(t, runtime, "scratch"); result != 0 || ruleExists(t, runtime, "scratch") {
		t.Fatalf("unlink of an unstored file = %d, or it still exists", result)
	}
	if written := store.written(); len(written) != 0 {
		t.Fatalf("unlinking a file that was never stored stored %v", written)
	}

	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	next := fileRuleRuntime(t, store, packaged)
	for _, name := range []string{"slot", "pkg", "open"} {
		if ruleExists(t, next, name) {
			t.Fatalf("%q came back in the next session", name)
		}
	}
	file := ruleXFile(t, next, "slot", xFileWrite)
	if !ruleExists(t, next, "slot") || ruleSize(t, next, "slot") != 0 {
		t.Fatal("opening a removed path for writing did not bring it back as an empty file")
	}
	ruleXWrite(t, next, file, 7)
	if !ruleExists(t, next, "slot") || ruleSize(t, next, "slot") != 1 {
		t.Fatal("writing a removed path did not bring it back")
	}
	wipiStreamCall(t, next, file, "close", "()V")
	ruleStoreHolds(t, store, "fs/slot", []byte{7})
	if list := store.removalList(); !slices.Equal(list, []string{"open", "pkg"}) {
		t.Fatalf("removal list after writing slot again = %v", list)
	}
}

// FileSystem.remove does the same for the WIPI surface: exists, isFile and
// sizeOf answer as for a missing file, READ_ONLY fails, the next session
// agrees, and removing a name that never existed stores nothing.
func TestWIPIFileSystemRemoveRemovesTheFile(t *testing.T) {
	packaged := map[string][]byte{"pkg": {4}}
	store := newFileRuleStore(t, map[string][]byte{"fs/save": {1, 2}})
	runtime := fileRuleRuntime(t, store, packaged)
	for _, name := range []string{"save", "pkg"} {
		ruleRemove(t, runtime, name)
		if exists, isFile, size := ruleFileSystem(t, runtime, name); exists || isFile || size != -1 {
			t.Fatalf("after remove(%q): exists=%v isFile=%v sizeOf=%d", name, exists, isFile, size)
		}
		if _, err := runtime.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;I)V", ruleText(runtime, name), jvm.IntValue(wipiFileReadOnly)); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
			t.Fatalf("READ_ONLY of removed %q: %v, want IOException", name, err)
		}
	}
	store.written()
	if _, err := runtime.VM.InvokeStatic(wipi.FileSystemClass, "remove", "(Ljava/lang/String;)V", ruleText(runtime, "never")); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
		t.Fatalf("FileSystem.remove of a missing name: %v, want IOException", err)
	}
	if written := store.written(); len(written) != 0 {
		t.Fatalf("removing a missing name stored %v", written)
	}
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	next := fileRuleRuntime(t, store, packaged)
	for _, name := range []string{"save", "pkg"} {
		if exists, _, _ := ruleFileSystem(t, next, name); exists {
			t.Fatalf("%q came back in the next session", name)
		}
	}
	file := wipiStreamFile(t, next, "pkg", wipiFileWrite)
	wipiStreamCall(t, next, file, "write", "([B)I", ruleBytes(2))
	wipiStreamCall(t, next, file, "close", "()V")
	if exists, _, size := ruleFileSystem(t, next, "pkg"); !exists || size != 1 {
		t.Fatal("writing a removed packaged path did not bring it back")
	}
}

// FileSystem.remove of a name with no file throws the IOException the
// specification gives it, as does a name the removal list cannot carry, while
// XFile.unlink keeps answering 0 and -1 for the two. Neither stores anything.
func TestWIPIFileSystemRemoveOfAMissingNameThrows(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/save": {1}})
	runtime := fileRuleRuntime(t, store, nil)
	remove := func(name string) error {
		_, err := runtime.VM.InvokeStatic(wipi.FileSystemClass, "remove", "(Ljava/lang/String;)V", ruleText(runtime, name))
		return err
	}
	if err := remove("save"); err != nil {
		t.Fatalf("FileSystem.remove of a present file: %v", err)
	}
	store.written()
	for _, name := range []string{"save", "never", "line\nbreak"} {
		if err := remove(name); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
			t.Fatalf("FileSystem.remove(%q): %v, want IOException", name, err)
		}
	}
	if answer := ruleUnlink(t, runtime, "never"); answer != 0 {
		t.Fatalf("XFile.unlink of a missing name = %d, want 0", answer)
	}
	if answer := ruleUnlink(t, runtime, "line\nbreak"); answer != -1 {
		t.Fatalf("XFile.unlink of an unremovable name = %d, want -1", answer)
	}
	if written := store.written(); len(written) != 0 {
		t.Fatalf("removing missing names stored %v", written)
	}
}

// A removal the store refuses stands for the session and is stored at its
// end, as a refused write is, and FileSystem.remove reports the refusal with
// an IOException; XFile.unlink, whose answer every local title discards,
// answers 0 as for any removal.
func TestWIPIFileSystemRemoveReportsARefusedStore(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/save": {1}, "fs/other": {2}})
	runtime := fileRuleRuntime(t, store, nil)
	store.setRefuse(true)
	if _, err := runtime.VM.InvokeStatic(wipi.FileSystemClass, "remove", "(Ljava/lang/String;)V", ruleText(runtime, "save")); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
		t.Fatalf("FileSystem.remove the store refused: %v, want IOException", err)
	}
	if answer := ruleUnlink(t, runtime, "other"); answer != 0 {
		t.Fatalf("XFile.unlink the store refused = %d, want 0", answer)
	}
	for _, name := range []string{"save", "other"} {
		if exists, _, _ := ruleFileSystem(t, runtime, name); exists {
			t.Fatalf("%q is still there after its refused removal", name)
		}
	}
	store.setRefuse(false)
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	next := fileRuleRuntime(t, store, nil)
	for _, name := range []string{"save", "other"} {
		if exists, _, _ := ruleFileSystem(t, next, name); exists {
			t.Fatalf("%q came back in the next session after its refused removal", name)
		}
	}
}

// The streams see a removed file as missing, and a FileOutputStream brings it
// back.
func TestXCEStreamsSeeARemovedFileAsMissing(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/s": {1, 2}})
	runtime := fileRuleRuntime(t, store, nil)
	ruleUnlink(t, runtime, "s")
	if _, err := runtime.VM.NewObject(skvm.FileInputStreamClass, "(Ljava/lang/String;)V", ruleText(runtime, "s")); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
		t.Fatalf("FileInputStream of a removed file: %v, want IOException", err)
	}
	output := ruleNew(t, runtime, skvm.FileOutputStreamClass, "(Ljava/lang/String;)V", ruleText(runtime, "s"))
	if !ruleExists(t, runtime, "s") {
		t.Fatal("a truncating open of a removed file did not make it an empty file again")
	}
	wipiStreamCall(t, runtime, output, "write", "(I)V", jvm.IntValue(3))
	wipiStreamCall(t, runtime, output, "close", "()V")
	ruleStoreHolds(t, store, "fs/s", []byte{3})
	if list := store.removalList(); len(list) != 0 {
		t.Fatalf("removal list after the file came back = %v", list)
	}
}

// A name the removal list cannot carry is not removed, rather than hiding the
// names its lines spell; and an empty file an earlier build's unlink left is a
// present empty file.
func TestXFileRemovalKeepsTheListWhole(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/a": {1}, "fs/b": {2}, "fs/a\nb": {3}, "fs/old": {}})
	runtime := fileRuleRuntime(t, store, nil)
	if result := ruleUnlink(t, runtime, "a\nb"); result != -1 {
		t.Fatalf("unlink of a name with a line break = %d, want -1", result)
	}
	if !ruleExists(t, runtime, "a") || !ruleExists(t, runtime, "b") {
		t.Fatal("removing a name with a line break hid other files")
	}
	if !ruleExists(t, runtime, "old") || ruleSize(t, runtime, "old") != 0 {
		t.Fatal("an earlier build's empty file is not a present empty file")
	}
	if got := ruleXRead(t, runtime, ruleXFile(t, runtime, "old", xFileRead), 4); len(got) != 0 {
		t.Fatalf("an earlier build's empty file read %v", got)
	}
}

// --- Creation: a writable open of a missing name makes the file ---

// An XFile opened with the write bit on a missing name creates it: an empty
// file that exists, measures 0 and a read-only open finds, at once. The close
// stores it with nothing written, and the end of the session stores one that
// was never closed. A read-only open of a missing name still fails and makes
// nothing.
func TestXFileWritableOpenCreatesTheFile(t *testing.T) {
	store := newFileRuleStore(t, nil)
	runtime := fileRuleRuntime(t, store, nil)
	if _, err := runtime.VM.NewObject(skvm.XFileClass, "(Ljava/lang/String;I)V", ruleText(runtime, "absent"), jvm.IntValue(xFileRead)); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
		t.Fatalf("read-only open of a missing name: %v, want IOException", err)
	}
	if ruleExists(t, runtime, "absent") || ruleSize(t, runtime, "absent") != -1 {
		t.Fatal("a refused read-only open made the file")
	}
	for _, open := range []struct {
		name string
		mode int32
	}{{"write", xFileWrite}, {"readwrite", xFileRead | xFileWrite}} {
		file := ruleXFile(t, runtime, open.name, open.mode)
		if !ruleExists(t, runtime, open.name) || ruleSize(t, runtime, open.name) != 0 {
			t.Fatalf("XFile(%q, %d) did not make an empty file", open.name, open.mode)
		}
		if got := ruleXRead(t, runtime, ruleXFile(t, runtime, open.name, xFileRead), 4); len(got) != 0 {
			t.Fatalf("a read-only open of the made file read %v", got)
		}
		wipiStreamCall(t, runtime, file, "close", "()V")
		ruleStoreHolds(t, store, "fs/"+open.name, []byte{})
	}
	ruleXFile(t, runtime, "unclosed", xFileWrite)
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	ruleStoreHolds(t, store, "fs/unclosed", []byte{})
	next := fileRuleRuntime(t, store, nil)
	for _, name := range []string{"write", "readwrite", "unclosed"} {
		if !ruleExists(t, next, name) || ruleSize(t, next, name) != 0 {
			t.Fatalf("the next session does not find the empty file %q", name)
		}
	}
	if ruleExists(t, next, "absent") {
		t.Fatal("the refused read-only open made a file for the next session")
	}
}

// WIPI File's WRITE and READ_WRITE on a missing name make an empty file the
// same way, which FileSystem and a READ_ONLY open see at once and the close or
// the end of the session stores; READ_ONLY of a missing name still fails.
func TestWIPIFileWritableOpenCreatesTheFile(t *testing.T) {
	store := newFileRuleStore(t, nil)
	runtime := fileRuleRuntime(t, store, nil)
	if _, err := runtime.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;I)V", ruleText(runtime, "absent"), jvm.IntValue(wipiFileReadOnly)); !runtime.VM.IsGuestException(err, jvm.IOExceptionClass) {
		t.Fatalf("READ_ONLY of a missing name: %v, want IOException", err)
	}
	for _, open := range []struct {
		name string
		mode int32
	}{{"write", wipiFileWrite}, {"readwrite", wipiFileReadWrite}} {
		file := wipiStreamFile(t, runtime, open.name, open.mode)
		if exists, isFile, size := ruleFileSystem(t, runtime, open.name); !exists || !isFile || size != 0 {
			t.Fatalf("File(%q, %d): exists=%v isFile=%v sizeOf=%d, want an empty file", open.name, open.mode, exists, isFile, size)
		}
		reader := wipiStreamFile(t, runtime, open.name, wipiFileReadOnly)
		if count := storeReviewInt(t, wipiStreamCall(t, runtime, reader, "read", "([B)I", ruleBytes(0, 0))); count != -1 {
			t.Fatalf("READ_ONLY of the made file read %d bytes, want the end", count)
		}
		wipiStreamCall(t, runtime, file, "close", "()V")
		ruleStoreHolds(t, store, "fs/"+open.name, []byte{})
	}
	wipiStreamFile(t, runtime, "unclosed", wipiFileReadWrite)
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	ruleStoreHolds(t, store, "fs/unclosed", []byte{})
	if _, ok := store.LoadSave("fs/absent"); ok {
		t.Fatal("the refused READ_ONLY open stored a file")
	}
}

// FileOutputStream(String, true) on a missing name makes the file it appends
// to: XFile and a FileInputStream find it at once, and the close — or the end
// of the session — stores it.
func TestXCEAppendingStreamCreatesAMissingFile(t *testing.T) {
	store := newFileRuleStore(t, nil)
	runtime := fileRuleRuntime(t, store, nil)
	output := ruleNew(t, runtime, skvm.FileOutputStreamClass, "(Ljava/lang/String;Z)V", ruleText(runtime, "log"), jvm.IntValue(1))
	if !ruleExists(t, runtime, "log") || ruleSize(t, runtime, "log") != 0 {
		t.Fatal("an appending FileOutputStream did not make the missing file")
	}
	input := ruleNew(t, runtime, skvm.FileInputStreamClass, "(Ljava/lang/String;)V", ruleText(runtime, "log"))
	if value := storeReviewInt(t, wipiStreamCall(t, runtime, input, "read", "()I")); value != -1 {
		t.Fatalf("FileInputStream of the made file read %d, want the end", value)
	}
	wipiStreamCall(t, runtime, output, "close", "()V")
	ruleStoreHolds(t, store, "fs/log", []byte{})
	ruleNew(t, runtime, skvm.FileOutputStreamClass, "(Ljava/lang/String;Z)V", ruleText(runtime, "tail"), jvm.IntValue(1))
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	ruleStoreHolds(t, store, "fs/tail", []byte{})
}

// A removed name opened for writing comes back as an empty file at once, and
// the store that writes it takes the name off the removal list in the same
// batch — through XFile and through WIPI File alike.
func TestRemovedFileOpenedForWritingComesBack(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/slot": {1, 2, 3}, "fs/other": {4}})
	runtime := fileRuleRuntime(t, store, nil)
	ruleUnlink(t, runtime, "slot")
	ruleRemove(t, runtime, "other")
	store.takeBatches()
	file := ruleXFile(t, runtime, "slot", xFileWrite)
	if !ruleExists(t, runtime, "slot") || ruleSize(t, runtime, "slot") != 0 {
		t.Fatal("a removed name opened for writing did not come back as an empty file")
	}
	wipiStreamCall(t, runtime, file, "close", "()V")
	ruleStoreHolds(t, store, "fs/slot", []byte{})
	if list := store.removalList(); !slices.Equal(list, []string{"other"}) {
		t.Fatalf("removal list after slot came back = %v", list)
	}
	if batches := store.takeBatches(); len(batches) != 1 || !slices.Equal(batches[0], []string{"fs/.removed", "fs/slot"}) {
		t.Fatalf("slot came back in the batches %v, want the file and the list together", batches)
	}
	wipiStreamFile(t, runtime, "other", wipiFileReadWrite)
	if exists, _, size := ruleFileSystem(t, runtime, "other"); !exists || size != 0 {
		t.Fatal("a removed name opened READ_WRITE did not come back as an empty file")
	}
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	if list := store.removalList(); len(list) != 0 {
		t.Fatalf("removal list after both came back = %v", list)
	}
	next := fileRuleRuntime(t, store, nil)
	for _, name := range []string{"slot", "other"} {
		if !ruleExists(t, next, name) || ruleSize(t, next, name) != 0 {
			t.Fatalf("%q did not stay back in the next session", name)
		}
	}
}

// --- Rule 5: nothing else writes saves ---

// Opening without truncating, reading, writing, seeking, asking about a file,
// pausing, resuming and running pending work store nothing; the title's flush
// or close does, and the end of a session with nothing pending stores nothing
// more.
func TestFilesAreStoredOnlyWhenTheTitleAsks(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/x": {1, 2, 3}, "fs/w": {4}})
	runtime := fileRuleRuntime(t, store, nil)
	xfile := ruleXFile(t, runtime, "x", xFileRead|xFileWrite)
	ruleXWrite(t, runtime, xfile, 9)
	wipiStreamCall(t, runtime, xfile, "seek", "(II)I", jvm.IntValue(0), jvm.IntValue(0))
	ruleXRead(t, runtime, xfile, 4)
	ruleExists(t, runtime, "x")
	ruleSize(t, runtime, "x")
	file := wipiStreamFile(t, runtime, "w", wipiFileWrite)
	wipiStreamCall(t, runtime, file, "write", "([B)I", ruleBytes(5))
	ruleFileSystem(t, runtime, "w")
	output := ruleNew(t, runtime, skvm.FileOutputStreamClass, "(Ljava/lang/String;Z)V", ruleText(runtime, "o"), jvm.IntValue(1))
	wipiStreamCall(t, runtime, output, "write", "(I)V", jvm.IntValue(6))
	for _, step := range []func() error{runtime.Pause, runtime.Resume, runtime.RunPending, runtime.RunPending} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if written := store.written(); len(written) != 0 {
		t.Fatalf("saves were written without a flush, close or truncation: %v", written)
	}
	wipiStreamCall(t, runtime, xfile, "close", "()V")
	wipiStreamCall(t, runtime, file, "close", "()V")
	wipiStreamCall(t, runtime, output, "flush", "()V")
	if written := store.written(); !slices.Equal(written, []string{"fs/o", "fs/w", "fs/x"}) {
		t.Fatalf("the title's flush and closes stored %v", written)
	}
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	if written := store.written(); len(written) != 0 {
		t.Fatalf("the end of a session with nothing pending stored %v", written)
	}
}

// A title that probes generated names — "slot0" to "slotN" — asks about paths
// that are not there, and the session keeps no file for any of them; a path
// that is there, or one a title opens for writing, is kept as before.
func TestXFileProbesOfMissingPathsKeepNothing(t *testing.T) {
	store := newFileRuleStore(t, map[string][]byte{"fs/slot7": {1}})
	runtime := fileRuleRuntime(t, store, nil)
	for index := range 100 {
		name := "slot" + string(rune('a'+index%26)) + strings.Repeat("x", index/26)
		if ruleExists(t, runtime, name) || ruleSize(t, runtime, name) != -1 {
			t.Fatalf("missing path %q exists", name)
		}
		if removal := runtime.removeXFile(name); removal != xFileAbsent {
			t.Fatalf("removing missing path %q = %d, want absent", name, removal)
		}
	}
	if !ruleExists(t, runtime, "slot7") {
		t.Fatal("a stored path does not exist")
	}
	runtime.fileMu.Lock()
	kept := len(runtime.files)
	runtime.fileMu.Unlock()
	if kept != 1 {
		t.Fatalf("the session keeps %d files after probing, want only the one that is there", kept)
	}
	if written := store.written(); len(written) != 0 {
		t.Fatalf("probes wrote %v", written)
	}
	ruleXFile(t, runtime, "made", xFileWrite)
	if !ruleExists(t, runtime, "made") {
		t.Fatal("a path opened for writing does not exist")
	}
}

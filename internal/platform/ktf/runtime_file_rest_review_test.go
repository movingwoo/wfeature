package ktf

import (
	"bytes"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// refusableSaveStore counts the store writes a test makes and can be told to
// refuse them.
type refusableSaveStore struct {
	saves  map[string][]byte
	stores int
	refuse error
}

func (store *refusableSaveStore) LoadSave(name string) ([]byte, bool) {
	data, ok := store.saves[name]
	return data, ok
}

func (store *refusableSaveStore) StoreSave(name string, data []byte) error {
	if store.refuse != nil {
		return store.refuse
	}
	store.stores++
	store.saves[name] = append([]byte(nil), data...)
	return nil
}

// guestDataOutputStream opens a DataOutputStream over a File the way a title
// does, binds it to a guest address, and answers a function that calls one of
// its methods the way guest code does: through the method's native stub and
// a supervisor call.
func guestDataOutputStream(t *testing.T, client *Client, runtime *initializationRuntime, name string) (*runtimeGuestFile, func(method, descriptor string, arguments ...uint32) error) {
	t.Helper()
	state := &runtimeGuestFile{name: name}
	file := &jvm.Object{ClassName: "org/kwis/msp/io/File", Native: state, Fields: make(map[string]jvm.Value)}
	value, err := runtimeFileOpenDataOutputStream(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(file)})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := value.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ensureResultBound(stream); err != nil {
		t.Fatal(err)
	}
	address, ok := client.vm.AOTAddress(stream)
	if !ok {
		t.Fatal("the stream has no guest address")
	}
	classAddress, err := runtime.ensureJavaClass("java/io/DataOutputStream")
	if err != nil {
		t.Fatal(err)
	}
	class, ok := client.vm.AOTClassAt(classAddress)
	if !ok {
		t.Fatal("DataOutputStream is not registered")
	}
	return state, func(method, descriptor string, arguments ...uint32) error {
		t.Helper()
		for _, candidate := range class.Methods {
			if candidate.Name != method || candidate.Descriptor != descriptor {
				continue
			}
			typeInfo, err := jvm.ParseMethodDescriptor(descriptor)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = runtime.runAOTMethod(t.Context(), client.Thread, class.Name, candidate, typeInfo, append([]uint32{address}, arguments...))
			return err
		}
		t.Fatalf("DataOutputStream has no %s%s", method, descriptor)
		return nil
	}
}

// TestDataOutputStreamWriteStoresOncePerCall covers the save path a title
// takes through File.openDataOutputStream. Every fixed-width write reaches the
// file a byte at a time, and each byte was a whole-file store — a locked,
// synced, renamed write — so a save of a few hundred ints made a thousand of
// them. The bytes one call writes are now stored together before the call
// returns, which is the point at which every write here has always been
// stored.
func TestDataOutputStreamWriteStoresOncePerCall(t *testing.T) {
	client, runtime := newTestRuntime(t)
	store := &refusableSaveStore{saves: make(map[string][]byte)}
	client.SaveStore = store
	state, call := guestDataOutputStream(t, client, runtime, "save.dat")

	if err := call("writeInt", "(I)V", 0x01020304); err != nil {
		t.Fatal(err)
	}
	if store.stores != 1 {
		t.Fatalf("writeInt made %d store writes, want 1", store.stores)
	}
	if err := call("writeShort", "(I)V", 0x0506); err != nil {
		t.Fatal(err)
	}
	if store.stores != 2 {
		t.Fatalf("writeShort made %d store writes, want 1 more", store.stores-1)
	}
	want := []byte{1, 2, 3, 4, 5, 6}
	if got := store.saves["fs/save.dat"]; !bytes.Equal(got, want) {
		t.Fatalf("stored file = %x, want %x", got, want)
	}
	if !bytes.Equal(state.data, want) || state.position != len(want) || !bytes.Equal(runtime.GuestFiles["save.dat"], want) {
		t.Fatalf("file = %x at %d, table %x", state.data, state.position, runtime.GuestFiles["save.dat"])
	}

	// A single-byte write that is the guest's own call is still stored before
	// it returns.
	if _, err := runtimeFileStreamWrite(runtimeFileWriteByte)(runtime, client.vm, []jvm.Value{
		jvm.ReferenceValue(&jvm.Object{ClassName: runtimeFileOutputStreamClass, Native: state}), jvm.IntValue(7),
	}); err != nil {
		t.Fatal(err)
	}
	if store.stores != 3 || !bytes.Equal(store.saves["fs/save.dat"], append(want, 7)) {
		t.Fatalf("a direct byte write made %d stores, stored %x", store.stores-2, store.saves["fs/save.dat"])
	}
}

// TestDataOutputStreamWriteThatCannotBeStoredThrows is the failure half: a
// call whose bytes the store refuses throws the IOException a refused write
// has always thrown, and the file is left as it was, the way a refused write
// always left it.
func TestDataOutputStreamWriteThatCannotBeStoredThrows(t *testing.T) {
	client, runtime := newTestRuntime(t)
	store := &refusableSaveStore{saves: make(map[string][]byte)}
	client.SaveStore = store
	state, call := guestDataOutputStream(t, client, runtime, "save.dat")
	if err := call("writeShort", "(I)V", 0x0102); err != nil {
		t.Fatal(err)
	}
	store.refuse = errors.New("disk full")
	err := call("writeInt", "(I)V", 0x03040506)
	var uncaught *UncaughtAOTException
	if !errors.As(err, &uncaught) || uncaught.Exception == nil || uncaught.Exception.Object.ClassName != "java/io/IOException" {
		t.Fatalf("refused writeInt error = %v, want an uncaught IOException", err)
	}
	want := []byte{1, 2}
	if !bytes.Equal(state.data, want) || state.position != len(want) || !bytes.Equal(runtime.GuestFiles["save.dat"], want) {
		t.Fatalf("after the refusal the file = %x at %d, table %x; want it as it was", state.data, state.position, runtime.GuestFiles["save.dat"])
	}
	if runtime.fileBatch != nil {
		t.Fatal("the refused call left its batch open")
	}
}

// TestHeldFileWritesAreStoredBeforeTheThreadsNextCall keeps held writes
// inside the one guest call that made them. A title's own stream under a
// DataOutputStream can make another platform call before the outer call
// returns — one that parks the thread among them — and what is held is stored
// as that call enters, so nothing held is ever left at a boundary.
func TestHeldFileWritesAreStoredBeforeTheThreadsNextCall(t *testing.T) {
	client, runtime := newTestRuntime(t)
	store := &refusableSaveStore{saves: make(map[string][]byte)}
	client.SaveStore = store
	batch := &guestFileBatch{owner: client.activeWorker}
	runtime.fileBatch = batch
	defer func() { runtime.fileBatch = nil }()
	state := &runtimeGuestFile{name: "save.dat"}
	file := &jvm.Object{ClassName: "org/kwis/msp/io/File", Native: state, Fields: make(map[string]jvm.Value)}
	if _, err := runtimeFileWriteByte(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(file), jvm.IntValue(9)}); err != nil {
		t.Fatal(err)
	}
	if store.stores != 0 {
		t.Fatalf("a write inside the batch made %d store writes before the call returned", store.stores)
	}
	finish := runtime.enterGuestFileCall(wipicrt.SVCCategoryWIPIC, 0)
	if store.stores != 1 || !bytes.Equal(store.saves["fs/save.dat"], []byte{9}) {
		t.Fatalf("another call from the same thread left the write held: %d stores", store.stores)
	}
	if err := finish(); err != nil || runtime.fileBatch != batch {
		t.Fatalf("the inner call closed the outer call's batch: %v", err)
	}
}

// TestHeldFileWritesAreStoredWhenTheThreadStops covers a guest thread that
// stops inside a DataOutputStream call without making another platform call:
// its own stream's AOT write ran out of its slice. What the call holds is
// stored before another thread runs or a checkpoint is taken, and the call
// goes on holding what it writes after that.
func TestHeldFileWritesAreStoredWhenTheThreadStops(t *testing.T) {
	client, runtime := newTestRuntime(t)
	store := &refusableSaveStore{saves: make(map[string][]byte)}
	client.SaveStore = store
	worker := &guestWorker{}
	client.activeWorker = worker
	defer func() { client.activeWorker = nil }()
	batch := &guestFileBatch{owner: worker}
	runtime.fileBatch = batch
	defer func() { runtime.fileBatch = nil }()
	state := &runtimeGuestFile{name: "save.dat"}
	file := &jvm.Object{ClassName: "org/kwis/msp/io/File", Native: state, Fields: make(map[string]jvm.Value)}
	write := func(value int32) {
		t.Helper()
		if _, err := runtimeFileWriteByte(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(file), jvm.IntValue(value)}); err != nil {
			t.Fatal(err)
		}
	}
	write(9)
	if store.stores != 0 {
		t.Fatalf("a write inside the batch made %d store writes", store.stores)
	}
	runtime.storeHeldGuestFiles()
	if store.stores != 1 || !bytes.Equal(store.saves["fs/save.dat"], []byte{9}) {
		t.Fatalf("the stopped thread's write was not stored: %d stores, %x", store.stores, store.saves["fs/save.dat"])
	}
	if runtime.fileBatch != batch {
		t.Fatal("storing what a stopped call held closed its batch")
	}
	write(8)
	if store.stores != 1 {
		t.Fatal("a write after the thread resumed was not held")
	}
	runtime.storeGuestFileBatch(batch)
	if !bytes.Equal(store.saves["fs/save.dat"], []byte{9, 8}) || batch.failure != nil {
		t.Fatalf("stored %x (%v), want both writes", store.saves["fs/save.dat"], batch.failure)
	}
}

// TestRefusedHeldWritesAreUnreadThroughEveryFile covers a second File on the
// path a refused batch held writes for. Putting the path back counts as a
// write, so a File that had read the refused bytes reads what the store has.
func TestRefusedHeldWritesAreUnreadThroughEveryFile(t *testing.T) {
	client, runtime := newTestRuntime(t)
	store := &refusableSaveStore{saves: map[string][]byte{"fs/save.dat": {1}}}
	client.SaveStore = store
	runtime.setGuestFile("save.dat", []byte{1})
	writer := &runtimeGuestFile{name: "save.dat", data: []byte{1}, seen: runtime.GuestFileWrites["save.dat"]}
	reader := &runtimeGuestFile{name: "save.dat", data: []byte{1}, seen: runtime.GuestFileWrites["save.dat"]}
	object := func(state *runtimeGuestFile) jvm.Value {
		return jvm.ReferenceValue(&jvm.Object{ClassName: "org/kwis/msp/io/File", Native: state, Fields: make(map[string]jvm.Value)})
	}
	batch := &guestFileBatch{owner: client.activeWorker}
	runtime.fileBatch = batch
	defer func() { runtime.fileBatch = nil }()
	if _, err := runtimeFileWriteByte(runtime, client.vm, []jvm.Value{object(writer), jvm.IntValue(9)}); err != nil {
		t.Fatal(err)
	}
	if _, state, err := runtime.fileState([]jvm.Value{object(reader)}); err != nil || !bytes.Equal(state.data, []byte{9}) {
		t.Fatalf("reader before the store = %x (%v), want the held write", state.data, err)
	}
	store.refuse = errors.New("disk full")
	runtime.storeGuestFileBatch(batch)
	if batch.failure == nil {
		t.Fatal("the refused store was not reported")
	}
	for name, file := range map[string]*runtimeGuestFile{"writer": writer, "reader": reader} {
		if _, state, err := runtime.fileState([]jvm.Value{object(file)}); err != nil || !bytes.Equal(state.data, []byte{1}) {
			t.Fatalf("%s after the refusal = %x (%v), want what the store has", name, state.data, err)
		}
	}
}

package ktf

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// openRuleFile opens an org.kwis.msp.io.File the way a title's constructor
// call does.
func openRuleFile(t *testing.T, client *Client, runtime *initializationRuntime, name string, mode int32) *jvm.Object {
	t.Helper()
	file := &jvm.Object{ClassName: "org/kwis/msp/io/File", Fields: make(map[string]jvm.Value)}
	if _, err := runtimeFileConstructor(runtime, client.vm, []jvm.Value{
		jvm.ReferenceValue(file), jvm.ReferenceValue(client.vm.NewString(name)), jvm.IntValue(mode),
	}); err != nil {
		t.Fatalf("open %q mode %d: %v", name, mode, err)
	}
	return file
}

func writeRuleFile(t *testing.T, client *Client, runtime *initializationRuntime, file *jvm.Object, data []byte) {
	t.Helper()
	array := newByteArray(t, client, data)
	if _, err := runtimeFileWrite(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(file), jvm.ReferenceValue(array)}); err != nil {
		t.Fatal(err)
	}
}

func readRuleFile(t *testing.T, client *Client, runtime *initializationRuntime, file *jvm.Object) []byte {
	t.Helper()
	if _, err := runtimeFileSeek(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(file), jvm.IntValue(0)}); err != nil {
		t.Fatal(err)
	}
	size, err := runtimeFileSize(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(file)})
	if err != nil {
		t.Fatal(err)
	}
	length, _ := size.Int32()
	array := newByteArray(t, client, make([]byte, max(length, 1)))
	if _, err := runtimeFileRead(runtime, client.vm, []jvm.Value{jvm.ReferenceValue(file), jvm.ReferenceValue(array)}); err != nil {
		t.Fatal(err)
	}
	data, err := jvm.ByteArraySnapshot(array)
	if err != nil {
		t.Fatal(err)
	}
	return data[:length]
}

// A write through one File is what every other File open on the same path
// reads from then on, as the common save rule has it: an object opened before
// the write used to keep the copy its own open took.
func TestAWriteThroughOneFileIsSeenByAnotherOpenOnThePath(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.saveStore = &refusableSaveStore{saves: map[string][]byte{"fs/save.dat": []byte("old")}}

	reader := openRuleFile(t, client, runtime, "save.dat", 4)
	writer := openRuleFile(t, client, runtime, "/save.dat", 4)
	writeRuleFile(t, client, runtime, writer, []byte("NEWER"))

	if got := readRuleFile(t, client, runtime, reader); !bytes.Equal(got, []byte("NEWER")) {
		t.Fatalf("a File opened before the write read %q, want %q", got, "NEWER")
	}
}

// A truncating open (mode 3) empties the file in the store at the open, so a
// title that opens to rewrite and closes without writing has an empty file,
// and every other open sees the empty file at once.
func TestATruncatingOpenEmptiesTheFileAtTheOpen(t *testing.T) {
	client, runtime := newTestRuntime(t)
	store := &refusableSaveStore{saves: map[string][]byte{"fs/save.dat": []byte("old save")}}
	client.saveStore = store

	openRuleFile(t, client, runtime, "save.dat", 3)

	if data, ok := store.saves["fs/save.dat"]; !ok || len(data) != 0 {
		t.Fatalf("after a truncating open the store holds %q (found %v), want an empty file", data, ok)
	}
	other := openRuleFile(t, client, runtime, "save.dat", 1)
	if got := readRuleFile(t, client, runtime, other); len(got) != 0 {
		t.Fatalf("another open read %q after the truncation, want nothing", got)
	}
}

// A truncating open the store refuses fails the open rather than leaving a
// title to believe it is rewriting a file that still holds its old bytes.
func TestATruncatingOpenTheStoreRefusesFails(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.saveStore = &refusableSaveStore{saves: map[string][]byte{"fs/save.dat": []byte("old")}, refuse: errRuleStoreRefused}

	file := &jvm.Object{ClassName: "org/kwis/msp/io/File", Fields: make(map[string]jvm.Value)}
	_, err := runtimeFileConstructor(runtime, client.vm, []jvm.Value{
		jvm.ReferenceValue(file), jvm.ReferenceValue(client.vm.NewString("save.dat")), jvm.IntValue(3),
	})
	if _, ok := err.(*jvm.GuestException); !ok {
		t.Fatalf("a refused truncating open answered %v, want a guest IOException", err)
	}
}

var errRuleStoreRefused = ruleStoreError("store refused")

type ruleStoreError string

func (err ruleStoreError) Error() string { return string(err) }

// The earlier native package empties a file at a truncating open in the store
// as well, not at the next frame or close: a title that opens to rewrite and
// is then closed by the Host before a frame ends has an empty file, as the
// common save rule has it.
func TestNativeTruncatingOpenStoresTheEmptyFileAtTheOpen(t *testing.T) {
	store := &countingSaveStore{saves: map[string][]byte{}}
	platform := newTestNativePlatform(t, map[string][]byte{"settings.dat": []byte("a much longer previous save")})
	platform.AttachSaves(store)
	name := nativeString(t, platform, "settings.dat")

	if object := nativeCall(t, platform.openFile, 0, name, nativeModeWriteTruncate); object == 0 {
		t.Fatal("the truncating open was refused")
	}
	if data, ok := store.saves[nativeSaveKey("settings.dat")]; !ok || len(data) != 0 {
		t.Fatalf("after a truncating open the store holds %q (found %v), want an empty file", data, ok)
	}
}

// Opening a missing file for writing creates it: an empty file every later
// read sees, already in the store, the way the C file table, the earlier
// native package and the other platforms create one. Opening it only for
// reading still fails.
func TestAWritingOpenCreatesAMissingFile(t *testing.T) {
	for _, mode := range []int32{2, 4} {
		client, runtime := newTestRuntime(t)
		store := &refusableSaveStore{saves: map[string][]byte{}}
		client.saveStore = store

		openRuleFile(t, client, runtime, "new.dat", mode)

		if data, ok := store.saves["fs/new.dat"]; !ok || len(data) != 0 {
			t.Fatalf("mode %d: after opening a missing file for writing the store holds %q (found %v), want an empty file", mode, data, ok)
		}
		if _, exists := runtime.guestFile("new.dat"); !exists {
			t.Fatalf("mode %d: the created file is not there for the next open", mode)
		}
	}
	client, runtime := newTestRuntime(t)
	client.saveStore = &refusableSaveStore{saves: map[string][]byte{}}
	file := &jvm.Object{ClassName: "org/kwis/msp/io/File", Fields: make(map[string]jvm.Value)}
	if _, err := runtimeFileConstructor(runtime, client.vm, []jvm.Value{
		jvm.ReferenceValue(file), jvm.ReferenceValue(client.vm.NewString("missing.dat")), jvm.IntValue(1),
	}); err == nil {
		t.Fatal("a read-only open of a missing file succeeded")
	}
}

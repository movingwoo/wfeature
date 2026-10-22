package lgt

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The Java half of the common storage contract for files and directories,
// docs/wipi-storage.md: org.kwis.msp.io.FileSystem and File, row by row, the
// way a title's calls reach them, by the key the module names them with.

const (
	fsExists      = javaFileSystemClass + ".exists(Ljava/lang/String;)Z"
	fsExistsIn    = javaFileSystemClass + ".exists(Ljava/lang/String;I)Z"
	fsIsFile      = javaFileSystemClass + ".isFile(Ljava/lang/String;)Z"
	fsIsDirectory = javaFileSystemClass + ".isDirectory(Ljava/lang/String;)Z"
	fsList        = javaFileSystemClass + ".list(Ljava/lang/String;)Ljava/util/Vector;"
	fsMkdir       = javaFileSystemClass + ".mkdir(Ljava/lang/String;)V"
	fsRmdir       = javaFileSystemClass + ".rmdir(Ljava/lang/String;)V"
	fsRemove      = javaFileSystemClass + ".remove(Ljava/lang/String;)V"
	fsRemoveIn    = javaFileSystemClass + ".remove(Ljava/lang/String;I)V"
	fsRename      = javaFileSystemClass + ".rename(Ljava/lang/String;Ljava/lang/String;)V"
	fsCreation    = javaFileSystemClass + ".getCreationTime(Ljava/lang/String;)I"
	fsAvailable   = javaFileSystemClass + ".available()I"
	fsTotal       = javaFileSystemClass + ".totalSpace()I"
	fsMaxName     = javaFileSystemClass + ".getMaxFilenameLength()I"
	fileOpenKey   = javaFileClass + ".<init>(Ljava/lang/String;I)V"
	fileOpenIn    = javaFileClass + ".<init>(Ljava/lang/String;II)V"
)

// javaCall runs one platform member by its key with the words given.
func javaCall(t *testing.T, client *Client, key string, arguments ...uint32) (uint32, error) {
	t.Helper()
	method, ok := javaPlatformMethods[key]
	if !ok {
		t.Fatalf("%s is not served", key)
	}
	if len(arguments) != method.Words {
		t.Fatalf("%s takes %d words and the test passed %d", key, method.Words, len(arguments))
	}
	return method.Implementat(client, context.Background(), client.thread, arguments)
}

func mustJavaCall(t *testing.T, client *Client, key string, arguments ...uint32) uint32 {
	t.Helper()
	answer, err := javaCall(t, client, key, arguments...)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return answer
}

// javaVectorNames reads a Vector of Strings a member answered.
func javaVectorNames(t *testing.T, client *Client, vector uint32) []string {
	t.Helper()
	if class, ok := client.javaClassOfObject(vector); !ok || class.Name != javaVectorClass {
		t.Fatal("the answer is not a Vector")
	}
	names := []string{}
	for _, element := range client.javaRuntimeState().vectors[vector] {
		text, ok := client.javaText(element)
		if !ok {
			t.Fatal("the Vector holds something that is not a String")
		}
		names = append(names, text)
	}
	return names
}

// A null name is NullPointerException to every Java call that takes a name.
func TestStorageContractJavaNullNames(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	name := newTestString(t, client, "save.dat")
	for key, arguments := range map[string][]uint32{
		fsExists: {0}, fsExistsIn: {0, 1}, fsIsFile: {0}, fsIsDirectory: {0}, fsList: {0},
		fsMkdir: {0}, fsRmdir: {0}, fsRemove: {0}, fsRemoveIn: {0, 1}, fsCreation: {0},
		fsRename: {0, name},
		javaFileSystemClass + ".rename(Ljava/lang/String;Ljava/lang/String;I)V": {name, 0, 1},
		javaFileSystemClass + ".isFile(Ljava/lang/String;I)Z":                   {0, 1},
		javaFileSystemClass + ".isDirectory(Ljava/lang/String;I)Z":              {0, 1},
		javaFileSystemClass + ".list(Ljava/lang/String;I)Ljava/util/Vector;":    {0, 1},
		javaFileSystemClass + ".mkdir(Ljava/lang/String;I)V":                    {0, 1},
		javaFileSystemClass + ".rmdir(Ljava/lang/String;I)V":                    {0, 1},
		javaFileSystemClass + ".getCreationTime(Ljava/lang/String;I)I":          {0, 1},
		fileOpenKey: {0x1000, 0, 4}, fileOpenIn: {0x1000, 0, 4, 1},
	} {
		if _, err := javaCall(t, client, key, arguments...); thrownClass(err) != javaThrowNullClass {
			t.Errorf("%s with a null name answered %v", key, err)
		}
	}
}

// The longest name is 255 bytes, which getMaxFilenameLength answers, counted
// on the path the name spells and in a String's UTF-8 bytes, and a longer one
// — or one no file can have — is an IOException to every call that takes a
// name.
func TestStorageContractJavaNameBounds(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	if got := mustJavaCall(t, client, fsMaxName); got != 255 {
		t.Fatalf("getMaxFilenameLength = %d, want 255", got)
	}
	// 255 bytes of path however it is spelled; 85 syllables are 255 bytes of
	// UTF-8.
	for _, spelling := range []string{
		strings.Repeat("n", 255), "/" + strings.Repeat("n", 255), "\\\\" + strings.Repeat("n", 255),
		strings.Repeat("./", 200) + strings.Repeat("n", 255), strings.Repeat("가", 85),
	} {
		if exists, err := javaCall(t, client, fsExists, newTestString(t, client, spelling)); err != nil || exists != 0 {
			t.Fatalf("exists of a %d-byte spelling answered %d, %v", len(spelling), exists, err)
		}
	}
	other := newTestString(t, client, "data/hello.txt")
	for _, refused := range []string{
		strings.Repeat("n", 256), "//" + strings.Repeat("n", 256), strings.Repeat("가", 86),
		"../outside.dat", ".created", "/.directories/x",
	} {
		name := newTestString(t, client, refused)
		for key, arguments := range map[string][]uint32{
			fsExists: {name}, fsIsFile: {name}, fsIsDirectory: {name}, fsList: {name}, fsMkdir: {name},
			fsRmdir: {name}, fsRemove: {name}, fsRemoveIn: {name, 1}, fsCreation: {name},
			fsRename: {name, other}, fileOpenKey: {0x1000, name, 4},
		} {
			if _, err := javaCall(t, client, key, arguments...); thrownClass(err) != javaIOExceptionClass {
				t.Errorf("%s of %.12q answered %v, want IOException", key, refused, err)
			}
		}
		if _, err := javaCall(t, client, fsRename, other, name); thrownClass(err) != javaIOExceptionClass {
			t.Errorf("rename onto %.12q answered %v", refused, err)
		}
	}
}

// What a directory is, through FileSystem: mkdir returns, and a name that is
// a file or a directory is an IOException; rmdir returns, and something below
// it, a name that is not a directory and the root are each an IOException;
// exists is a file or a directory; isFile and isDirectory tell them apart;
// list names what is directly below, and a name that is not a directory is an
// IOException.
func TestStorageContractJavaDirectories(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	text := func(value string) uint32 { return newTestString(t, client, value) }
	client.writeFile("save.dat", []byte("x"))
	client.writeFile("below/slot.dat", []byte("x"))
	mustJavaCall(t, client, fsMkdir, text("made"))
	mustJavaCall(t, client, fsMkdir, text("made/inner"))
	for _, name := range []string{"made", "save.dat", "below", "data", "", "/"} {
		if _, err := javaCall(t, client, fsMkdir, text(name)); thrownClass(err) != javaIOExceptionClass {
			t.Errorf("mkdir(%q) answered %v, want IOException", name, err)
		}
	}
	for _, check := range []struct {
		name                    string
		exists, file, directory bool
	}{
		{"save.dat", true, true, false},
		{"data/hello.txt", true, true, false},
		{"made", true, false, true},
		{"made/inner", true, false, true},
		{"below", true, false, true},
		{"data", true, false, true},
		{"", true, false, true},
		{"absent", false, false, false},
	} {
		for key, want := range map[string]bool{fsExists: check.exists, fsIsFile: check.file, fsIsDirectory: check.directory} {
			if got := mustJavaCall(t, client, key, text(check.name)) == 1; got != want {
				t.Errorf("%s(%q) = %v, want %v", key, check.name, got, want)
			}
		}
	}
	for name, want := range map[string][]string{
		"made":       {"inner"},
		"below":      {"slot.dat"},
		"data":       {"hello.txt", "other.txt"},
		"":           {"below", "data", "made", "save.dat"},
		"/":          {"below", "data", "made", "save.dat"},
		"made/inner": {},
	} {
		if got := javaVectorNames(t, client, mustJavaCall(t, client, fsList, text(name))); !reflect.DeepEqual(got, want) {
			t.Errorf("list(%q) = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"save.dat", "absent"} {
		if _, err := javaCall(t, client, fsList, text(name)); thrownClass(err) != javaIOExceptionClass {
			t.Errorf("list(%q) answered %v, want IOException", name, err)
		}
	}
	for _, name := range []string{"made", "below", "absent", "save.dat", "", "."} {
		if _, err := javaCall(t, client, fsRmdir, text(name)); thrownClass(err) != javaIOExceptionClass {
			t.Errorf("rmdir(%q) answered %v, want IOException", name, err)
		}
	}
	mustJavaCall(t, client, fsRmdir, text("made/inner"))
	mustJavaCall(t, client, fsRmdir, text("made"))
	if mustJavaCall(t, client, fsExists, text("made")) != 0 {
		t.Fatal("a removed directory still exists")
	}
}

// Remove a file: remove(name) throws IOException for a name that is not a
// file, and remove(name, flag) returns either way, as the specification
// words the flagged form.
func TestStorageContractJavaRemove(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	text := func(value string) uint32 { return newTestString(t, client, value) }
	client.writeFile("save.dat", []byte("x"))
	client.writeFile("flagged.dat", []byte("x"))
	mustJavaCall(t, client, fsMkdir, text("made"))
	mustJavaCall(t, client, fsRemove, text("save.dat"))
	if client.fileExists("save.dat") {
		t.Fatal("remove left the file")
	}
	for _, name := range []string{"save.dat", "absent", "made", "data", ""} {
		if _, err := javaCall(t, client, fsRemove, text(name)); thrownClass(err) != javaIOExceptionClass {
			t.Errorf("remove(%q) answered %v, want IOException", name, err)
		}
		if _, err := javaCall(t, client, fsRemoveIn, text(name), 1); err != nil {
			t.Errorf("remove(%q, flag) answered %v, want nothing", name, err)
		}
	}
	mustJavaCall(t, client, fsRemoveIn, text("flagged.dat"), 1)
	if client.fileExists("flagged.dat") {
		t.Fatal("remove(name, flag) left the file")
	}
	if !client.directoryExists("made") {
		t.Fatal("a remove took a directory")
	}
}

// A removal the store refuses is IOException to both forms of remove, and
// the file is still there.
func TestStorageContractJavaRemoveRefused(t *testing.T) {
	client, store, _ := reviewFiles(t, map[string]string{"fs/save.dat": "kept"})
	store.refuse = true
	for key, flag := range map[string][]uint32{fsRemove: nil, fsRemoveIn: {1}} {
		arguments := append([]uint32{newTestString(t, client, "save.dat")}, flag...)
		if _, err := javaCall(t, client, key, arguments...); thrownClass(err) != javaIOExceptionClass {
			t.Errorf("%s refused by the store answered %v, want IOException", key, err)
		}
		if data, ok := client.readFile("save.dat"); !ok || string(data) != "kept" {
			t.Fatalf("after %s the file reads %q (%v)", key, data, ok)
		}
	}
	// remove(name, flag) of a name that is not a file returns, store or no.
	if _, err := javaCall(t, client, fsRemoveIn, newTestString(t, client, "absent"), 1); err != nil {
		t.Fatalf("remove(absent, flag) answered %v", err)
	}
}

// Creation time: 0 for a file, -1 for a directory, IOException for neither.
func TestStorageContractJavaCreationTime(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	text := func(value string) uint32 { return newTestString(t, client, value) }
	client.writeFile("save.dat", []byte("x"))
	mustJavaCall(t, client, fsMkdir, text("made"))
	for name, want := range map[string]int32{"save.dat": 0, "data/hello.txt": 0, "made": -1, "data": -1, "": -1} {
		if got := int32(mustJavaCall(t, client, fsCreation, text(name))); got != want {
			t.Errorf("getCreationTime(%q) = %d, want %d", name, got, want)
		}
	}
	if _, err := javaCall(t, client, fsCreation, text("absent")); thrownClass(err) != javaIOExceptionClass {
		t.Errorf("getCreationTime of a missing name answered %v", err)
	}
}

// Space through Java: totalSpace is the 1 MiB MC_fsTotalSpace answers, and
// available the figure MC_fsAvailable answers, which a File's writes move.
func TestStorageContractJavaSpace(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	if total := mustJavaCall(t, client, fsTotal); total != 1<<20 || total != callSlot(t, client, slotFsTotalSpace) {
		t.Fatalf("totalSpace = %d", total)
	}
	object := openJavaFile(t, client, "save.dat", 3)
	if _, err := javaFileWrite(client, context.Background(), client.thread,
		[]uint32{object, mustByteArray(t, client, strings.Repeat("s", 4000))}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaFileClose(client, context.Background(), client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	available := mustJavaCall(t, client, fsAvailable)
	if available != 1<<20-4000 || available != callSlot(t, client, slotFsAvailable) {
		t.Fatalf("available = %d, MC_fsAvailable = %d, want %d", available, callSlot(t, client, slotFsAvailable), 1<<20-4000)
	}
}

// One file holds at most 4 MiB: a write past it through a File, one byte or
// an array, or through a stream on one, is an IOException, and so is a write
// into a File opened to read. The file is left as it was.
func TestStorageContractJavaFileBound(t *testing.T) {
	ctx := context.Background()
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	object := openJavaFile(t, client, "big.dat", 4)
	mustJavaCall(t, client, javaFileClass+".seek(I)V", object, maxFileBytes-1)
	if _, err := javaFileWrite(client, ctx, client.thread, []uint32{object, mustByteArray(t, client, "ab")}); thrownClass(err) != javaIOExceptionClass {
		t.Errorf("write([B) past the bound answered %v", err)
	}
	if _, err := javaFileWriteByte(client, ctx, client.thread, []uint32{object, 'a'}); err != nil {
		t.Fatalf("write(int) up to the bound answered %v", err)
	}
	if _, err := javaFileWriteByte(client, ctx, client.thread, []uint32{object, 'b'}); thrownClass(err) != javaIOExceptionClass {
		t.Errorf("write(int) past the bound answered %v", err)
	}
	sink := openJavaOutputStream(t, client, object)
	if _, err := javaByteSinkWrite(client, ctx, client.thread, []uint32{sink, 'c'}); thrownClass(err) != javaIOExceptionClass {
		t.Errorf("a stream write past the bound answered %v", err)
	}
	if held := client.javaRun.sinks[sink]; len(held) != 0 {
		t.Errorf("the refused stream write left %d bytes in the stream", len(held))
	}
	if _, open, err := client.javaFileHandle(object); err != nil || len(open.data) != maxFileBytes {
		t.Fatalf("the refused writes left the file at %d bytes (%v)", len(open.data), err)
	}
	reading := openJavaFile(t, client, "data/hello.txt", 1)
	if _, err := javaFileWrite(client, ctx, client.thread, []uint32{reading, mustByteArray(t, client, "x")}); thrownClass(err) != javaIOExceptionClass {
		t.Errorf("a write into a File opened to read answered %v", err)
	}
	if _, err := javaFileWriteByte(client, ctx, client.thread, []uint32{reading, 'x'}); thrownClass(err) != javaIOExceptionClass {
		t.Errorf("write(int) into a File opened to read answered %v", err)
	}
}

// The File constructor reads its name as every call does: the title's own
// directory and a name no file can have are IOExceptions.
func TestStorageContractJavaFileNames(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	for _, name := range []string{"", "/", ".", "__databases__.db"} {
		if _, err := javaCall(t, client, fileOpenKey, 0x1000, newTestString(t, client, name), 4); thrownClass(err) != javaIOExceptionClass {
			t.Errorf("File(%q) answered %v, want IOException", name, err)
		}
	}
}

// A directory made through Java is there after a restart, and gone after one
// once it is removed, read by later clients over the same directory store.
func TestStorageContractJavaPersistsAcrossSessions(t *testing.T) {
	root := t.TempDir()
	session := func() *Client {
		client := fixtureClient(t)
		client.saveStore = backend.NewDirectorySaveStore(root)
		return client
	}
	first := session()
	mustJavaCall(t, first, fsMkdir, newTestString(t, first, "saves"))
	second := session()
	if mustJavaCall(t, second, fsIsDirectory, newTestString(t, second, "saves")) != 1 {
		t.Fatal("after a restart the directory is gone")
	}
	if got := javaVectorNames(t, second, mustJavaCall(t, second, fsList, newTestString(t, second, ""))); !reflect.DeepEqual(got, []string{"data", "saves"}) {
		t.Fatalf("after a restart the root lists %q", got)
	}
	mustJavaCall(t, second, fsRmdir, newTestString(t, second, "saves"))
	third := session()
	if mustJavaCall(t, third, fsExists, newTestString(t, third, "saves")) != 0 {
		t.Fatal("a removed directory came back after a restart")
	}
}

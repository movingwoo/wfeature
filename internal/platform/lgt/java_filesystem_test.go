package lgt

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// The FileSystem and File members beyond the storage contract's own rows (see
// storage_contract_java_test.go): that every one is served in both forms, and
// what rename, toCString, File.seek and File.openDataOutputStream do.

const fsToCString = javaFileSystemClass + ".toCString(Ljava/lang/String;)[B"

// Every member the specification gives FileSystem, and the two File members
// that were missing beside them, is served by name, in both forms where it has
// two. The two forms of each pair are one implementation, because the access
// flag chooses nothing on a platform with one directory — except remove,
// whose flagged form the specification has throw nothing for a missing file.
func TestFileSystemMembersAreServedInBothForms(t *testing.T) {
	served := func(key string) javaPlatformMethod {
		t.Helper()
		method, ok := javaPlatformMethods[key]
		if !ok {
			t.Fatalf("%s is not served", key)
		}
		return method
	}
	for _, pair := range [][2]string{
		{"exists(Ljava/lang/String;)Z", "exists(Ljava/lang/String;I)Z"},
		{"isFile(Ljava/lang/String;)Z", "isFile(Ljava/lang/String;I)Z"},
		{"isDirectory(Ljava/lang/String;)Z", "isDirectory(Ljava/lang/String;I)Z"},
		{"list(Ljava/lang/String;)Ljava/util/Vector;", "list(Ljava/lang/String;I)Ljava/util/Vector;"},
		{"mkdir(Ljava/lang/String;)V", "mkdir(Ljava/lang/String;I)V"},
		{"rmdir(Ljava/lang/String;)V", "rmdir(Ljava/lang/String;I)V"},
		{"remove(Ljava/lang/String;)V", "remove(Ljava/lang/String;I)V"},
		{"rename(Ljava/lang/String;Ljava/lang/String;)V", "rename(Ljava/lang/String;Ljava/lang/String;I)V"},
		{"getCreationTime(Ljava/lang/String;)I", "getCreationTime(Ljava/lang/String;I)I"},
	} {
		short, long := served(javaFileSystemClass+"."+pair[0]), served(javaFileSystemClass+"."+pair[1])
		if long.Words != short.Words+1 {
			t.Errorf("%s takes %d words and its flag form %d", pair[0], short.Words, long.Words)
		}
		same := reflect.ValueOf(short.Implementat).Pointer() == reflect.ValueOf(long.Implementat).Pointer()
		if same == strings.HasPrefix(pair[0], "remove") {
			t.Errorf("the two forms of %s share an implementation: %v", pair[0], same)
		}
	}
	for _, key := range []string{
		javaFileSystemClass + ".<init>()V",
		javaFileSystemClass + ".getMaxFilenameLength()I",
		fsToCString,
		fsTotal,
		fsAvailable,
		javaFileClass + ".seek(I)V",
		javaFileClass + ".openDataOutputStream()Ljava/io/DataOutputStream;",
	} {
		served(key)
	}
}

// rename is MC_fsRename, and every refusal MC_fsRename answers with a code is
// the IOException the specification gives an error while renaming: an absent
// file, a taken name — a file's or a directory's — and a name no file can
// have.
func TestFileSystemRenameIsTheCRename(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	rename := func(from, to string) error {
		t.Helper()
		_, err := javaCall(t, client, fsRename, newTestString(t, client, from), newTestString(t, client, to))
		return err
	}
	if err := rename("data/hello.txt", "moved.txt"); err != nil {
		t.Fatal(err)
	}
	if data, ok := client.readFile("moved.txt"); !ok || string(data) != "packaged" {
		t.Fatalf("the renamed file reads %q (%v)", data, ok)
	}
	if client.fileExists("data/hello.txt") {
		t.Fatal("the old name is still there")
	}
	mustJavaCall(t, client, fsMkdir, newTestString(t, client, "made"))
	for _, refused := range [][2]string{
		{"absent.txt", "elsewhere.txt"}, // M_E_NOENT
		{"data/other.txt", "moved.txt"}, // M_E_EXIST
		{"moved.txt", "made"},           // M_E_EXIST: a directory
		{"moved.txt", "data"},           // M_E_EXIST: a directory the archive's files imply
		{"made", "elsewhere"},           // M_E_NOENT: a directory is not a file
		{"moved.txt", ""},               // M_E_BADFILENAME: the root
		{"moved.txt", ".created"},       // M_E_BADFILENAME
		{"moved.txt", "../outside.txt"}, // M_E_BADFILENAME
	} {
		if err := rename(refused[0], refused[1]); thrownClass(err) != javaIOExceptionClass {
			t.Errorf("rename(%q, %q) answered %v, want IOException", refused[0], refused[1], err)
		}
	}
	if data, ok := client.readFile("moved.txt"); !ok || string(data) != "packaged" {
		t.Fatalf("a refused rename changed the file to %q (%v)", data, ok)
	}
}

// toCString is the text in the handset's encoding with the NUL a C string
// ends in.
func TestFileSystemToCString(t *testing.T) {
	client := fixtureClient(t)
	array := mustJavaCall(t, client, fsToCString, newTestString(t, client, "A가"))
	data, err := client.readJavaArrayBytes(array)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{'A', 0xb0, 0xa1, 0}; !reflect.DeepEqual(data, want) {
		t.Fatalf("toCString = % x, want % x", data, want)
	}
	if _, err := javaCall(t, client, fsToCString, 0); thrownClass(err) != javaThrowNullClass {
		t.Fatalf("toCString(null) answered %v", err)
	}
}

// File.seek is MC_fsSeek from the start of the file. A position the C call
// refuses, and a File that is not open, are the IOException the specification
// gives a pointer that could not be moved.
func TestFileSeekMovesTheCursorAbsolutely(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	client.writeFile("save.dat", []byte("0123456789"))
	object := openJavaFile(t, client, "save.dat", 4)
	seek := func(position int32) error {
		t.Helper()
		_, err := javaCall(t, client, javaFileClass+".seek(I)V", object, uint32(position))
		return err
	}
	read := func(count int) string {
		t.Helper()
		buffer := mustByteArray(t, client, strings.Repeat("\x00", count))
		if _, err := javaFileRead(client, context.Background(), client.thread, []uint32{object, buffer}); err != nil {
			t.Fatal(err)
		}
		data, err := client.readJavaArrayBytes(buffer)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if err := seek(4); err != nil {
		t.Fatal(err)
	}
	if got := read(2); got != "45" {
		t.Fatalf("after seek(4) the file reads %q", got)
	}
	if err := seek(1); err != nil {
		t.Fatal(err)
	}
	if got := read(2); got != "12" {
		t.Fatalf("after seek(1) the file reads %q: the position is not absolute", got)
	}
	if err := seek(-1); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("seek(-1) answered %v", err)
	}
	if err := seek(maxFileBytes + 1); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("a seek past the size a file may grow to answered %v", err)
	}
	if _, err := javaFileClose(client, context.Background(), client.thread, []uint32{object}); err != nil {
		t.Fatal(err)
	}
	if err := seek(0); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("seek on a closed File answered %v", err)
	}
}

// File.openDataOutputStream is openOutputStream with DataOutputStream's
// methods: a typed write lands in the File at its cursor at once, the close
// stores it, and the two openers share the one output stream a File may have.
func TestFileDataOutputStreamWritesIntoTheFile(t *testing.T) {
	ctx := context.Background()
	client, store, _ := fileFixture(t, map[string]string{"fs/record.dat": "head"})
	object := openJavaFile(t, client, "record.dat", 4)
	if _, err := javaCall(t, client, javaFileClass+".seek(I)V", object, 4); err != nil {
		t.Fatal(err)
	}
	key := javaFileClass + ".openDataOutputStream()Ljava/io/DataOutputStream;"
	stream := mustJavaCall(t, client, key, object)
	if class, ok := client.javaClassOfObject(stream); !ok || class.Name != javaDataOutputStreamClass {
		t.Fatal("openDataOutputStream did not answer a DataOutputStream")
	}
	if _, err := javaCall(t, client, key, object); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("a second data output stream answered %v", err)
	}
	if _, err := javaFileOpenOutputStream(client, ctx, client.thread, []uint32{object}); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("openOutputStream beside a data output stream answered %v", err)
	}
	// writeInt and close, through the slots a title dispatches them by.
	dispatch := func(slot uint32, arguments ...uint32) {
		t.Helper()
		for index, value := range arguments {
			if err := client.thread.SetRegister(index, value); err != nil {
				t.Fatal(err)
			}
		}
		if served, err := client.callJavaPlatformVirtual(ctx, client.thread, javaVirtualSlot(javaDataOutputStreamClass, slot)); !served || err != nil {
			t.Fatalf("DataOutputStream slot %d: served=%v error=%v", slot, served, err)
		}
	}
	dispatch(19, stream, 0x01020304)
	if size := javaFileSizeOf(t, client, object); size != 8 {
		t.Fatalf("after writeInt the File holds %d bytes, want 8", size)
	}
	if got := store.held("fs/record.dat"); got != "head" {
		t.Fatalf("the store holds %q before the stream is closed", got)
	}
	dispatch(14, stream)
	if got := store.held("fs/record.dat"); got != "head\x01\x02\x03\x04" {
		t.Fatalf("after the stream's close the store holds %q", got)
	}
	// The close ended the binding, so the File may open another stream.
	if _, err := javaCall(t, client, key, object); err != nil {
		t.Fatalf("a data output stream after the first one closed answered %v", err)
	}
}

// openJavaFile opens a File the way a title's constructor call does.
func openJavaFile(t *testing.T, client *Client, name string, mode uint32) uint32 {
	t.Helper()
	object, err := newTestObject(t, client, javaFileClass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaFileOpen(client, context.Background(), client.thread, []uint32{object, newTestString(t, client, name), mode, 0}); err != nil {
		t.Fatal(err)
	}
	return object
}

// A seek or a write that would take a file past the cap is refused before
// anything is allocated for it: a title's garbage offset or length must not
// size a host allocation. The cap is the common contract's 4 MiB, and a write
// past it fails as a write does, with M_E_NOSPACE.
func TestFileSeekAndWriteStopAtTheFileSizeCap(t *testing.T) {
	client, _, files := fileFixture(t, nil)
	handle := files.open("big.dat", fileOpenReadWrite)
	seek := func(offset, whence uint32) int32 {
		return int32(callSlot(t, client, slotFsSeek, handle, offset, whence))
	}
	if got := seek(maxFileBytes+1, 0); got != wipiError {
		t.Fatalf("a seek past the cap answered %d", got)
	}
	if got := seek(0x7fffffff, 0); got != wipiError {
		t.Fatalf("a seek to 0x7fffffff answered %d", got)
	}
	if got := seek(maxFileBytes, 0); got != maxFileBytes {
		t.Fatalf("a seek to the cap itself answered %d", got)
	}
	if got := seek(1, 1); got != wipiError || client.files[handle].cursor != maxFileBytes {
		t.Fatalf("a relative seek past the cap answered %d and left the cursor at %d", got, client.files[handle].cursor)
	}
	if got := int32(callSlot(t, client, slotFsWrite, handle, files.buffer, 1)); got != wipiNoSpace || len(client.files[handle].data) != 0 {
		t.Fatalf("a write past the cap answered %d and left %d bytes", got, len(client.files[handle].data))
	}
	seek(0, 0)
	if got := int32(callSlot(t, client, slotFsWrite, handle, files.buffer, 0xffffffff)); got != wipiNoSpace {
		t.Fatalf("a write of 0xffffffff bytes answered %d", got)
	}
	// Inside the cap a write past the end fills the gap with zeros, as before.
	seek(4, 0)
	files.write(handle, "ab")
	if open := client.files[handle]; string(open.data) != "\x00\x00\x00\x00ab" || open.cursor != 6 {
		t.Fatalf("a write inside the cap left %q at %d", open.data, open.cursor)
	}
}

// A cursor a checkpoint record restored far past the end of a file is kept, as
// the record has it, and the first write there is refused rather than sizing a
// host allocation from the record: the IOException a write through a File or
// a stream on one fails with.
func TestARestoredCursorPastTheCapCannotGrowAJavaFile(t *testing.T) {
	ctx := context.Background()
	store := newFaultStore(t, map[string]string{"fs/save.dat": "0123"})
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	object := openJavaFile(t, client, "save.dat", 4)
	_, open, err := client.javaFileHandle(object)
	if err != nil {
		t.Fatal(err)
	}
	open.cursor = maxFileBytes
	if _, err := javaFileWriteByte(client, ctx, client.thread, []uint32{object, 'Z'}); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("write(int) past the cap answered %v", err)
	}
	sink, err := javaFileOpenOutputStream(client, ctx, client.thread, []uint32{object})
	if err != nil {
		t.Fatal(err)
	}
	client.javaRun.sinks[sink] = []byte("MARKER")
	if _, err := javaByteSinkFlush(client, ctx, client.thread, []uint32{sink}); thrownClass(err) != javaIOExceptionClass {
		t.Fatalf("a stream drained past the cap answered %v", err)
	}
	if string(open.data) != "0123" || open.dirty {
		t.Fatalf("the refused writes left %d bytes, dirty=%t", len(open.data), open.dirty)
	}
	open.cursor = 6
	if written, err := javaFileWriteByte(client, ctx, client.thread, []uint32{object, 'Z'}); err != nil || written != 1 || string(open.data) != "0123\x00\x00Z" {
		t.Fatalf("write(int) inside the cap = %d, %v, leaving %q", written, err, open.data)
	}
}

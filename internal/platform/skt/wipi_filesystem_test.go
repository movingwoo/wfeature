package skt

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The tests here hold SKT's WIPI FileSystem and File to docs/wipi-storage.md,
// one or more for each rule of it; the database rules are in
// wipi_database_test.go.

const wipiPathText = "Ljava/lang/String;"

// refusingBatchStore is a directory store that can be told to refuse every
// write, one key or a batch, and lists and reads as the directory does.
type refusingBatchStore struct {
	*backend.DirectorySaveStore
	refuse bool
	writes int
}

func (store *refusingBatchStore) StoreSave(name string, data []byte) error {
	store.writes++
	if store.refuse {
		return errors.New("save write refused")
	}
	return store.DirectorySaveStore.StoreSave(name, data)
}

func (store *refusingBatchStore) StoreSaves(entries map[string][]byte) error {
	store.writes++
	if store.refuse {
		return errors.New("save write refused")
	}
	return store.DirectorySaveStore.StoreSaves(entries)
}

// wipiPathMembers are the FileSystem members that take one name, with what
// each answers.
var wipiPathMembers = map[string]string{
	"exists": "Z", "isFile": "Z", "isDirectory": "Z", "getCreationTime": "I",
	"list": "Ljava/util/Vector;", "mkdir": "V", "rmdir": "V", "remove": "V",
}

// Every storage member the WIPI library declares has a body once a session is
// up, with and without the access flag: a declared native nothing registered
// is a title's save failing at the call.
func TestWIPIStorageMembersAreServed(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	const text = wipiPathText
	members := map[string][]string{
		wipi.FileClass: {"write(I)I"},
		wipi.FileSystemClass: {
			"<init>()V", "available()I", "totalSpace()I", "getMaxFilenameLength()I", "toCString(" + text + ")[B",
			"rename(" + text + text + ")V", "rename(" + text + text + "I)V", "sizeOf(" + text + ")I",
		},
		wipi.DataBaseClass: {
			"openDataBase(" + text + "IZ)Lorg/kwis/msp/db/DataBase;", "openDataBase(" + text + "IZI)Lorg/kwis/msp/db/DataBase;",
			"closeDataBase()V", "deleteDataBase(" + text + ")V", "deleteDataBase(" + text + "I)V",
			"deleteRecord(I)V", "getAccessMode(" + text + ")I", "getDataBaseName()" + text,
			"getDataBaseSize()I", "getLastModified()J", "getNumberOfRecords()I", "getRecordSize()I",
			"getSizeAvailable()I", "insertRecord([B)I", "insertRecord([BII)I", "listDataBases()[" + text,
			"selectRecord(I)[B", "selectRecord(I[BI)V", "updateRecord(I[B)V", "updateRecord(I[BII)V",
			"sortRecord(Lorg/kwis/msp/db/DataFilter;Lorg/kwis/msp/db/DataComparator;)[I",
		},
	}
	for name, result := range wipiPathMembers {
		members[wipi.FileSystemClass] = append(members[wipi.FileSystemClass], name+"("+text+")"+result, name+"("+text+"I)"+result)
	}
	for class, list := range members {
		for _, member := range list {
			cut := strings.IndexByte(member, '(')
			if !runtime.VM.HasMethodBody(class, member[:cut], member[cut:]) {
				t.Errorf("%s.%s has no body", class, member)
			}
		}
	}
}

func wipiWriteFile(t *testing.T, runtime *Runtime, name string, data ...byte) {
	t.Helper()
	file := wipiStreamFile(t, runtime, name, wipiFileWriteTrunc)
	if written := storeReviewInt(t, wipiStreamCall(t, runtime, file, "write", "([B)I", ruleBytes(data...))); written != int32(len(data)) {
		t.Fatalf("write %q = %d", name, written)
	}
	wipiStreamCall(t, runtime, file, "close", "()V")
}

func wipiList(t *testing.T, runtime *Runtime, directory string, flag ...int32) ([]string, error) {
	t.Helper()
	value, err := wipiFileSystemCall(runtime, "list", directory, flag...)
	if err != nil {
		return nil, err
	}
	vector, _ := value.Reference()
	if vector == nil || vector.ClassName != jvm.VectorClass {
		t.Fatalf("list(%q) answered %v, want a Vector", directory, vector)
	}
	size := storeReviewInt(t, wipiStreamCall(t, runtime, vector, "size", "()I"))
	names := make([]string, 0, size)
	for index := range size {
		element, _ := wipiStreamCall(t, runtime, vector, "elementAt", "(I)Ljava/lang/Object;", jvm.IntValue(index)).Reference()
		name, ok := jvm.StringText(element)
		if !ok {
			t.Fatalf("list(%q) element %d is not a String", directory, index)
		}
		names = append(names, name)
	}
	return names, nil
}

func wipiFileSystemAsk(t *testing.T, runtime *Runtime, member, name string, flag ...int32) bool {
	t.Helper()
	value, err := wipiFileSystemCall(runtime, member, name, flag...)
	if err != nil {
		t.Fatalf("FileSystem.%s(%q, %v): %v", member, name, flag, err)
	}
	return storeReviewInt(t, value) != 0
}

// wipiFileSystemCall calls a one-name FileSystem member, with the access flag
// when one is given. A name of wipiNullName passes null.
func wipiFileSystemCall(runtime *Runtime, member, name string, flag ...int32) (jvm.Value, error) {
	result := wipiPathMembers[member]
	if member == "sizeOf" {
		result = "I"
	}
	arguments := []jvm.Value{wipiNameArgument(runtime, name)}
	parameters := wipiPathText
	if len(flag) > 0 {
		parameters += "I"
		arguments = append(arguments, jvm.IntValue(flag[0]))
	}
	return runtime.VM.InvokeStatic(wipi.FileSystemClass, member, "("+parameters+")"+result, arguments...)
}

func wipiRename(runtime *Runtime, from, to string, flag ...int32) error {
	descriptor := "(" + wipiPathText + wipiPathText + ")V"
	arguments := []jvm.Value{wipiNameArgument(runtime, from), wipiNameArgument(runtime, to)}
	if len(flag) > 0 {
		descriptor = "(" + wipiPathText + wipiPathText + "I)V"
		arguments = append(arguments, jvm.IntValue(flag[0]))
	}
	_, err := runtime.VM.InvokeStatic(wipi.FileSystemClass, "rename", descriptor, arguments...)
	return err
}

func wipiIOException(runtime *Runtime, err error) bool {
	return runtime.VM.IsGuestException(err, jvm.IOExceptionClass)
}

// Names and areas: a name is a path of segments, so a leading separator, an
// empty segment and a "." segment change nothing, and a ".." segment is
// refused.
func TestWIPIFileSystemNamesAreNormalized(t *testing.T) {
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(t.TempDir()))
	wipiWriteFile(t, runtime, "./save.dat", 7)
	for _, spelling := range []string{"save.dat", "/save.dat", "./save.dat", "//./save.dat"} {
		if !wipiFileSystemAsk(t, runtime, "isFile", spelling) {
			t.Fatalf("isFile(%q) is false", spelling)
		}
	}
	if names, _ := wipiList(t, runtime, "/"); strings.Count(strings.Join(names, "\n")+"\n", "save.dat\n") != 1 {
		t.Fatalf("list(/) = %q, want save.dat once", names)
	}
	for _, climbing := range []string{"../save.dat", "a/../../save.dat"} {
		if _, err := wipiFileSystemCall(runtime, "exists", climbing); !wipiIOException(runtime, err) {
			t.Fatalf("exists(%q): %v", climbing, err)
		}
		if _, err := runtime.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;I)V", ruleText(runtime, climbing), jvm.IntValue(wipiFileReadWrite)); !wipiIOException(runtime, err) {
			t.Fatalf("new File(%q): %v", climbing, err)
		}
	}
}

// Names and areas: "", "/" and "." are the root, a directory every member
// answers for — and one that cannot be made, removed or renamed.
func TestWIPIFileSystemRoot(t *testing.T) {
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(t.TempDir()))
	wipiWriteFile(t, runtime, "top", 1)
	want, err := wipiList(t, runtime, "/")
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"", "/", ".", "./", "//"} {
		if !wipiFileSystemAsk(t, runtime, "exists", root) || !wipiFileSystemAsk(t, runtime, "isDirectory", root) || wipiFileSystemAsk(t, runtime, "isFile", root) {
			t.Fatalf("%q is not the root directory", root)
		}
		if created, err := wipiFileSystemCall(runtime, "getCreationTime", root); err != nil || storeReviewInt(t, created) != -1 {
			t.Fatalf("getCreationTime(%q) = %v, %v", root, created, err)
		}
		if names, err := wipiList(t, runtime, root); err != nil || !slices.Equal(names, want) {
			t.Fatalf("list(%q) = %q, %v", root, names, err)
		}
		for _, member := range []string{"mkdir", "rmdir", "remove"} {
			if _, err := wipiFileSystemCall(runtime, member, root); !wipiIOException(runtime, err) {
				t.Fatalf("%s(%q): %v", member, root, err)
			}
		}
		if _, err := wipiFileSystemCall(runtime, "remove", root, wipi.PrivateAccess); err != nil {
			t.Fatalf("remove(%q, flag) is not a file and returns: %v", root, err)
		}
		if err := wipiRename(runtime, root, "moved"); !wipiIOException(runtime, err) {
			t.Fatalf("rename(%q): %v", root, err)
		}
	}
}

// Names and areas: the access flag addresses the one area; every flag, the
// three the specification names and any other number, answers alike, and
// nothing answers SecurityException.
func TestWIPIFileSystemAccessFlagsAddressOneArea(t *testing.T) {
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(t.TempDir()))
	wipiWriteFile(t, runtime, "dir/file", 1)
	for _, flag := range []int32{wipi.PrivateAccess, wipi.SharedAccess, wipi.SystemAccess, 0, 99, -1} {
		if !wipiFileSystemAsk(t, runtime, "exists", "dir/file", flag) || !wipiFileSystemAsk(t, runtime, "isFile", "dir/file", flag) ||
			!wipiFileSystemAsk(t, runtime, "isDirectory", "dir", flag) {
			t.Fatalf("flag %d does not address the title's own directory", flag)
		}
		if names, err := wipiList(t, runtime, "dir", flag); err != nil || !slices.Equal(names, []string{"file"}) {
			t.Fatalf("list(dir, %d) = %q, %v", flag, names, err)
		}
		if _, err := wipiFileSystemCall(runtime, "exists", "/.removed", flag); runtime.VM.IsGuestException(err, "java/lang/SecurityException") {
			t.Fatalf("flag %d answered SecurityException", flag)
		}
	}
}

// Names and areas: the longest name is 255 bytes, which getMaxFilenameLength
// answers; every File and FileSystem call refuses a longer one with
// IOException. The bound is on the name as the store keeps it, so a leading
// separator does not count. XFile, which the contract does not cover, keeps
// taking longer names.
func TestWIPIFileSystemNamesLongerThan255Bytes(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	if length := ruleStatic(t, runtime, wipi.FileSystemClass, "getMaxFilenameLength", "()I"); length != 255 {
		t.Fatalf("getMaxFilenameLength() = %d, want 255", length)
	}
	longest := strings.Repeat("a", 255)
	longer := longest + "a"
	wipiWriteFile(t, runtime, "/"+longest, 1)
	if !wipiFileSystemAsk(t, runtime, "isFile", longest) {
		t.Fatal("a 255-byte name is not a file")
	}
	for _, name := range []string{longer, "dir/" + strings.Repeat("b", 252), strings.Repeat("가", 86)} {
		for member := range wipiPathMembers {
			for _, flag := range [][]int32{nil, {wipi.PrivateAccess}} {
				if _, err := wipiFileSystemCall(runtime, member, name, flag...); !wipiIOException(runtime, err) {
					t.Fatalf("%s of a %d-byte name, flag %v: %v", member, len(name), flag, err)
				}
			}
		}
		if _, err := wipiFileSystemCall(runtime, "sizeOf", name); !wipiIOException(runtime, err) {
			t.Fatalf("sizeOf of a %d-byte name: %v", len(name), err)
		}
		for _, pair := range [][2]string{{longest, name}, {name, "short"}} {
			if err := wipiRename(runtime, pair[0], pair[1]); !wipiIOException(runtime, err) {
				t.Fatalf("rename with a %d-byte name: %v", len(name), err)
			}
		}
		if _, err := runtime.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;I)V", ruleText(runtime, name), jvm.IntValue(wipiFileReadWrite)); !wipiIOException(runtime, err) {
			t.Fatalf("new File of a %d-byte name: %v", len(name), err)
		}
		if _, err := runtime.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;II)V", ruleText(runtime, name), jvm.IntValue(wipiFileReadWrite), jvm.IntValue(wipi.PrivateAccess)); !wipiIOException(runtime, err) {
			t.Fatalf("new File of a %d-byte name with a flag: %v", len(name), err)
		}
	}
	// A name the store cannot keep is IOException too.
	if _, err := wipiFileSystemCall(runtime, "exists", "nul\x00name"); !wipiIOException(runtime, err) {
		t.Fatalf("exists of a name with a NUL: %v", err)
	}
	ruleXFile(t, runtime, "/"+longer, xFileRead|xFileWrite)
	if !ruleExists(t, runtime, longer) {
		t.Fatal("XFile no longer takes a name the WIPI bound refuses")
	}
}

// Names and areas: a backslash separates segments as a slash does, in every
// File, FileSystem and DataBase name, before the 255-byte count, and in a
// later session too, since the store keeps the slash. XFile, which the
// contract does not cover, keeps refusing one.
func TestWIPIStorageBackslashSeparatesSegments(t *testing.T) {
	root := t.TempDir()
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	wipiWriteFile(t, runtime, `data\save.dat`, 1, 2)
	for _, spelling := range []string{"data/save.dat", `data\save.dat`, `\data\save.dat`, `.\data\\save.dat`} {
		if !wipiFileSystemAsk(t, runtime, "isFile", spelling) {
			t.Fatalf("isFile(%q) is false", spelling)
		}
	}
	if !wipiFileSystemAsk(t, runtime, "isDirectory", `data\`) {
		t.Fatal(`data\ is not a directory`)
	}
	if names, err := wipiList(t, runtime, `data\`); err != nil || !slices.Equal(names, []string{"save.dat"}) {
		t.Fatalf(`list(data\) = %q, %v`, names, err)
	}
	rootNames, _ := wipiList(t, runtime, "/")
	for _, spelling := range []string{`\`, `.\`, `\.\`} {
		if names, err := wipiList(t, runtime, spelling); err != nil || !slices.Equal(names, rootNames) {
			t.Fatalf("list(%q) = %q, %v; want the root's", spelling, names, err)
		}
	}
	if size, err := wipiFileSystemCall(runtime, "sizeOf", `data\save.dat`); err != nil || storeReviewInt(t, size) != 2 {
		t.Fatalf(`sizeOf(data\save.dat) = %v, %v`, size, err)
	}
	if created, err := wipiFileSystemCall(runtime, "getCreationTime", `data\save.dat`); err != nil || storeReviewInt(t, created) != 0 {
		t.Fatalf(`getCreationTime(data\save.dat) = %v, %v`, created, err)
	}
	if _, err := wipiFileSystemCall(runtime, "mkdir", `made\deep`); err != nil {
		t.Fatal(err)
	}
	if !wipiFileSystemAsk(t, runtime, "isDirectory", "made/deep") {
		t.Fatal(`mkdir(made\deep) did not make made/deep`)
	}
	if _, err := wipiFileSystemCall(runtime, "rmdir", `made\deep`, wipi.PrivateAccess); err != nil {
		t.Fatal(err)
	}
	if err := wipiRename(runtime, `data\save.dat`, `data\moved.dat`); err != nil {
		t.Fatal(err)
	}
	if !wipiFileSystemAsk(t, runtime, "isFile", "data/moved.dat") || wipiFileSystemAsk(t, runtime, "exists", "data/save.dat") {
		t.Fatal("a rename named with backslashes did not move the file")
	}
	for _, flag := range [][]int32{nil, {wipi.PrivateAccess}} {
		wipiWriteFile(t, runtime, `data\gone.dat`, 4)
		if _, err := wipiFileSystemCall(runtime, "remove", `data\gone.dat`, flag...); err != nil || wipiFileSystemAsk(t, runtime, "exists", "data/gone.dat") {
			t.Fatalf(`remove(data\gone.dat, %v): %v`, flag, err)
		}
	}
	if _, err := wipiFileSystemCall(runtime, "exists", `..\x`); !wipiIOException(runtime, err) {
		t.Fatalf(`exists(..\x): %v`, err)
	}
	// The 255 bytes are counted after the backslashes are read: a leading one
	// is dropped, and one between segments counts as the slash it is.
	for _, fits := range []string{`\` + strings.Repeat("a", 255), `a\` + strings.Repeat("b", 253)} {
		if _, err := wipiFileSystemCall(runtime, "exists", fits); err != nil {
			t.Fatalf("exists(%q): %v", fits, err)
		}
	}
	if _, err := wipiFileSystemCall(runtime, "exists", `a\`+strings.Repeat("b", 254)); !wipiIOException(runtime, err) {
		t.Fatalf("exists of a 256-byte path: %v", err)
	}
	db := wipiMustOpen(t, runtime, `dir\save`, 2, true)
	wipiInsert(t, runtime, db, 9)
	if same, err := wipiOpen(runtime, "dir/save", 0, false); err != nil || same.Native != db.Native {
		t.Fatalf(`dir/save is not the database dir\save made: %v`, err)
	}
	if got := wipiNames(t, runtime); !slices.Equal(got, []string{"dir/save"}) {
		t.Fatalf("listDataBases() = %q", got)
	}
	if mode, err := wipiAccessMode(runtime, `dir\save`); err != nil || mode != wipi.PrivateAccess {
		t.Fatalf(`getAccessMode(dir\save) = %d, %v`, mode, err)
	}
	if _, err := runtime.VM.NewObject(skvm.XFileClass, "(Ljava/lang/String;I)V", ruleText(runtime, `data\x`), jvm.IntValue(xFileRead|xFileWrite)); !wipiIOException(runtime, err) {
		t.Fatalf("XFile now takes a backslash: %v", err)
	}
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}

	store := backend.NewDirectorySaveStore(root)
	for _, key := range []string{"fs/data/moved.dat", "jdb/dir/save"} {
		if _, ok := store.LoadSave(key); !ok {
			t.Fatalf("%s is not where the store keeps it", key)
		}
	}
	later := wipiStreamRuntime(t, store)
	if !ruleExists(t, later, "data/moved.dat") || !wipiFileSystemAsk(t, later, "isFile", `data\moved.dat`) {
		t.Fatal("a later session does not find the file named with a backslash")
	}
	reopened := wipiMustOpen(t, later, `dir\save`, 0, false)
	if !bytes.Equal(wipiSelect(t, later, reopened, 0), []byte{9}) {
		t.Fatal("a later session does not find the database named with a backslash")
	}
	if _, err := later.VM.InvokeStatic(wipi.DataBaseClass, "deleteDataBase", "(Ljava/lang/String;)V", ruleText(later, `dir\save`)); err != nil {
		t.Fatal(err)
	}
	if got := wipiNames(t, later); got != nil {
		t.Fatalf("listDataBases() after the delete = %q", got)
	}
}

// Names and areas: a null name is NullPointerException to every FileSystem
// and File call that takes one, in either form and either rename position.
func TestWIPIFileSystemNullNames(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	isNull := func(err error) bool { return runtime.VM.IsGuestException(err, "java/lang/NullPointerException") }
	for member := range wipiPathMembers {
		for _, flag := range [][]int32{nil, {wipi.PrivateAccess}} {
			if _, err := wipiFileSystemCall(runtime, member, wipiNullName, flag...); !isNull(err) {
				t.Fatalf("%s(null, %v): %v", member, flag, err)
			}
		}
	}
	if _, err := wipiFileSystemCall(runtime, "sizeOf", wipiNullName); !isNull(err) {
		t.Fatalf("sizeOf(null): %v", err)
	}
	for _, pair := range [][2]string{{wipiNullName, "b"}, {"a", wipiNullName}} {
		for _, flag := range [][]int32{nil, {wipi.PrivateAccess}} {
			if err := wipiRename(runtime, pair[0], pair[1], flag...); !isNull(err) {
				t.Fatalf("rename(%q, %q, %v): %v", pair[0], pair[1], flag, err)
			}
		}
	}
	if _, err := runtime.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;I)V", jvm.ReferenceValue(nil), jvm.IntValue(wipiFileReadWrite)); !isNull(err) {
		t.Fatalf("new File(null): %v", err)
	}
	if _, err := runtime.VM.NewObject(wipi.FileClass, "(Ljava/lang/String;II)V", jvm.ReferenceValue(nil), jvm.IntValue(wipiFileReadWrite), jvm.IntValue(1)); !isNull(err) {
		t.Fatalf("new File(null, mode, flag): %v", err)
	}
}

// What a directory is, and list: a listing reads the saves of earlier sessions
// as well as this one's and lists the archive's files; a name is the entry
// directly below, each once, in name order, a deeper path appearing as its
// directory; a name that is not a directory is IOException.
func TestWIPIFileSystemListFindsEarlierSessionsFiles(t *testing.T) {
	root := t.TempDir()
	first := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	wipiWriteFile(t, first, "save/slot1", 1)
	wipiWriteFile(t, first, "save/slot2", 2)
	wipiWriteFile(t, first, "/save/deep/slot3", 3)
	wipiWriteFile(t, first, "top", 4)
	ruleRemove(t, first, "save/slot2")
	if err := first.Destroy(true); err != nil {
		t.Fatal(err)
	}

	second := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	names, err := wipiList(t, second, "save")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"deep", "slot1"}; !slices.Equal(names, want) {
		t.Fatalf("list(save) = %q, want %q", names, want)
	}
	names, err = wipiList(t, second, "/")
	if err != nil {
		t.Fatal(err)
	}
	// The archive's own files are files here, as exists says they are; the
	// removal list is not.
	for _, want := range []string{"save", "top", "data.bin", "META-INF"} {
		if !slices.Contains(names, want) {
			t.Fatalf("list(/) = %q, missing %q", names, want)
		}
	}
	if slices.Contains(names, ".removed") || !slices.IsSorted(names) || len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Fatalf("list(/) = %q: the removal list, out of order, or twice", names)
	}
	if deep, _ := wipiList(t, second, "save/deep"); !slices.Equal(deep, []string{"slot3"}) {
		t.Fatalf("list(save/deep) = %q", deep)
	}
	if !wipiFileSystemAsk(t, second, "isDirectory", "save") || !wipiFileSystemAsk(t, second, "exists", "save") || wipiFileSystemAsk(t, second, "isFile", "save") {
		t.Fatal("a directory an earlier session wrote into is not a directory")
	}
	if wipiFileSystemAsk(t, second, "isDirectory", "top") || !wipiFileSystemAsk(t, second, "isFile", "top") {
		t.Fatal("a file is a directory")
	}
	for _, missing := range []string{"top", "nothing", "save/slot2", "/.removed"} {
		if _, err := wipiList(t, second, missing); !wipiIOException(second, err) {
			t.Fatalf("list(%q) error = %v, want IOException", missing, err)
		}
	}
}

// A store that cannot list is answered from what the session knows, which is
// the files it wrote and the archive's: a listing never fails for it.
func TestWIPIFileSystemListWithoutAListingStore(t *testing.T) {
	store := &refusingWIPIStreamStore{SaveStore: backend.NewDirectorySaveStore(t.TempDir())}
	if keys, listed, _ := backend.ListSaves(store, "fs"); listed || keys != nil {
		t.Fatal("the wrapped store lists")
	}
	runtime := wipiStreamRuntime(t, store)
	wipiWriteFile(t, runtime, "save/one", 1)
	if names, err := wipiList(t, runtime, "save"); err != nil || !slices.Equal(names, []string{"one"}) {
		t.Fatalf("list(save) = %q, %v", names, err)
	}
}

// The directory table: a directory is the root, a name mkdir recorded, or a
// name with a file below it, and never a name that is a file. mkdir refuses a
// name that is a file or a directory and does not ask for a parent; rmdir
// refuses something below, a name that is not a directory, and the root;
// getCreationTime is 0 for a file, -1 for a directory, IOException for
// neither.
func TestWIPIFileSystemDirectories(t *testing.T) {
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(t.TempDir()))
	call := func(member, name string, flag ...int32) error {
		_, err := wipiFileSystemCall(runtime, member, name, flag...)
		return err
	}
	if err := call("mkdir", "made", wipi.PrivateAccess); err != nil {
		t.Fatal(err)
	}
	if !wipiFileSystemAsk(t, runtime, "isDirectory", "made") || !wipiFileSystemAsk(t, runtime, "exists", "/made") || wipiFileSystemAsk(t, runtime, "isFile", "made") {
		t.Fatal("a directory mkdir made is not one")
	}
	if names, err := wipiList(t, runtime, "made"); err != nil || len(names) != 0 {
		t.Fatalf("list(made) = %q, %v; want an empty listing", names, err)
	}
	if names, _ := wipiList(t, runtime, "/"); !slices.Contains(names, "made") {
		t.Fatalf("list(/) = %q, missing the made directory", names)
	}
	if created, err := wipiFileSystemCall(runtime, "getCreationTime", "made"); err != nil || storeReviewInt(t, created) != -1 {
		t.Fatalf("getCreationTime(made) = %v, %v; want the directory's -1", created, err)
	}
	if err := call("mkdir", "made"); !wipiIOException(runtime, err) {
		t.Fatalf("mkdir of an existing directory: %v", err)
	}
	wipiWriteFile(t, runtime, "made/inside", 9)
	if err := call("rmdir", "made"); !wipiIOException(runtime, err) {
		t.Fatalf("rmdir of a directory with a file in it: %v", err)
	}
	ruleRemove(t, runtime, "made/inside")
	if err := call("rmdir", "made", wipi.SharedAccess); err != nil {
		t.Fatalf("rmdir of the emptied directory: %v", err)
	}
	if wipiFileSystemAsk(t, runtime, "exists", "made") {
		t.Fatal("a removed directory is still there")
	}
	if err := call("rmdir", "made"); !wipiIOException(runtime, err) {
		t.Fatalf("rmdir of a directory that is gone: %v", err)
	}
	// A file written below a directory nobody made implies it.
	wipiWriteFile(t, runtime, "implied/file", 1)
	if !wipiFileSystemAsk(t, runtime, "isDirectory", "implied") {
		t.Fatal("a file's parent is not a directory")
	}
	if err := call("mkdir", "implied"); !wipiIOException(runtime, err) {
		t.Fatalf("mkdir of an implied directory: %v", err)
	}
	wipiWriteFile(t, runtime, "plain", 1)
	for _, member := range []string{"mkdir", "rmdir"} {
		if err := call(member, "plain"); !wipiIOException(runtime, err) {
			t.Fatalf("%s of a file: %v", member, err)
		}
	}
	// A directory made deeper needs no parent, and makes its parent one.
	if err := call("mkdir", "a/b"); err != nil {
		t.Fatal(err)
	}
	if !wipiFileSystemAsk(t, runtime, "isDirectory", "a") {
		t.Fatal("the parent of a made directory is not a directory")
	}
	if err := call("rmdir", "a"); !wipiIOException(runtime, err) {
		t.Fatalf("rmdir of a directory holding a made one: %v", err)
	}
	if created, err := wipiFileSystemCall(runtime, "getCreationTime", "plain", wipi.PrivateAccess); err != nil || storeReviewInt(t, created) != 0 {
		t.Fatalf("getCreationTime(plain) = %v, %v; want 0, the store keeps no time", created, err)
	}
	if _, err := wipiFileSystemCall(runtime, "getCreationTime", "missing"); !wipiIOException(runtime, err) {
		t.Fatalf("getCreationTime of a missing name: %v", err)
	}
}

// mkdir records its name in a list beside the saves, so a directory made and
// left empty is still there after a restart, and rmdir takes it off for good.
// The list is a platform key no title path or XFile name reaches, a save tree
// without one has no directory made, and a list the store refuses to change is
// IOException with nothing changed.
func TestWIPIFileSystemMadeDirectoriesPersist(t *testing.T) {
	root := t.TempDir()
	first := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	if wipiFileSystemAsk(t, first, "isDirectory", "empty") {
		t.Fatal("a save tree without a directory list has a directory")
	}
	for _, name := range []string{"empty", "kept/deep", "gone"} {
		if _, err := wipiFileSystemCall(first, "mkdir", name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := wipiFileSystemCall(first, "rmdir", "gone"); err != nil {
		t.Fatal(err)
	}
	// A file named like the list, by either API, is the title's own file.
	wipiWriteFile(t, first, "wipi/directories", 1)
	ruleXWrite(t, first, ruleXFile(t, first, "/wipi/directories", xFileWrite), 2)
	if err := first.Destroy(true); err != nil {
		t.Fatal(err)
	}
	store := backend.NewDirectorySaveStore(root)
	if list, _ := store.LoadSave(wipiDirectoriesKey); string(list) != "empty\nkept/deep" {
		t.Fatalf("%s = %q", wipiDirectoriesKey, list)
	}

	second := wipiStreamRuntime(t, store)
	if !wipiFileSystemAsk(t, second, "isDirectory", "empty") || !wipiFileSystemAsk(t, second, "isDirectory", "kept") || wipiFileSystemAsk(t, second, "exists", "gone") {
		t.Fatal("the directories made in an earlier session are not what the list says")
	}
	if names, _ := wipiList(t, second, "/"); !slices.Contains(names, "empty") || !slices.Contains(names, "kept") || slices.Contains(names, "gone") {
		t.Fatalf("list(/) = %q", names)
	}
	if names, err := wipiList(t, second, "kept"); err != nil || !slices.Equal(names, []string{"deep"}) {
		t.Fatalf("list(kept) = %q, %v", names, err)
	}
	if _, err := wipiFileSystemCall(second, "mkdir", "empty"); !wipiIOException(second, err) {
		t.Fatalf("mkdir of a directory an earlier session made: %v", err)
	}
	if _, err := wipiFileSystemCall(second, "rmdir", "empty"); err != nil {
		t.Fatal(err)
	}
	if err := second.Destroy(true); err != nil {
		t.Fatal(err)
	}
	third := wipiStreamRuntime(t, store)
	if wipiFileSystemAsk(t, third, "exists", "empty") || !wipiFileSystemAsk(t, third, "isDirectory", "kept/deep") {
		t.Fatal("an rmdir did not reach the next session")
	}
	if err := third.Destroy(true); err != nil {
		t.Fatal(err)
	}

	refusing := &refusingBatchStore{DirectorySaveStore: store}
	fourth := wipiStreamRuntime(t, refusing)
	refusing.refuse = true
	if _, err := wipiFileSystemCall(fourth, "mkdir", "refused"); !wipiIOException(fourth, err) {
		t.Fatalf("a refused mkdir: %v", err)
	}
	if _, err := wipiFileSystemCall(fourth, "rmdir", "kept/deep"); !wipiIOException(fourth, err) {
		t.Fatalf("a refused rmdir: %v", err)
	}
	if wipiFileSystemAsk(t, fourth, "exists", "refused") || !wipiFileSystemAsk(t, fourth, "isDirectory", "kept/deep") {
		t.Fatal("a refused directory change was made in the session")
	}
}

// Removing a file: remove(name) is IOException for a name that is not a file —
// none there, a directory — and remove(name, flag) returns either way, as the
// specification words the flagged form. A file is removed by both, and a
// removal the store refuses is IOException from both.
func TestWIPIFileSystemRemoveForms(t *testing.T) {
	store := &refusingBatchStore{DirectorySaveStore: backend.NewDirectorySaveStore(t.TempDir())}
	runtime := wipiStreamRuntime(t, store)
	wipiWriteFile(t, runtime, "one", 1)
	wipiWriteFile(t, runtime, "two", 2)
	wipiWriteFile(t, runtime, "dir/inside", 3)
	if _, err := wipiFileSystemCall(runtime, "remove", "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := wipiFileSystemCall(runtime, "remove", "two", wipi.SharedAccess); err != nil {
		t.Fatal(err)
	}
	if wipiFileSystemAsk(t, runtime, "exists", "one") || wipiFileSystemAsk(t, runtime, "exists", "two") {
		t.Fatal("a removed file is still there")
	}
	for _, notAFile := range []string{"one", "never", "dir", "line\nbreak"} {
		if _, err := wipiFileSystemCall(runtime, "remove", notAFile); !wipiIOException(runtime, err) {
			t.Fatalf("remove(%q): %v", notAFile, err)
		}
		for _, flag := range []int32{wipi.PrivateAccess, wipi.SystemAccess} {
			if _, err := wipiFileSystemCall(runtime, "remove", notAFile, flag); err != nil {
				t.Fatalf("remove(%q, %d) of what is not a file: %v", notAFile, flag, err)
			}
		}
	}
	if !wipiFileSystemAsk(t, runtime, "isFile", "dir/inside") {
		t.Fatal("removing a directory's name removed what is in it")
	}
	store.refuse = true
	for _, flag := range [][]int32{nil, {wipi.PrivateAccess}} {
		if _, err := wipiFileSystemCall(runtime, "remove", "dir/inside", flag...); !wipiIOException(runtime, err) {
			t.Fatalf("a removal the store refused, flag %v: %v", flag, err)
		}
		wipiWriteFile(t, runtime, "dir/inside", 3)
	}
}

// A rename moves a file to its new name in the session and in the store, in
// one batch: a later session finds it under the new name only, and the old
// name is on the removal list, so a packaged file of that name stays hidden.
func TestWIPIFileSystemRenameReachesLaterSessions(t *testing.T) {
	root := t.TempDir()
	first := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	wipiWriteFile(t, first, "save/a", 1, 2, 3)
	if err := wipiRename(first, "save/a", "save/b"); err != nil {
		t.Fatal(err)
	}
	if ruleExists(t, first, "save/a") || !ruleExists(t, first, "save/b") || ruleSize(t, first, "save/b") != 3 {
		t.Fatal("the rename is not what the session sees")
	}
	// The archive's own file is renamed as one of the title's files.
	if err := wipiRename(first, "data.bin", "copy.bin", wipi.PrivateAccess); err != nil {
		t.Fatal(err)
	}
	if err := first.Destroy(true); err != nil {
		t.Fatal(err)
	}
	store := backend.NewDirectorySaveStore(root)
	if data, ok := store.LoadSave("fs/save/b"); !ok || !bytes.Equal(data, []byte{1, 2, 3}) {
		t.Fatalf("fs/save/b = %v, %v", data, ok)
	}
	if removed, _ := store.LoadSave(xFileRemovedKey); !slices.Equal(splitRemovalList(removed), []string{"data.bin", "save/a"}) {
		t.Fatalf("removal list = %q", removed)
	}
	second := wipiStreamRuntime(t, store)
	if ruleExists(t, second, "save/a") || ruleExists(t, second, "data.bin") || ruleSize(t, second, "copy.bin") != 8 {
		t.Fatal("a later session does not find the renamed files where the rename put them")
	}
	if names, _ := wipiList(t, second, "save"); !slices.Equal(names, []string{"b"}) {
		t.Fatalf("list(save) = %q", names)
	}
}

// The rename refusals, each an IOException that changes no file: the flagged
// form takes names in the root and the other two paths in one directory, and
// an existing new name, a missing old one, a directory, the root and the
// removal list's own name are refused.
func TestWIPIFileSystemRenameRefusals(t *testing.T) {
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(t.TempDir()))
	wipiWriteFile(t, runtime, "save/a", 1)
	wipiWriteFile(t, runtime, "save/b", 2)
	wipiWriteFile(t, runtime, "top", 3)
	for _, refused := range []struct {
		from, to string
		flag     []int32
	}{
		{"save/a", "save/b", nil},          // the new name is a file
		{"save/a", "./save/b", nil},        // the same, spelled another way
		{"save/a", "other/a", nil},         // another directory
		{"save/a", "a", nil},               // another directory
		{"save/a", "save/c", []int32{1}},   // the flagged form takes no directory
		{"missing", "fresh", nil},          // nothing to rename
		{"save", "saved", nil},             // a directory is not a file
		{"top", "save", nil},               // the new name is a directory
		{"top", ".removed", []int32{1}},    // the platform's own list
		{"top", "line\nbreak", []int32{1}}, // the list could not carry it
		{"/", "root", nil},                 // the root
		{"save/a", "save/../../x", nil},    // a path that climbs out
	} {
		if err := wipiRename(runtime, refused.from, refused.to, refused.flag...); !wipiIOException(runtime, err) {
			t.Fatalf("rename(%q, %q, %v) error = %v, want IOException", refused.from, refused.to, refused.flag, err)
		}
	}
	for _, name := range []string{"save/a", "save/b", "top"} {
		if ruleSize(t, runtime, name) != 1 {
			t.Fatalf("a refused rename changed %s", name)
		}
	}
	if err := wipiRename(runtime, "top", "/second", wipi.PrivateAccess); err != nil {
		t.Fatalf("a flagged rename in the root: %v", err)
	}
	if ruleExists(t, runtime, "top") || ruleSize(t, runtime, "second") != 1 {
		t.Fatal("a flagged rename in the root did not rename")
	}
}

// A rename the store refuses is not made: the title keeps its file under the
// old name, in the session and in the store, and nothing is left waiting to be
// written over it at the session's end.
func TestWIPIFileSystemRenameRefusedByTheStoreChangesNothing(t *testing.T) {
	store := &refusingBatchStore{DirectorySaveStore: backend.NewDirectorySaveStore(t.TempDir())}
	runtime := wipiStreamRuntime(t, store)
	wipiWriteFile(t, runtime, "slot", 4, 5)
	store.refuse = true
	if err := wipiRename(runtime, "slot", "moved"); !wipiIOException(runtime, err) {
		t.Fatalf("a refused rename: %v", err)
	}
	if ruleSize(t, runtime, "slot") != 2 || ruleExists(t, runtime, "moved") {
		t.Fatal("a refused rename moved the file in the session")
	}
	store.refuse = false
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}
	if data, ok := store.LoadSave("fs/slot"); !ok || !bytes.Equal(data, []byte{4, 5}) {
		t.Fatalf("fs/slot = %v, %v after a refused rename", data, ok)
	}
	if _, ok := store.LoadSave("fs/moved"); ok {
		t.Fatal("a refused rename reached the store at the session's end")
	}
	if removed, _ := store.LoadSave(xFileRemovedKey); len(removed) != 0 {
		t.Fatalf("a refused rename left the removal list %q", removed)
	}
}

// Space: every title has 1 MiB. available is 1 MiB less what the title has
// stored — each file it wrote, less the archive's file of the same name, and
// each database's records — in this session and earlier ones, never below
// zero; the platform's lists are not counted, DataBase.getSizeAvailable
// answers the same figure, and nothing is refused to keep it true. XFile keeps
// its own answers.
func TestWIPIFileSystemSpace(t *testing.T) {
	root := t.TempDir()
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	available := func(runtime *Runtime) int32 {
		return ruleStatic(t, runtime, wipi.FileSystemClass, "available", "()I")
	}
	if total := ruleStatic(t, runtime, wipi.FileSystemClass, "totalSpace", "()I"); total != 1<<20 {
		t.Fatalf("totalSpace() = %d, want 1 MiB", total)
	}
	if free := available(runtime); free != 1<<20 {
		t.Fatalf("available() with nothing stored = %d", free)
	}
	wipiWriteFile(t, runtime, "save", make([]byte, 100)...)
	// data.bin ships 8 bytes, fixture.png 79: a longer data.bin counts what it
	// adds, and a shorter fixture.png counts nothing.
	wipiWriteFile(t, runtime, "data.bin", make([]byte, 20)...)
	wipiWriteFile(t, runtime, "fixture.png", make([]byte, 10)...)
	db := wipiMustOpen(t, runtime, "records", 4, true)
	wipiInsert(t, runtime, db, make([]byte, 50)...)
	// A database counts as a file does, less the archive's file of its
	// name: 30 bytes of records against a shipped 8-byte data.bin add 22.
	shipped := wipiMustOpen(t, runtime, "data.bin", 4, true)
	wipiInsert(t, runtime, shipped, make([]byte, 30)...)
	// The platform's lists are not the title's.
	if _, err := wipiFileSystemCall(runtime, "mkdir", "made"); err != nil {
		t.Fatal(err)
	}
	wipiWriteFile(t, runtime, "gone", make([]byte, 1000)...)
	ruleRemove(t, runtime, "gone")
	want := int32(1<<20 - 100 - 12 - 50 - 22)
	if free := available(runtime); free != want {
		t.Fatalf("available() = %d, want %d", free, want)
	}
	if free := wipiInt(t, runtime, db, "getSizeAvailable"); free != want {
		t.Fatalf("DataBase.getSizeAvailable() = %d, want the title's %d", free, want)
	}
	if xfile := ruleStatic(t, runtime, skvm.XFileClass, "fsavail", "()I"); xfile != xFileCapacity {
		t.Fatalf("XFile.fsavail() = %d, want its own %d", xfile, xFileCapacity)
	}
	if err := runtime.Destroy(true); err != nil {
		t.Fatal(err)
	}

	later := wipiStreamRuntime(t, backend.NewDirectorySaveStore(root))
	if free := available(later); free != want {
		t.Fatalf("a later session's available() = %d, want %d", free, want)
	}
	// Nothing is refused for space, and the figure stops at zero.
	wipiWriteFile(t, later, "large", make([]byte, 1<<20)...)
	if free := available(later); free != 0 {
		t.Fatalf("available() past the space = %d, want 0", free)
	}
	reopened := wipiMustOpen(t, later, "records", 4, false)
	wipiInsert(t, later, reopened, 1)
	if free := wipiInt(t, later, reopened, "getSizeAvailable"); free != 0 {
		t.Fatalf("getSizeAvailable() past the space = %d", free)
	}
}

// Space: one file holds at most 4 MiB. A WIPI File write that would grow it
// past is IOException from every form and from the output stream, a write
// within it goes on, and XFile keeps its own bound.
func TestWIPIFileWritesAreBoundedAt4MiB(t *testing.T) {
	runtime := wipiStreamRuntime(t, nil)
	// XFile, past the WIPI bound, makes a file exactly 4 MiB long.
	xfile := ruleXFile(t, runtime, "big", xFileRead|xFileWrite)
	wipiStreamCall(t, runtime, xfile, "seek", "(II)I", jvm.IntValue(wipiFileMaxBytes-1), jvm.IntValue(0))
	ruleXWrite(t, runtime, xfile, 1)
	if ruleSize(t, runtime, "big") != wipiFileMaxBytes {
		t.Fatal("XFile did not make the file")
	}
	file := wipiStreamFile(t, runtime, "big", wipiFileWrite)
	for _, write := range []struct {
		descriptor string
		arguments  []jvm.Value
	}{
		{"([B)I", []jvm.Value{ruleBytes(1)}},
		{"([BII)I", []jvm.Value{ruleBytes(1), jvm.IntValue(0), jvm.IntValue(1)}},
		{"(I)I", []jvm.Value{jvm.IntValue(1)}},
	} {
		if _, err := runtime.VM.InvokeVirtual(file, "write", write.descriptor, write.arguments...); !wipiIOException(runtime, err) {
			t.Fatalf("File.write%s past 4 MiB: %v", write.descriptor, err)
		}
	}
	output := wipiOpenStream(t, runtime, file, "openOutputStream", jvm.OutputStreamClass)
	if _, err := runtime.VM.InvokeVirtual(output, "write", "(I)V", jvm.IntValue(1)); !wipiIOException(runtime, err) {
		t.Fatalf("an output stream's write past 4 MiB: %v", err)
	}
	wipiStreamCall(t, runtime, file, "seek", "(I)V", jvm.IntValue(0))
	if written := storeReviewInt(t, wipiStreamCall(t, runtime, file, "write", "([B)I", ruleBytes(7))); written != 1 {
		t.Fatalf("a write within the bound = %d", written)
	}
	if ruleSize(t, runtime, "big") != wipiFileMaxBytes {
		t.Fatal("a refused write changed the file")
	}
	ruleXWrite(t, runtime, xfile, 2)
	if ruleSize(t, runtime, "big") != wipiFileMaxBytes+1 {
		t.Fatal("XFile no longer writes past the WIPI bound")
	}
}

// The members with nothing of the contract's but their shape: a C string in
// the handset's charset, and File.write(int), which answers the byte it wrote
// or the specification's IOException.
func TestWIPIFileSystemSmallMembersAndFileWriteByte(t *testing.T) {
	runtime := wipiStreamRuntime(t, backend.NewDirectorySaveStore(t.TempDir()))
	value, err := runtime.VM.InvokeStatic(wipi.FileSystemClass, "toCString", "("+wipiPathText+")[B", ruleText(runtime, "A가"))
	if err != nil {
		t.Fatal(err)
	}
	array, _ := value.Reference()
	if data, _ := jvm.ByteArraySnapshot(array); !bytes.Equal(data, []byte{'A', 0xb0, 0xa1, 0}) {
		t.Fatalf("toCString(A가) = % x, want EUC-KR and a NUL", data)
	}
	if _, err := runtime.VM.InvokeStatic(wipi.FileSystemClass, "toCString", "("+wipiPathText+")[B", jvm.ReferenceValue(nil)); !runtime.VM.IsGuestException(err, "java/lang/NullPointerException") {
		t.Fatalf("toCString(null): %v", err)
	}

	file := wipiStreamFile(t, runtime, "bytes", wipiFileReadWrite)
	for _, b := range []int32{0x41, 0x1ff} {
		if written := storeReviewInt(t, wipiStreamCall(t, runtime, file, "write", "(I)I", jvm.IntValue(b))); written != 1 {
			t.Fatalf("write(%#x) = %d, want 1", b, written)
		}
	}
	wipiStreamCall(t, runtime, file, "close", "()V")
	if _, err := runtime.VM.InvokeVirtual(file, "write", "(I)I", jvm.IntValue(1)); !wipiIOException(runtime, err) {
		t.Fatalf("write(int) on a closed File: %v", err)
	}
	reader := wipiStreamFile(t, runtime, "bytes", wipiFileReadOnly)
	if _, err := runtime.VM.InvokeVirtual(reader, "write", "(I)I", jvm.IntValue(1)); !wipiIOException(runtime, err) {
		t.Fatalf("write(int) on a read-only File: %v", err)
	}
	if got := ruleXRead(t, runtime, reader, 4); !bytes.Equal(got, []byte{0x41, 0xff}) {
		t.Fatalf("written bytes read back %v", got)
	}
}

package lgt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The C half of the common storage contract, docs/wipi-storage.md, row by row:
// names, what a directory is and what each file slot answers about one, the
// listing's form, the mode, removal, the room a title has and the bound on one
// file. Each test drives the slots the way a Clet does.

// contractFiles drives the C file slots over one client.
type contractFiles struct {
	t      *testing.T
	client *Client
	*guestFiles
}

func newContractFiles(t *testing.T, client *Client) *contractFiles {
	t.Helper()
	return &contractFiles{t: t, client: client, guestFiles: newGuestFiles(t, client)}
}

// code calls a slot whose first argument is a name.
func (files *contractFiles) code(slot uint32, name string, arguments ...uint32) int32 {
	files.t.Helper()
	return int32(callSlot(files.t, files.client, slot, append([]uint32{files.name(name)}, arguments...)...))
}

// save writes a whole file through the slots, from a guest buffer of its own
// size, and closes it.
func (files *contractFiles) save(name, data string) {
	files.t.Helper()
	handle := files.open(name, fileOpenWriteTruncate)
	if data != "" {
		buffer, err := files.client.allocateBytes([]byte(data))
		if err != nil {
			files.t.Fatal(err)
		}
		if written := int32(callSlot(files.t, files.client, slotFsWrite, handle, buffer, uint32(len(data)))); written != int32(len(data)) {
			files.t.Fatalf("write of %d bytes to %q answered %d", len(data), name, written)
		}
	}
	if code := int32(callSlot(files.t, files.client, slotFsClose, handle)); code != wipiSuccess {
		files.t.Fatalf("close %q answered %d", name, code)
	}
}

// list runs MC_fsList into a buffer of the given size that starts out holding
// 0xee everywhere, and answers the code and what the buffer holds after.
func (files *contractFiles) list(name string, size uint32) (int32, []byte) {
	files.t.Helper()
	buffer, err := files.client.allocateBytes(bytes.Repeat([]byte{0xee}, int(size)+8))
	if err != nil {
		files.t.Fatal(err)
	}
	code := files.code(slotFsList, name, buffer, size, 1)
	held := make([]byte, size+8)
	if err := files.client.core.Memory().Read(buffer, held); err != nil {
		files.t.Fatal(err)
	}
	return code, held
}

// attribute runs MC_fsFileAttribute and answers the code and the three words.
func (files *contractFiles) attribute(name string) (int32, [3]uint32) {
	files.t.Helper()
	info, err := files.client.allocateBytes(bytes.Repeat([]byte{0xee}, 12))
	if err != nil {
		files.t.Fatal(err)
	}
	code := files.code(slotFsFileAttribute, name, info, 1)
	var words [3]uint32
	for index := range words {
		words[index], err = files.client.readWord(info + uint32(index)*4)
		if err != nil {
			files.t.Fatal(err)
		}
	}
	return code, words
}

// Names and areas: a backslash separates segments as a slash does, and leading
// separators, empty and "." segments are dropped, so every spelling of a path
// is one file, saved or packaged; "", "/" and "." are the root; a ".." segment
// and a name the store cannot keep are M_E_BADFILENAME; 255 bytes of the path
// is the longest name and a longer one is M_E_LONGNAME — to every C call that
// takes a name.
func TestStorageContractCNames(t *testing.T) {
	client, _, _ := reviewFiles(t, nil)
	files := newContractFiles(t, client)
	files.save("/save.dat", "one")
	for _, spelling := range []string{"save.dat", "./save.dat", "//save.dat", "/./save.dat", "\\save.dat", ".\\save.dat"} {
		if code, words := files.attribute(spelling); code != wipiSuccess || words[2] != 3 {
			t.Errorf("%q is not the file /save.dat wrote: %d, %v", spelling, code, words)
		}
	}
	// A file the archive ships is one file under every spelling too.
	for _, spelling := range []string{"/data/hello.txt", "//data/hello.txt", "./data//hello.txt", "data\\hello.txt", "\\data\\hello.txt"} {
		if code, words := files.attribute(spelling); code != wipiSuccess || words[2] != uint32(len("packaged")) {
			t.Errorf("%q is not the packaged data/hello.txt: %d, %v", spelling, code, words)
		}
	}
	files.save("data\\made.dat", "two")
	if code, words := files.attribute("data/made.dat"); code != wipiSuccess || words[2] != 3 {
		t.Errorf("data\\made.dat is not data/made.dat: %d, %v", code, words)
	}
	for _, root := range []string{"", "/", ".", "./", "//"} {
		if code := files.code(slotFsIsExist, root, 1); code != wipiSuccess {
			t.Errorf("MC_fsIsExist(%q) = %d, want the root to exist", root, code)
		}
		if code, words := files.attribute(root); code != wipiSuccess || words[0] != fileAttributeDirectory || words[2] != 0 {
			t.Errorf("MC_fsFileAttribute(%q) = %d, %v, want a directory", root, code, words)
		}
	}
	longest := strings.Repeat("n", maxFileNameBytes)
	if code := files.code(slotFsOpen, longest, fileOpenReadWrite); code < 0 {
		t.Errorf("a %d-byte name was refused: %d", maxFileNameBytes, code)
	}
	// The bound counts the path the name spells, not the separators and "."
	// segments that spell it, and a C name's own bytes: two to a KS X 1001
	// syllable.
	for _, spelling := range []string{"/" + longest, "\\" + longest, strings.Repeat("./", 200) + longest} {
		if code, _ := files.attribute(spelling); code != wipiSuccess {
			t.Errorf("a %d-byte spelling of the %d-byte name answered %d", len(spelling), maxFileNameBytes, code)
		}
	}
	syllables := strings.Repeat("\xb0\xa1", maxFileNameBytes/2)
	if code := files.code(slotFsIsExist, syllables+"n", 1); code != wipiNoEntry {
		t.Errorf("a %d-byte name in the handset's encoding answered %d", len(syllables)+1, code)
	}
	if code := files.code(slotFsIsExist, syllables+"\xb0\xa1", 1); code != wipiLongName {
		t.Errorf("a %d-byte name in the handset's encoding answered %d", len(syllables)+2, code)
	}
	over, bad := longest+"n", "../outside.dat"
	if code := files.code(slotFsIsExist, "//"+over, 1); code != wipiLongName {
		t.Errorf("a %d-byte path spelled with leading separators answered %d", len(over), code)
	}
	calls := map[string]func(name string) int32{
		"MC_fsOpen":          func(name string) int32 { return files.code(slotFsOpen, name, fileOpenReadWrite) },
		"MC_fsIsExist":       func(name string) int32 { return files.code(slotFsIsExist, name, 1) },
		"MC_fsFileAttribute": func(name string) int32 { code, _ := files.attribute(name); return code },
		"MC_fsRemove":        func(name string) int32 { return files.code(slotFsRemove, name, 1) },
		"MC_fsRename from":   func(name string) int32 { return files.code(slotFsRename, name, files.name("x.dat"), 1) },
		"MC_fsRename to":     func(name string) int32 { return files.code(slotFsRename, "save.dat", files.name(name), 1) },
		"MC_fsMkDir":         func(name string) int32 { return files.code(slotFsMkDir, name, 1) },
		"MC_fsRmDir":         func(name string) int32 { return files.code(slotFsRmDir, name, 1) },
		"MC_fsList":          func(name string) int32 { code, _ := files.list(name, 64); return code },
		"MC_fsSetMode":       func(name string) int32 { return files.code(slotFsSetMode, name, fileModeReadWrite, 1) },
		"MC_fsGetCounts":     func(name string) int32 { return files.code(slotFsGetCounts, name, 1) },
	}
	for call, run := range calls {
		if code := run(over); code != wipiLongName {
			t.Errorf("%s of a %d-byte name = %d, want M_E_LONGNAME", call, len(over), code)
		}
		if code := run(bad); code != wipiBadFileName {
			t.Errorf("%s of %q = %d, want M_E_BADFILENAME", call, bad, code)
		}
	}
	if code, _ := files.attribute("save.dat"); code != wipiSuccess {
		t.Fatal("a refused call changed the file")
	}
}

// Make a directory: 0, and M_E_EXIST for a name that is a file or a directory
// — one made, one a file lies below, and the root. A parent nobody made is
// not asked for.
func TestStorageContractCMakeDirectory(t *testing.T) {
	client, _, _ := reviewFiles(t, nil)
	files := newContractFiles(t, client)
	files.save("save.dat", "x")
	if code := files.code(slotFsMkDir, "saves", 1); code != wipiSuccess {
		t.Fatalf("MC_fsMkDir(saves) = %d", code)
	}
	if code := files.code(slotFsMkDir, "deep/below/nothing", 1); code != wipiSuccess {
		t.Fatalf("MC_fsMkDir below a parent nobody made = %d", code)
	}
	for _, name := range []string{"saves", "save.dat", "data", "deep", "", "/"} {
		if code := files.code(slotFsMkDir, name, 1); code != wipiExists {
			t.Errorf("MC_fsMkDir(%q) = %d, want M_E_EXIST", name, code)
		}
	}
}

// Remove a directory: 0 for an empty one that was made; M_E_NOTEMPTY with
// anything below it, a file or a made directory; M_E_NOENT for a name that is
// not a directory, a file's included; M_E_BADFILENAME for the root.
func TestStorageContractCRemoveDirectory(t *testing.T) {
	client, _, _ := reviewFiles(t, nil)
	files := newContractFiles(t, client)
	files.save("full/slot.dat", "x")
	files.save("save.dat", "x")
	files.code(slotFsMkDir, "outer", 1)
	files.code(slotFsMkDir, "outer/inner", 1)
	files.code(slotFsMkDir, "empty", 1)
	for name, want := range map[string]int32{
		"full":     wipiNotEmpty,
		"outer":    wipiNotEmpty,
		"data":     wipiNotEmpty, // the archive's files lie below it
		"absent":   wipiNoEntry,
		"save.dat": wipiNoEntry,
		"":         wipiBadFileName,
		"/":        wipiBadFileName,
		".":        wipiBadFileName,
	} {
		if code := files.code(slotFsRmDir, name, 1); code != want {
			t.Errorf("MC_fsRmDir(%q) = %d, want %d", name, code, want)
		}
	}
	for _, name := range []string{"empty", "outer/inner", "outer"} {
		if code := files.code(slotFsRmDir, name, 1); code != wipiSuccess {
			t.Fatalf("MC_fsRmDir(%q) = %d", name, code)
		}
		if code := files.code(slotFsIsExist, name, 1); code != wipiNoEntry {
			t.Errorf("after MC_fsRmDir(%q) it still exists: %d", name, code)
		}
	}
	if code := files.code(slotFsRmDir, "empty", 1); code != wipiNoEntry {
		t.Errorf("removing a directory twice = %d, want M_E_NOENT", code)
	}
}

// Exists, and kind: MC_fsIsExist is 0 for a file or a directory and M_E_NOENT
// otherwise; MC_fsFileAttribute is 0 with the size for a file and with
// MC_FILE_IS_DIR and size 0 for a directory, creation time 0 for both, and
// M_E_NOENT for a missing name. A name that is a file is not a directory.
func TestStorageContractCExistsAndKind(t *testing.T) {
	client, _, _ := reviewFiles(t, nil)
	files := newContractFiles(t, client)
	files.save("save.dat", "twelve bytes")
	files.save("below/slot.dat", "x")
	files.code(slotFsMkDir, "made", 1)
	for name, want := range map[string][3]uint32{
		"save.dat":       {0, 0, 12},
		"data/hello.txt": {0, 0, uint32(len("packaged"))},
		"made":           {fileAttributeDirectory, 0, 0},
		"below":          {fileAttributeDirectory, 0, 0},
		"data":           {fileAttributeDirectory, 0, 0},
	} {
		if code := files.code(slotFsIsExist, name, 1); code != wipiSuccess {
			t.Errorf("MC_fsIsExist(%q) = %d", name, code)
		}
		if code, words := files.attribute(name); code != wipiSuccess || words != want {
			t.Errorf("MC_fsFileAttribute(%q) = %d, %v, want %v", name, code, words, want)
		}
	}
	for _, name := range []string{"absent", "save.dat/inside", "made/nothing"} {
		if code := files.code(slotFsIsExist, name, 1); code != wipiNoEntry {
			t.Errorf("MC_fsIsExist(%q) = %d, want M_E_NOENT", name, code)
		}
		if code, _ := files.attribute(name); code != wipiNoEntry {
			t.Errorf("MC_fsFileAttribute(%q) = %d, want M_E_NOENT", name, code)
		}
	}
	// A directory made under a name that becomes a file is a file from then on.
	files.code(slotFsMkDir, "turned", 1)
	files.save("turned", "now a file")
	if code, words := files.attribute("turned"); code != wipiSuccess || words[0] != 0 || words[2] != 10 {
		t.Errorf("a name that is a file answers %d, %v", code, words)
	}
}

// Entries: the names directly below, files and directories alike, each once,
// in name order, "a\0b\0\0"; an empty or missing directory "\0\0"; a short
// buffer M_E_SHORTBUF with nothing written. MC_fsGetCounts counts the same
// names.
func TestStorageContractCListAndCount(t *testing.T) {
	client, _, _ := reviewFiles(t, nil)
	files := newContractFiles(t, client)
	files.save("dir/b.dat", "x")
	files.save("dir/a.dat", "x")
	files.save("dir/sub/deep.dat", "x")
	files.code(slotFsMkDir, "dir\\made", 1) // a backslash separates as a slash does
	files.code(slotFsMkDir, "dir/sub", 1)   // already one: refused, and listed once either way
	files.code(slotFsMkDir, "hollow", 1)
	for name, want := range map[string]string{
		"dir":       "a.dat\x00b.dat\x00made\x00sub\x00\x00",
		"/dir/":     "a.dat\x00b.dat\x00made\x00sub\x00\x00",
		"\\dir\\":   "a.dat\x00b.dat\x00made\x00sub\x00\x00",
		"dir/sub":   "deep.dat\x00\x00",
		"dir\\sub":  "deep.dat\x00\x00",
		"hollow":    "\x00\x00",
		"absent":    "\x00\x00",
		"dir/a.dat": "\x00\x00", // a file holds no entries
		"data":      "hello.txt\x00other.txt\x00\x00",
	} {
		code, held := files.list(name, 64)
		if code != wipiSuccess || string(held[:len(want)]) != want {
			t.Errorf("MC_fsList(%q) = %d, %q, want %q", name, code, held[:len(want)], want)
		}
		if held[len(want)] != 0xee {
			t.Errorf("MC_fsList(%q) wrote past its list", name)
		}
		entries := int32(strings.Count(strings.TrimSuffix(want, "\x00\x00"), "\x00"))
		if want != "\x00\x00" {
			entries++
		}
		if count := files.code(slotFsGetCounts, name, 1); count != entries {
			t.Errorf("MC_fsGetCounts(%q) = %d, want %d", name, count, entries)
		}
	}
	root, held := files.list("", 128)
	if root != wipiSuccess || !bytes.HasPrefix(held, []byte("data\x00dir\x00hollow\x00\x00")) {
		t.Errorf("MC_fsList of the root = %d, %q", root, held)
	}
	code, held := files.list("dir", 8)
	if code != wipiShortBuffer || !bytes.Equal(held, bytes.Repeat([]byte{0xee}, 16)) {
		t.Errorf("a short buffer answered %d and holds %q", code, held)
	}
	if code := files.code(slotFsList, "dir", 0, 64, 1); code != wipiShortBuffer {
		t.Errorf("a null buffer answered %d", code)
	}
}

// Mode: 1 to 3 and 0x10, 0x20, 0x30 are accepted for a file or a directory and
// kept nowhere; another mode is M_E_INVALID; a missing name M_E_NOENT.
func TestStorageContractCSetMode(t *testing.T) {
	client, store, _ := reviewFiles(t, nil)
	files := newContractFiles(t, client)
	files.save("save.dat", "x")
	files.code(slotFsMkDir, "made", 1)
	for _, name := range []string{"save.dat", "data/hello.txt", "made", "data", ""} {
		for _, mode := range []uint32{1, 2, 3, 0x10, 0x20, 0x30} {
			if code := files.code(slotFsSetMode, name, mode, 1); code != wipiSuccess {
				t.Errorf("MC_fsSetMode(%q, %#x) = %d", name, mode, code)
			}
		}
		for _, mode := range []uint32{0, 4, 8, 0x11, 0x40, 0xffffffff} {
			if code := files.code(slotFsSetMode, name, mode, 1); code != wipiInvalid {
				t.Errorf("MC_fsSetMode(%q, %#x) = %d, want M_E_INVALID", name, mode, code)
			}
		}
	}
	if code := files.code(slotFsSetMode, "absent", 1, 1); code != wipiNoEntry {
		t.Errorf("MC_fsSetMode of a missing name = %d", code)
	}
	writes := store.writes
	files.code(slotFsSetMode, "save.dat", 1, 1)
	if store.writes != writes {
		t.Fatal("a mode reached the store")
	}
	// Read-only is kept nowhere: the file is still written.
	files.save("save.dat", "still written")
	if got := store.held("fs/save.dat"); got != "still written" {
		t.Fatalf("after a read-only mode the store holds %q", got)
	}
}

// Remove a file: 0, and M_E_NOENT for a name that is not a file — missing, a
// directory, the root — which changes nothing.
func TestStorageContractCRemove(t *testing.T) {
	client, _, _ := reviewFiles(t, nil)
	files := newContractFiles(t, client)
	files.save("save.dat", "x")
	files.code(slotFsMkDir, "made", 1)
	for _, name := range []string{"absent", "made", "data", "", "/"} {
		if code := files.code(slotFsRemove, name, 1); code != wipiNoEntry {
			t.Errorf("MC_fsRemove(%q) = %d, want M_E_NOENT", name, code)
		}
	}
	if code := files.code(slotFsIsExist, "made", 1); code != wipiSuccess {
		t.Fatal("a refused removal took the directory")
	}
	for _, name := range []string{"save.dat", "data/hello.txt"} {
		if code := files.code(slotFsRemove, name, 1); code != wipiSuccess {
			t.Errorf("MC_fsRemove(%q) = %d", name, code)
		}
		if code := files.code(slotFsRemove, name, 1); code != wipiNoEntry {
			t.Errorf("removing %q twice = %d, want M_E_NOENT", name, code)
		}
	}
}

// A removal the store refuses does not happen: MC_fsRemove answers M_E_ERROR,
// the file is still there in the session and in the next one, and nothing
// waits to remove it later.
func TestStorageContractCRemoveRefused(t *testing.T) {
	client, store, _ := reviewFiles(t, nil)
	files := newContractFiles(t, client)
	files.save("save.dat", "kept")
	store.refuse = true
	if code := files.code(slotFsRemove, "save.dat", 1); code != wipiError {
		t.Fatalf("a removal the store refused answered %d, want M_E_ERROR", code)
	}
	if code := files.code(slotFsIsExist, "save.dat", 1); code != wipiSuccess {
		t.Fatal("a refused removal hid the file")
	}
	store.refuse = false
	client.flushOpenFiles()
	if client.removedUnsaved || strings.Contains(store.held(fileRemovedKey), "save.dat") {
		t.Fatalf("a refused removal waited to happen: %q", store.held(fileRemovedKey))
	}
	next := fixtureClient(t)
	next.saveStore = store
	if data, ok := next.readFile("save.dat"); !ok || string(data) != "kept" {
		t.Fatalf("the next session reads %q (%v)", data, ok)
	}
}

// Space: every title has 1 MiB, and the room left is that less what it has
// stored — a file's size less its archive's file of the same name, a
// database's record bytes, not the platform's own lists — never below zero,
// and nothing is refused to keep it true.
func TestStorageContractCSpace(t *testing.T) {
	client, _, _ := reviewFiles(t, nil)
	files := newContractFiles(t, client)
	quota := uint32(1 << 20)
	available := func() int32 { return int32(callSlot(t, client, slotFsAvailable)) }
	if total := callSlot(t, client, slotFsTotalSpace); total != quota {
		t.Fatalf("MC_fsTotalSpace = %d, want 1 MiB", total)
	}
	if got := available(); got != int32(quota) {
		t.Fatalf("an untouched title has %d available", got)
	}
	files.save("save.dat", strings.Repeat("s", 1000))
	files.code(slotFsMkDir, "made", 1) // a list of the platform's: not counted
	files.code(slotFsRemove, "absent", 1)
	if got := available(); got != int32(quota-1000) {
		t.Fatalf("after a 1000-byte save %d is available, want %d", got, quota-1000)
	}
	// A file rewritten over the archive's counts only what it adds.
	files.save("data/hello.txt", strings.Repeat("h", len("packaged")+100))
	if got := available(); got != int32(quota-1100) {
		t.Fatalf("after a packaged file grew by 100, %d is available, want %d", got, quota-1100)
	}
	files.save("data/other.txt", "s")
	if got := available(); got != int32(quota-1100) {
		t.Fatalf("a packaged file written shorter changed the room to %d", got)
	}
	// A file written and removed is no longer counted.
	files.code(slotFsRemove, "save.dat", 1)
	if got := available(); got != int32(quota-100) {
		t.Fatalf("after the save was removed %d is available, want %d", got, quota-100)
	}
	// Past the room: never below zero, and the write is not refused.
	for index := range 5 {
		files.save("big"+string(rune('a'+index))+".dat", strings.Repeat("b", 300<<10))
	}
	if got := available(); got != 0 {
		t.Fatalf("past the room %d is available, want 0", got)
	}
	if got := callSlot(t, client, slotFsTotalSpace); got != quota {
		t.Fatalf("MC_fsTotalSpace moved to %d", got)
	}
}

// The archive's file of the same name is found under any spelling of it: a
// packaged file rewritten as "//data\hello.txt" counts only what it adds,
// while the write waits in its handle and once it is stored.
func TestStorageContractCSpaceSpellings(t *testing.T) {
	client, _, _ := reviewFiles(t, nil)
	files := newContractFiles(t, client)
	quota := int32(1 << 20)
	handle := files.open("//data\\hello.txt", fileOpenWriteTruncate)
	data := strings.Repeat("h", len("packaged")+100)
	buffer, err := client.allocateBytes([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	callSlot(t, client, slotFsWrite, handle, buffer, uint32(len(data)))
	if got := int32(callSlot(t, client, slotFsAvailable)); got != quota-100 {
		t.Fatalf("while the write waits %d is available, want %d", got, quota-100)
	}
	callSlot(t, client, slotFsClose, handle)
	if got := int32(callSlot(t, client, slotFsAvailable)); got != quota-100 {
		t.Fatalf("once it is stored %d is available, want %d", got, quota-100)
	}
}

// One file holds at most 4 MiB: a write that would pass it fails with
// M_E_NOSPACE and leaves the file as it was.
func TestStorageContractCFileBound(t *testing.T) {
	client, _, _ := reviewFiles(t, nil)
	files := newContractFiles(t, client)
	handle := files.open("big.dat", fileOpenReadWrite)
	if maxFileBytes != 4<<20 {
		t.Fatalf("the bound is %d bytes", maxFileBytes)
	}
	if moved := int32(callSlot(t, client, slotFsSeek, handle, maxFileBytes-2, 0)); moved != maxFileBytes-2 {
		t.Fatalf("seek answered %d", moved)
	}
	if written := int32(callSlot(t, client, slotFsWrite, handle, files.buffer, 2)); written != 2 {
		t.Fatalf("a write up to the bound answered %d", written)
	}
	if written := int32(callSlot(t, client, slotFsWrite, handle, files.buffer, 1)); written != wipiNoSpace {
		t.Fatalf("a write past the bound answered %d, want M_E_NOSPACE", written)
	}
	if size := len(client.files[handle].data); size != maxFileBytes {
		t.Fatalf("the refused write left %d bytes", size)
	}
}

// What the contract keeps across a restart: a directory made and left empty,
// one removed, a file, and a removal, read by a second client over the same
// directory store.
func TestStorageContractCPersistsAcrossSessions(t *testing.T) {
	root := t.TempDir()
	first := fixtureClient(t)
	first.saveStore = backend.NewDirectorySaveStore(root)
	files := newContractFiles(t, first)
	files.code(slotFsMkDir, "kept", 1)
	files.code(slotFsMkDir, "kept/inner", 1)
	files.code(slotFsMkDir, "gone", 1)
	files.code(slotFsRmDir, "gone", 1)
	files.save("kept/slot.dat", "saved")
	files.code(slotFsRemove, "data/hello.txt", 1)

	second := fixtureClient(t)
	second.saveStore = backend.NewDirectorySaveStore(root)
	next := newContractFiles(t, second)
	for name, want := range map[string]int32{"kept": wipiSuccess, "kept/inner": wipiSuccess, "gone": wipiNoEntry,
		"kept/slot.dat": wipiSuccess, "data/hello.txt": wipiNoEntry} {
		if code := next.code(slotFsIsExist, name, 1); code != want {
			t.Errorf("after a restart MC_fsIsExist(%q) = %d, want %d", name, code, want)
		}
	}
	if code, held := next.list("kept", 64); code != wipiSuccess || !bytes.HasPrefix(held, []byte("inner\x00slot.dat\x00\x00")) {
		t.Errorf("after a restart MC_fsList(kept) = %d, %q", code, held)
	}
	if code := next.code(slotFsMkDir, "kept/inner", 1); code != wipiExists {
		t.Errorf("after a restart a made directory can be made again: %d", code)
	}
	if code := next.code(slotFsRmDir, "kept/inner", 1); code != wipiSuccess {
		t.Fatalf("after a restart MC_fsRmDir(kept/inner) = %d", code)
	}
	third := fixtureClient(t)
	third.saveStore = backend.NewDirectorySaveStore(root)
	if code := newContractFiles(t, third).code(slotFsIsExist, "kept/inner", 1); code != wipiNoEntry {
		t.Errorf("a removal of a directory did not persist: %d", code)
	}
	// The list is the platform's: no title path reaches it.
	if code := next.code(slotFsOpen, ".directories", fileOpenReadOnly); code != wipiBadFileName {
		t.Errorf("the directory list opened as a title's file: %d", code)
	}
}

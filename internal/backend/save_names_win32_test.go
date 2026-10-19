package backend

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// On Windows an earlier build stored a key holding bytes that are not UTF-8 —
// an LGT or KTF C title's EUC-KR file name — under the name Go's UTF-16 made
// of it, U+FFFD for each such byte, and the same key opened that file again on
// every run. This build writes those bytes by their number, and nothing looked
// under the Win32 spelling, so the title started over on its first run here
// with its save left beside it. The file is read where it is until the key's
// own first write, which goes to the key's encoded name, and it stays the save
// of every other key Win32 spelled the same: "가나" in EUC-KR is four such
// bytes too, and shared the file there.
func TestAWindowsNameMadeOfANonUTF8KeyIsRead(t *testing.T) {
	win32 := saveNamesWin32
	saveNamesWin32 = true
	t.Cleanup(func() { saveNamesWin32 = win32 })
	key, other := "fs/\xbc\xbc\xc0\xcc.dat", "fs/\xb0\xa1\xb3\xaa.dat"
	for _, record := range []bool{false, true} {
		parent := t.TempDir()
		state := []string{"owner/fs/\uFFFD\uFFFD\uFFFD\uFFFD.dat=written on Windows"}
		if record {
			state = append(state, encodedRecord)
		}
		leftoverBuild(t, parent, state)
		store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
		wantSaves(t, store, map[string]string{key: "written on Windows", other: "written on Windows"})
		if err := store.StoreSave(key, []byte("its own")); err != nil {
			t.Fatal(err)
		}
		wantSaves(t, store, map[string]string{key: "its own", other: "written on Windows"})
	}
}

// Win32's spelling of a segment is the one Go's UTF-16 conversion and Win32's
// own trimming make, as a listing reads it back: U+FFFD for each byte that is
// not UTF-8, three bytes that encode a lone surrogate kept as they are, two
// that make a pair the one character they encode, and no dots or spaces at
// the end of the last segment. Any other segment loses a single dot at its
// end, and one Win32 would have left with a dot or a space there names a
// directory an earlier build never made.
func TestWin32SpellsASegmentAsGoAndWin32Do(t *testing.T) {
	for _, spelling := range []struct {
		segment string
		last    bool
		want    string
	}{
		{"save", true, "save"},
		{"save. ", true, "save"},
		{"\xbc\xbc.", true, "\uFFFD\uFFFD"},
		{"가\xff나", true, "가\uFFFD나"},
		{"\xed\xa0\x80x", true, "\xed\xa0\x80x"},
		{"\xed\xa0", true, "\uFFFD\uFFFD"},
		{"\xc7\xd1\xb1\xdb", true, "\uFFFD\u0471\uFFFD"},
		{"\uFFFD", true, "\uFFFD"},
		{"...", true, ""},
		// The EUC-KR "立고가": a high and a low surrogate side by side.
		{"\xed\xa1\xb0\xed\xb0\xa1.dat", true, "\U0002C021.dat"},
		// A low one before a high one is no pair, and neither is a high one
		// before a character outside the BMP.
		{"\xed\xb0\xa1\xed\xa1\xb0", true, "\xed\xb0\xa1\xed\xa1\xb0"},
		{"\xed\xa1\xb0\U0002C021", true, "\xed\xa1\xb0\U0002C021"},
		{"dir", false, "dir"},
		{"dir.", false, "dir"},
		{"d.ir.", false, "d.ir"},
		{"\xbc\xbc.", false, "\uFFFD\uFFFD"},
		{"dir..", false, ""},
		{"dir ", false, ""},
		{"dir. ", false, ""},
		{"dir .", false, ""},
	} {
		if got := win32SaveName(spelling.segment, spelling.last); got != spelling.want {
			t.Errorf("win32SaveName(%+q, %t) = %+q, want %+q", spelling.segment, spelling.last, got, spelling.want)
		}
	}
}

// Two bytes of an EUC-KR name can encode a high surrogate and the next two a
// low one, and Go hands Win32 those two as the pair they make: an earlier
// build stored "立고가.dat" as one character and ".dat", and the name was
// looked for with the six bytes as they are, which is not how a listing reads
// it. It is found under the character.
func TestAWindowsNameOfAPairOfSurrogatesIsRead(t *testing.T) {
	win32 := saveNamesWin32
	saveNamesWin32 = true
	t.Cleanup(func() { saveNamesWin32 = win32 })
	key := "fs/\xed\xa1\xb0\xed\xb0\xa1.dat"
	for _, record := range []bool{false, true} {
		parent := t.TempDir()
		state := []string{"owner/fs/\U0002C021.dat=written on Windows"}
		if record {
			state = append(state, encodedRecord)
		}
		leftoverBuild(t, parent, state)
		wantSaves(t, NewDirectorySaveStore(filepath.Join(parent, "owner")), map[string]string{key: "written on Windows"})
	}
}

// Win32 drops only a single dot from the end of a directory segment, and every
// dot and space from the end of the last one, the name a directory is made
// under. An earlier build stored "dir./save" in "dir", the same file as
// "dir/save", but no key whose directory ends in a space or in two dots: there
// was no such directory to write it in. Read as "dir/save", those keys
// answered another key's save as their own.
func TestAWindowsNameIsReadOnlyForKeysWin32CouldStore(t *testing.T) {
	win32 := saveNamesWin32
	saveNamesWin32 = true
	t.Cleanup(func() { saveNamesWin32 = win32 })
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{"owner/dir/save=dir's save"})
	store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
	wantSaves(t, store, map[string]string{"dir/save": "dir's save", "dir./save": "dir's save", "dir/save.": "dir's save"})
	for _, key := range []string{"dir /save", "dir../save", "dir. /save", "dir ./save"} {
		if data, found, err := store.ReadSave(key); err != nil || found {
			t.Errorf("%q read %q (found %t, %v), want no save", key, data, found, err)
		}
	}
}

// A folder read as an earlier build left it — one holding a name that cannot
// move, or one that cannot be written — is read as that build read it, and on
// Windows that build read a non-UTF-8 key under the name Win32 spelled. Looked
// up only by the key's own bytes, the save was not there, and the title began
// again in a folder whose names it could not write either.
func TestAnEarlierFolderReadsAWindowsName(t *testing.T) {
	retrySaveNamesAtOnce(t)
	win32 := saveNamesWin32
	saveNamesWin32 = true
	t.Cleanup(func() { saveNamesWin32 = win32 })
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{"owner/fs/\uFFFD\uFFFD.dat=written on Windows", "owner/rms/a#Top=top"})
	failRenamesOf(t, "a#Top")
	wantSaves(t, NewDirectorySaveStore(filepath.Join(parent, "owner")), map[string]string{
		"fs/\xbc\xbc.dat": "written on Windows", "rms/a#Top": "top",
	})
}

// A restore settles the keys whose encoded names are too long before it writes
// anything, and it looked them up as a read does, which finds a file Win32
// spelled for an earlier build. That file is never written, so the key passed
// there and its own write refused it after the entries before it had been
// replaced: half a restore, answered as a failure. It is refused first.
func TestARestoreRefusesAKeyOnlyAWindowsNameHoldsBeforeWriting(t *testing.T) {
	win32 := saveNamesWin32
	saveNamesWin32 = true
	t.Cleanup(func() { saveNamesWin32 = win32 })
	key := "fs/" + strings.Repeat("\xbc", 64)
	if saveNamesFit(encodeSaveKey(key)) {
		t.Fatalf("the key's encoded name %q fits; the test needs one that does not", encodeSaveKey(key))
	}
	parent := t.TempDir()
	state := []string{"owner/a/first=kept", "owner/fs/" + strings.Repeat("\uFFFD", 64) + "=written on Windows"}
	leftoverBuild(t, parent, state)
	root := filepath.Join(parent, "owner")
	wantSaves(t, NewDirectorySaveStore(root), map[string]string{key: "written on Windows"})
	written, removed, err := WriteSaveTree(root, []SaveEntry{{Key: "a/first", Data: []byte("restored")}, {Key: key, Data: []byte("restored")}})
	if err == nil || written != 0 || removed != 0 {
		t.Fatalf("the restore wrote %d, removed %d, answered %v; want it refused before writing", written, removed, err)
	}
	leftoverExpect(t, parent, append(slices.Clone(state), leftoverReserved+"/lock="), "after the refused restore")
}

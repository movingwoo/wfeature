package backend

import (
	"path/filepath"
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
// own trimming make: U+FFFD for each byte that is not UTF-8, three bytes that
// encode a lone surrogate kept as they are, and no dots or spaces at the end.
func TestWin32SpellsASegmentAsGoAndWin32Do(t *testing.T) {
	for segment, want := range map[string]string{
		"save":             "save",
		"save. ":           "save",
		"\xbc\xbc.":        "\uFFFD\uFFFD",
		"가\xff나":           "가\uFFFD나",
		"\xed\xa0\x80x":    "\xed\xa0\x80x",
		"\xed\xa0":         "\uFFFD\uFFFD",
		"\xc7\xd1\xb1\xdb": "\uFFFD\u0471\uFFFD",
		"\uFFFD":           "\uFFFD",
		"...":              "",
	} {
		if got := win32SaveName(segment); got != want {
			t.Errorf("win32SaveName(%+q) = %+q, want %+q", segment, got, want)
		}
	}
}

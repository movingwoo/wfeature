package backend

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// What the review of the encoding's second form found: names a volume lists
// decomposed, a Windows key an earlier build stored without its ending, a
// name measured in bytes where the host counts characters, a name that reads
// as a segment no key has, a path longer than the host opens, a byte that is
// not UTF-8 and the shape of an NTFS short name.

// A Hangul or kana save whose name the volume lists decomposed — HFS+, exFAT
// and FAT on macOS do for every name, and a folder copied from one of them
// keeps it so — is the composed key's save: a conversion leaves it where it
// is, the key reads it, and a listing names the composed key. Read as listed,
// the conversion moved it to a name with its jamo numbered, and the key's own
// lookup found nothing.
func TestADecomposedListingIsTheComposedKey(t *testing.T) {
	if !normalizationInsensitive(t) {
		t.Skip("the volume keeps a decomposed name apart from its composed one")
	}
	for _, record := range []bool{false, true} {
		parent := t.TempDir()
		state := []string{
			"owner/fs/" + norm.NFD.String("가나") + "=hangul",
			"owner/fs/" + norm.NFD.String("がぎ") + "=kana",
			"owner/rms/" + norm.NFD.String("세이브") + "/" + norm.NFD.String("슬롯") + "=nested",
		}
		if record {
			state = append(state, encodedRecord)
		}
		leftoverBuild(t, parent, state)
		saves := map[string]string{"fs/가나": "hangul", "fs/がぎ": "kana", "rms/세이브/슬롯": "nested"}
		root := filepath.Join(parent, "owner")
		store := NewDirectorySaveStore(root)
		wantSaves(t, store, saves)
		listed, err := ReadSaveTree(root)
		if err != nil {
			t.Fatal(err)
		}
		wantEntries(t, listed, saves, "the listing")
		snapshot, err := store.SnapshotSaves()
		if err != nil {
			t.Fatal(err)
		}
		wantEntries(t, snapshot, saves, "the snapshot")
		for _, name := range []string{"fs", "rms"} {
			entries, err := os.ReadDir(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.Contains(entry.Name(), "#+") {
					t.Fatalf("the conversion numbered %+q", entry.Name())
				}
			}
		}
		if err := store.StoreSave("fs/가나", []byte("again")); err != nil {
			t.Fatal(err)
		}
		wantSaves(t, store, map[string]string{"fs/가나": "again"})
	}
}

// On a host that drops a '.' or ' ' at the end of a path segment, as Win32
// does, an earlier build stored "fs/save." as "fs/save". That file is read for
// the key until the key's own first write, which goes to the key's encoded
// name, and the file stays the save of "fs/save", which shared it.
func TestASaveWindowsStoredWithoutItsEndingIsRead(t *testing.T) {
	strip := saveNamesStripTrailing
	saveNamesStripTrailing = true
	t.Cleanup(func() { saveNamesStripTrailing = strip })
	for _, record := range []bool{false, true} {
		parent := t.TempDir()
		state := []string{"owner/fs/save=stored without its dot", "owner/jdb/SAVE=stored without its spaces"}
		if record {
			state = []string{encodedRecord, "owner/fs/save=stored without its dot", "owner/jdb/#S#A#V#E=stored without its spaces"}
		}
		leftoverBuild(t, parent, state)
		store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
		wantSaves(t, store, map[string]string{
			"fs/save.": "stored without its dot", "jdb/SAVE    ": "stored without its spaces",
			"fs/save": "stored without its dot",
		})
		if err := store.StoreSave("fs/save.", []byte("its own")); err != nil {
			t.Fatal(err)
		}
		wantSaves(t, store, map[string]string{"fs/save.": "its own", "fs/save": "stored without its dot"})
	}
}

// A segment is measured as the host measures a name: on macOS, iOS and
// Windows in UTF-16 units, 255 of which a name may hold. A save of 150 Hangul
// syllables, 450 bytes, is stored and read there, and so is one an earlier
// build stored under such a name; one of 256 characters is refused.
func TestASaveNameIsMeasuredAsTheHostMeasuresIt(t *testing.T) {
	if saveNameLength(strings.Repeat("가", 255)) != 255 || saveNameLength("\U0001F600") != 2 && saveNamesInUnits {
		t.Fatal("a name is not measured in UTF-16 units")
	}
	if !saveNamesInUnits {
		if saveNameLength(strings.Repeat("가", 85)) != 255 {
			t.Fatal("a name is not measured in bytes")
		}
		t.Skip("this host measures a name in bytes")
	}
	parent := t.TempDir()
	long, earlier := strings.Repeat("가", 150), strings.Repeat("나", 150)
	leftoverBuild(t, parent, []string{"owner/fs/" + earlier + "=earlier"})
	store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
	if err := store.StoreSave("fs/"+long, []byte("new")); err != nil {
		t.Fatalf("a 150-syllable name: %v", err)
	}
	wantSaves(t, store, map[string]string{"fs/" + long: "new", "fs/" + earlier: "earlier"})
	if err := store.StoreSave("fs/"+strings.Repeat("a", 256), []byte("x")); !errors.Is(err, errSaveNameTooLong) {
		t.Fatalf("a 256-character name: %v, want it refused as too long", err)
	}
}

// A name that reads as a segment no key holds — "." or "\\" written by its
// number — is not a key's encoded name. A file of that name in an encoded
// directory is an earlier build's key of its own, and a snapshot of the
// directory does not fail over it.
func TestANameForASegmentNoKeyHoldsIsNoEncodedName(t *testing.T) {
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{encodedRecord, "owner/fs/#+00002e=dot", "owner/fs/#+00005c=backslash", "owner/fs/kept=kept"})
	store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
	snapshot, err := store.SnapshotSaves()
	if err != nil {
		t.Fatalf("the snapshot: %v", err)
	}
	wantEntries(t, snapshot, map[string]string{"fs/#+00002e": "dot", "fs/#+00005c": "backslash", "fs/kept": "kept"}, "the snapshot")
}

// A key whose path, written out below the save directory, is longer than the
// host opens has no save: a read finds none rather than meeting the file
// system's error, and a write is refused up front with the limit named.
func TestASavePathLongerThanTheHostOpensIsNoSave(t *testing.T) {
	if maxSavePathBytes == 0 {
		t.Skip("this host opens a path of any length a key makes")
	}
	segment := "a" + strings.Repeat("́", 30)
	key := strings.TrimSuffix(strings.Repeat(segment+"/", 1+maxSavePathBytes/(len(encodeSaveName(segment))+1)), "/")
	if _, err := NormalizeSaveKey(key); err != nil {
		t.Fatalf("the probe key is no key: %v", err)
	}
	store := NewDirectorySaveStore(filepath.Join(t.TempDir(), "owner"))
	if err := store.StoreSave("fs/kept", []byte("kept")); err != nil {
		t.Fatal(err)
	}
	if data, found, err := store.ReadSave(key); err != nil || found {
		t.Fatalf("a read of a key longer than a path: %q, found %t, %v", data, found, err)
	}
	if err := store.StoreSave(key, []byte("x")); !errors.Is(err, errSaveNameTooLong) {
		t.Fatalf("a write of a key longer than a path: %v, want it refused as too long", err)
	}
	wantSaves(t, store, map[string]string{"fs/kept": "kept"})
}

// A key with a byte that is not UTF-8 — an LGT C Clet's EUC-KR file name — is
// stored with the byte by its number, so macOS, which refuses such a name,
// holds the save, and Windows does not make two keys one.
func TestAKeyThatIsNotUTF8IsStored(t *testing.T) {
	store := NewDirectorySaveStore(filepath.Join(t.TempDir(), "owner"))
	keys := map[string]string{"fs/\xbc\xbc\xc0\xcc.dat": "first", "fs/\xbc\xbd\xc0\xcc.dat": "second"}
	for key, data := range keys {
		if err := store.StoreSave(key, []byte(data)); err != nil {
			t.Fatalf("store %+q: %v", key, err)
		}
	}
	wantSaves(t, store, keys)
	snapshot, err := store.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, snapshot, keys, "the snapshot")
}

// A backup that carries a key that is not UTF-8, which an export on any host
// can hold, is restored whole on every host, macOS included, and lists the key
// again. macOS refused such a backup before anything was written, when the key
// would have been written as it is.
func TestABackupWithAKeyThatIsNotUTF8IsRestored(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner")
	store := NewDirectorySaveStore(root)
	if err := store.StoreSave("fs/0", []byte("old")); err != nil {
		t.Fatal(err)
	}
	saves := map[string]string{"fs/0": "new", "fs/\xbc\xbc.dat": "raw"}
	if _, _, err := WriteSaveTree(root, []SaveEntry{{Key: "fs/0", Data: []byte("new")}, {Key: "fs/\xbc\xbc.dat", Data: []byte("raw")}}); err != nil {
		t.Fatalf("the restore: %v", err)
	}
	wantSaves(t, store, saves)
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, saves, "the listing")
}

// A raw name with a byte that is not UTF-8, which an earlier build wrote on a
// host that keeps such names (Linux, Android), converts with the folder.
func TestAnEarlierNameThatIsNotUTF8Converts(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" || runtime.GOOS == "windows" {
		t.Skip("this host keeps no name that is not UTF-8")
	}
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{"owner/fs/\xbc\xbc.dat=earlier", "owner/fs/Save=upper"})
	store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
	wantSaves(t, store, map[string]string{"fs/\xbc\xbc.dat": "earlier", "fs/Save": "upper"})
	leftoverExpect(t, parent, []string{encodedRecord, leftoverReserved + "/lock=", "owner/fs/#-bc#-bc.dat=earlier", "owner/fs/#Save=upper"}, "after the conversion")
}

// A name the encoding wrote before it numbered a '~' before a digit reads as
// its key and moves to the name it has now.
func TestAnEarlierShortNameShapeMoves(t *testing.T) {
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{encodedRecord, "owner/fs/savefi~1.dat=earlier"})
	store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
	wantSaves(t, store, map[string]string{"fs/savefi~1.dat": "earlier"})
	leftoverExpect(t, parent, []string{encodedRecord, leftoverReserved + "/lock=", "owner/fs/savefi#+00007e1.dat=earlier"}, "after the read")
}

// The companion macOS writes beside a file on exFAT and FAT, "._" and the
// file's name in the AppleDouble format, is no save: a snapshot and an export
// leave it out, a conversion leaves it where it is, and a restore does not
// remove it. A file of that name that is not AppleDouble is a save.
func TestAnAppleDoubleCompanionIsNoSave(t *testing.T) {
	companion := "\x00\x05\x16\x07\x00\x02\x00\x00metadata"
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{
		"owner/fs/Upper=upper", "owner/fs/._Upper=" + companion,
		"owner/fs/plain=plain", "owner/fs/._plain=" + companion,
		"owner/fs/._mine=a save of its own",
	})
	root := filepath.Join(parent, "owner")
	saves := map[string]string{"fs/Upper": "upper", "fs/plain": "plain", "fs/._mine": "a save of its own"}
	store := NewDirectorySaveStore(root)
	wantSaves(t, store, saves)
	snapshot, err := store.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, snapshot, saves, "the snapshot")
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, saves, "the listing")
	if _, _, err := WriteSaveTree(root, listed); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"._Upper", "._plain"} {
		if _, err := os.Lstat(filepath.Join(root, "fs", name)); err != nil {
			t.Fatalf("the companion %s is gone: %v", name, err)
		}
	}
}

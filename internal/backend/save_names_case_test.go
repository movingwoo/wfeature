package backend

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// The record an encoded save directory holds, in the notation of
// leftoverBuild.
var encodedRecord = "owner/" + saveNamesRecord + "=" + saveNamesHeader + "\nencoded\n"

// The format is pinned here by hand rather than by calling the encoder: these
// are the names on a player's disk.
func TestSaveNamesSpellOutCase(t *testing.T) {
	for _, test := range []struct{ key, path string }{
		{"save", "save"},
		{"Save", "#Save"},
		{"SAVE", "#S#A#V#E"},
		{"sAvE", "s#Av#E"},
		{"rms/Option", "rms/#Option"},
		{"fs/Data/Slot1.sav", "fs/#Data/#Slot1.sav"},
		{"save#1", "save##1"},
		{"#", "##"},
		{"#Top", "###Top"},
		{"a##b", "a####b"},
		{"rms/.index", "rms/.index"},
		{"Ärger", "#Ärger"},
		{"ΣΩ", "#Σ#Ω"},
		{"한글", "한글"},
		// Any other letter with case outside ASCII is written by its number:
		// upper-case letters whose lower case does not lead back to them, a
		// title-case letter, and lower-case letters, which a file system that
		// ignores case may take for another name ("ß" for "ss").
		{"İ", "#+000130"},
		{"\u212A", "#+00212a"},
		{"ǅ", "#+0001c5"},
		{"ß", "#+0000df"},
		{"ς", "#+0003c2"},
		{"café", "caf#+0000e9"},
		{"Été", "#Ét#+0000e9"},
		// Bytes that are not UTF-8 are kept.
		{"\xffA", "\xff#A"},
	} {
		if got := encodeSaveKey(test.key); got != test.path {
			t.Errorf("%q is stored at %q, want %q", test.key, got, test.path)
		}
		for index, segment := range strings.Split(test.path, "/") {
			decoded, ok := decodeSaveName(segment)
			if want := strings.Split(test.key, "/")[index]; !ok || decoded != want {
				t.Errorf("%q reads back as %q (%t), want %q", segment, decoded, ok, want)
			}
		}
	}
	for _, name := range []string{"Save", "#a", "#", "a#", "#1", "##A#", "#K", "Ä", "é", "#+000061", "#+0000E9", "#+0000e", "#+", "#é"} {
		if decoded, ok := decodeSaveName(name); ok {
			t.Errorf("%q, which the encoding never writes, read as the name %q", name, decoded)
		}
	}
}

// Every rune survives the round trip, before and after a '#' and alone, and
// what the encoding writes holds no letter a file system that ignores case
// would take for another without a '#' before it, is its own NFC form, and
// holds nothing Windows refuses or drops.
func TestEverySaveNameReadsBack(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		name := "a" + string(r) + "#" + string(r) + "Z"
		encoded := encodeSaveName(name)
		decoded, ok := decodeSaveName(encoded)
		if !ok || decoded != name {
			t.Fatalf("%q is written %q and reads back as %q (%t)", name, encoded, decoded, ok)
		}
		// What Windows drops from the end of a name is ASCII.
		written := []string{encoded}
		if r < utf8.RuneSelf {
			alone := encodeSaveName(string(r))
			if decoded, ok := decodeSaveName(alone); !ok || decoded != string(r) {
				t.Fatalf("%q is written %q and reads back as %q (%t)", string(r), alone, decoded, ok)
			}
			written = append(written, alone)
		}
		for _, written := range written {
			if !norm.NFC.IsNormalString(written) || strings.ContainsAny(written, "<>:\"\\|?*") || strings.IndexFunc(written, func(r rune) bool { return r < 0x20 }) >= 0 ||
				strings.HasSuffix(written, ".") || strings.HasSuffix(written, " ") {
				t.Fatalf("%q is written %q, which a host changes or refuses", string(r), written)
			}
		}
		if saveNameEncodes(r) && !strings.Contains(encoded, "#"+string(r)) {
			t.Fatalf("%q is written %q, which leaves %q bare", name, encoded, r)
		}
		// Outside ASCII, nothing with case is written bare: a letter with case
		// is there only after the '#' that marks it upper case.
		previous := rune(0)
		for _, written := range encoded {
			cased := unicode.IsUpper(written) || unicode.IsLower(written) || unicode.IsTitle(written) || unicode.SimpleFold(written) != written
			if written >= utf8.RuneSelf && cased && (previous != '#' || !saveNameEncodes(written)) {
				t.Fatalf("%q is written %q, which leaves %q bare", name, encoded, written)
			}
			previous = written
		}
	}
}

// No key is ever stored under the record's name, in any spelling of its case,
// which is what lets a file system that ignores case keep the record apart
// from every save.
func TestTheRecordsNameIsNoKeysPath(t *testing.T) {
	letters := 0
	for _, r := range saveNamesRecord {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	for variant := 0; variant < 1<<letters; variant++ {
		spelled := []rune(saveNamesRecord)
		bit := 0
		for index, r := range spelled {
			if unicode.IsLetter(r) {
				if variant&(1<<bit) != 0 {
					spelled[index] = unicode.ToUpper(r)
				}
				bit++
			}
		}
		if decoded, ok := decodeSaveName(string(spelled)); ok {
			t.Fatalf("%q is the encoded name of the key %q", string(spelled), decoded)
		}
	}
}

// On every host — the case-insensitive file system of a Mac above all — names
// that differ only by case are separate saves, through every way into the
// store, and a backup carries them to another directory the same way.
func TestNamesThatDifferOnlyByCaseAreDifferentSaves(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	store := NewDirectorySaveStore(root)
	saves := map[string]string{"rms/Save": "upper", "rms/save": "lower", "rms/SAVE": "every letter", "rms/sAvE": "mixed"}
	keys := make([]string, 0, len(saves))
	for key := range saves {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := store.StoreSave(key, []byte(saves[key])); err != nil {
			t.Fatalf("store %s: %v", key, err)
		}
	}
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, []string{
		encodedRecord,
		"owner/rms/#Save=upper", "owner/rms/save=lower", "owner/rms/#S#A#V#E=every letter", "owner/rms/s#Av#E=mixed",
		leftoverReserved + "/lock=",
	}, "after the writes")
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

	// A batch keeps them apart the same way.
	saves["rms/Save"], saves["rms/save"] = "upper again", "lower again"
	if err := store.StoreSaves(map[string][]byte{"rms/Save": []byte("upper again"), "rms/save": []byte("lower again")}); err != nil {
		t.Fatal(err)
	}
	wantSaves(t, NewDirectorySaveStore(root), saves)

	// A backup holds the four keys, and restores them as four files.
	identity := SaveIdentity([]byte("archive"))
	listed, err = ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := EncodeSavePack(SavePack{Identity: identity, Entries: listed})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSavePack(pack)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	if written, removed, err := WriteSaveTree(other, decoded.Entries); err != nil || written != 4 || removed != 0 {
		t.Fatalf("restore wrote %d and removed %d: %v", written, removed, err)
	}
	wantSaves(t, NewDirectorySaveStore(other), saves)
}

// A save directory as the previous release left it: every key its own path,
// upper-case names, a '#' before a letter, a digit or another '#', nested
// directories, and beside it a quick-save slot and the saves an earlier quick
// load set aside. This build reads every key with the bytes it had, lists and
// exports exactly what the earlier build did, leaves the slot and the set-aside
// saves alone, and leaves the directory converted.
func TestAnEarlierSaveDirectoryIsConvertedOnFirstUse(t *testing.T) {
	earlier := []struct{ key, path, data string }{
		{"rms/.index", "rms/.index", "Option\nsave#1\n#Top\n"},
		{"rms/Option", "rms/#Option", "settings"},
		{"rms/save#1", "rms/save##1", "slot one"},
		// Reads both ways — the key "#Top" or an encoded "Top" — and is the
		// key "#Top" in a directory without the record.
		{"rms/#Top", "rms/###Top", "top scores"},
		{"rms/a##b", "rms/a####b", "two hashes"},
		{"fs/Data/Slot1.sav", "fs/#Data/#Slot1.sav", "nested"},
		{"fs/Data/notes.txt", "fs/#Data/notes.txt", "lower case below an upper-case directory"},
		{"fs/data2/Deep/X", "fs/data2/#Deep/#X", "deeper"},
		{"fs/.removed", "fs/.removed", "gone.dat\n"},
		{"fs/" + saveNamesRecord, "fs/.wfeature-names##1", "the record's name, below the top"},
		{"fs/Ärger", "fs/#Ärger", "a letter outside ASCII"},
		{"fs/한글", "fs/한글", "no case at all"},
		{"fs/empty", "fs/empty", ""},
		{"db/scores", "db/scores", "\x00\x01\x02"},
		{"jdb/Records", "jdb/#Records", "records"},
	}
	identity := SaveIdentity([]byte("the archive these saves belong to"))
	beside := []string{
		leftoverReserved + "/" + checkpointSlotName(identity) + "=" + strings.Repeat("s", checkpointHeaderSize),
		leftoverPrevious + "/rms/Option=what an earlier quick load set aside",
		leftoverPrevious + "/fs/Data/Slot1.sav=older",
	}
	saves := map[string]string{}
	var state, converted []string
	var entries []SaveEntry
	for _, entry := range earlier {
		saves[entry.key] = entry.data
		state = append(state, "owner/"+entry.key+"="+entry.data)
		converted = append(converted, "owner/"+entry.path+"="+entry.data)
		entries = append(entries, SaveEntry{Key: entry.key, Data: []byte(entry.data)})
	}
	// What the earlier build exported of this directory.
	earlierPack, err := EncodeSavePack(SavePack{Identity: identity, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	converted = append(converted, encodedRecord, leftoverReserved+"/lock=")
	converted = append(converted, beside...)

	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	leftoverBuild(t, parent, append(slices.Clone(state), beside...))
	store := NewDirectorySaveStore(root)
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, converted, "after the first read")

	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, saves, "the listing")
	exported, err := EncodeSavePack(SavePack{Identity: identity, Entries: listed})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(exported, earlierPack) {
		t.Fatal("the export differs from the one the earlier build made of the same saves")
	}
	snapshot, err := store.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, snapshot, saves, "the snapshot")
	if found, err := store.HasCheckpoint(identity); err != nil || !found {
		t.Fatalf("the quick-save slot is found = %t, %v", found, err)
	}
	if displaced, err := store.DisplacedGeneration(); err != nil || !displaced {
		t.Fatalf("the saves an earlier quick load set aside are reported = %t, %v", displaced, err)
	}
	leftoverExpect(t, parent, converted, "after the listing, the export and the questions")

	// Another store on the same directory, as the next run of the program
	// opens it, reads the same; and a name that differs from one of these
	// only by case is a save of its own.
	again := NewDirectorySaveStore(root)
	wantSaves(t, again, saves)
	if err := again.StoreSave("rms/option", []byte("a different store")); err != nil {
		t.Fatal(err)
	}
	if data, _, err := again.ReadSave("rms/Option"); err != nil || string(data) != "settings" {
		t.Fatalf("rms/Option after rms/option was written = %q, %v", data, err)
	}

	// The earlier build's backup restores into an empty directory under the
	// encoded names, and exports again as the same bytes.
	decoded, err := DecodeSavePack(earlierPack)
	if err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(t.TempDir(), "fresh")
	if _, _, err := WriteSaveTree(fresh, decoded.Entries); err != nil {
		t.Fatal(err)
	}
	restored, err := ReadSaveTree(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := EncodeSavePack(SavePack{Identity: identity, Entries: restored}); err != nil || !bytes.Equal(again, earlierPack) {
		t.Fatalf("the restored backup exports differently: %v", err)
	}
	for _, entry := range earlier {
		if data, err := os.ReadFile(filepath.Join(fresh, filepath.FromSlash(entry.path))); err != nil || string(data) != entry.data {
			t.Fatalf("restored %s is %q, %v", entry.path, data, err)
		}
	}

	// And it restores over a directory the earlier build left, which is
	// converted first, so that what the backup does not name is what goes.
	other := t.TempDir()
	leftoverBuild(t, other, append(slices.Clone(state), "owner/rms/Extra=not in the backup"))
	written, removed, err := WriteSaveTree(filepath.Join(other, "owner"), decoded.Entries)
	if err != nil || written != len(earlier) || removed != 1 {
		t.Fatalf("restore over an earlier directory wrote %d and removed %d: %v", written, removed, err)
	}
	wantSaves(t, NewDirectorySaveStore(filepath.Join(other, "owner")), saves)
	if _, err := os.Lstat(filepath.Join(other, "owner", "rms", "#Extra")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the entry the backup left out is still there: %v", err)
	}
}

// An earlier build's quick load that stopped between its two renames left the
// live saves as "previous". Recovery puts them back first, and they are then
// an earlier directory like any other.
func TestSavesAnEarlierQuickLoadSetAsideAreConvertedWhenRecoveryRestoresThem(t *testing.T) {
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{
		leftoverReserved + "/intent=WFRSTR01\x01",
		leftoverStaging + "/rms/Option=what the load brought",
		leftoverPrevious + "/rms/Option=the saves in use",
		leftoverPrevious + "/fs/Data/Slot=nested",
	})
	store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
	wantSaves(t, store, map[string]string{"rms/Option": "the saves in use", "fs/Data/Slot": "nested"})
	leftoverExpect(t, parent, []string{
		encodedRecord, "owner/rms/#Option=the saves in use", "owner/fs/#Data/#Slot=nested",
		leftoverReserved + "/lock=",
	}, "after recovery and the first read")
}

// A host that ignores case let an earlier build keep one file for "Save" and
// "save", and the data in it cannot be split. It belongs to the name it is
// spelled with on the disk; the other name has no save until one is written.
func TestAnEarlierFileBelongsToTheNameItIsSpelledWith(t *testing.T) {
	for _, test := range []struct{ spelled, other string }{{"save", "Save"}, {"Save", "save"}, {"SAVE", "Save"}} {
		t.Run(test.spelled, func(t *testing.T) {
			parent := t.TempDir()
			leftoverBuild(t, parent, []string{"owner/rms/" + test.spelled + "=progress"})
			store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
			if _, found, err := store.ReadSave("rms/" + test.other); err != nil || found {
				t.Fatalf("rms/%s found = %t, %v; the file is spelled %s", test.other, found, err, test.spelled)
			}
			wantSaves(t, store, map[string]string{"rms/" + test.spelled: "progress"})
			if err := store.StoreSave("rms/"+test.other, []byte("its own")); err != nil {
				t.Fatal(err)
			}
			wantSaves(t, store, map[string]string{"rms/" + test.spelled: "progress", "rms/" + test.other: "its own"})
		})
	}
	// Where the earlier build ran on a host that keeps them apart, the two
	// spellings are two files, and they stay two saves.
	t.Run("both spellings", func(t *testing.T) {
		parent := t.TempDir()
		if err := os.WriteFile(filepath.Join(parent, "probe"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(parent, "PROBE")); err == nil {
			t.Skip("this file system takes the two spellings for one name, so an earlier build could not leave both")
		}
		if err := os.Remove(filepath.Join(parent, "probe")); err != nil {
			t.Fatal(err)
		}
		leftoverBuild(t, parent, []string{"owner/rms/Save=upper", "owner/rms/save=lower", "owner/fs/Data/x=one", "owner/fs/data/x=two"})
		saves := map[string]string{"rms/Save": "upper", "rms/save": "lower", "fs/Data/x": "one", "fs/data/x": "two"}
		wantSaves(t, NewDirectorySaveStore(filepath.Join(parent, "owner")), saves)
		leftoverExpect(t, parent, []string{
			encodedRecord, leftoverReserved + "/lock=",
			"owner/rms/#Save=upper", "owner/rms/save=lower", "owner/fs/#Data/x=one", "owner/fs/data/x=two",
		}, "after the conversion")
	})
}

// A directory none of whose names needs a '#' reads the same either way, and
// opening it changes nothing on the disk: no record and no rename. The record
// arrives with the first key that needs one, ahead of it.
func TestADirectoryWithoutUpperCaseOrHashIsLeftAsItIs(t *testing.T) {
	parent := t.TempDir()
	state := []string{"owner/rms/.index=slot0\n", "owner/rms/slot0=records", "owner/fs/dir/nested.bin=\x01\x02", "owner/fs/empty="}
	leftoverBuild(t, parent, state)
	root := filepath.Join(parent, "owner")
	store := NewDirectorySaveStore(root)
	wantSaves(t, store, map[string]string{"rms/.index": "slot0\n", "rms/slot0": "records", "fs/dir/nested.bin": "\x01\x02", "fs/empty": ""})
	if _, err := ReadSaveTree(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SnapshotSaves(); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave("rms/slot0", []byte("records 2")); err != nil {
		t.Fatal(err)
	}
	state[1] = "owner/rms/slot0=records 2"
	leftoverExpect(t, parent, append(slices.Clone(state), leftoverReserved+"/lock="), "after reads and a write")

	if err := store.StoreSave("rms/Slot1", []byte("upper")); err != nil {
		t.Fatal(err)
	}
	leftoverExpect(t, parent, append(slices.Clone(state), leftoverReserved+"/lock=", encodedRecord, "owner/rms/#Slot1=upper"), "after the first name with a '#'")
}

// A conversion is a run of renames and three writes of its record, and a
// process can stop after any of them. Each is made to fail in turn, which
// leaves the disk as a stop right before it would; the next operation finishes
// the conversion, and every save reads with its bytes under its own key.
//
// The names are chosen so that one directory takes the name another one left:
// "#A" is the encoded name of "A", "###A" that of "#A".
func TestAConversionThatStopsAnywhereFinishes(t *testing.T) {
	saves := map[string]string{
		"x/###A/#B": "one", "x/#A/#B": "two", "x/A": "three", "x/b": "four",
		"Top/Mid/Low": "five", "Top/other": "six",
	}
	converted := []string{
		encodedRecord, leftoverReserved + "/lock=",
		"owner/x/#######A/###B=one", "owner/x/###A/###B=two", "owner/x/#A=three", "owner/x/b=four",
		"owner/#Top/#Mid/#Low=five", "owner/#Top/other=six",
	}
	var state []string
	for key, data := range saves {
		state = append(state, "owner/"+key+"="+data)
	}
	original := struct {
		rename func(string, string) error
		write  func(string, []byte) error
	}{saveNamesRename, saveNamesWrite}
	resume := func() { saveNamesRename, saveNamesWrite = original.rename, original.write }
	t.Cleanup(resume)
	// stopAt makes one step of a conversion fail, counting renames and record
	// writes together from one, and answers how many steps have been taken.
	stopAt := func(stop int) *int {
		taken := new(int)
		stopped := errors.New("the process stopped here")
		saveNamesRename = func(from, to string) error {
			if *taken++; *taken == stop {
				return stopped
			}
			return original.rename(from, to)
		}
		saveNamesWrite = func(path string, data []byte) error {
			if *taken++; *taken == stop {
				return stopped
			}
			return original.write(path, data)
		}
		return taken
	}

	parent := t.TempDir()
	leftoverBuild(t, parent, state)
	taken := stopAt(0)
	wantSaves(t, NewDirectorySaveStore(filepath.Join(parent, "owner")), saves)
	steps := *taken
	resume()
	// Eight names move twice each, and the record is written three times.
	if want := 2*8 + 3; steps != want {
		t.Fatalf("an uninterrupted conversion took %d steps, want %d", steps, want)
	}
	leftoverExpect(t, parent, converted, "after an uninterrupted conversion")

	for stop := 1; stop <= steps; stop++ {
		parent := t.TempDir()
		leftoverBuild(t, parent, state)
		root := filepath.Join(parent, "owner")
		stopAt(stop)
		// Whatever this answers, it is never a wrong save.
		for key, want := range saves {
			if data, found, err := NewDirectorySaveStore(root).ReadSave(key); err == nil && (!found || string(data) != want) {
				t.Fatalf("stop %d: %s read %q (found %t) while the conversion was stopping", stop, key, data, found)
			}
		}
		resume()
		listed, err := ReadSaveTree(root)
		if err != nil {
			t.Fatalf("stop %d: the next operation: %v", stop, err)
		}
		wantEntries(t, listed, saves, "the listing after the stop")
		wantSaves(t, NewDirectorySaveStore(root), saves)
		leftoverExpect(t, parent, converted, "after the conversion finished")
	}
}

// A directory an earlier build wrote that this build cannot write is read as
// the earlier build left it, each name by its exact spelling, and nothing is
// written to it. Once it can be written, the next operation converts it.
func TestAnEarlierDirectoryThatCannotBeWrittenIsReadAsItIs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not refuse a write by directory mode bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("the superuser is not refused by directory mode bits")
	}
	parent := t.TempDir()
	state := []string{"owner/rms/Option=settings", "owner/rms/save=progress", "owner/fs/Data/x=deep"}
	leftoverBuild(t, parent, state)
	root := filepath.Join(parent, "owner")
	directories := []string{root, filepath.Join(root, "rms"), filepath.Join(root, "fs"), filepath.Join(root, "fs", "Data")}
	for _, directory := range directories {
		if err := os.Chmod(directory, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, directory := range directories {
			_ = os.Chmod(directory, 0o755)
		}
	})
	store := NewDirectorySaveStore(root)
	saves := map[string]string{"rms/Option": "settings", "rms/save": "progress", "fs/Data/x": "deep"}
	wantSaves(t, store, saves)
	for _, key := range []string{"rms/option", "rms/Save", "fs/data/x"} {
		if _, found, err := store.ReadSave(key); err != nil || found {
			t.Fatalf("%s found = %t, %v; no file is spelled so", key, found, err)
		}
	}
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, saves, "the listing")
	if err := store.StoreSave("rms/save", []byte("new")); err == nil || !strings.Contains(err.Error(), "earlier build") {
		t.Fatalf("a write into a directory that could not be converted = %v", err)
	}
	if _, _, err := WriteSaveTree(root, listed); err == nil {
		t.Fatal("a restore into a directory that could not be converted succeeded")
	}
	leftoverExpect(t, parent, append(slices.Clone(state), leftoverReserved+"/lock="), "while it cannot be written")

	for _, directory := range directories {
		if err := os.Chmod(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, []string{
		encodedRecord, "owner/rms/#Option=settings", "owner/rms/save=progress", "owner/fs/#Data/x=deep",
		leftoverReserved + "/lock=",
	}, "once it can be written")
}

// An earlier build that writes into a directory this build converted — run
// again after an upgrade, or its files copied in by hand — leaves names the
// encoding would not have written. They read as their own keys; the encoded
// name wins where both are there, and the earlier one is then left alone. A
// read moves an earlier name to its encoded one, a directory whole where it
// can, and removes only directories the move left empty.
func TestEarlierNamesInAConvertedDirectoryReadAsTheirOwnKeys(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	store := NewDirectorySaveStore(root)
	for key, data := range map[string]string{"rms/Option": "settings", "fs/Data/Other": "encoded below"} {
		if err := store.StoreSave(key, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	leftoverBuild(t, parent, []string{
		"owner/rms/Option=a stale copy",
		"owner/rms/Late=written by an earlier build",
		"owner/fs/Data/Slot=into a directory that has its encoded twin",
		"owner/fs/Old/A=in a directory that has none",
		"owner/fs/Old/B=beside it",
	})
	// The stale copy is older than the encoded entry beside it.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, "rms", "Option"), past, past); err != nil {
		t.Fatal(err)
	}
	saves := map[string]string{
		"rms/Option": "settings", "rms/Late": "written by an earlier build", "fs/Data/Other": "encoded below",
		"fs/Data/Slot": "into a directory that has its encoded twin",
		"fs/Old/A":     "in a directory that has none", "fs/Old/B": "beside it",
	}
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
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, []string{
		encodedRecord, leftoverReserved + "/lock=",
		"owner/rms/#Option=settings", "owner/rms/Option=a stale copy",
		"owner/rms/#Late=written by an earlier build",
		"owner/fs/#Data/#Other=encoded below", "owner/fs/#Data/#Slot=into a directory that has its encoded twin",
		"owner/fs/#Old/#A=in a directory that has none", "owner/fs/#Old/#B=beside it",
	}, "after the reads")
}

// Anything at the record's name that is not a record this build wrote — an
// earlier save that happens to have the name, a damaged record, a directory —
// is no answer to how the names read. Every operation refuses, and nothing is
// changed.
func TestWhatIsNotARecordAtTheRecordsNameIsRefusedAndKept(t *testing.T) {
	for _, test := range []struct{ name, line string }{
		{"an earlier save", "owner/" + saveNamesRecord + "=an earlier save under this name"},
		{"an unknown state", "owner/" + saveNamesRecord + "=" + saveNamesHeader + "\nsomething else\n"},
		{"trailing lines", "owner/" + saveNamesRecord + "=" + saveNamesHeader + "\nencoded\n\"rms/Option\"\n"},
		{"a conversion with nothing in it", "owner/" + saveNamesRecord + "=" + saveNamesHeader + "\nconverting: to temporary names\n"},
		{"a path that climbs", "owner/" + saveNamesRecord + "=" + saveNamesHeader + "\nconverting: to temporary names\n\"../x\"\n"},
		{"a directory", "owner/" + saveNamesRecord + "/x=inside"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent := t.TempDir()
			state := []string{test.line, "owner/rms/Option=settings"}
			leftoverBuild(t, parent, state)
			root := filepath.Join(parent, "owner")
			store := NewDirectorySaveStore(root)
			if _, _, err := store.ReadSave("rms/Option"); err == nil {
				t.Fatal("a read succeeded")
			}
			if err := store.StoreSave("rms/Option", []byte("x")); err == nil {
				t.Fatal("a write succeeded")
			}
			if _, err := ReadSaveTree(root); err == nil {
				t.Fatal("a listing succeeded")
			}
			leftoverExpect(t, parent, append(slices.Clone(state), leftoverReserved+"/lock="), "after the refusals")
		})
	}
}

// An earlier name whose encoded name is longer than a file system stores
// stays where it is and reads as its own key, while the rest of the directory
// is converted. One that could also be read as an encoded name cannot stay,
// so the directory is read as it was left instead, and nothing is written.
func TestAnEarlierNameTooLongToConvert(t *testing.T) {
	long := strings.Repeat("A", 200)
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{"owner/fs/" + long + "=long", "owner/rms/Option=settings"})
	store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
	wantSaves(t, store, map[string]string{"fs/" + long: "long", "rms/Option": "settings"})
	leftoverExpect(t, parent, []string{
		encodedRecord, leftoverReserved + "/lock=", "owner/fs/" + long + "=long", "owner/rms/#Option=settings",
	}, "after the first read")
	listed, err := ReadSaveTree(filepath.Join(parent, "owner"))
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, map[string]string{"fs/" + long: "long", "rms/Option": "settings"}, "the listing")

	// Alone in its directory it converts nothing, and the directory stays
	// without the record; the name still reads as its own key.
	parent = t.TempDir()
	leftoverBuild(t, parent, []string{"owner/fs/" + long + "=long", "owner/fs/plain=plain"})
	store = NewDirectorySaveStore(filepath.Join(parent, "owner"))
	wantSaves(t, store, map[string]string{"fs/" + long: "long", "fs/plain": "plain"})
	leftoverExpect(t, parent, []string{
		leftoverReserved + "/lock=", "owner/fs/" + long + "=long", "owner/fs/plain=plain",
	}, "after reading a directory with nothing to convert")

	ambiguous := strings.Repeat("#A", 64)
	parent = t.TempDir()
	state := []string{"owner/fs/" + ambiguous + "=ambiguous", "owner/rms/Option=settings"}
	leftoverBuild(t, parent, state)
	store = NewDirectorySaveStore(filepath.Join(parent, "owner"))
	wantSaves(t, store, map[string]string{"fs/" + ambiguous: "ambiguous", "rms/Option": "settings"})
	if err := store.StoreSave("rms/Option", []byte("x")); err == nil {
		t.Fatal("a write into a directory that could not be converted succeeded")
	}
	leftoverExpect(t, parent, append(slices.Clone(state), leftoverReserved+"/lock="), "after the refused conversion")
}

func wantSaves(t *testing.T, store *DirectorySaveStore, saves map[string]string) {
	t.Helper()
	for key, want := range saves {
		data, found, err := store.ReadSave(key)
		if err != nil || !found || string(data) != want {
			t.Fatalf("%s reads %q (found %t, %v), want %q", key, data, found, err, want)
		}
		limited, found, err := store.ReadSaveLimit(key, int64(len(want)))
		if err != nil || !found || string(limited) != want {
			t.Fatalf("%s reads %q on a budget (found %t, %v), want %q", key, limited, found, err, want)
		}
	}
}

func wantEntries(t *testing.T, entries []SaveEntry, saves map[string]string, what string) {
	t.Helper()
	got := make(map[string]string, len(entries))
	for index, entry := range entries {
		if index > 0 && entries[index-1].Key >= entry.Key {
			t.Fatalf("%s is not in key order at %q", what, entry.Key)
		}
		got[entry.Key] = string(entry.Data)
	}
	if len(got) != len(saves) {
		t.Fatalf("%s holds %d entries, want %d: %q", what, len(got), len(saves), got)
	}
	for key, want := range saves {
		if data, found := got[key]; !found || data != want {
			t.Fatalf("%s has %s = %q (found %t), want %q", what, key, data, found, want)
		}
	}
}

// A batch into a directory that does not exist yet writes the record before
// the first name that needs a '#', as a single write does.
func TestABatchKeepsNamesThatDifferOnlyByCaseApart(t *testing.T) {
	parent := t.TempDir()
	store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
	if err := store.StoreSaves(map[string][]byte{"rms/Save": []byte("upper"), "rms/save": []byte("lower"), "fs/a#b": []byte("hash")}); err != nil {
		t.Fatal(err)
	}
	wantSaves(t, store, map[string]string{"rms/Save": "upper", "rms/save": "lower", "fs/a#b": "hash"})
	leftoverExpect(t, parent, []string{
		encodedRecord, leftoverReserved + "/lock=", "owner/rms/#Save=upper", "owner/rms/save=lower", "owner/fs/a##b=hash",
	}, "after the batch")
}

// A save folder may be a link to saves kept elsewhere. The conversion happens
// in the directory the link leads to, and the link stays a link.
func TestAnEarlierFolderBehindALinkIsConvertedWhereItIs(t *testing.T) {
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{"elsewhere/rms/Option=settings", "owner->elsewhere"})
	wantSaves(t, NewDirectorySaveStore(filepath.Join(parent, "owner")), map[string]string{"rms/Option": "settings"})
	leftoverExpect(t, parent, []string{
		"owner->elsewhere", "elsewhere/" + saveNamesRecord + "=" + saveNamesHeader + "\nencoded\n",
		"elsewhere/rms/#Option=settings", leftoverReserved + "/lock=", ".wfeature-quicksave/owners/elsewhere/lock=",
	}, "after the first read through the link")
}

// Several stores opened on one earlier folder at once — a game, a listing and
// an export in one server — convert it once between them, and every read
// answers the save it asked for.
func TestStoresMeetingAnEarlierFolderAtOnceConvertItOnce(t *testing.T) {
	parent := t.TempDir()
	saves := map[string]string{"rms/Option": "settings", "rms/#Top": "scores", "fs/Data/Slot": "nested", "db/plain": "plain"}
	var state []string
	for key, data := range saves {
		state = append(state, "owner/"+key+"="+data)
	}
	leftoverBuild(t, parent, state)
	root := filepath.Join(parent, "owner")
	failures := make(chan error, 16)
	done := make(chan struct{})
	for range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			store := NewDirectorySaveStore(root)
			for key, want := range saves {
				data, found, err := store.ReadSave(key)
				if err != nil || !found || string(data) != want {
					failures <- errors.New(key + " read " + string(data))
					return
				}
			}
			if _, err := ReadSaveTree(root); err != nil {
				failures <- err
			}
		}()
	}
	for range 8 {
		<-done
	}
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	leftoverExpect(t, parent, []string{
		encodedRecord, leftoverReserved + "/lock=",
		"owner/rms/#Option=settings", "owner/rms/###Top=scores", "owner/fs/#Data/#Slot=nested", "owner/db/plain=plain",
	}, "after the stores met it")
}

// A file system that ignores case folds more than upper into lower: APFS takes
// "ß" for "ss", the ligature "ﬁ" for "fi", the Kelvin sign for "k", "ς" for
// "σ" and the title-case "ǅ" for "ǆ". Keys kept apart only by such a letter
// are separate saves too.
func TestNamesThatFoldTogetherAreDifferentSaves(t *testing.T) {
	store := NewDirectorySaveStore(filepath.Join(t.TempDir(), "owner"))
	saves := map[string]string{
		"rms/ß": "sharp s", "rms/ss": "two s", "rms/ẞ": "capital sharp s",
		"rms/ﬁ": "ligature", "rms/fi": "two letters",
		"rms/Key": "kelvin", "rms/key": "k",
		"rms/ς": "final sigma", "rms/σ": "sigma",
		"rms/ǅ": "title case", "rms/ǆ": "lower case", "rms/Ǆ": "upper case",
		"rms/é": "precomposed", "rms/É": "upper precomposed",
	}
	keys := make([]string, 0, len(saves))
	for key := range saves {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := store.StoreSave(key, []byte(saves[key])); err != nil {
			t.Fatalf("store %s: %v", key, err)
		}
	}
	wantSaves(t, store, saves)
	listed, err := ReadSaveTree(store.root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, saves, "the listing")
}

// A player who went back to an earlier build after this one converted a save
// directory, played there, and came back finds the progress they made on the
// earlier build: it is under the name the earlier build gives the key, beside
// the older encoded entry, and the one written later is the save. A read moves
// it over the encoded entry, and a listing before that read already chooses it.
func TestAnEarlierNameWrittenAfterTheEncodedOneIsTheSave(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	store := NewDirectorySaveStore(root)
	for key, data := range map[string]string{"rms/Save": "before the downgrade", "fs/Data/Slot": "before it too"} {
		if err := store.StoreSave(key, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-time.Hour)
	for _, path := range []string{"rms/#Save", "fs/#Data/#Slot"} {
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(path)), past, past); err != nil {
			t.Fatal(err)
		}
	}
	leftoverBuild(t, parent, []string{
		"owner/rms/Save=played on the earlier build",
		"owner/fs/Data/Slot=played there as well",
	})
	saves := map[string]string{"rms/Save": "played on the earlier build", "fs/Data/Slot": "played there as well"}
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
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, []string{
		encodedRecord, leftoverReserved + "/lock=",
		"owner/rms/#Save=played on the earlier build",
		"owner/fs/#Data/#Slot=played there as well",
	}, "after the reads")
}

// A listing spelledExactly made is kept until its directory changes, so a
// title that asks for a missing key every frame does not list the directory
// every frame, and a name added or removed is seen at once.
func TestSaveDirectoryListingsAreKeptUntilTheDirectoryChanges(t *testing.T) {
	directory := t.TempDir()
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	setTime := func(when time.Time) {
		t.Helper()
		if err := os.Chtimes(directory, when, when); err != nil {
			t.Fatal(err)
		}
	}
	write("a")
	first := time.Now().Add(-time.Hour)
	setTime(first)
	if names, err := directoryNames(directory); err != nil || !slices.Equal(names, []string{"a"}) {
		t.Fatalf("listing = %v (%v)", names, err)
	}
	// Changed behind the listing's back, with the time put back: the kept
	// listing is what answers, which is the listing being kept.
	write("b")
	setTime(first)
	if names, _ := directoryNames(directory); !slices.Equal(names, []string{"a"}) {
		t.Fatalf("listing of an unchanged directory = %v, want the kept one", names)
	}
	setTime(first.Add(time.Second))
	names, err := directoryNames(directory)
	sort.Strings(names)
	if err != nil || !slices.Equal(names, []string{"a", "b"}) {
		t.Fatalf("listing after the directory changed = %v (%v)", names, err)
	}
	// A name removed under a kept listing is not reported as spelled there.
	if err := os.Remove(filepath.Join(directory, "b")); err != nil {
		t.Fatal(err)
	}
	setTime(first.Add(time.Second))
	if present, err := spelledExactly(directory, "b"); err != nil || present {
		t.Fatalf("a removed name is spelled there: %t (%v)", present, err)
	}
}

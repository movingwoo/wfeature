package backend

import (
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

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// The format is pinned by hand here too: what a host file system would take
// for another name, or refuse, is written by its number.
func TestSaveNamesNumberWhatHostsChange(t *testing.T) {
	for _, test := range []struct{ key, path string }{
		// Normalization: a mark that joins the letter before it, a character
		// that is another one's canonical spelling, a Hangul vowel or final
		// consonant jamo, a CJK compatibility ideograph, and the seven Greek
		// capitals with an accent that normalization turns into others.
		{"é", "e#+000301"},
		{";", "#+00037e"},
		{"豈", "#+00f900"},
		{"가", "ᄀ#+001161"},
		{"각", "ᄀ#+001161#+0011a8"},
		{"が", "か#+003099"},
		{"Ά", "#+001fbb"},
		{"Ά", "#Ά"},
		// What stays readable.
		{"가", "가"},
		{"각", "각"},
		{"が", "が"},
		{"豈", "豈"},
		{"ﾞ", "ﾞ"},
		// Windows: the characters it refuses anywhere,
		{"slot:1", "slot#+00003a1"},
		{`a<b>c"d|e?f*g`, "a#+00003cb#+00003ec#+000022d#+00007ce#+00003ff#+00002ag"},
		{"a\x01b\x1fc", "a#+000001b#+00001fc"},
		// a '.' or ' ' it drops from the end of a name,
		{"save.", "save#+00002e"},
		{"save ", "save#+000020"},
		{"a..", "a.#+00002e"},
		{"...", "..#+00002e"},
		{"a. ", "a.#+000020"},
		{".index", ".index"},
		{"a.b", "a.b"},
		{" a", " a"},
		// and the names of its devices, with or without an extension.
		{"con", "#+000063on"},
		{"CON", "#C#O#N"},
		{"Con", "#Con"},
		{"con.txt", "#+000063on.txt"},
		{"con .txt", "#+000063on .txt"},
		{"con.", "#+000063on#+00002e"},
		{"aux", "#+000061ux"},
		{"nul.tar.gz", "#+00006eul.tar.gz"},
		{"prn", "#+000070rn"},
		{"com0", "#+000063om0"},
		{"com1", "#+000063om1"},
		{"lpt9.sav", "#+00006cpt9.sav"},
		{"com¹", "#+000063om¹"},
		{"lpt³", "#+00006cpt³"},
		{"com10", "com10"},
		{"con1", "con1"},
		{"conin$", "#+000063onin$"},
		{"CONOUT$.log", "#C#O#N#O#U#T$.log"},
		{"conout$x", "conout$x"},
		{"console", "console"},
		{"xcon", "xcon"},
		{"fs/con/save", "fs/#+000063on/save"},
	} {
		if got := encodeSaveKey(test.key); got != test.path {
			t.Errorf("%+q is stored at %+q, want %+q", test.key, got, test.path)
		}
		for index, segment := range strings.Split(test.path, "/") {
			decoded, ok := decodeSaveName(segment)
			if want := strings.Split(test.key, "/")[index]; !ok || decoded != want {
				t.Errorf("%+q reads back as %+q (%t), want %+q", segment, decoded, ok, want)
			}
		}
	}
	// A name reads back only when it is the very name the encoding writes for
	// what it reads back as.
	for _, name := range []string{
		"slot:1", "save.", "save ", "con", "con.txt", "é", ";", "Ά", "#Ά",
		"a#+00002eb", "#+000063onx", "c#+00006fn", "#+000063", "#+00002e#+00002e", "#+000041", "#+00d800", "#+110000",
	} {
		if decoded, ok := decodeSaveName(name); ok {
			t.Errorf("%+q, which the encoding never writes, read as the name %+q", name, decoded)
		}
	}
}

// hostSaveName models what the file systems a Host runs on compare when they
// look a name up: Windows drops a '.' and ' ' at its end and takes a device
// name for the device, whatever follows it; APFS and HFS+ compare names after
// decomposing them; and the ones that ignore case fold it. A name Windows
// refuses outright answers "".
func hostSaveName(name string) string {
	for _, r := range name {
		if r < 0x20 || strings.ContainsRune(`<>:"\|?*`, r) {
			return ""
		}
	}
	trimmed := strings.TrimRight(name, ". ")
	if trimmed == "" {
		return ""
	}
	stem, _, _ := strings.Cut(trimmed, ".")
	stem = strings.ToUpper(strings.TrimRight(stem, " "))
	switch {
	case stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL":
		return "device " + stem
	case len(stem) > 3 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")):
		if rest := stem[3:]; len(rest) == 1 && rest[0] >= '0' && rest[0] <= '9' || rest == "¹" || rest == "²" || rest == "³" {
			return "device " + stem
		}
	}
	return norm.NFD.String(cases.Fold().String(norm.NFD.String(trimmed)))
}

// Every encoded name is one a host stores as it is and tells apart from every
// other: no two keys of a set chosen to meet each way a host would join them
// share a name there, no name is refused or a device, and each is its own NFC
// form, so a host that normalizes has nothing to change.
func TestEncodedSaveNamesAreApartOnEveryHost(t *testing.T) {
	fold := cases.Fold()
	keys := map[string]bool{}
	add := func(segment string) {
		if segment == "" || segment == "." || segment == ".." || strings.ContainsAny(segment, "/\\\x00") || !utf8.ValidString(segment) {
			return
		}
		keys[segment] = true
	}
	// Each rune that has case or that normalization changes, with every
	// spelling a host could take for it.
	for r := rune(1); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		single := string(r)
		cased := unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) || unicode.SimpleFold(r) != r
		if r >= utf8.RuneSelf && !cased && norm.NFC.String(single) == single && norm.NFC.PropertiesString(single).BoundaryBefore() {
			continue
		}
		for _, spelling := range []string{single, norm.NFC.String(single), norm.NFD.String(single), fold.String(single), strings.ToUpper(single), strings.ToLower(single)} {
			add(spelling)
			add("x" + spelling)
			add(spelling + "x")
		}
	}
	for _, segment := range []string{
		"é", "é", "Å", "Å", "Å", "Ω", "Ω", "ω", ";", ";",
		"豈", "豈", "가", "가", "각", "각", "각",
		"が", "が", "Ά", "Ά", "ά", "ά", "Ά",
		"ß", "ss", "SS", "ẞ", "ﬁ", "fi", "K", "k", "K", "σ", "ς", "Σ",
		"ǅ", "ǆ", "Ǆ", "save", "Save", "SAVE", "save.", "save..", "save ", "save. ", "save .",
		"con", "CON", "Con", "con.txt", "CON.TXT", "con .txt", "con.", "aux", "nul", "prn", "com1", "COM1",
		"lpt1", "com¹", "COM¹", "lpt³", "com0", "conin$", "CONOUT$", "a:b", "a<b", "a#b", "a##b", "#", "##",
		"#Top", "Top", "a#+00003ab", "a:b.", "#+000063on",
	} {
		add(segment)
		add(segment + ".txt")
		add("x" + segment)
	}
	written := make(map[string]string, len(keys))
	for key := range keys {
		encoded := encodeSaveName(key)
		if decoded, ok := decodeSaveName(encoded); !ok || decoded != key {
			t.Fatalf("%+q is written %+q and reads back as %+q (%t)", key, encoded, decoded, ok)
		}
		if !norm.NFC.IsNormalString(encoded) {
			t.Fatalf("%+q is written %+q, which is not its own NFC form", key, encoded)
		}
		host := hostSaveName(encoded)
		if host == "" || strings.HasPrefix(host, "device ") {
			t.Fatalf("%+q is written %+q, which a host refuses or takes for a device", key, encoded)
		}
		if other, taken := written[host]; taken {
			t.Fatalf("%+q and %+q are written %+q and %+q, one name to a host", key, other, encoded, encodeSaveName(other))
		}
		written[host] = key
	}
}

// On the file system the test runs on — on a Mac, one that ignores both case
// and normalization — keys a host would take for one name are separate saves,
// and each is on the disk under a name every host keeps as it is.
func TestNamesAHostTakesForOneAreDifferentSaves(t *testing.T) {
	parent := t.TempDir()
	store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
	saves := map[string]string{
		"rms/é": "precomposed", "rms/é": "decomposed",
		"rms/;": "semicolon", "rms/;": "greek question mark",
		"rms/Ω": "omega", "rms/Ω": "ohm sign",
		"rms/豈": "ideograph", "rms/豈": "compatibility ideograph",
		"rms/가": "syllable", "rms/가": "jamo",
		"rms/が": "kana", "rms/が": "kana and mark",
		"rms/Ά": "tonos", "rms/Ά": "oxia",
		"fs/save": "plain", "fs/save.": "trailing dot", "fs/save ": "trailing space",
		"fs/con": "device", "fs/CON": "upper device", "fs/con.txt": "device with an extension",
		"fs/slot:1": "colon", "fs/slot?1": "question mark",
	}
	keys := make([]string, 0, len(saves))
	for key := range saves {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := store.StoreSave(key, []byte(saves[key])); err != nil {
			t.Fatalf("store %+q: %v", key, err)
		}
	}
	wantSaves(t, store, saves)
	listed, err := ReadSaveTree(store.root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, saves, "the listing")
	leftoverExpect(t, parent, []string{
		encodedRecord, leftoverReserved + "/lock=",
		"owner/rms/#+0000e9=precomposed", "owner/rms/e#+000301=decomposed",
		"owner/rms/;=semicolon", "owner/rms/#+00037e=greek question mark",
		"owner/rms/#Ω=omega", "owner/rms/#+002126=ohm sign",
		"owner/rms/豈=ideograph", "owner/rms/#+00f900=compatibility ideograph",
		"owner/rms/가=syllable", "owner/rms/ᄀ#+001161=jamo",
		"owner/rms/が=kana", "owner/rms/か#+003099=kana and mark",
		"owner/rms/#Ά=tonos", "owner/rms/#+001fbb=oxia",
		"owner/fs/save=plain", "owner/fs/save#+00002e=trailing dot", "owner/fs/save#+000020=trailing space",
		"owner/fs/#+000063on=device", "owner/fs/#C#O#N=upper device", "owner/fs/#+000063on.txt=device with an extension",
		"owner/fs/slot#+00003a1=colon", "owner/fs/slot#+00003f1=question mark",
	}, "after the writes")
}

// A folder the previous release wrote holds names the encoding now changes —
// ones Windows refuses or changes, ones normalization joins — as their keys.
// The first operation converts them with the rest, and every key reads with
// the bytes it had.
func TestAnEarlierFolderConvertsTheNamesHostsChange(t *testing.T) {
	skipWhereEarlierNamesCannotBe(t)
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{
		"owner/fs/slot:1=colon", "owner/fs/con=device", "owner/fs/save.=trailing dot",
		"owner/fs/é=decomposed", "owner/fs/;=greek question mark",
		"owner/fs/Dir:x/inner=below a directory to convert", "owner/rms/Save=upper", "owner/fs/plain=plain",
	})
	saves := map[string]string{
		"fs/slot:1": "colon", "fs/con": "device", "fs/save.": "trailing dot",
		"fs/é": "decomposed", "fs/;": "greek question mark",
		"fs/Dir:x/inner": "below a directory to convert", "rms/Save": "upper", "fs/plain": "plain",
	}
	root := filepath.Join(parent, "owner")
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, saves, "the listing")
	wantSaves(t, NewDirectorySaveStore(root), saves)
	leftoverExpect(t, parent, []string{
		encodedRecord, leftoverReserved + "/lock=",
		"owner/fs/slot#+00003a1=colon", "owner/fs/#+000063on=device", "owner/fs/save#+00002e=trailing dot",
		"owner/fs/e#+000301=decomposed", "owner/fs/#+00037e=greek question mark",
		"owner/fs/#Dir#+00003ax/inner=below a directory to convert", "owner/rms/#Save=upper", "owner/fs/plain=plain",
	}, "after the conversion")
}

// A folder the first form of the encoding converted — the record is the same —
// holds names it wrote that this form writes otherwise. Each reads as the key
// it was written for, through a listing before anything moved and through a
// read, which moves it to the name this form gives it, a directory whole.
func TestNamesTheFirstFormWroteReadAsTheirKeys(t *testing.T) {
	skipWhereEarlierNamesCannotBe(t)
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{
		encodedRecord,
		"owner/fs/slot:1=colon", "owner/rms/#Slot:1=upper and colon", "owner/fs/con=device",
		"owner/fs/#Con=upper device", "owner/fs/#Ά=oxia", "owner/fs/#Data:x/#File=nested",
		"owner/fs/#Data:x/notes=nested lower", "owner/rms/#Option=the same in both forms",
	})
	saves := map[string]string{
		"fs/slot:1": "colon", "rms/Slot:1": "upper and colon", "fs/con": "device", "fs/Con": "upper device",
		"fs/Ά": "oxia", "fs/Data:x/File": "nested", "fs/Data:x/notes": "nested lower",
		"rms/Option": "the same in both forms",
	}
	root := filepath.Join(parent, "owner")
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, saves, "the listing before a read")
	snapshot, err := NewDirectorySaveStore(root).SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, snapshot, saves, "the snapshot before a read")
	wantSaves(t, NewDirectorySaveStore(root), saves)
	converted := []string{
		encodedRecord, leftoverReserved + "/lock=",
		"owner/fs/slot#+00003a1=colon", "owner/rms/#Slot#+00003a1=upper and colon", "owner/fs/#+000063on=device",
		"owner/fs/#Con=upper device", "owner/fs/#+001fbb=oxia", "owner/fs/#Data#+00003ax/#File=nested",
		"owner/fs/#Data#+00003ax/notes=nested lower", "owner/rms/#Option=the same in both forms",
	}
	leftoverExpect(t, parent, converted, "after the reads")
	listed, err = ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, saves, "the listing after the reads")
	leftoverExpect(t, parent, converted, "after the second listing")
}

// Of the names one key can have in a converted folder — this form's, the
// first form's and the key itself, as the previous release writes it after a
// downgrade — the one written last is the save, for a read as for a listing.
func TestTheLatestOfAKeysNamesIsTheSave(t *testing.T) {
	skipWhereEarlierNamesCannotBe(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "owner")
	store := NewDirectorySaveStore(root)
	if err := store.StoreSave("rms/Slot:1", []byte("this form, oldest")); err != nil {
		t.Fatal(err)
	}
	leftoverBuild(t, parent, []string{"owner/rms/#Slot:1=the first form", "owner/rms/Slot:1=the previous release, newest"})
	now := time.Now()
	for path, age := range map[string]time.Duration{"rms/#Slot#+00003a1": 3 * time.Hour, "rms/#Slot:1": 2 * time.Hour, "rms/Slot:1": time.Hour} {
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(path)), now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	saves := map[string]string{"rms/Slot:1": "the previous release, newest"}
	listed, err := ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, saves, "the listing")
	wantSaves(t, store, saves)
	leftoverExpect(t, parent, []string{
		encodedRecord, leftoverReserved + "/lock=",
		"owner/rms/#Slot#+00003a1=the previous release, newest", "owner/rms/#Slot:1=the first form",
	}, "after the read")
	listed, err = ReadSaveTree(root)
	if err != nil {
		t.Fatal(err)
	}
	wantEntries(t, listed, saves, "the listing after the read")
}

// A key one of whose names, written out, is longer than a file system stores
// has no save and cannot have one. Every way in answers so, the same way: a
// read finds nothing, a write is refused with the limit named, a batch or a
// restore holding it is refused before anything is written.
func TestASaveNameTooLongOnceWrittenOutIsRefused(t *testing.T) {
	for _, segment := range []string{strings.Repeat("A", 200), strings.Repeat(":", 40), strings.Repeat("a", 300)} {
		t.Run(segment[:1], func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "owner")
			store := NewDirectorySaveStore(root)
			if err := store.StoreSave("fs/kept", []byte("kept")); err != nil {
				t.Fatal(err)
			}
			key := "fs/" + segment
			for _, name := range []string{key, key + "/below"} {
				if data, found, err := store.ReadSave(name); err != nil || found {
					t.Fatalf("read %s = %q (found %t), %v; want no save", name, data, found, err)
				}
				if data, found, err := store.ReadSaveLimit(name, 1<<20); err != nil || found {
					t.Fatalf("budgeted read %s = %q (found %t), %v; want no save", name, data, found, err)
				}
				if data, found := store.LoadSave(name); found {
					t.Fatalf("load %s = %q, want no save", name, data)
				}
				err := store.StoreSave(name, []byte("x"))
				if !errors.Is(err, errSaveNameTooLong) || !strings.Contains(err.Error(), "255") {
					t.Fatalf("write %s = %v, want the refusal that names the limit", name, err)
				}
			}
			if err := store.StoreSaves(map[string][]byte{"fs/kept": []byte("changed"), key: []byte("x")}); !errors.Is(err, errSaveNameTooLong) {
				t.Fatalf("batch = %v, want the refusal", err)
			}
			if written, removed, err := WriteSaveTree(root, []SaveEntry{{Key: "rms/new", Data: []byte("new")}, {Key: key, Data: []byte("x")}}); !errors.Is(err, errSaveNameTooLong) || written != 0 || removed != 0 {
				t.Fatalf("restore wrote %d and removed %d: %v, want the refusal before anything", written, removed, err)
			}
			wantSaves(t, store, map[string]string{"fs/kept": "kept"})
			leftoverExpect(t, parent, []string{leftoverReserved + "/lock=", "owner/fs/kept=kept"}, "after the refusals")
		})
	}
}

// An earlier name whose encoded name would be too long stays where it is, and
// it is written there too, in a folder that was converted around it and in one
// left without the record. A name the first form wrote that this form would
// write too long stays the same way.
func TestAnEarlierNameTooLongToConvertIsWrittenWhereItIs(t *testing.T) {
	long := strings.Repeat("A", 200)
	for _, test := range []struct {
		name  string
		state []string
		after []string
	}{
		{"converted around it", []string{"owner/fs/" + long + "=long", "owner/rms/Option=settings"},
			[]string{encodedRecord, "owner/fs/" + long + "=written again", "owner/rms/#Option=settings"}},
		{"left without the record", []string{"owner/fs/" + long + "=long", "owner/fs/plain=plain"},
			[]string{"owner/fs/" + long + "=written again", "owner/fs/plain=plain"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent := t.TempDir()
			leftoverBuild(t, parent, test.state)
			store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
			wantSaves(t, store, map[string]string{"fs/" + long: "long"})
			if err := store.StoreSave("fs/"+long, []byte("written again")); err != nil {
				t.Fatal(err)
			}
			wantSaves(t, store, map[string]string{"fs/" + long: "written again"})
			if err := store.StoreSaves(map[string][]byte{"fs/" + long: []byte("written again"), "fs/other": []byte("other")}); err != nil {
				t.Fatal(err)
			}
			after := append(slices.Clone(test.after), leftoverReserved+"/lock=", "owner/fs/other=other")
			leftoverExpect(t, parent, after, "after the writes")
			listed, err := ReadSaveTree(filepath.Join(parent, "owner"))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := WriteSaveTree(filepath.Join(parent, "owner"), listed); err != nil {
				t.Fatalf("a restore of the folder's own listing: %v", err)
			}
			leftoverExpect(t, parent, after, "after a restore of its own listing")
		})
	}

	first := strings.Repeat("A", 100) + strings.Repeat("\u037e", 10)
	written := strings.Repeat("#A", 100) + strings.Repeat("\u037e", 10)
	parent := t.TempDir()
	leftoverBuild(t, parent, []string{encodedRecord, "owner/fs/" + written + "=the first form"})
	store := NewDirectorySaveStore(filepath.Join(parent, "owner"))
	wantSaves(t, store, map[string]string{"fs/" + first: "the first form"})
	if err := store.StoreSave("fs/"+first, []byte("written again")); err != nil {
		t.Fatal(err)
	}
	leftoverExpect(t, parent, []string{encodedRecord, leftoverReserved + "/lock=", "owner/fs/" + written + "=written again"}, "after the write")
}

// skipWhereEarlierNamesCannotBe skips a test that builds a folder an earlier
// build wrote with names Windows refuses or changes, which no earlier build
// could have written there.
func skipWhereEarlierNamesCannotBe(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses or changes the earlier names this folder holds")
	}
}

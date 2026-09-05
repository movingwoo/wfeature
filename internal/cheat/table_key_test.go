package cheat

import "testing"

func TestTableReadsTheShapeWrittenBeforeItWasKeyed(t *testing.T) {
	// A table saved before the key existed carries a game name and nothing
	// else. It has to keep loading: the addresses in it were expensive to
	// find, and dropping them because the file has no hash would throw away
	// exactly the work the format exists to hold.
	older := `{"game":"a KTF title","entries":[{"name":"gold","address":4096,"value":500,"type":"u32","frozen":true}]}`
	table, err := UnmarshalTable([]byte(older))
	if err != nil {
		t.Fatalf("an older table did not read: %v", err)
	}
	if table.Game != "a KTF title" || len(table.Entries) != 1 {
		t.Fatalf("read %+v", table)
	}
	if table.Image != "" || table.File != "" {
		t.Fatalf("an unkeyed table came back keyed: %+v", table.TableKey)
	}
	if got := table.Match(TableKey{Image: "abc"}); got != MatchUnkeyed {
		t.Fatalf("an unkeyed table matched as %d, want MatchUnkeyed", got)
	}

	session := NewSession(newTestMemory())
	applied, err := session.LoadTable(table)
	if err != nil || applied != 1 {
		t.Fatalf("LoadTable = %d, %v", applied, err)
	}
}

func TestTableMatchPrefersTheImageOverTheFile(t *testing.T) {
	// Repackaging changes the file and leaves the image alone, so the image is
	// what a patch is true of; the file is the fallback for a platform with no
	// single image to hash.
	table := Table{TableKey: TableKey{Image: "image-a", File: "file-a"}}
	for _, testCase := range []struct {
		name string
		key  TableKey
		want KeyMatch
	}{
		{"the same image in another container", TableKey{Image: "image-a", File: "file-b"}, MatchImage},
		{"no image to compare, the same file", TableKey{File: "file-a"}, MatchFile},
		{"neither", TableKey{Image: "image-b", File: "file-b"}, MatchNone},
		{"a session that cannot say what it runs", TableKey{}, MatchUnkeyed},
	} {
		if got := table.Match(testCase.key); got != testCase.want {
			t.Errorf("%s: Match = %d, want %d", testCase.name, got, testCase.want)
		}
	}
}

func TestSaveTableCarriesTheKeyAndThePatches(t *testing.T) {
	session := NewSession(newTestMemory())
	session.SetTableKey(TableKey{Image: "image-a"})
	// The two halves of the key are known in different places, so setting one
	// must not clear the other.
	session.SetTableKey(TableKey{File: "file-a"})
	if key := session.TableKey(); key.Image != "image-a" || key.File != "file-a" {
		t.Fatalf("key = %+v", key)
	}
	if err := session.ApplyPatch(PatchEntry{
		Name:    "gate",
		Patches: []Patch{{Address: 0x1000, Expect: HexBytes{0, 0}, Replace: HexBytes{1, 2}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Freeze(0x1100, u32Type(t), 500, "gold"); err != nil {
		t.Fatal(err)
	}

	table := session.SaveTable("a KTF title")
	if table.Image != "image-a" || table.File != "file-a" {
		t.Fatalf("saved key = %+v", table.TableKey)
	}
	if len(table.Patches) != 1 || table.Patches[0].Name != "gate" {
		t.Fatalf("saved patches = %+v", table.Patches)
	}

	data, err := MarshalTable(table)
	if err != nil {
		t.Fatal(err)
	}
	read, err := UnmarshalTable(data)
	if err != nil {
		t.Fatalf("a saved table did not read back: %v", err)
	}
	if read.Match(session.TableKey()) != MatchImage {
		t.Fatal("a table saved from this session did not match it")
	}

	// Loading it into a fresh session puts the patch in before the value.
	other := NewSession(newTestMemory())
	if _, err := other.LoadTable(read); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !other.PatchApplied("gate") {
		t.Fatal("the table's patch was not applied")
	}
	if other.Freezes().Len() != 1 {
		t.Fatalf("the table's freezes did not load: %d", other.Freezes().Len())
	}
}

func TestLoadTableStopsBeforeWritingValuesWhenAPatchIsRefused(t *testing.T) {
	memory := newTestMemory()
	memory.writeU32(0x1000, 0xffffffff)
	session := NewSession(memory)

	table := Table{
		Patches: []PatchEntry{{
			Name:    "gate",
			Patches: []Patch{{Address: 0x1000, Expect: HexBytes{0, 0, 0, 0}, Replace: HexBytes{1, 1, 1, 1}}},
		}},
		Entries: []TableEntry{{Name: "gold", Address: 0x1100, Value: 500, Type: "u32", Frozen: true}},
	}
	if _, err := session.LoadTable(table); err == nil {
		t.Fatal("a table whose patch was refused loaded anyway")
	}
	// A refused patch means the table was written against something else, so
	// its addresses should not have been written either.
	value, err := session.ReadValue(0x1100, u32Type(t))
	if err != nil {
		t.Fatal(err)
	}
	if value != 0 {
		t.Fatalf("a value was written after the patch was refused: %d", value)
	}
}

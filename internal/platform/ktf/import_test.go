package ktf

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeSource lays out one file of an external save tree.
func writeSource(t *testing.T, root, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestImportExternalSavesTranslatesLayout covers the four ways the external tree differs
// from this project's: scope above owner instead of below, PID-keyed
// databases, the record identifier glued to the database name, and the
// percent-encoded slash in a database name.
func TestImportExternalSavesTranslatesLayout(t *testing.T) {
	source := t.TempDir()
	saveRoot := t.TempDir()
	writeSource(t, source, "fs/01035D0B/g1", []byte("guest file"))
	writeSource(t, source, "db/PD004517/save0.dat1", []byte("c database"))
	writeSource(t, source, "db/PD004517/data%2FXlsItem.zt11", []byte("packaged"))
	writeSource(t, source, "db/PD004517/NOM21", []byte("record one"))
	writeSource(t, source, "db/PD004517/NOM22", []byte("record two"))

	report, err := ImportExternalSaves(ImportOptions{
		SourceRoot: source,
		SaveRoot:   saveRoot,
		Identities: []GameIdentity{{Descriptor: Descriptor{AID: "01035D0B", PID: "PD004517"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skipped) != 0 {
		t.Fatalf("skipped %v", report.Skipped)
	}

	// Both scopes land under the PID, including the guest files the source keyed
	// by AID.
	store := NewDirectorySaveStore(filepath.Join(saveRoot, "PD004517"))
	for key, want := range map[string]string{
		"fs/g1":               "guest file",
		"db/save0.dat":        "c database",
		"db/data/XlsItem.zt1": "packaged",
	} {
		data, ok := store.LoadSave(key)
		if !ok || string(data) != want {
			t.Fatalf("%s loaded %q ok=%t, want %q", key, data, ok, want)
		}
	}

	// The multi-record database can only be a Java DataBase, so it exists in
	// jdb/ alone, with both records in the source's identifier order.
	if _, ok := store.LoadSave("db/NOM2"); ok {
		t.Fatal("multi-record database was written to the WIPI C scope")
	}
	encoded, ok := store.LoadSave("jdb/NOM2")
	if !ok {
		t.Fatal("jdb/NOM2 is missing")
	}
	records, err := decodeSaveRecords(encoded)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{[]byte("record one"), []byte("record two")}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("records %q, want %q", records, want)
	}
}

// TestImportExternalSavesReportsUnknownOwner keeps an unresolvable PID visible
// instead of dropping a save silently.
func TestImportExternalSavesReportsUnknownOwner(t *testing.T) {
	source := t.TempDir()
	saveRoot := t.TempDir()
	writeSource(t, source, "db/PD999999/slot.sav1", []byte("orphan"))

	report, err := ImportExternalSaves(ImportOptions{SourceRoot: source, SaveRoot: saveRoot})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("imported %v", report.Imported)
	}
	if len(report.Skipped) != 1 {
		t.Fatalf("skipped %v, want one reason", report.Skipped)
	}
}

// TestGroupRecordsRecoversWideIdentifiers covers the ambiguity in the external
// naming: a record identifier of ten or more shares its leading digit with the
// database name, so only the database its siblings claim resolves it.
func TestGroupRecordsRecoversWideIdentifiers(t *testing.T) {
	files := []string{"NOM21", "NOM22", "NOM23", "NOM24", "NOM25", "NOM26", "NOM27", "NOM28", "NOM29", "NOM210", "NOM211"}
	databases, warnings := groupRecords(files)
	if len(warnings) != 0 {
		t.Fatalf("warnings %v", warnings)
	}
	slots, ok := databases["NOM2"]
	if !ok {
		t.Fatalf("databases %v, want NOM2", databases)
	}
	if len(slots) != 11 || slots[9] != "NOM210" || slots[10] != "NOM211" {
		t.Fatalf("slots %v", slots)
	}
}

// TestResolveFileOwnerNeedsTheFilesToDecide covers the AID several games
// share: with no archive shipping any of the names, the owner is unknowable
// and has to be reported rather than picked.
func TestResolveFileOwnerNeedsTheFilesToDecide(t *testing.T) {
	shared := []GameIdentity{
		{Descriptor: Descriptor{AID: "010100D5", PID: "PD002678"}, Path: filepath.Join(t.TempDir(), "missing.zip")},
		{Descriptor: Descriptor{AID: "010100D5", PID: "PD007974"}, Path: filepath.Join(t.TempDir(), "missing.zip")},
	}
	if _, err := resolveFileOwner(shared, "010100D5", []string{"c", "k"}); err == nil {
		t.Fatal("a shared AID with nothing to go on was resolved")
	}
	// One archive with the AID needs no tie-break.
	owner, err := resolveFileOwner(shared[:1], "010100D5", []string{"c"})
	if err != nil {
		t.Fatal(err)
	}
	if owner != "PD002678" {
		t.Fatalf("owner %q, want PD002678", owner)
	}
}

// TestImportExternalSavesDryRunWritesNothing keeps the preview honest.
func TestImportExternalSavesDryRunWritesNothing(t *testing.T) {
	source := t.TempDir()
	saveRoot := t.TempDir()
	writeSource(t, source, "fs/01035D0B/g1", []byte("guest file"))

	report, err := ImportExternalSaves(ImportOptions{
		SourceRoot: source,
		SaveRoot:   saveRoot,
		Identities: []GameIdentity{{Descriptor: Descriptor{AID: "01035D0B", PID: "PD004517"}}},
		DryRun:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 1 {
		t.Fatalf("imported %v, want one entry", report.Imported)
	}
	entries, err := os.ReadDir(saveRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("dry run wrote %v", entries)
	}
}

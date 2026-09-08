package ktf

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// A packaged database is untrusted input: it is a file inside an archive
// somebody downloaded, and this parser reads a record size and a record count
// out of a header before anything has said the file is a database. Both size
// an allocation and both divide the data file, which is what makes the
// arithmetic worth fuzzing rather than only reading.
//
// The seeds are the shapes the parser has branches for — the two packaging
// formats, an index declaring nothing, and the bent header the data-file
// recovery exists for — so the fuzzer starts inside the arithmetic.
func FuzzPackagedRecordDatabaseNeverPanics(f *testing.F) {
	index, data := splitRecordDatabase(4, []byte("aaaa"), []byte("bbbb"))
	f.Add(index, data)
	emptyIndex, emptyData := splitRecordDatabase(100)
	f.Add(emptyIndex, emptyData)
	bent, bentData := splitRecordDatabase(4, []byte("aaaa"), []byte("bbbb"))
	bent[recordDatabaseMagicMatch] = 0xff
	binary.BigEndian.PutUint32(bent[recordDatabaseSizeOffset:], 0xff000004)
	f.Add(bent, bentData)
	f.Add(packedRecordDatabase(8, []byte("YVQZZQEX")), []byte{})
	f.Add([]byte("qtnothing"), []byte{})
	f.Add([]byte{}, []byte{})
	f.Fuzz(func(t *testing.T, index, data []byte) {
		_, runtime := newTestRuntime(t)
		runtime.guestFiles = map[string][]byte{"F.idx": index, "F.db": data}
		records, ok := runtime.packagedRecordDatabase("F", 4)
		if !ok {
			return
		}
		// A database that parsed has to be usable rather than merely
		// non-panicking: every one of these is something the record table goes
		// on to hand a guest.
		if len(records) > maxDataBaseRecords {
			t.Fatalf("parsed %d records, over the %d bound", len(records), maxDataBaseRecords)
		}
		carried := 0
		for id, record := range records {
			if record == nil {
				// A slot with nothing in it is an id that was never used,
				// which is what the one-file format's clear flag means.
				continue
			}
			if len(record) > maxRecordDatabaseBytes {
				t.Fatalf("record %d is %d bytes, over the %d bound", id+1, len(record), maxRecordDatabaseBytes)
			}
			carried += len(record)
		}
		// The split format's records are the data file and nothing else, and
		// the one-file format's are a flag and a record each inside the index.
		if carried > len(data)+len(index) {
			t.Fatalf("records carry %d bytes out of %d of input", carried, len(data)+len(index))
		}
	})
}

// A database name is the other untrusted input. What this pins is the property
// the review kept finding holes in one entry point at a time: a name a table
// accepts has to survive the removal list, must not address one of the lists a
// table keeps, and has to still be a name under its own scope once the save
// boundary has normalized it.
func FuzzDatabaseNameSurvivesTheRemovalList(f *testing.F) {
	for _, name := range []string{
		"save", "save ", "A\nB", ".removed", ".dirs", "./.removed",
		"..", ".", "/", "//", "", strings.Repeat("x", 40),
	} {
		f.Add(javaDatabaseScope, name)
		f.Add(recordDatabaseScope, name)
		f.Add(guestFileScope, name)
		f.Add(cFileScope, name)
	}
	f.Fuzz(func(t *testing.T, scope, name string) {
		if reservedStorageNames[scope] == nil || len(name) > 512 {
			return
		}
		if !storableName(scope, name) {
			return
		}
		// It must not be one of the names a table keeps its own list under.
		if reservedStorageName(scope, name) {
			t.Fatalf("storableName(%q, %q) accepted a name a table keeps its own list under", scope, name)
		}
		// It has to key to a name under its own scope: one that normalizes to
		// the scope itself makes a regular file where the directory belongs,
		// after which every write under it fails for good.
		key, err := backend.NormalizeSaveKey(scope + "/" + name)
		if err != nil {
			t.Fatalf("storableName(%q, %q) accepted a name with no save key: %v", scope, name, err)
		}
		if rest, under := strings.CutPrefix(key, scope+"/"); !under || rest == "" {
			t.Fatalf("storableName(%q, %q) accepted a name that keys to %q", scope, name, key)
		}
		// And it has to come back from the removal list as itself: the list is
		// names joined by newlines, read a line at a time, so a name that does
		// not round trip hides a database nobody deleted. Beside a second name
		// as well, because a name that splits takes its neighbours with it.
		rebuilt := splitRemovalList(joinRemovalList([]string{name, "other"}))
		if len(rebuilt) != 2 {
			t.Fatalf("%q came back from a two-name list as %q", name, rebuilt)
		}
		found := false
		for _, line := range rebuilt {
			if line == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q did not come back from the removal list; it holds %q", name, rebuilt)
		}
	})
}

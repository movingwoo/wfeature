package lgt

import (
	"context"
	"reflect"
	"testing"
)

// openFixtureDatabase opens a database on a client with a store behind it, the
// way a title's startApp does.
func openFixtureDatabase(t *testing.T, client *Client, name string, recordSize uint32) uint32 {
	t.Helper()
	handle, err := client.newJavaString(name)
	if err != nil {
		t.Fatal(err)
	}
	object, err := javaOpenDataBase(client, nil, nil, []uint32{handle, recordSize, 1})
	if err != nil {
		t.Fatalf("openDataBase(%q) failed: %v", name, err)
	}
	if object == 0 {
		t.Fatalf("openDataBase(%q) answered null", name)
	}
	return object
}

func databaseRecord(t *testing.T, client *Client, object, identifier uint32) []byte {
	t.Helper()
	array, err := javaSelectRecord(client, nil, nil, []uint32{object, identifier})
	if err != nil {
		t.Fatalf("selectRecord(%d) failed: %v", identifier, err)
	}
	data, err := client.readJavaArrayBytes(array)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func insertFixtureRecord(t *testing.T, client *Client, object uint32, record []byte) uint32 {
	t.Helper()
	array, err := client.newJavaByteArray(record)
	if err != nil {
		t.Fatal(err)
	}
	identifier, err := javaInsertRecord(client, nil, nil, []uint32{object, array})
	if err != nil {
		t.Fatalf("insertRecord failed: %v", err)
	}
	return identifier
}

// A save written through the record store has to come back through it, because
// that is the whole of what the class is for.
func TestJavaDatabaseRoundTripsARecord(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	object := openFixtureDatabase(t, client, "save", 64)

	first := insertFixtureRecord(t, client, object, []byte("alpha"))
	second := insertFixtureRecord(t, client, object, []byte("beta"))
	if first != 0 || second != 1 {
		t.Fatalf("record identifiers are %d and %d, want 0 and 1", first, second)
	}
	if got := string(databaseRecord(t, client, object, first)); got != "alpha" {
		t.Fatalf("record 0 = %q, want %q", got, "alpha")
	}

	updated, err := client.newJavaByteArray([]byte("gamma"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaUpdateRecord(client, nil, nil, []uint32{object, first, updated}); err != nil {
		t.Fatal(err)
	}
	if got := string(databaseRecord(t, client, object, first)); got != "gamma" {
		t.Fatalf("the updated record 0 = %q, want %q", got, "gamma")
	}

	count, err := javaDataBaseRecordCount(client, nil, nil, []uint32{object})
	if err != nil || count != 2 {
		t.Fatalf("getNumberOfRecords() = %d (%v), want 2", count, err)
	}
}

// A deleted record's identifier is reused, which the specification states and a
// title that walks identifiers depends on.
func TestJavaDatabaseReusesADeletedRecordIdentifier(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	object := openFixtureDatabase(t, client, "save", 64)

	insertFixtureRecord(t, client, object, []byte("one"))
	insertFixtureRecord(t, client, object, []byte("two"))
	if _, err := javaDeleteRecord(client, nil, nil, []uint32{object, 0}); err != nil {
		t.Fatal(err)
	}
	count, err := javaDataBaseRecordCount(client, nil, nil, []uint32{object})
	if err != nil || count != 1 {
		t.Fatalf("getNumberOfRecords() after a delete = %d (%v), want 1", count, err)
	}
	if identifier := insertFixtureRecord(t, client, object, []byte("three")); identifier != 0 {
		t.Fatalf("the next record took identifier %d, want the deleted 0", identifier)
	}
	if got := string(databaseRecord(t, client, object, 1)); got != "two" {
		t.Fatalf("record 1 moved: %q", got)
	}
}

// The store is what a database survives in, so a second session over the same
// store has to read what the first one wrote.
func TestJavaDatabaseSurvivesTheSession(t *testing.T) {
	store := newMemorySaveStore()
	first := fixtureClient(t)
	first.saveStore = store
	object := openFixtureDatabase(t, first, "save", 64)
	insertFixtureRecord(t, first, object, []byte("kept"))
	if _, err := javaCloseDataBase(first, nil, nil, []uint32{object}); err != nil {
		t.Fatal(err)
	}

	second := fixtureClient(t)
	second.saveStore = store
	reopened := openFixtureDatabase(t, second, "save", 64)
	if got := string(databaseRecord(t, second, reopened, 0)); got != "kept" {
		t.Fatalf("the reopened record 0 = %q, want %q", got, "kept")
	}
	names, err := javaListDataBases(second, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if names == 0 {
		t.Fatal("listDataBases answered null")
	}
	if listed := second.databaseNames(); len(listed) != 1 || listed[0] != "save" {
		t.Fatalf("listDataBases = %v, want [save]", listed)
	}
}

// A record longer than the size the database was created with is stored whole:
// the common contract does not enforce the record size, because a title that
// has always written such records here would lose its save on the next write.
// A container that is not one is refused rather than read as an empty
// database.
func TestJavaDatabaseRefusesWhatItCannotHold(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	object := openFixtureDatabase(t, client, "save", 4)

	array, err := client.newJavaByteArray([]byte("far too long"))
	if err != nil {
		t.Fatal(err)
	}
	identifier, err := javaInsertRecord(client, nil, nil, []uint32{object, array})
	if err != nil {
		t.Fatalf("a record longer than the record size was refused: %v", err)
	}
	if got := string(databaseRecord(t, client, object, identifier)); got != "far too long" {
		t.Fatalf("the long record reads %q", got)
	}

	database := &databaseContent{}
	if err := database.decode([]byte("not a database at all")); err == nil {
		t.Fatal("a file that is not a container was read as one")
	}
}

// A database name is read as a file's name: the spellings of one path are one
// database, and a name that would leave the title's own area, or names that
// area itself, is refused.
func TestJavaDatabaseNameIsAPath(t *testing.T) {
	for name, want := range map[string]string{
		"save": "save", "/save": "save", "./save": "save", "\\save": "save",
		"a/b": "a/b", "a\\b": "a/b", "//a/./b/": "a/b", " save ": " save ",
	} {
		if got, err := databasePath(name); err != nil || got != want {
			t.Errorf("databasePath(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, name := range []string{"", "/", ".", "..\\escape", "a/../b", "a\nb", "__databases__", "/.created/x"} {
		if got, err := databasePath(name); err == nil {
			t.Errorf("%q was accepted as the database %q", name, got)
		}
	}
}

// Two DataBase objects on one database are one store of records: what one
// saves, the other reads and keeps, and neither puts an older copy back.
func TestTwoDataBaseObjectsShareTheirRecords(t *testing.T) {
	ctx := context.Background()
	stored := func(t *testing.T, store *faultStore) []string {
		t.Helper()
		data, ok := store.MemorySaveStore.LoadSave("jdb/save.db")
		if !ok {
			return nil
		}
		database := &databaseContent{}
		if err := database.decode(data); err != nil {
			t.Fatal(err)
		}
		var records []string
		for index, record := range database.records {
			if !database.deleted[index] {
				records = append(records, string(record))
			}
		}
		return records
	}
	closeDatabase := func(t *testing.T, client *Client, object uint32) {
		t.Helper()
		if _, err := javaCloseDataBase(client, ctx, client.thread, []uint32{object}); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("a close after another object saved", func(t *testing.T) {
		client, store, _ := fileFixture(t, nil)
		settings := openFixtureDatabase(t, client, "save", 16)
		insertFixtureRecord(t, client, settings, []byte("settings"))
		progress := openFixtureDatabase(t, client, "save", 16)
		insertFixtureRecord(t, client, progress, []byte("progress"))
		closeDatabase(t, client, progress)
		closeDatabase(t, client, settings)
		if got := stored(t, store); !reflect.DeepEqual(got, []string{"settings", "progress"}) {
			t.Fatalf("the store holds %q", got)
		}
	})
	t.Run("an update after another object saved", func(t *testing.T) {
		client, store, _ := fileFixture(t, nil)
		first := openFixtureDatabase(t, client, "save", 16)
		insertFixtureRecord(t, client, first, []byte("one"))
		second := openFixtureDatabase(t, client, "save", 16)
		insertFixtureRecord(t, client, second, []byte("two"))
		closeDatabase(t, client, second)
		if count, _ := javaDataBaseRecordCount(client, ctx, client.thread, []uint32{first}); count != 2 {
			t.Fatalf("the first object counts %d records", count)
		}
		array, err := client.newJavaByteArray([]byte("uno"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := javaUpdateRecord(client, ctx, client.thread, []uint32{first, 0, array}); err != nil {
			t.Fatal(err)
		}
		if got := stored(t, store); !reflect.DeepEqual(got, []string{"uno", "two"}) {
			t.Fatalf("the store holds %q", got)
		}
	})
	t.Run("a close after the database was deleted", func(t *testing.T) {
		client, store, _ := fileFixture(t, nil)
		object := openFixtureDatabase(t, client, "save", 16)
		insertFixtureRecord(t, client, object, []byte("old"))
		if _, err := javaDeleteDataBase(client, ctx, client.thread, []uint32{newTestString(t, client, "save")}); err != nil {
			t.Fatal(err)
		}
		attempts := store.attempts
		closeDatabase(t, client, object)
		if _, exists := client.readFile(databaseFileName("save")); exists || store.attempts != attempts {
			t.Fatal("closing an object the title changed nothing through brought a deleted database back")
		}
	})
	t.Run("a close retries a refused store", func(t *testing.T) {
		client, store, _ := fileFixture(t, nil)
		object := openFixtureDatabase(t, client, "save", 16)
		store.refuse = true
		if _, err := javaInsertRecord(client, ctx, client.thread, []uint32{object, mustByteArray(t, client, "kept")}); err != nil {
			t.Fatal(err)
		}
		store.refuse = false
		closeDatabase(t, client, object)
		if got := stored(t, store); !reflect.DeepEqual(got, []string{"kept"}) {
			t.Fatalf("the close stored %q", got)
		}
	})
}

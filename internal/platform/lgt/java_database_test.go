package lgt

import (
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

// A record longer than the size the database was created with is the one limit
// the specification puts on a record, and a container that is not one is
// refused rather than read as an empty database.
func TestJavaDatabaseRefusesWhatItCannotHold(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = newMemorySaveStore()
	object := openFixtureDatabase(t, client, "save", 4)

	array, err := client.newJavaByteArray([]byte("far too long"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaInsertRecord(client, nil, nil, []uint32{object, array}); err == nil {
		t.Fatal("a record longer than the record size was stored")
	}

	database := &javaDatabase{}
	if err := database.decode([]byte("not a database at all")); err == nil {
		t.Fatal("a file that is not a container was read as one")
	}
}

// A name that is a path would put a title's save somewhere else in the store.
func TestJavaDatabaseRefusesANameThatIsAPath(t *testing.T) {
	for _, name := range []string{"", "a/b", "..\\escape"} {
		if err := validDatabaseName(name); err == nil {
			t.Fatalf("%q was accepted as a database name", name)
		}
	}
}

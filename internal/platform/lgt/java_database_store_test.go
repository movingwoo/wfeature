package lgt

import (
	"errors"
	"strings"
	"testing"
)

// earlierLayout is a store as earlier builds left it: each database's
// container among the title's files, the index of names beside them, and a
// title's own file.
func earlierLayout(t *testing.T) *faultStore {
	t.Helper()
	container := func(records ...string) string {
		content := &databaseContent{recordSize: 8}
		for _, record := range records {
			content.records = append(content.records, []byte(record))
			content.deleted = append(content.deleted, false)
		}
		return string(content.encode())
	}
	return newFaultStore(t, map[string]string{
		fileDatabaseIndexKey:         "save\nrank",
		fileSaveScope + "save.db":    container("progress"),
		fileSaveScope + "rank.db":    container("1st", "2nd"),
		fileSaveScope + "notes.txt":  "the title's own file",
		fileSaveScope + "stray.data": "x",
	})
}

// refusingBatchStore is a faultStore whose refusal reaches a batch too.
type refusingBatchStore struct{ *faultStore }

func (store refusingBatchStore) StoreSaves(entries map[string][]byte) error {
	store.attempts++
	if store.refuse {
		return errors.New("the fixture store refuses the batch")
	}
	return store.faultStore.MemorySaveStore.StoreSaves(entries)
}

// A store an earlier build wrote moves into the database scope the first time
// a session reads its databases, once: the records are where they were, the
// files no longer hold the containers, a title's own file of a container's
// name is a file of its own from then on, and a later session finds the move
// made rather than making it again.
func TestDatabasesMoveOutOfTheFilesOnce(t *testing.T) {
	store := earlierLayout(t)
	before := map[string]string{"save": store.held(fileSaveScope + "save.db"), "rank": store.held(fileSaveScope + "rank.db")}
	client := fixtureClient(t)
	client.saveStore = store
	fixture := databaseFixture{t: t, client: client}
	if got := string(databaseRecord(t, client, fixture.mustOpen("save", 8, false), 0)); got != "progress" {
		t.Fatalf("after the move the database reads %q", got)
	}
	for name, data := range before {
		if moved := store.held(databaseSaveKey(name)); moved != data {
			t.Errorf("the container of %q moved as %q, want its bytes unchanged", name, moved)
		}
	}
	if index := store.held(databaseIndexKey); index != "rank\nsave" {
		t.Errorf("the index in the scope reads %q", index)
	}
	files := newContractFiles(t, client)
	for _, name := range []string{"save.db", "rank.db"} {
		if code := files.code(slotFsIsExist, name, 1); code != wipiNoEntry {
			t.Errorf("MC_fsIsExist(%q) after the move = %d, want M_E_NOENT", name, code)
		}
	}
	if code := files.code(slotFsIsExist, "notes.txt", 1); code != wipiSuccess {
		t.Errorf("the title's own file is gone after the move: %d", code)
	}
	if names := fixture.names(); strings.Join(names, ",") != "rank,save" {
		t.Errorf("listDataBases after the move = %q", names)
	}

	// The title's own file of a container's old name is a file of its own.
	files.save("save.db", "a title file")
	if code := files.code(slotFsIsExist, "save.db", 1); code != wipiSuccess {
		t.Fatalf("a file save.db written after the move is not there: %d", code)
	}
	if got := string(databaseRecord(t, client, fixture.mustOpen("save", 8, false), 0)); got != "progress" {
		t.Fatalf("the title's file save.db changed the database: %q", got)
	}

	// A later session finds the move made.
	attempts := store.attempts
	next := fixtureClient(t)
	next.saveStore = store
	later := databaseFixture{t: t, client: next}
	if got := string(databaseRecord(t, next, later.mustOpen("rank", 8, false), 1)); got != "2nd" {
		t.Fatalf("in a later session the moved database reads %q", got)
	}
	if store.attempts != attempts {
		t.Errorf("a later session wrote %d times reading a moved store", store.attempts-attempts)
	}
	if data, ok := next.readFile("save.db"); !ok || string(data) != "a title file" {
		t.Errorf("in a later session the title's save.db reads %q, %t", data, ok)
	}
}

// A store that refuses the move changes nothing: the session reads and writes
// its databases where earlier builds kept them, the file calls still do not
// see them there, and the next session that can moves them.
func TestARefusedMoveKeepsTheEarlierLayout(t *testing.T) {
	store := earlierLayout(t)
	refusing := refusingBatchStore{store}
	store.refuse = true
	client := fixtureClient(t)
	client.saveStore = refusing
	fixture := databaseFixture{t: t, client: client}
	if got := string(databaseRecord(t, client, fixture.mustOpen("save", 8, false), 0)); got != "progress" {
		t.Fatalf("with the move refused the database reads %q", got)
	}
	if index := store.held(databaseIndexKey); index != "<absent>" {
		t.Fatalf("a refused move left an index in the scope: %q", index)
	}
	if code := newContractFiles(t, client).code(slotFsIsExist, "save.db", 1); code != wipiNoEntry {
		t.Errorf("with the move refused MC_fsIsExist(save.db) = %d, want M_E_NOENT", code)
	}

	store.refuse = false
	next := fixtureClient(t)
	next.saveStore = refusing
	later := databaseFixture{t: t, client: next}
	if got := string(databaseRecord(t, next, later.mustOpen("rank", 8, false), 0)); got != "1st" {
		t.Fatalf("after the move in a later session the database reads %q", got)
	}
	if store.held(databaseSaveKey("save")) == "<absent>" || store.held(databaseIndexKey) != "rank\nsave" {
		t.Fatal("a later session that could move the databases did not")
	}
}

// A quick save taken before the move holds the databases by name, so a load
// after it fills them from the scope.
func TestAQuickSaveFromBeforeTheMoveLoadsAfterIt(t *testing.T) {
	store := earlierLayout(t)
	refusing := refusingBatchStore{store}
	store.refuse = true
	fixture := newJavaThreadFixture(t, refusing)
	database := openFixtureDatabase(t, fixture.client, "save", 8)
	if got := string(databaseRecord(t, fixture.client, database, 0)); got != "progress" {
		t.Fatalf("before the move the database reads %q", got)
	}
	fixture.settle()
	store.refuse = false
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)

	// Another session moves the store, and the quick save loads over it.
	mover := fixtureClient(t)
	mover.saveStore = refusing
	databaseFixture{t: t, client: mover}.mustOpen("rank", 8, false)
	if store.held(databaseIndexKey) != "rank\nsave" {
		t.Fatal("the other session did not move the store")
	}
	restoredSession, err := commitFixture(t, fixture.archive, checkpoint, fixture.session, refusing)
	if err != nil {
		t.Fatalf("a quick save from before the move = %v", err)
	}
	restored := restoredSession.client
	for object, held := range restored.javaRun.databases {
		if held.name == "save" {
			if got := string(databaseRecord(t, restored, object, 0)); got != "progress" {
				t.Fatalf("the restored database reads %q", got)
			}
			return
		}
	}
	t.Fatal("the restored session holds no database save")
}

package lgt

import (
	"bytes"
	"maps"
	"slices"
	"strings"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Where a title's databases are kept
//
// **A database has a scope of its own, `jdb/`, apart from the title's files**,
// as KTF and SKT keep theirs (docs/wipi-storage.md). The container of the
// database "save" is the save `jdb/save.db`, and the index of names that
// listDataBases reads is `jdb/.index`. A title's file "save.db" and its
// database "save" are two things, and the space count takes the database
// once, as its records. The container keeps this runtime's format; the suffix
// keeps a database "a" and a database "a/b" apart in a store that can make
// "jdb/a" a file or a directory but not both.
//
// The store has no delete, so a deleted database's container is written
// empty: no container is, since every one starts with its header.
//
// **Earlier builds kept each container among the title's files**, as
// `fs/<name>.db`, with the index at `fs/__databases__.db`. The first time a
// running session over a real store reads its databases, a store still in
// that layout is moved, once (moveDatabases). A session whose store refuses
// the move reads and writes its databases where those builds did, and the file
// calls do not see the containers there (databaseContainers); the next
// session asks the store again.

const (
	// databaseScope is the scope every database's container is kept in.
	databaseScope = "jdb/"
	// databaseIndexKey is the index of the databases' names, one to a line,
	// beside them. No container is at it, since a container ends in
	// databaseSuffix, and databasePath refuses a name that would put one
	// below it (databaseIndexSegment).
	databaseIndexKey = databaseScope + databaseIndexSegment
	// databaseIndexSegment is the index's name in the scope, which no
	// database name may begin with as a directory.
	databaseIndexSegment = ".index"
	// fileDatabaseIndexKey is where earlier builds kept the index, among the
	// files. fileSaveKey still gives no title's file that key.
	fileDatabaseIndexKey = fileSaveScope + databaseIndexName
)

// databaseLayout is where a session's databases are kept.
type databaseLayout uint8

const (
	// databasesUndecided is a session that has not read its databases yet.
	databasesUndecided databaseLayout = iota
	// databasesInScope keeps them in databaseScope.
	databasesInScope
	// databasesAmongFiles keeps them where earlier builds did, for a session
	// whose store refused the move.
	databasesAmongFiles
)

// databaseSaveKey is the save key of a database's container in its scope.
func databaseSaveKey(name string) string {
	return databaseScope + name + databaseSuffix
}

// databasesAmongFiles reports whether this session's databases are still kept
// among the files, deciding it, and moving a store earlier builds wrote, the
// first time a running session asks (decideDatabaseLayout).
//
// **A load being checked or rebuilt decides nothing for the session it will
// become.** It reads the saves as they are, and writes none (RebuildReader
// refuses a write and fails the load), so it reads a store not yet moved where
// earlier builds kept the databases, every time it asks; the session that
// runs decides at its own first read, over the live store, where the move can
// be made. The records a checkpoint holds name a database, not where it is
// kept, so one taken before the move is filled from the scope after it.
func (client *Client) databasesAmongFiles() bool {
	layout := client.databaseLayout
	if layout == databasesUndecided {
		layout = client.decideDatabaseLayout()
		if client.restoredStorage == nil {
			client.databaseLayout = layout
		}
	}
	return layout == databasesAmongFiles
}

// decideDatabaseLayout answers where the databases are: in the scope when it
// has an index, when nothing was kept among the files, and in a session with
// no store or whose reads failed; otherwise among the files, which a running
// session moves (moveDatabases) and keeps using only when the store refuses.
func (client *Client) decideDatabaseLayout() databaseLayout {
	if client.storeless() {
		return databasesInScope
	}
	if _, moved := client.loadSave(databaseIndexKey); moved || client.saveReadError != nil {
		return databasesInScope
	}
	earlier, found := client.loadSave(fileDatabaseIndexKey)
	if !found || client.saveReadError != nil {
		return databasesInScope
	}
	if client.restoredStorage != nil {
		return databasesAmongFiles
	}
	if err := client.moveDatabases(earlier); err != nil {
		if client.logger != nil {
			client.logger.Debug("LGT databases stay where earlier builds kept them: the store refused the move", "error", err)
		}
		return databasesAmongFiles
	}
	return databasesInScope
}

// moveDatabases moves a store from the layout earlier builds wrote into the
// database scope. It is made once for a store: the index it writes in the
// scope is what every later session finds first.
//
// The databases are the names the earlier index lists, and the containers the
// list of created paths names that the index lost — a database deleted under
// an object and written through it afterwards, which those builds did not
// list again — whose bytes the store holds and the removal list does not
// hide. A container the index lists moves as it is; one only the created list
// names moves only if it reads as a container, since it could be a title's
// own file. A name no database can have now stays where it is, a file.
//
// **The order keeps every save in one place or the other.** The containers
// and the index are given to the store as one batch, and only once it has
// taken them are the old paths put on the removal list, which hides them: a
// title's own later file of such a name is then a file of its own. A batch the
// store refuses moves nothing and changes nothing, and the session goes on in
// the old layout. A removal list the store refuses after the batch hides the
// old paths in this session and waits for the ending and a quick step, as any
// refused list does. The old index stays as it is: it has no save key, so the
// removal list cannot hold it, no file call can name it, and nothing reads it
// once the scope has an index.
func (client *Client) moveDatabases(earlier []byte) error {
	listed := make(map[string]bool)
	for _, name := range strings.Split(string(earlier), "\n") {
		if name != "" {
			listed[name] = true
		}
	}
	candidates := maps.Clone(listed)
	consider := func(path string) {
		if name, container := strings.CutSuffix(path, databaseSuffix); container && name != "" {
			candidates[name] = true
		}
	}
	for path := range client.createdFiles() {
		consider(path)
	}
	for entry := range client.createdLegacy {
		consider(fileListKey(entry))
	}
	if client.saveReadError != nil {
		return client.saveReadError
	}
	batch := make(map[string][]byte, len(candidates)+1)
	moved := make([]string, 0, len(candidates))
	for _, name := range slices.Sorted(maps.Keys(candidates)) {
		if path, err := databasePath(name); err != nil || path != name {
			continue
		}
		container := databaseFileName(name)
		key, err := fileSaveKey(container)
		if err != nil || client.fileRemoved(container) {
			continue
		}
		data, stored := client.loadSave(key)
		if client.saveReadError != nil {
			return client.saveReadError
		}
		if !stored || !listed[name] && (&databaseContent{}).decode(data) != nil {
			continue
		}
		batch[databaseSaveKey(name)] = data
		moved = append(moved, name)
	}
	batch[databaseIndexKey] = []byte(strings.Join(moved, "\n"))
	if err := backend.StoreSaves(client.saveStore, batch); err != nil {
		return err
	}
	if len(moved) == 0 {
		return nil
	}
	removed := client.removedFiles()
	for _, name := range moved {
		container := databaseFileName(name)
		removed[fileListKey(container)] = true
		client.bumpFileEpoch(container)
		client.forgetSessionFile(container)
	}
	if err := client.storeFileList(fileRemovedKey); err != nil && client.logger != nil {
		client.logger.Debug("LGT removal list store failed after the databases moved", "error", err)
	}
	if client.logger != nil {
		client.logger.Debug("LGT databases moved into their own scope", "databases", len(moved))
	}
	return nil
}

// readDatabase answers a database's container and whether the database is
// there. In its scope an empty entry is one the title deleted.
func (client *Client) readDatabase(name string) ([]byte, bool) {
	if client.databasesAmongFiles() {
		return client.readFile(databaseFileName(name))
	}
	key := databaseSaveKey(name)
	var data []byte
	stored := false
	if client.saveStore != nil {
		data, stored = client.loadSave(key)
	}
	if !stored {
		if kept, ok := client.sessionFiles[key]; ok {
			data, stored = bytes.Clone(kept), true
		}
	}
	if !stored || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// writeDatabase gives the store a database's container, and answers a store
// that refuses it.
func (client *Client) writeDatabase(name string, data []byte) error {
	if client.databasesAmongFiles() {
		return client.storeFile(databaseFileName(name), data)
	}
	// A session whose reads failed holds its writes back, as storeFile does.
	if client.saveReadError != nil {
		return nil
	}
	return client.storeDatabaseEntry(name, data)
}

// deleteDatabase deletes a database's container, and answers a store that
// refuses it, which leaves the database as it was. In its scope the entry is
// written empty; a session whose reads failed cannot write that, and refuses
// the deletion.
func (client *Client) deleteDatabase(name string) error {
	if client.databasesAmongFiles() {
		return client.removeFile(databaseFileName(name))
	}
	if client.saveReadError != nil {
		return client.saveReadError
	}
	return client.storeDatabaseEntry(name, []byte{})
}

func (client *Client) storeDatabaseEntry(name string, data []byte) error {
	if err := client.storeSessionSave(databaseSaveKey(name), data); err != nil {
		if client.logger != nil {
			client.logger.Debug("LGT database store failed", "name", name, "error", err)
		}
		return err
	}
	client.bumpDatabaseEpoch(name)
	return nil
}

// databaseEpoch counts how often a database's container has changed under the
// session, which the records an object holds are compared with
// (javaDatabaseOf). Among the files it is the container path's own count.
func (client *Client) databaseEpoch(name string) uint64 {
	if client.databasesAmongFiles() {
		return client.fileEpoch(databaseFileName(name))
	}
	return client.databaseEpochs[name]
}

func (client *Client) bumpDatabaseEpoch(name string) {
	if client.databaseEpochs == nil {
		client.databaseEpochs = make(map[string]uint64)
	}
	client.databaseEpochs[name]++
}

// databaseIndexSaveKey is the key the index of names is kept at in this
// session's layout.
func (client *Client) databaseIndexSaveKey() string {
	if client.databasesAmongFiles() {
		return fileDatabaseIndexKey
	}
	return databaseIndexKey
}

// databaseRecordKey is the key and the epoch the space count keeps a
// database's figure under (databaseRecordBytes): its container's, in the
// session's layout.
func (client *Client) databaseRecordKey(name string) (string, uint64, bool) {
	if !client.databasesAmongFiles() {
		return databaseSaveKey(name), client.databaseEpochs[name], true
	}
	container := databaseFileName(name)
	key, err := fileSaveKey(container)
	return key, client.fileEpoch(container), err == nil
}

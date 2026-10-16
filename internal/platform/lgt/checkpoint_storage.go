package lgt

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Quick save, quick load and the saves
//
// A checkpoint is the title's execution state and nothing else. The slot
// carries no save and a load replaces none: what the title saved after the
// checkpoint is still there after a load, and the restored title reads and
// writes the saves as the store has them then.
//
// Two things on this platform stand between that rule and the code.
//
// **A write is in a buffer until its file is closed.** A title that has
// written to a file and not closed it has issued a write the store does not
// have. A record holds no save bytes, so such a write would be in no place at
// all once the session that made it is gone; and a load displaces a session.
// So both steps give the store what it has not been given first
// (storeIssuedWrites), and either is refused when the store refuses. That is
// what ending the game does to storage, made at this boundary instead.
//
// **The host keeps copies of what a title has open.** An open file's buffer, a
// DataBase object's records, the window of a stream opened on a file, the two
// path lists, and what an authentication adapter read from its file. Each
// answers the title before the store is asked, and a file is written back
// whole. Restored from a record, a copy would answer with what the file held
// when the checkpoint was taken and then be written over the file as it is
// now. So a record names these objects and holds nothing of them, and
// committing a load fills each from the store, through the lookup an open
// makes (bindRestoredStorage).
//
// The rules, which are the other platforms' too:
//
//   - A cursor stays where the record has it, even past the end of a file
//     that is shorter now.
//   - A path the store has nothing for leaves its object empty, and the host
//     makes nothing: the title's next write does.
//   - The handles open on one path share one buffer again, as the objects on
//     one database share one set of records.
//   - A handle whose open asked for an empty file takes at most what the file
//     held at the quick save, from the front of the file as it is now: the
//     open emptied the store, so that is what the title had written since. A
//     handle whose open made a file that was not there is given the whole
//     file, like any other. See openFile.truncated.
//   - A save that cannot be read, or a container that does not decode,
//     refuses the load, and what is read is bounded: the names come from a
//     slot.
//
// What stays in a record, because it was never in a file: the bytes a title
// has written into a byte sink of its own, and what an authentication adapter
// made for the run. A stream opened on a File writes the File as it is
// written to, so its bytes are in the File's buffer and stored with it. A
// record an earlier build took can still hold bytes such a stream had not
// handed its File; the load puts them in the File (rebuildRestoredStorage).

// restoredStorage is what a record says about open storage that a rebuild
// needs and the objects themselves cannot keep, because a rebuild overwrites
// it: a rebuild that is refused part way is started again from these.
type restoredStorage struct {
	// lengths is, for a handle whose open asked for an empty file, how many
	// bytes it held when the checkpoint was taken.
	lengths map[uint32]int
	// recordSizes is the size each open database was opened with, which is
	// what an absent container leaves it with.
	recordSizes map[uint32]uint32
	// cursors is where each stream on a file had read to, and its mark.
	cursors map[uint32][2]int
	// fileCursors is where each open file's cursor was when the checkpoint
	// was taken. Putting a stream's bytes into a File moves its cursor on,
	// and every attempt at a rebuild starts from the record's own.
	fileCursors map[uint32]int
	// sinkBytes holds, for a stream opened on a File, the bytes a record an
	// earlier build took kept in the stream. That build held a stream's
	// writes until a flush; this one writes the File as the stream is written
	// to, so a load puts them in the File, as if they were written then.
	sinkBytes map[uint32][]byte
	// budget is how many bytes the rebuilt objects may take from the store
	// between them, and how many may be read to fill them. Zero is
	// maxStateBytes.
	budget uint64
}

func (restored *restoredStorage) limit() uint64 {
	if restored == nil || restored.budget == 0 {
		return maxStateBytes
	}
	return restored.budget
}

// storageBytes is how much the open storage objects hold between them: what a
// load would keep for them if nothing changed. A quick save is refused over
// the limit a load has, so that a slot is not written that a load over the
// same saves would refuse for its size.
//
// The two do not count exactly the same things. A load also reads the two
// path lists and a database's container framing, and reads the whole of a
// file whose handle keeps only a part of it. The difference is a few bytes
// per object against a limit of megabytes, and a save that grows after the
// quick save can take a load over the limit whatever was counted here.
func (client *Client) storageBytes() uint64 {
	var total uint64
	for _, file := range client.openContents() {
		total += uint64(len(file.data))
	}
	if client.javaRun == nil {
		return total
	}
	databases, open := client.databaseContents()
	for _, database := range databases {
		if !open[database.databaseContent] {
			continue
		}
		for _, record := range database.records {
			total += uint64(len(record))
		}
	}
	for _, stream := range client.javaRun.streams {
		if stream != nil && stream.File {
			total += uint64(len(stream.Data))
		}
	}
	return total
}

// issuedWritesBlocked names what keeps the title's unstored writes from being
// stored by the host. It changes nothing.
//
// A dirty buffer is the file as it was when the buffer was filled plus what
// the title wrote into it, and storing it stores all of it. The handles on a
// path share the buffer, and a rename or a removal of the path brings it
// level, so only a store that is not the buffer's moves the file past it: a
// DataBase written under the same name. Storing the buffer then would put the
// older part of it over newer content, and the host would be the one doing
// it. The title's own close does exactly that and is the title's to do; a
// quick step is not.
func (client *Client) issuedWritesBlocked() error {
	dirty := make(map[string]string)
	for _, file := range client.openContents() {
		if !file.dirty {
			continue
		}
		if _, err := fileSaveKey(file.name); err != nil {
			return fmt.Errorf("the open file %s has unstored writes and no save to store them in", strconv.Quote(file.name))
		}
		dirty[fileListKey(file.name)] = file.name
		if file.synced != client.fileEpoch(file.name) {
			return fmt.Errorf("the open file %s has unstored writes and its save has changed since it was opened", strconv.Quote(file.name))
		}
	}
	databases, open := client.databaseContents()
	unsaved := make(map[string]bool)
	for _, database := range databases {
		if !database.unsaved {
			continue
		}
		behind := database.synced != client.fileEpoch(databaseFileName(database.name))
		if behind && !open[database.databaseContent] {
			// Its last store was refused at the close, and the container has
			// been written since. Nothing can store this one any more, and
			// storeIssuedWrites lets it go.
			continue
		}
		if behind {
			return fmt.Errorf("the database %s has an unstored write and its container has changed since", strconv.Quote(database.name))
		}
		// A File holding the same container with its own unstored write would
		// be stored over it or under it, and the host would be choosing which
		// of the title's writes is lost.
		key := fileListKey(databaseFileName(database.name))
		if file, shared := dirty[key]; shared {
			return fmt.Errorf("the open file %s and database %s share one save and both have unstored writes", strconv.Quote(file), strconv.Quote(database.name))
		}
		if unsaved[key] {
			return fmt.Errorf("two objects of the database %s both have an unstored write", strconv.Quote(database.name))
		}
		unsaved[key] = true
	}
	return nil
}

// prepareIssuedWriteLists reads the lists storeFile will need before any write
// is attempted. A checkpoint's failed read must not install an empty list or
// a sticky read error in the live session. Existing lists are only inspected;
// newly read lists are adopted together after both reads succeed.
func (client *Client) prepareIssuedWriteLists() error {
	pending := client.removedUnsaved || client.createdUnsaved
	needRemoved, needCreated := false, false
	needsFile := func(name string) {
		pending, needRemoved = true, true
		if _, packaged := client.archive.Resource(name); !packaged {
			needCreated = true
		}
	}
	for _, file := range client.openContents() {
		if file.dirty {
			needsFile(file.name)
		}
	}
	databases, open := client.databaseContents()
	for _, database := range databases {
		if !database.unsaved {
			continue
		}
		name := databaseFileName(database.name)
		if !open[database.databaseContent] && database.synced != client.fileEpoch(name) {
			continue
		}
		needsFile(name)
	}
	if !pending {
		return nil
	}
	if client.saveReadError != nil {
		return client.saveReadError
	}
	view := &Client{saveStore: client.saveStore, removed: client.removed, removedLegacy: client.removedLegacy,
		created: client.created, createdLegacy: client.createdLegacy}
	if needRemoved {
		view.removedFiles()
	}
	if needCreated {
		view.createdFiles()
	}
	if view.saveReadError != nil {
		return view.saveReadError
	}
	client.removed, client.removedLegacy = view.removed, view.removedLegacy
	client.created, client.createdLegacy = view.created, view.createdLegacy
	return nil
}

// storeIssuedWrites gives the store every write the title has issued and the
// store does not have: the two path lists and the databases whose last store
// was refused, and every open file with writes in its buffer. It answers how
// many things it stored, and an error that says the saves could not be
// written when the store refuses one or when issuedWritesBlocked names one.
//
// A file stays open, at its cursor; only its buffer becomes clean. A stream
// opened on a File writes the File as it is written to, so what a title wrote
// through one is in that buffer and is stored with it.
//
// A session whose writes are held back by a failed save read stores nothing,
// here as anywhere. Pending writes refuse the step rather than being lost on
// a load; a session with none may still recover by loading a checkpoint.
func (client *Client) storeIssuedWrites() (int, error) {
	if client.saveStore == nil {
		return 0, nil
	}
	refused := func(err error) error {
		return fmt.Errorf("%w: %v", backend.ErrCheckpointSaveWrite, err)
	}
	if err := client.issuedWritesBlocked(); err != nil {
		return 0, refused(err)
	}
	if err := client.prepareIssuedWriteLists(); err != nil {
		return 0, refused(err)
	}
	if client.saveReadError != nil {
		return 0, nil
	}
	stored := 0
	for _, key := range []string{fileRemovedKey, fileCreatedKey} {
		if key == fileRemovedKey && !client.removedUnsaved || key == fileCreatedKey && !client.createdUnsaved {
			continue
		}
		if err := client.storeFileList(key); err != nil {
			return stored, refused(fmt.Errorf("%s: %v", key, err))
		}
		stored++
	}
	databases, open := client.databaseContents()
	for _, database := range databases {
		if !database.unsaved {
			continue
		}
		stale, err := client.storeRefusedDatabase(database, open[database.databaseContent])
		if err != nil {
			return stored, refused(fmt.Errorf("the database %s: %v", strconv.Quote(database.name), err))
		}
		if !stale {
			stored++
		}
	}
	for _, file := range client.openContents() {
		if !file.dirty {
			continue
		}
		if err := client.storeContent(file.name, file.fileContent); err != nil {
			return stored, refused(fmt.Errorf("the open file %s: %v", strconv.Quote(file.name), err))
		}
		stored++
	}
	// A list the stores above changed and the store then refused is as
	// unstored as a file.
	if client.removedUnsaved || client.createdUnsaved {
		return stored, refused(fmt.Errorf("the list of removed or created paths"))
	}
	return stored, nil
}

// storeRefusedDatabase gives the store a database's records whose last store
// it refused, and reports whether they were let go instead. Records no open
// object has, behind a container that moved on after their close was refused,
// are older than what the store holds and no call of the title's can reach
// them again: the write their close lost stays lost, as it does without a
// quick step or an ending, and the records stop waiting.
func (client *Client) storeRefusedDatabase(database *javaDatabase, open bool) (bool, error) {
	name := databaseFileName(database.name)
	if !open && database.synced != client.fileEpoch(name) {
		database.unsaved = false
		return true, nil
	}
	if err := client.storeFile(name, database.encode()); err != nil {
		return false, err
	}
	database.unsaved, database.synced = false, client.fileEpoch(name)
	return false, nil
}

// storeRefusedDatabases is the ending's part for databases: every record set
// whose last store was refused is given to the store again. One it refuses
// again stays waiting, for a quick step or a later ending.
func (client *Client) storeRefusedDatabases() {
	databases, open := client.databaseContents()
	for _, database := range databases {
		if !database.unsaved {
			continue
		}
		if _, err := client.storeRefusedDatabase(database, open[database.databaseContent]); err != nil && client.logger != nil {
			client.logger.Debug("LGT java database store failed at the ending", "name", database.name, "error", err)
		}
	}
}

// bindRestoredStorage connects a restored client to the store it will run
// over: its authentication adapter is built over that store and every storage
// object the record names is filled from it. Only reads are made. On a
// failure the client is left on the store it had, still to be bound, and the
// error says the saves could not be read; nothing of the store or of a
// running session has changed.
func (client *Client) bindRestoredStorage(archive *Archive, adapters adapterState, live backend.SaveStore) error {
	// Every read goes through one reader, under the adapter: each key is read
	// once, so every object of one path is given the same bytes, and on a
	// budget, because the names come from a slot.
	reader := backend.NewRebuildReader(live, int64(client.restoredStorage.limit()))
	unbound := client.saveStore
	err := client.attachAdapters(archive, adapters, reader)
	if err == nil {
		err = client.rebuildRestoredStorage()
	}
	if err == nil {
		err = reader.Err()
	}
	if err != nil {
		client.saveStore = unbound
		return fmt.Errorf("%w: %v", backend.ErrCheckpointSaveRead, err)
	}
	// The adapter now stands on the store itself, as in a session that was
	// started over it.
	client.rebaseAdapters(live)
	return nil
}

// rebuildRestoredStorage fills what a checkpoint brought back from the store
// the client reads. It starts from the record's own account every time, so a
// second attempt after a refused one is the first attempt again; the caller
// clears restoredStorage once the result is adopted.
func (client *Client) rebuildRestoredStorage() error {
	restored := client.restoredStorage
	if restored == nil {
		return nil
	}
	// A read that failed in an earlier attempt does not carry into this one.
	// The client keeps a failed read for the life of a session and holds back
	// every write after it, which is the rule for a session that is running;
	// this one is not yet.
	client.saveReadError = nil
	client.removed, client.created, client.removedLegacy, client.createdLegacy = nil, nil, nil, nil
	client.removedUnsaved, client.createdUnsaved, client.fileEpochs = false, false, nil
	for handle, cursor := range restored.fileCursors {
		if file := client.files[handle]; file != nil {
			file.cursor = cursor
		}
	}
	kept, limit := uint64(0), restored.limit()
	keep := func(size int) error {
		if uint64(size) > limit-kept {
			return fmt.Errorf("the saves the checkpoint has open hold more than the %d bytes a load restores", limit)
		}
		kept += uint64(size)
		return nil
	}
	// Both lists are read here, where a list that cannot be read refuses the
	// load. Left to the title's first file call, the failure would hold back
	// every write of a session that had already replaced the running one.
	client.removedFiles()
	client.createdFiles()
	if client.saveReadError != nil {
		return client.saveReadError
	}
	// The handles on one path, in the order of the lowest of them, share the
	// buffer filled for that path. Every handle is given a buffer of its own
	// first, so that one a refused attempt left shared is not read as
	// holding a write.
	paths := make(map[string][]uint32)
	order := make([]string, 0, len(client.files))
	for _, handle := range slices.Sorted(maps.Keys(client.files)) {
		client.files[handle].fileContent = &fileContent{}
		key := fileListKey(client.files[handle].name)
		if _, seen := paths[key]; !seen {
			order = append(order, key)
		}
		paths[key] = append(paths[key], handle)
	}
	for _, key := range order {
		handles := paths[key]
		data, _ := client.readFile(client.files[handles[0]].name)
		if client.saveReadError != nil {
			return client.saveReadError
		}
		// A record an earlier build took can hold two handles on one path
		// opened to rewrite it, which had a buffer each; the longer length
		// is the more of the file the title had written.
		cut := -1
		for _, handle := range handles {
			if client.files[handle].truncated {
				cut = max(cut, restored.lengths[handle])
			}
		}
		if cut >= 0 && len(data) > cut {
			data = data[:cut]
		}
		if err := keep(len(data)); err != nil {
			return err
		}
		content := &fileContent{data: bytes.Clone(data)}
		for _, handle := range handles {
			client.files[handle].fileContent = content
		}
	}
	runtime := client.javaRun
	if runtime == nil {
		return nil
	}
	// Bytes a stream held in a record an earlier build took go into its File
	// at the File's cursor, as the write that put them in the stream does
	// now; the File's buffer then holds them waiting, and nothing is stored.
	// What the File cannot take stays in the stream, as that build held it.
	for _, sink := range slices.Sorted(maps.Keys(restored.sinkBytes)) {
		held := restored.sinkBytes[sink]
		runtime.sinks[sink] = []byte{}
		handle, bound := runtime.files[runtime.sinkFiles[sink]]
		file := client.files[handle]
		if !bound || file == nil || !file.writable || !file.extend(file.cursor+len(held)) {
			runtime.sinks[sink] = bytes.Clone(held)
			continue
		}
		copy(file.data[file.cursor:], held)
		file.cursor += len(held)
		file.dirty = true
	}
	// The open objects on one database share the records read for it; a
	// closed object, which no call of the title's reads, is left empty.
	contents := make(map[string]*databaseContent)
	for _, object := range slices.Sorted(maps.Keys(runtime.databases)) {
		database := runtime.databases[object]
		modified := database.modified
		if !database.closed {
			if content, filled := contents[database.name]; filled {
				content.modified = max(content.modified, modified)
				database.databaseContent = content
				continue
			}
		}
		database.databaseContent = &databaseContent{recordSize: restored.recordSizes[object], modified: modified}
		if database.closed {
			continue
		}
		contents[database.name] = database.databaseContent
		stored, exists := client.readFile(databaseFileName(database.name))
		if client.saveReadError != nil {
			return client.saveReadError
		}
		if !exists {
			continue
		}
		if err := keep(len(stored)); err != nil {
			return err
		}
		// A container this runtime does not read is not an empty database: an
		// object left empty over it would replace it at its first write.
		if err := database.decode(stored); err != nil {
			return fmt.Errorf("the database %s %v", strconv.Quote(database.name), err)
		}
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.streams)) {
		stream := runtime.streams[object]
		if stream == nil || !stream.File {
			continue
		}
		// The window is on the handle the File object still has open, and on
		// the path itself once the stream is all that is left of it: a stream
		// the title closed, or one whose File it closed first.
		var source []byte
		if handle, bound := runtime.files[runtime.streamFiles[object]]; bound && client.files[handle] != nil {
			source = client.files[handle].data
		} else {
			source, _ = client.readFile(stream.Name)
			if client.saveReadError != nil {
				return client.saveReadError
			}
		}
		window := source[min(stream.Offset, len(source)):]
		if err := keep(len(window)); err != nil {
			return err
		}
		stream.Data = bytes.Clone(window)
		// What was read of a longer window is all of a shorter one.
		cursor := restored.cursors[object]
		stream.Read, stream.Mark = min(cursor[0], len(stream.Data)), min(cursor[1], len(stream.Data))
	}
	return nil
}

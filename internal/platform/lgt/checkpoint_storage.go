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
//   - A handle whose open asked for an empty file takes at most what it had
//     written, from the front of the file as it is now. A handle whose open
//     made a file that was not there is given the whole file, like any other.
//     See openFile.truncated.
//   - A save that cannot be read, or a container that does not decode,
//     refuses the load, and what is read is bounded: the names come from a
//     slot.
//
// What stays in a record, because it was never in a file: the bytes a title
// has written into a stream and not yet flushed, and what an authentication
// adapter made for the run.

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
	for _, file := range client.files {
		if file != nil {
			total += uint64(len(file.data))
		}
	}
	if client.javaRun == nil {
		return total
	}
	for _, database := range client.javaRun.databases {
		if database == nil || database.closed {
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
// A dirty handle's buffer is the file as it was when the handle was opened
// plus what the title wrote through it, and storing it stores all of it. When
// the file has changed since — through another handle, a rename, a removal —
// that would put the older part of the buffer over newer content, and the
// host would be the one doing it. The title's own close does exactly that and
// is the title's to do; a quick step is not.
func (client *Client) issuedWritesBlocked() error {
	dirty := make(map[string]string)
	for _, handle := range slices.Sorted(maps.Keys(client.files)) {
		file := client.files[handle]
		if file == nil || !file.dirty {
			continue
		}
		if _, err := fileSaveKey(file.name); err != nil {
			return fmt.Errorf("the open file %s has unstored writes and no save to store them in", strconv.Quote(file.name))
		}
		key := fileEpochKey(file.name)
		if other, shared := dirty[key]; shared {
			return fmt.Errorf("the open files %s and %s are one file and both have unstored writes", strconv.Quote(other), strconv.Quote(file.name))
		}
		dirty[key] = file.name
		if file.synced != client.fileEpoch(file.name) {
			return fmt.Errorf("the open file %s has unstored writes and its save has changed since it was opened", strconv.Quote(file.name))
		}
	}
	if client.javaRun == nil {
		return nil
	}
	unsaved := make(map[string]bool)
	for _, object := range slices.Sorted(maps.Keys(client.javaRun.databases)) {
		database := client.javaRun.databases[object]
		if database == nil || !database.unsaved {
			continue
		}
		behind := database.synced != client.fileEpoch(databaseFileName(database.name))
		if behind && database.closed {
			// Its last store was refused at the close, and the container has
			// been written since. Nothing can store this one any more, and
			// storeIssuedWrites lets it go.
			continue
		}
		if behind {
			return fmt.Errorf("the database %s has an unstored write and its container has changed since", strconv.Quote(database.name))
		}
		// Two objects of one database that both hold an unstored write would
		// be stored one over the other, and the host would be choosing which
		// of the title's writes is lost.
		key := fileEpochKey(databaseFileName(database.name))
		if unsaved[key] {
			return fmt.Errorf("two objects of the database %s both have an unstored write", strconv.Quote(database.name))
		}
		unsaved[key] = true
	}
	return nil
}

// storeIssuedWrites gives the store every write the title has issued and the
// store does not have: the two path lists and the databases whose last store
// was refused, and every open file with writes in its buffer. It answers how
// many things it stored, and an error that says the saves could not be
// written when the store refuses one or when issuedWritesBlocked names one.
//
// A file stays open, at its cursor; only its buffer becomes clean. What a
// title has written into a stream and not flushed is not a write to the file
// yet by this platform's own contract, and is left where it is.
//
// A session whose writes are held back by a failed save read stores nothing,
// here as anywhere, and that is not an error: it is the rule that keeps a
// save that could not be read from being overwritten.
func (client *Client) storeIssuedWrites() (int, error) {
	if client.saveStore == nil || client.saveReadError != nil {
		return 0, nil
	}
	refused := func(err error) error {
		return fmt.Errorf("%w: %v", backend.ErrCheckpointSaveWrite, err)
	}
	if err := client.issuedWritesBlocked(); err != nil {
		return 0, refused(err)
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
	if client.javaRun != nil {
		for _, object := range slices.Sorted(maps.Keys(client.javaRun.databases)) {
			database := client.javaRun.databases[object]
			if database == nil || !database.unsaved {
				continue
			}
			if database.closed && database.synced != client.fileEpoch(databaseFileName(database.name)) {
				// A closed database whose container moved on after its close
				// was refused: newer records are in the store and no call of
				// the title's can reach this object again. The write its close
				// lost stays lost, as it does without a quick step.
				database.unsaved = false
				continue
			}
			if err := client.storeFile(databaseFileName(database.name), database.encode()); err != nil {
				return stored, refused(fmt.Errorf("the database %s: %v", strconv.Quote(database.name), err))
			}
			database.unsaved, database.synced = false, client.fileEpoch(databaseFileName(database.name))
			stored++
		}
	}
	for _, handle := range slices.Sorted(maps.Keys(client.files)) {
		file := client.files[handle]
		if file == nil || !file.dirty {
			continue
		}
		if err := client.storeFile(file.name, file.data); err != nil {
			return stored, refused(fmt.Errorf("the open file %s: %v", strconv.Quote(file.name), err))
		}
		file.dirty, file.synced = false, client.fileEpoch(file.name)
		stored++
	}
	// A list the stores above changed and the store then refused is as
	// unstored as a file.
	if client.removedUnsaved || client.createdUnsaved {
		return stored, refused(fmt.Errorf("the list of removed or created paths"))
	}
	return stored, nil
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
	client.removed, client.created = nil, nil
	client.removedUnsaved, client.createdUnsaved, client.fileEpochs = false, false, nil
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
	for _, handle := range slices.Sorted(maps.Keys(client.files)) {
		file := client.files[handle]
		data, _ := client.readFile(file.name)
		if client.saveReadError != nil {
			return client.saveReadError
		}
		if file.truncated && len(data) > restored.lengths[handle] {
			data = data[:restored.lengths[handle]]
		}
		if err := keep(len(data)); err != nil {
			return err
		}
		file.data, file.dirty, file.synced = bytes.Clone(data), false, 0
	}
	runtime := client.javaRun
	if runtime == nil {
		return nil
	}
	for _, object := range slices.Sorted(maps.Keys(runtime.databases)) {
		database := runtime.databases[object]
		database.records, database.deleted = nil, nil
		database.recordSize = restored.recordSizes[object]
		database.rebuilt, database.changed, database.unsaved, database.synced = true, false, false, 0
		if database.closed {
			continue
		}
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

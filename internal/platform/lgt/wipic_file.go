package lgt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// The 0x190 block is the filesystem module. A file is a save entry under the
// "fs/" scope every other platform in this repository uses, falling back to
// the packaged JAR resource of the same name — which is how a game reads data
// it shipped and writes data it did not.
const fileSaveScope = "fs/"

// fileRemovedKey lists the paths a title has deleted.
//
// The save boundary has no delete, and a resource read falls back to the
// packaged JAR, so a removed file has to be recorded as removed rather than
// simply not written: writing an empty file in its place leaves it existing,
// and a title that deletes its save and then asks whether a save is there is
// told yes. That is not a cosmetic difference. A title starting a new game
// deletes the slot first and reads the answer as whether this is a fresh
// game — one that is told the save is still there skips everything a new game
// begins with, including its opening scene.
//
// RMS keeps the same kind of index for the same reason: a SaveStore only
// answers about keys it is asked for, so anything the store cannot represent
// has to be written down beside it.
const fileRemovedKey = fileSaveScope + ".removed"

// openFile is one MC_fsOpen handle.
type openFile struct {
	name     string
	data     []byte
	cursor   int
	writable bool
	dirty    bool
}

// handleResource services MC_knlGetResourceID and MC_knlGetResource. A
// resource is a packaged file, and the two calls are a pair: the first turns a
// name into an id and reports the size, the second reads that id into a buffer
// the Clet sized from it.
//
// The id is a guest pointer to a copy of the name, which is the same handle
// the other WIPI platform hands out. Answering the size instead would look
// right at the first call and fail at the second, because the size is then
// what arrives where a name is expected.
//
// **One name answers one id, for the life of the client.** The id is the
// resource's identity to a title, not a scratch handle it is done with when
// the read returns: a title here reads its resource list at boot and builds a
// `{id, size}` table from it, then loads a resource by asking for its id a
// second time and searching that table for the value. A fresh copy per call
// makes every one of those searches miss, and a miss leaves the size the
// caller had zeroed — so the title allocates nothing for the data, parses the
// heap that follows as its own record count, and walks off the end of a table
// it sized from the garbage. That is how it died, several thousand calls past
// the one that caused it.
func (client *Client) handleResource(thread *armcore.Thread, slot uint32) error {
	identifier, err := thread.Register(0)
	if err != nil {
		return err
	}
	name, err := client.readCString(identifier)
	if err != nil {
		return err
	}
	data, ok := client.readFile(name)
	if slot == slotGetResourceID {
		// The size out parameter is written either way: a title that checks
		// the size rather than the id has to see zero for a missing resource.
		size, sizeErr := thread.Register(1)
		if sizeErr != nil {
			return sizeErr
		}
		if size != 0 {
			length := uint32(0)
			if ok {
				length = uint32(len(data))
			}
			if err := client.writeWord(size, length); err != nil {
				return err
			}
		}
		if !ok {
			return answerCode(thread, wipiNoEntry)
		}
		if handle, seen := client.resourceIDs[name]; seen {
			return thread.SetRegister(0, handle)
		}
		handle, err := client.allocateBytes(append([]byte(name), 0))
		if err != nil {
			return err
		}
		if client.resourceIDs == nil {
			client.resourceIDs = make(map[string]uint32)
		}
		client.resourceIDs[name] = handle
		return thread.SetRegister(0, handle)
	}
	if !ok {
		return answerCode(thread, wipiNoEntry)
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return err
	}
	length, err := thread.Register(2)
	if err != nil {
		return err
	}
	if buffer == 0 {
		return answerCode(thread, wipiError)
	}
	// MC_knlGetResource answers 0, not the byte count. The pair is written as
	// `id = getResourceID(name, &size); buf = calloc(size); if
	// (getResource(id, buf, size)) { free(buf); return 0; }` — a nonzero
	// answer is the failure branch, so reporting the length frees the buffer
	// that was just filled and hands the caller a null it copies from a few
	// instructions later. A buffer too small for the resource is the one
	// documented failure, M_E_SHORTBUF.
	if int(length) < len(data) {
		return answerCode(thread, wipiShortBuffer)
	}
	if err := client.core.Memory().Write(buffer, data); err != nil {
		return err
	}
	return answerCode(thread, wipiSuccess)
}

// handleFile services the filesystem slots.
func (client *Client) handleFile(thread *armcore.Thread, slot uint32) error {
	answer := func(value int32) error { return thread.SetRegister(0, uint32(value)) }

	switch slot {
	case slotFsOpen:
		pointer, err := thread.Register(0)
		if err != nil {
			return err
		}
		mode, err := thread.Register(1)
		if err != nil {
			return err
		}
		name, err := client.readCString(pointer)
		if err != nil {
			return err
		}
		return answer(client.openFile(name, mode))

	case slotFsIsExist:
		pointer, err := thread.Register(0)
		if err != nil {
			return err
		}
		name, err := client.readCString(pointer)
		if err != nil {
			return err
		}
		if _, ok := client.readFile(name); ok {
			return answer(wipiSuccess)
		}
		return answer(wipiNoEntry)

	case slotFsFileAttribute:
		pointer, err := thread.Register(0)
		if err != nil {
			return err
		}
		info, err := thread.Register(1)
		if err != nil {
			return err
		}
		name, err := client.readCString(pointer)
		if err != nil {
			return err
		}
		data, ok := client.readFile(name)
		if !ok {
			return answer(wipiNoEntry)
		}
		// MH_FileInfo is three words, and which word is which comes from the
		// caller rather than from a header: a title wraps this call twice,
		// once returning the word at offset 8 and once the word at offset 4,
		// and reserves twelve bytes for it. The one the callers act on is the
		// size at offset 8 — they allocate a read buffer with it. Nothing
		// backing a file here is a directory and no creation time is kept, so
		// the other two are zero.
		if info != 0 {
			if err := client.writeWord(info, 0); err != nil {
				return err
			}
			if err := client.writeWord(info+4, 0); err != nil {
				return err
			}
			if err := client.writeWord(info+8, uint32(len(data))); err != nil {
				return err
			}
		}
		return answer(wipiSuccess)

	case slotFsRemove:
		pointer, err := thread.Register(0)
		if err != nil {
			return err
		}
		name, err := client.readCString(pointer)
		if err != nil {
			return err
		}
		client.removeFile(name)
		return answer(wipiSuccess)

	case slotFsClose:
		handle, err := thread.Register(0)
		if err != nil {
			return err
		}
		file := client.files[handle]
		if file == nil {
			return answer(wipiError)
		}
		if file.dirty {
			client.writeFile(file.name, file.data)
		}
		delete(client.files, handle)
		return answer(wipiSuccess)

	case slotFsRead, slotFsWrite:
		handle, err := thread.Register(0)
		if err != nil {
			return err
		}
		buffer, err := thread.Register(1)
		if err != nil {
			return err
		}
		length, err := thread.Register(2)
		if err != nil {
			return err
		}
		return answer(client.transferFile(slot, handle, buffer, length))

	case slotFsSeek:
		handle, err := thread.Register(0)
		if err != nil {
			return err
		}
		offset, err := thread.Register(1)
		if err != nil {
			return err
		}
		whence, err := thread.Register(2)
		if err != nil {
			return err
		}
		return answer(client.seekFile(handle, int32(offset), whence))

	case slotFsTell:
		handle, err := thread.Register(0)
		if err != nil {
			return err
		}
		file := client.files[handle]
		if file == nil {
			return answer(wipiError)
		}
		return answer(int32(file.cursor))

	case slotFsTotalSpace, slotFsAvailable:
		// These two take no argument: they report the storage the program has,
		// not the remainder of an open file. Reading the first register as a
		// file handle answers an error to a title asking how much room it has,
		// and a title that asks before writing a save reports that the handset
		// is full and refuses to start a game.
		//
		// The number is this platform's own. A handset gave a program a small
		// fixed quota and the titles here check it against a save of a few
		// kilobytes, while the store behind it is a directory with no limit
		// worth reporting, so what matters is that it is comfortably larger
		// than any save and small enough to stay an ordinary integer.
		return answer(storageQuotaBytes)

	case slotFsMkDir, slotFsRmDir:
		// The store has no directories — a path is a save key. A title that
		// creates its save directory before writing into it is answered that
		// the directory is there, because from its next call's point of view
		// it is.
		return answer(wipiSuccess)

	}
	return fmt.Errorf("unimplemented LGT file slot %#x", slot)
}

// openFile resolves a path and returns a handle.
func (client *Client) openFile(name string, flag uint32) int32 {
	data, ok := client.readFile(name)
	// Read-only is the one flag that cannot create. Anything else is an open
	// with write intent, and a file that is not there yet is created by it —
	// which is how a title makes its save in the first place. Treating only one
	// bit as "writable" refused the flag a title actually opens its save with,
	// and the title then retried forever against a handset it read as full.
	writable := flag != fileOpenReadOnly
	if !ok {
		if !writable {
			return wipiNoEntry
		}
		data = nil
	}
	cursor := 0
	switch {
	case flag == fileOpenWriteTruncate:
		data = nil
	case flag == fileOpenWriteOnly:
		// Write-only appends, which the specification says in as many words.
		cursor = len(data)
	}
	handle := client.takeHandle()
	if client.files == nil {
		client.files = make(map[uint32]*openFile)
	}
	client.files[handle] = &openFile{name: name, data: data, cursor: cursor, writable: writable}
	return int32(handle)
}

// The MC_fsOpen flags, in the specification's own order: read, write (which
// appends), write-and-truncate, read-write.
const (
	fileOpenReadOnly      uint32 = 1
	fileOpenWriteOnly     uint32 = 2
	fileOpenWriteTruncate uint32 = 4
	fileOpenReadWrite     uint32 = 8
)

// fileOpenReadWrite is named for the same reason the others are: the flag set
// is only legible as a whole, and it is the one a title opens its save with.
var _ = fileOpenReadWrite

func (client *Client) transferFile(slot, handle, buffer, length uint32) int32 {
	file := client.files[handle]
	if file == nil || buffer == 0 {
		return wipiError
	}
	if slot == slotFsRead {
		count := min(int(length), len(file.data)-file.cursor)
		if count <= 0 {
			return 0
		}
		if err := client.core.Memory().Write(buffer, file.data[file.cursor:file.cursor+count]); err != nil {
			return wipiError
		}
		file.cursor += count
		return int32(count)
	}
	if !file.writable {
		return wipiError
	}
	chunk := make([]byte, length)
	if err := client.core.Memory().Read(buffer, chunk); err != nil {
		return wipiError
	}
	end := file.cursor + int(length)
	if end > len(file.data) {
		file.data = append(file.data, make([]byte, end-len(file.data))...)
	}
	copy(file.data[file.cursor:end], chunk)
	file.cursor = end
	file.dirty = true
	return int32(length)
}

func (client *Client) seekFile(handle uint32, offset int32, whence uint32) int32 {
	file := client.files[handle]
	if file == nil {
		return wipiError
	}
	base := 0
	switch whence {
	case 0:
		base = 0
	case 1:
		base = file.cursor
	case 2:
		base = len(file.data)
	default:
		return wipiError
	}
	position := base + int(offset)
	if position < 0 {
		return wipiError
	}
	file.cursor = position
	return int32(position)
}

// readFile resolves a guest path: the save first, then the packaged resource.
// A removed path resolves to neither, which is what makes a delete a delete.
func (client *Client) readFile(name string) ([]byte, bool) {
	if client.removedFiles()[canonicalFileName(name)] {
		return nil, false
	}
	if key, err := fileSaveKey(name); err == nil && client.saveStore != nil {
		if data, ok := client.saveStore.LoadSave(key); ok {
			return data, true
		}
	}
	return client.archive.Resource(name)
}

// writeFile persists a guest file. A failure is a diagnostic, not a guest
// error: the in-memory copy stays authoritative for the session.
func (client *Client) writeFile(name string, data []byte) {
	// Writing a path brings it back, whether or not the store round trip below
	// succeeds: the session's own view has the file from here on.
	client.markFileRemoved(name, false)
	if client.saveStore == nil {
		return
	}
	key, err := fileSaveKey(name)
	if err != nil {
		return
	}
	if err := client.saveStore.StoreSave(key, data); err != nil && client.logger != nil {
		client.logger.Debug("LGT save store failed", "name", name, "error", err)
	}
}

// removeFile deletes a guest path. The stored bytes stay where they are —
// there is nothing to delete them with — and the removal list is what makes
// them unreachable until something writes the path again.
func (client *Client) removeFile(name string) {
	client.markFileRemoved(name, true)
}

// canonicalFileName is the form the removal list is keyed by, so that a title
// naming the same file twice with different case or a leading slash removes
// and reopens the one file. It matches what the resource lookup already does.
func canonicalFileName(name string) string {
	return strings.ToLower(strings.TrimPrefix(name, "/"))
}

// removedFiles is the set of deleted paths, read from the store once per
// session and kept in memory after that.
func (client *Client) removedFiles() map[string]bool {
	if client.removed != nil {
		return client.removed
	}
	client.removed = make(map[string]bool)
	if client.saveStore == nil {
		return client.removed
	}
	data, ok := client.saveStore.LoadSave(fileRemovedKey)
	if !ok {
		return client.removed
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			client.removed[line] = true
		}
	}
	return client.removed
}

// markFileRemoved records or clears one path and writes the list back.
func (client *Client) markFileRemoved(name string, removed bool) {
	set := client.removedFiles()
	key := canonicalFileName(name)
	if set[key] == removed {
		return
	}
	if removed {
		set[key] = true
	} else {
		delete(set, key)
	}
	if client.saveStore == nil {
		return
	}
	names := make([]string, 0, len(set))
	for entry := range set {
		names = append(names, entry)
	}
	sort.Strings(names)
	if err := client.saveStore.StoreSave(fileRemovedKey, []byte(strings.Join(names, "\n"))); err != nil && client.logger != nil {
		client.logger.Debug("LGT removal list store failed", "name", name, "error", err)
	}
}

func fileSaveKey(name string) (string, error) {
	return backend.NormalizeSaveKey(fileSaveScope + strings.TrimPrefix(name, "/"))
}

// storageQuotaBytes is what MC_fsTotalSpace and MC_fsAvailable report. See the
// slots themselves for why it is a constant rather than the host's free space.
const storageQuotaBytes int32 = 1 << 20

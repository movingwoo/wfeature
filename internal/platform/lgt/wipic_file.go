package lgt

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
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

// fileCreatedKey lists the paths a title has written that the archive does not
// package. It is the removal list's mirror and exists for the same reason: a
// SaveStore answers about the keys it is handed and cannot be walked, so a
// directory listing built only from the archive would show a title the files
// it shipped and none of the files it wrote — which for a title that lists a
// directory to find its own save slots is a listing that is always empty.
const fileCreatedKey = fileSaveScope + ".created"

// openFile is one MC_fsOpen handle: where the title is in a file and how it
// opened it. What the file holds is the fileContent it shares with every other
// handle open on the same path.
type openFile struct {
	name string
	*fileContent
	cursor   int
	writable bool
	// truncated says the open itself asked for an empty file: the title opened
	// the path to rewrite it, and everything in data was written since that
	// open emptied it. That is what a quick load needs to know about it: it
	// gives such a handle at most what the file held at the quick save, so
	// that the file the title is rewriting is never followed by the tail of a
	// newer one.
	//
	// A handle whose open made a path that was not there is not one of these.
	// The title asked for the file and found none; had there been one it
	// would have kept it. After a load such a handle is given the file as it
	// is, like any other, because a title that keeps its save open for a
	// whole session is in exactly that position on its first run, and cutting
	// its buffer to what it held at the quick save would drop what it saved
	// afterwards the next time the handle is stored.
	truncated bool
}

// fileContent is what a path holds while the title has it open.
//
// **Every handle open on one path reads and writes one buffer.** A handset's
// file is one file however many times it is open: a write through one handle,
// or an open that empties it, is what a read through any other handle sees
// next. A buffer per handle kept each open's own copy instead, so a handle
// opened before another one wrote read the file as it had been, and whichever
// handle was closed last put its copy over the other's write.
//
// A path is what fileListKey makes of a name: the spellings the store gives
// one key share one buffer, and two names that differ only by case are two
// files.
type fileContent struct {
	data []byte
	// dirty says data holds a write the store has not been given: what the
	// title wrote through any handle on the path, or the empty file an open
	// made, since the path was last stored. The close of any of those handles
	// stores it, and so do the ending and a quick step.
	dirty bool
	// synced is the path's epoch when data was last level with the store: at
	// the first open, and at each store of it. A store that is not this
	// buffer's, such as a DataBase written under the same name, moves the
	// epoch past it: a dirty buffer then holds older bytes than the store for
	// everything the title did not write through it.
	synced uint64
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
// **An id this platform issued is read back from its own table, not from the
// guest's memory.** The id is a pointer to a copy of the name because that is
// the handle the other WIPI platform hands out, and a copy in guest memory is
// something a title can write over: one local title's fill loop runs past the
// end of the LCD and lands on it, after which the name reads empty, the read
// fails, and the title parses garbage into an allocation size. The overrun is
// kept away from the platform's data now (see surfaceBase), and this is the
// other half of the same answer — what the platform issued, the platform
// remembers.
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
	name, known := client.resourceNames[identifier]
	if !known {
		// A name the platform has not issued an id for is read from guest
		// memory, which is what MC_knlGetResourceID is always handed.
		if name, err = client.readCString(identifier); err != nil {
			return err
		}
	}
	// What the title saved is a resource too, but a database's container is
	// no save of the title's (titleFile).
	data, ok := client.titleFile(name)
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
		if client.resourceNames == nil {
			client.resourceNames = make(map[uint32]string)
		}
		client.resourceNames[handle] = name
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

// handleFile services the filesystem slots. What each answers is the common
// storage contract, docs/wipi-storage.md; this platform's part of it is how a
// name becomes a save key and what the store can tell about one.
func (client *Client) handleFile(thread *armcore.Thread, slot uint32) error {
	answer := func(value int32) error { return thread.SetRegister(0, uint32(value)) }
	// name reads the name argument at a register and the code its form earns
	// (see fileNameCode); a code other than success is the whole answer.
	name := func(index int) (string, bool, int32, error) {
		pointer, err := thread.Register(index)
		if err != nil {
			return "", false, wipiSuccess, err
		}
		text, err := client.readCString(pointer)
		if err != nil {
			return "", false, wipiSuccess, err
		}
		root, code := fileNameCode(text)
		return text, root, code, nil
	}

	switch slot {
	case slotFsOpen:
		path, root, code, err := name(0)
		if err != nil {
			return err
		}
		mode, err := thread.Register(1)
		if err != nil {
			return err
		}
		if code != wipiSuccess {
			return answer(code)
		}
		if root {
			// The title's own directory is no name a file can be kept under.
			return answer(wipiBadFileName)
		}
		return answer(client.openFile(path, mode))

	case slotFsIsExist:
		path, root, code, err := name(0)
		if err != nil {
			return err
		}
		if code != wipiSuccess {
			return answer(code)
		}
		if root || client.fileExists(path) || client.holdsDirectory(path) {
			return answer(wipiSuccess)
		}
		return answer(wipiNoEntry)

	case slotFsFileAttribute:
		path, root, code, err := name(0)
		if err != nil {
			return err
		}
		info, err := thread.Register(1)
		if err != nil {
			return err
		}
		if code != wipiSuccess {
			return answer(code)
		}
		// MH_FileInfo is three words: the attributes, the creation time and
		// the size, in that order, which a title's own wrappers agree with:
		// one returns the word at offset 8 and one the word at offset 4, and
		// the callers allocate a read buffer with the size. No creation time
		// is kept, so that word is zero; a directory sets MH_FILE_IS_DIR,
		// the v2.0 HAL's 0x01, and is no size.
		attributes, size := uint32(0), uint32(0)
		switch data, isFile := client.titleFile(path); {
		case root:
			attributes = fileAttributeDirectory
		case isFile:
			size = uint32(len(data))
		case client.directoryExists(path):
			attributes = fileAttributeDirectory
		default:
			return answer(wipiNoEntry)
		}
		if info != 0 {
			for index, word := range []uint32{attributes, 0, size} {
				if err := client.writeWord(info+uint32(index)*4, word); err != nil {
					return err
				}
			}
		}
		return answer(wipiSuccess)

	case slotFsRemove:
		// MC_fsRemove removes a file. A name that is not one — nothing there,
		// a directory, the title's own directory — is M_E_NOENT, and nothing
		// changes; a removal the store refuses is M_E_ERROR, and nothing
		// changes either (removeFile).
		path, root, code, err := name(0)
		if err != nil {
			return err
		}
		if code != wipiSuccess {
			return answer(code)
		}
		if root || !client.fileExists(path) {
			return answer(wipiNoEntry)
		}
		if client.removeFile(path) != nil {
			return answer(wipiError)
		}
		return answer(wipiSuccess)

	case slotFsRename:
		// The store has no rename: a path is a save key, so the new name is
		// written with the old one's bytes and the old one is removed. The
		// two documented refusals are what make it a rename rather than a
		// copy — an absent source and an occupied destination both leave the
		// store untouched, and the specification names a code for each.
		oldName, oldRoot, oldCode, err := name(0)
		if err != nil {
			return err
		}
		newName, newRoot, newCode, err := name(1)
		if err != nil {
			return err
		}
		switch {
		case oldCode != wipiSuccess:
			return answer(oldCode)
		case newCode != wipiSuccess:
			return answer(newCode)
		case oldRoot:
			return answer(wipiNoEntry)
		case newRoot:
			return answer(wipiBadFileName)
		}
		return answer(client.renameFile(oldName, newName))

	case slotFsClose:
		handle, err := thread.Register(0)
		if err != nil {
			return err
		}
		if !client.closeFileHandle(handle) {
			return answer(wipiError)
		}
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

	case slotFsTotalSpace:
		// This and MC_fsAvailable take no argument: they report the storage
		// the program has, not the remainder of an open file. Reading the
		// first register as a file handle answers an error to a title asking
		// how much room it has, and a title that asks before writing a save
		// reports that the handset is full and refuses to start a game.
		return answer(storageQuotaBytes)

	case slotFsAvailable:
		return answer(client.storageAvailable())

	case slotFsList:
		path, _, code, err := name(0)
		if err != nil {
			return err
		}
		buffer, err := thread.Register(1)
		if err != nil {
			return err
		}
		size, err := thread.Register(2)
		if err != nil {
			return err
		}
		if code != wipiSuccess {
			return answer(code)
		}
		// The names go back NUL-terminated one after another and the list ends
		// with an empty one, which is the convention the caller reads: it takes
		// the buffer's first byte as the whole answer's "is there anything",
		// and walks name by name until it meets a zero-length one. A directory
		// with nothing in it, or none there, is the empty list's two NULs; the
		// specification gives this call no code for a missing directory, and a
		// local title walks the empty answer.
		entries := client.listDirectory(path)
		packed := make([]byte, 0, 64)
		for _, entry := range entries {
			packed = append(packed, entry...)
			packed = append(packed, 0)
		}
		packed = append(packed, 0)
		if len(entries) == 0 {
			packed = append(packed, 0)
		}
		if buffer == 0 || uint32(len(packed)) > size {
			return answerCode(thread, wipiShortBuffer)
		}
		if err := client.core.Memory().Write(buffer, packed); err != nil {
			return err
		}
		return answerCode(thread, wipiSuccess)

	case slotFsMkDir:
		path, root, code, err := name(0)
		if err != nil {
			return err
		}
		return answer(client.makeDirectory(path, root, code))

	case slotFsRmDir:
		path, root, code, err := name(0)
		if err != nil {
			return err
		}
		return answer(client.removeDirectory(path, root, code))

	case slotFsSetMode:
		path, root, code, err := name(0)
		if err != nil {
			return err
		}
		mode, err := thread.Register(1)
		if err != nil {
			return err
		}
		if code != wipiSuccess {
			return answer(code)
		}
		return answer(client.setFileMode(path, root, mode))

	case slotFsGetCounts:
		// How many entries a directory holds, sub-directories included, which
		// is what MC_fsList names: the same listing, counted.
		path, _, code, err := name(0)
		if err != nil {
			return err
		}
		if code != wipiSuccess {
			return answer(code)
		}
		return answer(int32(len(client.listDirectory(path))))

	}
	return fmt.Errorf("unimplemented LGT file slot %#x%s, with %s; %s", slot,
		client.describeJavaCallSite(thread),
		formatWords(registerWords(thread, 4)), client.describeCallWords(thread, 4))
}

// openFile resolves a path and returns a handle.
func (client *Client) openFile(name string, flag uint32) int32 {
	// Read-only is the one flag that cannot create. Anything else is an open
	// with write intent, and a file that is not there yet is created by it —
	// which is how a title makes its save in the first place. Treating only one
	// bit as "writable" refused the flag a title actually opens its save with,
	// and the title then retried forever against a handset it read as full.
	writable := flag != fileOpenReadOnly
	// The container of a database is no file (titleFile), so an open to read
	// finds nothing there. One that would create a file there is refused as
	// a name no file can be kept under, as the platform's own lists are: the
	// file would be the container, and its first store the database's end.
	if client.databaseContainer(name) {
		if !writable {
			return wipiNoEntry
		}
		return wipiBadFileName
	}
	data, ok := client.readFile(name)
	if !ok && !writable {
		return wipiNoEntry
	}
	// A path another handle has open is that handle's buffer. A buffer of its
	// own is a copy of what a read answers, because what a read answers for a
	// packaged file is the archive's own bytes: writing through those would
	// change what the archive says that file holds for the rest of the
	// session, for every later reader of the resource and for a checkpoint,
	// which reopens the archive and finds what was shipped.
	content := client.openContent(name)
	switch {
	case content == nil:
		content = &fileContent{data: bytes.Clone(data), synced: client.fileEpoch(name)}
	case !content.dirty && content.synced != client.fileEpoch(name):
		// Something that is not a handle stored the path since the buffer was
		// filled: the buffer takes the file as it is now.
		content.data, content.synced = bytes.Clone(data), client.fileEpoch(name)
	}
	cursor := 0
	switch {
	case !ok:
		// **The create happens at open, not at the first write.** A handset
		// has the file the moment the open returns, so everything that asks
		// about a path — MC_fsFileAttribute, MC_fsIsExist, a second open —
		// answers about a file of length zero from here on. Leaving the
		// creation until something is written keeps the handle usable but
		// hides the file from those calls, and a title that opens its save and
		// immediately stats it is told there is no such file. One reads that
		// as a filesystem it cannot save to and quits with a message saying
		// so, before any of its own screens.
		client.emptyFile(name, content)
	case flag == fileOpenWriteTruncate && (len(content.data) != 0 || content.dirty):
		// **The open empties the file in the store, whether or not a write
		// follows.** A title clears a save slot by opening it this way and
		// closing it; one whose session ends before it writes the slot again
		// has still cleared it. Leaving the store alone until the title's own
		// close loaded the save the player had just cleared into a session cut
		// short, and handed a quick load the old bytes.
		client.emptyFile(name, content)
	case flag == fileOpenWriteOnly:
		// Write-only appends, which the specification says in as many words.
		cursor = len(content.data)
	}
	handle := client.takeHandle()
	if client.files == nil {
		client.files = make(map[uint32]*openFile)
	}
	client.files[handle] = &openFile{name: name, fileContent: content, cursor: cursor, writable: writable,
		truncated: flag == fileOpenWriteTruncate}
	return int32(handle)
}

// emptyFile makes a path an empty file in the store at once, for an open that
// creates it or asks for it empty. Every handle on the path reads it empty
// from here on. A store that refuses leaves the empty file a write waiting in
// the buffer: every read of the path in the session sees it, and the close,
// the ending and a quick step give it to the store again.
func (client *Client) emptyFile(name string, content *fileContent) {
	content.data, content.dirty = nil, true
	_ = client.storeContent(name, content)
}

// openContent answers the buffer the handles open on a path share, or nil
// when none is open on it.
func (client *Client) openContent(name string) *fileContent {
	if len(client.files) == 0 {
		return nil
	}
	key := fileListKey(name)
	for _, file := range client.files {
		if file != nil && file.fileContent != nil && fileListKey(file.name) == key {
			return file.fileContent
		}
	}
	return nil
}

// openContents answers one open handle for each buffer, the lowest-numbered
// one, in handle order: what the ending and a quick step store, once each.
func (client *Client) openContents() []*openFile {
	seen := make(map[*fileContent]bool, len(client.files))
	files := make([]*openFile, 0, len(client.files))
	for _, handle := range slices.Sorted(maps.Keys(client.files)) {
		file := client.files[handle]
		if file == nil || file.fileContent == nil || seen[file.fileContent] {
			continue
		}
		seen[file.fileContent] = true
		files = append(files, file)
	}
	return files
}

// storeContent gives the store a path's buffer, with the store's answer. A
// buffer the store took is level with it; one it refused keeps its write.
func (client *Client) storeContent(name string, content *fileContent) error {
	if err := client.storeFile(name, content.data); err != nil {
		return err
	}
	content.dirty, content.synced = false, client.fileEpoch(name)
	return nil
}

// fileEpoch is how often a path's save has changed under this session.
func (client *Client) fileEpoch(name string) uint64 {
	return client.fileEpochs[fileListKey(name)]
}

func (client *Client) bumpFileEpoch(name string) {
	if client.fileEpochs == nil {
		client.fileEpochs = make(map[string]uint64)
	}
	client.fileEpochs[fileListKey(name)]++
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

// The MC_fsSetMode modes, MH_FILEMODE_RDONLY, _WRONLY and _RDWR. They are not
// the MC_fsOpen flags: those are bits, these are the three values 1 to 3. The
// specification also spells the same three 0x10, 0x20 and 0x30, and both
// spellings are taken.
const (
	fileModeReadOnly  uint32 = 1
	fileModeWriteOnly uint32 = 2
	fileModeReadWrite uint32 = 3
)

var _ = fileModeWriteOnly

// fileModeAccepted reports whether a value is one of the modes MC_fsSetMode
// takes, in either spelling.
func fileModeAccepted(mode uint32) bool {
	switch mode {
	case fileModeReadOnly, fileModeWriteOnly, fileModeReadWrite,
		fileModeReadOnly << 4, fileModeWriteOnly << 4, fileModeReadWrite << 4:
		return true
	}
	return false
}

// setFileMode is MC_fsSetMode(fileName, fmode, aMode), which marks a file or
// a directory read-only, write-only or both. **The store keeps no
// attributes**: a file is a save key and its bytes, so there is nowhere to
// keep a mode, and nothing here refuses a read or a write by one. Accepting a
// mode is therefore answering that the mode is good and the name is there,
// and nothing more: every file stays readable and writable by its own
// program. The name has been checked by the caller; the mode is asked before
// the name's existence, so a missing name answers M_E_NOENT only for a good
// mode.
func (client *Client) setFileMode(name string, root bool, mode uint32) int32 {
	if !fileModeAccepted(mode) {
		return wipiInvalid
	}
	if root || client.fileExists(name) || client.holdsDirectory(name) {
		return wipiSuccess
	}
	return wipiNoEntry
}

// makeDirectory is MC_fsMkDir and FileSystem.mkdir: a name that is already a
// file or a directory — the title's own directory included — is M_E_EXIST,
// and any other good name is written down in the directory list, which is
// all a directory is to the store. A parent nobody made is not asked for:
// saves have always been written below directories that were never made, so
// the parent of a file or a directory is implied rather than required.
func (client *Client) makeDirectory(name string, root bool, code int32) int32 {
	switch {
	case code != wipiSuccess:
		return code
	case root || client.fileExists(name) || client.holdsDirectory(name):
		return wipiExists
	}
	client.markDirectoryMade(name, true)
	return wipiSuccess
}

// removeDirectory is MC_fsRmDir and FileSystem.rmdir. The title's own
// directory is not removed (M_E_BADFILENAME); a name that is not a directory,
// a file's among them, is M_E_NOENT; one with anything below it, a file or a
// directory, is M_E_NOTEMPTY; otherwise the name comes off the directory list.
// The contents are asked before the list, so a directory that holds files
// without ever having been made answers that it is not empty rather than that
// it does not exist.
func (client *Client) removeDirectory(name string, root bool, code int32) int32 {
	switch {
	case code != wipiSuccess:
		return code
	case root:
		return wipiBadFileName
	case client.fileExists(name):
		return wipiNoEntry
	case len(client.listDirectory(name)) > 0:
		return wipiNotEmpty
	case !client.directoryMade(name):
		return wipiNoEntry
	}
	client.markDirectoryMade(name, false)
	return wipiSuccess
}

// closeFileHandle flushes an open file and drops it, and reports whether it
// closed. It is the body of MC_fsClose, and the collector reaches it too: a
// File object nothing holds any more is one the language would have closed on
// the handset, and its buffered writes have to reach the store the same way a
// close's do. What it stores is the path's buffer, with what other handles on
// the path wrote in it: storing early is never wrong, and it leaves those
// handles nothing older to put back.
//
// **A write the store refuses keeps the handle open.** The buffer is the only
// place the write is, so dropping the handle would lose it while the title was
// told it had saved. The buffer stays dirty, the close answers that it failed,
// and the next close, the ending and a quick step each give the store the
// write again.
func (client *Client) closeFileHandle(handle uint32) bool {
	file := client.files[handle]
	if file == nil {
		return false
	}
	// A handle opened only to read has nothing of its own to store; see
	// javaFileClose.
	if file.writable && file.dirty && client.storeContent(file.name, file.fileContent) != nil {
		return false
	}
	delete(client.files, handle)
	return true
}

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
	// The bound is checked before the length sizes anything.
	end := file.cursor + int(length)
	if end > len(file.data) && end > maxFileBytes {
		return wipiNoSpace
	}
	chunk := make([]byte, length)
	if err := client.core.Memory().Read(buffer, chunk); err != nil {
		return wipiError
	}
	file.extend(end)
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
	if position < 0 || position > max(len(file.data), maxFileBytes) {
		return wipiError
	}
	file.cursor = position
	return int32(position)
}

// readFile resolves a guest path: the save first, then the packaged resource.
// A removed path resolves to neither, which is what makes a delete a delete.
//
// **A write waiting in an open buffer comes before anything else.** A
// handset's write reaches the file, so a second open, a stat or a rename of
// the path sees it; here it is in the path's buffer until a close, and the
// store still has the bytes from before. That includes a write made after the
// path was removed, which brings the path back at once, as it brings it back
// in the store at the close; and a write the store refused, which is in the
// session and nowhere else. A removal leaves the buffer nothing waiting, so it
// is the removal that answers until something writes the path again.
//
// A session with no store reads what it wrote from sessionFiles, where its
// stores went, after an authentication adapter has answered for its own keys.
func (client *Client) readFile(name string) ([]byte, bool) {
	if content := client.openContent(name); content != nil && content.dirty {
		return bytes.Clone(content.data), true
	}
	if client.fileRemoved(name) {
		return nil, false
	}
	if key, err := fileSaveKey(name); err == nil {
		if client.saveStore != nil {
			if data, ok := client.loadSave(key); ok {
				return data, true
			}
		}
		if data, kept := client.sessionFiles[key]; kept {
			return bytes.Clone(data), true
		}
	}
	// The archive is asked for the path every spelling of the name is, as
	// the store is: its own lookup drops one leading slash and reads "." and
	// a backslash, but takes "//data/a.txt" for a path outside it.
	return client.packagedResource(guestPath(name))
}

// renameFile is MC_fsRename. The store has no rename: a path is a save key, so
// the new name is written with the old one's bytes and the old one is removed.
// The two documented refusals are what make it a rename rather than a copy: an
// absent source and an occupied destination both leave the store untouched,
// and the specification names a code for each.
func (client *Client) renameFile(oldName, newName string) int32 {
	// A name with no save key is one the store cannot hold — the index of
	// the databases, the path lists — and moving a file there would leave it
	// under neither name. A database's container is held, by the database:
	// a file moved there would be written over it.
	if !hasSaveKey(newName) || client.databaseContainer(newName) {
		return wipiBadFileName
	}
	// A container is no file to move (titleFile).
	data, ok := client.titleFile(oldName)
	if !ok {
		return wipiNoEntry
	}
	moved := fileListKey(oldName) != fileListKey(newName)
	if moved && (client.fileExists(newName) || client.holdsDirectory(newName)) {
		// A name that is a file or a directory is taken either way.
		return wipiExists
	}
	// A new name the store refuses leaves the old one where it is: removing
	// it would leave the file under neither.
	if client.storeFile(newName, data) != nil {
		return wipiError
	}
	source, target := client.openContent(oldName), client.openContent(newName)
	if moved {
		client.markFileRemoved(oldName, true)
		client.bumpFileEpoch(oldName)
		client.forgetSessionFile(oldName)
	}
	// A handle open on the file is on the file, not on its old name: its next
	// store goes under the new one rather than bringing the old one back. A
	// handle open on the new name, which was no file until now, is on the file
	// too. Either way the buffer holds what was just stored.
	content := source
	if content == nil {
		content = target
	}
	if content == nil {
		return wipiSuccess
	}
	content.data, content.dirty, content.synced = bytes.Clone(data), false, client.fileEpoch(newName)
	for _, file := range client.files {
		if file == nil || file.fileContent != source && file.fileContent != target {
			continue
		}
		if moved && file.fileContent == source {
			file.name = newName
		}
		file.fileContent = content
	}
	return wipiSuccess
}

// flushOpenFiles persists every open file that has been written to and not
// closed, and what else the store refused to take when the title wrote it.
//
// **A handset's write reaches the filesystem; ours reaches a buffer.** The
// bytes are kept on the open file here and persisted when it is closed, which
// is right for a title that closes what it opened and wrong for one that does
// not — and one local title does not: it writes the single byte that says its
// first run is over and calls `MC_knlExit` on the next line, so the file it
// created stayed on disk at zero bytes and the title showed its first-run
// notice again on every run, for ever.
//
// Closing is not the only moment a write becomes durable, so the ending is a
// second one. It costs nothing per write, which is what a flush on every write
// would not: a title that fills a save a byte at a time would rewrite the whole
// file once per byte.
//
// A Java title's writes through a stream opened on a File are in the File
// already; anything a stream still holds is drained into its File first. A
// database whose store was refused, and a path list the store refused, are
// given to the store again here as well. Anything whose store is refused
// again stays as it was, so a later ending or a quick step tries again.
func (client *Client) flushOpenFiles() {
	if client.javaRun != nil {
		for _, sink := range slices.Sorted(maps.Keys(client.javaRun.sinkFiles)) {
			if err := client.drainJavaSinkToFile(sink); err != nil && client.logger != nil {
				client.logger.Debug("LGT java file stream could not be drained at the ending", "stream", sink, "error", err)
			}
		}
		client.storeRefusedDatabases()
	}
	for _, file := range client.openContents() {
		if file.dirty {
			_ = client.storeContent(file.name, file.fileContent)
		}
	}
	if client.saveStore == nil || client.saveReadError != nil {
		return
	}
	for _, key := range client.pathListsUnsaved() {
		if err := client.storeFileList(key); err != nil && client.logger != nil {
			client.logger.Debug("LGT path list store failed at the ending", "list", key, "error", err)
		}
	}
	client.storeWaitingDatabaseIndex()
}

// storeFile gives the store a guest path's bytes, with the store's answer:
// nil when the store took the file, and when there was nothing to give it to
// — a session whose writes are held back by a failed read, a path that is not
// a save key. It is what a close, the ending, a quick save and a quick load
// call, because none may drop a write the store refused; the two path lists
// report through their own bits.
//
// A session with no store keeps the file in sessionFiles as well, which is its
// store for as long as it runs.
func (client *Client) storeFile(name string, data []byte) error {
	key, err := fileSaveKey(name)
	if err != nil {
		// A name with no save key is no file the store or a path list can
		// hold. See fileSaveKey.
		return nil
	}
	if client.saveReadError == nil {
		if err := client.storeSessionSave(key, data); err != nil {
			if client.logger != nil {
				client.logger.Debug("LGT save store failed", "name", name, "error", err)
			}
			return err
		}
		client.bumpFileEpoch(name)
	}
	// Writing a path brings it back. The removal comes off the list only now
	// that the store has the bytes: taken off first, a write the store then
	// refused left the next session reading what the removal had hidden. The
	// session itself sees a refused write in the buffer that holds it.
	client.markFileRemoved(name, false)
	client.markFileCreated(name)
	return nil
}

// storeSessionSave gives one save key to the store the session writes
// through, and to sessionFiles when there is no store beneath it. A write
// held back by a failed read is its caller's to skip.
func (client *Client) storeSessionSave(key string, data []byte) error {
	if client.saveStore != nil {
		if err := client.saveStore.StoreSave(key, data); err != nil {
			return err
		}
	}
	if client.storeless() {
		return client.keepSessionFile(key, data)
	}
	return nil
}

// storeless reports whether the session has no save store: none at all, or
// none beneath its authentication adapter, which keeps what it rewrites and
// lets everything else go.
func (client *Client) storeless() bool {
	switch store := client.saveStore.(type) {
	case nil:
		return true
	case *authenticationOptionStore:
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.base == nil
	case *authenticationCertificate58Store:
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.base == nil
	case *authenticationCertificate100Store:
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.base == nil
	}
	return false
}

// maxSessionFileBytes bounds what a session with no store keeps of the files
// its title wrote. The names and the lengths are the guest's, and the files
// stay for the whole session. Past it a store is refused, as a full store
// refuses one: the write waits in its buffer, where every read sees it.
const maxSessionFileBytes = 64 << 20

// keepSessionFile is the store of a session that has none. **A write is seen
// at once and after its close**, which is the first part of the save rule, and
// a session without a store keeps it the only way it can: in memory, for as
// long as the session runs. Nothing of it reaches a store or the next session,
// and a quick step does not carry it, because such a session takes none. An
// authentication adapter over no store answers first for the keys it keeps;
// see readFile.
func (client *Client) keepSessionFile(key string, data []byte) error {
	held := client.sessionFileBytes - len(client.sessionFiles[key]) + len(data)
	if held > maxSessionFileBytes {
		return fmt.Errorf("a session without a store keeps at most %d bytes of files", maxSessionFileBytes)
	}
	if client.sessionFiles == nil {
		client.sessionFiles = make(map[string][]byte)
	}
	client.sessionFiles[key], client.sessionFileBytes = bytes.Clone(data), held
	return nil
}

// forgetSessionFile drops what a session with no store kept of a path the
// title removed or renamed: the removal list answers for it from here on, and
// a later write keeps it again.
func (client *Client) forgetSessionFile(name string) {
	key, err := fileSaveKey(name)
	if err != nil {
		return
	}
	if data, kept := client.sessionFiles[key]; kept {
		client.sessionFileBytes -= len(data)
		delete(client.sessionFiles, key)
	}
}

// removeFile deletes a guest path. The stored bytes stay where they are —
// there is nothing to delete them with — and the removal list is what makes
// them unreachable until something writes the path again.
//
// **A removal the store refuses does not happen.** The list is given to the
// store before the session lets the file go (storeRemoval), and a refusal is
// answered with everything as it was: the file is there, to every read in the
// session and to the next one. MC_fsRemove answers it M_E_ERROR,
// FileSystem.remove in either form throws IOException, and deleteDataBase
// throws DataBaseException, as the common contract has a refused removal
// (docs/wipi-storage.md). Kept in the session and retried later, as a write is,
// the removal was one the title had been told succeeded and the next session
// could still undo.
//
// **What waits in a buffer open on the path goes with the file.** On a
// handset those writes were in the file the removal deleted, so neither the
// close of a handle that made them nor the ending may bring the file back
// with them. The handles stay open on an empty buffer, and a write through
// one of them is a write of the path, which brings it back.
func (client *Client) removeFile(name string) error {
	if err := client.storeRemoval(name); err != nil {
		return err
	}
	client.bumpFileEpoch(name)
	client.forgetSessionFile(name)
	if content := client.openContent(name); content != nil {
		content.data, content.dirty, content.synced = nil, false, client.fileEpoch(name)
	}
	return nil
}

// storeRemoval puts a path on the removal list and gives the list to the
// store, which is the removal as the store sees it. A store that refuses the
// list has the path taken off it again, and the refusal is answered: the list
// in the session is then what it was, which the store already holds unless an
// earlier write of it is still waiting, and that one stays waiting. A path
// with no save is not recorded, as markFileRemoved records none, and a session
// with no store, or one whose reads failed, keeps the list in memory, as every
// other write of such a session goes nowhere.
func (client *Client) storeRemoval(name string) error {
	if !hasSaveKey(name) {
		return nil
	}
	set := client.removedFiles()
	key := fileListKey(name)
	if set[key] {
		return nil
	}
	set[key] = true
	if client.saveStore == nil || client.saveReadError != nil {
		return nil
	}
	waiting := client.removedUnsaved
	if err := client.storeFileList(fileRemovedKey); err != nil {
		delete(set, key)
		client.removedUnsaved = waiting
		if client.logger != nil {
			client.logger.Debug("LGT removal refused by the store", "name", name, "error", err)
		}
		return err
	}
	return nil
}

// fileListMarker separates, in a stored path list, the entries a build before
// case-sensitive names wrote from the entries this build writes.
//
// **Those builds folded every entry to lower case**, so an entry of theirs
// stands for the file under any case: it hid "Save.dat" as well as
// "save.dat", and a write of either took it off the list. This build keeps
// an entry's case, because two names that differ only by case are two files,
// and an entry in lower case would otherwise read as either kind. So the
// entries an earlier build wrote come first, as it wrote them, and this
// build's come after the marker. No entry of an earlier build holds a capital
// letter, so none can be the marker, which does. A list with no entry of this
// build's is written without it, which is the layout those builds wrote.
const fileListMarker = "#Case-sensitive paths follow"

// createdFiles is the set of paths a title has written, read from the store
// once per session and kept in memory after that. A path the archive packages
// under that spelling is not in it: the listing finds those anyway, and
// recording them would grow the index by every file a title ever rewrote. What
// an earlier build recorded is kept beside it, in createdLegacy.
func (client *Client) createdFiles() map[string]bool {
	if client.created == nil {
		client.created, client.createdLegacy = client.loadFileList(fileCreatedKey)
	}
	return client.created
}

// removedFiles is the set of deleted paths, read from the store once per
// session and kept in memory after that. What an earlier build deleted is
// kept beside it, in removedLegacy.
func (client *Client) removedFiles() map[string]bool {
	if client.removed == nil {
		client.removed, client.removedLegacy = client.loadFileList(fileRemovedKey)
	}
	return client.removed
}

// loadFileList reads one of the two path lists: this build's entries, and the
// entries an earlier build wrote. A session with nothing to read starts both
// empty.
func (client *Client) loadFileList(key string) (exact, legacy map[string]bool) {
	exact, legacy = make(map[string]bool), make(map[string]bool)
	if client.saveStore == nil || client.saveReadError != nil {
		return exact, legacy
	}
	if data, ok := client.loadSave(key); ok {
		parseFileList(data, exact, legacy)
	}
	return exact, legacy
}

// parseFileList reads a stored path list into this build's entries and an
// earlier build's. See fileListMarker.
//
// **An entry that names no save is not read.** The lists say what the store
// holds and what it hides, so a path the store has no key for has no place in
// either; the builds before this one recorded such paths all the same — the
// database index among them, and a title's own "/.created" — which a listing
// then showed as files that were not there. The next store of the list leaves
// them out.
func parseFileList(data []byte, exact, legacy map[string]bool) {
	marked := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case marked:
			if line != "" && hasSaveKey(line) {
				exact[line] = true
			}
		case strings.TrimSpace(line) == fileListMarker:
			marked = true
		default:
			if line = strings.TrimSpace(line); line != "" && hasSaveKey(line) {
				legacy[line] = true
			}
		}
	}
}

// hasSaveKey reports whether a guest path has a save. See fileSaveKey.
func hasSaveKey(name string) bool {
	_, err := fileSaveKey(name)
	return err == nil
}

// formatFileList lays a path list out the way parseFileList reads it. A path
// holding a line break cannot be one line of the list, and is left out of it
// rather than read back as two paths.
func formatFileList(exact, legacy map[string]bool) []byte {
	lines := slices.Sorted(maps.Keys(legacy))
	if len(exact) != 0 {
		lines = append(lines, fileListMarker)
		for _, entry := range slices.Sorted(maps.Keys(exact)) {
			if !strings.Contains(entry, "\n") {
				lines = append(lines, entry)
			}
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// markFileCreated records one written path and writes the list back. A path
// with no save is not recorded, and neither is one the listing shows as
// packaged already; see packagedAs.
func (client *Client) markFileCreated(name string) {
	// The packaged check comes first because it reads nothing, which is what
	// lets a quick save store a packaged file without reading the list on the
	// live session (prepareIssuedWriteLists reads it only for other paths).
	if !hasSaveKey(name) || client.packagedAs(name) {
		return
	}
	set := client.createdFiles()
	key := fileListKey(name)
	if set[key] {
		return
	}
	set[key] = true
	if client.saveStore == nil || client.saveReadError != nil {
		return
	}
	if err := client.storeFileList(fileCreatedKey); err != nil && client.logger != nil {
		client.logger.Debug("LGT created list store failed", "name", name, "error", err)
	}
}

// packagedAs reports whether the archive packages a file at exactly the path a
// name is, in the name's own case: one the listing shows under that name
// without the created list.
//
// **A packaged file found only without regard to case is not one.** A read
// finds "data/hello.txt" when a title asks for "data/HELLO.TXT", but a save
// the title writes under "data/HELLO.TXT" is a file of its own, beside the
// packaged one, and the listing shows the packaged file in the archive's
// spelling. Asking the case-blind lookup left that save out of the list, so a
// title listing the directory to find its saves did not find it.
//
// The name is read the way the archive lookup reads it (archive.Resource): a
// title's raw EUC-KR spelling of a packaged Korean name is that packaged file,
// and so is a backslash spelling.
func (client *Client) packagedAs(name string) bool {
	if client.archive == nil {
		return false
	}
	path, err := safeEntryName(strings.TrimPrefix(archiveText([]byte(fileListKey(name))), "/"))
	if err != nil {
		return false
	}
	for _, files := range []map[string][]byte{client.archive.Resources, client.archive.Packaged, client.archive.supplemental} {
		if _, packaged := files[path]; packaged {
			return true
		}
	}
	return false
}

// storeFileList writes one of the three path lists as this session holds it,
// and keeps whether the store took it: a list the store refused is one memory
// is ahead of, until a later write of it goes through.
func (client *Client) storeFileList(key string) error {
	exact, legacy, unsaved := client.created, client.createdLegacy, &client.createdUnsaved
	switch key {
	case fileRemovedKey:
		exact, legacy, unsaved = client.removed, client.removedLegacy, &client.removedUnsaved
	case fileDirectoriesKey:
		exact, legacy, unsaved = client.directories, nil, &client.directoriesUnsaved
	}
	err := client.saveStore.StoreSave(key, formatFileList(exact, legacy))
	*unsaved = err != nil
	return err
}

// pathListsUnsaved names the path lists whose last write the store refused,
// in the order the ending and a quick step write them again.
func (client *Client) pathListsUnsaved() []string {
	var keys []string
	for _, list := range []struct {
		key     string
		unsaved bool
	}{
		{fileRemovedKey, client.removedUnsaved},
		{fileCreatedKey, client.createdUnsaved},
		{fileDirectoriesKey, client.directoriesUnsaved},
	} {
		if list.unsaved {
			keys = append(keys, list.key)
		}
	}
	return keys
}

// listDirectory answers the immediate children of one directory, in name
// order: the entries the archive packages under it, the paths a title has
// written under it, minus anything removed, and the directories it has made
// under it. A child is one path segment, so a directory two levels down
// contributes its own name once rather than each file inside it — which is
// what a listing is, and what the title reading this one wants: it lists a
// folder of song folders and reads each name as a number. A name that is a
// file, or no directory at all, holds nothing and lists empty.
//
// A path the title wrote is listed as it spelled it, under the directory as it
// spelled that: "Saves" and "saves" are two directories. What the archive
// packages is found without regard to case, as a read of it is, and is listed
// in the archive's own spelling. An entry an earlier build recorded is in
// lower case, and is listed as it was unless this build has recorded or
// removed the file under some case since, or a name already listed is that
// one in another case.
func (client *Client) listDirectory(name string) []string {
	prefix := guestPath(name)
	if prefix != "" {
		prefix += "/"
	}
	// childOf answers the first segment of a path below the directory, and
	// nothing for a path that is not below it.
	childOf := func(path string, anyCase bool) string {
		path = strings.TrimPrefix(path, "/")
		if len(path) <= len(prefix) {
			return ""
		}
		if head := path[:len(prefix)]; head != prefix && (!anyCase || !strings.EqualFold(head, prefix)) {
			return ""
		}
		rest := path[len(prefix):]
		if cut := strings.IndexByte(rest, '/'); cut >= 0 {
			rest = rest[:cut]
		}
		return rest
	}
	names := make([]string, 0, 8)
	shown, folded := make(map[string]bool), make(map[string]bool)
	add := func(child string) {
		if child == "" || shown[child] {
			return
		}
		shown[child], folded[strings.ToLower(child)] = true, true
		names = append(names, child)
	}
	// container reports whether a path is a database's container, which is
	// neither an entry nor a reason its directory is one (databaseContainers).
	// The index is read once, at the first path in a container's form. An
	// entry an earlier build recorded is in lower case and stands for the
	// path under any case, so it is matched without regard to case.
	var containers, foldedContainers map[string]bool
	read := false
	container := func(path string, anyCase bool) bool {
		key := fileListKey(path)
		if len(key) < len(databaseSuffix) || !strings.EqualFold(key[len(key)-len(databaseSuffix):], databaseSuffix) {
			return false
		}
		if !read {
			read, containers, foldedContainers = true, client.databaseContainers(), make(map[string]bool)
			for known := range containers {
				foldedContainers[strings.ToLower(known)] = true
			}
		}
		return containers[key] || anyCase && foldedContainers[strings.ToLower(key)]
	}
	if client.archive != nil {
		packaged := make(map[string]string)
		for _, files := range []map[string][]byte{client.archive.Resources, client.archive.Packaged, client.archive.supplemental} {
			for path := range files {
				child := childOf(path, true)
				if child == "" || client.fileRemoved(path) || container(path, false) {
					continue
				}
				// One name per packaged file: of two spellings, the one that
				// sorts first, as the archive's own lookup chooses.
				lower := strings.ToLower(child)
				if kept, seen := packaged[lower]; !seen || child < kept {
					packaged[lower] = child
				}
			}
		}
		for _, child := range packaged {
			add(child)
		}
	}
	for path := range client.createdFiles() {
		if !client.fileRemoved(path) && !container(path, false) {
			add(childOf(path, false))
		}
	}
	// A directory the title made is an entry of its parent whether or not
	// anything has been written into it, and makes every directory above it
	// one, as a file written below them does.
	for path := range client.madeDirectories() {
		add(childOf(path, false))
	}
	recorded := make(map[string]bool, len(client.created)+len(client.removed))
	for _, set := range []map[string]bool{client.created, client.removed} {
		for path := range set {
			recorded[strings.ToLower(path)] = true
		}
	}
	for entry := range client.createdLegacy {
		path := fileListKey(entry)
		if recorded[path] || client.fileRemoved(entry) || container(path, true) {
			continue
		}
		if child := childOf(path, true); !folded[strings.ToLower(child)] {
			add(child)
		}
	}
	sort.Strings(names)
	return names
}

// guestPath is a name as every storage call reads it, file or database
// (docs/wipi-storage.md): a backslash separates segments as a slash does, as
// the archive lookup reads it, and leading separators, empty segments and "."
// segments are dropped, so "/save.dat", "./save.dat", "//save.dat" and
// "save.dat" are one path, and "data\save.dat" is "data/save.dat". It is the
// form the path lists hold a path in, with the case the title gave it, and
// what the 255-byte bound counts. "" is the title's own directory. A ".."
// segment is kept, for fileSaveKey to refuse.
//
// Every save key is made through it, so a name that is a path already, as
// nearly every name is, comes back as it is without being taken apart.
func guestPath(name string) string {
	if isGuestPath(name) {
		return name
	}
	name = strings.ReplaceAll(name, "\\", "/")
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(name, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/")
}

// isGuestPath reports whether a name is in guestPath's form already: not
// empty, no backslash, and no empty or "." segment.
func isGuestPath(name string) bool {
	start := 0
	for index := 0; index <= len(name); index++ {
		if index < len(name) && name[index] == '\\' {
			return false
		}
		if index == len(name) || name[index] == '/' {
			if segment := name[start:index]; segment == "" || segment == "." {
				return false
			}
			start = index + 1
		}
	}
	return true
}

// canonicalFileName is the form the two path lists were keyed by in the
// builds before fileListKey: case folded and one leading slash dropped. It is
// kept to read what those builds wrote. See fileListMarker.
func canonicalFileName(name string) string {
	return strings.ToLower(strings.TrimPrefix(name, "/"))
}

// fileListKey is the form a path takes in the two path lists, in the epochs,
// and in the buffer the handles open on it share: the path of the file's save
// key under the scope. The store drops "." and empty segments from a key, so
// "./save.dat" and "//save.dat" are the one save "save.dat" is, and a removal
// under one spelling hides the file under every other. Case is kept, as the
// save key keeps it: "Save.dat" is another file, on every Host. A path that is
// not a save key is kept as the title named it, less one leading slash.
func fileListKey(name string) string {
	key, err := fileSaveKey(name)
	if err != nil {
		return strings.TrimPrefix(name, "/")
	}
	return strings.TrimPrefix(key, fileSaveScope)
}

// fileRemoved reports whether a path is on the removal list.
//
// An entry this build writes is a fileListKey and hides every spelling of
// that path, in that case. An entry an earlier build wrote is in lower case
// and hid the file under any case, and it still does: in its plain form it
// hides every spelling, and in another spelling ("./save.dat") it hides only
// that spelling, because the file may have been written under the plain one
// since, which that build did not take off the list. A write of the file
// clears either kind.
func (client *Client) fileRemoved(name string) bool {
	client.removedFiles()
	if len(client.removed) == 0 && len(client.removedLegacy) == 0 {
		return false
	}
	key := fileListKey(name)
	if client.removed[key] {
		return true
	}
	return len(client.removedLegacy) != 0 &&
		(client.removedLegacy[strings.ToLower(key)] || client.removedLegacy[canonicalFileName(name)])
}

// markFileRemoved records or clears one path and writes the list back. A
// clear takes this build's entry for the path, and every entry an earlier
// build wrote for the file under any case and any spelling: once written, the
// file is there under all of them, as that build's own write made it. A path
// with no save is not recorded, as it is not read; see parseFileList.
func (client *Client) markFileRemoved(name string, removed bool) {
	if !hasSaveKey(name) {
		return
	}
	set := client.removedFiles()
	key := fileListKey(name)
	if removed {
		if set[key] {
			return
		}
		set[key] = true
	} else {
		cleared := set[key]
		delete(set, key)
		if len(client.removedLegacy) != 0 {
			folded := strings.ToLower(key)
			for entry := range client.removedLegacy {
				if fileListKey(entry) == folded {
					delete(client.removedLegacy, entry)
					cleared = true
				}
			}
		}
		if !cleared {
			return
		}
	}
	if client.saveStore == nil || client.saveReadError != nil {
		return
	}
	if err := client.storeFileList(fileRemovedKey); err != nil && client.logger != nil {
		client.logger.Debug("LGT removal list store failed", "name", name, "error", err)
	}
}

// fileSaveKey is the save key a guest path is stored under.
//
// **The scope itself, the three path lists and the database index are not
// guest files.** The lists live under the same scope, at the keys a title's
// "/.removed", "/.created" and "/.directories" would have, the index of
// database names at the key "/__databases__.db" would have, and the scope is
// the directory every other key sits in. A title that wrote one of those names
// would put its bytes where the platform keeps its list, which the next
// session parses as paths, or a file where the directory has to be. So those
// names, and anything beneath them, have no save key, as a path with ".." has
// none, and the file calls refuse them as a name no file can have
// (M_E_BADFILENAME). Case is folded because a Host's filesystem may fold it.
//
// A backslash separates directories as a slash does, as the archive lookup
// reads it: a title's "data\save.dat" is "data/save.dat", so a write of it is
// stored and a removal of it hides what a read finds. The store refuses a
// backslash in a key, so no earlier build stored a save under one. The key is
// made from the name as guestPath reads it, which is the key the store would
// have made of it, so that a spelling the store would find too long whole —
// many leading separators, many "./" — is still the one file it names.
func fileSaveKey(name string) (string, error) {
	key, err := backend.NormalizeSaveKey(fileSaveScope + guestPath(name))
	if err != nil {
		return "", err
	}
	path, scoped := strings.CutPrefix(key, fileSaveScope)
	first, _, _ := strings.Cut(path, "/")
	if !scoped || strings.EqualFold(first, strings.TrimPrefix(fileRemovedKey, fileSaveScope)) ||
		strings.EqualFold(first, strings.TrimPrefix(fileCreatedKey, fileSaveScope)) ||
		strings.EqualFold(first, strings.TrimPrefix(fileDirectoriesKey, fileSaveScope)) ||
		strings.EqualFold(first, databaseIndexName) {
		return "", fmt.Errorf("%q names a key the platform keeps for itself", name)
	}
	return key, nil
}

// storageQuotaBytes is the room every title has: what MC_fsTotalSpace and
// FileSystem.totalSpace answer, and what MC_fsAvailable, FileSystem.available
// and DataBase.getSizeAvailable count down from (storageAvailable). It is this
// platform's own number rather than the host's free space: a handset gave a
// program a small fixed quota and the titles here check it against a save of
// a few kilobytes, while the store behind it is a directory with no limit
// worth reporting.
const storageQuotaBytes int32 = 1 << 20

// maxFileNameBytes is the longest name a file or a directory can have, which
// FileSystem.getMaxFilenameLength answers. The specification leaves
// MC_MAX_FILENAME_LENGTH to the handset, and the common contract
// (docs/wipi-storage.md) fixes it at 255 for every platform here. A longer
// name is M_E_LONGNAME to every C file call and an IOException to every Java
// one.
const maxFileNameBytes = 255

// fileAttributeDirectory is MH_FILE_IS_DIR, the attribute bit the v2.0 HAL
// gives a directory: what MC_fsFileAttribute sets for one.
const fileAttributeDirectory uint32 = 0x01

// fileNameCode is the check every file call makes on a name, C and Java, and
// whether the name is the title's own directory. A name longer than
// maxFileNameBytes is M_E_LONGNAME, counted on the path guestPath leaves: a
// C name's own bytes, a Java String's UTF-8 bytes, without the separators and
// "." segments it drops. The own directory is "", "/" or ".", and any
// spelling the separators and "." segments reduce to nothing: a name for a
// directory question, and no name a file can be kept under. Any other name the
// store cannot keep a file under — a ".." segment, one of the names the
// platform keeps for itself — is M_E_BADFILENAME, which is also what
// MC_fsRename answers for a new name of that kind.
func fileNameCode(name string) (root bool, code int32) {
	path := guestPath(name)
	if len(path) > maxFileNameBytes {
		return false, wipiLongName
	}
	if path == "" {
		return true, wipiSuccess
	}
	if !hasSaveKey(name) {
		return false, wipiBadFileName
	}
	return false, wipiSuccess
}

// fileExists reports whether a name is a file: one stored there, written by
// the title or shipped in its archive, and not removed, and not the container
// of a database (titleFile). It is what FileSystem.isFile answers.
func (client *Client) fileExists(name string) bool {
	_, ok := client.titleFile(name)
	return ok
}

// titleFile is readFile as the title's own file calls see a path: the
// container of a database is the runtime's storage and no file of the
// title's (databaseContainers), so under its name there is nothing, whatever
// the store holds there. The database code reads containers with readFile.
func (client *Client) titleFile(name string) ([]byte, bool) {
	if client.databaseContainer(name) {
		return nil, false
	}
	return client.readFile(name)
}

// directoryExists reports whether a name is a directory: the title's own
// directory, a name MC_fsMkDir or FileSystem.mkdir wrote down, or a name
// with anything beneath it — a file, or a directory made below it, which is
// how the listing shows such a name among its parent's entries. **A name that
// is a file is not a directory**, whatever was made or packaged under it.
func (client *Client) directoryExists(name string) bool {
	if root, _ := fileNameCode(name); root {
		return true
	}
	return !client.fileExists(name) && client.holdsDirectory(name)
}

// holdsDirectory is directoryExists without the question whether the name is
// a file, for a caller that has asked it already: the name was made, or
// something lies beneath it.
func (client *Client) holdsDirectory(name string) bool {
	return client.directoryMade(name) || len(client.listDirectory(name)) > 0
}

// fileDirectoriesKey lists the directories a title has made. The store keeps
// files under their full names and makes no directory entries, so a directory
// the title made and has not written into is nowhere but here; without the
// list it would be gone after a restart, and a title that makes its save
// directory and asks whether it is there would be told no. It lives beside the
// other two path lists and is the platform's, as they are: fileSaveKey gives
// no title's path a key there. Its lines are paths in the form the other two
// lists hold them (fileListKey), case kept.
const fileDirectoriesKey = fileSaveScope + ".directories"

// madeDirectories is the set of directories the title has made, read from the
// store once per session and kept in memory after that.
func (client *Client) madeDirectories() map[string]bool {
	if client.directories == nil {
		exact, legacy := client.loadFileList(fileDirectoriesKey)
		// The list is newer than any build that folded paths to lower case,
		// so whatever it holds is read as this build's entries.
		for entry := range legacy {
			exact[entry] = true
		}
		client.directories = exact
	}
	return client.directories
}

// directoryMade reports whether a name is on the directory list.
func (client *Client) directoryMade(name string) bool {
	return client.madeDirectories()[fileListKey(name)]
}

// markDirectoryMade records or forgets one directory and writes the list
// back. A list the store refuses waits here, as the other two lists do, for
// the next change, the ending and a quick step.
func (client *Client) markDirectoryMade(name string, made bool) {
	if !hasSaveKey(name) {
		return
	}
	set := client.madeDirectories()
	key := fileListKey(name)
	if set[key] == made {
		return
	}
	if made {
		set[key] = true
	} else {
		delete(set, key)
	}
	if client.saveStore == nil || client.saveReadError != nil {
		return
	}
	if err := client.storeFileList(fileDirectoriesKey); err != nil && client.logger != nil {
		client.logger.Debug("LGT directory list store failed", "name", name, "error", err)
	}
}

// storageAvailable is what MC_fsAvailable, FileSystem.available and
// DataBase.getSizeAvailable answer: storageQuotaBytes less what the title has
// stored, never below zero (storageUsed). **Nothing is refused for room**: the
// figure is reported, and a save is never turned away to keep it true.
func (client *Client) storageAvailable() int32 {
	used := client.storageUsed()
	if used >= int64(storageQuotaBytes) {
		return 0
	}
	return storageQuotaBytes - int32(used)
}

// storageUsed is what the title has stored, as the common contract counts it:
// for each file it has written, its size less the size of the file of the
// same name its archive ships, and for each database the bytes of its records.
// The platform's own lists and the database index are not counted, and a
// database is counted once, as its records: in its own scope it is no file,
// and among the files, where a session whose store refused the move keeps it,
// its container is left out of the files. A file the title wrote shorter than
// the one its archive ships adds nothing rather than taking the difference off
// another file's.
//
// A file the title wrote is one on the created list, one of the archive's own
// paths that has a save, or one waiting in an open buffer. The archive's paths
// are asked about once and then only again after a write of theirs
// (savedSize), because an archive holds hundreds of them and a title may ask
// for the figure often.
func (client *Client) storageUsed() int64 {
	var used int64
	containers := make(map[string]bool)
	amongFiles := client.databasesAmongFiles()
	for _, name := range client.databaseNames() {
		if amongFiles {
			containers[fileListKey(databaseFileName(name))] = true
		}
		used += client.databaseRecordBytes(name)
	}
	counted := make(map[string]bool)
	count := func(path string) {
		key := fileListKey(path)
		if counted[key] || containers[key] || !hasSaveKey(path) {
			return
		}
		counted[key] = true
		size, saved := client.savedSize(path)
		if !saved {
			return
		}
		shipped := 0
		if client.archive != nil {
			if data, ok := client.archive.Resource(key); ok {
				shipped = len(data)
			}
		}
		if size > shipped {
			used += int64(size - shipped)
		}
	}
	for _, file := range client.openContents() {
		count(file.name)
	}
	for path := range client.createdFiles() {
		count(path)
	}
	// An entry an earlier build recorded is in lower case and stands for the
	// file under any case, unless this build has recorded or removed the file
	// under some case since, as the listing reads it.
	recorded := make(map[string]bool, len(client.created)+len(client.removed))
	for _, set := range []map[string]bool{client.created, client.removed} {
		for path := range set {
			recorded[strings.ToLower(path)] = true
		}
	}
	for entry := range client.createdLegacy {
		if !recorded[fileListKey(entry)] {
			count(entry)
		}
	}
	if client.archive != nil {
		for _, files := range []map[string][]byte{client.archive.Resources, client.archive.Packaged, client.archive.supplemental} {
			for _, path := range slices.Sorted(maps.Keys(files)) {
				count(path)
			}
		}
	}
	return used
}

// storedSize is what the space count last read of one path's save, and the
// path's epoch then: whether there was a save and how many bytes it held, and
// for a database's container what its records hold. Each half is known only
// once it has been read at that epoch.
type storedSize struct {
	epoch        uint64
	sizeKnown    bool
	size         int
	saved        bool
	recordsKnown bool
	records      int
}

// savedSize answers how many bytes the title has saved under a path, and
// whether it has saved anything there: a write waiting in an open buffer, or
// what the store or a session with no store holds, and nothing for a removed
// path. The archive's own copy is not a save. What the store answered is
// kept with the path's epoch, which every store and removal of the path in
// the session moves on.
func (client *Client) savedSize(path string) (int, bool) {
	if content := client.openContent(path); content != nil && content.dirty {
		return len(content.data), true
	}
	if client.fileRemoved(path) {
		return 0, false
	}
	key, err := fileSaveKey(path)
	if err != nil {
		return 0, false
	}
	epoch := client.fileEpoch(path)
	known, ok := client.storedSizes[key]
	if ok && known.epoch == epoch && known.sizeKnown {
		return known.size, known.saved
	}
	var data []byte
	saved := false
	if client.saveStore != nil {
		data, saved = client.loadSave(key)
	}
	if !saved {
		data, saved = client.sessionFiles[key]
	}
	if !ok || known.epoch != epoch {
		known = storedSize{epoch: epoch}
	}
	known.sizeKnown, known.size, known.saved = true, len(data), saved
	if client.storedSizes == nil {
		client.storedSizes = make(map[string]storedSize)
	}
	client.storedSizes[key] = known
	return len(data), saved
}

// maxFileBytes is how far a write may grow a file and a seek may move a cursor
// past what the file already holds: the common contract's bound on untrusted
// input, 4 MiB for one file (docs/wipi-storage.md). A title's offset and
// length are guest words, and without a bound one stray seek and a one-byte
// write size a host allocation of gigabytes. It is four times the room the
// space slots report, which the titles here check a save of a few kilobytes
// against, so nothing a title saves comes near it; a packaged file already
// larger is still read and written within its own length. A write past it
// fails as a write does — M_E_NOSPACE from MC_fsWrite, an IOException from a
// File or a stream on one — and a seek past it as a bad seek does.
const maxFileBytes = 4 << 20

// extend grows a file's buffer with zeros so that it reaches end, and reports
// false, leaving it as it was, for an end past maxFileBytes that the buffer
// does not reach already.
func (file *openFile) extend(end int) bool {
	if end <= len(file.data) {
		return true
	}
	if end > maxFileBytes {
		return false
	}
	file.data = append(file.data, make([]byte, end-len(file.data))...)
	return true
}

// loadSave records an unrecoverable read failure so a native boundary reports
// it instead of allowing an absent-file fallback to overwrite the old save.
func (client *Client) loadSave(name string) ([]byte, bool) {
	data, present, err := backend.ReadSave(client.saveStore, name)
	if err != nil && client.saveReadError == nil {
		client.saveReadError = fmt.Errorf("read save %s: %w", name, err)
	}
	return data, present
}

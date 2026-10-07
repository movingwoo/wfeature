package testfixture

import (
	"encoding/binary"
	"fmt"
)

// The KTF native save fixture is a native package whose module is authored
// here in ARM instructions, for the tests that need a title of that kind to
// write, read and hold a save on purpose. It starts through the ordinary loader
// the way KTFNativeCheckpointArchive does, asks the platform for its file
// interface, and then does nothing until a key arrives. Each key is one action
// on that interface, finished inside the call that delivers the press:
//
//	SAVE      opens the save emptied, writes both parts and closes it.
//	PATCH     opens the save as it is, writes part A over its first four
//	          bytes and closes it.
//	READ      opens the save for reading, fills the status, length and seen
//	          words, and closes it.
//	HOLD      opens the save as it is, writes part A and keeps the object.
//	TRUNCATE  opens the save emptied and keeps the object without writing.
//	FINISH    writes part B through the kept object, where its cursor
//	          stands, and closes it.
//
// Part A is the progress word and part B is the progress word XOR
// KTFNativeSaveMask, both read when the key arrives, so a test changes the
// progress word between two keys to tell their writes apart.
//
// There is no DELETE. The file interface the platform serves this package is
// open, information, create, exists and an error report, and a file object
// closes, reads, writes, seeks and reports its own record: nothing removes a
// file, so the digit between READ and HOLD is left without an action.
//
// **A write is not in the save store when its call returns.** The platform
// keeps what a title wrote in NativePlatform.written and marks the name in
// NativePlatform.unsaved, and it hands what is marked to the store when any
// file is closed, when a frame the title asked for ends, and when the session
// closes. SAVE and PATCH close their file, so the store has them by the time
// the key returns. What HOLD wrote and the emptying TRUNCATE asked for stay in
// those two tables: until the next frame ends in the form that registers one,
// and in the form that registers none until FINISH, until another key closes a
// file — READ's close stores everything pending, like any other — or until the
// session closes.
//
// The same table answers a READ. Every key opens the save by name, and a name
// this session has written is answered from NativePlatform.written for as long
// as the session lasts; only a name it has not written is looked up in the
// store, each time it is opened.
//
// One object is kept at a time: HOLD and TRUNCATE do nothing while one is
// kept, and FINISH does nothing without one. Opening for writing creates a file
// that is not there, so PATCH and HOLD on a missing save leave four bytes; and
// FINISH after TRUNCATE leaves part B alone at the front of the file, because
// that is where the cursor of an emptied file stands.
//
// READ answers KTFNativeSaveStatusError when the read moved another number of
// bytes than the open file's own record promised. Nothing on this platform does
// that today — a store that cannot be read stops the key's call with the
// store's error instead of answering the module — so it is what a test serving
// its own read slot sees.
const (
	// KTFNativeSaveApplicationID names the fixture's applet, and
	// KTFNativeSaveOwner is the save owner the platform derives from it: the
	// directory a Host test places this fixture's saves under.
	KTFNativeSaveApplicationID = 0x10203050
	KTFNativeSaveOwner         = "270544976"

	// KTFNativeSaveName is the file the module opens, and
	// KTFNativeSaveStoreKey is where the platform keeps it in a save store.
	KTFNativeSaveName     = "save.dat"
	KTFNativeSaveStoreKey = "fs/" + KTFNativeSaveName

	// KTFNativeSaveMask is what part B differs from part A by.
	KTFNativeSaveMask uint32 = 0x5aa5c33c
)

// The keys, in the codes a Host sends. A digit is its own character there and
// in the WIPI codes NativeSession.SendKey takes, and the platform turns it into
// the code the module compares against. '4' is the DELETE this surface has no
// call for.
const (
	KTFNativeSaveKeySave     = '1'
	KTFNativeSaveKeyPatch    = '2'
	KTFNativeSaveKeyRead     = '3'
	KTFNativeSaveKeyHold     = '5'
	KTFNativeSaveKeyTruncate = '6'
	KTFNativeSaveKeyFinish   = '7'
)

// What READ leaves in the status word.
const (
	KTFNativeSaveStatusMissing = 0
	KTFNativeSaveStatusFound   = 1
	KTFNativeSaveStatusError   = 2
)

// The module keeps its whole state in the object its factory hands out, which
// sits at a fixed place in the image so that a test can read a word of it by
// address.
const (
	ktfNativeSaveObject = ktfImageBase + 0x800

	// KTFNativeSaveStartupCounter is incremented by the start event. Restoring
	// a checkpoint must recover this word without incrementing it again.
	KTFNativeSaveStartupCounter = ktfNativeSaveObject + 0x08
	// KTFNativeSaveFrameCounter counts the frames of the form that registers
	// one, and is what the red channel of its screen shows. It stays zero in
	// the form that registers none.
	KTFNativeSaveFrameCounter = ktfNativeSaveObject + 0x0c
	// KTFNativeSaveHandle is the file object HOLD or TRUNCATE kept, and zero
	// when none is kept.
	KTFNativeSaveHandle = ktfNativeSaveObject + 0x18
	// KTFNativeSaveProgress is the word a test writes and the module saves.
	KTFNativeSaveProgress = ktfNativeSaveObject + 0x1c
	// KTFNativeSaveSeenA and KTFNativeSaveSeenB are the two parts as READ last
	// found them. READ clears both before it opens the save, so a part the file
	// was too short to hold reads as zero.
	KTFNativeSaveSeenA = ktfNativeSaveObject + 0x20
	KTFNativeSaveSeenB = ktfNativeSaveObject + 0x24
	// KTFNativeSaveStatus is one of the three status values above, and
	// KTFNativeSaveLength is the length the open file reported: eight for a
	// whole save, and zero beside KTFNativeSaveStatusMissing.
	KTFNativeSaveStatus = ktfNativeSaveObject + 0x28
	KTFNativeSaveLength = ktfNativeSaveObject + 0x2c
)

// KTFNativeSaveFile is the save as the store holds it once part A was written
// at progress a and part B at progress b.
func KTFNativeSaveFile(a, b uint32) []byte {
	file := binary.LittleEndian.AppendUint32(nil, a)
	return binary.LittleEndian.AppendUint32(file, b^KTFNativeSaveMask)
}

// KTFNativeSaveArchive is the form whose start event registers a frame
// callback, as KTFNativeCheckpointArchive does. Every frame counts, fills the
// screen with the count and presents it, and the platform stores what is
// pending when the frame ends.
func KTFNativeSaveArchive() ([]byte, error) { return ktfNativeSaveArchive(true) }

// KTFNativeSaveArchiveWithoutFrame is the form that registers none. Nothing
// runs between two keys and a tick reaches no boundary, so a write that no
// close followed stays pending until the session is closed.
func KTFNativeSaveArchiveWithoutFrame() ([]byte, error) { return ktfNativeSaveArchive(false) }

func ktfNativeSaveArchive(frame bool) ([]byte, error) {
	// The platform's own numbers, as the platform package serves them: an
	// event, a key, two interfaces, and the byte offset a module indexes each
	// table with. They are spelt out rather than imported because the platform
	// package's tests use this fixture too.
	const (
		eventKeyDown = 0x101
		keyDigitBase = 0xe021

		interfaceScreen = 0x1001001
		interfaceFiles  = 0x1001003

		platformQueryInterface = 0x08
		platformSchedule       = 0x2c
		screenRectangle        = 0x14
		screenPresent          = 0x1c
		filesOpen              = 0x08
		fileClose              = 0x04
		fileRead               = 0x0c
		fileWrite              = 0x14
		fileStatus             = 0x18

		// 1 is the only mode that cannot write, 4 empties a file that exists,
		// and any other writes over what is there.
		openRead     = 1
		openWrite    = 2
		openTruncate = 4

		// recordLength is where an open file's record keeps its length.
		recordLength = 0x08
	)
	// The object's fields, by byte offset. The ones a test reads are where the
	// exported addresses say.
	const (
		platform = 0x04 // the object the entry was handed
		startup  = KTFNativeSaveStartupCounter - ktfNativeSaveObject
		frames   = KTFNativeSaveFrameCounter - ktfNativeSaveObject
		screen   = 0x10 // the screen interface, in the form that draws
		files    = 0x14 // the file interface
		kept     = KTFNativeSaveHandle - ktfNativeSaveObject
		progress = KTFNativeSaveProgress - ktfNativeSaveObject
		seen     = KTFNativeSaveSeenA - ktfNativeSaveObject
		status   = KTFNativeSaveStatus - ktfNativeSaveObject
		length   = KTFNativeSaveLength - ktfNativeSaveObject
		scratch  = 0x30 // two words: the parts a write hands over
		record   = 0x38 // three words: the record an open file reports
	)
	// The module's own tables, which sit behind the object in its image.
	const (
		objectTable = ktfNativeSaveObject + 0x60
		entryRecord = ktfNativeSaveObject + 0x80
		entryTable  = ktfNativeSaveObject + 0xb0
		saveName    = ktfNativeSaveObject + 0xc0
	)
	const always, equal, notEqual, higher = 0xe, 0x0, 0x1, 0x8

	// The instructions are laid out with the assembler lgt.go authors its Clet
	// with: an ARM word is the same word on both platforms. r4 holds the object
	// in everything the platform calls, and r5 the file object in hand.
	a := &lgtAssembler{base: ktfImageBase}
	eor := func(rd, rn, rm uint32) uint32 { return 0xe0200000 | rn<<16 | rd<<12 | rm }
	sub := func(rd, rn, rm uint32) uint32 { return 0xe0400000 | rn<<16 | rd<<12 | rm }
	cmp := func(rn, rm uint32) uint32 { return 0xe1500000 | rn<<16 | rm }
	// when makes one instruction conditional.
	when := func(condition, word uint32) uint32 { return word&0x0fffffff | condition<<28 }
	// method calls the function at offset in the table the object in r0 points
	// at, which is how the two halves of this package call each other.
	method := func(offset uint32) {
		a.emit(lgtLdr(12, 0, 0), lgtLdr(12, 12, offset))
		a.call(12)
	}

	// entry(platform, scratch, out): answer with the record the factory is
	// found through. The loader enters a module at its first word.
	a.literal(3, entryRecord)
	a.emit(lgtStr(3, 2, 0), lgtMovImm(0, 1), lgtBXLR)

	// Raising and dropping a count, which both tables open with.
	counted := a.here()
	a.emit(lgtMovImm(0, 1), lgtBXLR)

	// factory(record, platform, identifier, out): hand out the object and keep
	// the platform's in it.
	factory := a.here()
	a.literal(0, ktfNativeSaveObject)
	a.emit(lgtStr(0, 3, 0), lgtStr(1, 0, platform), lgtMovImm(0, 1), lgtBXLR)

	// draw(object): count, put the count in the red channel of the whole
	// screen, present it. It is the frame one form registers.
	var draw uint32
	if frame {
		draw = a.here()
		a.emit(lgtPushLR, lgtMovReg(4, 0))
		a.emit(lgtLdr(3, 4, frames), lgtAddImm(3, 3, 1), lgtStr(3, 4, frames))
		a.emit(0xe1a03c03) // mov r3, r3, lsl #24
		a.emit(lgtLdr(0, 4, screen), lgtMovImm(1, 0), lgtMovImm(2, 0))
		method(screenRectangle)
		a.emit(lgtLdr(0, 4, screen))
		method(screenPresent)
		a.emit(lgtMovImm(0, 1), lgtPopPC)
	}

	// event(object, event, first, second): the start event, a key going down,
	// or nothing. A press arrives as two events and its release as a third;
	// only the first of the three acts, so one press is one action. The digit
	// of a key is its code less the code of the zero key, and r6 holds it.
	event := a.here()
	a.emit(lgtPushLR, lgtMovReg(4, 0), lgtCmpImm(1, 0))
	started := a.branch(equal, 0)
	a.literal(3, eventKeyDown)
	a.emit(cmp(1, 3))
	ignored := a.branch(notEqual, 0)
	a.literal(3, keyDigitBase)
	a.emit(sub(6, 2, 3))

	// done is where every path of the handler ends.
	var done uint32
	// open asks the file interface for the save in one mode and leaves the
	// file object in r5, or ends the handler when the open is refused.
	open := func(mode uint32) {
		a.emit(lgtLdr(0, 4, files))
		a.literal(1, saveName)
		a.emit(lgtMovImm(2, mode))
		method(filesOpen)
		a.emit(lgtMovReg(5, 0), lgtCmpImm(5, 0))
		a.branch(equal, done)
	}
	// write hands count bytes of one of the object's fields to the file in r5.
	write := func(field, count uint32) {
		a.emit(lgtMovReg(0, 5), lgtAddImm(1, 4, field), lgtMovImm(2, count))
		method(fileWrite)
	}
	shut := func() {
		a.emit(lgtMovReg(0, 5))
		method(fileClose)
	}
	// partB leaves the second part in the scratch word at offset.
	partB := func(offset uint32) {
		a.emit(lgtLdr(1, 4, progress))
		a.literal(2, KTFNativeSaveMask)
		a.emit(eor(1, 1, 2), lgtStr(1, 4, scratch+offset))
	}
	// unlessKept ends the handler when an object is already kept.
	unlessKept := func() {
		a.emit(lgtLdr(0, 4, kept), lgtCmpImm(0, 0))
		a.branch(notEqual, done)
	}
	actions := []struct {
		digit uint32
		emit  func()
	}{
		{KTFNativeSaveKeySave - '0', func() {
			open(openTruncate)
			a.emit(lgtLdr(1, 4, progress), lgtStr(1, 4, scratch))
			partB(4)
			write(scratch, 8)
			shut()
		}},
		{KTFNativeSaveKeyPatch - '0', func() {
			open(openWrite)
			write(progress, 4)
			shut()
		}},
		{KTFNativeSaveKeyRead - '0', func() {
			// A refused open is a save that is not there, which the cleared
			// status already says.
			a.emit(lgtMovImm(0, KTFNativeSaveStatusMissing),
				lgtStr(0, 4, seen), lgtStr(0, 4, seen+4), lgtStr(0, 4, status), lgtStr(0, 4, length))
			open(openRead)
			a.emit(lgtAddImm(1, 4, record))
			method(fileStatus)
			a.emit(lgtLdr(6, 4, record+recordLength), lgtStr(6, 4, length))
			a.emit(lgtMovReg(0, 5), lgtAddImm(1, 4, seen), lgtMovImm(2, 8))
			method(fileRead)
			// The read should have moved both parts, or all of a shorter file.
			a.emit(lgtCmpImm(6, 8), when(higher, lgtMovImm(6, 8)))
			a.emit(lgtMovImm(1, KTFNativeSaveStatusFound), cmp(0, 6),
				when(notEqual, lgtMovImm(1, KTFNativeSaveStatusError)), lgtStr(1, 4, status))
			shut()
		}},
		{KTFNativeSaveKeyHold - '0', func() {
			unlessKept()
			open(openWrite)
			a.emit(lgtStr(5, 4, kept))
			write(progress, 4)
		}},
		{KTFNativeSaveKeyTruncate - '0', func() {
			unlessKept()
			open(openTruncate)
			a.emit(lgtStr(5, 4, kept))
		}},
		{KTFNativeSaveKeyFinish - '0', func() {
			a.emit(lgtLdr(5, 4, kept), lgtCmpImm(5, 0))
			a.branch(equal, done)
			partB(0)
			write(scratch, 4)
			shut()
			a.emit(lgtMovImm(0, 0), lgtStr(0, 4, kept))
		}},
	}
	chosen := make([]int, len(actions))
	for index, action := range actions {
		a.emit(lgtCmpImm(6, action.digit))
		chosen[index] = a.branch(equal, 0)
	}
	done = a.here()
	a.patch(ignored, notEqual, done)
	a.emit(lgtMovImm(0, 1), lgtPopPC)
	for index, action := range actions {
		a.patch(chosen[index], equal, a.here())
		action.emit()
		a.branch(always, done)
	}

	// The start event counts itself, asks for the interfaces the module calls,
	// and in one form registers the frame.
	a.patch(started, equal, a.here())
	a.emit(lgtLdr(0, 4, startup), lgtAddImm(0, 0, 1), lgtStr(0, 4, startup))
	query := func(identifier, field uint32) {
		a.emit(lgtLdr(0, 4, platform))
		a.literal(1, identifier)
		a.emit(lgtAddImm(2, 4, field))
		method(platformQueryInterface)
	}
	query(interfaceFiles, files)
	if frame {
		query(interfaceScreen, screen)
		a.emit(lgtLdr(0, 4, platform), lgtMovImm(1, 16))
		a.literal(2, draw)
		a.emit(lgtMovReg(3, 4))
		method(platformSchedule)
	}
	a.branch(always, done)

	code := a.finish()
	if len(code) > int(ktfNativeSaveObject-ktfImageBase) {
		return nil, fmt.Errorf("KTF native save fixture code is %d bytes and runs into its object", len(code))
	}
	module := make([]byte, 0x1000)
	copy(module, code)
	put := func(address uint32, values ...uint32) {
		for index, value := range values {
			binary.LittleEndian.PutUint32(module[address-ktfImageBase+uint32(index)*4:], value)
		}
	}
	// The object opens with its table: raise, drop and the event handler. The
	// entry's record opens with a table of the same shape, whose third entry is
	// the factory.
	put(ktfNativeSaveObject, objectTable)
	put(objectTable, counted, counted, event)
	put(entryRecord, entryTable, 1)
	put(entryTable, counted, counted, factory)
	copy(module[saveName-ktfImageBase:], KTFNativeSaveName+"\x00")

	// One numeric applet record, a three-word identity trailer and the
	// package's closing marker, laid out as KTFNativeCheckpointArchive's are.
	info := make([]byte, 0x74)
	word := func(offset int, value uint32) { binary.LittleEndian.PutUint32(info[offset:], value) }
	word(0x08, 0x20)
	word(0x0c, 0x28)
	word(0x10, 0x48)
	word(0x14, 1)
	word(0x18, 0x50)
	word(0x48, 0x50)
	word(0x4c, 0x64)
	word(0x50, KTFNativeSaveApplicationID)
	word(0x58, 1000)
	word(0x60, ktfImageBase)
	word(0x64, 1)
	word(0x68, KTFNativeSaveApplicationID)
	word(0x6c, 0xff)
	copy(info[0x70:], "1fim")
	return fixtureZIP([]fixtureEntry{{"storage.mod", module}, {"storage.mif", info}})
}

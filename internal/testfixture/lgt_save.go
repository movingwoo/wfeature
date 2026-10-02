package testfixture

import (
	"encoding/binary"
	"fmt"
)

// The LGT save fixtures are the checkpoint Clet's two siblings: a Clet and an
// AOT Java title that keep a save and change it when they are told to. A test
// writes a number into the guest, presses a key, and then reads what the title
// did with it in two places — the guest's own words, and the save store.
//
// Both follow one contract. The save holds two four-byte parts: A is the
// progress word, and B is the progress word with LGTSaveXOR folded in, so a
// test can tell which of the two a write reached. One key press is one action,
// finished inside the tick that delivers the key:
//
//	LGTSaveKeySave      write both parts, creating the save if it is absent
//	LGTSaveKeyPatch     write part A only, in place, without truncating
//	LGTSaveKeyRead      read both parts into the seen words; set status and length
//	LGTSaveKeyDelete    delete the save
//	LGTSaveKeyHold      open the save for writing, write part A, keep it open
//	LGTSaveKeyTruncate  open the save with truncation and keep it open, unwritten
//	LGTSaveKeyFinish    write part B through what was kept, and close it
//
// Every word the checkpoint Clet exports is at the same address in both and
// means the same thing, so a test written against LGTCheckpointArchive reads
// these two the same way.
const (
	// LGTSaveProgress is the word a test writes and the title saves.
	LGTSaveProgress = lgtDataBase + 0x140
	// LGTSaveSeenA and LGTSaveSeenB are what the title last read back. A read
	// clears both first, so a part the save does not hold reads as zero.
	LGTSaveSeenA = lgtDataBase + 0x144
	LGTSaveSeenB = lgtDataBase + 0x148
	// LGTSaveStatus is how the last read ended: one of the three below.
	LGTSaveStatus = lgtDataBase + 0x14c
	// LGTSaveLength is how many bytes of save the last read found: the size of
	// the file, or the lengths of the two records added together.
	LGTSaveLength = lgtDataBase + 0x150
	// LGTSaveHeld is what LGTSaveKeyHold or LGTSaveKeyTruncate left open, and
	// zero when nothing is. In the Clet it is the handle MC_fsOpen answered,
	// which is the key of the platform's own table of open files.
	LGTSaveHeld = lgtDataBase + 0x154

	lgtSaveScratch = lgtDataBase + 0x158 // the two parts, on their way to a write
	lgtSaveName    = lgtDataBase + 0x160 // the save's name, as the title spells it

	// LGTSaveXOR is what separates part B from part A.
	LGTSaveXOR uint32 = 0x5a5aa5a5

	// LGTSaveFileName is the save as the title names it, and LGTSaveFileKey the
	// save store's key for it.
	LGTSaveFileName = "save.dat"
	LGTSaveFileKey  = "fs/" + LGTSaveFileName

	// LGTSaveOwner is the Clet's own save owner.
	LGTSaveOwner = "PF0000SV"
)

// What LGTSaveStatus holds after a read.
const (
	LGTSaveStatusMissing uint32 = 0
	LGTSaveStatusFound   uint32 = 1
	LGTSaveStatusError   uint32 = 2
)

// The keys, as a Host sends them. None of them is one of the pad's, so a press
// reaches the title as one press whatever else is held.
const (
	LGTSaveKeySave     int32 = '1'
	LGTSaveKeyPatch    int32 = '3'
	LGTSaveKeyRead     int32 = '5'
	LGTSaveKeyDelete   int32 = '7'
	LGTSaveKeyHold     int32 = '9'
	LGTSaveKeyTruncate int32 = '*'
	LGTSaveKeyFinish   int32 = '#'
)

// The filesystem block, in the import table's own numbers, and the flags
// MC_fsOpen takes.
const (
	lgtSlotFsOpen   uint32 = 0x190
	lgtSlotFsRead   uint32 = 0x191
	lgtSlotFsWrite  uint32 = 0x192
	lgtSlotFsClose  uint32 = 0x193
	lgtSlotFsSeek   uint32 = 0x194
	lgtSlotFsRemove uint32 = 0x196

	lgtFileReadOnly  uint32 = 1
	lgtFileTruncate  uint32 = 4
	lgtFileReadWrite uint32 = 8

	// lgtEventKeyPressed is the kind a Clet's key press arrives as, and
	// lgtNoEntry what MC_fsOpen answers for a file that is not there.
	lgtEventKeyPressed uint32 = 502
	lgtNoEntry         uint32 = 12
)

const (
	lgtGlobalFsOpen = lgtGlobalSetTimer + 1 + iota
	lgtGlobalFsRead
	lgtGlobalFsWrite
	lgtGlobalFsClose
	lgtGlobalFsSeek
	lgtGlobalFsRemove
)

// Condition codes, and the registers that have a name.
const (
	lgtEQ uint32 = 0x0
	lgtNE uint32 = 0x1
	lgtLO uint32 = 0x3
	lgtLT uint32 = 0xb
	lgtAL uint32 = 0xe

	lgtIP uint32 = 12
	lgtSP uint32 = 13
	lgtLR uint32 = 14
)

const (
	lgtPushAll = 0xe92d4ff0 // push {r4-r11, lr}
	lgtPopAll  = 0xe8bd8ff0 // pop  {r4-r11, pc}
)

func lgtMvnImm(rd, value uint32) uint32     { return 0xe3e00000 | rd<<12 | value&0xff }
func lgtSubImm(rd, rn, value uint32) uint32 { return 0xe2400000 | rn<<16 | rd<<12 | value&0xff }
func lgtAddReg(rd, rn, rm uint32) uint32    { return 0xe0800000 | rn<<16 | rd<<12 | rm }
func lgtEorReg(rd, rn, rm uint32) uint32    { return 0xe0200000 | rn<<16 | rd<<12 | rm }
func lgtCmpReg(rn, rm uint32) uint32        { return 0xe1500000 | rn<<16 | rm }
func lgtCmnImm(rn, value uint32) uint32     { return 0xe3700000 | rn<<16 | value&0xff }
func lgtLslImm(rd, rm, shift uint32) uint32 { return 0xe1a00000 | rd<<12 | shift<<7 | rm }
func lgtLdrsh(rd, rn uint32) uint32         { return 0xe1d000f0 | rn<<16 | rd<<12 }
func lgtBX(rm uint32) uint32                { return 0xe12fff10 | rm }

// lgtAddShift is `add rd, rn, rm, lsl #shift`.
func lgtAddShift(rd, rn, rm, shift uint32) uint32 {
	return 0xe0800000 | rn<<16 | rd<<12 | shift<<7 | rm
}

// lgtProgram lays out ARM words the way lgtAssembler does, with the two things
// a longer module needs: a branch to a name that is defined further on, and a
// literal pool after each routine, so no load has to reach past the four
// kilobytes its offset has room for.
type lgtProgram struct {
	base     uint32
	words    []uint32
	labels   map[string]uint32
	branches []lgtProgramBranch
	loads    []lgtLoad
	literals []uint32
	err      error
}

type lgtProgramBranch struct {
	word   int
	opcode uint32
	name   string
}

func newLGTProgram(base uint32) *lgtProgram {
	return &lgtProgram{base: base, labels: map[string]uint32{}}
}

func (p *lgtProgram) emit(words ...uint32) { p.words = append(p.words, words...) }
func (p *lgtProgram) here() uint32         { return p.base + uint32(len(p.words))*4 }

func (p *lgtProgram) fail(format string, arguments ...any) {
	if p.err == nil {
		p.err = fmt.Errorf("LGT fixture module: "+format, arguments...)
	}
}

// label names the next word, and answers its address.
func (p *lgtProgram) label(name string) uint32 {
	if _, defined := p.labels[name]; defined {
		p.fail("%s is defined twice", name)
	}
	p.labels[name] = p.here()
	return p.here()
}

// branch emits `b<condition> name`, and call `bl name`. The name may be
// defined before or after.
func (p *lgtProgram) branch(condition uint32, name string) {
	p.branches = append(p.branches, lgtProgramBranch{len(p.words), condition<<28 | 0x0a000000, name})
	p.emit(0)
}

func (p *lgtProgram) call(name string) {
	p.branches = append(p.branches, lgtProgramBranch{len(p.words), lgtAL<<28 | 0x0b000000, name})
	p.emit(0)
}

// literal emits `ldr rd, =value`. The value goes into the pool the next call
// to pool lays down.
func (p *lgtProgram) literal(rd, value uint32) {
	index := -1
	for position, existing := range p.literals {
		if existing == value {
			index = position
		}
	}
	if index < 0 {
		p.literals = append(p.literals, value)
		index = len(p.literals) - 1
	}
	p.loads = append(p.loads, lgtLoad{len(p.words), index})
	p.emit(0xe59f0000 | rd<<12)
}

// through calls the function whose address is kept in the word at an address,
// leaving r0 to r3 as the caller set them. It is how a module reaches a
// platform function it resolved: the pointer is a Thumb stub, so the call is
// the ARMv4T interworking pair rather than a `bl`.
func (p *lgtProgram) through(address uint32) {
	p.literal(lgtIP, address)
	p.emit(lgtLdr(lgtIP, lgtIP, 0), lgtMovLRPC, lgtBX(lgtIP))
}

// pool lays down the literals loaded since the last one. It belongs where
// nothing falls through: after a return.
func (p *lgtProgram) pool() {
	pool := p.here()
	for _, load := range p.loads {
		offset := pool + uint32(load.literal)*4 - (p.base + uint32(load.word)*4 + 8)
		if offset > 0xfff {
			p.fail("a literal is %d bytes from its load", offset)
		}
		p.words[load.word] |= offset
	}
	p.words = append(p.words, p.literals...)
	p.loads, p.literals = nil, nil
}

func (p *lgtProgram) finish() ([]byte, error) {
	p.pool()
	for _, branch := range p.branches {
		target, defined := p.labels[branch.name]
		if !defined {
			p.fail("%s is not defined", branch.name)
			continue
		}
		from := p.base + uint32(branch.word)*4 + 8
		p.words[branch.word] = branch.opcode | uint32(int32(int64(target)-int64(from))/4)&0xffffff
	}
	if p.err != nil {
		return nil, p.err
	}
	data := make([]byte, len(p.words)*4)
	for index, word := range p.words {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	return data, nil
}

// lgtData is a module's data section, addressed the way the module sees it.
type lgtData struct {
	bytes []byte
	next  uint32
}

func (d *lgtData) words(address uint32, values ...uint32) {
	for index, value := range values {
		binary.LittleEndian.PutUint32(d.bytes[address-lgtDataBase+uint32(index)*4:], value)
	}
}

// reserve answers the address of the next free bytes, word aligned, and makes
// the section long enough to hold them.
func (d *lgtData) reserve(size uint32) uint32 {
	address := d.next
	d.next += (size + 3) &^ 3
	if end := int(d.next - lgtDataBase); end > len(d.bytes) {
		d.bytes = append(d.bytes, make([]byte, end-len(d.bytes))...)
	}
	return address
}

// text places a NUL-terminated string and answers where.
func (d *lgtData) text(value string) uint32 {
	address := d.reserve(uint32(len(value)) + 1)
	copy(d.bytes[address-lgtDataBase:], value)
	return address
}

// lgtCletBase assembles what the checkpoint Clet does and the save Clet does
// too: resolve the import requests, register the entry points, take the
// screen, and run a frame loop off a timer that arms itself again. It answers
// the module's entry and the six addresses of the Clet table, with the event
// handler left for the caller to define under the name "event".
func lgtCletBase(p *lgtProgram) (entry uint32, initialise uint32, table func() []uint32) {
	global := func(index int) { p.through(lgtGlobals + uint32(index)*4) }
	increment := func(address uint32) {
		p.literal(4, address)
		p.emit(lgtLdr(0, 4, 0), lgtAddImm(0, 0, 1), lgtStr(0, 4, 0))
	}

	// entry(param1, param2): resolve every platform function, then hand the
	// platform the init struct. A module is entered as Thumb, so the entry is a
	// `bx pc` that lands in the ARM body at the next word.
	entry = p.here()
	p.emit(0x46c04778)
	p.emit(lgtPushLR)
	p.emit(lgtMovReg(4, 0))
	p.emit(lgtLdr(5, 1, 4)) // param2->get_import_function
	p.literal(6, lgtImportRequests)
	p.literal(7, lgtGlobals)
	p.label("resolve")
	p.emit(lgtLdrPost(0, 6, 4), lgtCmpImm(0, 0))
	p.branch(lgtEQ, "resolved")
	p.emit(lgtLdrPost(1, 6, 4), lgtMovLRPC, lgtBX(5))
	p.emit(lgtStrPost(0, 7, 4))
	p.branch(lgtAL, "resolve")
	p.label("resolved")
	p.literal(0, lgtInitStruct)
	p.emit(lgtStr(0, 4, 512+20))
	p.emit(lgtMovImm(0, 0), lgtPopPC)
	p.pool()

	// init(): register the Clet's entry points.
	initialise = p.here()
	p.emit(lgtPushLR)
	p.literal(0, lgtCletFunctions)
	global(lgtGlobalRegister)
	p.emit(lgtPopPC)
	p.pool()

	// frame(timer, param): count, draw the count, present it, arm again.
	frame := p.here()
	p.emit(lgtPushLR)
	p.emit(lgtMovReg(5, 0))
	increment(LGTCheckpointFrameCounter)
	p.literal(6, lgtFrameBuffer)
	p.emit(lgtLdr(6, 6, 0), lgtStrh(0, 6))
	p.emit(lgtMovImm(0, 0), lgtMovImm(1, 0))
	global(lgtGlobalFlush)
	p.emit(lgtMovReg(0, 5), lgtMovImm(1, 20), lgtMovImm(2, 0), lgtMovImm(3, 0))
	global(lgtGlobalSetTimer)
	p.emit(lgtMovImm(0, 0), lgtPopPC)
	p.pool()

	// startClet(): count the start, take the screen, define and arm the timer.
	start := p.here()
	p.emit(lgtPushLR)
	increment(LGTCheckpointStartupCounter)
	p.emit(lgtMovImm(0, 0))
	global(lgtGlobalGetScreen)
	p.literal(5, lgtScreenHandle)
	p.emit(lgtStr(0, 5, 0))
	global(lgtGlobalGetPointer)
	p.literal(5, lgtFrameBuffer)
	p.emit(lgtStr(0, 5, 0))
	p.literal(0, lgtTimer)
	p.literal(1, frame)
	global(lgtGlobalDefTimer)
	p.literal(0, lgtTimer)
	p.emit(lgtMovImm(1, 20), lgtMovImm(2, 0), lgtMovImm(3, 0))
	global(lgtGlobalSetTimer)
	p.literal(4, LGTCheckpointLifecycle)
	p.emit(lgtMovImm(0, 2), lgtStr(0, 4, 0))
	p.emit(lgtMovImm(0, 0), lgtPopPC)
	p.pool()

	pause := p.here()
	p.literal(3, LGTCheckpointLifecycle)
	p.emit(lgtMovImm(0, 3), lgtStr(0, 3, 0), lgtMovImm(0, 0), lgtBXLR)
	resume := p.here()
	p.literal(3, LGTCheckpointLifecycle)
	p.emit(lgtMovImm(0, 2), lgtStr(0, 3, 0), lgtMovImm(0, 0), lgtBXLR)
	p.pool()

	// start, pause, resume, destroy, paint, handleEvent.
	return entry, initialise, func() []uint32 { return []uint32{start, pause, resume, 0, 0, p.labels["event"]} }
}

// LGTSaveArchive is the checkpoint Clet with a save: an app_info/JAR/module
// archive that starts through the ordinary loader, runs the same frame loop,
// and acts on the keys above through the WIPI C file calls.
//
// What each action is to the platform:
//
//	save      MC_fsOpen(truncate), MC_fsWrite of eight bytes, MC_fsClose
//	patch     MC_fsOpen(read-write), MC_fsWrite of four bytes at offset zero, MC_fsClose
//	read      MC_fsOpen(read-only), MC_fsSeek to the end for the length, MC_fsRead, MC_fsClose
//	delete    MC_fsRemove
//	hold      MC_fsOpen(read-write), MC_fsWrite of part A, and no close
//	truncate  MC_fsOpen(truncate), and no close
//	finish    MC_fsWrite of part B through the handle that was kept, MC_fsClose
//
// **A write this platform is handed reaches a buffer, not the store.** The
// bytes are kept on the open file — the platform client's table of open
// handles, `files`, in the entry's `data` with `dirty` set — and are stored
// when the handle is closed or the session ends. So after hold the store still
// has the save as it was and part A is pending in that table under the handle
// LGTSaveHeld names; after truncate the handle is empty and clean and nothing
// is pending at all. An open with write intent creates a file that is not
// there, at the open, so either of them on an absent save leaves an empty one
// stored.
//
// A second hold or truncate while a handle is kept does nothing, and so does a
// finish with none: there is one handle to keep.
func LGTSaveArchive() ([]byte, error) {
	p := newLGTProgram(lgtTextBase)
	entry, initialise, table := lgtCletBase(p)
	global := func(index int) { p.through(lgtGlobals + uint32(index)*4) }
	// open leaves the handle, or the refusal, in r0 and in r5.
	open := func(flag uint32) {
		p.literal(0, lgtSaveName)
		p.emit(lgtMovImm(1, flag), lgtMovImm(2, 0))
		global(lgtGlobalFsOpen)
		p.emit(lgtMovReg(5, 0))
	}
	// write hands the platform some of the scratch words through r5.
	write := func(offset, length uint32) {
		p.literal(1, lgtSaveScratch+offset)
		p.emit(lgtMovReg(0, 5), lgtMovImm(2, length))
		global(lgtGlobalFsWrite)
	}
	closeFile := func() {
		p.emit(lgtMovReg(0, 5))
		global(lgtGlobalFsClose)
	}

	// handleCletEvent(kind, key, other): remember what arrived, and act on a
	// key press.
	p.label("event")
	p.emit(lgtPushLR)
	p.literal(3, LGTCheckpointLastEvent)
	p.emit(lgtStr(0, 3, 0), lgtStr(1, 3, 4))
	p.literal(3, lgtEventKeyPressed)
	p.emit(lgtCmpReg(0, 3))
	p.branch(lgtNE, "done")
	for _, action := range []struct {
		key  int32
		name string
	}{
		{LGTSaveKeySave, "save"}, {LGTSaveKeyPatch, "patch"}, {LGTSaveKeyRead, "read"},
		{LGTSaveKeyDelete, "delete"}, {LGTSaveKeyHold, "hold"}, {LGTSaveKeyTruncate, "truncate"},
		{LGTSaveKeyFinish, "finish"},
	} {
		p.emit(lgtCmpImm(1, uint32(action.key)))
		p.branch(lgtEQ, action.name)
	}
	p.label("done")
	p.emit(lgtMovImm(0, 0), lgtPopPC)
	p.pool()

	// parts(): the scratch words become A and B.
	p.label("parts")
	p.literal(3, LGTSaveProgress)
	p.emit(lgtLdr(0, 3, 0))
	p.literal(2, LGTSaveXOR)
	p.emit(lgtEorReg(1, 0, 2))
	p.literal(3, lgtSaveScratch)
	p.emit(lgtStr(0, 3, 0), lgtStr(1, 3, 4), lgtBXLR)
	p.pool()

	p.label("save")
	open(lgtFileTruncate)
	p.emit(lgtCmpImm(5, 0))
	p.branch(lgtLT, "done")
	p.call("parts")
	write(0, 8)
	closeFile()
	p.branch(lgtAL, "done")

	p.label("patch")
	open(lgtFileReadWrite)
	p.emit(lgtCmpImm(5, 0))
	p.branch(lgtLT, "done")
	p.call("parts")
	write(0, 4)
	closeFile()
	p.branch(lgtAL, "done")
	p.pool()

	// The four words a read answers in are one run: seen A, seen B, status,
	// length. All four are cleared first, so a save that is not there leaves
	// them saying so, and the status says "error" from the open until the
	// bytes are in.
	p.label("read")
	p.literal(6, LGTSaveSeenA)
	p.emit(lgtMovImm(0, 0), lgtStr(0, 6, 0), lgtStr(0, 6, 4), lgtStr(0, 6, 8), lgtStr(0, 6, 12))
	open(lgtFileReadOnly)
	p.emit(lgtCmnImm(5, lgtNoEntry))
	p.branch(lgtEQ, "done")
	p.emit(lgtMovImm(0, LGTSaveStatusError), lgtStr(0, 6, 8))
	p.emit(lgtCmpImm(5, 0))
	p.branch(lgtLT, "done")
	p.emit(lgtMovReg(0, 5), lgtMovImm(1, 0), lgtMovImm(2, 2))
	global(lgtGlobalFsSeek)
	p.emit(lgtCmpImm(0, 0))
	p.branch(lgtLT, "read closes")
	p.emit(lgtStr(0, 6, 12))
	p.emit(lgtMovReg(0, 5), lgtMovImm(1, 0), lgtMovImm(2, 0))
	global(lgtGlobalFsSeek)
	p.emit(lgtMovReg(0, 5), lgtMovReg(1, 6), lgtMovImm(2, 8))
	global(lgtGlobalFsRead)
	p.emit(lgtCmpImm(0, 0))
	p.branch(lgtLT, "read closes")
	p.emit(lgtMovImm(0, LGTSaveStatusFound), lgtStr(0, 6, 8))
	p.label("read closes")
	closeFile()
	p.branch(lgtAL, "done")
	p.pool()

	p.label("delete")
	p.literal(0, lgtSaveName)
	p.emit(lgtMovImm(1, 0))
	global(lgtGlobalFsRemove)
	p.branch(lgtAL, "done")

	p.label("hold")
	p.literal(6, LGTSaveHeld)
	p.emit(lgtLdr(0, 6, 0), lgtCmpImm(0, 0))
	p.branch(lgtNE, "done")
	open(lgtFileReadWrite)
	p.emit(lgtCmpImm(5, 0))
	p.branch(lgtLT, "done")
	p.call("parts")
	write(0, 4)
	p.emit(lgtStr(5, 6, 0))
	p.branch(lgtAL, "done")
	p.pool()

	p.label("truncate")
	p.literal(6, LGTSaveHeld)
	p.emit(lgtLdr(0, 6, 0), lgtCmpImm(0, 0))
	p.branch(lgtNE, "done")
	open(lgtFileTruncate)
	p.emit(lgtCmpImm(5, 0))
	p.branch(lgtLT, "done")
	p.emit(lgtStr(5, 6, 0))
	p.branch(lgtAL, "done")

	p.label("finish")
	p.literal(6, LGTSaveHeld)
	p.emit(lgtLdr(5, 6, 0), lgtCmpImm(5, 0))
	p.branch(lgtEQ, "done")
	p.call("parts")
	write(4, 4)
	closeFile()
	p.emit(lgtMovImm(0, 0), lgtStr(0, 6, 0))
	p.branch(lgtAL, "done")

	code, err := p.finish()
	if err != nil {
		return nil, err
	}
	if lgtTextBase+uint32(len(code)) > lgtDataBase {
		return nil, fmt.Errorf("LGT fixture module: %d bytes of code reach the data section", len(code))
	}

	data := &lgtData{bytes: make([]byte, 0x400)}
	data.words(lgtCletFunctions, table()...)
	data.words(lgtInitStruct, 0, initialise, 0)
	data.words(lgtImportRequests,
		lgtImportTableWIPIC, lgtSlotCletRegister,
		lgtImportTableWIPIC, lgtSlotGetScreen,
		lgtImportTableWIPIC, lgtSlotFramebufferPointer,
		lgtImportTableWIPIC, lgtSlotFlushLcd,
		lgtImportTableWIPIC, lgtSlotDefTimer,
		lgtImportTableWIPIC, lgtSlotSetTimer,
		lgtImportTableWIPIC, lgtSlotFsOpen,
		lgtImportTableWIPIC, lgtSlotFsRead,
		lgtImportTableWIPIC, lgtSlotFsWrite,
		lgtImportTableWIPIC, lgtSlotFsClose,
		lgtImportTableWIPIC, lgtSlotFsSeek,
		lgtImportTableWIPIC, lgtSlotFsRemove,
		0, 0)
	copy(data.bytes[lgtSaveName-lgtDataBase:], LGTSaveFileName)

	jar, err := fixtureZIP([]fixtureEntry{{"binary.mod", lgtELF(code, data.bytes, entry)}, {"data/packaged.txt", []byte("packaged")}})
	if err != nil {
		return nil, err
	}
	return fixtureZIP([]fixtureEntry{
		{"app_info", []byte("AID=0102SAVE\nPID=" + LGTSaveOwner + "\nMClass=Save\nName=Save Fixture\n")},
		{"0102SAVE.jar", jar},
	})
}

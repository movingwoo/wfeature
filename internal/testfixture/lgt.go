package testfixture

import "encoding/binary"

// The LGT checkpoint fixture is a Clet authored here in ARM instructions. It
// does what a title of that kind does at its smallest: resolves its platform
// functions through the import table, registers its entry points, takes the
// screen, and then runs a frame loop off a timer that arms itself again at the
// end of every frame. Each frame counts and draws the count, so two sessions
// that are not the same session disagree about the picture within a tick.
const (
	lgtTextBase uint32 = 0x1000
	lgtDataBase uint32 = 0x3000

	lgtGlobals        = lgtDataBase
	lgtCletFunctions  = lgtDataBase + 0x40
	lgtInitStruct     = lgtDataBase + 0x60
	lgtImportRequests = lgtDataBase + 0x80
	lgtTimer          = lgtDataBase + 0x120

	// LGTCheckpointStartupCounter is incremented by the fixture's startClet.
	// Restoring a checkpoint must recover this word without running it again.
	LGTCheckpointStartupCounter = lgtDataBase + 0x100
	// LGTCheckpointFrameCounter counts the timer's frames, and is what the
	// first pixel of the screen shows.
	LGTCheckpointFrameCounter = lgtDataBase + 0x104
	// LGTCheckpointLastEvent and LGTCheckpointLastKey are the kind and the key
	// of the last event the Clet was handed.
	LGTCheckpointLastEvent = lgtDataBase + 0x108
	LGTCheckpointLastKey   = lgtDataBase + 0x10c
	// LGTCheckpointLifecycle is 2 while the Clet believes it is running and 3
	// once it has been told its player went away.
	LGTCheckpointLifecycle = lgtDataBase + 0x110

	lgtScreenHandle = lgtDataBase + 0x114
	lgtFrameBuffer  = lgtDataBase + 0x118

	// The fixture's own save owner, which a Host test places its saves under.
	LGTCheckpointSaveOwner = "PF0000CK"
)

// The platform's own numbers, as the import table names them. They are spelt
// out rather than imported because the platform package's tests use this
// fixture too.
const (
	lgtImportTableWIPIC uint32 = 0x1fb

	lgtSlotCletRegister       uint32 = 0x03
	lgtSlotFramebufferPointer uint32 = 0x32
	lgtSlotDefTimer           uint32 = 0x7a
	lgtSlotSetTimer           uint32 = 0x7b
	lgtSlotGetScreen          uint32 = 0xca
	lgtSlotFlushLcd           uint32 = 0xde
)

const (
	lgtGlobalRegister = iota
	lgtGlobalGetScreen
	lgtGlobalGetPointer
	lgtGlobalFlush
	lgtGlobalDefTimer
	lgtGlobalSetTimer
)

// lgtAssembler lays out ARM words and a literal pool.
type lgtAssembler struct {
	base     uint32
	words    []uint32
	literals []uint32
	loads    []lgtLoad
}

type lgtLoad struct{ word, literal int }

const (
	lgtPushLR  = 0xe92d40f0 // push {r4-r7, lr}
	lgtPopPC   = 0xe8bd80f0 // pop  {r4-r7, pc}
	lgtMovLRPC = 0xe1a0e00f // mov  lr, pc
	lgtBXLR    = 0xe12fff1e // bx   lr
)

func lgtMovImm(rd, value uint32) uint32     { return 0xe3a00000 | rd<<12 | value&0xff }
func lgtMovReg(rd, rm uint32) uint32        { return 0xe1a00000 | rd<<12 | rm }
func lgtLdr(rd, rn, offset uint32) uint32   { return 0xe5900000 | rn<<16 | rd<<12 | offset }
func lgtStr(rd, rn, offset uint32) uint32   { return 0xe5800000 | rn<<16 | rd<<12 | offset }
func lgtLdrPost(rd, rn, step uint32) uint32 { return 0xe4900000 | rn<<16 | rd<<12 | step }
func lgtStrPost(rd, rn, step uint32) uint32 { return 0xe4800000 | rn<<16 | rd<<12 | step }
func lgtStrh(rd, rn uint32) uint32          { return 0xe1c000b0 | rn<<16 | rd<<12 }
func lgtAddImm(rd, rn, value uint32) uint32 { return 0xe2800000 | rn<<16 | rd<<12 | value&0xff }
func lgtCmpImm(rn, value uint32) uint32     { return 0xe3500000 | rn<<16 | value&0xff }
func lgtBranch(condition, offset int32) uint32 {
	return uint32(condition)<<28 | 0x0a000000 | uint32(offset)&0xffffff
}

func (a *lgtAssembler) emit(words ...uint32) { a.words = append(a.words, words...) }
func (a *lgtAssembler) here() uint32         { return a.base + uint32(len(a.words))*4 }

// literal emits `ldr rd, =value`.
func (a *lgtAssembler) literal(rd, value uint32) {
	index := -1
	for position, existing := range a.literals {
		if existing == value {
			index = position
		}
	}
	if index < 0 {
		a.literals = append(a.literals, value)
		index = len(a.literals) - 1
	}
	a.loads = append(a.loads, lgtLoad{len(a.words), index})
	a.words = append(a.words, 0xe59f0000|rd<<12)
}

// call emits the ARMv4T interworking call through a register.
func (a *lgtAssembler) call(rm uint32) { a.emit(lgtMovLRPC, 0xe12fff10|rm) }

// branch emits a branch to an address already laid out, or to one named later
// through the index it answers.
func (a *lgtAssembler) branch(condition int32, target uint32) int {
	index := len(a.words)
	a.emit(0)
	a.patch(index, condition, target)
	return index
}

func (a *lgtAssembler) patch(index int, condition int32, target uint32) {
	from := a.base + uint32(index)*4 + 8
	a.words[index] = lgtBranch(condition, int32(int64(target)-int64(from))/4)
}

func (a *lgtAssembler) finish() []byte {
	pool := a.base + uint32(len(a.words))*4
	for _, load := range a.loads {
		at := a.base + uint32(load.word)*4
		a.words[load.word] |= pool + uint32(load.literal)*4 - (at + 8)
	}
	all := append(append([]uint32(nil), a.words...), a.literals...)
	data := make([]byte, len(all)*4)
	for index, word := range all {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	return data
}

// LGTCheckpointArchive is a complete app_info/JAR/module archive that starts
// through the ordinary loader and runs through the ordinary tick.
func LGTCheckpointArchive() ([]byte, error) {
	const always, equal = 0xe, 0x0
	a := &lgtAssembler{base: lgtTextBase}
	global := func(rd uint32, index int) {
		a.literal(rd, lgtGlobals+uint32(index)*4)
		a.emit(lgtLdr(rd, rd, 0))
	}
	increment := func(address uint32) {
		a.literal(4, address)
		a.emit(lgtLdr(0, 4, 0), lgtAddImm(0, 0, 1), lgtStr(0, 4, 0))
	}
	store := func(address, value uint32) {
		a.literal(4, address)
		a.emit(lgtMovImm(0, value), lgtStr(0, 4, 0))
	}

	// entry(param1, param2): resolve every platform function, then hand the
	// platform the init struct. A module is entered as Thumb, so the entry is a
	// `bx pc` that lands in the ARM body at the next word.
	entry := a.here()
	a.emit(0x46c04778)
	a.emit(lgtPushLR)
	a.emit(lgtMovReg(4, 0))
	a.emit(lgtLdr(5, 1, 4)) // param2->get_import_function
	a.literal(6, lgtImportRequests)
	a.literal(7, lgtGlobals)
	loop := a.here()
	a.emit(lgtLdrPost(0, 6, 4), lgtCmpImm(0, 0))
	done := a.branch(equal, 0)
	a.emit(lgtLdrPost(1, 6, 4))
	a.call(5)
	a.emit(lgtStrPost(0, 7, 4))
	a.branch(always, loop)
	a.patch(done, equal, a.here())
	a.literal(0, lgtInitStruct)
	a.emit(lgtStr(0, 4, 512+20))
	a.emit(lgtMovImm(0, 0), lgtPopPC)

	// init(): register the Clet's entry points.
	initialise := a.here()
	a.emit(lgtPushLR)
	global(4, lgtGlobalRegister)
	a.literal(0, lgtCletFunctions)
	a.call(4)
	a.emit(lgtPopPC)

	// frame(timer, param): count, draw the count, present it, arm again.
	frame := a.here()
	a.emit(lgtPushLR)
	a.emit(lgtMovReg(5, 0))
	increment(LGTCheckpointFrameCounter)
	a.literal(6, lgtFrameBuffer)
	a.emit(lgtLdr(6, 6, 0), lgtStrh(0, 6))
	global(4, lgtGlobalFlush)
	a.emit(lgtMovImm(0, 0), lgtMovImm(1, 0))
	a.call(4)
	global(4, lgtGlobalSetTimer)
	a.emit(lgtMovReg(0, 5), lgtMovImm(1, 20), lgtMovImm(2, 0), lgtMovImm(3, 0))
	a.call(4)
	a.emit(lgtMovImm(0, 0), lgtPopPC)

	// startClet(): count the start, take the screen, define and arm the timer.
	start := a.here()
	a.emit(lgtPushLR)
	increment(LGTCheckpointStartupCounter)
	global(4, lgtGlobalGetScreen)
	a.emit(lgtMovImm(0, 0))
	a.call(4)
	a.literal(5, lgtScreenHandle)
	a.emit(lgtStr(0, 5, 0))
	global(4, lgtGlobalGetPointer)
	a.call(4)
	a.literal(5, lgtFrameBuffer)
	a.emit(lgtStr(0, 5, 0))
	global(4, lgtGlobalDefTimer)
	a.literal(0, lgtTimer)
	a.literal(1, frame)
	a.call(4)
	global(4, lgtGlobalSetTimer)
	a.literal(0, lgtTimer)
	a.emit(lgtMovImm(1, 20), lgtMovImm(2, 0), lgtMovImm(3, 0))
	a.call(4)
	store(LGTCheckpointLifecycle, 2)
	a.emit(lgtMovImm(0, 0), lgtPopPC)

	// handleCletEvent(kind, key, other): remember what arrived.
	event := a.here()
	a.literal(3, LGTCheckpointLastEvent)
	a.emit(lgtStr(0, 3, 0), lgtStr(1, 3, 4))
	a.emit(lgtMovImm(0, 0), lgtBXLR)

	pause := a.here()
	a.literal(3, LGTCheckpointLifecycle)
	a.emit(lgtMovImm(0, 3), lgtStr(0, 3, 0), lgtMovImm(0, 0), lgtBXLR)
	resume := a.here()
	a.literal(3, LGTCheckpointLifecycle)
	a.emit(lgtMovImm(0, 2), lgtStr(0, 3, 0), lgtMovImm(0, 0), lgtBXLR)
	code := a.finish()

	data := make([]byte, 0x400)
	put := func(address uint32, values ...uint32) {
		for index, value := range values {
			binary.LittleEndian.PutUint32(data[address-lgtDataBase+uint32(index)*4:], value)
		}
	}
	// start, pause, resume, destroy, paint, handleEvent.
	put(lgtCletFunctions, start, pause, resume, 0, 0, event)
	put(lgtInitStruct, 0, initialise, 0)
	put(lgtImportRequests,
		lgtImportTableWIPIC, lgtSlotCletRegister,
		lgtImportTableWIPIC, lgtSlotGetScreen,
		lgtImportTableWIPIC, lgtSlotFramebufferPointer,
		lgtImportTableWIPIC, lgtSlotFlushLcd,
		lgtImportTableWIPIC, lgtSlotDefTimer,
		lgtImportTableWIPIC, lgtSlotSetTimer,
		0, 0)

	jar, err := fixtureZIP([]fixtureEntry{{"binary.mod", lgtELF(code, data, entry)}, {"data/packaged.txt", []byte("packaged")}})
	if err != nil {
		return nil, err
	}
	return fixtureZIP([]fixtureEntry{
		{"app_info", []byte("AID=0102CKPT\nPID=" + LGTCheckpointSaveOwner + "\nMClass=Checkpoint\nName=Checkpoint Fixture\n")},
		{"0102CKPT.jar", jar},
	})
}

// lgtELF wraps the two sections in the ELF32 ARM executable the loader reads.
func lgtELF(code, data []byte, entry uint32) []byte {
	const (
		headerSize   = 52
		sectionSize  = 40
		sectionCount = 4 // null, .text, .data, .shstrtab
	)
	names := []byte("\x00.text\x00.data\x00.shstrtab\x00")
	codeOffset := uint32(headerSize)
	dataOffset := codeOffset + uint32(len(code))
	namesOffset := dataOffset + uint32(len(data))
	tableOffset := namesOffset + uint32(len(names))
	image := make([]byte, tableOffset+sectionCount*sectionSize)
	copy(image, "\x7fELF")
	image[4], image[5], image[6] = 1, 1, 1        // ELF32, little-endian, version 1
	binary.LittleEndian.PutUint16(image[16:], 2)  // ET_EXEC
	binary.LittleEndian.PutUint16(image[18:], 40) // EM_ARM
	binary.LittleEndian.PutUint32(image[20:], 1)
	binary.LittleEndian.PutUint32(image[24:], entry)
	binary.LittleEndian.PutUint32(image[32:], tableOffset)
	binary.LittleEndian.PutUint16(image[40:], headerSize)
	binary.LittleEndian.PutUint16(image[46:], sectionSize)
	binary.LittleEndian.PutUint16(image[48:], sectionCount)
	binary.LittleEndian.PutUint16(image[50:], 3)
	copy(image[codeOffset:], code)
	copy(image[dataOffset:], data)
	copy(image[namesOffset:], names)
	section := func(index int, name, kind, flags, address, offset, size uint32) {
		at := tableOffset + uint32(index)*sectionSize
		for field, value := range []uint32{name, kind, flags, address, offset, size} {
			binary.LittleEndian.PutUint32(image[at+uint32(field)*4:], value)
		}
	}
	const allocated, executable = 0x2, 0x4
	section(1, 1, 1, allocated|executable, lgtTextBase, codeOffset, uint32(len(code)))
	section(2, 7, 1, allocated, lgtDataBase, dataOffset, uint32(len(data)))
	section(3, 13, 3, 0, 0, namesOffset, uint32(len(names)))
	return image
}

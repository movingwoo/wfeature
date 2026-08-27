package lgt

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"
)

// The fixture is a newly authored ARM module, hand-assembled here rather than
// taken from any game. It does what a real Clet does at startup — resolve its
// platform functions through the import table, register its entry points, take
// the screen framebuffer and write pixels into it directly — which is exactly
// the path this platform exists to serve.

// Guest addresses. The module keeps whatever its ELF names, so these are the
// fixture's own choice.
const (
	fixtureTextBase uint32 = 0x1000
	fixtureDataBase uint32 = 0x3000

	fixtureGlobals        = fixtureDataBase        // resolved function pointers
	fixtureCletFunctions  = fixtureDataBase + 0x40 // the six Clet entry points
	fixtureInitStruct     = fixtureDataBase + 0x60 // { unk, init, name }
	fixtureImportRequests = fixtureDataBase + 0x80 // (table, index) pairs, zero-terminated
	fixtureLastEvent      = fixtureDataBase + 0xc0
	fixtureScreenHandle   = fixtureDataBase + 0xc4
	fixtureFrameBuffer    = fixtureDataBase + 0xc8
	// fixtureLifecycle holds what the Clet was last told about its own life:
	// 2 once it has started or resumed, 3 once it has been paused. The numbers
	// are the MIDP fixture's, so the two platforms' lifecycle tests read the
	// same way.
	fixtureLifecycle = fixtureDataBase + 0xcc
)

const (
	fixtureLifecycleRunning = 2
	fixtureLifecyclePaused  = 3
)

// The order the fixture resolves its imports in, which is also the order the
// resolved pointers land in fixtureGlobals.
const (
	globalCletRegister = 0
	globalGetScreen    = 1
	globalGetPointer   = 2
	globalFlush        = 3
)

// A32 encodings. Only the handful the fixture needs are here; each is written
// as the instruction it stands for so the program below reads as assembly.
func armMovImm(rd, value uint32) uint32    { return 0xe3a00000 | rd<<12 | value&0xff }
func armMovReg(rd, rm uint32) uint32       { return 0xe1a00000 | rd<<12 | rm }
func armLdrPC(rd, offset uint32) uint32    { return 0xe59f0000 | rd<<12 | offset }
func armLdr(rd, rn, off uint32) uint32     { return 0xe5900000 | rn<<16 | rd<<12 | off }
func armStr(rd, rn, off uint32) uint32     { return 0xe5800000 | rn<<16 | rd<<12 | off }
func armLdrPost(rd, rn, off uint32) uint32 { return 0xe4900000 | rn<<16 | rd<<12 | off }
func armStrPost(rd, rn, off uint32) uint32 { return 0xe4800000 | rn<<16 | rd<<12 | off }
func armStrh(rd, rn uint32) uint32         { return 0xe1c000b0 | rn<<16 | rd<<12 }
func armCmpImm(rn, value uint32) uint32    { return 0xe3500000 | rn<<16 | value&0xff }
func armBX(rm uint32) uint32               { return 0xe12fff10 | rm }
func armBranch(offset int32) uint32        { return 0xea000000 | uint32(offset)&0xffffff }
func armBranchEq(offset int32) uint32      { return 0x0a000000 | uint32(offset)&0xffffff }

const (
	armPushLR  = 0xe92d40f0 // push {r4-r7, lr}
	armPopPC   = 0xe8bd80f0 // pop  {r4-r7, pc}
	armMovLRPC = 0xe1a0e00f // mov  lr, pc — the ARMv4T call sequence, since
	// this core is ARMv4T and has no BLX register.

	// One word holding two Thumb halfwords: `bx pc` then `mov r8, r8`. It is
	// how Thumb code reaches ARM code at the next word; see the entry point.
	thumbBXPCTrampoline = 0x46c04778
)

// assembler lays out ARM words and a literal pool, resolving pc-relative
// loads for the caller.
type assembler struct {
	base     uint32
	words    []uint32
	literals []uint32
}

func (a *assembler) emit(words ...uint32) { a.words = append(a.words, words...) }

func (a *assembler) here() uint32 { return a.base + uint32(len(a.words))*4 }

// literal emits `ldr rd, =value`, adding the constant to the pool.
func (a *assembler) literal(rd, value uint32) {
	index := -1
	for position, existing := range a.literals {
		if existing == value {
			index = position
			break
		}
	}
	if index < 0 {
		a.literals = append(a.literals, value)
		index = len(a.literals) - 1
	}
	// The offset is patched once the pool's address is known; the placeholder
	// records which literal this load wants.
	a.words = append(a.words, armLdrPC(rd, 0)|literalMark|uint32(index))
}

// literalMark tags a placeholder load. It sits in the immediate field, which
// a real pc-relative load never fills with this pattern because the pool is
// always within a few hundred bytes.
const literalMark = 0x800

// call emits the ARMv4T interworking call: set lr to the instruction after
// the branch, then bx.
func (a *assembler) call(rm uint32) { a.emit(armMovLRPC, armBX(rm)) }

// finish appends the literal pool and patches every placeholder load.
func (a *assembler) finish() []byte {
	poolAddress := a.base + uint32(len(a.words))*4
	for index, word := range a.words {
		if word&0xfff < literalMark {
			continue
		}
		if word&0xffff0000 != armLdrPC(word>>12&0xf, 0)&0xffff0000 {
			continue
		}
		literal := word & 0x7ff
		instructionAddress := a.base + uint32(index)*4
		// A pc-relative load reads from pc+8 plus the offset.
		offset := poolAddress + literal*4 - (instructionAddress + 8)
		a.words[index] = armLdrPC(word>>12&0xf, offset)
	}
	all := append(append([]uint32(nil), a.words...), a.literals...)
	data := make([]byte, len(all)*4)
	for index, word := range all {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	return data
}

// fixtureModule assembles the module's code section and reports the addresses
// of the entry points the data section names.
func fixtureModule() (code []byte, entry, initFunction, startClet, handleEvent, pauseClet, resumeClet uint32) {
	a := &assembler{base: fixtureTextBase}

	// entry(param1, param2): resolve every platform function the module uses,
	// then hand the platform its init struct.
	//
	// A real LGT module is Thumb and is entered as Thumb, so the entry point
	// is reached in that state. This fixture's body is ARM, so it opens with
	// the trampoline a mixed module uses: `bx pc` from a word-aligned Thumb
	// address lands in ARM at the next word, and the halfword after it is
	// padding to get there. LR is untouched, so the ARM body still returns to
	// the platform the way it was called.
	entry = a.here()
	a.emit(thumbBXPCTrampoline)
	a.emit(armPushLR)
	a.emit(armMovReg(4, 0)) // r4 = param1
	a.emit(armLdr(5, 1, 4)) // r5 = param2->fn_get_import_function
	a.literal(6, fixtureImportRequests)
	a.literal(7, fixtureGlobals)
	loop := a.here()
	a.emit(armLdrPost(0, 6, 4)) // r0 = table, advance
	a.emit(armCmpImm(0, 0))
	forwardPatch := len(a.words)
	a.emit(armBranchEq(0))      // patched to `done` below
	a.emit(armLdrPost(1, 6, 4)) // r1 = index, advance
	a.call(5)
	a.emit(armStrPost(0, 7, 4)) // store the resolved pointer
	a.emit(armBranch(int32(int64(loop)-int64(a.here()+8)) / 4))
	done := a.here()
	a.words[forwardPatch] = armBranchEq(int32(int64(done)-int64(a.base+uint32(forwardPatch)*4+8)) / 4)
	a.literal(0, fixtureInitStruct)
	a.emit(armStr(0, 4, 512+20)) // param1->ptr_init_struct
	a.emit(armMovImm(0, 0))
	a.emit(armPopPC)

	// init(): register the Clet entry points.
	initFunction = a.here()
	a.emit(armPushLR)
	a.literal(4, fixtureGlobals+globalCletRegister*4)
	a.emit(armLdr(4, 4, 0))
	a.literal(0, fixtureCletFunctions)
	a.call(4)
	a.emit(armPopPC)

	// startClet(): take the screen framebuffer, write one red pixel into it
	// directly — which is how LGT games actually draw — and flush.
	startClet = a.here()
	a.emit(armPushLR)
	a.literal(4, fixtureGlobals+globalGetScreen*4)
	a.emit(armLdr(4, 4, 0))
	a.emit(armMovImm(0, 0))
	a.call(4)
	a.literal(5, fixtureScreenHandle)
	a.emit(armStr(0, 5, 0))
	a.literal(4, fixtureGlobals+globalGetPointer*4)
	a.emit(armLdr(4, 4, 0))
	a.call(4)
	a.emit(armMovReg(6, 0))
	a.literal(5, fixtureFrameBuffer)
	a.emit(armStr(6, 5, 0))
	a.literal(0, 0xf800) // red in RGB565
	a.emit(armStrh(0, 6))
	a.literal(4, fixtureGlobals+globalFlush*4)
	a.emit(armLdr(4, 4, 0))
	a.emit(armMovImm(0, 0))
	a.call(4)
	a.literal(4, fixtureLifecycle)
	a.emit(armMovImm(0, fixtureLifecycleRunning))
	a.emit(armStr(0, 4, 0))
	a.emit(armMovImm(0, 0))
	a.emit(armPopPC)

	// handleCletEvent(kind, param1, param2): record the key and paint it into
	// the second pixel, so a test can see the event arrived.
	handleEvent = a.here()
	a.emit(armPushLR)
	a.literal(4, fixtureLastEvent)
	a.emit(armStr(1, 4, 0))
	a.literal(5, fixtureFrameBuffer)
	a.emit(armLdr(6, 5, 0))
	a.emit(armStrh(1, 6)) // the key code lands where the test can read it
	a.emit(armMovImm(0, 0))
	a.emit(armPopPC)

	// pauseClet() and resumeClet(): the lifecycle a Host drives when the page
	// watching the game goes away and comes back. Each writes what it was told
	// into one word, which is the whole of what a test needs to see: the
	// platform either called the entry point in the Clet's table or it did
	// not.
	pauseClet = a.here()
	a.emit(armPushLR)
	a.literal(4, fixtureLifecycle)
	a.emit(armMovImm(0, fixtureLifecyclePaused))
	a.emit(armStr(0, 4, 0))
	a.emit(armMovImm(0, 0))
	a.emit(armPopPC)

	resumeClet = a.here()
	a.emit(armPushLR)
	a.literal(4, fixtureLifecycle)
	a.emit(armMovImm(0, fixtureLifecycleRunning))
	a.emit(armStr(0, 4, 0))
	a.emit(armMovImm(0, 0))
	a.emit(armPopPC)

	return a.finish(), entry, initFunction, startClet, handleEvent, pauseClet, resumeClet
}

// fixtureData builds the module's data section: the Clet table, the init
// struct, and the import requests the entry point walks.
func fixtureData(initFunction, startClet, handleEvent, pauseClet, resumeClet uint32) []byte {
	// The section is larger than the module itself needs, because the Java
	// fixtures plant class records and member tables further into it and a scan
	// for records walks the sections a module declares. A record that lands
	// past the end of every section is in mapped memory and in no section,
	// which is not a place a real module puts one.
	data := make([]byte, 0xc00)
	put := func(address, value uint32) {
		binary.LittleEndian.PutUint32(data[address-fixtureDataBase:], value)
	}
	// The Clet table: start, pause, resume, destroy, paint, handleEvent. The
	// fixture leaves destroy and paint at zero, which the platform treats as
	// "not provided" — and half the local titles leave the lifecycle entries
	// that way too, so both cases are real.
	put(fixtureCletFunctions+0, startClet)
	put(fixtureCletFunctions+4, pauseClet)
	put(fixtureCletFunctions+8, resumeClet)
	put(fixtureCletFunctions+20, handleEvent)
	// InitStruct is { unk, fn_init, name }.
	put(fixtureInitStruct+4, initFunction)
	// (table, index) pairs, zero-terminated.
	requests := []uint32{
		importTableWIPIC, slotCletRegister,
		importTableWIPIC, slotGetScreenFramebuffer,
		importTableWIPIC, slotFramebufferPointer,
		importTableWIPIC, slotFlushLcd,
		0, 0,
	}
	for index, value := range requests {
		put(fixtureImportRequests+uint32(index)*4, value)
	}
	return data
}

// fixtureELF wraps the code and data sections in the ELF32 ARM executable the
// loader reads.
func fixtureELF(code, data []byte, entry uint32) []byte {
	const (
		headerSize  = 52
		entrySize   = 40
		sectionsNum = 4 // null, .text, .data, .shstrtab
	)
	names := []byte("\x00.text\x00.data\x00.shstrtab\x00")
	codeOffset := uint32(headerSize)
	dataOffset := codeOffset + uint32(len(code))
	namesOffset := dataOffset + uint32(len(data))
	sectionOffset := namesOffset + uint32(len(names))

	image := make([]byte, sectionOffset+sectionsNum*entrySize)
	copy(image, "\x7fELF")
	image[4], image[5], image[6] = 1, 1, 1        // ELF32, little-endian, version 1
	binary.LittleEndian.PutUint16(image[16:], 2)  // ET_EXEC
	binary.LittleEndian.PutUint16(image[18:], 40) // EM_ARM
	binary.LittleEndian.PutUint32(image[20:], 1)
	binary.LittleEndian.PutUint32(image[24:], entry)
	binary.LittleEndian.PutUint32(image[32:], sectionOffset)
	binary.LittleEndian.PutUint16(image[40:], headerSize)
	binary.LittleEndian.PutUint16(image[46:], entrySize)
	binary.LittleEndian.PutUint16(image[48:], sectionsNum)
	binary.LittleEndian.PutUint16(image[50:], 3) // .shstrtab index

	copy(image[codeOffset:], code)
	copy(image[dataOffset:], data)
	copy(image[namesOffset:], names)

	section := func(index int, nameOffset, kind, flags, address, offset, size uint32) {
		base := sectionOffset + uint32(index)*entrySize
		binary.LittleEndian.PutUint32(image[base:], nameOffset)
		binary.LittleEndian.PutUint32(image[base+4:], kind)
		binary.LittleEndian.PutUint32(image[base+8:], flags)
		binary.LittleEndian.PutUint32(image[base+12:], address)
		binary.LittleEndian.PutUint32(image[base+16:], offset)
		binary.LittleEndian.PutUint32(image[base+20:], size)
	}
	section(0, 0, 0, 0, 0, 0, 0)
	section(1, 1, 1, sectionFlagAlloc|sectionFlagExec, fixtureTextBase, codeOffset, uint32(len(code)))
	section(2, 7, 1, sectionFlagAlloc, fixtureDataBase, dataOffset, uint32(len(data)))
	section(3, 13, 3, 0, 0, namesOffset, uint32(len(names)))
	return image
}

// fixtureArchive packages the module the way a handset ships one: app_info
// beside a JAR that contains binary.mod.
func fixtureArchive(t testing.TB) []byte {
	t.Helper()
	return zipOf(t, map[string][]byte{
		// Name is what MC_knlGetProgramName answers with, so the fixture
		// carries one rather than leaving that slot with nothing to say.
		"app_info":     []byte("AID=0102ABCD\nPID=PF000001\nMClass=Fixture\nName=Fixture Title\n"),
		"0102ABCD.jar": fixtureJAR(t),
	})
}

// fixtureJAR is the application half of the archive on its own, so a test can
// package it beside files of its own choosing.
func fixtureJAR(t testing.TB) []byte {
	t.Helper()
	code, entry, initFunction, startClet, handleEvent, pauseClet, resumeClet := fixtureModule()
	module := fixtureELF(code, fixtureData(initFunction, startClet, handleEvent, pauseClet, resumeClet), entry)
	return zipOf(t, map[string][]byte{
		binaryModuleName: module,
		"data/hello.txt": []byte("packaged"),
		// A second resource, so a test can check that two names are told
		// apart rather than only that one name answers itself.
		"data/other.txt": []byte("beside it"),
	})
}

func zipOf(t testing.TB, entries map[string][]byte) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	for name, content := range entries {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", name, err)
		}
		if _, err := file.Write(content); err != nil {
			t.Fatalf("write zip entry %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

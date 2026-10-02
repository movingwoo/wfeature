package testfixture

import (
	"encoding/binary"
	"fmt"
	"slices"
	"unicode/utf16"
)

// The KTF storage fixtures are two archives authored here that start through
// the ordinary loader and do with one save what a title does with its own:
// write it whole, patch part of it, read it back, delete it, and hold it open
// across key presses. One is a descriptor/JAR/AOT archive of the current
// generation and the other an older descriptor module. Both run the same ARM
// routines and differ only in how they are loaded and in which platform table
// they resolve their classes through.
//
// **The save is an org.kwis.msp.io.File.** Of the four storage surfaces this
// runtime has, it is the only one on which every action below means what it
// says: a File opened for truncation stores nothing until its first write,
// where the WIPI C file table stores the empty file at the open and the two
// record tables have no truncating open at all. It is also the surface with a
// copy of its own to go stale, because each File object keeps a private buffer
// and stores the whole of it on every write.
//
// A key press is one action, and the action is finished when the key call
// returns: this runtime hands a key to the card that is showing, and every
// storage call the card makes is stored before it answers.
const (
	// KTFSaveName is what the guest calls its save, KTFSaveStoreKey the key the
	// platform stores it under, and KTFSaveRemovalKey the list a delete is
	// written on. The store has no delete, so a deleted save keeps its bytes
	// under KTFSaveStoreKey and is gone because its name is on that list, one
	// name to a line.
	KTFSaveName       = "progress.sav"
	KTFSaveStoreKey   = "fs/" + KTFSaveName
	KTFSaveRemovalKey = "fs/.removed"

	// KTFSaveMask is what part B is part A exclusive-or'd with. The save is
	// part A, the progress word, followed by part B, both little-endian.
	KTFSaveMask uint32 = 0x5aa51c3e
)

// What the status word reads after KTFSaveActionRead. KTFSaveError is what a
// count the platform should not have answered is filed under; a failure this
// platform reports to a Java title is an exception, which the fixture does not
// catch, so it ends the key call with an error rather than with a status.
const (
	KTFSaveMissing uint32 = 0
	KTFSaveFound   uint32 = 1
	KTFSaveError   uint32 = 2
)

// The actions, as the key codes a Host sends for them. They are the digits, so
// the shared session passes them through unchanged.
const (
	// KTFSaveActionSave writes both parts from the progress word through a File
	// opened for truncation, creating the save if it is not there.
	KTFSaveActionSave int32 = '1'
	// KTFSaveActionPatch writes part A alone at the start of a File opened for
	// writing, which keeps what follows it.
	KTFSaveActionPatch int32 = '2'
	// KTFSaveActionRead reads both parts into the seen words and sets the
	// status and the length, which is how many bytes it was given.
	KTFSaveActionRead int32 = '3'
	// KTFSaveActionDelete removes the save by name.
	KTFSaveActionDelete int32 = '4'
	// KTFSaveActionHold opens the save for writing, writes part A and keeps the
	// File. Nothing is left pending by that: the write is stored before the
	// call returns. What the Host goes on holding is the File's own copy of the
	// save and its cursor, in the object's native payload, and the copy the
	// session-written file table keeps under the name.
	KTFSaveActionHold int32 = '5'
	// KTFSaveActionTruncate opens the save for truncation and keeps the File
	// without writing. The stored save is untouched until KTFSaveActionFinish.
	KTFSaveActionTruncate int32 = '6'
	// KTFSaveActionFinish writes part B through the kept File and closes it.
	// After a hold that completes the save; after a truncation the save is
	// part B alone, because that File's own copy began empty.
	KTFSaveActionFinish int32 = '7'
)

// KTFSaveContent is what KTFSaveActionSave stores for a progress word.
func KTFSaveContent(progress uint32) []byte {
	content := make([]byte, 8)
	binary.LittleEndian.PutUint32(content, progress)
	binary.LittleEndian.PutUint32(content[4:], progress^KTFSaveMask)
	return content
}

// Both images are laid out the same way past their own headers, so the same
// routines address the same words in each.
const (
	ktfSaveData    = 0x100 // the fixture's own words
	ktfSaveRecords = 0x200 // its class record, descriptor, methods and constants
	ktfSaveText    = 0x400 // names
	ktfSaveRoutine = 0x800 // code

	ktfSaveClass      = ktfSaveRecords
	ktfSaveDescriptor = ktfSaveRecords + 0x20
	ktfSaveMethods    = ktfSaveRecords + 0x60
	ktfSaveMethodList = ktfSaveRecords + 0xe0
	ktfSaveString     = ktfSaveRecords + 0x100
	ktfSaveCharacters = ktfSaveRecords + 0x120

	ktfSaveMethodSize  = 28
	ktfSaveMethodCount = 4
)

// The words, by their offset into the data block. The first seven are what a
// test reads and writes; the rest are the routines' own.
const (
	ktfSaveWordProgress = iota * 4
	ktfSaveWordSeenA
	ktfSaveWordSeenB
	ktfSaveWordStatus
	ktfSaveWordLength
	ktfSaveWordActions
	ktfSaveWordKept
	ktfSaveWordBuffer
	ktfSaveWordName
	ktfSaveWordMask
	ktfSaveWordReady
	ktfSaveWordCallbacks
	ktfSaveWordInterface
	ktfSaveWordLoadClass
	ktfSaveWordFindMethod
	ktfSaveWordNewObject
	ktfSaveWordNewArray
	ktfSaveWordByteType
	ktfSaveWordRegisterString
	ktfSaveWordCardClass
	ktfSaveWordDisplayClass
	ktfSaveWordFileClass
	ktfSaveWordFileSystemClass
	ktfSaveWordCardInit
	ktfSaveWordGetDisplay
	ktfSaveWordPushCard
	ktfSaveWordFileInit
	ktfSaveWordFileWrite
	ktfSaveWordFileRead
	ktfSaveWordFileClose
	ktfSaveWordExists
	ktfSaveWordRemove
)

// Guest words of KTFSaveArchive.
const (
	// KTFSaveProgress is the word a test writes and the save is made from.
	KTFSaveProgress = ktfImageBase + ktfSaveData + ktfSaveWordProgress
	// KTFSaveSeenA and KTFSaveSeenB are the two parts the last read found, and
	// zero where it found none.
	KTFSaveSeenA = ktfImageBase + ktfSaveData + ktfSaveWordSeenA
	KTFSaveSeenB = ktfImageBase + ktfSaveData + ktfSaveWordSeenB
	// KTFSaveStatus and KTFSaveLength are what the last read answered.
	KTFSaveStatus = ktfImageBase + ktfSaveData + ktfSaveWordStatus
	KTFSaveLength = ktfImageBase + ktfSaveData + ktfSaveWordLength
	// KTFSaveActions counts the actions the guest has finished, so a test can
	// tell a key that did nothing from a key that never arrived.
	KTFSaveActions = ktfImageBase + ktfSaveData + ktfSaveWordActions
	// KTFSaveKept is the guest address of the File a hold or a truncation
	// kept, and zero when none is.
	KTFSaveKept = ktfImageBase + ktfSaveData + ktfSaveWordKept
)

// Guest words of KTFModuleSaveArchive, which mean what the ones above do.
const (
	KTFModuleSaveProgress = ktfSaveModuleSegment + ktfSaveData + ktfSaveWordProgress
	KTFModuleSaveSeenA    = ktfSaveModuleSegment + ktfSaveData + ktfSaveWordSeenA
	KTFModuleSaveSeenB    = ktfSaveModuleSegment + ktfSaveData + ktfSaveWordSeenB
	KTFModuleSaveStatus   = ktfSaveModuleSegment + ktfSaveData + ktfSaveWordStatus
	KTFModuleSaveLength   = ktfSaveModuleSegment + ktfSaveData + ktfSaveWordLength
	KTFModuleSaveActions  = ktfSaveModuleSegment + ktfSaveData + ktfSaveWordActions
	KTFModuleSaveKept     = ktfSaveModuleSegment + ktfSaveData + ktfSaveWordKept
)

// The older module's own tables, which sit where the current generation keeps
// its executable record, and the segment words its loader reads.
const (
	ktfSaveModuleDescriptor = 0x40
	ktfSaveModuleClasses    = 0x60
	ktfSaveModuleNames      = 0x80
	ktfSaveModuleJumps      = 0xa0

	ktfSaveModuleNameTable  = 0x10
	ktfSaveModuleJumpTable  = 0x14
	ktfSaveModuleStaticBase = 0x18
	ktfSaveModuleSegmentEnd = 0x1c
	ktfSaveModuleEntry      = 0x24
	ktfSaveModuleMarker     = 0x13580001
)

// ktfSaveModuleRelocations is the module's relocation list: the segment offset
// of every word that holds an address. The routines hold none, so the list is
// the records' own and does not move with the code. It is an array so that its
// length, which decides where the segment begins, is a constant.
var ktfSaveModuleRelocations = [...]uint32{
	0x00, ktfSaveModuleNameTable, ktfSaveModuleJumpTable, ktfSaveModuleEntry,
	ktfSaveModuleDescriptor, ktfSaveModuleDescriptor + 0x14,
	ktfSaveModuleClasses,
	ktfSaveModuleNames,
	ktfSaveClass, ktfSaveClass + 8,
	ktfSaveDescriptor, ktfSaveDescriptor + 12,
	ktfSaveMethods, ktfSaveMethods + 4, ktfSaveMethods + 12,
	ktfSaveMethods + ktfSaveMethodSize, ktfSaveMethods + ktfSaveMethodSize + 4, ktfSaveMethods + ktfSaveMethodSize + 12,
	ktfSaveMethods + 2*ktfSaveMethodSize, ktfSaveMethods + 2*ktfSaveMethodSize + 4, ktfSaveMethods + 2*ktfSaveMethodSize + 12,
	ktfSaveMethods + 3*ktfSaveMethodSize, ktfSaveMethods + 3*ktfSaveMethodSize + 4, ktfSaveMethods + 3*ktfSaveMethodSize + 12,
	ktfSaveMethodList, ktfSaveMethodList + 4, ktfSaveMethodList + 8, ktfSaveMethodList + 12,
	ktfSaveString, ktfSaveString + 8,
	ktfSaveCharacters,
}

// ktfSaveModuleSegment is where the module's segment is mapped: behind the two
// header words and the relocation list.
const ktfSaveModuleSegment = ktfImageBase + 8 + 4*uint32(len(ktfSaveModuleRelocations))

// KTFSaveArchive is a descriptor/JAR/AOT archive of the current generation.
// Its entry hands back an executable record, its interface initialization
// keeps the platform's callback table, and its GetClass answers one class: a
// Card that pushes itself onto the display and acts on the keys above.
func KTFSaveArchive() ([]byte, error) {
	image, entries, _, err := ktfSaveBuild(ktfImageBase, false)
	if err != nil {
		return nil, err
	}
	// The entry returns the executable record, which names the interface
	// record, which names the three functions the loader calls.
	binary.LittleEndian.PutUint16(image.data[0x00:], 0x4800) // ldr r0, [pc]
	binary.LittleEndian.PutUint16(image.data[0x02:], 0x4770) // bx  lr
	image.word(0x04, ktfImageBase+0x40)
	image.word(0x40, ktfImageBase+0x80)
	image.word(0x44, ktfImageBase+0xc0)
	image.word(0x40+5*4, entries.wipiInit)
	image.word(0x80, ktfImageBase+0xa0)
	image.word(0x84, ktfImageBase+0xd0)
	image.word(0xa0+2*4, entries.interfaceInit)
	image.word(0xa0+4*4, entries.getClass)
	copy(image.data[0xc0:], "WIPI_exe\x00")
	copy(image.data[0xd0:], "ExeInterface\x00")
	if image.err != nil {
		return nil, image.err
	}
	return ktfSavePackage("ktfsave", "P0002", image.data)
}

// KTFModuleSaveArchive is the same title as an older descriptor module: a
// relocation list in front of a segment whose header names the module's class
// table, its name table, the routine table the platform fills and its entry.
// The platform links the class and binds the save's name, which the module
// carries as a constant string; the entry only asks for the module interface.
func KTFModuleSaveArchive() ([]byte, error) {
	image, entries, names, err := ktfSaveBuild(ktfSaveModuleSegment, true)
	if err != nil {
		return nil, err
	}
	image.pointer(0x00, ktfSaveModuleSegment+ktfSaveModuleDescriptor)
	image.pointer(ktfSaveModuleNameTable, ktfSaveModuleSegment+ktfSaveModuleNames)
	image.pointer(ktfSaveModuleJumpTable, ktfSaveModuleSegment+ktfSaveModuleJumps)
	// The static base and the end of the file's bytes bound a table the loader
	// rebases whole. This module keeps no such table, so the two meet.
	image.word(ktfSaveModuleStaticBase, uint32(len(image.data)))
	image.word(ktfSaveModuleSegmentEnd, uint32(len(image.data)))
	image.word(ktfSaveModuleEntry-4, ktfSaveModuleMarker)
	image.pointer(ktfSaveModuleEntry, entries.moduleEntry)
	image.word(ktfSaveModuleEntry+4, ktfSaveModuleMarker)
	// The descriptor the platform reads the classes from: one class in a table
	// of one bucket, and the descriptor's own address, which identifies it.
	image.pointer(ktfSaveModuleDescriptor, ktfSaveModuleSegment+ktfSaveModuleClasses)
	image.word(ktfSaveModuleDescriptor+4, 1)
	image.word(ktfSaveModuleDescriptor+8, 1)
	image.pointer(ktfSaveModuleDescriptor+0x14, ktfSaveModuleSegment+ktfSaveModuleDescriptor)
	image.pointer(ktfSaveModuleClasses, ktfSaveModuleSegment+ktfSaveClass)
	// The name table holds the one name a record refers to by index: the
	// superclass. The six words at ktfSaveModuleJumps stay zero for the
	// platform to fill.
	image.pointer(ktfSaveModuleNames, names.card)
	if image.err != nil {
		return nil, image.err
	}
	slices.Sort(image.pointers)
	if !slices.Equal(image.pointers, ktfSaveModuleRelocations[:]) {
		return nil, fmt.Errorf("KTF save fixture relocation list does not name the module's address words")
	}
	module := make([]byte, 8, 8+4*len(ktfSaveModuleRelocations)+len(image.data))
	binary.LittleEndian.PutUint32(module[4:], uint32(len(ktfSaveModuleRelocations)))
	for _, offset := range ktfSaveModuleRelocations {
		module = binary.LittleEndian.AppendUint32(module, offset)
	}
	return ktfSavePackage("ktfmsave", "P0003", append(module, image.data...))
}

// ktfSavePackage wraps a client image in the JAR its descriptor names.
func ktfSavePackage(aid, pid string, client []byte) ([]byte, error) {
	jar, err := fixtureZIP([]fixtureEntry{{"client.bin0", client}})
	if err != nil {
		return nil, err
	}
	return fixtureZIP([]fixtureEntry{
		{"__adf__", []byte("AID:" + aid + "\nPID:" + pid + "\nMClass:fixture.Save\n")},
		{aid + ".jar", jar},
	})
}

// ktfSaveBuild assembles everything the two images share: the names, the
// routines, the class the platform constructs and the words the routines
// start from.
func ktfSaveBuild(base uint32, module bool) (*ktfSaveImage, ktfSaveEntries, ktfSaveNames, error) {
	image := &ktfSaveImage{base: base, data: make([]byte, ktfSaveRoutine), relocated: module, text: ktfSaveText}
	names := image.names(module)
	entries, err := image.routines(module, names)
	if err != nil {
		return nil, entries, names, err
	}
	image.records(module, names, entries)
	image.word(ktfSaveData+ktfSaveWordMask, KTFSaveMask)
	// The element type an array allocation is asked for by: the current
	// generation passes the descriptor character, and a module the byte offset
	// of that character in a table of the eight primitives.
	byteType := uint32('B')
	if module {
		byteType = 0x20
	}
	image.word(ktfSaveData+ktfSaveWordByteType, byteType)
	return image, entries, names, image.err
}

// ktfSaveImage lays one image out: fixed places for the words a test reads and
// for the class records, names packed after them, and the routines last.
type ktfSaveImage struct {
	// base is the address offset zero is mapped at.
	base uint32
	data []byte
	// relocated says an address is stored as an offset for the loader to
	// rebase, and pointers is where the stored addresses are.
	relocated bool
	pointers  []uint32
	text      uint32
	err       error
}

func (image *ktfSaveImage) fail(format string, arguments ...any) {
	if image.err == nil {
		image.err = fmt.Errorf(format, arguments...)
	}
}

func (image *ktfSaveImage) word(offset, value uint32) {
	if uint64(offset)+4 > uint64(len(image.data)) {
		image.fail("KTF save fixture word %#x is outside the image", offset)
		return
	}
	binary.LittleEndian.PutUint32(image.data[offset:], value)
}

func (image *ktfSaveImage) half(offset uint32, value uint16) {
	if uint64(offset)+2 > uint64(len(image.data)) {
		image.fail("KTF save fixture halfword %#x is outside the image", offset)
		return
	}
	binary.LittleEndian.PutUint16(image.data[offset:], value)
}

// pointer stores the address of something inside the image.
func (image *ktfSaveImage) pointer(offset, target uint32) {
	if image.relocated {
		image.pointers = append(image.pointers, offset)
		target -= image.base
	}
	image.word(offset, target)
}

// name packs one string into the name block and answers its address. Every
// name starts on a word, which is what lets a routine form its address.
func (image *ktfSaveImage) name(text string) uint32 {
	offset := image.text
	end := uint64(offset) + uint64(len(text)) + 1
	if end > ktfSaveRoutine {
		image.fail("KTF save fixture names outgrow their block")
		return 0
	}
	copy(image.data[offset:], text)
	image.text = uint32(end+3) &^ 3
	return image.base + offset
}

// ktfSaveNames are the names the routines and the records hand the platform.
type ktfSaveNames struct {
	own, card, display, file, fileSystem     uint32
	construct, start, key, paint             uint32
	getDisplay, pushCard                     uint32
	fileInit, fileWrite, fileRead, fileClose uint32
	exists, remove                           uint32
	platform, saveUnits, saveLength          uint32
}

// ktfSaveMethodName names a method the way its record does: a tag byte, the
// descriptor, a plus sign and the name.
func ktfSaveMethodName(descriptor, name string) string {
	return "\x00" + descriptor + "+" + name
}

func (image *ktfSaveImage) names(module bool) ktfSaveNames {
	names := ktfSaveNames{
		own:        image.name("fixture/Save"),
		card:       image.name("org/kwis/msp/lcdui/Card"),
		display:    image.name("org/kwis/msp/lcdui/Display"),
		file:       image.name("org/kwis/msp/io/File"),
		fileSystem: image.name("org/kwis/msp/io/FileSystem"),
		construct:  image.name(ktfSaveMethodName("()V", "<init>")),
		start:      image.name(ktfSaveMethodName("([Ljava/lang/String;)V", "startApp")),
		key:        image.name(ktfSaveMethodName("(II)Z", "keyNotify")),
		paint:      image.name(ktfSaveMethodName("(Lorg/kwis/msp/lcdui/Graphics;)V", "paint")),
		getDisplay: image.name(ktfSaveMethodName("()Lorg/kwis/msp/lcdui/Display;", "getDefaultDisplay")),
		pushCard:   image.name(ktfSaveMethodName("(Lorg/kwis/msp/lcdui/Card;)V", "pushCard")),
		fileInit:   image.name(ktfSaveMethodName("(Ljava/lang/String;I)V", "<init>")),
		fileWrite:  image.name(ktfSaveMethodName("([BII)I", "write")),
		fileRead:   image.name(ktfSaveMethodName("([BII)I", "read")),
		fileClose:  image.name(ktfSaveMethodName("()V", "close")),
		exists:     image.name(ktfSaveMethodName("(Ljava/lang/String;)Z", "exists")),
		remove:     image.name(ktfSaveMethodName("(Ljava/lang/String;)V", "remove")),
	}
	if module {
		names.platform = image.name("MNInterface")
		return names
	}
	names.platform = image.name("WIPI_JBInterface")
	// The current generation makes its strings at run time out of UTF-16 it
	// carries, so the save's name is carried that way.
	units := utf16.Encode([]rune(KTFSaveName))
	encoded := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(encoded[index*2:], unit)
	}
	names.saveUnits, names.saveLength = image.name(string(encoded)), uint32(len(units))
	return names
}

// ktfSaveFirstSlot is the dispatch slot the class's first method claims. A
// module ships slot numbers and leaves the table to the platform, so the
// numbers sit past any table its superclass fills. Nothing here makes a
// virtual call, and the platform finds the methods it calls by name.
const ktfSaveFirstSlot = 64

// records writes the one class: a Card with four methods and no field.
func (image *ktfSaveImage) records(module bool, names ktfSaveNames, entries ktfSaveEntries) {
	// A class record opens with the address of its own second word.
	image.pointer(ktfSaveClass, image.base+ktfSaveClass+4)
	image.pointer(ktfSaveClass+8, image.base+ktfSaveDescriptor)
	image.pointer(ktfSaveDescriptor, names.own)
	image.pointer(ktfSaveDescriptor+12, image.base+ktfSaveMethodList)
	image.half(ktfSaveDescriptor+28, 0x0021)
	for index, name := range []uint32{names.construct, names.start, names.key, names.paint} {
		record := uint32(ktfSaveMethods + index*ktfSaveMethodSize)
		image.pointer(record, entries.methods[index])
		image.pointer(record+4, image.base+ktfSaveClass)
		image.pointer(record+12, name)
		image.half(record+20, uint16(ktfSaveFirstSlot+index))
		image.half(record+22, 0x0001)
		image.pointer(ktfSaveMethodList+uint32(index)*4, image.base+record)
	}
	if !module {
		// The superclass word stays empty until GetClass resolves it.
		return
	}
	// A module names its superclass by a cell: the name's index in the module's
	// name table shifted up one, with the low bit set until the platform has
	// linked it. The name is the table's first.
	image.word(ktfSaveDescriptor+8, 0<<1|1)
	// Its constant string is two objects the platform binds when it loads the
	// module: the characters, and the string over them. Each opens with the
	// address of its own second word and a dispatch header that places its
	// class in the table a module's constants dispatch through, where the
	// character array is first and the string is second.
	units := utf16.Encode([]rune(KTFSaveName))
	image.pointer(ktfSaveCharacters, image.base+ktfSaveCharacters+4)
	image.word(ktfSaveCharacters+8, uint32(len(units)))
	for index, unit := range units {
		image.half(ktfSaveCharacters+12+uint32(index)*2, unit)
	}
	image.pointer(ktfSaveString, image.base+ktfSaveString+4)
	image.word(ktfSaveString+4, 20<<5)
	image.pointer(ktfSaveString+8, image.base+ktfSaveCharacters)
	image.word(ktfSaveString+16, uint32(len(units)))
}

// ARM condition codes, the register every routine keeps the data block in, and
// the three instructions every routine is framed by.
const (
	ktfSaveEqual    = 0x0
	ktfSaveNotEqual = 0x1
	ktfSaveHigher   = 0x8
	ktfSaveAlways   = 0xe

	ktfSaveBlock = 4

	ktfSavePush   = 0xe92d40f0 // push {r4-r7, lr}
	ktfSavePop    = 0x08bd80f0 // pop  {r4-r7, pc}, under a condition
	ktfSaveReturn = 0xe12fff1e // bx   lr
)

// ktfSaveCode assembles the routines. Every address it forms is relative to
// the program counter and no word of it holds one, so the same instructions
// run wherever the image is mapped and a relocating loader has nothing in them
// to rebase.
type ktfSaveCode struct {
	base   uint32
	words  []uint32
	labels []*ktfSaveLabel
	err    error
}

// ktfSaveLabel is a place a routine branches to.
type ktfSaveLabel struct {
	address uint32
	bound   bool
	uses    []int
}

func (code *ktfSaveCode) emit(words ...uint32) { code.words = append(code.words, words...) }
func (code *ktfSaveCode) here() uint32         { return code.base + uint32(len(code.words))*4 }

func (code *ktfSaveCode) fail(format string, arguments ...any) {
	if code.err == nil {
		code.err = fmt.Errorf(format, arguments...)
	}
}

func (code *ktfSaveCode) label() *ktfSaveLabel {
	label := &ktfSaveLabel{}
	code.labels = append(code.labels, label)
	return label
}

// bind places a label at the next instruction and completes the branches that
// were waiting for it.
func (code *ktfSaveCode) bind(label *ktfSaveLabel) {
	label.address, label.bound = code.here(), true
	for _, index := range label.uses {
		code.words[index] |= code.distance(index, label.address)
	}
	label.uses = nil
}

// distance is a branch's operand: how many words its target is from the
// program counter, which reads two instructions ahead.
func (code *ktfSaveCode) distance(index int, target uint32) uint32 {
	from := code.base + uint32(index)*4 + 8
	return uint32(int32(target-from)/4) & 0xffffff
}

func (code *ktfSaveCode) branch(condition uint32, label *ktfSaveLabel) {
	index := len(code.words)
	code.emit(condition<<28 | 0x0a000000)
	if label.bound {
		code.words[index] |= code.distance(index, label.address)
		return
	}
	label.uses = append(label.uses, index)
}

// call is a branch with link to a routine already assembled.
func (code *ktfSaveCode) call(condition, target uint32) {
	index := len(code.words)
	code.emit(condition<<28 | 0x0b000000 | code.distance(index, target))
}

// through calls the function a register holds. The platform's own are Thumb
// stubs, so the call is the ARMv4T interworking pair.
func (code *ktfSaveCode) through(register uint32) {
	code.emit(0xe1a0e00f, 0xe12fff10|register) // mov lr, pc; bx register
}

func (code *ktfSaveCode) immediate(value uint32) uint32 {
	if value > 0xff {
		code.fail("KTF save fixture immediate %#x does not fit an instruction", value)
	}
	return value & 0xff
}

func (code *ktfSaveCode) offset(value uint32) uint32 {
	if value > 0xfff {
		code.fail("KTF save fixture offset %#x does not fit an instruction", value)
	}
	return value & 0xfff
}

func (code *ktfSaveCode) move(rd, rm uint32) { code.emit(0xe1a00000 | rd<<12 | rm) }

func (code *ktfSaveCode) set(condition, rd, value uint32) {
	code.emit(condition<<28 | 0x03a00000 | rd<<12 | code.immediate(value))
}

func (code *ktfSaveCode) load(rd, rn, offset uint32) {
	code.emit(0xe5900000 | rn<<16 | rd<<12 | code.offset(offset))
}

func (code *ktfSaveCode) store(rd, rn, offset uint32) {
	code.emit(0xe5800000 | rn<<16 | rd<<12 | code.offset(offset))
}

func (code *ktfSaveCode) loadHalf(rd, rn, offset uint32) {
	offset = code.immediate(offset)
	code.emit(0xe1d000b0 | rn<<16 | rd<<12 | offset>>4<<8 | offset&0xf)
}

func (code *ktfSaveCode) storeHalf(rd, rn, offset uint32) {
	offset = code.immediate(offset)
	code.emit(0xe1c000b0 | rn<<16 | rd<<12 | offset>>4<<8 | offset&0xf)
}

func (code *ktfSaveCode) add(rd, rn, value uint32) {
	code.emit(0xe2800000 | rn<<16 | rd<<12 | code.immediate(value))
}

func (code *ktfSaveCode) compare(rn, value uint32) {
	code.emit(0xe3500000 | rn<<16 | code.immediate(value))
}

// address forms the address of something in the image from the program
// counter, in two instructions: the part above ten bits, then the part below.
func (code *ktfSaveCode) address(rd, target uint32) {
	from := code.here() + 8
	first, second, distance := uint32(0xe28f0000), uint32(0xe2800000), target-from
	if target < from {
		first, second, distance = 0xe24f0000, 0xe2400000, from-target
	}
	if distance&3 != 0 || distance >= 1<<18 {
		code.fail("KTF save fixture address %#x is out of reach of %#x", target, from)
	}
	code.emit(first|rd<<12|0xb00|distance>>10&0xff, second|rd<<16|rd<<12|0xf00|distance>>2&0xff)
}

func (code *ktfSaveCode) bytes() ([]byte, error) {
	for _, label := range code.labels {
		if !label.bound {
			code.fail("KTF save fixture routine branches to a label it never placed")
		}
	}
	if code.err != nil {
		return nil, code.err
	}
	data := make([]byte, len(code.words)*4)
	for index, word := range code.words {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	return data, nil
}

// ktfSaveEntries is where the assembled routines begin.
type ktfSaveEntries struct {
	// The class's four methods, in the order its records list them: the
	// constructor, startApp, keyNotify and paint.
	methods [ktfSaveMethodCount]uint32
	// The current generation's interface initialization, WIPI initialization
	// and GetClass, or the older module's entry.
	interfaceInit, wipiInit, getClass, moduleEntry uint32
}

// routines assembles the fixture's code behind the image's fixed blocks.
func (image *ktfSaveImage) routines(module bool, names ktfSaveNames) (ktfSaveEntries, error) {
	code := &ktfSaveCode{base: image.base + ktfSaveRoutine}
	var entries ktfSaveEntries
	block, descriptor := image.base+ktfSaveData, image.base+ktfSaveDescriptor

	enter := func() uint32 {
		at := code.here()
		code.emit(ktfSavePush)
		return at
	}
	leave := func(condition uint32) { code.emit(condition<<28 | ktfSavePop) }
	// call reaches the platform through a pointer the data block holds.
	call := func(word uint32) {
		code.load(12, ktfSaveBlock, word)
		code.through(12)
	}

	// sizeInstance(r0 = the superclass record) leaves room in an instance for
	// the fields the superclass publishes. A title's own code does this when
	// it prepares a class, from the size its parent declares, and the size is
	// not this image's to know: the platform's Card constructor writes its
	// geometry into an instance whose class declares no field of its own.
	sizeInstance := code.here()
	code.load(0, 0, 8)      // the superclass's descriptor
	code.loadHalf(0, 0, 26) // the bytes its fields take
	code.address(1, descriptor)
	code.storeHalf(0, 1, 26)
	code.emit(ktfSaveReturn)

	// setup() resolves, once, everything the routines below call: three
	// classes, nine method bodies, the save's name and an eight-byte array to
	// carry its two parts in.
	setup := enter()
	code.address(ktfSaveBlock, block)
	code.load(0, ktfSaveBlock, ktfSaveWordReady)
	code.compare(0, 0)
	leave(ktfSaveNotEqual)
	for _, class := range []struct{ word, name uint32 }{
		{ktfSaveWordDisplayClass, names.display},
		{ktfSaveWordFileClass, names.file},
		{ktfSaveWordFileSystemClass, names.fileSystem},
	} {
		code.add(0, ktfSaveBlock, class.word)
		code.address(1, class.name)
		call(ktfSaveWordLoadClass)
	}
	// The superclass is the one the class record names, which is resolved by
	// the time anything constructs an instance.
	code.address(5, descriptor)
	code.load(5, 5, 8)
	for _, method := range []struct{ class, name, word uint32 }{
		{0, names.construct, ktfSaveWordCardInit},
		{ktfSaveWordDisplayClass, names.getDisplay, ktfSaveWordGetDisplay},
		{ktfSaveWordDisplayClass, names.pushCard, ktfSaveWordPushCard},
		{ktfSaveWordFileClass, names.fileInit, ktfSaveWordFileInit},
		{ktfSaveWordFileClass, names.fileWrite, ktfSaveWordFileWrite},
		{ktfSaveWordFileClass, names.fileRead, ktfSaveWordFileRead},
		{ktfSaveWordFileClass, names.fileClose, ktfSaveWordFileClose},
		{ktfSaveWordFileSystemClass, names.exists, ktfSaveWordExists},
		{ktfSaveWordFileSystemClass, names.remove, ktfSaveWordRemove},
	} {
		if method.class == 0 {
			code.move(0, 5)
		} else {
			code.load(0, ktfSaveBlock, method.class)
		}
		code.address(1, method.name)
		call(ktfSaveWordFindMethod)
		code.load(0, 0, 0) // a method record opens with its body
		code.store(0, ktfSaveBlock, method.word)
	}
	if module {
		// A module carries its strings as objects the platform binds at load.
		code.address(0, image.base+ktfSaveString)
	} else {
		code.address(0, names.saveUnits)
		code.set(ktfSaveAlways, 1, names.saveLength)
		call(ktfSaveWordRegisterString)
	}
	code.store(0, ktfSaveBlock, ktfSaveWordName)
	code.load(0, ktfSaveBlock, ktfSaveWordByteType)
	code.set(ktfSaveAlways, 1, 8)
	call(ktfSaveWordNewArray)
	code.store(0, ktfSaveBlock, ktfSaveWordBuffer)
	code.set(ktfSaveAlways, 0, 1)
	code.store(0, ktfSaveBlock, ktfSaveWordReady)
	leave(ktfSaveAlways)

	// The pieces an action is made of. Each expects the data block in r4 and,
	// once there is one, the File in r6.
	//
	// elements leaves the address of the array's first byte in a register: an
	// object's first word is the address of its field block, and an array's
	// fields are its dispatch header, its length and then its elements.
	elements := func(rd uint32) {
		code.load(rd, ktfSaveBlock, ktfSaveWordBuffer)
		code.load(rd, rd, 0)
		code.add(rd, rd, 8)
	}
	partA := func() { code.load(0, ktfSaveBlock, ktfSaveWordProgress) }
	partB := func() {
		code.load(0, ktfSaveBlock, ktfSaveWordProgress)
		code.load(1, ktfSaveBlock, ktfSaveWordMask)
		code.emit(0xe0200001) // eor r0, r0, r1
	}
	// open is `new File(name, mode)`.
	open := func(mode uint32) {
		code.load(0, ktfSaveBlock, ktfSaveWordFileClass)
		call(ktfSaveWordNewObject)
		code.move(6, 0)
		code.move(1, 6)
		code.load(2, ktfSaveBlock, ktfSaveWordName)
		code.set(ktfSaveAlways, 3, mode)
		call(ktfSaveWordFileInit)
	}
	// transfer is `file.write(array, 0, count)` or the read of the same shape.
	// The receiver and two arguments go in registers and the fourth word on
	// the stack, which is where the platform's method bodies look for it.
	transfer := func(word, count uint32) {
		code.set(ktfSaveAlways, 0, count)
		code.emit(0xe52d0004) // str r0, [sp, #-4]!
		code.move(1, 6)
		code.load(2, ktfSaveBlock, ktfSaveWordBuffer)
		code.set(ktfSaveAlways, 3, 0)
		call(word)
		code.emit(0xe28dd004) // add sp, sp, #4
	}
	closeFile := func() {
		code.move(1, 6)
		call(ktfSaveWordFileClose)
	}
	// finish counts the action and returns.
	finish := func() {
		code.load(0, ktfSaveBlock, ktfSaveWordActions)
		code.add(0, 0, 1)
		code.store(0, ktfSaveBlock, ktfSaveWordActions)
		leave(ktfSaveAlways)
	}
	// The three ways the fixture opens a File: to read one that has to be
	// there, to write over what one holds, and to write one from nothing.
	const (
		modeRead     = 1
		modeWrite    = 2
		modeTruncate = 3
	)

	save := enter()
	elements(7)
	partA()
	code.store(0, 7, 0)
	partB()
	code.store(0, 7, 4)
	open(modeTruncate)
	transfer(ktfSaveWordFileWrite, 8)
	closeFile()
	finish()

	patch := enter()
	elements(7)
	partA()
	code.store(0, 7, 0)
	open(modeWrite)
	transfer(ktfSaveWordFileWrite, 4)
	closeFile()
	finish()

	// A read that finds no save leaves every word it answers in at zero. The
	// name is asked about before it is opened, because opening a file that is
	// not there for reading is an exception rather than an answer.
	read := enter()
	absent := code.label()
	code.set(ktfSaveAlways, 0, 0)
	for _, word := range []uint32{ktfSaveWordSeenA, ktfSaveWordSeenB, ktfSaveWordStatus, ktfSaveWordLength} {
		code.store(0, ktfSaveBlock, word)
	}
	elements(7)
	code.store(0, 7, 0)
	code.store(0, 7, 4)
	code.load(1, ktfSaveBlock, ktfSaveWordName)
	call(ktfSaveWordExists)
	code.compare(0, 0)
	code.branch(ktfSaveEqual, absent)
	open(modeRead)
	transfer(ktfSaveWordFileRead, 8)
	code.move(5, 0)
	closeFile()
	// A read at the end of a file answers -1, which is what an empty save
	// answers at once. Any other count the array could not hold is not one.
	code.emit(0xe3750001) // cmn r5, #1
	code.set(ktfSaveEqual, 5, 0)
	code.set(ktfSaveAlways, 0, KTFSaveFound)
	code.compare(5, 8)
	code.set(ktfSaveHigher, 0, KTFSaveError)
	code.set(ktfSaveHigher, 5, 0)
	code.store(0, ktfSaveBlock, ktfSaveWordStatus)
	code.store(5, ktfSaveBlock, ktfSaveWordLength)
	elements(7)
	code.load(0, 7, 0)
	code.store(0, ktfSaveBlock, ktfSaveWordSeenA)
	code.load(0, 7, 4)
	code.store(0, ktfSaveBlock, ktfSaveWordSeenB)
	code.bind(absent)
	finish()

	remove := enter()
	code.load(1, ktfSaveBlock, ktfSaveWordName)
	call(ktfSaveWordRemove)
	finish()

	hold := enter()
	elements(7)
	partA()
	code.store(0, 7, 0)
	open(modeWrite)
	code.store(6, ktfSaveBlock, ktfSaveWordKept)
	transfer(ktfSaveWordFileWrite, 4)
	finish()

	truncate := enter()
	open(modeTruncate)
	code.store(6, ktfSaveBlock, ktfSaveWordKept)
	finish()

	// Finishing with nothing kept is an action that does nothing.
	complete := enter()
	nothingKept := code.label()
	code.load(6, ktfSaveBlock, ktfSaveWordKept)
	code.compare(6, 0)
	code.branch(ktfSaveEqual, nothingKept)
	elements(7)
	partB()
	code.store(0, 7, 0)
	transfer(ktfSaveWordFileWrite, 4)
	closeFile()
	code.set(ktfSaveAlways, 0, 0)
	code.store(0, ktfSaveBlock, ktfSaveWordKept)
	code.bind(nothingKept)
	finish()

	// <init>(): resolve the platform, then the superclass constructor. A
	// method is entered with a reserved word in r0, its receiver in r1 and its
	// arguments after that.
	entries.methods[0] = enter()
	code.move(5, 1)
	code.call(ktfSaveAlways, setup)
	code.address(ktfSaveBlock, block)
	code.move(1, 5)
	call(ktfSaveWordCardInit)
	leave(ktfSaveAlways)

	// startApp(arguments): `Display.getDefaultDisplay().pushCard(this)`.
	entries.methods[1] = enter()
	code.move(5, 1)
	code.address(ktfSaveBlock, block)
	call(ktfSaveWordGetDisplay)
	code.move(1, 0)
	code.move(2, 5)
	call(ktfSaveWordPushCard)
	leave(ktfSaveAlways)

	// keyNotify(type, key): one action for a press of its digit, and nothing
	// for a release, a repeat or any other key. It answers false, so the event
	// goes no further down the card stack.
	entries.methods[2] = enter()
	ignored := code.label()
	code.address(ktfSaveBlock, block)
	code.compare(2, 1)
	code.branch(ktfSaveNotEqual, ignored)
	code.move(5, 3)
	for _, action := range []struct {
		key     int32
		routine uint32
	}{
		{KTFSaveActionSave, save},
		{KTFSaveActionPatch, patch},
		{KTFSaveActionRead, read},
		{KTFSaveActionDelete, remove},
		{KTFSaveActionHold, hold},
		{KTFSaveActionTruncate, truncate},
		{KTFSaveActionFinish, complete},
	} {
		code.compare(5, uint32(action.key))
		code.call(ktfSaveEqual, action.routine)
	}
	code.bind(ignored)
	code.set(ktfSaveAlways, 0, 0)
	leave(ktfSaveAlways)

	// paint(graphics) draws nothing. The platform paints whichever card is
	// showing, and a card with no paint of its own fails the round.
	entries.methods[3] = code.here()
	code.emit(ktfSaveReturn)

	if module {
		// The entry is Thumb, as every module's is, and steps into ARM at once.
		// It is handed the platform's callback table, whose first word answers
		// an interface by name; a module asks it for one interface, at any
		// version, and does everything else through what it is given back. The
		// loader takes anything but zero for a refusal.
		veneer := code.here()
		code.emit(0x46c04778, ktfSavePush) // bx pc; nop
		code.address(ktfSaveBlock, block)
		code.move(5, 0)
		code.store(5, ktfSaveBlock, ktfSaveWordCallbacks)
		code.address(0, names.platform)
		code.emit(0xe3e01000, 0xe3e02000) // mvn r1, #0; mvn r2, #0
		code.load(12, 5, 0)
		code.through(12)
		code.compare(0, 0)
		code.set(ktfSaveEqual, 0, 1)
		leave(ktfSaveEqual)
		code.store(0, ktfSaveBlock, ktfSaveWordInterface)
		for _, function := range []struct{ offset, word uint32 }{
			{0x40, ktfSaveWordLoadClass},
			{0x64, ktfSaveWordFindMethod},
			{0x38, ktfSaveWordNewObject},
			{0x70, ktfSaveWordNewArray},
		} {
			code.load(1, 0, function.offset)
			code.store(1, ktfSaveBlock, function.word)
		}
		// The platform links the class after the entry returns, so the room
		// its instances need is settled here, from the superclass by name.
		code.add(0, ktfSaveBlock, ktfSaveWordCardClass)
		code.address(1, names.card)
		call(ktfSaveWordLoadClass)
		code.load(0, ktfSaveBlock, ktfSaveWordCardClass)
		code.call(ktfSaveAlways, sizeInstance)
		code.set(ktfSaveAlways, 0, 0)
		leave(ktfSaveAlways)
		entries.moduleEntry = veneer | 1
	} else {
		// Interface initialization is handed five words, the fifth on the
		// stack: the platform's callback table. Class loading and allocation
		// are slots of it, and its first word answers the Java interface the
		// method lookup and the string registration belong to.
		entries.interfaceInit = enter()
		code.address(ktfSaveBlock, block)
		code.load(5, 13, 20) // above the five registers just pushed
		code.store(5, ktfSaveBlock, ktfSaveWordCallbacks)
		for _, slot := range []struct{ index, word uint32 }{
			{8, ktfSaveWordLoadClass},
			{5, ktfSaveWordNewObject},
			{6, ktfSaveWordNewArray},
		} {
			code.load(0, 5, slot.index*4)
			code.store(0, ktfSaveBlock, slot.word)
		}
		code.address(0, names.platform)
		code.load(12, 5, 0)
		code.through(12)
		code.compare(0, 0)
		code.set(ktfSaveEqual, 0, 1)
		leave(ktfSaveEqual)
		code.store(0, ktfSaveBlock, ktfSaveWordInterface)
		for _, slot := range []struct{ index, word uint32 }{
			{4, ktfSaveWordFindMethod},
			{11, ktfSaveWordRegisterString},
		} {
			code.load(1, 0, slot.index*4)
			code.store(1, ktfSaveBlock, slot.word)
		}
		code.set(ktfSaveAlways, 0, 0)
		leave(ktfSaveAlways)

		entries.wipiInit = code.here()
		code.set(ktfSaveAlways, 0, 0)
		code.emit(ktfSaveReturn)

		// GetClass(name) answers the one class this image has, and null for
		// any other name. The first time, it resolves the superclass into the
		// descriptor and sizes the instance from it, which is the preparation
		// a title's own GetClass does before it hands a class over.
		entries.getClass = enter()
		compared, unknown, prepared := code.label(), code.label(), code.label()
		code.address(ktfSaveBlock, block)
		code.address(1, names.own)
		code.bind(compared)
		code.emit(0xe4d02001, 0xe4d13001, 0xe1520003) // ldrb r2, [r0], #1; ldrb r3, [r1], #1; cmp r2, r3
		code.branch(ktfSaveNotEqual, unknown)
		code.compare(2, 0)
		code.branch(ktfSaveNotEqual, compared)
		code.address(5, descriptor)
		code.load(0, 5, 8)
		code.compare(0, 0)
		code.branch(ktfSaveNotEqual, prepared)
		code.add(0, 5, 8)
		code.address(1, names.card)
		call(ktfSaveWordLoadClass)
		code.load(0, 5, 8)
		code.call(ktfSaveAlways, sizeInstance)
		code.bind(prepared)
		code.address(0, image.base+ktfSaveClass)
		leave(ktfSaveAlways)
		code.bind(unknown)
		code.set(ktfSaveAlways, 0, 0)
		leave(ktfSaveAlways)
	}

	assembled, err := code.bytes()
	if err != nil {
		return entries, err
	}
	image.data = append(image.data, assembled...)
	return entries, image.err
}

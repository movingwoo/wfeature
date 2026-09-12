//go:build ignore

// Command generate_text_input writes the authored LGT AOT fixture used by
// cross-package Host text-input tests.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
)

const (
	textBase uint32 = 0x1000
	dataBase uint32 = 0x3000

	globals    = dataBase
	initStruct = dataBase + 0x20
	shellWord  = dataBase + 0x30
	fieldWord  = dataBase + 0x34

	staticMethods  = dataBase + 0x400
	methods        = dataBase + 0x460
	virtualMethods = dataBase + 0x468
	staticFields   = dataBase + 0x488
	fields         = dataBase + 0x490
	classes        = dataBase + 0x498
	strings        = dataBase + 0x700

	fieldsOut         = dataBase + 0x900
	staticFieldsOut   = dataBase + 0x910
	virtualMethodsOut = dataBase + 0x920
	methodsOut        = dataBase + 0x930
	staticMethodsOut  = dataBase + 0x940
)

const (
	importTableJava   uint32 = 0x64
	javaLoadClasses   uint32 = 0x14
	javaAllocate      uint32 = 0x0f
	globalLoadClasses        = 0
	globalAllocate           = 1
)

const (
	armPushLR  = 0xe92d40f0 // push {r4-r7, lr}
	armPopPC   = 0xe8bd80f0 // pop {r4-r7, pc}
	armMovLRPC = 0xe1a0e00f
	thumbToARM = 0x46c04778 // bx pc; mov r8, r8
)

func armMovImm(rd, value uint32) uint32 { return 0xe3a00000 | rd<<12 | value&0xff }
func armMovReg(rd, rm uint32) uint32    { return 0xe1a00000 | rd<<12 | rm }
func armLdr(rd, rn, off uint32) uint32  { return 0xe5900000 | rn<<16 | rd<<12 | off }
func armStr(rd, rn, off uint32) uint32  { return 0xe5800000 | rn<<16 | rd<<12 | off }
func armLdrPC(rd, off uint32) uint32    { return 0xe59f0000 | rd<<12 | off }
func armLdrh(rd, rn, off uint32) uint32 {
	return 0xe1d000b0 | rn<<16 | rd<<12 | (off&0xf0)<<4 | off&0xf
}
func armAddShift(rd, rn, rm, shift uint32) uint32 {
	return 0xe0800000 | rn<<16 | rd<<12 | shift<<7 | rm
}
func armSubSP(value uint32) uint32 { return 0xe24dd000 | value }
func armAddSP(value uint32) uint32 { return 0xe28dd000 | value }
func armStrSP(rd, off uint32) uint32 {
	return 0xe58d0000 | rd<<12 | off
}
func armBX(rm uint32) uint32 { return 0xe12fff10 | rm }

type assembler struct {
	base     uint32
	words    []uint32
	literals []uint32
}

func (a *assembler) emit(words ...uint32) { a.words = append(a.words, words...) }
func (a *assembler) here() uint32         { return a.base + uint32(len(a.words))*4 }

const literalMark = 0x800

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
	a.words = append(a.words, armLdrPC(rd, literalMark|uint32(index)))
}

func (a *assembler) call(register uint32) { a.emit(armMovLRPC, armBX(register)) }

func (a *assembler) callGlobal(index uint32) {
	a.literal(4, globals+index*4)
	a.emit(armLdr(4, 4, 0))
	a.call(4)
}

func (a *assembler) callStatic(index uint32) {
	a.literal(4, staticMethodsOut+index*4)
	a.emit(armLdr(4, 4, 0))
	a.call(4)
}

func (a *assembler) callVirtual(index uint32) {
	a.literal(3, virtualMethodsOut+index*2)
	a.emit(armLdrh(3, 3, 0))
	a.emit(armLdr(2, 0, 0))
	a.emit(armAddShift(3, 2, 3, 2))
	a.emit(armLdr(4, 3, 4))
	a.call(4)
}

func (a *assembler) finish() []byte {
	poolAddress := a.base + uint32(len(a.words))*4
	for index, word := range a.words {
		if word&0xfff < literalMark || word&0xffff0000 != armLdrPC(word>>12&0xf, 0)&0xffff0000 {
			continue
		}
		literal := word & 0x7ff
		address := a.base + uint32(index)*4
		a.words[index] = armLdrPC(word>>12&0xf, poolAddress+literal*4-(address+8))
	}
	all := append(append([]uint32(nil), a.words...), a.literals...)
	data := make([]byte, len(all)*4)
	for index, word := range all {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	return data
}

func module() (code []byte, entry, initialize uint32) {
	a := &assembler{base: textBase}
	entry = a.here()
	a.emit(thumbToARM, armPushLR)
	a.emit(armMovReg(4, 0))
	a.emit(armLdr(5, 1, 4))
	for index, function := range []uint32{javaLoadClasses, javaAllocate} {
		a.emit(armMovImm(0, importTableJava), armMovImm(1, function))
		a.call(5)
		a.literal(6, globals+uint32(index)*4)
		a.emit(armStr(0, 6, 0))
	}
	a.literal(0, initStruct)
	a.emit(armStr(0, 4, 512+20), armMovImm(0, 0), armPopPC)

	initialize = a.here()
	a.emit(armPushLR, armSubSP(28))
	for register, value := range []uint32{classes, fields, staticFields, virtualMethods} {
		a.literal(uint32(register), value)
	}
	for index, value := range []uint32{
		methods, staticMethods, fieldsOut, staticFieldsOut,
		virtualMethodsOut, methodsOut, staticMethodsOut,
	} {
		a.literal(4, value)
		a.emit(armStrSP(4, uint32(index)*4))
	}
	a.callGlobal(globalLoadClasses)
	a.emit(armAddSP(28))

	// Shell class, instance and constructor.
	a.callStatic(4)
	a.callGlobal(globalAllocate)
	a.literal(5, shellWord)
	a.emit(armStr(0, 5, 0))
	a.emit(armMovImm(1, 0), armMovImm(2, 0), armMovImm(3, 16), armSubSP(4))
	a.emit(armMovImm(4, 8), armStrSP(4, 0))
	a.callStatic(6)
	a.emit(armAddSP(4))

	// Text field class, instance and constructor with empty Any text.
	a.callStatic(9)
	a.callGlobal(globalAllocate)
	a.literal(5, fieldWord)
	a.emit(armStr(0, 5, 0), armMovImm(1, 0), armMovImm(2, 0))
	a.callStatic(11)

	// shell.addComponent(field), shell.show(), field.setMaxLength(16),
	// and field.setFocus(), all through the vtable slots the load call filled.
	a.literal(5, shellWord)
	a.emit(armLdr(0, 5, 0))
	a.literal(5, fieldWord)
	a.emit(armLdr(1, 5, 0))
	a.callVirtual(1)
	a.literal(5, shellWord)
	a.emit(armLdr(0, 5, 0))
	a.callVirtual(2)
	a.literal(5, fieldWord)
	a.emit(armLdr(0, 5, 0), armMovImm(1, 16))
	a.callVirtual(3)
	a.literal(5, fieldWord)
	a.emit(armLdr(0, 5, 0))
	a.callVirtual(0)
	a.emit(armMovImm(0, 0), armPopPC)
	return a.finish(), entry, initialize
}

func data(initialize uint32) []byte {
	data := make([]byte, 0xc00)
	put := func(address uint32, words ...uint32) {
		for index, word := range words {
			binary.LittleEndian.PutUint32(data[address-dataBase+uint32(index)*4:], word)
		}
	}
	put(initStruct, 0, initialize, 0)

	pool := strings
	plant := func(value string) uint32 {
		address := pool
		copy(data[address-dataBase:], append([]byte(value), 0))
		pool += uint32(len(value)+4) &^ 3
		return address
	}
	component := plant("org/kwis/msp/lwc/Component")
	container := plant("org/kwis/msp/lwc/ContainerComponent")
	shell := plant("org/kwis/msp/lwc/ShellComponent")
	text := plant("org/kwis/msp/lwc/TextComponent")
	field := plant("org/kwis/msp/lwc/TextFieldComponent")
	constructor := plant("<init>")
	shellConstructor := plant("(IIII)V")
	fieldConstructor := plant("(Ljava/lang/String;I)V")
	setFocus := plant("setFocus")
	noArgs := plant("()V")
	addComponent := plant("addComponent")
	addDescriptor := plant("(Lorg/kwis/msp/lwc/Component;)I")
	show := plant("show")
	setMaxLength := plant("setMaxLength")
	maxDescriptor := plant("(I)V")

	// Two unnamed static entries lead each class run. Constructors follow the
	// unnamed entries for the classes that declare one.
	put(staticMethods,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0, constructor, shellConstructor,
		0, 0, 0, 0,
		0, 0, 0, 0, constructor, fieldConstructor,
	)
	put(methods, 0xffffffff, 0xffffffff)
	put(virtualMethods,
		setFocus, noArgs,
		addComponent, addDescriptor,
		show, noArgs,
		setMaxLength, maxDescriptor,
	)
	put(staticFields, 0xffffffff, 0xffffffff)
	put(fields, 0xffffffff, 0xffffffff)
	put(classes, 5,
		component, 0, 0, 1<<16|0, 0, 2<<16|0,
		container, 0, 0, 0, 0, 2<<16|2,
		shell, 0, 0, 2<<16|1, 0, 3<<16|4,
		text, 0, 0, 1<<16|3, 0, 2<<16|7,
		field, 0, 0, 0, 0, 3<<16|9,
	)
	return data
}

func elf(code, data []byte, entry uint32) []byte {
	const headerSize, entrySize, sections = uint32(52), uint32(40), uint32(4)
	names := []byte("\x00.text\x00.data\x00.shstrtab\x00")
	codeOffset := headerSize
	dataOffset := codeOffset + uint32(len(code))
	namesOffset := dataOffset + uint32(len(data))
	sectionOffset := namesOffset + uint32(len(names))
	image := make([]byte, sectionOffset+sections*entrySize)
	copy(image, "\x7fELF")
	image[4], image[5], image[6] = 1, 1, 1
	binary.LittleEndian.PutUint16(image[16:], 2)
	binary.LittleEndian.PutUint16(image[18:], 40)
	binary.LittleEndian.PutUint32(image[20:], 1)
	binary.LittleEndian.PutUint32(image[24:], entry)
	binary.LittleEndian.PutUint32(image[32:], sectionOffset)
	binary.LittleEndian.PutUint16(image[40:], uint16(headerSize))
	binary.LittleEndian.PutUint16(image[46:], uint16(entrySize))
	binary.LittleEndian.PutUint16(image[48:], uint16(sections))
	binary.LittleEndian.PutUint16(image[50:], 3)
	copy(image[codeOffset:], code)
	copy(image[dataOffset:], data)
	copy(image[namesOffset:], names)
	section := func(index, name, kind, flags, address, offset, size uint32) {
		base := sectionOffset + index*entrySize
		put := func(word, value uint32) { binary.LittleEndian.PutUint32(image[base+word*4:], value) }
		put(0, name)
		put(1, kind)
		put(2, flags)
		put(3, address)
		put(4, offset)
		put(5, size)
	}
	section(0, 0, 0, 0, 0, 0, 0)
	section(1, 1, 1, 0x6, textBase, codeOffset, uint32(len(code)))
	section(2, 7, 1, 0x2, dataBase, dataOffset, uint32(len(data)))
	section(3, 13, 3, 0, 0, namesOffset, uint32(len(names)))
	return image
}

func pack(entries map[string][]byte) ([]byte, error) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		contents := entries[name]
		entry, err := writer.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(contents); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func main() {
	code, entry, initialize := module()
	moduleArchive, err := pack(map[string][]byte{"binary.mod": elf(code, data(initialize), entry)})
	if err != nil {
		panic(err)
	}
	archive, err := pack(map[string][]byte{
		"app_info":     []byte("AID=54585449\nPID=PF000001\nMClass=TextInputFixture\nName=Text Input Fixture\n"),
		"54585449.jar": moduleArchive,
	})
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("text-input.zip", archive, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("wrote text-input.zip (%d bytes)\n", len(archive))
}

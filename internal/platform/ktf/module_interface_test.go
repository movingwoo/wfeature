package ktf

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// syntheticModuleClient builds an older module whose entry does what all three
// local ones do: read the first word of the table it was handed and call it
// with an interface name, then report success.
func syntheticModuleClient() []byte {
	const (
		entryCode  = 0x40
		nameWord   = 0x50
		nameText   = 0x60
		descriptor = 0x80
		classTable = 0x90
		nameTable  = 0xa0
		jumpTable  = 0xb0
		staticBase = 0xe0
		segmentEnd = 0xf0
	)
	segment := make([]byte, 0x100)
	put := func(offset int, value uint32) {
		binary.LittleEndian.PutUint32(segment[offset:], value)
	}
	// The segment header: the module descriptor, the name table every
	// reference cell indexes, the table of routines the platform fills, and
	// the two bounds the loader reads.
	put(0x00, descriptor)
	put(moduleNameTableOffset, nameTable)
	put(0x14, jumpTable)
	put(relocatableStaticBaseOffset, staticBase)
	put(relocatableSegmentEndOffset, segmentEnd)
	put(relocatableEntryOffset-4, relocatableMarker)
	put(relocatableEntryOffset, entryCode|1)
	put(relocatableEntryOffset+4, relocatableMarker)
	// A descriptor that names itself, over a table with no classes in it.
	put(descriptor+0x00, classTable)
	put(descriptor+0x04, 0)
	put(descriptor+0x08, 1)
	put(descriptor+0x14, descriptor)
	copy(segment[entryCode:], []byte{
		0x00, 0xb5, // push {lr}
		0x03, 0x68, // ldr r3, [r0]      the table's first function
		0x02, 0x48, // ldr r0, [pc, #8]  the interface name
		0x98, 0x47, // blx r3
		0x00, 0x20, // movs r0, #0       success
		0x00, 0xbd, // pop {pc}
	})
	put(nameWord, nameText)
	copy(segment[nameText:], append([]byte(moduleInterfaceName), 0))

	relocations := []uint32{0x00, moduleNameTableOffset, 0x14, relocatableEntryOffset, nameWord, descriptor, descriptor + 0x14}
	image := make([]byte, relocatableHeaderWords*4)
	binary.LittleEndian.PutUint32(image[0:], 0)
	binary.LittleEndian.PutUint32(image[4:], uint32(len(relocations)))
	for _, offset := range relocations {
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], offset)
		image = append(image, word[:]...)
	}
	return append(image, segment...)
}

func loadSyntheticModule(t *testing.T) *Client {
	t.Helper()
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticModuleClient()}, armcore.CoreOptions{MaxSteps: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !client.IsModule() {
		t.Fatal("a module in the older layout was not recognized as one")
	}
	return client
}

// The entry of an older module is handed the platform's callback table rather
// than a size, which means the runtime that owns that table has to exist
// before the entry runs. Without it the entry called the size as a function.
func TestModuleEntryResolvesItsInterfaceThroughThePlatformTable(t *testing.T) {
	client := loadSyntheticModule(t)
	summary, err := client.ExecuteModuleEntry(context.Background())
	if err != nil {
		t.Fatalf("ExecuteModuleEntry() error = %v", err)
	}
	if summary.Steps == 0 {
		t.Fatal("the module entry retired no instructions")
	}
	if client.runtime == nil || client.runtime.moduleInterface == 0 {
		t.Fatal("the entry did not resolve the interface it asks for by name")
	}
}

// A slot no local module reaches is a stub that names itself and the guest
// address that called it, because a table reached only through a register
// never mentions a slot number in the module's own code.
func TestModuleInterfaceSlotsNameThemselvesAndTheirCaller(t *testing.T) {
	client := loadSyntheticModule(t)
	if _, err := client.ExecuteModuleEntry(context.Background()); err != nil {
		t.Fatal(err)
	}
	runtime := client.runtime
	table := runtime.moduleInterface
	buffer := make([]byte, 4)
	if err := client.core.Memory().Read(table+0x2c, buffer); err != nil {
		t.Fatal(err)
	}
	stub := binary.LittleEndian.Uint32(buffer)
	initial := armcore.NewContext()
	initial.Registers[armcore.RegisterSP] = ThreadStackBase + uint32(ThreadStackSize)
	thread := armcore.NewThread(initial)
	_, err := client.core.Call(context.Background(), thread, stub, ReturnAddress, nil, runtime.handleSupervisorCall)
	if err == nil {
		t.Fatal("an unimplemented interface slot answered instead of reporting itself")
	}
	if !strings.Contains(err.Error(), "module interface function 11 (0x2c)") {
		t.Fatalf("error = %v, want the slot's number and byte offset", err)
	}
}

// A module names a primitive by the byte offset into the eight characters the
// current generation is handed as an initialization parameter, so the code has
// to be translated rather than passed on: everything downstream is keyed on
// the descriptor character.
func TestModulePrimitiveArrayTypeCodesAreTranslated(t *testing.T) {
	for code, want := range map[uint32]byte{0x10: 'Z', 0x14: 'C', 0x18: 'F', 0x1c: 'D', 0x20: 'B', 0x24: 'S', 0x28: 'I', 0x2c: 'J'} {
		if got := modulePrimitiveTypes[code/4]; got != want {
			t.Errorf("type code %#x = %q, want %q", code, got, want)
		}
	}
	// The four reserved words in front of the characters, and anything past
	// them, name nothing. A code that is not a whole number of words is
	// refused by the caller rather than by the table.
	for _, code := range []uint32{0, 0x0c, 0x30} {
		index := code / 4
		if index < uint32(len(modulePrimitiveTypes)) && modulePrimitiveTypes[index] != 0 {
			t.Errorf("type code %#x answers %q and should not", code, modulePrimitiveTypes[index])
		}
	}
}

// The restore routine is the one piece of guest code this platform writes for
// a module, and a catch block resumes through it. It reads the register block
// in the module's own order and answers with the label, so the two ends of it
// are what the test can hold: the block offsets it loads, and the register the
// label comes back in.
func TestModuleRestoreStubReadsTheModuleRegisterOrder(t *testing.T) {
	code := moduleRestoreStub()
	if len(code)%2 != 0 {
		t.Fatalf("restore routine is %d bytes, want an even count", len(code))
	}
	halfwords := make([]uint16, len(code)/2)
	for index := range halfwords {
		halfwords[index] = binary.LittleEndian.Uint16(code[index*2:])
	}
	// `ldr r2, [r0, #imm]` is 0x6800 | imm5<<6 | rn<<3 | rt, and the offsets
	// it names are the module's: the stack pointer first, then the link
	// register, then r10, r8 and r9.
	var offsets []uint32
	for _, halfword := range halfwords {
		if halfword&0xf83f == 0x6802 {
			offsets = append(offsets, uint32((halfword>>6)&0x1f)*4)
		}
	}
	want := []uint32{0, 4, 36, 24, 28}
	if len(offsets) != len(want) {
		t.Fatalf("restore routine reads %v, want %v", offsets, want)
	}
	for index := range want {
		if offsets[index] != want[index] {
			t.Fatalf("restore routine reads %v, want %v", offsets, want)
		}
	}
	if halfwords[len(halfwords)-2] != 0x4660 {
		t.Errorf("restore routine ends by answering %#x, want the label in r0", halfwords[len(halfwords)-2])
	}
	if last := halfwords[len(halfwords)-1]; last != 0x4770 {
		t.Errorf("restore routine leaves through %#x, want the saved link register", last)
	}
}

// A module's handler record keeps the label and the caught object the other
// way round from the current generation's, and its saved stack pointer is the
// first word of the register block rather than the tenth.
func TestModuleHandlerRecordOffsetsDifferFromTheCurrentGeneration(t *testing.T) {
	client := loadSyntheticModule(t)
	if _, err := client.ExecuteModuleEntry(context.Background()); err != nil {
		t.Fatal(err)
	}
	runtime := client.runtime
	if runtime.exceptionLabel() != javaExceptionObject || runtime.exceptionObject() != javaExceptionCurrentPC {
		t.Errorf("label at %d and object at %d, want them swapped", runtime.exceptionLabel(), runtime.exceptionObject())
	}
	if runtime.exceptionFrameStack() != javaExceptionContext {
		t.Errorf("saved stack pointer at %d, want %d", runtime.exceptionFrameStack(), javaExceptionContext)
	}
	if head := runtime.exceptionHead(); head != runtime.moduleContext+moduleContextHandlers {
		t.Errorf("handler head at %#x, want %#x", head, runtime.moduleContext+moduleContextHandlers)
	}
}

// The equal case is what a module's records make common, and answering it the
// way the current generation does resumes a catch block inside the call that
// threw rather than inside the one that catches.
func TestAModuleUnwindDoesNotOwnTheCallItEntersOn(t *testing.T) {
	const frame = 0x200ffdb4
	strict := &aotExceptionUnwind{framePointer: frame, strict: true}
	relaxed := &aotExceptionUnwind{framePointer: frame}
	if strict.resumableFrom(frame) {
		t.Error("a module unwind claimed the call that entered on its own frame")
	}
	if !relaxed.resumableFrom(frame) {
		t.Error("the current generation's rule changed")
	}
	if !strict.resumableFrom(frame + 4) {
		t.Error("a module unwind refused the call above its frame")
	}
}

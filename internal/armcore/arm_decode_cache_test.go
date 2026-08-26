package armcore

import (
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"
)

// runARM executes count ARM instructions from address and answers the context.
func runARM(t *testing.T, memory *Memory, address uint32, count uint32) Context {
	t.Helper()
	context := NewContext()
	if err := context.SetPC(address); err != nil {
		t.Fatal(err)
	}
	if _, err := (Engine{}).Run(&context, memory, 0xffffffff, count); err != nil {
		t.Fatal(err)
	}
	return context
}

func placeARM(t *testing.T, memory *Memory, address uint32, instruction uint32) {
	t.Helper()
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, instruction)
	if err := memory.Load(address, word); err != nil {
		t.Fatal(err)
	}
}

// TestARMDecodeCacheFollowsRewrittenCode is the Thumb hazard on the ARM side:
// a module that copies code into RAM and runs it, then copies different code
// over the same address, must not run the instruction that used to be there.
func TestARMDecodeCacheFollowsRewrittenCode(t *testing.T) {
	const base = uint32(0x11000)
	memory := NewMemory()
	if err := memory.Map(base, memoryPageSize*2, PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	placeARM(t, memory, base, 0xe2800005) // add r0, r0, #5
	if got := runARM(t, memory, base, 1).Registers[0]; got != 5 {
		t.Fatalf("r0 after the original instruction = %d, want 5", got)
	}
	if page := memory.pageFor(base); page.decodedARM == nil {
		t.Fatal("a wholly executable page was not cached")
	}

	// A Host write replaces it.
	word := make([]byte, 4)
	binary.LittleEndian.PutUint32(word, 0xe2800009) // add r0, r0, #9
	if err := memory.Write(base, word); err != nil {
		t.Fatal(err)
	}
	if got := runARM(t, memory, base, 1).Registers[0]; got != 9 {
		t.Fatalf("r0 after the Host rewrote the instruction = %d, want 9", got)
	}

	// And a guest store does too. The replacement is a different *form*, not
	// just a different immediate, because the cache holds the form: a store
	// where a data-processing instruction used to be is what a stale entry
	// would get wrong most visibly.
	binary.LittleEndian.PutUint32(word, 0xea000000) // b .+8
	memory.beginQuantum()
	if err := memory.writeGuest(base, word); err != nil {
		memory.endQuantum()
		t.Fatal(err)
	}
	memory.endQuantum()
	context := runARM(t, memory, base, 1)
	if got := context.PC(); got != base+8 {
		t.Fatalf("PC after a guest store rewrote the instruction = %#x, want %#x", got, base+8)
	}
}

// TestARMDecodeCacheRefusesPagesItCannotCoverWholly is the ARM half of the
// shortcut a cached page takes: it answers later addresses without re-checking
// permission, so a page a mapping only partly covers must not be cached.
func TestARMDecodeCacheRefusesPagesItCannotCoverWholly(t *testing.T) {
	const base = uint32(0x21000)
	memory := NewMemory()
	if err := memory.Map(base, memoryPageSize/2, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	placeARM(t, memory, base, 0xe2800005)
	if got := runARM(t, memory, base, 1).Registers[0]; got != 5 {
		t.Fatalf("r0 inside the mapped half = %d, want 5", got)
	}
	if page := memory.pageFor(base); page.decodedARM != nil {
		t.Fatal("a page the mapping only half covers was cached")
	}

	memory.beginQuantum()
	_, _, err := memory.decodeARM(base + uint32(memoryPageSize)/2)
	memory.endQuantum()
	if !errors.Is(err, ErrUnmapped) && !errors.Is(err, ErrPermission) {
		t.Fatalf("decoding the unmapped tail of a partly covered page = %v, want a fault", err)
	}
}

// TestARMDecodeCacheRetiresOnRemap covers the other way a cached answer goes
// stale: the page is unchanged but what it is allowed to do is not.
func TestARMDecodeCacheRetiresOnRemap(t *testing.T) {
	const base = uint32(0x31000)
	memory := NewMemory()
	if err := memory.Map(base, memoryPageSize, PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	placeARM(t, memory, base, 0xe2800005)
	runARM(t, memory, base, 1)
	if page := memory.pageFor(base); page.decodedARM == nil {
		t.Fatal("a wholly executable page was not cached")
	}
	if err := memory.Map(base+uint32(memoryPageSize), memoryPageSize, PermissionRead); err != nil {
		t.Fatal(err)
	}
	if page := memory.pageFor(base); page.decodedARM != nil {
		t.Fatal("a remap left decoded entries behind")
	}
}

// TestARMDecodeCacheCanBeTurnedOff covers the diagnostic switch on the ARM
// side: with the cache off the interpreter runs the same code, faults the same
// way, and caches nothing.
func TestARMDecodeCacheCanBeTurnedOff(t *testing.T) {
	SetDecodeCacheEnabled(false)
	t.Cleanup(func() { SetDecodeCacheEnabled(true) })

	const base = uint32(0x51000)
	memory := NewMemory()
	if err := memory.Map(base, memoryPageSize, PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	placeARM(t, memory, base, 0xe2800005)
	if got := runARM(t, memory, base, 1).Registers[0]; got != 5 {
		t.Fatalf("r0 with the cache off = %d, want 5", got)
	}
	if page := memory.pageFor(base); page.decodedARM != nil {
		t.Fatal("a page was cached with the cache off")
	}

	const unreadable = uint32(0x61000)
	if err := memory.Map(unreadable, memoryPageSize, PermissionRead); err != nil {
		t.Fatal(err)
	}
	memory.beginQuantum()
	_, _, err := memory.decodeARM(unreadable)
	memory.endQuantum()
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("decoding a non-executable page with the cache off = %v, want ErrPermission", err)
	}
}

// TestARMUndefinedIsNotCached guards the one entry the cache deliberately does
// not keep. An encoding this engine does not implement has to fault every time
// it is reached, so its slot stays undecoded — and a fault that only happened
// once would look like an engine that quietly learned to execute it.
func TestARMUndefinedIsNotCached(t *testing.T) {
	const base = uint32(0x71000)
	memory := NewMemory()
	if err := memory.Map(base, memoryPageSize, PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	// A coprocessor data operation on CP10, which this engine has no answer
	// for, followed by an instruction it does.
	placeARM(t, memory, base, 0xee000a10)
	placeARM(t, memory, base+4, 0xe2800005)

	for attempt := 0; attempt < 2; attempt++ {
		context := NewContext()
		if err := context.SetPC(base); err != nil {
			t.Fatal(err)
		}
		_, err := (Engine{}).Run(&context, memory, 0xffffffff, 4)
		if !errors.Is(err, ErrUndefinedInstruction) {
			t.Fatalf("attempt %d: running an undefined encoding = %v, want ErrUndefinedInstruction", attempt, err)
		}
	}
	if page := memory.pageFor(base); page.decodedARM != nil && page.decodedARM[0] != armUndecoded {
		t.Fatalf("an undefined encoding was cached as form %d", page.decodedARM[0])
	}
}

// TestARMCachedAndUncachedAgree runs the same encodings both ways round. The
// cached path reaches a handler through a form the classifier stored earlier
// and the uncached one classifies on the spot, so a classifier that disagreed
// with the match chain it replaced would show up here rather than as a title
// that misbehaves.
func TestARMCachedAndUncachedAgree(t *testing.T) {
	encodings := []uint32{
		0xe2800005, // add r0, r0, #5
		0xe0800001, // add r0, r0, r1
		0xe3a02007, // mov r2, #7
		0xe5801000, // str r1, [r0]
		0xe5901000, // ldr r1, [r0]
		0xe1c010b0, // strh r1, [r0]
		0xe1d010b0, // ldrh r1, [r0]
		0xe0010291, // mul r1, r1, r2
		0xe0834192, // umull r4, r3, r2, r1
		0xe10f0000, // mrs r0, cpsr
		0xe1001091, // swp r1, r1, [r0]
		0xe8800006, // stm r0, {r1, r2}
		0xe8900006, // ldm r0, {r1, r2}
		0xea000000, // b .+8
		0xeb000000, // bl .+8
		0xe12fff1e, // bx lr
		0x0a000000, // beq .+8, whose condition fails
		0xee071f9a, // mcr p15, 0, r1, c7, c10, 4
	}
	const code = uint32(0x81000)
	const data = uint32(0x82000)

	build := func() *Memory {
		memory := NewMemory()
		if err := memory.Map(code, memoryPageSize, PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
		if err := memory.Map(data, memoryPageSize, PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		return memory
	}
	execute := func(memory *Memory, encoding uint32, warm bool) (Context, error) {
		placeARM(t, memory, code, encoding)
		if warm {
			// Classify it without executing it, so the run below is the one
			// the cache answers. Executing it twice instead would let the
			// first execution's stores decide the second one's result.
			memory.beginQuantum()
			if _, _, err := memory.decodeARM(code); err != nil {
				memory.endQuantum()
				t.Fatalf("%#08x warming: %v", encoding, err)
			}
			memory.endQuantum()
		}
		context := NewContext()
		if err := context.SetPC(code); err != nil {
			t.Fatal(err)
		}
		context.Registers[0] = data
		context.Registers[1] = 3
		context.Registers[2] = 11
		context.Registers[RegisterLR] = code + 0x40
		_, err := (Engine{}).Run(&context, memory, 0xffffffff, 1)
		return context, err
	}

	for _, encoding := range encodings {
		SetDecodeCacheEnabled(true)
		cachedMemory := build()
		cached, cachedErr := execute(cachedMemory, encoding, true)
		if page := cachedMemory.pageFor(code); page.decodedARM == nil || page.decodedARM[0] == armUndecoded {
			t.Fatalf("%#08x was not cached, so this compares one path with itself", encoding)
		}

		SetDecodeCacheEnabled(false)
		uncachedMemory := build()
		uncached, uncachedErr := execute(uncachedMemory, encoding, false)
		SetDecodeCacheEnabled(true)

		if (cachedErr == nil) != (uncachedErr == nil) {
			t.Fatalf("%#08x: cached error %v, uncached error %v", encoding, cachedErr, uncachedErr)
		}
		if cached.Registers != uncached.Registers || cached.CPSR != uncached.CPSR {
			t.Fatalf("%#08x: cached %v/%#x, uncached %v/%#x",
				encoding, cached.Registers, cached.CPSR, uncached.Registers, uncached.CPSR)
		}
	}
}

// TestARMDecodedEntryStaysOneByte holds the footprint the entry was chosen
// for. It is a quarter of the page it describes; widening it to carry the
// encoding — which the page already holds, in the order the host wants it —
// would multiply the cost of every code page a title ever executes from.
func TestARMDecodedEntryStaysOneByte(t *testing.T) {
	if size := unsafe.Sizeof(armForm(0)); size != 1 {
		t.Fatalf("armForm is %d bytes, want 1", size)
	}
	if size := unsafe.Sizeof([armDecodedPerPage]armForm{}); size != uintptr(memoryPageSize)/4 {
		t.Fatalf("an ARM decode table is %d bytes for a %d-byte page", size, memoryPageSize)
	}
}

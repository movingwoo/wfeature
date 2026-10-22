package ktf

import (
	"bytes"
	"fmt"
	"math"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// The bottom of the platform table is a C string and memory library, and the
// module leans on it hard: it copies with slot 0 and clears with slot 1 rather
// than carrying its own. Each one was read off its call sites —
//
//	slot 1 (dst, 0, 0x1b) then slot 0 (dst, src, 0xa)  a clear then a copy
//	slot 5 (text) compared against 14                  a length
//	slot 8 (buffer, "…%d…", halfword, halfword)        a rendered string
//
// — which is also why the numbering being unlike the descriptor package's
// kernel table does not matter here: what a slot does is decided by what its
// callers do with it. See docs/ktf.md.
const (
	nativeSlotMemoryCopy = 0x00
	nativeSlotMemorySet  = 0x04
	nativeSlotStringCopy = 0x08
	nativeSlotStringJoin = 0x0c
	// nativeSlotStringOrder is BREW's strcmp. A later title looks its
	// resources up by name with it, and a trap's zero matched the first
	// entry of every directory: the size it then computed was negative.
	nativeSlotStringOrder = 0x10
	nativeSlotStringSize  = 0x14
	nativeSlotFormat      = 0x20
	// nativeSlotDecimal is BREW's atoi. A later title reads a count out of
	// a one-digit text field with it and sizes a table by the answer.
	nativeSlotDecimal = 0x90
	// nativeSlotFloatOperation and nativeSlotFloatCompare are BREW's f_op and
	// f_cmp, the double arithmetic and comparisons its FADD, FSUB, FMUL, FDIV,
	// FPOW and FCMP_* macros call. A later title computes with them every
	// frame of a race, tens of thousands of calls, and a trap's zero made
	// every result nothing.
	nativeSlotFloatOperation = 0x94
	nativeSlotFloatCompare   = 0x98
	// nativeSlotBoundedCopy is a second copy, and the module reaches it from
	// one place: it measures a string with the length slot, allocates that many
	// bytes and one more, and copies the string's own length into the block —
	// which memcpy, memmove and strncpy all do the same way, so which of them
	// the carrier put here cannot be told from this call and does not matter to
	// it. Everything the title draws as text goes through that block, so a
	// platform that leaves this a trap draws its dialogs empty.
	nativeSlotBoundedCopy = 0xc8
	// nativeSlotCompare compares two runs of bytes and answers zero when they
	// are the same. Its two call sites are what name it: the module reads the
	// first bytes of a sound resource and compares four of them against the
	// literals "MMMD" and "cmid", which are the two formats it can play.
	nativeSlotCompare = 0xcc
	// nativeSlotMemoryOrder is BREW's memcmp, where 0xcc is its strncmp. A
	// later title searches a sound it has loaded for a run of bytes with it,
	// and a trap's zero found the run at the first place it looked. The 2005
	// module reaches the slot too and keeps the zero it ran with (laterOnly).
	nativeSlotMemoryOrder = 0xdc
	// nativeSlotWiden is BREW's STREXPAND, which turns a byte string into the
	// wide one the display draws. A later title builds the text of its
	// message box with it, and a trap left the box empty.
	nativeSlotWiden = 0xe4
)

// nativeMaxString bounds every string this library reads out of guest memory,
// which is what keeps a missing terminator a refusal rather than a scan of the
// address space.
const nativeMaxString = 64 << 10

// nativeMaxTransfer bounds one copy or fill. It is the platform's allocation
// limit rather than the string limit: a title that copies a decoded asset
// around moves more than a name, and a bound that stopped it would report a
// copy where the fault is a size the title computed.
const nativeMaxTransfer = maxPlatformAllocation

// installLibrary registers the platform table's C library.
func (platform *NativePlatform) installLibrary() {
	client := platform.client
	client.Serve(NativePlatformTable, nativeSlotMemoryCopy, platform.memoryCopy)
	client.Serve(NativePlatformTable, nativeSlotMemorySet, platform.memorySet)
	client.Serve(NativePlatformTable, nativeSlotStringCopy, platform.stringCopy)
	client.Serve(NativePlatformTable, nativeSlotStringJoin, platform.stringJoin)
	client.Serve(NativePlatformTable, nativeSlotStringOrder, platform.laterOnly(platform.stringOrder))
	client.Serve(NativePlatformTable, nativeSlotStringSize, platform.stringSize)
	client.Serve(NativePlatformTable, nativeSlotFormat, platform.format)
	client.Serve(NativePlatformTable, nativeSlotDecimal, platform.laterOnly(platform.decimal))
	client.Serve(NativePlatformTable, nativeSlotFloatOperation, platform.laterOnly(platform.floatOperation))
	client.Serve(NativePlatformTable, nativeSlotFloatCompare, platform.laterOnly(platform.floatCompare))
	client.Serve(NativePlatformTable, nativeSlotBoundedCopy, platform.memoryCopy)
	client.Serve(NativePlatformTable, nativeSlotCompare, platform.memoryCompare)
	client.Serve(NativePlatformTable, nativeSlotMemoryOrder, platform.laterOnly(platform.memoryCompare))
	client.Serve(NativePlatformTable, nativeSlotWiden, platform.laterOnly(platform.widen))
}

// laterOnly answers a helper slot for a later module, and the 2005 module with
// the zero a trap gave it. The helpers served here for later titles are ones
// the 2005 module has always run without — one of them, `0xdc`, it is known
// to reach — and it is not in the local corpus, so what a real answer would
// change for it cannot be measured: a comparison that has always come out
// equal could, for one, decide differently about a save it wrote. So it keeps
// the answers it ran with.
func (platform *NativePlatform) laterOnly(handler NativeSlotHandler) NativeSlotHandler {
	return func(thread *armcore.Thread) (uint32, error) {
		if !platform.archive.AsksForInterfaceVersion() {
			return 0, nil
		}
		return handler(thread)
	}
}

// nativeArguments reads the first four arguments of a trapped call.
func nativeArguments(thread *armcore.Thread, count int) ([]uint32, error) {
	arguments := make([]uint32, count)
	for index := range arguments {
		value, err := thread.Register(index)
		if err != nil {
			return nil, err
		}
		arguments[index] = value
	}
	return arguments, nil
}

// readString reads a terminated string out of guest memory.
func (platform *NativePlatform) readString(address uint32) ([]byte, error) {
	if address == 0 {
		return nil, fmt.Errorf("KTF native library call on a null string")
	}
	text, terminated, err := readGuestString(platform.client.core.Memory(), address, nativeMaxString, 1)
	if err != nil {
		return nil, fmt.Errorf("read KTF native string at %#x: %w", address, err)
	}
	if !terminated {
		return nil, fmt.Errorf("KTF native string at %#x is not terminated within %d bytes", address, nativeMaxString)
	}
	return text, nil
}

// memoryCompare answers the platform table's comparison. The module only ever
// asks whether the answer is zero, but the sign is what the library it stands
// in for returns, so it is what this returns.
func (platform *NativePlatform) memoryCompare(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 3)
	if err != nil {
		return 0, err
	}
	first, second, length := arguments[0], arguments[1], arguments[2]
	if length == 0 {
		return 0, nil
	}
	if uint64(length) > nativeMaxTransfer {
		return 0, fmt.Errorf("KTF native comparison of %d bytes at %#x", length, first)
	}
	memory := platform.client.core.Memory()
	left, right := make([]byte, length), make([]byte, length)
	if err := memory.Read(first, left); err != nil {
		return 0, fmt.Errorf("read %d bytes at %#x: %w", length, first, err)
	}
	if err := memory.Read(second, right); err != nil {
		return 0, fmt.Errorf("read %d bytes at %#x: %w", length, second, err)
	}
	for index := range left {
		if left[index] == right[index] {
			continue
		}
		if left[index] < right[index] {
			return ^uint32(0), nil
		}
		return 1, nil
	}
	return 0, nil
}

func (platform *NativePlatform) memoryCopy(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 3)
	if err != nil {
		return 0, err
	}
	destination, source, length := arguments[0], arguments[1], arguments[2]
	if length == 0 {
		return destination, nil
	}
	if uint64(length) > nativeMaxTransfer {
		return 0, fmt.Errorf("KTF native copy of %d bytes from %#x", length, source)
	}
	data := make([]byte, length)
	memory := platform.client.core.Memory()
	if err := memory.Read(source, data); err != nil {
		return 0, fmt.Errorf("read %d bytes at %#x: %w", length, source, err)
	}
	if err := memory.Write(destination, data); err != nil {
		return 0, fmt.Errorf("write %d bytes at %#x: %w", length, destination, err)
	}
	return destination, nil
}

func (platform *NativePlatform) memorySet(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 3)
	if err != nil {
		return 0, err
	}
	destination, value, length := arguments[0], arguments[1], arguments[2]
	if length == 0 {
		return destination, nil
	}
	if uint64(length) > nativeMaxTransfer {
		return 0, fmt.Errorf("KTF native fill of %d bytes at %#x", length, destination)
	}
	data := bytes.Repeat([]byte{byte(value)}, int(length))
	if err := platform.client.core.Memory().Write(destination, data); err != nil {
		return 0, fmt.Errorf("fill %d bytes at %#x: %w", length, destination, err)
	}
	return destination, nil
}

func (platform *NativePlatform) stringCopy(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 2)
	if err != nil {
		return 0, err
	}
	text, err := platform.readString(arguments[1])
	if err != nil {
		return 0, err
	}
	if err := platform.client.core.Memory().Write(arguments[0], append(text, 0)); err != nil {
		return 0, fmt.Errorf("write %d string bytes at %#x: %w", len(text), arguments[0], err)
	}
	return arguments[0], nil
}

func (platform *NativePlatform) stringJoin(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 2)
	if err != nil {
		return 0, err
	}
	head, err := platform.readString(arguments[0])
	if err != nil {
		return 0, err
	}
	tail, err := platform.readString(arguments[1])
	if err != nil {
		return 0, err
	}
	if err := platform.client.core.Memory().Write(arguments[0]+uint32(len(head)), append(tail, 0)); err != nil {
		return 0, fmt.Errorf("join %d string bytes at %#x: %w", len(tail), arguments[0], err)
	}
	return arguments[0], nil
}

// stringOrder compares two terminated strings byte by byte, unsigned, and
// answers the sign of the first difference.
func (platform *NativePlatform) stringOrder(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 2)
	if err != nil {
		return 0, err
	}
	left, err := platform.readString(arguments[0])
	if err != nil {
		return 0, err
	}
	right, err := platform.readString(arguments[1])
	if err != nil {
		return 0, err
	}
	return uint32(int32(bytes.Compare(left, right))), nil
}

// decimal reads a signed decimal number the way C's atoi does: leading white
// space, one optional sign, then digits up to the first byte that is not one.
// The value wraps as the 32-bit int it is returned in.
func (platform *NativePlatform) decimal(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	text, err := platform.readString(address)
	if err != nil {
		return 0, err
	}
	index := 0
	for index < len(text) && (text[index] == ' ' || text[index] >= '\t' && text[index] <= '\r') {
		index++
	}
	negative := false
	if index < len(text) && (text[index] == '+' || text[index] == '-') {
		negative = text[index] == '-'
		index++
	}
	value := uint32(0)
	for ; index < len(text) && text[index] >= '0' && text[index] <= '9'; index++ {
		value = value*10 + uint32(text[index]-'0')
	}
	if negative {
		value = -value
	}
	return value, nil
}

// The operation numbers f_op and f_cmp take, as BREW's header numbers them.
const (
	nativeFloatAdd          = 0
	nativeFloatSubtract     = 1
	nativeFloatMultiply     = 2
	nativeFloatDivide       = 3
	nativeFloatLess         = 4
	nativeFloatLessEqual    = 5
	nativeFloatEqual        = 6
	nativeFloatGreater      = 7
	nativeFloatGreaterEqual = 8
	nativeFloatPower        = 9
)

// floatOperands reads the two doubles an f_op or f_cmp call takes, each in two
// registers with its low word first, and the operation after them on the
// stack.
func (platform *NativePlatform) floatOperands(thread *armcore.Thread) (left, right float64, operation uint32, err error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, 0, 0, err
	}
	stacked, err := platform.varargs(thread, 4)(1)
	if err != nil {
		return 0, 0, 0, err
	}
	left = math.Float64frombits(uint64(arguments[1])<<32 | uint64(arguments[0]))
	right = math.Float64frombits(uint64(arguments[3])<<32 | uint64(arguments[2]))
	return left, right, uint32(stacked), nil
}

// floatOperation answers f_op with a double in r0 and r1, low word first. An
// operation it does not name answers zero.
func (platform *NativePlatform) floatOperation(thread *armcore.Thread) (uint32, error) {
	left, right, operation, err := platform.floatOperands(thread)
	if err != nil {
		return 0, err
	}
	result := 0.0
	switch operation {
	case nativeFloatAdd:
		result = left + right
	case nativeFloatSubtract:
		result = left - right
	case nativeFloatMultiply:
		result = left * right
	case nativeFloatDivide:
		result = left / right
	case nativeFloatPower:
		result = math.Pow(left, right)
	}
	bits := math.Float64bits(result)
	if err := thread.SetRegister(1, uint32(bits>>32)); err != nil {
		return 0, err
	}
	return uint32(bits), nil
}

// floatCompare answers f_cmp: 1 when the comparison it names holds, else 0.
func (platform *NativePlatform) floatCompare(thread *armcore.Thread) (uint32, error) {
	left, right, operation, err := platform.floatOperands(thread)
	if err != nil {
		return 0, err
	}
	holds := false
	switch operation {
	case nativeFloatLess:
		holds = left < right
	case nativeFloatLessEqual:
		holds = left <= right
	case nativeFloatEqual:
		holds = left == right
	case nativeFloatGreater:
		holds = left > right
	case nativeFloatGreaterEqual:
		holds = left >= right
	}
	if holds {
		return 1, nil
	}
	return 0, nil
}

func (platform *NativePlatform) stringSize(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	text, err := platform.readString(address)
	if err != nil {
		return 0, err
	}
	return uint32(len(text)), nil
}

// widen answers STREXPAND(source, count, destination, size). Each character of
// up to count bytes of source, as far as its terminator, becomes one halfword:
// a single-byte character widened with a zero, and the two bytes of an EUC-KR
// character copied side by side, which is the halfword the display reads as
// that character (native_text.go). It writes no further than size bytes, and
// ends what it wrote with a zero halfword when there is room for one.
func (platform *NativePlatform) widen(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 4)
	if err != nil {
		return 0, err
	}
	source, count, destination, size := arguments[0], int32(arguments[1]), arguments[2], int32(arguments[3])
	if destination == 0 || size < 2 {
		return 0, nil
	}
	var text []byte
	if count > 0 {
		if source == 0 {
			return 0, fmt.Errorf("KTF native library call on a null string")
		}
		memory := platform.client.core.Memory()
		if text, _, err = readGuestString(memory, source, uint32(min(count, nativeMaxString)), 1); err != nil {
			return 0, fmt.Errorf("read KTF native string at %#x: %w", source, err)
		}
	}
	room := min(int(size), nativeMaxString*2) &^ 1
	wide := make([]byte, 0, min(room, 2*len(text)+2))
	for index := 0; index < len(text) && len(wide)+2 <= room; index++ {
		character := text[index]
		if character < 0x80 || index+1 == len(text) {
			wide = append(wide, character, 0)
			continue
		}
		index++
		wide = append(wide, character, text[index])
	}
	if len(wide)+2 <= room {
		wide = append(wide, 0, 0)
	}
	if err := platform.client.core.Memory().Write(destination, wide); err != nil {
		return 0, fmt.Errorf("write %d bytes of wide text at %#x: %w", len(wide), destination, err)
	}
	return 0, nil
}

// format renders into a caller-supplied buffer. The variadic arguments follow
// the buffer and the format in the ARM procedure call standard's own order —
// two registers, then two more, then the stack — which is what the module's
// call sites pass.
func (platform *NativePlatform) format(thread *armcore.Thread) (uint32, error) {
	destination, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	formatAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	template, err := platform.readString(formatAddress)
	if err != nil {
		return 0, err
	}
	rendered, err := wipic.Format(template, platform.varargs(thread, 2), func(address uint32, limit int) ([]byte, error) {
		text, err := platform.readString(address)
		if err != nil {
			return nil, err
		}
		if limit >= 0 && limit < len(text) {
			text = text[:limit]
		}
		return text, nil
	})
	if err != nil {
		return 0, fmt.Errorf("format KTF native %q: %w", template, err)
	}
	if err := platform.client.core.Memory().Write(destination, append(rendered, 0)); err != nil {
		return 0, fmt.Errorf("write KTF native formatted output at %#x: %w", destination, err)
	}
	return uint32(len(rendered)), nil
}

// varargs walks the arguments after the fixed ones: r0 to r3 and then the
// caller's stack, pairing words for the long-long modifiers.
func (platform *NativePlatform) varargs(thread *armcore.Thread, first int) func(words int) (uint64, error) {
	index := first
	read := func() (uint32, error) {
		defer func() { index++ }()
		if index < 4 {
			return thread.Register(index)
		}
		stack, err := thread.Register(armcore.RegisterSP)
		if err != nil {
			return 0, err
		}
		word := make([]byte, 4)
		address := stack + uint32(index-4)*4
		if err := platform.client.core.Memory().Read(address, word); err != nil {
			return 0, fmt.Errorf("read KTF native stacked argument at %#x: %w", address, err)
		}
		return uint32(word[0]) | uint32(word[1])<<8 | uint32(word[2])<<16 | uint32(word[3])<<24, nil
	}
	return func(words int) (uint64, error) {
		low, err := read()
		if err != nil {
			return 0, err
		}
		if words == 1 {
			return uint64(low), nil
		}
		high, err := read()
		if err != nil {
			return 0, err
		}
		return uint64(high)<<32 | uint64(low), nil
	}
}

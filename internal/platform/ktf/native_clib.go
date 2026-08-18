package ktf

import (
	"bytes"
	"fmt"

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
	nativeSlotStringSize = 0x14
	nativeSlotFormat     = 0x20
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
	client.Serve(NativePlatformTable, nativeSlotStringSize, platform.stringSize)
	client.Serve(NativePlatformTable, nativeSlotFormat, platform.format)
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
	text := make([]byte, 0, 64)
	buffer := make([]byte, 1)
	for offset := uint32(0); offset < nativeMaxString; offset++ {
		if err := platform.client.core.Memory().Read(address+offset, buffer); err != nil {
			return nil, fmt.Errorf("read KTF native string at %#x: %w", address, err)
		}
		if buffer[0] == 0 {
			return text, nil
		}
		text = append(text, buffer[0])
	}
	return nil, fmt.Errorf("KTF native string at %#x is not terminated within %d bytes", address, nativeMaxString)
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

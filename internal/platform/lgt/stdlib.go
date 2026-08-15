package lgt

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// LGT provides the C standard library through an import table instead of
// linking it into the module, which is the other half of what makes an LGT
// binary smaller than a KTF one. The slots follow the same section numbering
// as the WIPI blocks: 0x3e8 is the C library's base.
const (
	// stdio.h comes before stdlib.h in the C library's own sectioning, and
	// sprintf is where a title formats the path of the resource it is about to
	// open. It takes the same arguments as MC_knlSprintk and is served by the
	// same formatter; the two exist separately because the kernel's copy is a
	// WIPI call and this one is the C library the module did not link in.
	stdlibSprintf uint32 = 0x3f7
	stdlibAtoi    uint32 = 0x3fb
	stdlibStrcpy  uint32 = 0x405
	stdlibStrncpy uint32 = 0x406
	stdlibStrcat  uint32 = 0x407
	stdlibStrcmp  uint32 = 0x409
	stdlibStrncmp uint32 = 0x40a

	// The rest of the specification's string.h list, in its order: strncat
	// sits between strcat and strcmp, and the other five between strncmp and
	// strstr. Only strchr has a caller — a title splitting a message on
	// newlines — and the others are fixed by their neighbours rather than
	// guessed, because the slots on both sides of each gap are ones a title
	// did call. See "The rest of string.h, placed by its neighbours" in
	// docs/lgt.md.
	stdlibStrncat uint32 = 0x408
	stdlibStrchr  uint32 = 0x40b
	stdlibStrrchr uint32 = 0x40c
	stdlibStrspn  uint32 = 0x40d
	stdlibStrcspn uint32 = 0x40e
	stdlibStrpbrk uint32 = 0x40f

	stdlibStrstr    uint32 = 0x410
	stdlibStrlen    uint32 = 0x411
	stdlibMemcpy    uint32 = 0x414
	stdlibMemmove   uint32 = 0x415
	stdlibMemcmp    uint32 = 0x416
	stdlibMemchr    uint32 = 0x417
	stdlibMemset    uint32 = 0x418
	stdlibTime      uint32 = 0x41a
	stdlibLocaltime uint32 = 0x420

	// stdlibRunFunction takes one function pointer into the module and runs it.
	// No name for it was found: the specification's list of supported ANSI-C
	// functions has nothing that runs a callback, and it sits past the end of
	// that list in this table. It is named for what it was watched doing.
	//
	// It reads like `atexit`, and it is not: see "The slot that runs a function"
	// in docs/lgt.md for the A/B that settled it.
	stdlibRunFunction uint32 = 0x424
)

// maxStdlibLength bounds one string or block operation, because a length that
// came from uninitialized guest memory would otherwise walk the address space.
const maxStdlibLength = 1 << 24

func (client *Client) handleStdlibSVC(ctx context.Context, thread *armcore.Thread, slot uint32) error {
	memory := client.core.Memory()
	argument := func(index int) (uint32, error) { return thread.Register(index) }
	answer := func(value uint32) error { return thread.SetRegister(0, value) }

	switch slot {
	case stdlibSprintf:
		written, err := client.wipicSprintk(thread)
		if err != nil {
			return err
		}
		return answer(written)

	case stdlibStrlen:
		pointer, err := argument(0)
		if err != nil {
			return err
		}
		text, err := client.readCString(pointer)
		if err != nil {
			return err
		}
		return answer(uint32(len(text)))

	case stdlibStrcpy, stdlibStrncpy, stdlibStrcat, stdlibStrncat:
		return client.stringWrite(thread, slot)

	case stdlibStrchr, stdlibStrrchr:
		pointer, err := argument(0)
		if err != nil {
			return err
		}
		value, err := argument(1)
		if err != nil {
			return err
		}
		text, err := client.readCString(pointer)
		if err != nil {
			return err
		}
		// The terminator is part of the string these two search, so asking for
		// NUL answers the end of it rather than nothing.
		symbol := byte(value)
		haystack := append([]byte(text), 0)
		index := -1
		for at := range haystack {
			if haystack[at] != symbol {
				continue
			}
			index = at
			if slot == stdlibStrchr {
				break
			}
		}
		if index < 0 {
			return answer(0)
		}
		return answer(pointer + uint32(index))

	case stdlibStrspn, stdlibStrcspn, stdlibStrpbrk:
		pointer, err := argument(0)
		if err != nil {
			return err
		}
		set, err := argument(1)
		if err != nil {
			return err
		}
		text, err := client.readCString(pointer)
		if err != nil {
			return err
		}
		listed, err := client.readCString(set)
		if err != nil {
			return err
		}
		// Byte-wise, not rune-wise: a set holding one byte of a multi-byte
		// character is a set the C library would still match byte by byte, and
		// a title's separators arrive as bytes.
		inSet := [256]bool{}
		for _, symbol := range []byte(listed) {
			inSet[symbol] = true
		}
		span := uint32(0)
		body := []byte(text)
		for span < uint32(len(body)) && inSet[body[span]] == (slot == stdlibStrspn) {
			span++
		}
		if slot == stdlibStrpbrk {
			// strcspn's span ends at the first listed byte, which is the one
			// strpbrk points at — unless it ran to the terminator.
			if span == uint32(len(body)) {
				return answer(0)
			}
			return answer(pointer + span)
		}
		return answer(span)

	case stdlibStrcmp, stdlibStrncmp:
		left, err := argument(0)
		if err != nil {
			return err
		}
		right, err := argument(1)
		if err != nil {
			return err
		}
		if slot == stdlibStrncmp {
			length, lengthErr := argument(2)
			if lengthErr != nil {
				return lengthErr
			}
			if length > maxStdlibLength {
				return fmt.Errorf("LGT strncmp length %d exceeds the limit", length)
			}
			// Counted, so neither side has to be terminated within it.
			leftText := string(client.readBoundedString(left, length))
			rightText := string(client.readBoundedString(right, length))
			return answer(uint32(int32(strings.Compare(leftText, rightText))))
		}
		leftText, err := client.readCString(left)
		if err != nil {
			return err
		}
		rightText, err := client.readCString(right)
		if err != nil {
			return err
		}
		return answer(uint32(int32(strings.Compare(leftText, rightText))))

	case stdlibStrstr:
		haystack, err := argument(0)
		if err != nil {
			return err
		}
		needle, err := argument(1)
		if err != nil {
			return err
		}
		haystackText, err := client.readCString(haystack)
		if err != nil {
			return err
		}
		needleText, err := client.readCString(needle)
		if err != nil {
			return err
		}
		index := strings.Index(haystackText, needleText)
		if index < 0 {
			return answer(0)
		}
		return answer(haystack + uint32(index))

	case stdlibAtoi:
		pointer, err := argument(0)
		if err != nil {
			return err
		}
		text, err := client.readCString(pointer)
		if err != nil {
			return err
		}
		return answer(uint32(parseLeadingInt(text)))

	case stdlibMemcpy, stdlibMemmove:
		destination, err := argument(0)
		if err != nil {
			return err
		}
		source, err := argument(1)
		if err != nil {
			return err
		}
		length, err := argument(2)
		if err != nil {
			return err
		}
		if length > maxStdlibLength {
			return fmt.Errorf("LGT memcpy length %d exceeds the limit", length)
		}
		// One buffer read then written is already the memmove semantics, so
		// overlapping regions are safe without a direction check.
		block := make([]byte, length)
		if err := memory.Read(source, block); err != nil {
			return err
		}
		if err := memory.Write(destination, block); err != nil {
			return err
		}
		return answer(destination)

	case stdlibMemset:
		destination, err := argument(0)
		if err != nil {
			return err
		}
		value, err := argument(1)
		if err != nil {
			return err
		}
		length, err := argument(2)
		if err != nil {
			return err
		}
		if length > maxStdlibLength {
			return fmt.Errorf("LGT memset length %d exceeds the limit", length)
		}
		block := make([]byte, length)
		for index := range block {
			block[index] = byte(value)
		}
		if err := memory.Write(destination, block); err != nil {
			return err
		}
		return answer(destination)

	case stdlibMemcmp:
		left, err := argument(0)
		if err != nil {
			return err
		}
		right, err := argument(1)
		if err != nil {
			return err
		}
		length, err := argument(2)
		if err != nil {
			return err
		}
		if length > maxStdlibLength {
			return fmt.Errorf("LGT memcmp length %d exceeds the limit", length)
		}
		leftBlock, rightBlock := make([]byte, length), make([]byte, length)
		if err := memory.Read(left, leftBlock); err != nil {
			return err
		}
		if err := memory.Read(right, rightBlock); err != nil {
			return err
		}
		for index := range leftBlock {
			if leftBlock[index] != rightBlock[index] {
				if leftBlock[index] < rightBlock[index] {
					return answer(^uint32(0))
				}
				return answer(1)
			}
		}
		return answer(0)

	case stdlibMemchr:
		pointer, err := argument(0)
		if err != nil {
			return err
		}
		value, err := argument(1)
		if err != nil {
			return err
		}
		length, err := argument(2)
		if err != nil {
			return err
		}
		if length > maxStdlibLength {
			return fmt.Errorf("LGT memchr length %d exceeds the limit", length)
		}
		block := make([]byte, length)
		if err := memory.Read(pointer, block); err != nil {
			return err
		}
		for index, symbol := range block {
			if symbol == byte(value) {
				return answer(pointer + uint32(index))
			}
		}
		return answer(0)

	case stdlibTime:
		seconds := uint32(client.clock.unixMillis() / 1000)
		if out, err := argument(0); err == nil && out != 0 {
			if err := client.writeWord(out, seconds); err != nil {
				return err
			}
		}
		return answer(seconds)

	case stdlibRunFunction:
		target, err := argument(0)
		if err != nil {
			return err
		}
		if target == 0 {
			// Nothing to run is not a failure: the caller reads no result, so
			// there is nothing to report a refusal through either.
			return answer(0)
		}
		// On the running thread: the module calls this from inside a frame
		// whose locals it goes on to use, and the platform's own thread would
		// run the function over them. See callOn.
		if _, err := client.callOn(ctx, thread, target, nil); err != nil {
			return fmt.Errorf("run the function at %#x: %w", target, err)
		}
		return answer(0)

	case stdlibSetJump:
		// `setjmp`, and the other half of a Java title's try region: the
		// buffer it is given came from the Java table's own enter call, and
		// what it saves is where a throw comes back to. See java_throw.go.
		buffer, err := argument(0)
		if err != nil {
			return err
		}
		if err := client.armJavaTry(thread, buffer); err != nil {
			return fmt.Errorf("save a jump point at %#x: %w", buffer, err)
		}
		return nil

	case stdlibLocaltime:
		pointer, err := argument(0)
		if err != nil {
			return err
		}
		address, err := client.writeLocaltime(pointer)
		if err != nil {
			return err
		}
		return answer(address)
	}
	return fmt.Errorf("unimplemented LGT stdlib slot %#x", slot)
}

// stringWrite implements strcpy, strncpy and strcat, which differ only in
// where the write starts and how much of the source is taken.
func (client *Client) stringWrite(thread *armcore.Thread, slot uint32) error {
	destination, err := thread.Register(0)
	if err != nil {
		return err
	}
	source, err := thread.Register(1)
	if err != nil {
		return err
	}
	if slot == stdlibStrncpy {
		length, lengthErr := thread.Register(2)
		if lengthErr != nil {
			return lengthErr
		}
		if length > maxStdlibLength {
			return fmt.Errorf("LGT strncpy length %d exceeds the limit", length)
		}
		// strncpy reads at most n bytes and never requires the source to be
		// terminated. Reading it as a C string first refuses a source that
		// runs past the end of its own buffer — which a title does, and it was
		// stopped there over bytes strncpy would never have looked at.
		text := client.readBoundedString(source, length)
		// It pads with NUL to exactly n bytes and does not terminate a source
		// that fills the buffer, which is what a game relying on the padding
		// depends on.
		block := make([]byte, length)
		copy(block, text)
		if err := client.core.Memory().Write(destination, block); err != nil {
			return err
		}
		return thread.SetRegister(0, destination)
	}
	var text string
	if slot == stdlibStrncat {
		length, lengthErr := thread.Register(2)
		if lengthErr != nil {
			return lengthErr
		}
		if length > maxStdlibLength {
			return fmt.Errorf("LGT strncat length %d exceeds the limit", length)
		}
		// Counted like strncpy, so an unterminated source is not an error —
		// but unlike strncpy it always terminates and never pads.
		text = string(client.readBoundedString(source, length))
	} else {
		copied, readErr := client.readCString(source)
		if readErr != nil {
			return readErr
		}
		text = copied
	}
	offset := uint32(0)
	if slot == stdlibStrcat || slot == stdlibStrncat {
		existing, existingErr := client.readCString(destination)
		if existingErr != nil {
			return existingErr
		}
		offset = uint32(len(existing))
	}
	if err := client.core.Memory().Write(destination+offset, append([]byte(text), 0)); err != nil {
		return err
	}
	return thread.SetRegister(0, destination)
}

// The tm struct localtime answers with. The WIPI specification lists
// localtime among the "supported ANSI-C interface functions" without
// restating its interface, so the contract it defers to is ANSI C's: nine int
// members, and a returned pointer to storage the caller does not own and must
// not free. C89 does not fix the members' order, but every ARM toolchain a
// module here was built with lays them out in declaration order, which is the
// order below.
const (
	tmSeconds = 0
	tmMinutes = 4
	tmHours   = 8
	tmDay     = 12
	tmMonth   = 16
	tmYear    = 20
	tmWeekday = 24
	tmYearDay = 28
	tmIsDST   = 32
	tmSize    = 36
)

// writeLocaltime fills the shared tm struct from a time_t the guest points at
// and answers its address. The storage is allocated once and reused, because
// that is what a caller holding the previous result across a second call
// expects to see happen.
func (client *Client) writeLocaltime(pointer uint32) (uint32, error) {
	seconds := int64(client.clock.unixMillis() / 1000)
	if pointer != 0 {
		stored, err := client.readWord(pointer)
		if err != nil {
			return 0, err
		}
		seconds = int64(int32(stored))
	}
	if client.tmStorage == 0 {
		address, ok := client.arena.allocate(tmSize)
		if !ok {
			return 0, fmt.Errorf("LGT localtime cannot allocate its result")
		}
		client.tmStorage = address
	}
	moment := time.Unix(seconds, 0).UTC()
	fields := map[uint32]int32{
		tmSeconds: int32(moment.Second()),
		tmMinutes: int32(moment.Minute()),
		tmHours:   int32(moment.Hour()),
		tmDay:     int32(moment.Day()),
		// ANSI C counts months from zero and years from 1900.
		tmMonth:   int32(moment.Month()) - 1,
		tmYear:    int32(moment.Year()) - 1900,
		tmWeekday: int32(moment.Weekday()),
		tmYearDay: int32(moment.YearDay()) - 1,
		// The guest clock carries no zone, so there is no daylight saving to
		// report rather than an unknown one.
		tmIsDST: 0,
	}
	for offset, value := range fields {
		if err := client.writeWord(client.tmStorage+offset, uint32(value)); err != nil {
			return 0, err
		}
	}
	return client.tmStorage, nil
}

// parseLeadingInt is atoi: the longest numeric prefix, zero when there is
// none.
func parseLeadingInt(value string) int32 {
	trimmed := strings.TrimLeft(value, " \t\n\r")
	end := 0
	if end < len(trimmed) && (trimmed[end] == '+' || trimmed[end] == '-') {
		end++
	}
	for end < len(trimmed) && trimmed[end] >= '0' && trimmed[end] <= '9' {
		end++
	}
	parsed, err := strconv.ParseInt(trimmed[:end], 10, 64)
	if err != nil {
		return 0
	}
	if parsed > 1<<31-1 {
		return 1<<31 - 1
	}
	if parsed < -(1 << 31) {
		return -(1 << 31)
	}
	return int32(parsed)
}

// readBoundedString reads up to limit bytes and stops at the first NUL. It is
// what a counted string function is allowed to look at: memory past the count,
// or past the end of the mapping, is never touched, so a source that is not
// terminated is not an error.
func (client *Client) readBoundedString(address, limit uint32) []byte {
	const chunk = 256
	text := make([]byte, 0, min(limit, chunk))
	buffer := make([]byte, chunk)
	for read := uint32(0); read < limit; {
		want := min(limit-read, uint32(chunk))
		block := buffer[:want]
		if err := client.core.Memory().Read(address+read, block); err != nil {
			// A read that cannot be served ends the string where the mapping
			// does; the caller pads the rest.
			return text
		}
		if index := indexByte(block, 0); index >= 0 {
			return append(text, block[:index]...)
		}
		text = append(text, block...)
		read += want
	}
	return text
}

func indexByte(block []byte, value byte) int {
	for index, item := range block {
		if item == value {
			return index
		}
	}
	return -1
}

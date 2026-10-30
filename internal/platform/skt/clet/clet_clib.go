package clet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The SKT root's slot 14 is the C library the specification requires a
// platform to offer, in the order it lists the functions: string.h from 0 to
// 18, stdlib.h from 19 to 24, time.h from 25 to 30 (the stdarg.h entries are
// macros and take no slot). Both local titles call through it hundreds of
// times — strcpy at 0, strcmp at 4, strlen at 12, memcpy at 14 and memset at 18
// are each called with the arguments those functions take, and one title's
// localtime at 29 is handed a pointer to the word time at 26 filled — so the
// rest of the list sits where the same order puts it.
const (
	clibStrcpy = iota
	clibStrncpy
	clibStrcat
	clibStrncat
	clibStrcmp
	clibStrncmp
	clibStrchr
	clibStrrchr
	clibStrspn
	clibStrcspn
	clibStrpbrk
	clibStrstr
	clibStrlen
	clibStrtok
	clibMemcpy
	clibMemmove
	clibMemcmp
	clibMemchr
	clibMemset
	clibAtof
	clibAtoi
	clibAtoll
	clibStrtod
	clibStrtol
	clibStrtoul
	clibClock
	clibTime
	clibDifftime
	clibMktime
	clibLocaltime
	clibGmtime
)

// clibTimeRecordSize is a struct tm: nine ints, seconds to the daylight flag.
const clibTimeRecordSize = 36

func (runtime *initializationRuntime) handleCletLibraryCall(thread *armcore.Thread, function uint32) (uint32, error) {
	memory := runtime.client.core.Memory()
	argument := func(index int) (uint32, error) { return runtime.wipicArgument(thread, index) }
	first, err := argument(0)
	if err != nil {
		return 0, err
	}
	second, err := argument(1)
	if err != nil {
		return 0, err
	}
	third, err := argument(2)
	if err != nil {
		return 0, err
	}
	switch function {
	case clibStrcpy, clibStrcat:
		source, err := runtime.clibString(memory, second)
		if err != nil {
			return 0, err
		}
		destination := first
		if function == clibStrcat {
			length, err := guestStringLength(memory, first)
			if err != nil {
				return 0, err
			}
			destination += length
		}
		return first, memory.Write(destination, append(source, 0))
	case clibStrncpy:
		// strncpy pads with zeros to n and does not terminate a source of n
		// bytes or more.
		if third == 0 {
			return first, nil
		}
		source, err := runtime.clibBoundedString(memory, second, third)
		if err != nil {
			return 0, err
		}
		if err := hookRange(memory, first, third, armcore.PermissionWrite); err != nil {
			return 0, err
		}
		buffer := runtime.hookBuffer(third)
		clear(buffer)
		copy(buffer, source)
		return first, memory.Write(first, buffer)
	case clibStrncat:
		source, err := runtime.clibBoundedString(memory, second, third)
		if err != nil {
			return 0, err
		}
		length, err := guestStringLength(memory, first)
		if err != nil {
			return 0, err
		}
		return first, memory.Write(first+length, append(source, 0))
	case clibStrcmp, clibStrncmp:
		limit := uint32(maxGuestStringLength)
		if function == clibStrncmp {
			limit = third
		}
		left, err := runtime.clibBoundedString(memory, first, limit)
		if err != nil {
			return 0, err
		}
		right, err := runtime.clibBoundedString(memory, second, limit)
		if err != nil {
			return 0, err
		}
		return uint32(int32(bytes.Compare(left, right))), nil
	case clibStrchr, clibStrrchr:
		text, err := runtime.clibString(memory, first)
		if err != nil {
			return 0, err
		}
		target := byte(second)
		// The terminator is part of the string for both: searching for zero
		// answers its address.
		text = append(text, 0)
		index := bytes.IndexByte(text, target)
		if function == clibStrrchr {
			index = bytes.LastIndexByte(text, target)
		}
		if index < 0 {
			return 0, nil
		}
		return first + uint32(index), nil
	case clibStrspn, clibStrcspn, clibStrpbrk:
		text, err := runtime.clibString(memory, first)
		if err != nil {
			return 0, err
		}
		set, err := runtime.clibString(memory, second)
		if err != nil {
			return 0, err
		}
		span := 0
		for span < len(text) && (bytes.IndexByte(set, text[span]) >= 0) == (function == clibStrspn) {
			span++
		}
		if function != clibStrpbrk {
			return uint32(span), nil
		}
		if span == len(text) {
			return 0, nil
		}
		return first + uint32(span), nil
	case clibStrstr:
		text, err := runtime.clibString(memory, first)
		if err != nil {
			return 0, err
		}
		needle, err := runtime.clibString(memory, second)
		if err != nil {
			return 0, err
		}
		index := bytes.Index(text, needle)
		if index < 0 {
			return 0, nil
		}
		return first + uint32(index), nil
	case clibStrlen:
		return guestStringLength(memory, first)
	case clibStrtok:
		return runtime.clibStrtok(memory, first, second)
	case clibMemcpy, clibMemmove:
		// Both are a read of the whole source and then a write, so an overlap
		// copies what the source held before the call, which is memmove's
		// promise and more than memcpy's.
		if third == 0 {
			return first, nil
		}
		if err := hookRange(memory, second, third, armcore.PermissionRead); err != nil {
			return 0, err
		}
		if err := hookRange(memory, first, third, armcore.PermissionWrite); err != nil {
			return 0, err
		}
		buffer := runtime.hookBuffer(third)
		if err := memory.Read(second, buffer); err != nil {
			return 0, err
		}
		return first, memory.Write(first, buffer)
	case clibMemcmp:
		if third == 0 {
			return 0, nil
		}
		if err := hookRange(memory, first, third, armcore.PermissionRead); err != nil {
			return 0, err
		}
		if err := hookRange(memory, second, third, armcore.PermissionRead); err != nil {
			return 0, err
		}
		left := make([]byte, third)
		if err := memory.Read(first, left); err != nil {
			return 0, err
		}
		right := runtime.hookBuffer(third)
		if err := memory.Read(second, right); err != nil {
			return 0, err
		}
		return uint32(int32(bytes.Compare(left, right))), nil
	case clibMemchr:
		if third == 0 {
			return 0, nil
		}
		if err := hookRange(memory, first, third, armcore.PermissionRead); err != nil {
			return 0, err
		}
		buffer := runtime.hookBuffer(third)
		if err := memory.Read(first, buffer); err != nil {
			return 0, err
		}
		if index := bytes.IndexByte(buffer, byte(second)); index >= 0 {
			return first + uint32(index), nil
		}
		return 0, nil
	case clibMemset:
		if third == 0 {
			return first, nil
		}
		if err := hookRange(memory, first, third, armcore.PermissionWrite); err != nil {
			return 0, err
		}
		fill := runtime.hookBuffer(third)
		for index := range fill {
			fill[index] = byte(second)
		}
		return first, memory.Write(first, fill)
	case clibAtoi, clibStrtol, clibStrtoul:
		text, err := runtime.clibString(memory, first)
		if err != nil {
			return 0, err
		}
		base := 10
		if function != clibAtoi {
			base = int(int32(third))
		}
		value, consumed := clibParseInteger(text, base, function == clibStrtoul)
		if function != clibAtoi && second != 0 {
			if err := runtime.clibWriteWord(memory, second, first+uint32(consumed)); err != nil {
				return 0, err
			}
		}
		return uint32(value), nil
	case clibAtoll:
		text, err := runtime.clibString(memory, first)
		if err != nil {
			return 0, err
		}
		value, _ := clibParseInteger(text, 10, false)
		return clibWide(thread, uint64(value))
	case clibAtof, clibStrtod:
		text, err := runtime.clibString(memory, first)
		if err != nil {
			return 0, err
		}
		value, consumed := clibParseFloat(text)
		if function == clibStrtod && second != 0 {
			if err := runtime.clibWriteWord(memory, second, first+uint32(consumed)); err != nil {
				return 0, err
			}
		}
		// A double comes back in r0 and r1, the low word first, which is
		// the order one title's own conversion routine reads it in.
		return clibWide(thread, math.Float64bits(value))
	case clibClock:
		// Milliseconds the program has run, the resolution a handset's tick
		// gives.
		return uint32(runtime.guestElapsed() / time.Millisecond), nil
	case clibTime:
		seconds := uint32(runtime.guestMillis() / 1000)
		if first != 0 {
			if err := runtime.clibWriteWord(memory, first, seconds); err != nil {
				return 0, err
			}
		}
		return seconds, nil
	case clibDifftime:
		return clibWide(thread, math.Float64bits(float64(int32(first)-int32(second))))
	case clibMktime:
		return runtime.clibMktime(memory, first)
	case clibLocaltime, clibGmtime:
		if first == 0 {
			return 0, nil
		}
		words, err := runtime.readAOTWords(first, 1, "time value")
		if err != nil {
			return 0, err
		}
		moment := time.Unix(int64(int32(words[0])), 0)
		if function == clibLocaltime {
			moment = moment.In(time.Local)
		} else {
			moment = moment.UTC()
		}
		return runtime.clibWriteTime(memory, moment)
	}
	return 0, fmt.Errorf("SKT WIPI-C C library function %d is not implemented%s", function, runtime.callerSite(thread))
}

// clibString reads a terminated string the way every reader here does,
// bounded by maxGuestStringLength.
func (runtime *initializationRuntime) clibString(memory *armcore.Memory, address uint32) ([]byte, error) {
	length, err := guestStringLength(memory, address)
	if err != nil {
		return nil, err
	}
	text := make([]byte, length, length+1)
	if err := memory.Read(address, text); err != nil {
		return nil, err
	}
	return text, nil
}

// clibBoundedString reads at most limit bytes, stopping at a terminator.
func (runtime *initializationRuntime) clibBoundedString(memory *armcore.Memory, address, limit uint32) ([]byte, error) {
	limit = min(limit, maxGuestStringLength)
	if limit == 0 {
		return nil, nil
	}
	data, _, err := readGuestString(memory, address, limit, 1)
	return data, err
}

func (runtime *initializationRuntime) clibWriteWord(memory *armcore.Memory, address, value uint32) error {
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	return memory.Write(address, word[:])
}

// clibWide answers a 64-bit result in r0 and r1, the low word in r0.
func clibWide(thread *armcore.Thread, value uint64) (uint32, error) {
	if err := thread.SetRegister(1, uint32(value>>32)); err != nil {
		return 0, err
	}
	return uint32(value), nil
}

// clibStrtok keeps its place between calls, as strtok does: a null string
// continues after the last token. The place is the address in the guest's own
// buffer, which strtok writes its terminators into.
func (runtime *initializationRuntime) clibStrtok(memory *armcore.Memory, address, delimiters uint32) (uint32, error) {
	state := runtime.client.clet
	if address == 0 {
		address = state.tokenNext
	}
	if address == 0 {
		return 0, nil
	}
	text, err := runtime.clibString(memory, address)
	if err != nil {
		return 0, err
	}
	set, err := runtime.clibString(memory, delimiters)
	if err != nil {
		return 0, err
	}
	start := 0
	for start < len(text) && bytes.IndexByte(set, text[start]) >= 0 {
		start++
	}
	if start == len(text) {
		state.tokenNext = 0
		return 0, nil
	}
	end := start
	for end < len(text) && bytes.IndexByte(set, text[end]) < 0 {
		end++
	}
	if end == len(text) {
		state.tokenNext = 0
	} else {
		if err := memory.Write(address+uint32(end), []byte{0}); err != nil {
			return 0, err
		}
		state.tokenNext = address + uint32(end) + 1
	}
	return address + uint32(start), nil
}

// clibParseInteger reads what strtol and strtoul read: leading space, a sign,
// an optional 0x for base 16 or 0, and the digits of the base. It answers the
// value, saturated as those functions saturate, and how many bytes it used —
// none when there was no number.
func clibParseInteger(text []byte, base int, unsigned bool) (int64, int) {
	index := 0
	for index < len(text) && (text[index] == ' ' || text[index] >= '\t' && text[index] <= '\r') {
		index++
	}
	negative := false
	if index < len(text) && (text[index] == '+' || text[index] == '-') {
		negative = text[index] == '-'
		index++
	}
	if (base == 0 || base == 16) && index+1 < len(text) && text[index] == '0' && (text[index+1] == 'x' || text[index+1] == 'X') {
		base = 16
		index += 2
	} else if base == 0 && index < len(text) && text[index] == '0' {
		base = 8
	} else if base == 0 {
		base = 10
	}
	if base < 2 || base > 36 {
		return 0, 0
	}
	start := index
	var value uint64
	overflow := false
	for ; index < len(text); index++ {
		digit := strings.IndexByte("0123456789abcdefghijklmnopqrstuvwxyz", text[index]|0x20)
		if digit < 0 || digit >= base {
			break
		}
		if value > (math.MaxUint32-uint64(digit))/uint64(base) {
			overflow = true
		}
		value = value*uint64(base) + uint64(digit)
		if value > math.MaxUint32 {
			value = math.MaxUint32
		}
	}
	if index == start {
		return 0, 0
	}
	if unsigned {
		if overflow {
			return int64(int32(-1)), index
		}
		if negative {
			return int64(-int64(value)), index
		}
		return int64(value), index
	}
	signed := int64(value)
	if negative {
		signed = -signed
	}
	if overflow && negative || signed < math.MinInt32 {
		return math.MinInt32, index
	}
	if overflow || signed > math.MaxInt32 {
		return math.MaxInt32, index
	}
	return signed, index
}

// clibParseFloat reads the longest prefix of text that is a decimal number,
// after leading space, and answers its value and how many bytes it used.
func clibParseFloat(text []byte) (float64, int) {
	index := 0
	for index < len(text) && (text[index] == ' ' || text[index] >= '\t' && text[index] <= '\r') {
		index++
	}
	start := index
	end := index
	if end < len(text) && (text[end] == '+' || text[end] == '-') {
		end++
	}
	digits := 0
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
		digits++
	}
	if end < len(text) && text[end] == '.' {
		end++
		for end < len(text) && text[end] >= '0' && text[end] <= '9' {
			end++
			digits++
		}
	}
	if digits == 0 {
		return 0, 0
	}
	if end < len(text) && (text[end] == 'e' || text[end] == 'E') {
		exponent := end + 1
		if exponent < len(text) && (text[exponent] == '+' || text[exponent] == '-') {
			exponent++
		}
		if exponent < len(text) && text[exponent] >= '0' && text[exponent] <= '9' {
			for exponent < len(text) && text[exponent] >= '0' && text[exponent] <= '9' {
				exponent++
			}
			end = exponent
		}
	}
	value, err := strconv.ParseFloat(string(text[start:end]), 64)
	if err != nil && value == 0 {
		return 0, 0
	}
	return value, end
}

// clibWriteTime fills the struct tm localtime and gmtime answer with, which is
// one record the program shares between calls, as the C library's is.
func (runtime *initializationRuntime) clibWriteTime(memory *armcore.Memory, moment time.Time) (uint32, error) {
	state := runtime.client.clet
	if state.timeRecord == 0 {
		address, err := runtime.allocate(clibTimeRecordSize)
		if err != nil {
			return 0, err
		}
		state.timeRecord = address
	}
	fields := []int32{
		int32(moment.Second()), int32(moment.Minute()), int32(moment.Hour()),
		int32(moment.Day()), int32(moment.Month()) - 1, int32(moment.Year() - 1900),
		int32(moment.Weekday()), int32(moment.YearDay() - 1), 0,
	}
	record := make([]byte, clibTimeRecordSize)
	for index, value := range fields {
		binary.LittleEndian.PutUint32(record[index*4:], uint32(value))
	}
	if err := memory.Write(state.timeRecord, record); err != nil {
		return 0, err
	}
	return state.timeRecord, nil
}

// clibMktime is localtime's inverse over the same nine fields, which it
// normalises in place as mktime does.
func (runtime *initializationRuntime) clibMktime(memory *armcore.Memory, address uint32) (uint32, error) {
	words, err := runtime.readAOTWords(address, 6, "struct tm")
	if err != nil {
		return 0, err
	}
	field := func(index int) int { return int(int32(words[index])) }
	moment := time.Date(field(5)+1900, time.Month(field(4)+1), field(3), field(2), field(1), field(0), 0, time.Local)
	record := []int32{
		int32(moment.Second()), int32(moment.Minute()), int32(moment.Hour()),
		int32(moment.Day()), int32(moment.Month()) - 1, int32(moment.Year() - 1900),
		int32(moment.Weekday()), int32(moment.YearDay() - 1), 0,
	}
	data := make([]byte, clibTimeRecordSize)
	for index, value := range record {
		binary.LittleEndian.PutUint32(data[index*4:], uint32(value))
	}
	if err := memory.Write(address, data); err != nil {
		return 0, err
	}
	return uint32(moment.Unix()), nil
}

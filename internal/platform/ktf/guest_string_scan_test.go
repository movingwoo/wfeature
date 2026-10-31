package ktf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"strconv"
	"testing"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The readers below are the terminated-string readers as they were before
// every KTF reader shared one walk, kept as the answers the shared walk has to
// give each of them. readGuestStringByByte, beside the test of the walk
// itself, is the one readCString and readBoundedString stood on.

// readCStringBefore is readCString over readGuestStringByByte.
func readCStringBefore(memory *armcore.Memory, address, limit uint32) (string, error) {
	data, terminated, err := readGuestStringByByte(memory, address, limit)
	if err != nil {
		return "", err
	}
	if !terminated {
		return "", fmt.Errorf("string at %#x exceeds %d bytes", address, limit)
	}
	return string(data), nil
}

// readBoundedStringBefore is readBoundedString over readGuestStringByByte.
func readBoundedStringBefore(memory *armcore.Memory, address, limit uint32) ([]byte, error) {
	data, _, err := readGuestStringByByte(memory, address, limit)
	return data, err
}

// guestStringLengthBefore is the strlen and strcpy hooks' measure: 256 bytes
// at a time, a chunk that cannot be read whole read again a byte at a time,
// and an address past the top of the address space carried on at zero.
func guestStringLengthBefore(memory *armcore.Memory, start uint32) (uint32, error) {
	const chunk = 256
	buffer := make([]byte, chunk)
	for length := uint32(0); length < wipicrt.MaxGuestStringLength; {
		if err := memory.Read(start+length, buffer); err != nil {
			for index := 0; index < chunk; index++ {
				single := buffer[:1]
				if err := memory.Read(start+length+uint32(index), single); err != nil {
					return 0, err
				}
				if single[0] == 0 {
					return length + uint32(index), nil
				}
			}
			return 0, err
		}
		if index := bytes.IndexByte(buffer, 0); index >= 0 {
			return length + uint32(index), nil
		}
		length += chunk
	}
	return 0, fmt.Errorf("KTF guest string at %#x is not terminated within %d bytes", start, wipicrt.MaxGuestStringLength)
}

// wipicReadTerminatedBefore is wipicReadString for a length of -1: a unit at
// a time, a byte or a UTF-16 unit, up to 1024 of them.
func wipicReadTerminatedBefore(memory *armcore.Memory, pointer uint32, unicode bool) ([]rune, error) {
	if pointer == 0 {
		return nil, nil
	}
	unit := uint32(1)
	if unicode {
		unit = 2
	}
	var data []byte
	for offset := uint32(0); offset < 1024*unit; offset += unit {
		chunk := make([]byte, unit)
		if err := memory.Read(pointer+offset, chunk); err != nil {
			return nil, fmt.Errorf("read KTF draw string: %w", err)
		}
		if chunk[0] == 0 && (unit == 1 || chunk[1] == 0) {
			break
		}
		data = append(data, chunk...)
	}
	if unicode {
		units := make([]uint16, len(data)/2)
		for index := range units {
			units[index] = binary.LittleEndian.Uint16(data[index*2:])
		}
		return utf16.Decode(units), nil
	}
	return []rune(wipicrt.DecodeEUCKR(data)), nil
}

// nativeReadStringBefore is NativePlatform.readString: a byte at a time.
func nativeReadStringBefore(memory *armcore.Memory, address uint32) ([]byte, error) {
	if address == 0 {
		return nil, fmt.Errorf("KTF native library call on a null string")
	}
	text := make([]byte, 0, 64)
	buffer := make([]byte, 1)
	for offset := uint32(0); offset < nativeMaxString; offset++ {
		if err := memory.Read(address+offset, buffer); err != nil {
			return nil, fmt.Errorf("read KTF native string at %#x: %w", address, err)
		}
		if buffer[0] == 0 {
			return text, nil
		}
		text = append(text, buffer[0])
	}
	return nil, fmt.Errorf("KTF native string at %#x is not terminated within %d bytes", address, nativeMaxString)
}

// guestStringLayout maps the places a guest string can end against and
// answers the addresses to start reading at, chosen so that between them the
// strings end in every way there is to end:
//
//   - a run of short strings with a terminator every 97 bytes, so strings
//     starting a byte apart end on either side of a chunk, and the last of
//     which runs into the end of the mapping at a page boundary;
//   - one terminator, a zero halfword so that UTF-16 ends there too, with a
//     string of every length around a chunk before it, and after it bytes
//     that run into a mapping that stops inside a page, at an odd address;
//   - a lone zero byte just before the end of a mapping, which ends a string
//     of bytes and is only half of a UTF-16 unit;
//   - the last two pages of the address space, whose last 300 bytes hold no
//     terminator, and the first page, holding the string a read that carried
//     on past the top would find there;
//   - a run longer than the longest bound, with its terminator where each
//     bound puts it just inside and just outside.
func guestStringLayout(t testing.TB) (*armcore.Core, []uint32) {
	t.Helper()
	core := armcore.NewCore(armcore.CoreOptions{})
	memory := core.Memory()
	place := func(base uint32, data []byte) {
		t.Helper()
		if err := memory.Map(base, uint64(len(data)), armcore.PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		if err := memory.Write(base, data); err != nil {
			t.Fatal(err)
		}
	}
	periodic := func(size int) []byte {
		data := make([]byte, size)
		for index := range data {
			if index%97 != 96 {
				data[index] = byte('a' + index%26)
			}
		}
		return data
	}
	var starts []uint32

	const short = uint32(0x10000)
	place(short, periodic(0x3000))
	for _, offset := range []uint32{0, 1, 31, 32, 33, 63, 64, 65, 95, 96, 97, 160, 161, 0x1000 - 70, 0x1000 - 1, 0x1000,
		0x3000 - 66, 0x3000 - 64, 0x3000 - 63, 0x3000 - 2, 0x3000 - 1, 0x3000} {
		starts = append(starts, short+offset)
	}
	starts = append(starts, short-1)

	const single, terminator, singleEnd = uint32(0x20000), uint32(0x20500), uint32(0x21801)
	run := bytes.Repeat([]byte{'b'}, int(singleEnd-single))
	run[terminator-single], run[terminator-single+1] = 0, 0
	place(single, run)
	for _, length := range []uint32{0, 1, 2, 63, 64, 65, 127, 128, 129, 255, 256, 257, 1023, 1024, 1025, 1280} {
		starts = append(starts, terminator-length)
	}
	starts = append(starts, terminator+1, terminator+2)
	for _, before := range []uint32{0, 1, 2, 3, 63, 64, 65, 255, 256, 257, 1023, 1024, 1025} {
		starts = append(starts, singleEnd-before)
	}

	// A lone zero just before the end of a mapping, inside the chunk that has
	// to be read again a unit at a time: a string of bytes ends there, and a
	// UTF-16 one reads it as half of a unit and runs on into the end.
	const lone, loneZero = uint32(0x50000), uint32(0xf0)
	tail := bytes.Repeat([]byte{'w'}, 0x101)
	tail[loneZero] = 0
	place(lone, tail)
	for _, offset := range []uint32{0x30, 0x31, loneZero - 2, loneZero - 1, loneZero, loneZero + 1} {
		starts = append(starts, lone+offset)
	}

	const top = uint32(0xffffe000)
	high := periodic(0x2000)
	copy(high[len(high)-300:], bytes.Repeat([]byte{'z'}, 300))
	place(top, high)
	for _, offset := range []uint32{0, 1, 31, 32, 33, 63, 64, 65, 96, 97, 0x1000 - 1, 0x1000} {
		starts = append(starts, top+offset)
	}
	for _, before := range []uint32{300, 257, 256, 64, 3, 2, 1} {
		starts = append(starts, uint32(uint64(1)<<32-uint64(before)))
	}
	place(0, append([]byte("ab\x00\x00"), bytes.Repeat([]byte{'q'}, 0x1000-4)...))
	starts = append(starts, 0)

	const long = uint32(0x40000000)
	const reach = uint32(wipicrt.MaxGuestStringLength)
	far := bytes.Repeat([]byte{'x'}, int(reach)+0x1000)
	far[reach], far[reach+1] = 0, 0
	place(long, far)
	for _, before := range []uint32{reach, reach - 1, 65536, 65535, 4097, 4096, 4095, 2048, 2046, 1025, 1024, 1023, 64} {
		starts = append(starts, long+reach-before)
	}
	return core, starts
}

// runsPastTheTop reports whether a reader walking unit at a time from start,
// at most limit bytes, reaches the top of the address space before it meets
// a zero unit or a unit it cannot read: the one place the readers were
// allowed to answer differently once they shared a walk.
func runsPastTheTop(memory *armcore.Memory, start, limit, unit uint32) bool {
	piece := make([]byte, unit)
	for offset := uint64(0); offset+uint64(unit) <= uint64(limit); offset += uint64(unit) {
		current := uint64(start) + offset
		if current+uint64(unit) > uint64(1)<<32 {
			return true
		}
		if memory.Read(uint32(current), piece) != nil || piece[0] == 0 && (unit == 1 || piece[1] == 0) {
			return false
		}
	}
	return false
}

// described puts what a reader answered in one comparable line: the text it
// read, or the error it failed with.
func described(text string, err error) string {
	if err != nil {
		return "error " + err.Error()
	}
	return strconv.Quote(text)
}

// shortened cuts a long answer down for a failure message.
func shortened(answer string) string {
	if len(answer) > 160 {
		return fmt.Sprintf("%s... (%d bytes)", answer[:160], len(answer))
	}
	return answer
}

// guestStringReader is one caller of the walk, read the way it reads now and
// the way it read before.
type guestStringReader struct {
	name        string
	limit, unit uint32
	now, before func(start uint32) (string, error)
}

func guestStringReaders(core *armcore.Core) []guestStringReader {
	memory := core.Memory()
	runtime := detachedRuntime(wipicrt.NewClient(ktfPlatform, core, nil, 0))
	platform := &NativePlatform{client: &NativeClient{core: core}}
	var readers []guestStringReader
	for _, limit := range []uint32{0, 1, 63, 64, 65, 128, 255, 256, 257, 512, 1024, 4096, 65536} {
		readers = append(readers, guestStringReader{
			name: fmt.Sprintf("readCString(%d)", limit), limit: limit, unit: 1,
			now: func(start uint32) (string, error) {
				return runtime.ReadCString(start, limit)
			},
			before: func(start uint32) (string, error) {
				return readCStringBefore(memory, start, limit)
			},
		})
	}
	for _, limit := range []uint32{0, 1, 19, 63, 64, 65, 255, 256, 257, 4096} {
		readers = append(readers, guestStringReader{
			name: fmt.Sprintf("readBoundedString(%d)", limit), limit: limit, unit: 1,
			now: func(start uint32) (string, error) {
				data, err := runtime.ReadBoundedString(start, limit)
				return string(data), err
			},
			before: func(start uint32) (string, error) {
				data, err := readBoundedStringBefore(memory, start, limit)
				return string(data), err
			},
		})
	}
	readers = append(readers, guestStringReader{
		name: "guestStringLength", limit: wipicrt.MaxGuestStringLength, unit: 1,
		now: func(start uint32) (string, error) {
			length, err := guestStringLength(memory, start)
			return strconv.FormatUint(uint64(length), 10), err
		},
		before: func(start uint32) (string, error) {
			length, err := guestStringLengthBefore(memory, start)
			return strconv.FormatUint(uint64(length), 10), err
		},
	})
	for _, unicode := range []bool{false, true} {
		unit := uint32(1)
		if unicode {
			unit = 2
		}
		readers = append(readers, guestStringReader{
			name: fmt.Sprintf("wipicReadString(unicode %t)", unicode), limit: 1024 * unit, unit: unit,
			now: func(start uint32) (string, error) {
				text, err := runtime.WIPICReadString(start, -1, unicode)
				return string(text), err
			},
			before: func(start uint32) (string, error) {
				text, err := wipicReadTerminatedBefore(memory, start, unicode)
				return string(text), err
			},
		})
	}
	readers = append(readers, guestStringReader{
		name: "NativePlatform.readString", limit: nativeMaxString, unit: 1,
		now: func(start uint32) (string, error) {
			text, err := platform.readString(start)
			return string(text), err
		},
		before: func(start uint32) (string, error) {
			text, err := nativeReadStringBefore(memory, start)
			return string(text), err
		},
	})
	return readers
}

// Every KTF reader of a terminated guest string answers as it did before
// they shared one walk — the same text, the same refusal at its own bound,
// the same error naming the same unreadable unit — wherever the string starts
// and ends against a chunk, a page, the end of a mapping and the reader's
// bound. Only a string that runs off the top of the address space is left
// out here; what every reader answers for one of those is pinned on its own.
func TestEveryTerminatedGuestStringReaderAnswersAsBefore(t *testing.T) {
	core, starts := guestStringLayout(t)
	compared := 0
	for _, reader := range guestStringReaders(core) {
		for _, start := range starts {
			if runsPastTheTop(core.Memory(), start, reader.limit, reader.unit) {
				continue
			}
			compared++
			if got, want := described(reader.now(start)), described(reader.before(start)); got != want {
				t.Errorf("%s at %#x answered %s; before, %s", reader.name, start, shortened(got), shortened(want))
			}
		}
	}
	if compared < 2000 {
		t.Fatalf("compared only %d answers", compared)
	}
}

// A string that reaches the top of the address space without ending is
// refused, by every reader alike. Before they shared one walk, only
// readCString and readBoundedString refused one; strlen, the draw-string
// read and the native library carried on at address zero and answered with
// whatever they found there. Nothing depended on that: no KTF layout maps the
// first page or the last, so a string a title built cannot reach the top, and
// for untrusted memory a pointer that wraps is better refused than followed.
func TestAGuestStringRunningOffTheTopOfTheAddressSpaceIsRefused(t *testing.T) {
	core, starts := guestStringLayout(t)
	memory := core.Memory()
	// The last three bytes of the address space are "zzz", and the first
	// page starts with "ab" and a terminator: the readers that carried on
	// read the five as one string.
	const top = uint32(0xfffffffd)
	if length, err := guestStringLengthBefore(memory, top); err != nil || length != 5 {
		t.Fatalf("before, strlen at %#x answered %d, %v", top, length, err)
	}
	if text, err := wipicReadTerminatedBefore(memory, top, false); err != nil || string(text) != "zzzab" {
		t.Fatalf("before, the draw-string read at %#x answered %q, %v", top, string(text), err)
	}
	if text, err := nativeReadStringBefore(memory, top); err != nil || string(text) != "zzzab" {
		t.Fatalf("before, the native library at %#x answered %q, %v", top, text, err)
	}
	refused := 0
	for _, reader := range guestStringReaders(core) {
		for _, start := range starts {
			if !runsPastTheTop(memory, start, reader.limit, reader.unit) {
				continue
			}
			refused++
			if text, err := reader.now(start); !errors.Is(err, wipicrt.ErrGuestStringOverflow) {
				t.Errorf("%s at %#x answered %s, want the string refused", reader.name, start, shortened(described(text, err)))
			}
		}
	}
	// Every reader meets the top from some start: its own bound reaches it.
	if refused < len(guestStringReaders(core)) {
		t.Fatalf("only %d strings ran off the top", refused)
	}
}

// nineteenByteGuestString lays a 19-byte string out in guest memory, as bytes
// and as UTF-16 units: the length of a line of menu text or a short name.
func nineteenByteGuestString(t testing.TB) (*armcore.Core, uint32, uint32) {
	t.Helper()
	core := armcore.NewCore(armcore.CoreOptions{})
	memory := core.Memory()
	const base = uint32(0x30000000)
	if err := memory.Map(base, 0x10000, armcore.PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	const text = "nineteen bytes long"
	if err := memory.Write(base+0x100, append([]byte(text), 0)); err != nil {
		t.Fatal(err)
	}
	units := utf16.Encode([]rune(text))
	wide := make([]byte, 2*len(units)+2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(wide[2*index:], unit)
	}
	if err := memory.Write(base+0x200, wide); err != nil {
		t.Fatal(err)
	}
	return core, base + 0x100, base + 0x200
}

// A short string costs one allocation, for what its reader answers, and
// measuring one costs none. Each reader used to cost more: readCString a
// 128-byte buffer besides its string, the draw-string read an allocation per
// unit and more to grow what it had read, the native library a 64-byte buffer
// that its string copies then appended the terminator to. That copy still
// appends without allocating, because the slice it is handed has room.
func TestReadingAShortGuestStringAllocatesOnlyWhatItAnswers(t *testing.T) {
	core, address, wide := nineteenByteGuestString(t)
	memory := core.Memory()
	runtime := detachedRuntime(wipicrt.NewClient(ktfPlatform, core, nil, 0))
	platform := &NativePlatform{client: &NativeClient{core: core}}
	allocations := func(name string, read func() (int, error)) float64 {
		t.Helper()
		if length, err := read(); err != nil || length != 19 {
			t.Fatalf("%s read %d, %v", name, length, err)
		}
		return testing.AllocsPerRun(100, func() { _, _ = read() })
	}
	for _, check := range []struct {
		name string
		want float64
		read func() (int, error)
	}{
		{"readCString", 1, func() (int, error) {
			text, err := runtime.ReadCString(address, 128)
			return len(text), err
		}},
		{"readBoundedString", 1, func() (int, error) {
			data, err := runtime.ReadBoundedString(address, 64)
			return len(data), err
		}},
		{"guestStringLength", 0, func() (int, error) {
			length, err := guestStringLength(memory, address)
			return int(length), err
		}},
		{"NativePlatform.readString", 1, func() (int, error) {
			text, err := platform.readString(address)
			return len(text), err
		}},
	} {
		if got := allocations(check.name, check.read); got != check.want {
			t.Errorf("%s allocated %.0f times, want %.0f", check.name, got, check.want)
		}
	}
	if text, err := platform.readString(address); err != nil || cap(text) <= len(text) {
		t.Errorf("the native library's string has no room for its terminator: %d of %d, %v", len(text), cap(text), err)
	}

	// The draw-string read costs what decoding costs and one allocation more,
	// for the bytes it decodes. The runes are kept, as the read's own are, or
	// the conversion would cost nothing here.
	sample := []byte("nineteen bytes long")
	var decoded []rune
	decoding := testing.AllocsPerRun(100, func() { decoded = []rune(wipicrt.DecodeEUCKR(sample)) })
	if string(decoded) != string(sample) {
		t.Fatalf("decoded %q", string(decoded))
	}
	drawn := allocations("wipicReadString", func() (int, error) {
		text, err := runtime.WIPICReadString(address, -1, false)
		return len(text), err
	})
	if drawn > decoding+1 {
		t.Errorf("wipicReadString allocated %.0f times, decoding alone %.0f", drawn, decoding)
	}
	widened := allocations("wipicReadString unicode", func() (int, error) {
		text, err := runtime.WIPICReadString(wide, -1, true)
		return len(text), err
	})
	before := allocations("wipicReadString unicode before", func() (int, error) {
		text, err := wipicReadTerminatedBefore(memory, wide, true)
		return len(text), err
	})
	if widened >= before {
		t.Errorf("wipicReadString of UTF-16 allocated %.0f times, before %.0f", widened, before)
	}
}

func BenchmarkNineteenByteGuestString(b *testing.B) {
	core, address, wide := nineteenByteGuestString(b)
	memory := core.Memory()
	runtime := detachedRuntime(wipicrt.NewClient(ktfPlatform, core, nil, 0))
	platform := &NativePlatform{client: &NativeClient{core: core}}
	check := func(b *testing.B, length int, err error) {
		if err != nil || length != 19 {
			b.Fatalf("read %d, %v", length, err)
		}
	}
	b.Run("readCString", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			text, err := runtime.ReadCString(address, 128)
			check(b, len(text), err)
		}
	})
	b.Run("readBoundedString", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			data, err := runtime.ReadBoundedString(address, 64)
			check(b, len(data), err)
		}
	})
	b.Run("guestStringLength", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			length, err := guestStringLength(memory, address)
			check(b, int(length), err)
		}
	})
	b.Run("wipicReadString", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			text, err := runtime.WIPICReadString(address, -1, false)
			check(b, len(text), err)
		}
	})
	b.Run("wipicReadStringUnicode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			text, err := runtime.WIPICReadString(wide, -1, true)
			check(b, len(text), err)
		}
	})
	b.Run("NativePlatform.readString", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			text, err := platform.readString(address)
			check(b, len(text), err)
		}
	})
}

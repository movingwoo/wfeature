package ktf

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// readGuestStringByByte is how a guest string was read before it was read a
// chunk at a time, kept as the answer the chunked read has to give.
func readGuestStringByByte(memory *armcore.Memory, address, limit uint32) ([]byte, bool, error) {
	data := make([]byte, 0, min(limit, 128))
	for offset := uint32(0); offset < limit; offset++ {
		current := uint64(address) + uint64(offset)
		if current >= uint64(1)<<32 {
			return nil, false, fmt.Errorf("string address overflows guest memory")
		}
		var value [1]byte
		if err := memory.Read(uint32(current), value[:]); err != nil {
			return nil, false, err
		}
		if value[0] == 0 {
			return data, true, nil
		}
		data = append(data, value[0])
	}
	return data, false, nil
}

// A string read a chunk at a time reads as it did a byte at a time: the same
// bytes, the same answer about the terminator, and the same error, wherever
// the string starts and ends against a chunk, a page, the end of a mapping
// and the end of the address space.
func TestAGuestStringReadsAsItDidAByteAtATime(t *testing.T) {
	memory := armcore.NewMemory()
	const (
		low  = uint32(0x10000)
		high = uint32(0xffffe000)
	)
	if err := memory.Map(low, 0x3000, armcore.PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(high, 0x2000, armcore.PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	fill := func(base, size uint32) {
		data := make([]byte, size)
		for index := range data {
			// Mostly letters, with a terminator every 97 bytes.
			if index%97 == 96 {
				continue
			}
			data[index] = byte('a' + index%26)
		}
		if err := memory.Write(base, data); err != nil {
			t.Fatal(err)
		}
	}
	fill(low, 0x3000)
	fill(high, 0x2000)
	// The last bytes before each mapping's end hold no terminator, so a string
	// starting there runs into the unmapped space past it — or, at the top,
	// out of the address space.
	if err := memory.Write(low+0x3000-150, bytes.Repeat([]byte{'z'}, 150)); err != nil {
		t.Fatal(err)
	}
	if err := memory.Write(high+(0x2000-150), bytes.Repeat([]byte{'z'}, 150)); err != nil {
		t.Fatal(err)
	}
	// A terminator in the very last byte of the low mapping.
	if err := memory.Write(low+0x3000-1, []byte{0}); err != nil {
		t.Fatal(err)
	}
	var starts []uint32
	for _, base := range []uint32{low, high} {
		for _, offset := range []uint32{0, 1, 31, 63, 64, 65, 95, 96, 97, 0x1000 - 70, 0x1000 - 1, 0x1000} {
			starts = append(starts, base+offset)
		}
	}
	starts = append(starts, low+0x3000-150, low+0x3000-64, low+0x3000-2, low+0x3000-1,
		high+(0x2000-150), high+(0x2000-64), high+(0x2000-1), 0xffffffff, low-1, low+0x3000)
	for _, start := range starts {
		for _, limit := range []uint32{0, 1, 63, 64, 65, 97, 128, 200, 4096} {
			want, wantTerminated, wantErr := readGuestStringByByte(memory, start, limit)
			got, gotTerminated, gotErr := readGuestString(memory, start, limit)
			if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) || gotTerminated != wantTerminated || !bytes.Equal(got, want) {
				t.Fatalf("at %#x for %d bytes: read %q, %t, %v; a byte at a time read %q, %t, %v",
					start, limit, got, gotTerminated, gotErr, want, wantTerminated, wantErr)
			}
		}
	}
}

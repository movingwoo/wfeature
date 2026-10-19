package lgt

import (
	"bytes"
	"fmt"
	"strconv"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// readCStringByByte is readCString as it was before it read a chunk at a
// time, kept as the answer the chunked read has to give: a byte at a time, a
// null pointer an empty string, and an address past the top of the address
// space carried on at zero.
func readCStringByByte(memory *armcore.Memory, address uint32) (string, error) {
	if address == 0 {
		return "", nil
	}
	const limit = 4096
	buffer := make([]byte, 0, 64)
	one := make([]byte, 1)
	for offset := uint32(0); offset < limit; offset++ {
		if err := memory.Read(address+offset, one); err != nil {
			return "", err
		}
		if one[0] == 0 {
			return string(buffer), nil
		}
		buffer = append(buffer, one[0])
	}
	return "", fmt.Errorf("LGT string at %#x is not terminated within %d bytes", address, limit)
}

// cStringLayout maps the places a name can end against and answers the
// addresses to start reading at: short strings a terminator every 97 bytes
// apart; one terminator with a string of every length around a chunk and the
// bound before it, and after it bytes that run into a mapping that stops
// inside a page; and the last page of the address space, holding no
// terminator, before a first page that holds one — which this reader reaches
// by carrying on at zero, as it always has.
func cStringLayout(t testing.TB) (*Client, []uint32) {
	t.Helper()
	client := &Client{core: armcore.NewCore(armcore.CoreOptions{})}
	memory := client.core.Memory()
	place := func(base uint32, data []byte) {
		t.Helper()
		if err := memory.Map(base, uint64(len(data)), armcore.PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		if err := memory.Write(base, data); err != nil {
			t.Fatal(err)
		}
	}
	var starts []uint32

	const short = uint32(0x10000)
	periodic := make([]byte, 0x2000)
	for index := range periodic {
		if index%97 != 96 {
			periodic[index] = byte('a' + index%26)
		}
	}
	place(short, periodic)
	for _, offset := range []uint32{0, 1, 31, 32, 33, 63, 64, 65, 95, 96, 97, 0x1000 - 1, 0x1000, 0x2000 - 2, 0x2000 - 1, 0x2000} {
		starts = append(starts, short+offset)
	}
	starts = append(starts, short-1)

	const single, terminator, singleEnd = uint32(0x20000), uint32(0x21100), uint32(0x22801)
	run := bytes.Repeat([]byte{'b'}, int(singleEnd-single))
	run[terminator-single] = 0
	place(single, run)
	for _, length := range []uint32{0, 1, 63, 64, 65, 127, 128, 255, 256, 257, 4095, 4096, 4097} {
		starts = append(starts, terminator-length)
	}
	for _, before := range []uint32{0, 1, 2, 63, 64, 65, 255, 256, 257, 4095, 4096} {
		starts = append(starts, singleEnd-before)
	}

	const top = uint32(0xfffff000)
	place(top, bytes.Repeat([]byte{'z'}, 0x1000))
	place(0, append([]byte("ab\x00"), bytes.Repeat([]byte{'q'}, 0x1000-3)...))
	for _, before := range []uint32{4096, 4095, 300, 64, 3, 1} {
		starts = append(starts, uint32(uint64(1)<<32-uint64(before)))
	}
	starts = append(starts, 0, 1)
	return client, starts
}

// A name read a chunk at a time reads as it did a byte at a time: the same
// bytes, the same refusal at the bound, and the same error naming the same
// unreadable byte, wherever it starts and ends against a chunk, a page, the
// end of a mapping and the bound — and, at the top of the address space, the
// same string carried on at address zero.
func TestACStringReadsAsItDidAByteAtATime(t *testing.T) {
	client, starts := cStringLayout(t)
	answer := func(text string, err error) string {
		if err != nil {
			return "error " + err.Error()
		}
		return strconv.Quote(text)
	}
	for _, start := range starts {
		got, want := answer(client.readCString(start)), answer(readCStringByByte(client.core.Memory(), start))
		if got != want {
			if len(got) > 160 {
				got = fmt.Sprintf("%s... (%d bytes)", got[:160], len(got))
			}
			if len(want) > 160 {
				want = fmt.Sprintf("%s... (%d bytes)", want[:160], len(want))
			}
			t.Errorf("at %#x read %s; a byte at a time, %s", start, got, want)
		}
	}
	// Carrying on at zero is what this reader has always done, so the
	// layout has to show it: the last three bytes and then "ab".
	if text, err := client.readCString(0xfffffffd); err != nil || text != "zzzab" {
		t.Fatalf("at the top of the address space read %q, %v", text, err)
	}
}

// nineteenByteCString lays a 19-byte name out in guest memory.
func nineteenByteCString(t testing.TB) (*Client, uint32) {
	t.Helper()
	client := &Client{core: armcore.NewCore(armcore.CoreOptions{})}
	const base = uint32(0x20000000)
	if err := client.core.Memory().Map(base, 0x10000, armcore.PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := client.core.Memory().Write(base+0x100, []byte("nineteen bytes long\x00")); err != nil {
		t.Fatal(err)
	}
	return client, base + 0x100
}

// A short name costs one allocation, the string itself, and reading it a
// chunk at a time must not add another.
func TestReadingAShortCStringAllocatesOnlyTheString(t *testing.T) {
	client, address := nineteenByteCString(t)
	if text, err := client.readCString(address); err != nil || text != "nineteen bytes long" {
		t.Fatalf("read %q, %v", text, err)
	}
	if allocations := testing.AllocsPerRun(100, func() { _, _ = client.readCString(address) }); allocations != 1 {
		t.Fatalf("reading a 19-byte name allocated %.0f times, want 1", allocations)
	}
}

func BenchmarkNineteenByteCString(b *testing.B) {
	client, address := nineteenByteCString(b)
	b.ReportAllocs()
	for b.Loop() {
		if text, err := client.readCString(address); err != nil || len(text) != 19 {
			b.Fatalf("read %q, %v", text, err)
		}
	}
}

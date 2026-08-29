package lgt

import "testing"

// strtok is the one C library function here that carries state from one call
// to the next, which is why it stayed unimplemented until a call site settled
// both its slot and its contract. A title parsing a text table calls it once
// with the buffer and then again and again with a null pointer, feeding each
// answer to atoi.
func TestStrtokContinuesFromWhereItStopped(t *testing.T) {
	client := fixtureClient(t)

	table, err := client.allocateBytes(append([]byte("12,7\n8,9\n"), 0))
	if err != nil {
		t.Fatal(err)
	}
	newline, err := client.allocateBytes([]byte{'\n', 0})
	if err != nil {
		t.Fatal(err)
	}
	comma, err := client.allocateBytes([]byte{',', 0})
	if err != nil {
		t.Fatal(err)
	}

	// The first call names the buffer; every call after it continues, and the
	// delimiter may change between them — which the title that found this slot
	// does, alternating a comma inside a line with a newline at the end of it.
	first := callStdlib(t, client, stdlibStrtok, table, comma)
	if first != table {
		t.Fatalf("the first token started at %#x, want the buffer %#x", first, table)
	}
	if got := readFixtureString(t, client, first); got != "12" {
		t.Fatalf("the first field is %q, want %q", got, "12")
	}
	// The separator became the token's terminator in the caller's own buffer,
	// which is what makes the answer a string the title can hand straight to
	// atoi.
	if got := callStdlib(t, client, stdlibAtoi, first); got != 12 {
		t.Fatalf("atoi of the first field = %d, want 12", got)
	}

	second := callStdlib(t, client, stdlibStrtok, 0, newline)
	if got := readFixtureString(t, client, second); got != "7" {
		t.Fatalf("the second token is %q, want %q", got, "7")
	}
	third := callStdlib(t, client, stdlibStrtok, 0, comma)
	if got := readFixtureString(t, client, third); got != "8" {
		t.Fatalf("the third token is %q, want %q", got, "8")
	}
	fourth := callStdlib(t, client, stdlibStrtok, 0, newline)
	if got := readFixtureString(t, client, fourth); got != "9" {
		t.Fatalf("the fourth token is %q, want %q", got, "9")
	}
	// The table's last line ends with the delimiter, and a run of separators
	// is one separator: an empty final token would be parsed as a zero and
	// counted as a record.
	if last := callStdlib(t, client, stdlibStrtok, 0, newline); last != 0 {
		t.Fatalf("a continuation past the end answered %#x, want 0", last)
	}
	// And a continuation with nothing left to continue keeps answering
	// nothing rather than reading whatever the last state pointed at.
	if last := callStdlib(t, client, stdlibStrtok, 0, newline); last != 0 {
		t.Fatalf("a second continuation past the end answered %#x, want 0", last)
	}
}

// free is the other half of malloc and shares its heap, so a block returned
// through it is available to the next allocation. A pointer the heap never
// issued is ignored, which is the arena's rule: a title that frees a stack
// address must not be able to make the allocator hand that address out.
func TestFreeReturnsABlockToTheHeapMallocDrawsFrom(t *testing.T) {
	client := fixtureClient(t)

	block := callStdlib(t, client, stdlibMalloc, 64)
	if block == 0 {
		t.Fatal("malloc answered null")
	}
	callStdlib(t, client, stdlibFree, block)
	if again := callStdlib(t, client, stdlibMalloc, 64); again != block {
		t.Fatalf("the block after a free is %#x, want the freed %#x", again, block)
	}

	// A free of something this heap never handed out changes nothing.
	callStdlib(t, client, stdlibFree, 0x400f0000)
	callStdlib(t, client, stdlibFree, 0)
	if next := callStdlib(t, client, stdlibMalloc, 64); next == block || next == 0 {
		t.Fatalf("the next block is %#x, want a fresh one after a free of a foreign pointer", next)
	}
}

// readFixtureString reads back a guest C string a slot answered with.
func readFixtureString(t *testing.T, client *Client, address uint32) string {
	t.Helper()
	if address == 0 {
		return ""
	}
	text, err := client.readCString(address)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

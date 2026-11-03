package lgt

import (
	"strings"
	"testing"
)

// One LGT title stopped mid-play on stdlib slot 0x40b, which is where the
// specification's string.h list puts strchr. These check the run of slots that
// sits between two slots titles do call, so a wrong contract shows here rather
// than as a pointer a game follows into nothing.
func TestStringSearchSlotsFollowTheCLibrary(t *testing.T) {
	client := fixtureClient(t)

	text, err := client.allocateBytes(append([]byte("a/b/c"), 0))
	if err != nil {
		t.Fatal(err)
	}

	if found := callStdlib(t, client, stdlibStrchr, text, '/'); found != text+1 {
		t.Fatalf("strchr = %#x, want %#x", found, text+1)
	}
	if found := callStdlib(t, client, stdlibStrrchr, text, '/'); found != text+3 {
		t.Fatalf("strrchr = %#x, want %#x", found, text+3)
	}
	if found := callStdlib(t, client, stdlibStrchr, text, 'z'); found != 0 {
		t.Fatalf("strchr of an absent byte = %#x, want 0", found)
	}
	// Searching for NUL finds the terminator, which is how a caller measures
	// the end of a string it is already holding a pointer into.
	if found := callStdlib(t, client, stdlibStrchr, text, 0); found != text+5 {
		t.Fatalf("strchr of NUL = %#x, want the terminator %#x", found, text+5)
	}

	set, err := client.allocateBytes(append([]byte("ab"), 0))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := client.allocateBytes(append([]byte("/c"), 0))
	if err != nil {
		t.Fatal(err)
	}

	if span := callStdlib(t, client, stdlibStrspn, text, set); span != 1 {
		t.Fatalf("strspn = %d, want 1", span)
	}
	if span := callStdlib(t, client, stdlibStrcspn, text, accepted); span != 1 {
		t.Fatalf("strcspn = %d, want 1", span)
	}
	if found := callStdlib(t, client, stdlibStrpbrk, text, accepted); found != text+1 {
		t.Fatalf("strpbrk = %#x, want %#x", found, text+1)
	}
	// A set none of the string's bytes are in ends strpbrk at the terminator,
	// which it reports as no match rather than as a pointer to it.
	missing, err := client.allocateBytes(append([]byte("xyz"), 0))
	if err != nil {
		t.Fatal(err)
	}
	if found := callStdlib(t, client, stdlibStrpbrk, text, missing); found != 0 {
		t.Fatalf("strpbrk with no match = %#x, want 0", found)
	}
	if span := callStdlib(t, client, stdlibStrcspn, text, missing); span != 5 {
		t.Fatalf("strcspn with no match = %d, want 5", span)
	}
}

// strncat is counted on its source and terminates what it wrote; strcat is
// not. Both append at the destination's existing terminator rather than at its
// start, which is the part a wrong offset would silently destroy.
func TestConcatenationSlotsAppendRatherThanOverwrite(t *testing.T) {
	client := fixtureClient(t)

	destination, err := client.allocate(32)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.core.Memory().Write(destination, append([]byte("head"), 0)); err != nil {
		t.Fatal(err)
	}

	unterminated := make([]byte, 64)
	for index := range unterminated {
		unterminated[index] = 'a' + byte(index%26)
	}
	source, err := client.allocateBytes(unterminated)
	if err != nil {
		t.Fatal(err)
	}

	if result := callStdlib(t, client, stdlibStrncat, destination, source, 3); result != destination {
		t.Fatalf("strncat = %#x, want the destination %#x", result, destination)
	}
	joined, err := client.readCString(destination)
	if err != nil {
		t.Fatal(err)
	}
	if joined != "headabc" {
		t.Fatalf("strncat produced %q, want %q", joined, "headabc")
	}

	tail, err := client.allocateBytes(append([]byte("!"), 0))
	if err != nil {
		t.Fatal(err)
	}
	callStdlib(t, client, stdlibStrcat, destination, tail)
	joined, err = client.readCString(destination)
	if err != nil {
		t.Fatal(err)
	}
	if joined != "headabc!" {
		t.Fatalf("strcat produced %q, want %q", joined, "headabc!")
	}
}

// `strlen` is measured, not read. One title loads an eight-kilobyte text
// resource into an allocation of exactly its size — the file has no zero byte
// in it — and asks how long it is, which on a handset walks past the buffer to
// the first zero after it. A bound that belongs to a name stopped that title
// before its first frame.
func TestStringLengthWalksPastAnAllocationTheWayCDoes(t *testing.T) {
	client := fixtureClient(t)

	const size = 8192
	unterminated := make([]byte, size)
	for index := range unterminated {
		unterminated[index] = 'a'
	}
	buffer, err := client.allocateBytes(unterminated)
	if err != nil {
		t.Fatal(err)
	}
	length := callStdlib(t, client, stdlibStrlen, buffer)
	if length < size {
		t.Fatalf("strlen of an unterminated buffer of %d = %d, want at least %d",
			size, length, size)
	}
	// The answer is where the first zero after the buffer is, so the byte it
	// names has to be one.
	one := make([]byte, 1)
	if err := client.core.Memory().Read(buffer+length, one); err != nil {
		t.Fatal(err)
	}
	if one[0] != 0 {
		t.Errorf("strlen answered %d, where the byte is %#x rather than a terminator", length, one[0])
	}

	// A string that ends inside the last page of a mapping — a name on the
	// stack is the everyday one — is measured without reading past the end of
	// what is mapped. Reading a block at a time and nothing else refused one
	// of these, and it is the shape a real title's `strlen` argument most
	// often has.
	near := stackBase + uint32(stackSize) - 8
	if err := client.core.Memory().Write(near, []byte("abc\x00")); err != nil {
		t.Fatal(err)
	}
	if got := callStdlib(t, client, stdlibStrlen, near); got != 3 {
		t.Errorf("strlen of a string at the top of the stack = %d, want 3", got)
	}

	terminated, err := client.allocateBytes(append([]byte("a/b/c"), 0))
	if err != nil {
		t.Fatal(err)
	}
	if got := callStdlib(t, client, stdlibStrlen, terminated); got != 5 {
		t.Errorf("strlen of a five-byte string = %d, want 5", got)
	}
	if got := callStdlib(t, client, stdlibStrlen, 0); got != 0 {
		t.Errorf("strlen of a null pointer = %d, want 0", got)
	}
}

// The string slots read what a title built in a buffer of its own — a text
// resource, a line it is assembling — which is not a name and has no name's
// bound. A valid string longer than the bound a name is read with is still a
// string, and every slot here takes it the way strlen already does.
func TestStringSlotsTakeStringsLongerThanAName(t *testing.T) {
	client := fixtureClient(t)
	const run = 5000
	write := func(text string) uint32 {
		t.Helper()
		address, err := client.allocateBytes(append([]byte(text), 0))
		if err != nil {
			t.Fatal(err)
		}
		return address
	}
	long := write(strings.Repeat("x", run) + "/tail")
	other := write(strings.Repeat("x", run) + "/tail!")
	slash, tail, xs := write("/"), write("tail"), write("x")
	number := write("42" + strings.Repeat("x", run))
	readBack := func(address uint32, length int) string {
		t.Helper()
		data := make([]byte, length)
		if err := client.core.Memory().Read(address, data); err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	t.Run("strchr", func(t *testing.T) {
		if found := callStdlib(t, client, stdlibStrchr, long, '/'); found != long+run {
			t.Fatalf("strchr = %#x, want %#x", found, long+run)
		}
		if found := callStdlib(t, client, stdlibStrrchr, long, 'x'); found != long+run-1 {
			t.Fatalf("strrchr = %#x, want %#x", found, long+run-1)
		}
	})
	t.Run("strspn", func(t *testing.T) {
		if span := callStdlib(t, client, stdlibStrspn, long, xs); span != run {
			t.Fatalf("strspn = %d, want %d", span, run)
		}
		if span := callStdlib(t, client, stdlibStrcspn, long, slash); span != run {
			t.Fatalf("strcspn = %d, want %d", span, run)
		}
		if found := callStdlib(t, client, stdlibStrpbrk, long, slash); found != long+run {
			t.Fatalf("strpbrk = %#x, want %#x", found, long+run)
		}
	})
	t.Run("strcmp", func(t *testing.T) {
		if order := int32(callStdlib(t, client, stdlibStrcmp, long, other)); order >= 0 {
			t.Fatalf("strcmp = %d, want the shorter string first", order)
		}
		if order := callStdlib(t, client, stdlibStrcmp, long, long); order != 0 {
			t.Fatalf("strcmp of a string with itself = %d", order)
		}
	})
	t.Run("strstr", func(t *testing.T) {
		if found := callStdlib(t, client, stdlibStrstr, long, tail); found != long+run+1 {
			t.Fatalf("strstr = %#x, want %#x", found, long+run+1)
		}
	})
	t.Run("atoi", func(t *testing.T) {
		if value := callStdlib(t, client, stdlibAtoi, number); value != 42 {
			t.Fatalf("atoi = %d, want 42", value)
		}
	})
	t.Run("strcpy and strcat", func(t *testing.T) {
		destination, err := client.allocate(3 * run)
		if err != nil {
			t.Fatal(err)
		}
		callStdlib(t, client, stdlibStrcpy, destination, long)
		callStdlib(t, client, stdlibStrcat, destination, tail)
		want := strings.Repeat("x", run) + "/tailtail\x00"
		if got := readBack(destination, len(want)); got != want {
			t.Fatalf("strcpy then strcat wrote %q...", got[run:])
		}
	})
	t.Run("sprintk %s", func(t *testing.T) {
		destination, err := client.allocate(2 * run)
		if err != nil {
			t.Fatal(err)
		}
		format := write("%s!")
		if length := callSlot(t, client, slotSprintk, destination, format, long); length != run+6 {
			t.Fatalf("sprintk wrote %d bytes, want %d", length, run+6)
		}
		if got := readBack(destination+run, 7); got != "/tail!\x00" {
			t.Fatalf("sprintk ended %q", got)
		}
	})
}

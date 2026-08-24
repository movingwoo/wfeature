package lgt

import "testing"

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

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

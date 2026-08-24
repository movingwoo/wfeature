package lgt

import (
	"testing"
)

// Two titles stop on `srand(time(NULL))` — the seed call one instruction after
// `time` — and the second of them reaches `rand` as soon as seeding stops
// failing. Both are pinned here through the SVC, because what identified them
// was the pair of call sites and not a number counted off a list.
func TestSrandSeedsTheGeneratorRandAnswersFrom(t *testing.T) {
	client := fixtureClient(t)

	callStdlib(t, client, stdlibSrand, 12345)
	first := make([]uint32, 8)
	for index := range first {
		first[index] = callStdlib(t, client, stdlibRand)
	}
	for index, value := range first {
		if value > cRandomMax {
			t.Fatalf("rand answered %d at %d, which is above RAND_MAX %d", value, index, cRandomMax)
		}
	}

	// Reseeding with the same seed repeats the sequence, which is the whole of
	// what srand promises and what a title replaying a level depends on.
	callStdlib(t, client, stdlibSrand, 12345)
	for index, want := range first {
		if got := callStdlib(t, client, stdlibRand); got != want {
			t.Fatalf("after reseeding, draw %d is %d, want %d", index, got, want)
		}
	}

	// A different seed has to move the sequence, or seeding is decoration.
	callStdlib(t, client, stdlibSrand, 999)
	same := true
	for _, want := range first {
		if callStdlib(t, client, stdlibRand) != want {
			same = false
			break
		}
	}
	if same {
		t.Fatal("a different seed produced the same sequence")
	}
}

// A title never called srand at all and still expects numbers; ANSI C defines
// that as a generator seeded with 1.
func TestRandWithoutSrandStillAnswers(t *testing.T) {
	client := fixtureClient(t)
	unseeded := callStdlib(t, client, stdlibRand)

	seeded := fixtureClient(t)
	callStdlib(t, seeded, stdlibSrand, 1)
	if want := callStdlib(t, seeded, stdlibRand); unseeded != want {
		t.Fatalf("an unseeded draw is %d, want the seed-1 draw %d", unseeded, want)
	}
}

// srand is declared void, so it must not write r0. A caller that reads it is
// reading its own argument back, which is what a routine that never touches
// its return register leaves; see the same reasoning for KTF's MC_knlFree.
func TestSrandLeavesTheCallersRegisterAlone(t *testing.T) {
	client := fixtureClient(t)
	if result := callStdlib(t, client, stdlibSrand, 0x1234); result != 0x1234 {
		t.Fatalf("srand left %#x in r0, want the argument back", result)
	}
}

// malloc hands out memory rather than handing the size back. What said so is a
// title that memsets exactly the requested number of bytes at exactly what it
// received, so a returned size faults on unmapped memory.
func TestMallocAnswersWritableMemoryOfTheRequestedSize(t *testing.T) {
	client := fixtureClient(t)

	const size = 140
	address := callStdlib(t, client, stdlibMalloc, size)
	if address == 0 {
		t.Fatal("malloc answered null")
	}
	if address == size {
		t.Fatal("malloc handed the size back instead of memory")
	}
	if err := client.core.Memory().Write(address, make([]byte, size)); err != nil {
		t.Fatalf("the block malloc answered is not writable for %d bytes: %v", size, err)
	}

	// Two live blocks may not overlap, which a bump that forgot to advance
	// would not catch on one allocation alone.
	second := callStdlib(t, client, stdlibMalloc, size)
	if second == 0 {
		t.Fatal("the second malloc answered null")
	}
	if second < address+size && address < second+size {
		t.Fatalf("blocks at %#x and %#x of %d bytes overlap", address, second, size)
	}
}

// vsprintf renders the same format as sprintf, but its arguments come out of
// guest memory through the va_list it is handed rather than out of the
// registers the call arrived in. A title reaches it with a file name it has
// assembled on its own stack, so the two paths have to agree.
func TestVsprintfReadsItsArgumentsThroughTheList(t *testing.T) {
	client := fixtureClient(t)

	format, err := client.allocateBytes(append([]byte("%s-%d-%04x"), 0))
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.allocateBytes(append([]byte("slot"), 0))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.allocate(64)
	if err != nil {
		t.Fatal(err)
	}
	// The whole argument list lives in guest memory, including the words a
	// register call would have carried: that is the difference this pins.
	list, err := client.allocateWords([]uint32{text, 7, 0x2a})
	if err != nil {
		t.Fatal(err)
	}

	written := callStdlib(t, client, stdlibVsprintf, out, format, list)
	rendered, err := client.readCString(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := "slot-7-002a"; rendered != want {
		t.Fatalf("vsprintf rendered %q, want %q", rendered, want)
	}
	if int(written) != len(rendered) {
		t.Fatalf("vsprintf returned %d, want the %d bytes it wrote", written, len(rendered))
	}
}

package lgt

import (
	"context"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// callStdlib drives one C library slot the way the SVC handler would.
func callStdlib(t *testing.T, client *Client, slot uint32, arguments ...uint32) uint32 {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	for index, value := range arguments {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.handleStdlibSVC(context.Background(), thread, slot); err != nil {
		t.Fatalf("stdlib slot %#x: %v", slot, err)
	}
	result, err := thread.Register(0)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// localtime used to answer a null pointer on the reasoning that no title read
// a tm struct. One does: it calls localtime and loads the field at offset 4
// straight off the result, so a null answer faulted in the title's own code
// with nothing to say the platform had caused it.
func TestLocaltimeAnswersAStructRatherThanNull(t *testing.T) {
	client := fixtureClient(t)

	// A time_t the guest points at, rather than the guest clock: the argument
	// is what the caller asked about.
	const stored uint32 = 0x30000100
	moment := time.Date(2007, time.March, 4, 5, 6, 7, 0, time.UTC)
	if err := client.writeWord(stored, uint32(moment.Unix())); err != nil {
		t.Fatal(err)
	}

	result := callStdlib(t, client, stdlibLocaltime, stored)
	if result == 0 {
		t.Fatal("localtime answered a null pointer, which is what the caller dereferences")
	}

	for _, field := range []struct {
		name   string
		offset uint32
		want   uint32
	}{
		{"tm_sec", tmSeconds, 7},
		{"tm_min", tmMinutes, 6},
		{"tm_hour", tmHours, 5},
		{"tm_mday", tmDay, 4},
		// ANSI C counts months from zero and years from 1900.
		{"tm_mon", tmMonth, 2},
		{"tm_year", tmYear, 107},
		// 2007-03-04 was a Sunday, and it was the 63rd day of the year.
		{"tm_wday", tmWeekday, 0},
		{"tm_yday", tmYearDay, 62},
		{"tm_isdst", tmIsDST, 0},
	} {
		got, err := client.readWord(result + field.offset)
		if err != nil {
			t.Fatal(err)
		}
		if got != field.want {
			t.Errorf("%s at offset %d = %d, want %d", field.name, field.offset, got, field.want)
		}
	}
}

// ANSI C says the caller does not own localtime's result, so a second call
// has to land in the same storage rather than allocating a new struct every
// time — a title calling it once a frame would otherwise exhaust the arena.
func TestLocaltimeReusesItsStorage(t *testing.T) {
	client := fixtureClient(t)

	first := callStdlib(t, client, stdlibLocaltime, 0)
	second := callStdlib(t, client, stdlibLocaltime, 0)
	if first == 0 || first != second {
		t.Fatalf("localtime answered %#x then %#x, want one reused address", first, second)
	}
}

// A null argument means "use the current time" rather than "fail", because
// the guest clock is the only time this platform has.
func TestLocaltimeWithoutATimeUsesTheGuestClock(t *testing.T) {
	client := fixtureClient(t)

	result := callStdlib(t, client, stdlibLocaltime, 0)
	if result == 0 {
		t.Fatal("localtime answered a null pointer for a null argument")
	}
	year, err := client.readWord(result + tmYear)
	if err != nil {
		t.Fatal(err)
	}
	// The guest clock starts from a real date, so the year is at least the one
	// the epoch's own century begins at rather than 1900.
	if year < 70 {
		t.Errorf("tm_year = %d, which is before the guest clock's own epoch", year)
	}
}

// MC_knlExit is the fifth kernel function and so lands at 0x68. The slot it
// used to sit on, 0x6b, is the eighth — a different function entirely.
func TestExitSlotFollowsTheKernelNumbering(t *testing.T) {
	if slotExit != 0x68 {
		t.Errorf("slotExit = %#x, want 0x68: the kernel block's fifth function", slotExit)
	}
	if knownWIPICSlot(0x6b) {
		t.Error("0x6b is claimed as a known slot, but nothing here implements what it names")
	}
}

// strncpy and strncmp are counted, so neither may require its source to be
// terminated: a title copies a fixed number of bytes out of a buffer whose
// contents run past them, and reading that source as a C string first refuses
// the call over bytes the function would never have looked at.
func TestCountedStringCallsDoNotRequireATerminator(t *testing.T) {
	client := fixtureClient(t)

	// A source with no NUL anywhere near it.
	unterminated := make([]byte, 8192)
	for index := range unterminated {
		unterminated[index] = 'a' + byte(index%26)
	}
	source, err := client.allocateBytes(unterminated)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := client.allocate(16)
	if err != nil {
		t.Fatal(err)
	}

	if result := callStdlib(t, client, stdlibStrncpy, destination, source, 8); result != destination {
		t.Fatalf("strncpy = %#x, want the destination %#x", result, destination)
	}
	copied := make([]byte, 8)
	if err := client.core.Memory().Read(destination, copied); err != nil {
		t.Fatal(err)
	}
	if string(copied) != string(unterminated[:8]) {
		t.Fatalf("strncpy copied %q, want %q", copied, unterminated[:8])
	}

	if result := callStdlib(t, client, stdlibStrncmp, source, source, 8); result != 0 {
		t.Fatalf("strncmp of a source with itself = %d, want 0", int32(result))
	}

	// A shorter source still stops at its terminator and pads the rest.
	short, err := client.allocateBytes(append([]byte("ab"), 0))
	if err != nil {
		t.Fatal(err)
	}
	callStdlib(t, client, stdlibStrncpy, destination, short, 6)
	padded := make([]byte, 6)
	if err := client.core.Memory().Read(destination, padded); err != nil {
		t.Fatal(err)
	}
	if string(padded) != "ab\x00\x00\x00\x00" {
		t.Fatalf("strncpy padded to %q", padded)
	}
}

// The tick is a floor on the guest clock, not something the work is added to.
//
// A frame that spent part of a tick working and the rest idle took one tick.
// Summing the two ran every title at about 1.9x — fast enough to break every
// animation and cutscene, and invisible in a screenshot. The work only wins
// when it overruns the tick, which is the spin-wait case the work clock exists
// for: a loading screen that burns seconds of instructions inside one call has
// to move the clock by seconds or it never finishes.
func TestTheTickIsAFloorOnTheGuestClockNotAnAddition(t *testing.T) {
	const tick = 50 * time.Millisecond
	steps := uint64(0)
	clock := newGuestClock(func() uint64 { return steps })

	// An idle tick is one tick.
	clock.advance(tick)
	if got := clock.now(); got != tick {
		t.Fatalf("an idle tick advanced the clock %v, want %v", got, tick)
	}

	// A tick the guest was busy for part of is still one tick.
	steps += 43 * guestInstructionsPerMillisecond
	clock.advance(tick)
	if got := clock.now(); got != 2*tick {
		t.Fatalf("a partly busy tick advanced the clock to %v, want %v", got, 2*tick)
	}

	// Work that overruns the tick is what passed, because the guest really was
	// inside the call for that long.
	steps += 3300 * guestInstructionsPerMillisecond
	clock.advance(tick)
	if got, want := clock.now(), 2*tick+3300*time.Millisecond; got != want {
		t.Fatalf("an overrunning tick advanced the clock to %v, want %v", got, want)
	}
}

// Time never goes backwards across a tick boundary. A reader inside the tick
// already sees the work, so a boundary that moved the clock by less than the
// work would hand a game a timestamp older than one it had.
func TestTheGuestClockNeverGoesBackwardsAtATickBoundary(t *testing.T) {
	const tick = 50 * time.Millisecond
	steps := uint64(0)
	clock := newGuestClock(func() uint64 { return steps })

	boundary := clock.now()
	for round, work := range []uint64{0, 10, 49, 50, 51, 200, 3300} {
		steps += work * guestInstructionsPerMillisecond
		// The mid-tick reading already carries the work, which is the reading a
		// boundary must not undo.
		midTick := clock.now()
		clock.advance(tick)
		next := clock.now()
		if next < midTick {
			t.Fatalf("round %d: the boundary took the clock from %v back to %v", round, midTick, next)
		}
		if next-boundary < tick {
			t.Fatalf("round %d: a tick moved the clock %v, want at least %v", round, next-boundary, tick)
		}
		boundary = next
	}
}

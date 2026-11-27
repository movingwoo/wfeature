package lgt

import (
	"bytes"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/wipic"
)

// The component block refuses a text widget. A title that asks for one has
// to receive the error because a handle for a widget the platform never draws
// leaves it waiting for text from something invisible.
func TestComponentBlockRefusesToCreateATextComponent(t *testing.T) {
	client := fixtureClient(t)
	name := writeGuest(t, client, append([]byte("TextComponent"), 0))

	if got := callSlot(t, client, slotUicCreateApplicationContext); got == 0 {
		t.Fatal("MC_uicCreateApplicationContext answered no context at all")
	}
	if got := int32(callSlot(t, client, slotUicGetClass, name)); got != wipiError {
		t.Fatalf("MC_uicGetClass = %d, want %d", got, wipiError)
	}
	if got := int32(callSlot(t, client, slotUicCreate, uicApplicationContext, 0)); got != wipiError {
		t.Fatalf("MC_uicCreate = %d, want %d", got, wipiError)
	}
	// The rest of the block is accepted rather than fatal, so a title that
	// configures the component it was refused still runs.
	if got := int32(callSlot(t, client, slotUicLast)); got != wipiSuccess {
		t.Fatalf("the block's last slot = %d, want %d", got, wipiSuccess)
	}
}

func TestDateComponentKeepsItsCreationTimeAndAcceptsANewTime(t *testing.T) {
	client := fixtureClient(t)
	when := time.Date(2024, time.February, 29, 13, 47, 58, 0, time.Local)
	client.clock.origin = when
	name := writeGuest(t, client, append([]byte("DateTimeComponent"), 0))
	app := callSlot(t, client, 0x320)
	class := callSlot(t, client, 0x321, name)
	first := callSlot(t, client, 0x322, app, class)
	if int32(first) <= 0 {
		t.Fatalf("create date component = %#x", first)
	}
	client.clock.advance(time.Hour)
	second := callSlot(t, client, 0x322, app, class)
	if first == second {
		t.Fatal("two live date components share a handle")
	}
	out := writeGuest(t, client, bytes.Repeat([]byte{0xcc}, wipic.TimeRecordSize))
	check := func(handle uint32, want []byte) {
		t.Helper()
		callSlot(t, client, 0x338, handle, out)
		got := make([]byte, len(want))
		if err := client.core.Memory().Read(out, got); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("get time = %x (%v), want %x", got, err, want)
		}
	}
	check(first, wipic.TimeRecord(when))
	check(second, wipic.TimeRecord(when.Add(time.Hour)))
	callSlot(t, client, 0x336, first, out)
	check(first, wipic.TimeRecord(when.Add(time.Hour)))
	callSlot(t, client, slotUicSetTimeLong, first, ^uint32(0))
	check(first, wipic.TimeRecord(time.Unix(-1, 0).In(time.Local)))
	callSlot(t, client, 0x337, first, 0)
	epoch := wipic.TimeRecord(time.Unix(0, 0).In(time.Local))
	check(first, epoch)
	callSlot(t, client, slotUicSetTime, first, 0)
	if got := int32(callSlot(t, client, slotUicSetTime, first, 0xfffffff0)); got != wipiError {
		t.Fatalf("set time from an invalid range = %d", got)
	}
	check(first, epoch)
	callSlot(t, client, 0x323, second)
	check(second, epoch) // a destroyed component leaves the output alone
	callSlot(t, client, 0x338, first, 0)
	if got := int32(callSlot(t, client, 0x322, 0, class)); got != wipiError {
		t.Fatalf("create with an unknown context = %d", got)
	}
}

func TestDateComponentReusesDestroyedHandlesAtCapacity(t *testing.T) {
	client := fixtureClient(t)
	for index := range maxDateComponents {
		if handle := callSlot(t, client, slotUicCreate, uicApplicationContext, uicDateClass); handle != uicFirstDate+uint32(index) {
			t.Fatalf("component %d got handle %#x", index, handle)
		}
	}
	if got := int32(callSlot(t, client, slotUicCreate, uicApplicationContext, uicDateClass)); got != wipiNoMemory {
		t.Fatalf("create with full table = %d", got)
	}
	callSlot(t, client, slotUicDestroy, uicFirstDate)
	if got := callSlot(t, client, slotUicCreate, uicApplicationContext, uicDateClass); got != uicFirstDate {
		t.Fatalf("replacement handle = %#x, want %#x", got, uicFirstDate)
	}
}

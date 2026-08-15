package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The detector is only worth having if a write that lands in a released block
// is still visible when the arena hands that block out again, so the test is
// the fault itself: mark a block, write into it the way a title that kept the
// pointer would, and ask what the reuse sees.
func TestArenaPoisonFindsAWriteIntoAReleasedBlock(t *testing.T) {
	memory := armcore.NewMemory()
	const base uint32 = 0x30000000
	if err := memory.Map(base, 64<<10, armcore.PermissionReadWrite); err != nil {
		t.Fatalf("Map() = %v", err)
	}
	window := make([]byte, arenaPoisonWindow)

	// A block spanning more than one window, so the search is exercised past
	// its first read.
	const size = arenaPoisonWindow*2 + 16
	if err := poisonGuestBlock(memory, base, size); err != nil {
		t.Fatalf("poisonGuestBlock() = %v", err)
	}
	if _, written, err := checkGuestPoison(memory, window, base, size); err != nil || written != nil {
		t.Fatalf("checkGuestPoison() over an untouched block = %v, %v", written, err)
	}

	stale := []byte{0x12, 0x34, 0x56, 0x78}
	if err := memory.Write(base+arenaPoisonWindow+8, stale); err != nil {
		t.Fatalf("Write() = %v", err)
	}
	offset, written, err := checkGuestPoison(memory, window, base, size)
	if err != nil {
		t.Fatalf("checkGuestPoison() = %v", err)
	}
	if offset != arenaPoisonWindow+8 {
		t.Fatalf("offset = %d, want %d", offset, arenaPoisonWindow+8)
	}
	if len(written) < len(stale) || string(written[:len(stale)]) != string(stale) {
		t.Fatalf("written = % x, want it to begin with % x", written, stale)
	}

	// A block released again is marked again, so a fault is reported once
	// rather than for the rest of the session.
	if err := poisonGuestBlock(memory, base, size); err != nil {
		t.Fatalf("poisonGuestBlock() = %v", err)
	}
	if _, written, err := checkGuestPoison(memory, window, base, size); err != nil || written != nil {
		t.Fatalf("checkGuestPoison() after re-marking = %v, %v", written, err)
	}
}

// The pattern must not look like an address inside the arena: the collector
// reads every committed word as a possible reference, and a mark that pointed
// into the arena would keep dead objects alive while a block stayed free.
func TestArenaPoisonIsNotAnArenaAddress(t *testing.T) {
	word := uint32(arenaPoisonByte)
	word |= word<<8 | word<<16 | word<<24
	if uint64(word) < uint64(platformDataBase)+platformDataSize {
		t.Fatalf("poison word %#x falls inside the arena", word)
	}
}

package cheat

import (
	"errors"
	"testing"
)

// watchableMemory is a target with store instrumentation behind it, which is
// what the two ARM platforms have and the MIDlet runtime does not.
type watchableMemory struct {
	testMemory
	watched []uint32
}

func (memory *watchableMemory) Watch(address uint32) {
	memory.watched = append(memory.watched, address)
}
func (memory *watchableMemory) Unwatch(address uint32) {}
func (memory *watchableMemory) ClearWatches()          { memory.watched = nil }
func (memory *watchableMemory) Watches() []uint32      { return memory.watched }
func (memory *watchableMemory) WatchHits() []WatchHit  { return nil }
func (memory *watchableMemory) WatchHitsOverflowed() bool {
	return false
}

// A Host asks before it offers the control. It used to be able to ask -- the
// method was here -- and nothing did, so the browser panel offered write
// watching on a platform that cannot do it and its poll failed every interval.
func TestCanWatchAnswersForTheTargetUnderneath(t *testing.T) {
	plain := NewSession(&testMemory{base: 0x1000, data: make([]byte, 64)})
	if plain.CanWatch() {
		t.Error("a target with no store instrumentation said it can watch writes")
	}
	if err := plain.Watch(0x1000); !errors.Is(err, ErrWatchUnsupported) {
		t.Errorf("Watch() on such a target = %v, want ErrWatchUnsupported", err)
	}

	instrumented := NewSession(&watchableMemory{testMemory: testMemory{base: 0x1000, data: make([]byte, 64)}})
	if !instrumented.CanWatch() {
		t.Error("a target with store instrumentation said it cannot watch writes")
	}
	if err := instrumented.Watch(0x1000); err != nil {
		t.Errorf("Watch() on such a target = %v, want it accepted", err)
	}
}

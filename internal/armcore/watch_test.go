package armcore

import (
	"context"
	"encoding/binary"
	"testing"
)

const (
	watchCodeBase = uint32(0x10000)
	watchDataBase = uint32(0x20000)
	watchEndPC    = uint32(0x30000)
)

// watchCore lays out a Thumb program that stores to two words from two
// distinct instructions, so a hit has to name the right one, and then returns.
func watchCore(t *testing.T) *Core {
	t.Helper()
	core := NewCore(CoreOptions{MaxSteps: 1_000_000})
	memory := core.Memory()
	if err := memory.Map(watchCodeBase, 1<<16, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(watchDataBase, 1<<16, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	// A loop, not an unrolled run: the point of a watch is that one
	// instruction accounts for many writes, and an unrolled program would
	// report a separate site per store and prove nothing.
	//
	//	str r0,[r1,#0] ; str r0,[r1,#4] ; subs r2,#1 ; bne loop ; bx r3
	code := []uint16{0x6008, 0x6048, 0x3a01, 0xd1fb, 0x4718}
	encoded := make([]byte, len(code)*2)
	for index, instruction := range code {
		binary.LittleEndian.PutUint16(encoded[index*2:], instruction)
	}
	if err := memory.Load(watchCodeBase, encoded); err != nil {
		t.Fatal(err)
	}
	if err := memory.Write(watchDataBase, make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	return core
}

func runWatchProgram(t *testing.T, core *Core) {
	t.Helper()
	initial := NewContext()
	if err := initial.SetPC(watchCodeBase | 1); err != nil {
		t.Fatal(err)
	}
	initial.Registers[0] = 0x5a5a5a5a
	initial.Registers[1] = watchDataBase
	initial.Registers[2] = 6 // loop count
	initial.Registers[3] = watchEndPC
	if _, err := core.Run(context.Background(), NewThread(initial), watchEndPC, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWatchNamesTheInstructionThatWrote(t *testing.T) {
	core := watchCore(t)
	core.Watch(watchDataBase)
	runWatchProgram(t, core)

	hits := core.WatchHits()
	if len(hits) != 1 {
		t.Fatalf("got %d watch hits, want the one instruction that writes %#x: %+v", len(hits), watchDataBase, hits)
	}
	hit := hits[0]
	if hit.Address != watchDataBase {
		t.Errorf("hit is on %#x, want %#x", hit.Address, watchDataBase)
	}
	// The store to the watched word is the loop's first instruction; the one
	// to +4 follows it and must not be reported.
	if hit.PC != watchCodeBase {
		t.Errorf("hit names PC %#x, want the store at %#x", hit.PC, watchCodeBase)
	}
	if hit.Value != 0x5a5a5a5a || hit.Size != 4 {
		t.Errorf("hit recorded %#x in %d bytes, want 0x5a5a5a5a in 4", hit.Value, hit.Size)
	}
	if hit.Count != 6 {
		t.Errorf("hit counted %d writes, want 6", hit.Count)
	}
	if core.WatchHitsOverflowed() {
		t.Error("a two-site program overflowed the hit limit")
	}
}

func TestWatchIgnoresUnwatchedAddresses(t *testing.T) {
	core := watchCore(t)
	core.Watch(watchDataBase + 64) // never written
	runWatchProgram(t, core)

	if hits := core.WatchHits(); len(hits) != 0 {
		t.Fatalf("got %d hits on an address nothing writes: %+v", len(hits), hits)
	}
}

func TestWatchListIsManaged(t *testing.T) {
	core := watchCore(t)
	core.Watch(watchDataBase)
	core.Watch(watchDataBase + 4)
	// Re-watching is what re-arming does; it must not double anything.
	core.Watch(watchDataBase)
	if watches := core.Watches(); len(watches) != 2 || watches[0] != watchDataBase || watches[1] != watchDataBase+4 {
		t.Fatalf("Watches() = %#x, want both addresses once, in order", watches)
	}

	runWatchProgram(t, core)
	if hits := core.WatchHits(); len(hits) != 2 {
		t.Fatalf("got %d hits, want one per watched address: %+v", len(hits), hits)
	}

	core.Unwatch(watchDataBase)
	if watches := core.Watches(); len(watches) != 1 || watches[0] != watchDataBase+4 {
		t.Fatalf("after Unwatch, Watches() = %#x", watches)
	}
	for _, hit := range core.WatchHits() {
		if hit.Address == watchDataBase {
			t.Fatalf("hits still name the unwatched address: %+v", hit)
		}
	}

	core.ClearWatches()
	if len(core.Watches()) != 0 || len(core.WatchHits()) != 0 {
		t.Fatal("ClearWatches left watches or hits behind")
	}
}

// A hit is evidence a value changed, so a store that failed must not make one.
func TestWatchDoesNotRecordARefusedStore(t *testing.T) {
	core := NewCore(CoreOptions{})
	memory := core.Memory()
	if err := memory.Map(watchDataBase, 1<<16, PermissionRead); err != nil {
		t.Fatal(err)
	}
	core.Watch(watchDataBase)

	memory.beginQuantum()
	err := memory.write32(watchDataBase, 0x1234)
	memory.endQuantum()
	if err == nil {
		t.Fatal("a store into read-only memory succeeded")
	}
	if hits := core.WatchHits(); len(hits) != 0 {
		t.Fatalf("a refused store was recorded: %+v", hits)
	}
}

// Host writes are not guest stores: importing a save or patching an image must
// not look like the game touching the address.
func TestWatchIgnoresHostWrites(t *testing.T) {
	core := watchCore(t)
	core.Watch(watchDataBase)
	if err := core.Memory().Write(watchDataBase, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if hits := core.WatchHits(); len(hits) != 0 {
		t.Fatalf("a Host write was recorded as a guest store: %+v", hits)
	}
}

// A long jump restores the whole guest-visible state, not the registers alone:
// the instruction set and the condition flags at the saved point come back with
// them, which register-by-register restoration leaves behind.
func TestSetContextRestoresTheWholeState(t *testing.T) {
	thread := NewThread(NewContext())
	saved := NewContext()
	saved.Registers[4] = 0x1234
	if err := saved.SetPC(0x2001); err != nil { // odd address: Thumb
		t.Fatal(err)
	}
	if !saved.Thumb() {
		t.Fatal("the saved point is not in Thumb state")
	}
	if err := thread.SetContext(saved); err != nil {
		t.Fatalf("SetContext() error = %v", err)
	}
	restored := thread.Context()
	if restored.Registers[4] != 0x1234 || restored.PC() != 0x2000 || !restored.Thumb() {
		t.Errorf("restored = %+v", restored)
	}
}

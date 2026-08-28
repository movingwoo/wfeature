package ktf

import (
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// The detector is only worth having if a write that lands in a released block
// is still visible when the arena hands that block out again, so the test is
// the fault itself: record a block, write into it the way a title that kept
// the pointer would, and ask what the reuse sees.
func TestArenaShadowFindsAWriteIntoAReleasedBlock(t *testing.T) {
	memory := armcore.NewMemory()
	const base uint32 = 0x30000000
	const size = 8 << 10
	if err := memory.Map(base, 64<<10, armcore.PermissionReadWrite); err != nil {
		t.Fatalf("Map() = %v", err)
	}
	// Content the title left behind, which is what a released block holds:
	// the detector's own question is whether these bytes stay put.
	live := make([]byte, size)
	for index := range live {
		live[index] = byte(index)
	}
	if err := memory.Write(base, live); err != nil {
		t.Fatalf("Write() = %v", err)
	}

	shadow := newArenaShadow(base, 64<<10)
	window, ok := shadow.window(base, size)
	if !ok {
		t.Fatal("the shadow does not cover the block")
	}
	if err := memory.Read(base, window); err != nil {
		t.Fatalf("Read() = %v", err)
	}
	current := make([]byte, size)
	if err := memory.Read(base, current); err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if _, written := firstDifference(window, current); written != nil {
		t.Fatalf("an untouched block reported %x", written)
	}

	stale := []byte{0x12, 0x34, 0x56, 0x78}
	if err := memory.Write(base+4096+8, stale); err != nil {
		t.Fatalf("Write() = %v", err)
	}
	if err := memory.Read(base, current); err != nil {
		t.Fatalf("Read() = %v", err)
	}
	offset, written := firstDifference(window, current)
	if offset != 4096+8 {
		t.Fatalf("offset = %d, want %d", offset, 4096+8)
	}
	if len(written) < len(stale) || string(written[:len(stale)]) != string(stale) {
		t.Fatalf("written = % x, want it to begin with % x", written, stale)
	}

	// A block released again is recorded again, so a fault is reported once
	// rather than for the rest of the session.
	if err := memory.Read(base, window); err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if err := memory.Read(base, current); err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if _, written := firstDifference(window, current); written != nil {
		t.Fatalf("a re-recorded block reported %x", written)
	}
}

// The whole point of the copy is that the guest sees a freed block exactly as
// it left it. A title that frees a structure and keeps reading it runs on a
// handset and in a release build, and a debug build has to agree with them.
func TestArenaShadowLeavesAReleasedBlockUntouched(t *testing.T) {
	memory := armcore.NewMemory()
	const base uint32 = 0x30000000
	if err := memory.Map(base, 4<<10, armcore.PermissionReadWrite); err != nil {
		t.Fatalf("Map() = %v", err)
	}
	// A pointer the title keeps a copy of, which is what the fill used to
	// destroy: read back through the stale copy it has to still be the pointer.
	pointer := []byte{0x44, 0x33, 0x22, 0x11}
	if err := memory.Write(base+16, pointer); err != nil {
		t.Fatalf("Write() = %v", err)
	}
	shadow := newArenaShadow(base, 4<<10)
	window, _ := shadow.window(base, 64)
	if err := memory.Read(base, window); err != nil {
		t.Fatalf("Read() = %v", err)
	}
	after := make([]byte, len(pointer))
	if err := memory.Read(base+16, after); err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if string(after) != string(pointer) {
		t.Fatalf("the released block reads % x, want % x", after, pointer)
	}
}

// A range outside the arena is refused rather than answered with a window that
// belongs to something else.
func TestArenaShadowRefusesARangeOutsideTheArena(t *testing.T) {
	shadow := newArenaShadow(0x30000000, 1<<10)
	for _, probe := range []struct {
		name    string
		address uint32
		size    uint64
	}{
		{"before the arena", 0x2fffffff, 4},
		{"past its end", 0x300003ff, 4},
		{"larger than the arena", 0x30000000, 1 << 11},
	} {
		if _, ok := shadow.window(probe.address, probe.size); ok {
			t.Errorf("%s was accepted", probe.name)
		}
	}
}

// The two halves are wired to the arena rather than to a caller, so this walks
// the path a title takes: allocate, release, write into the released block the
// way a title holding a stale pointer would, and allocate again.
func TestArenaShadowReportsAWriteThroughTheArena(t *testing.T) {
	if !backend.DebugBuild() {
		t.Skip("the detector is installed in debug builds alone")
	}
	client, runtime := newTestRuntime(t)
	runtime.installArenaShadow()
	address, ok := runtime.arena.allocate(64)
	if !ok {
		t.Fatal("the arena refused an allocation")
	}
	runtime.arena.release(address, 64)
	if runtime.shadowedBlocks == 0 {
		t.Fatal("the release recorded nothing")
	}
	if err := client.core.Memory().Write(address+8, []byte{0xaa, 0xbb, 0xcc, 0xdd}); err != nil {
		t.Fatal(err)
	}
	if _, ok := runtime.arena.allocate(64); !ok {
		t.Fatal("the arena refused the second allocation")
	}
	if runtime.checkedBlocks == 0 {
		t.Fatal("the reuse checked nothing")
	}
	reported := false
	for event := range runtime.callCounts {
		if strings.HasPrefix(event.text, "arena use after free") {
			reported = true
		}
	}
	if !reported {
		t.Fatal("a write into the released block was not reported")
	}
}

// The copy reaches what the arena has handed out and no further. A session
// uses a fraction of a 64MB region, and reserving the span up front would cost
// more than everything else a debug session holds.
func TestArenaShadowGrowsToWhatWasReleased(t *testing.T) {
	shadow := newArenaShadow(0x30000000, 64<<20)
	if len(shadow.bytes) != 0 {
		t.Fatalf("a fresh shadow holds %d bytes", len(shadow.bytes))
	}
	if _, ok := shadow.window(0x30000000, 1<<10); !ok {
		t.Fatal("the first window was refused")
	}
	if len(shadow.bytes) >= 1<<20 {
		t.Fatalf("a 1KB release grew the copy to %d bytes", len(shadow.bytes))
	}
	// A window that reaches further grows it again, and one past the region is
	// still refused however far the copy has grown.
	if _, ok := shadow.window(0x30100000, 4<<10); !ok {
		t.Fatal("a later window was refused")
	}
	if _, ok := shadow.window(0x30000000+uint32(64<<20)-4, 8); ok {
		t.Fatal("a window crossing the end of the region was accepted")
	}
}

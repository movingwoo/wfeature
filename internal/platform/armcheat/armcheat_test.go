package armcheat

import (
	"slices"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/cheat"
)

const (
	testCode = uint32(0x10000)
	testData = uint32(0x40000)
)

// testCore maps a code and a data region apart from each other and writes a
// word into each, so both have a committed page to report.
func testCore(t *testing.T) *armcore.Core {
	t.Helper()
	core := armcore.NewCore(armcore.CoreOptions{MaxSteps: 1000})
	for _, base := range []uint32{testCode, testData} {
		if err := core.Memory().Map(base, 1<<16, armcore.PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		if err := core.Memory().Write(base, []byte{1, 2, 3, 4}); err != nil {
			t.Fatal(err)
		}
	}
	return core
}

func testLabel(base uint32) (string, bool) {
	if base == testCode {
		return "code", true
	}
	return "data", false
}

// A sweep covers the committed writable regions, each under the name and the
// kind the platform's label gives where it starts.
func TestTargetSweepsTheCommittedRegionsUnderTheirLabels(t *testing.T) {
	core := testCore(t)
	target := Target{Core: func() *armcore.Core { return core }, Label: testLabel}
	regions := target.Regions()
	want := []cheat.Region{
		{Base: testCode, Size: 1 << 12, Label: "code", Code: true},
		{Base: testData, Size: 1 << 12, Label: "data"},
	}
	if !slices.Equal(regions, want) {
		t.Fatalf("regions = %+v, want %+v", regions, want)
	}
}

// The cheat's own writes are not reported to a watch, and the platform's are,
// as the host's.
func TestTargetWatchesThePlatformsWritesButNotItsOwn(t *testing.T) {
	core := testCore(t)
	target := Target{Core: func() *armcore.Core { return core }, Label: testLabel}
	engine := cheat.NewSession(target)
	if !engine.CanWatch() {
		t.Fatal("a target on the core cannot watch")
	}
	if err := engine.Watch(testData); err != nil {
		t.Fatal(err)
	}
	if err := target.WriteMemory(testData, []byte{9, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	read := make([]byte, 4)
	if err := target.ReadMemory(testData, read); err != nil {
		t.Fatal(err)
	}
	if read[0] != 9 {
		t.Fatalf("the cheat's write reads back %v", read)
	}
	if hits, _, _ := engine.WatchHits(); len(hits) != 0 {
		t.Fatalf("the cheat's own write was reported: %+v", hits)
	}
	if err := core.Memory().Write(testData, []byte{5, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	hits, _, err := engine.WatchHits()
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Address != testData || hits[0].Origin != cheat.OriginHost {
		t.Fatalf("the platform's write was reported as %+v, want one host write to %#x", hits, testData)
	}
	if watches := target.Watches(); !slices.Equal(watches, []uint32{testData}) {
		t.Fatalf("watches = %#x", watches)
	}
	target.ClearWatches()
	if watches := target.Watches(); len(watches) != 0 {
		t.Fatalf("watches after clearing = %#x", watches)
	}
}

// The target asks for the core at each call, so a freeze follows a platform
// that hands its client another core, and one the new core cannot hold fails
// by the platform's name rather than silently.
func TestReapplyFollowsTheCurrentCoreAndNamesAFailure(t *testing.T) {
	if err := Reapply(nil, "KTF"); err != nil {
		t.Fatalf("a session without an engine: %v", err)
	}
	current := testCore(t)
	target := Target{Core: func() *armcore.Core { return current }, Label: testLabel}
	engine := cheat.NewSession(target)
	u32, _ := cheat.ParseValueType("u32")
	if _, err := engine.Freeze(testData, u32, 7, "lives"); err != nil {
		t.Fatal(err)
	}
	current = testCore(t)
	if err := Reapply(engine, "KTF"); err != nil {
		t.Fatal(err)
	}
	read := make([]byte, 4)
	if err := current.Memory().Read(testData, read); err != nil {
		t.Fatal(err)
	}
	if read[0] != 7 {
		t.Fatalf("the freeze did not reach the new core: %v", read)
	}
	current = armcore.NewCore(armcore.CoreOptions{MaxSteps: 1000})
	err := Reapply(engine, "KTF")
	if err == nil || !strings.Contains(err.Error(), "KTF cheat freeze failed at 0x40000") {
		t.Fatalf("a freeze the core cannot hold: %v", err)
	}
}

// An image is named by the hash of its bytes; no bytes are no image.
func TestImageHashAndConsole(t *testing.T) {
	if hash := ImageHash(nil); hash != "" {
		t.Fatalf("no image hashes to %q", hash)
	}
	const abc = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if hash := ImageHash([]byte("abc")); hash != abc {
		t.Fatalf("ImageHash(abc) = %q", hash)
	}
	engine := cheat.NewSession(Target{Core: func() *armcore.Core { return testCore(t) }, Label: testLabel})
	if NewConsole(engine, "title", abc) == nil {
		t.Fatal("no console")
	}
	if key := engine.TableKey(); key.Image != abc {
		t.Fatalf("the console keys its tables by %+v, want the image hash", key)
	}
}

package lgt

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

func TestArchiveParsesDescriptorAndModule(t *testing.T) {
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if archive.Descriptor.AID != "0102ABCD" || archive.Descriptor.PID != "PF000001" ||
		archive.Descriptor.MClass != "Fixture" {
		t.Fatalf("descriptor = %+v", archive.Descriptor)
	}
	if owner := SaveOwner(archive.Descriptor); owner != "PF000001" {
		t.Fatalf("SaveOwner() = %q, want the PID", owner)
	}
	if len(archive.Module) == 0 {
		t.Fatal("archive carries no binary.mod")
	}
	if _, ok := archive.Resource("data/hello.txt"); !ok {
		t.Fatal("packaged resource is missing")
	}
	// binary.mod is the executable, not a resource the game reads back.
	if _, ok := archive.Resource(binaryModuleName); ok {
		t.Fatal("binary.mod is exposed as a resource")
	}
}

func TestModuleParsesSectionsAndRejectsOtherFiles(t *testing.T) {
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	module, err := ParseModule(archive.Module)
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}
	if module.Entry != fixtureTextBase {
		t.Fatalf("entry = %#x, want %#x", module.Entry, fixtureTextBase)
	}
	var text, data bool
	for _, section := range module.Sections {
		switch section.Name {
		case ".text":
			text = section.Executable && section.Address == fixtureTextBase
		case ".data":
			data = !section.Executable && section.Address == fixtureDataBase
		}
	}
	if !text || !data {
		t.Fatalf("sections = %+v", module.Sections)
	}

	if _, err := ParseModule([]byte("not an ELF at all")); err == nil {
		t.Fatal("ParseModule() accepted a non-ELF file")
	}
	// A 64-bit or big-endian module is not something an LGT handset ships, so
	// it fails at the header rather than as a wild jump later.
	wrong := append([]byte(nil), archive.Module...)
	wrong[4] = 2
	if _, err := ParseModule(wrong); err == nil || !strings.Contains(err.Error(), "ELF32") {
		t.Fatalf("ParseModule() on an ELF64 header error = %v", err)
	}
}

func TestCletResolvesImportsRegistersAndDrawsThroughTheFramebuffer(t *testing.T) {
	session, err := StartSession(context.Background(), fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20,
	})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}

	// startClet took the screen framebuffer, wrote a red pixel straight into
	// guest memory, and flushed. Reading it back proves the whole path: the
	// import table resolved, the Clet registered, the framebuffer address the
	// platform handed out is writable, and flush re-read it.
	if flushes := session.Flushes(); flushes == 0 {
		t.Fatal("the Clet never flushed the screen")
	}
	frame, width, height, changed := session.Frame()
	if !changed || width != 16 || height != 8 {
		t.Fatalf("Frame() = %dx%d changed=%v", width, height, changed)
	}
	if frame[0] != 0xff || frame[1] != 0x00 || frame[2] != 0x00 {
		t.Fatalf("first pixel = %v, want red from the direct framebuffer write", frame[:4])
	}

	// A second Frame with nothing flushed since reports no change, so a Host
	// that polls does not pay for a conversion per tick.
	if _, _, _, changed := session.Frame(); changed {
		t.Fatal("Frame() reported a change with nothing flushed")
	}
}

func TestCletReceivesKeyEvents(t *testing.T) {
	session, err := StartSession(context.Background(), fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	session.Frame()

	// The fixture's handleCletEvent writes the key code into the second pixel.
	// 0x07e0 is green in RGB565.
	session.SendKey(true, 0x07e0)
	if err := session.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	// Nothing flushed, so the runtime's copy has to be refreshed the way a
	// flush would; ask the guest to flush by ticking again after the write.
	session.SendKey(false, 0x07e0)
	if err := session.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}

	value, err := session.client.readWord(fixtureLastEvent)
	if err != nil {
		t.Fatal(err)
	}
	if value != 0x07e0 {
		t.Fatalf("last event key = %#x, want the key the Host sent", value)
	}
}

func TestUnknownImportTableIsAnErrorRatherThanANullPointer(t *testing.T) {
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{Width: 16, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.importFunction(0x999, 1); err == nil {
		t.Fatal("an unknown import table resolved to something")
	}
	// The Java interface table resolves — a Java title has to get as far as
	// handing its class metadata over for any of it to be readable — and that
	// resolution is what marks the client, so the failure that follows is
	// reported as the Java app it is rather than as whichever slot came next.
	if _, err := client.importFunction(importTableJava, javaSVCLoadClasses); err != nil {
		t.Fatalf("java interface resolution = %v, want a stub", err)
	}
	if !client.javaApplication {
		t.Fatal("resolving the java interface table did not mark the client")
	}
	wrapped := client.asJavaFailure(errors.New("stopped somewhere"))
	if !errors.Is(wrapped, ErrJavaAppUnsupported) ||
		!strings.Contains(wrapped.Error(), "stopped somewhere") {
		t.Fatalf("java failure = %v, want it named as a Java app and keeping the cause", wrapped)
	}
	// An index no reading of this table covers resolves too, and stops the
	// title at the call rather than at the resolution: the arguments are what
	// tell one candidate meaning of a new slot from another, and a refusal at
	// resolution throws them away.
	stub, err := client.importFunction(importTableJava, 0x77)
	if err != nil || stub == 0 {
		t.Fatalf("unknown java interface index = %#x, %v, want a stub", stub, err)
	}
}

func TestFilesPersistThroughTheSaveBoundary(t *testing.T) {
	root := t.TempDir()
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	store := backend.NewDirectorySaveStore(filepath.Join(root, SaveOwner(archive.Descriptor)))
	client, err := Load(archive, Options{Width: 16, Height: 8, SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}

	// A packaged resource reads back without any save behind it.
	if data, ok := client.readFile("data/hello.txt"); !ok || string(data) != "packaged" {
		t.Fatalf("packaged resource = %q, %v", data, ok)
	}
	// A written file outlives the client, and shadows the packaged copy.
	client.writeFile("data/hello.txt", []byte("saved"))
	next, err := Load(archive, Options{Width: 16, Height: 8, SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	if data, ok := next.readFile("data/hello.txt"); !ok || string(data) != "saved" {
		t.Fatalf("saved file = %q, %v", data, ok)
	}
	// A name that escapes the owner directory is refused rather than written
	// somewhere else.
	if _, err := fileSaveKey("../escape"); err == nil {
		t.Fatal("a traversing file name produced a save key")
	}
}

func TestArenaReusesReleasedBlocks(t *testing.T) {
	region := newArena(0x1000, 4096)
	first, ok := region.allocate(64)
	if !ok {
		t.Fatal("first allocation failed")
	}
	second, _ := region.allocate(64)
	if !region.release(first) {
		t.Fatal("release of a live block failed")
	}
	reused, ok := region.allocate(64)
	if !ok || reused != first {
		t.Fatalf("allocate() after release = %#x, want the released block %#x", reused, first)
	}
	// A pointer the arena never handed out must not become allocatable.
	if region.release(0xdeadbeef) {
		t.Fatal("release accepted a pointer the arena never gave out")
	}
	_ = second
}

func TestTickForPacesGuestTimeAgainstTheWallClock(t *testing.T) {
	ctx := context.Background()
	session, err := StartSession(ctx, fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20, Tick: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	// A cheap tick leaves nearly the whole tick to wait out. The Host adding
	// its own fixed pace on top of this is what ran every title at the ratio
	// between its tick cost and that pace rather than at the game's speed.
	wait, err := session.TickFor(ctx)
	if err != nil {
		t.Fatalf("TickFor() error = %v", err)
	}
	if wait <= 0 || wait > 20*time.Millisecond {
		t.Fatalf("TickFor() wait = %v, want most of the 20ms tick", wait)
	}

	// Guest time and wall time advance together over a run: the whole point of
	// the wait is that one tick of the virtual clock costs one tick of real
	// time. The bound is loose because a loaded test machine can only ever be
	// slower, never faster, than the pace.
	before := session.client.clock.now()
	started := time.Now()
	for range 10 {
		wait, err := session.TickFor(ctx)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(wait)
	}
	guest := session.client.clock.now() - before
	wall := time.Since(started)
	if speed := guest.Seconds() / wall.Seconds(); speed > 1.2 {
		t.Fatalf("guest ran at %.2fx wall clock (guest=%v wall=%v), want about 1x", speed, guest, wall)
	}
}

func TestTickForCapsTheDebtALongTickLeavesBehind(t *testing.T) {
	ctx := context.Background()
	session, err := StartSession(ctx, fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20, Tick: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.TickFor(ctx); err != nil {
		t.Fatal(err)
	}

	// A world load runs inside one call and can take seconds. Repaying that as
	// zero-wait ticks would sprint the scene that follows it, so the schedule
	// gives up everything older than one tick.
	session.nextDue = time.Now().Add(-5 * time.Second)
	if _, err := session.TickFor(ctx); err != nil {
		t.Fatal(err)
	}
	if behind := time.Since(session.nextDue); behind > 2*20*time.Millisecond {
		t.Fatalf("the schedule stayed %v behind, want at most one tick of debt", behind)
	}
	wait, err := session.TickFor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if wait > 20*time.Millisecond {
		t.Fatalf("TickFor() wait = %v after a stall, want no more than one tick", wait)
	}
}

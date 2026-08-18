package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/platform/lgt"
)

// sktFixture is the MIDlet that paints into whatever surface it is handed. It
// is read from the platform package's testdata rather than copied: one fixture
// with one behaviour is worth more than two that can drift apart.
//
// It is the Canvas MIDlet in the shape a handset was sent — the JAR beside the
// .msd naming it — because that is how an SKT title actually arrives, and the
// bare JAR on its own is claimed by no vendor now.
func sktFixture(t *testing.T) []byte {
	t.Helper()
	archive, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "canvas-skt.zip"))
	if err != nil {
		t.Fatalf("read the SKT fixture: %v", err)
	}
	return archive
}

func TestInspectNamesTheGameAndItsSaveOwner(t *testing.T) {
	summary, err := Inspect(sktFixture(t))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if summary.Platform != "skt" {
		t.Errorf("platform = %q, want skt", summary.Platform)
	}
	if summary.Name == "" || summary.MainClass == "" {
		t.Errorf("summary = %+v, want a name and a main class", summary)
	}
	// The owner names the save directory, so it has to be answered for every
	// platform or the Host cannot find a game's progress.
	if summary.SaveOwner == "" {
		t.Error("no save owner")
	}
}

// localKTFNativeArchive finds the earlier KTF package in the ignored local
// game directory. Real games are not in Git, so this is opt-in.
func localKTFNativeArchive(t *testing.T) []byte {
	t.Helper()
	if os.Getenv("WFEATURE_KTF_NATIVE_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_KTF_NATIVE_ACCEPTANCE=1 to run the ignored local KTF native package")
	}
	entries, err := os.ReadDir(filepath.Join("..", "..", "var", "games", "ktf"))
	if err != nil {
		t.Skipf("read the local KTF game directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".zip" {
			continue
		}
		data, err := os.ReadFile(filepath.Join("..", "..", "var", "games", "ktf", entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if ktf.IsNativeArchive(data) {
			return data
		}
	}
	t.Skip("no local KTF native package present")
	return nil
}

// The carrier's earlier download package reaches a Host through the same four
// moves as everything else, and this is where that is checked: a Host has no
// switch of its own, so a package the shared layer does not carry is a package
// no Host can run.
func TestTheEarlierKTFPackageIsOnePlatformNeutralSession(t *testing.T) {
	archive := localKTFNativeArchive(t)
	summary, err := Inspect(archive)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if summary.Platform != "ktf" {
		t.Fatalf("platform = %q, want ktf — it is the same vendor in an earlier package", summary.Platform)
	}
	if summary.Name == "" || summary.SaveOwner == "" {
		t.Fatalf("summary = %+v, want a name and a save owner", summary)
	}

	started, err := Start(context.Background(), archive, Options{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer started.Close()
	if !started.Running() {
		t.Fatal("the session is not running after a successful start")
	}
	if width, height := started.Screen(); width != DefaultWidth || height != DefaultHeight {
		t.Errorf("screen = %dx%d, want the handset's %dx%d", width, height, DefaultWidth, DefaultHeight)
	}

	// The title paces itself, so a tick answers with what is left of the
	// interval it asked for. Ticking for that long and no longer is what a
	// Host does with the answer.
	painted := false
	for round := 0; round < 400 && !painted; round++ {
		progress, err := started.Tick(context.Background(), 0)
		if err != nil {
			t.Fatalf("tick %d: %v", round, err)
		}
		if progress.Flushes > 0 {
			painted = true
		}
	}
	if !painted {
		t.Fatal("the title ended no frame")
	}
	frame, width, height, ok := started.Frame()
	if !ok || width != DefaultWidth || height != DefaultHeight || len(frame) != width*height*4 {
		t.Fatalf("frame = %d bytes at %dx%d (%v), want a full handset screen", len(frame), width, height, ok)
	}
	if elapsed, ok := started.GuestElapsed(); !ok || elapsed <= 0 {
		t.Errorf("guest elapsed = %v (%v), want time to have passed", elapsed, ok)
	}
	// A key runs guest code, and the shared vocabulary is what a Host sends.
	for _, action := range []string{KeyPress, KeyRelease} {
		if err := started.SendKey(context.Background(), action, 148); err != nil {
			t.Fatalf("send %s: %v", action, err)
		}
	}
}

func TestInspectRefusesSomethingThatIsNoArchive(t *testing.T) {
	if _, err := Inspect([]byte("not an archive")); err == nil {
		t.Fatal("nonsense bytes were accepted")
	}
}

func TestStartTickAndFrameSpeakOnePlatformNeutralShape(t *testing.T) {
	// A MIDlet has no flush counter of its own — it draws into the surface the
	// Host gave it — so this is the platform where the shared "did the picture
	// change?" answer is actually manufactured.
	running, err := Start(context.Background(), sktFixture(t), Options{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer running.Close()

	if running.Platform() != "skt" {
		t.Errorf("platform = %q", running.Platform())
	}
	if !running.Running() {
		t.Fatal("the session is not running after a successful start")
	}
	// The fixture paints once as it comes up, so there is a frame before the
	// first tick.
	if running.Flushes() == 0 {
		t.Fatal("nothing was flushed by the start")
	}
	frame, width, height, ok := running.Frame()
	if !ok {
		t.Fatal("no frame after the start")
	}
	if width != DefaultWidth || height != DefaultHeight {
		t.Fatalf("frame is %dx%d, want %dx%d", width, height, DefaultWidth, DefaultHeight)
	}
	if len(frame) != width*height*4 {
		t.Fatalf("frame is %d bytes, want %d", len(frame), width*height*4)
	}

	progress, err := running.Tick(context.Background(), 8*time.Millisecond)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if progress.Exited {
		t.Fatal("the session exited on its first tick")
	}
	// A MIDlet has no clock of its own, so the tick has to ask to be paced. A
	// Host that believed "no wait" would spin a core and run the game as fast
	// as the machine allows.
	if progress.Wait != FramePace {
		t.Fatalf("wait = %v, want one frame", progress.Wait)
	}
	if err := running.SendKey(context.Background(), KeyPress, 148); err != nil {
		t.Fatalf("SendKey: %v", err)
	}
	if _, err := running.Tick(context.Background(), 8*time.Millisecond); err != nil {
		t.Fatalf("Tick after a key: %v", err)
	}
	// The key reached the game, which repaints on it.
	if running.Flushes() < 2 {
		t.Errorf("flushes = %d after a key, want the repaint to have counted", running.Flushes())
	}
}

func TestKeyActionsAreAClosedSet(t *testing.T) {
	for _, action := range []string{KeyPress, KeyRelease, KeyRepeat} {
		if _, ok := ktfKeyEventType(action); !ok {
			t.Errorf("the Host vocabulary %q is not translated", action)
		}
	}
	if _, ok := ktfKeyEventType("wiggle"); ok {
		t.Fatal("an unknown key action was translated")
	}
}

func TestKeyCodesTranslateOnlyWhereTheyMust(t *testing.T) {
	// The browser sends MIDP-style codes. A WIPI game compares against
	// different numbers for the direction pad and the soft keys, while digits
	// pass through untouched — translating those would break the keypad.
	for _, test := range []struct {
		from int32
		to   int32
		what string
	}{
		{141, ktf.KeyUp, "up"}, {146, ktf.KeyDown, "down"},
		{142, ktf.KeyLeft, "left"}, {145, ktf.KeyRight, "right"},
		{148, ktf.KeyFire, "fire"},
		// The three soft keys, including the third one a handset carries
		// beside the two under the screen — one title's submenu is on it and
		// the choice that leaves the screen is on the third. The keypad in
		// `web/` sends exactly these three numbers, so what they become is
		// pinned here rather than merely asserted to be something else:
		// `web/keypad.test.mjs` holds the other end of the same contract.
		{6, ktf.KeyLeftSoft, "left soft"},
		{7, ktf.KeyRightSoft, "right soft"},
		{9, ktf.KeyThirdSoft, "third soft"},
	} {
		if got := ktfKeyCode(test.from); got != test.to {
			t.Errorf("code %d became %d, want %d (%s)", test.from, got, test.to, test.what)
		}
	}
	for _, code := range []int32{'0', '9', '*', '#', 48, 57} {
		if got := ktfKeyCode(code); got != code {
			t.Errorf("code %d became %d; digits and symbols pass through", code, got)
		}
	}
	// The send key is the one the keypad has no other way to reach, and the one
	// a title tends to answer with a quick save. It has to arrive as the WIPI
	// code a game compares against rather than as the page's own number.
	if got := ktfKeyCode(10); got != ktf.KeyCall {
		t.Errorf("the send key became %d, want %d", got, ktf.KeyCall)
	}
}

func TestClosingTwiceIsSafe(t *testing.T) {
	// A game that exits closes the session from inside Tick, and the Host
	// closes it again on its way out.
	running, err := Start(context.Background(), sktFixture(t), Options{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	running.Close()
	running.Close()
	if running.Running() {
		t.Fatal("a closed session still reports itself running")
	}
	if _, err := running.Tick(context.Background(), time.Millisecond); err == nil {
		t.Fatal("a closed session ticked")
	}
	if err := running.SendKey(context.Background(), KeyPress, 148); err == nil {
		t.Fatal("a closed session took a key")
	}
	if _, _, _, ok := running.Frame(); ok {
		t.Fatal("a closed session answered with a frame")
	}
}

func TestScaleIsIgnoredWhereTheRuntimeOwnsItsSurface(t *testing.T) {
	// The MIDP runtimes enforce the dimensions they were given, so magnifying
	// their surface would make them reject every frame. The filter belongs to
	// the platforms that hand over a finished picture.
	running, err := Start(context.Background(), sktFixture(t), Options{Scale: 3})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer running.Close()
	_, width, height, ok := running.Frame()
	if !ok {
		t.Fatal("no frame")
	}
	if width != DefaultWidth || height != DefaultHeight {
		t.Fatalf("frame is %dx%d, want the unmagnified %dx%d", width, height, DefaultWidth, DefaultHeight)
	}
}

func TestScreenSizeIsTheHostsToChoose(t *testing.T) {
	running, err := Start(context.Background(), sktFixture(t), Options{Width: 128, Height: 160})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer running.Close()
	_, width, height, ok := running.Frame()
	if !ok {
		t.Fatal("no frame")
	}
	if width != 128 || height != 160 {
		t.Fatalf("frame is %dx%d, want 128x160", width, height)
	}
}

// A game ends when the player picks the option that quits, and the call that
// was running guest code at the time is the one that finds out. That used to
// reach a page as "the game failed", printed over the last frame of a game
// that had finished normally — and the session stayed up, streaming that frame
// for as long as the page was open.
func TestAGuestEndingIsAnEndingRatherThanAFailure(t *testing.T) {
	for _, ending := range []error{ktf.ErrGuestExited, lgt.ErrGuestExited} {
		running := &Session{}
		wrapped := fmt.Errorf("notify KTF card key: %w", ending)
		if err := running.endedOrFailed(wrapped); !errors.Is(err, ErrExited) {
			t.Fatalf("a guest ending answered %v, want ErrExited", err)
		}
		if running.Running() {
			t.Fatal("the session is still running after its game ended")
		}
	}

	running := &Session{}
	failure := errors.New("the core faulted")
	if err := running.endedOrFailed(failure); !errors.Is(err, failure) {
		t.Fatalf("a real failure answered %v, want it kept", err)
	}
	if err := running.endedOrFailed(nil); err != nil {
		t.Fatalf("a call that succeeded answered %v", err)
	}
}

// The cheat engine is reached through the session rather than through KTF(),
// because reaching through KTF() is what kept the engine LGT already had from
// ever answering a browser. The MIDP runtime has no guest address space at all
// and used to answer nil here; it now lays a synthetic one over its object
// graph, so every platform this emulator runs answers an engine.
func TestCheatIsAnsweredWhereThereIsGuestMemoryToSearch(t *testing.T) {
	running, err := Start(context.Background(), sktFixture(t), Options{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer running.Close()
	engine := running.Cheat()
	if engine == nil {
		t.Fatal("a MIDP runtime answered no cheat engine")
	}
	if running.CheatConsole() == nil {
		t.Error("a MIDP runtime answered no cheat console")
	}
	// The regions are the graph: a MIDlet that has started has at least its own
	// class's statics in one, and an engine with nothing to sweep would answer
	// an empty list without failing.
	if len(engine.Regions()) == 0 {
		t.Error("the MIDP engine found nothing to search")
	}
	// A watch names the instruction behind a write, which needs store
	// instrumentation this platform does not have. Saying so is the designed
	// answer; a Host reads it as "no watch control here".
	if engine.CanWatch() {
		t.Error("the MIDP engine claimed it can watch writes")
	}

	// A session that never started answers the same way rather than panicking
	// on the way to finding out which platform it is.
	var missing *Session
	if missing.Cheat() != nil || missing.CheatConsole() != nil {
		t.Error("a nil session answered a cheat engine")
	}
	empty := &Session{}
	if empty.Cheat() != nil || empty.CheatConsole() != nil {
		t.Error("a session with no platform answered a cheat engine")
	}
}

// The MIDP surface is the one a MIDlet's own threads paint into, and those
// threads are goroutines: a paint can land while the Host is still reading the
// last picture. So the surface answers with a copy, and the picture a Host was
// given stays the picture it was given.
func TestCaptureFramebufferAnswersACopy(t *testing.T) {
	surface, err := newCaptureFramebuffer(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	red := []byte{0xff, 0, 0, 0xff, 0xff, 0, 0, 0xff}
	if err := surface.Present(backend.Frame{Width: 2, Height: 1, RGBA: red}); err != nil {
		t.Fatal(err)
	}

	first, width, height := surface.Frame()
	if width != 2 || height != 1 {
		t.Fatalf("frame is %dx%d, want 2x1", width, height)
	}

	blue := []byte{0, 0, 0xff, 0xff, 0, 0, 0xff, 0xff}
	if err := surface.Present(backend.Frame{Width: 2, Height: 1, RGBA: blue}); err != nil {
		t.Fatal(err)
	}
	second, _, _ := surface.Frame()

	if first[0] != 0xff || first[2] != 0 {
		t.Fatalf("presenting again rewrote the frame already handed out: %v", first)
	}
	if second[0] != 0 || second[2] != 0xff {
		t.Fatalf("the second frame is %v, want the blue one", second)
	}
	if &first[0] == &second[0] {
		t.Fatal("two frames share one array")
	}
}

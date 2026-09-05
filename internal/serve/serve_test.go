package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/route"
)

// fakeGuest is a session shaped the way the platforms are: it ticks, it holds
// a screen, and it answers keys from a table.
type fakeGuest struct {
	ticks     int
	width     int
	height    int
	rgba      []byte
	keys      []string
	failAt    int
	parked    time.Duration
	touches   []string
	shots     []string
	diags     []string
	stalledAt int
}

func newFakeGuest() *fakeGuest {
	guest := &fakeGuest{width: 4, height: 2}
	guest.rgba = make([]byte, guest.width*guest.height*4)
	for index := range guest.rgba {
		guest.rgba[index] = 0
	}
	// One opaque white pixel, so the counts are not all zero.
	guest.rgba[0], guest.rgba[1], guest.rgba[2], guest.rgba[3] = 0xff, 0xff, 0xff, 0xff
	// One transparent pixel with colour left under it, which is what the
	// normalization has to erase.
	guest.rgba[4], guest.rgba[5], guest.rgba[6], guest.rgba[7] = 0x11, 0x22, 0x33, 0x00
	return guest
}

func lookupKey(name string) (int32, bool) {
	switch name {
	case "fire":
		return 1, true
	case "up":
		return 2, true
	}
	return 0, false
}

func (guest *fakeGuest) driver() *Driver {
	return &Driver{
		Advance: func(context.Context) (bool, error) {
			guest.ticks++
			if guest.failAt > 0 && guest.ticks == guest.failAt {
				return false, fmt.Errorf("the guest ended")
			}
			return true, nil
		},
		Frame:     func() ([]byte, int, int) { return guest.rgba, guest.width, guest.height },
		Digest:    func() uint64 { return uint64(guest.ticks) },
		Flushes:   func() uint64 { return uint64(guest.ticks) },
		Stalled:   func() bool { return guest.stalledAt > 0 && guest.ticks >= guest.stalledAt },
		LookupKey: lookupKey,
		SendKey: func(_ context.Context, pressed bool, key int32) error {
			guest.keys = append(guest.keys, fmt.Sprintf("%d:%t@%d", key, pressed, guest.ticks))
			return nil
		},
		SendTouch: func(_ context.Context, action string, x, y int) error {
			guest.touches = append(guest.touches, fmt.Sprintf("%s:%d,%d", action, x, y))
			return nil
		},
		Park: func(_ context.Context, hold time.Duration) error {
			guest.parked = hold
			return nil
		},
		Diag: func(path string) error {
			guest.diags = append(guest.diags, path)
			return os.WriteFile(path, []byte("{}"), 0o644)
		},
		Shot: func(path string) error {
			guest.shots = append(guest.shots, path)
			return os.WriteFile(path, []byte("png"), 0o644)
		},
		RunRoute: func(ctx context.Context, script *route.Route) (route.Result, error) {
			runner := &route.Runner{
				Advance: func(context.Context) (bool, error) { guest.ticks++; return true, nil },
				Digest:  func() uint64 { return uint64(guest.ticks) },
				SendKey: func(_ context.Context, pressed bool, key int32) error { return nil },
			}
			return runner.Run(ctx, script)
		},
		DefaultHold: 3,
	}
}

// exchange pushes the lines and returns one answer per line written.
func exchange(t *testing.T, driver *Driver, lines ...string) []Response {
	t.Helper()
	var out strings.Builder
	err := Serve(context.Background(), driver, strings.NewReader(strings.Join(lines, "\n")+"\n"), &out)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var answers []Response
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var response Response
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("answer %q did not parse: %v", line, err)
		}
		answers = append(answers, response)
	}
	return answers
}

func TestABadCommandIsAnsweredAndTheSessionCarriesOn(t *testing.T) {
	guest := newFakeGuest()
	answers := exchange(t, guest.driver(),
		`{"id":1,"cmd":"step","ticks":5}`,
		`{"id":2,"cmd":"nonsense"}`,
		`{"id":3,"cmd":"key","key":"nope"}`,
		`not json at all`,
		`{"id":5,"cmd":"step","ticks":2}`,
	)
	if len(answers) != 5 {
		t.Fatalf("got %d answers, want 5: %+v", len(answers), answers)
	}
	if !answers[0].OK || answers[0].Ticks != 5 || answers[0].Total != 5 {
		t.Fatalf("first step = %+v", answers[0])
	}
	for _, index := range []int{1, 2, 3} {
		if answers[index].OK {
			t.Fatalf("answer %d reported success: %+v", index, answers[index])
		}
		if answers[index].Error == "" {
			t.Fatalf("answer %d has no reason: %+v", index, answers[index])
		}
	}
	// The point of reporting in band: the run those five ticks bought is still
	// there afterwards.
	if !answers[4].OK || answers[4].Total != 7 {
		t.Fatalf("the session did not survive the bad commands: %+v", answers[4])
	}
	if guest.ticks != 7 {
		t.Fatalf("guest ran %d ticks, want 7", guest.ticks)
	}
}

func TestTheRequestIDComesBackOnTheAnswer(t *testing.T) {
	guest := newFakeGuest()
	answers := exchange(t, guest.driver(),
		`{"id":"a","cmd":"screen"}`,
		`{"id":42,"cmd":"step"}`,
		`{"cmd":"step"}`,
	)
	if string(answers[0].ID) != `"a"` {
		t.Fatalf("string id came back as %s", answers[0].ID)
	}
	if string(answers[1].ID) != "42" {
		t.Fatalf("number id came back as %s", answers[1].ID)
	}
	if len(answers[2].ID) != 0 {
		t.Fatalf("a command with no id was answered with %s", answers[2].ID)
	}
}

func TestScreenIdentityIgnoresWhatIsUnderTransparentPixels(t *testing.T) {
	guest := newFakeGuest()
	answers := exchange(t, guest.driver(), `{"cmd":"screen"}`)
	report := answers[0].Screen
	if report == nil {
		t.Fatal("screen answered no report")
	}
	if report.Width != 4 || report.Height != 2 {
		t.Fatalf("geometry = %dx%d", report.Width, report.Height)
	}
	if report.VisiblePixels != 1 || report.NonBlackPixels != 1 {
		t.Fatalf("counts = %d visible, %d non-black", report.VisiblePixels, report.NonBlackPixels)
	}
	first := report.RGBASHA256

	// Change the colour under the transparent pixel. The picture is the same,
	// so the identity must be too.
	guest.rgba[4], guest.rgba[5], guest.rgba[6] = 0x99, 0x88, 0x77
	answers = exchange(t, guest.driver(), `{"cmd":"screen"}`)
	if answers[0].Screen.RGBASHA256 != first {
		t.Fatal("what was left under a transparent pixel changed the screen identity")
	}

	// Change a visible pixel, and it must not be.
	guest.rgba[0] = 0x00
	answers = exchange(t, guest.driver(), `{"cmd":"screen"}`)
	if answers[0].Screen.RGBASHA256 == first {
		t.Fatal("a changed visible pixel left the screen identity alone")
	}
}

func TestScreenIdentityIsTiedToTheGeometry(t *testing.T) {
	// The same bytes in a different-shaped screen are a different picture, so
	// the width and height are hashed before the pixels.
	wide, err := Screen(make([]byte, 16), 4, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	tall, err := Screen(make([]byte, 16), 1, 4, 0)
	if err != nil {
		t.Fatal(err)
	}
	if wide.RGBASHA256 == tall.RGBASHA256 {
		t.Fatal("4x1 and 1x4 hashed the same")
	}
}

func TestKeyHoldsThePressForTheTicksItWasGiven(t *testing.T) {
	guest := newFakeGuest()
	// A press and its release in the same tick is not a press to a title that
	// samples the keypad once a frame, so the hold has to spend ticks.
	answers := exchange(t, guest.driver(), `{"cmd":"key","key":"fire","hold":10}`)
	if !answers[0].OK || answers[0].Ticks != 10 {
		t.Fatalf("key = %+v", answers[0])
	}
	if len(guest.keys) != 2 {
		t.Fatalf("keys = %v", guest.keys)
	}
	if guest.keys[0] != "1:true@0" || guest.keys[1] != "1:false@10" {
		t.Fatalf("the press and release did not straddle the hold: %v", guest.keys)
	}

	// With no hold named, the subcommand's own -hold is what applies.
	guest = newFakeGuest()
	answers = exchange(t, guest.driver(), `{"cmd":"key","key":"up"}`)
	if answers[0].Ticks != 3 {
		t.Fatalf("the default hold was not used: %+v", answers[0])
	}
}

func TestKeyPressAndReleaseCanSpanOtherCommands(t *testing.T) {
	guest := newFakeGuest()
	answers := exchange(t, guest.driver(),
		`{"cmd":"key","key":"up","action":"press"}`,
		`{"cmd":"step","ticks":20}`,
		`{"cmd":"key","key":"up","action":"release"}`,
	)
	for index, answer := range answers {
		if !answer.OK {
			t.Fatalf("answer %d failed: %+v", index, answer)
		}
	}
	if guest.keys[0] != "2:true@0" || guest.keys[1] != "2:false@20" {
		t.Fatalf("the hold did not span the step: %v", guest.keys)
	}
}

func TestAGuestThatEndsLeavesTheSessionReadable(t *testing.T) {
	guest := newFakeGuest()
	guest.failAt = 3
	directory := t.TempDir()
	answers := exchange(t, guest.driver(),
		`{"cmd":"step","ticks":100}`,
		`{"cmd":"step","ticks":5}`,
		`{"cmd":"screen"}`,
		fmt.Sprintf(`{"cmd":"shot","path":%q}`, filepath.Join(directory, "end.png")),
	)
	if !answers[0].Ended || answers[0].EndReason == "" {
		t.Fatalf("the ending was not reported: %+v", answers[0])
	}
	if answers[0].Ticks != 3 {
		t.Fatalf("the step ran on past the ending: %+v", answers[0])
	}
	if !answers[1].Ended {
		t.Fatalf("a step after the ending did not say so: %+v", answers[1])
	}
	// A run that just ended is exactly the one worth looking at.
	if !answers[2].OK || answers[2].Screen == nil {
		t.Fatalf("the screen was unreadable after the ending: %+v", answers[2])
	}
	if !answers[3].OK {
		t.Fatalf("a shot after the ending failed: %+v", answers[3])
	}
	if len(guest.shots) != 1 {
		t.Fatalf("shots = %v", guest.shots)
	}
}

func TestStepStopsWhenNothingIsLeftToRun(t *testing.T) {
	guest := newFakeGuest()
	guest.stalledAt = 4
	// Advance keeps reporting progress, so only Stalled can end the step; a
	// platform that could not tell would spend the whole count.
	driver := guest.driver()
	driver.Advance = func(context.Context) (bool, error) { guest.ticks++; return false, nil }
	answers := exchange(t, driver, `{"cmd":"step","ticks":1000}`)
	if !answers[0].Stalled {
		t.Fatalf("the stall was not reported: %+v", answers[0])
	}
	if answers[0].Ticks != 4 {
		t.Fatalf("the step spent %d ticks on a stalled guest", answers[0].Ticks)
	}
}

func TestPixelReadsTheGuestsOwnCoordinatesAndRefusesOutsideThem(t *testing.T) {
	guest := newFakeGuest()
	answers := exchange(t, guest.driver(),
		`{"cmd":"pixel","x":0,"y":0}`,
		`{"cmd":"pixel","x":9,"y":0}`,
	)
	if answers[0].Pixel == nil || answers[0].Pixel.RGBA != "#ffffffff" {
		t.Fatalf("pixel = %+v", answers[0].Pixel)
	}
	if answers[1].OK || !strings.Contains(answers[1].Error, "outside") {
		t.Fatalf("an off-screen pixel = %+v", answers[1])
	}
}

func TestACapabilityAPlatformLacksIsRefusedByName(t *testing.T) {
	guest := newFakeGuest()
	driver := guest.driver()
	driver.SendTouch, driver.Park, driver.Diag = nil, nil, nil
	answers := exchange(t, driver,
		`{"cmd":"touch","action":"press","x":1,"y":1}`,
		`{"cmd":"park","ms":10}`,
		`{"cmd":"diag","path":"x.json"}`,
	)
	for index, want := range []string{"no pointer", "no park", "-diag"} {
		if answers[index].OK || !strings.Contains(answers[index].Error, want) {
			t.Fatalf("answer %d = %+v, want a refusal mentioning %q", index, answers[index], want)
		}
	}
}

func TestTouchAndParkReachTheSession(t *testing.T) {
	guest := newFakeGuest()
	answers := exchange(t, guest.driver(),
		`{"cmd":"touch","action":"press","x":12,"y":34}`,
		`{"cmd":"touch","action":"release","x":12,"y":34}`,
		`{"cmd":"park","ms":250}`,
		`{"cmd":"touch","action":"poke","x":1,"y":1}`,
	)
	if len(guest.touches) != 2 || guest.touches[0] != "press:12,34" {
		t.Fatalf("touches = %v", guest.touches)
	}
	if guest.parked != 250*time.Millisecond {
		t.Fatalf("parked for %v", guest.parked)
	}
	if answers[3].OK {
		t.Fatalf("an unknown touch action was accepted: %+v", answers[3])
	}
}

func TestRouteReplaysAScriptFromTheSameConnection(t *testing.T) {
	guest := newFakeGuest()
	directory := t.TempDir()
	script := filepath.Join(directory, "way.route")
	if err := os.WriteFile(script, []byte("wait 5\nkey fire\nwait 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(directory, "bad.route")
	if err := os.WriteFile(bad, []byte("key nosuchkey\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	answers := exchange(t, guest.driver(),
		fmt.Sprintf(`{"cmd":"route","path":%q}`, script),
		fmt.Sprintf(`{"cmd":"route","path":%q}`, bad),
		`{"cmd":"step"}`,
	)
	if !answers[0].OK || answers[0].Route == nil || !answers[0].Route.Completed {
		t.Fatalf("route = %+v", answers[0])
	}
	if answers[0].Route.Ticks != 10 {
		t.Fatalf("the route spent %d ticks", answers[0].Route.Ticks)
	}
	if answers[0].Total != 10 {
		t.Fatalf("the route's ticks were not counted into the session: %d", answers[0].Total)
	}
	if answers[1].OK || !strings.Contains(answers[1].Error, "nosuchkey") {
		t.Fatalf("a script with an unknown key = %+v", answers[1])
	}
	if !answers[2].OK {
		t.Fatalf("the session did not survive the bad script: %+v", answers[2])
	}
}

func TestQuitEndsTheSessionAfterAnswering(t *testing.T) {
	guest := newFakeGuest()
	answers := exchange(t, guest.driver(),
		`{"id":1,"cmd":"quit"}`,
		`{"id":2,"cmd":"step","ticks":50}`,
	)
	if len(answers) != 1 || !answers[0].OK || answers[0].Cmd != "quit" {
		t.Fatalf("quit answered %+v", answers)
	}
	if guest.ticks != 0 {
		t.Fatalf("a command after quit still ran: %d ticks", guest.ticks)
	}
}

func TestAnOversizeLineIsTheOneThingThatEndsTheSession(t *testing.T) {
	guest := newFakeGuest()
	var out strings.Builder
	line := `{"cmd":"step","key":"` + strings.Repeat("x", maxCommandLine) + `"}`
	err := Serve(context.Background(), guest.driver(), strings.NewReader(line+"\n"+`{"cmd":"step"}`+"\n"), &out)
	if err == nil {
		t.Fatal("an oversize line did not end the session")
	}
	if !strings.Contains(out.String(), "resynchronized") {
		t.Fatalf("the reason was not reported: %s", out.String())
	}
	if guest.ticks != 0 {
		t.Fatalf("a step ran after the stream desynchronized: %d", guest.ticks)
	}
}

func TestStepBoundsWhatOneCommandCanSpend(t *testing.T) {
	guest := newFakeGuest()
	answers := exchange(t, guest.driver(),
		fmt.Sprintf(`{"cmd":"step","ticks":%d}`, maxStepTicks+1),
		`{"cmd":"step","ticks":-1}`,
	)
	for index, answer := range answers {
		if answer.OK {
			t.Fatalf("answer %d accepted an out-of-range step: %+v", index, answer)
		}
	}
	if guest.ticks != 0 {
		t.Fatalf("a refused step still ran %d ticks", guest.ticks)
	}
}

package ktf

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/gameroot"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/testlocal"
)

// TestLocalKTFNativePackageParses is opt-in because real games are ignored
// local data. It parses without executing anything.
func TestLocalKTFNativePackageParses(t *testing.T) {
	testlocal.Enabled(t, "WFEATURE_KTF_NATIVE_ACCEPTANCE", "parse ignored local KTF native packages")
	for _, path := range localNativePackages(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(path), err)
		}
		archive, err := OpenNative(data)
		if err != nil {
			t.Fatalf("open %s: %v", filepath.Base(path), err)
		}
		t.Logf("%s: module %q %d bytes, id %d, vendor %q, name %q, %d icons, %d numeric records, trailer %v",
			filepath.Base(path), archive.ModuleName, len(archive.Module), archive.Info.ApplicationID,
			archive.Info.Vendor, archive.Info.Name, len(archive.Info.Icons), len(archive.Info.Records), archive.Info.Trailer)
		for _, field := range archive.Info.Fields {
			t.Logf("  header field tag %d extra %d value %#x", field.Tag, field.Extra, field.Value)
		}
		for index, record := range archive.Info.Records {
			t.Logf("  record %d: %#x", index, record)
		}
		for name, contents := range archive.Files {
			t.Logf("  file %s: %d bytes", name, len(contents))
		}
	}
}

// localNativePackages lists the ignored local archives that carry this package
// rather than the descriptor package.
func localNativePackages(t *testing.T) []string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate KTF test source")
	}
	directory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games")
	found := []string{}
	for _, path := range gameroot.Paths(directory, ".zip") {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("read local native candidate archive")
		}
		files, err := readOuterZIP(data)
		if err != nil {
			continue
		}
		if IsNativePackage(files) {
			found = append(found, path)
		}
	}
	if len(found) == 0 {
		t.Skip("no local KTF native package present")
	}
	return found
}

// TestLocalKTFNativePackageRuns boots a local module against the platform half
// and runs the frame the title registers.
//
// It asserts what is established: the start-up protocol completes, the title
// loads the files it names, it asks to be called back, and its frames run and
// draw. It asserts nothing about the slots that are still traps — that list is
// the output, and it is the specification for what implementing this package
// still costs. **Read the log as well as the exit status.**
func TestLocalKTFNativePackageRuns(t *testing.T) {
	testlocal.Enabled(t, "WFEATURE_KTF_NATIVE_ACCEPTANCE", "execute ignored local KTF native modules")
	for _, path := range localNativePackages(t) {
		name := filepath.Base(path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		archive, err := OpenNative(data)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		client, err := LoadNativeClient(archive, armcore.CoreOptions{MaxSteps: localAcceptanceMaxSteps(t)})
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		// A manual clock makes the frame the title asked for cost nothing:
		// this measures what the guest computes, not how long it takes.
		clock := wipicrt.NewManualClock(time.Time{})
		platform := NewNativePlatform(client, archive, clock)
		// The title's music is the only thing a run can check about sound
		// without a person listening, and a count of nothing is what a silent
		// platform produced before the player existed.
		sink := &countingAudioSink{}
		platform.AttachAudio(sink)
		if err := platform.Boot(context.Background()); err != nil {
			t.Fatalf("%s: boot: %v", name, err)
		}
		if platform.FrameInterval() == 0 {
			t.Fatalf("%s: the title registered no frame", name)
		}
		t.Logf("%s: booted, frame every %v", name, platform.FrameInterval())
		for _, open := range platform.FileOpens() {
			t.Logf("  opened %-16q mode %d found=%v", open.Name, open.Mode, open.Found)
		}
		frames := 0
		for round := 0; round < localNativeFrames; round++ {
			clock.Advance(platform.FrameInterval())
			ran, err := platform.Tick(context.Background())
			if err != nil {
				t.Fatalf("%s: frame %d: %v", name, frames, err)
			}
			if ran {
				frames++
			}
		}
		_, presents := platform.Frame()
		t.Logf("%s: %d frames, %d draws, %d frames ended, %d images", name, frames, platform.Draws(), presents, len(platform.images))
		if frames != localNativeFrames {
			t.Errorf("%s: %d of %d frames ran", name, frames, localNativeFrames)
		}
		if platform.Draws() == 0 {
			t.Errorf("%s: the title drew nothing", name)
		}
		if presents == 0 {
			t.Errorf("%s: the title ended no frame", name)
		}
		if platform.Missed() != 0 {
			t.Errorf("%s: %d blits named an image the platform did not build", name, platform.Missed())
		}

		// The route into the game, in the title's own terms: the select key
		// opens the menu from the title screen, again enters the first item,
		// the right key moves to the second of its two choices, and select
		// twice more picks a difficulty and starts. Driving it is what proves
		// the input contract, because every screen after the first is one the
		// title only reaches by being played.
		images, draws := len(platform.images), platform.Draws()
		routeFrames := 0
		for _, step := range []struct {
			key    uint32
			frames int
		}{
			{NativeKeySelect, 60},
			{NativeKeySelect, 60},
			{NativeKeyRight, 30},
			{NativeKeySelect, 120},
			{NativeKeySelect, 400},
		} {
			routeFrames += step.frames
			if err := platform.Key(context.Background(), step.key, true); err != nil {
				t.Fatalf("%s: key %#x down: %v", name, step.key, err)
			}
			if err := platform.Key(context.Background(), step.key, false); err != nil {
				t.Fatalf("%s: key %#x up: %v", name, step.key, err)
			}
			for round := 0; round < step.frames; round++ {
				clock.Advance(platform.FrameInterval())
				if _, err := platform.Tick(context.Background()); err != nil {
					t.Fatalf("%s: frame after key %#x: %v", name, step.key, err)
				}
			}
		}
		t.Logf("%s: after the menu route, %d draws and %d images", name, platform.Draws(), len(platform.images))
		if len(platform.images) <= images {
			t.Errorf("%s: the menu route loaded no further images (%d, was %d)", name, len(platform.images), images)
		}
		// The 2005 module draws its menus' text as runs of rectangles, so its
		// route draws many times what its title screen did. A later module
		// draws its text whole and its title screen in thousands of points a
		// frame, so what its route has to show is that it kept drawing: at
		// least once a frame, on every frame of the route.
		if !archive.AsksForInterfaceVersion() && platform.Draws() <= draws*4 {
			t.Errorf("%s: the menu route drew %d, barely more than the %d before it", name, platform.Draws(), draws)
		}
		if archive.AsksForInterfaceVersion() && platform.Draws()-draws < routeFrames {
			t.Errorf("%s: the menu route drew %d times in %d frames", name, platform.Draws()-draws, routeFrames)
		}

		if sink.messages == 0 {
			t.Errorf("%s: the title played no note", name)
		}
		if platform.ClipRefusals() != 0 {
			t.Errorf("%s: %d clips were refused", name, platform.ClipRefusals())
		}
		t.Logf("%s: %d MIDI messages, %d wave samples, %d status messages %q",
			name, sink.messages, sink.samples, len(platform.Messages()), platform.Messages())

		summary := client.SlotSummary()
		t.Logf("%s: %d calls over %d distinct slots (%d kept in the ordered log)",
			name, client.CallCount(), len(summary), len(client.Calls()))
		for _, entry := range summary {
			mark := "trap"
			if entry.Served {
				mark = "served"
			}
			t.Logf("  %-6s %-20s offset %#04x (slot %3d) called %d times, first from %#x",
				mark, entry.Surface, entry.Offset, entry.Slot, entry.Count, entry.First)
		}
	}
}

// TestLocalKTFNativePackageScreenshot writes what the title draws to a PNG,
// after driving a key route into it. It is the eye on this package: the
// acceptance run above says a title drew and how much, and only a picture says
// whether what it drew is the screen it should be.
//
//	WFEATURE_KTF_NATIVE_ACCEPTANCE=1 \
//	WFEATURE_KTF_NATIVE_SHOT=/tmp/frame.png \
//	WFEATURE_KTF_NATIVE_ROUTE=e035:60,e035:60,e034:30,e035:120,e035:400 \
//	go test ./internal/platform/ktf -run TestLocalKTFNativePackageScreenshot
//
// A route step is a key code in hex and how many of the title's frames to run
// after it. WFEATURE_KTF_NATIVE_FRAMES sets the frames before the route.
func TestLocalKTFNativePackageScreenshot(t *testing.T) {
	testlocal.Enabled(t, "WFEATURE_KTF_NATIVE_ACCEPTANCE", "screenshot the ignored local KTF native package")
	out := testlocal.Value(t, "WFEATURE_KTF_NATIVE_SHOT", "the PNG file to write")
	frames := localNativeFrames
	if value := os.Getenv("WFEATURE_KTF_NATIVE_FRAMES"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("invalid WFEATURE_KTF_NATIVE_FRAMES %q", value)
		}
		frames = parsed
	}
	for _, path := range localNativePackages(t) {
		name := filepath.Base(path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		archive, err := OpenNative(data)
		if err != nil {
			t.Fatal(err)
		}
		client, err := LoadNativeClient(archive, armcore.CoreOptions{MaxSteps: localAcceptanceMaxSteps(t)})
		if err != nil {
			t.Fatal(err)
		}
		clock := wipicrt.NewManualClock(time.Time{})
		platform := NewNativePlatform(client, archive, clock)
		if err := platform.Boot(context.Background()); err != nil {
			t.Fatal(err)
		}
		run := func(count int) {
			for round := 0; round < count; round++ {
				clock.Advance(platform.FrameInterval())
				if _, err := platform.Tick(context.Background()); err != nil {
					t.Fatalf("%s: frame: %v", name, err)
				}
			}
		}
		run(frames)
		for _, step := range strings.Split(os.Getenv("WFEATURE_KTF_NATIVE_ROUTE"), ",") {
			step = strings.TrimSpace(step)
			if step == "" {
				continue
			}
			codeText, framesText, _ := strings.Cut(step, ":")
			code, err := strconv.ParseUint(strings.TrimPrefix(codeText, "0x"), 16, 32)
			if err != nil {
				t.Fatalf("route step %q: %v", step, err)
			}
			hold, err := strconv.Atoi(framesText)
			if err != nil {
				t.Fatalf("route step %q: %v", step, err)
			}
			if err := platform.Key(context.Background(), uint32(code), true); err != nil {
				t.Fatal(err)
			}
			if err := platform.Key(context.Background(), uint32(code), false); err != nil {
				t.Fatal(err)
			}
			run(hold)
		}
		frame, presents := platform.Frame()
		t.Logf("%s: %d draws, %d frames ended, %d images", name, platform.Draws(), presents, len(platform.images))
		file, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, frame); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: wrote %s", name, out)
	}
}

// countingAudioSink counts what a title played without keeping any of it.
type countingAudioSink struct {
	messages int
	samples  int
}

func (sink *countingAudioSink) PlayWave(_ uint8, _ uint32, samples []int16) {
	sink.samples += len(samples)
}

func (sink *countingAudioSink) MIDINoteOn(uint8, uint8, uint8) { sink.messages++ }

func (sink *countingAudioSink) MIDINoteOff(uint8, uint8, uint8) { sink.messages++ }

func (sink *countingAudioSink) MIDIProgramChange(uint8, uint8) { sink.messages++ }

func (sink *countingAudioSink) MIDIControlChange(uint8, uint8, uint8) { sink.messages++ }

func (sink *countingAudioSink) MIDIPitchBend(uint8, uint16) { sink.messages++ }

func (sink *countingAudioSink) MIDISysEx([]byte) { sink.messages++ }

// localNativeFrames is how many of the title's own frames the acceptance run
// drives. It is well past the point the title finishes loading and starts
// drawing, and short enough that the run stays under a second.
const localNativeFrames = 300

package ktf

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The screen a KTF game is told it runs on reaches it by two routes, and they
// have to agree. MC_grpGetDisplayInfo answers the size and a row stride; the
// screen framebuffer record answers a size and a stride of its own, and two
// local titles take the pixel buffer from the record and then write into it
// with the stride the info struct gave them.
//
// Answering one and not the other is not a cosmetic mismatch. Every row after
// the first lands at the wrong offset, so the picture shears into diagonal
// bands — which is exactly what a half-wired screen size produced before this
// was one answer read from one place. See Client.SetScreen and docs/ktf.md.
func TestScreenIsOneAnswerForBothSurfaces(t *testing.T) {
	for _, screen := range []struct {
		name          string
		width, height int
		wantW, wantH  uint32
	}{
		{"the platform's own", 0, 0, runtimeDisplayPixelWidth, runtimeDisplayPixelHeight},
		{"a smaller handset", 176, 220, 176, 220},
		{"a larger handset", 320, 480, 320, 480},
	} {
		t.Run(screen.name, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			client.SetScreen(screen.width, screen.height)
			runtime.currentThread, runtime.currentContext = client.thread, context.Background()

			handle, err := runtime.wipicGetScreenFramebuffer()
			if err != nil {
				t.Fatalf("build the screen framebuffer: %v", err)
			}
			framebuffer, err := runtime.readWIPICFramebuffer(handle)
			if err != nil {
				t.Fatalf("read the screen framebuffer: %v", err)
			}

			info := readDisplayInfo(t, client, runtime)

			if framebuffer.width != screen.wantW || framebuffer.height != screen.wantH {
				t.Errorf("framebuffer is %dx%d, want %dx%d",
					framebuffer.width, framebuffer.height, screen.wantW, screen.wantH)
			}
			if info.width != screen.wantW || info.height != screen.wantH {
				t.Errorf("MC_grpGetDisplayInfo says %dx%d, want %dx%d",
					info.width, info.height, screen.wantW, screen.wantH)
			}
			// The stride is the half that tore. A game reads it from the info
			// struct and writes into the record's buffer, so the record's rows
			// have to be at least as far apart as the info struct claims.
			if info.bpl != 2*screen.wantW {
				t.Errorf("MC_grpGetDisplayInfo stride is %d, want %d", info.bpl, 2*screen.wantW)
			}
			if framebuffer.bpl != info.bpl {
				t.Errorf("framebuffer stride %d and display-info stride %d disagree; rows would land at the wrong offsets",
					framebuffer.bpl, info.bpl)
			}
		})
	}
}

// The Java surface answers the same screen the WIPI-C surface does. A title
// that asks Display.getWidth and one that asks MC_grpGetDisplayInfo are the
// same title on the same handset, so a difference here is a game laying its
// screen out against a size nothing draws at.
func TestJavaDisplayReportsTheSameScreen(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.SetScreen(176, 220)

	width, err := runtimeDisplayWidth(runtime, client.JVM(), nil)
	if err != nil {
		t.Fatal(err)
	}
	height, err := runtimeDisplayHeight(runtime, client.JVM(), nil)
	if err != nil {
		t.Fatal(err)
	}
	gotWidth, err := width.Int32()
	if err != nil {
		t.Fatal(err)
	}
	gotHeight, err := height.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if gotWidth != 176 || gotHeight != 220 {
		t.Errorf("Display reports %dx%d, want 176x220", gotWidth, gotHeight)
	}
}

// A screen only half named is the platform's own rather than half of one: a
// Host that knows a width but not a height has not chosen a handset, and
// running a game on a 176x320 screen nothing shipped for is worse than
// ignoring the request.
func TestHalfAScreenSelectsThePlatformHandset(t *testing.T) {
	for _, half := range []struct{ width, height int }{{176, 0}, {0, 220}, {0, 0}, {-1, -1}} {
		client := &Client{}
		client.SetScreen(half.width, half.height)
		width, height := client.screenSize()
		if width != runtimeDisplayPixelWidth || height != runtimeDisplayPixelHeight {
			t.Errorf("SetScreen(%d, %d) gave %dx%d, want the platform's %dx%d",
				half.width, half.height, width, height,
				runtimeDisplayPixelWidth, runtimeDisplayPixelHeight)
		}
	}
}

// readDisplayInfo calls MC_grpGetDisplayInfo the way a game does and reads the
// struct back out of guest memory.
func readDisplayInfo(t *testing.T, client *Client, runtime *initializationRuntime) wipicFramebuffer {
	t.Helper()
	const words = 9
	address, ok := runtime.arena.allocate(words * 4)
	if !ok {
		t.Fatal("the arena would not hold the display info struct")
	}
	thread := armcore.NewThread(armcore.NewContext())
	if err := thread.SetRegister(1, address); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.wipicGetDisplayInfo(thread); err != nil {
		t.Fatalf("MC_grpGetDisplayInfo: %v", err)
	}
	data := make([]byte, words*4)
	if err := client.core.Memory().Read(address, data); err != nil {
		t.Fatal(err)
	}
	// The struct is {bpp, depth, width, height, bytes per line, ...}; the
	// framebuffer shape is reused here to compare the two side by side.
	return wipicFramebuffer{
		bpp:    binary.LittleEndian.Uint32(data[0:]),
		width:  binary.LittleEndian.Uint32(data[8:]),
		height: binary.LittleEndian.Uint32(data[12:]),
		bpl:    binary.LittleEndian.Uint32(data[16:]),
	}
}

// The earlier package answers the screen through a different pair of surfaces
// — a display record the module reads two halfwords out of, and the image the
// title's blits land in — but the rule is the same one: both come from
// NativePlatform.SetScreen, so a title cannot be told one size and drawn at
// another.
func TestNativeScreenIsOneAnswerForBothSurfaces(t *testing.T) {
	for _, screen := range []struct {
		name          string
		width, height int
		wantW, wantH  int
	}{
		{"the platform's own", 0, 0, runtimeDisplayPixelWidth, runtimeDisplayPixelHeight},
		{"a smaller handset", 176, 220, 176, 220},
	} {
		t.Run(screen.name, func(t *testing.T) {
			platform := newNativeScreenPlatform(t, screen.width, screen.height)

			frame, _ := platform.Frame()
			if frame == nil {
				t.Fatal("the platform built no screen")
			}
			bounds := frame.Bounds()
			if bounds.Dx() != screen.wantW || bounds.Dy() != screen.wantH {
				t.Errorf("the title draws into %dx%d, want %dx%d",
					bounds.Dx(), bounds.Dy(), screen.wantW, screen.wantH)
			}

			address, err := platform.client.Allocate(nativeDisplayRecordSize)
			if err != nil {
				t.Fatal(err)
			}
			thread := armcore.NewThread(armcore.NewContext())
			if err := thread.SetRegister(1, address); err != nil {
				t.Fatal(err)
			}
			if _, err := platform.displayInfo(thread); err != nil {
				t.Fatalf("display info: %v", err)
			}
			record := make([]byte, nativeDisplayRecordSize)
			if err := platform.client.core.Memory().Read(address, record); err != nil {
				t.Fatal(err)
			}
			gotWidth := int(binary.LittleEndian.Uint16(record[0:]))
			gotHeight := int(binary.LittleEndian.Uint16(record[2:]))
			if gotWidth != screen.wantW || gotHeight != screen.wantH {
				t.Errorf("the display record says %dx%d, want %dx%d",
					gotWidth, gotHeight, screen.wantW, screen.wantH)
			}
			if gotWidth != bounds.Dx() || gotHeight != bounds.Dy() {
				t.Errorf("the record says %dx%d and the frame is %dx%d; the title would draw off its own screen",
					gotWidth, gotHeight, bounds.Dx(), bounds.Dy())
			}
		})
	}
}

// newNativeScreenPlatform builds the synthetic platform the native tests use,
// naming the screen before Install so the screen is built from it.
func newNativeScreenPlatform(t *testing.T, width, height int) *NativePlatform {
	t.Helper()
	module := make([]byte, 4)
	binary.LittleEndian.PutUint32(module, 0xe12fff1e) // bx lr, in ARM
	archive := &NativeArchive{
		Info:   NativeInfo{Sections: [][]uint32{{nativePageSize}}},
		Module: module,
	}
	client, err := LoadNativeClient(archive, armcore.CoreOptions{})
	if err != nil {
		t.Fatalf("load synthetic native client: %v", err)
	}
	platform := NewNativePlatform(client, archive, NewManualClock(time.Time{}))
	platform.SetScreen(width, height)
	if err := platform.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}
	return platform
}

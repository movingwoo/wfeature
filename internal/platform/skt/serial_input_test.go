package skt

import (
	_ "embed"
	"testing"
)

//go:embed testdata/serial-input.jar
var serialInputJAR []byte

func TestSerialAnimationPaintConsumesInputBeforeNextUpdate(t *testing.T) {
	archive, err := Open(serialInputJAR)
	if err != nil {
		t.Fatal(err)
	}
	framebuffer := newTestFramebuffer(t, 4, 3)
	runtime, err := Start(archive, Options{Framebuffer: framebuffer})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	for pass := 1; pass <= 4; pass++ {
		if err := runtime.RunPending(); err != nil {
			t.Fatal(err)
		}
		if got := invokeFixtureInt(t, runtime, "SerialInputMIDlet", "runs"); got != int32(pass) {
			t.Fatalf("serial runs = %d, want %d", got, pass)
		}
		// A press and release can both arrive between host ticks.
		for _, event := range []KeyEventType{KeyPressed, KeyReleased} {
			if err := runtime.SendKey(event, KeyCodeFire); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatal(err)
	}
	if got := invokeFixtureInt(t, runtime, "SerialInputMIDlet", "consumed"); got != 4 {
		t.Fatalf("paint consumed %d presses, want 4", got)
	}
	frame, _ := framebuffer.Snapshot()
	assertRGBAPixel(t, frame, 0, 0, []byte{0x33, 0xaa, 0x55, 0xff})
}

func TestSerialRepaintCanBeServicedSynchronously(t *testing.T) {
	archive, err := Open(serialInputJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	if _, err := runtime.VM.InvokeStatic("SerialInputMIDlet", "synchronous", "()V"); err != nil {
		t.Fatal(err)
	}
	before := invokeFixtureInt(t, runtime, "SerialInputMIDlet", "paints")
	if err := runtime.RunPending(); err != nil {
		t.Fatal(err)
	}
	if got := invokeFixtureInt(t, runtime, "SerialInputMIDlet", "paints"); got != before+1 {
		t.Fatalf("synchronous paint count = %d, want %d", got, before+1)
	}
}

func TestThrowingSerialCallbackDoesNotDeferLaterInputRepaints(t *testing.T) {
	archive, err := Open(serialInputJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, testRuntimeOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	if _, err := runtime.VM.InvokeStatic("SerialInputMIDlet", "fail", "()V"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatal(err)
	}
	// Consume the deferred paint, then request another outside the failed
	// Runnable. It must be eligible for ordinary event dispatch immediately.
	if err := runtime.paintPendingCanvas(); err != nil {
		t.Fatal(err)
	}
	before := invokeFixtureInt(t, runtime, "SerialInputMIDlet", "paints")
	if _, err := runtime.VM.InvokeStatic("SerialInputMIDlet", "repaint", "()V"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SendKey(KeyPressed, KeyCodeFire); err != nil {
		t.Fatal(err)
	}
	if got := invokeFixtureInt(t, runtime, "SerialInputMIDlet", "paints"); got != before+1 {
		t.Fatalf("paint after failed Runnable = %d, want %d", got, before+1)
	}
}

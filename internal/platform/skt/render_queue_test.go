package skt

import "testing"

func TestSynchronousRepaintsDoNotAccumulateStaleEvents(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3)})
	if err != nil {
		t.Fatal(err)
	}
	// A guest worker services its own paints while the Host is waiting for
	// input. More calls than the queue limit must still leave room for input.
	for index := 0; index < 2048; index++ {
		if _, err := runtime.VM.InvokeStatic("CanvasMIDlet", "requestSynchronousRepaint", "()V"); err != nil {
			t.Fatalf("synchronous repaint %d: %v", index, err)
		}
	}
	if paints := invokeFixtureInt(t, runtime, "CanvasMIDlet", "paintCount"); paints != 2049 {
		t.Fatalf("paints = %d, want 2049", paints)
	}
	if err := runtime.SendKey(KeyPressed, KeyCodeFire); err != nil {
		t.Fatal(err)
	}
	if events := invokeFixtureInt(t, runtime, "CanvasMIDlet", "keyEvents"); events != 1 {
		t.Fatalf("input was not delivered: %d", events)
	}
	if _, err := runtime.VM.InvokeStatic("CanvasMIDlet", "requestPartialRepaint", "()V"); err != nil {
		t.Fatal(err)
	}
	before := invokeFixtureInt(t, runtime, "CanvasMIDlet", "paintCount")
	if err := runtime.RunPending(); err != nil {
		t.Fatal(err)
	}
	if after := invokeFixtureInt(t, runtime, "CanvasMIDlet", "paintCount"); after != before+1 {
		t.Fatalf("pending repaint count = %d, want %d", after, before+1)
	}
}

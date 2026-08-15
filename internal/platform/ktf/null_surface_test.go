package ktf

import "testing"

// A drawing call handed a null frame buffer draws nothing. The WIPI C API is
// C and the handle is a pointer, so a null one is a title saying it has no
// surface there — which one does, for the few frames between leaving its world
// and arriving at its main menu: the scene's images are already released and
// its frame timer is still running. Failing the call there ended the session
// on the title's own "return to the main menu".
//
// A handle that is not null and not a frame buffer still fails. That one is a
// bug rather than a state, and the two have to stay distinguishable.
func TestNullDrawSurfaceDrawsNothingAndAGarbageOneStillFails(t *testing.T) {
	_, runtime := newTestRuntime(t)

	if _, ok, err := runtime.readWIPICDrawSurface(0, "draw image source"); err != nil || ok {
		t.Fatalf("a null surface answered ok=%v, err=%v; want false, nil", ok, err)
	}
	if count := runtime.diagnosticCounts()["grp draw image source on a null surface"]; count != 1 {
		t.Fatalf("the null draw was counted %d times, want 1 — a report has to show it", count)
	}

	handle, err := runtime.wipicGetScreenFramebuffer()
	if err != nil {
		t.Fatal(err)
	}
	surface, ok, err := runtime.readWIPICDrawSurface(handle, "draw image destination")
	if err != nil || !ok {
		t.Fatalf("the screen answered ok=%v, err=%v; want true, nil", ok, err)
	}
	if surface.width == 0 || surface.height == 0 {
		t.Fatalf("the screen surface came back empty: %dx%d", surface.width, surface.height)
	}

	// Not null, not a frame buffer: still an error.
	if _, _, err := runtime.readWIPICDrawSurface(handle+2, "draw image source"); err == nil {
		t.Fatal("an unaligned handle was accepted as a surface")
	}
}

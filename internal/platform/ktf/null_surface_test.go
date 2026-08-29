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

// A surface the guest destroyed and drew again draws nothing rather than
// ending the run. On a handset the freed block still holds the picture, so the
// title draws its old image and carries on; here the arena has already handed
// the span to something else, and the record reads back as whatever now lives
// there. One title destroys an image and blits it on a later frame, and the
// depth this platform read out of it was a heap address.
func TestADestroyedDrawSurfaceDrawsNothing(t *testing.T) {
	_, runtime := newTestRuntime(t)

	handle, err := runtime.newWIPICFramebufferRecord(4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := runtime.readWIPICDrawSurface(handle, "draw image source"); err != nil || !ok {
		t.Fatalf("a live surface answered ok=%v, err=%v; want true, nil", ok, err)
	}

	runtime.destroyWIPICFramebufferRecord(handle)
	// Whatever the arena hands out next may or may not land here, so the
	// record is overwritten deliberately: what is being pinned is the answer
	// to a record that no longer decodes, not the arena's reuse order.
	if err := runtime.clearGuestRange(handle, 64); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := runtime.readWIPICDrawSurface(handle, "draw image source"); err != nil || ok {
		t.Fatalf("a destroyed surface answered ok=%v, err=%v; want false, nil", ok, err)
	}
	if count := runtime.diagnosticCounts()["grp draw image source on a destroyed surface"]; count != 1 {
		t.Fatalf("the destroyed draw was counted %d times, want 1 — a report has to show it", count)
	}

	// A block allocated at that address again is live, and a record that does
	// not decode there is a bug once more rather than a release.
	reissued, err := runtime.allocateWIPIC(32)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.wasReleasedWIPIC(reissued) {
		t.Fatal("an address the arena reissued is still remembered as released")
	}
}

// The bound on a surface is what it costs, not how long its sides are. A
// per-dimension cap of 2048 refused a title's 2464x32 sprite strip, which is
// 154KB, while allowing 2048x2048, which is 8MB.
func TestASurfaceIsBoundedByItsCostRatherThanItsSides(t *testing.T) {
	_, runtime := newTestRuntime(t)

	if _, err := runtime.newWIPICFramebufferRecord(2464, 32); err != nil {
		t.Fatalf("a 2464x32 strip was refused: %v", err)
	}
	if _, err := runtime.newWIPICFramebufferRecord(4096, 4096); err == nil {
		t.Fatal("a 32MB surface was accepted")
	}
	if _, err := runtime.newWIPICFramebufferRecord(0, 8); err == nil {
		t.Fatal("an empty surface was accepted")
	}
}

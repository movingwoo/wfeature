package lgt

import "testing"

// A route waits on what the screen is doing — steady, or changed — and asking
// that question every tick through Frame would convert and copy the whole
// screen each time only to throw it away. The digest reads the display in
// place, so it has to answer over the pixels rather than over the converted
// frame no Host asked for.
func TestFrameDigestFollowsTheLCD(t *testing.T) {
	client := &Client{screen: &framebuffer{width: 4, height: 2, pixels: make([]uint16, 8), screen: true}}
	client.present()
	session := &Session{client: client}

	steady := session.FrameDigest()
	if steady != session.FrameDigest() {
		t.Fatal("a screen that did not change answered two digests")
	}
	client.screen.pixels[5] = 0x07e0
	client.present()
	if changed := session.FrameDigest(); changed == steady {
		t.Fatal("a screen that changed answered the same digest")
	}
	if (&Session{}).FrameDigest() != 0 {
		t.Fatal("a session with no client answered a digest")
	}
}

// **What a Host sees is what the last flush put on the panel**, not what the
// framebuffer holds when it asks. A title that flushes and then starts the
// next frame into the same surface would otherwise be read half-drawn — and
// one local title fills the screen, flushes, and immediately fills it black,
// so every frame a Host took from it was the black one.
func TestTheDisplayDoesNotFollowTheFramebufferUntilAFlush(t *testing.T) {
	client := &Client{screen: &framebuffer{width: 4, height: 2, pixels: make([]uint16, 8), screen: true}}
	client.present()
	session := &Session{client: client}

	flushed := session.FrameDigest()
	client.screen.pixels[3] = 0xffff
	if session.FrameDigest() != flushed {
		t.Fatal("drawing into the framebuffer changed the display before a flush")
	}
	client.present()
	if session.FrameDigest() == flushed {
		t.Fatal("a flush did not put the framebuffer on the display")
	}
}

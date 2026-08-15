package lgt

import "testing"

// A route waits on what the screen is doing — steady, or changed — and asking
// that question every tick through Frame would convert and copy the whole
// screen each time only to throw it away. The digest reads the LCD in place,
// so it has to answer over the pixels rather than over the converted frame no
// Host asked for.
func TestFrameDigestFollowsTheLCD(t *testing.T) {
	client := &Client{screen: &framebuffer{width: 4, height: 2, pixels: make([]uint16, 8), screen: true}}
	session := &Session{client: client}

	steady := session.FrameDigest()
	if steady != session.FrameDigest() {
		t.Fatal("a screen that did not change answered two digests")
	}
	client.screen.pixels[5] = 0x07e0
	if changed := session.FrameDigest(); changed == steady {
		t.Fatal("a screen that changed answered the same digest")
	}
	if (&Session{}).FrameDigest() != 0 {
		t.Fatal("a session with no client answered a digest")
	}
}

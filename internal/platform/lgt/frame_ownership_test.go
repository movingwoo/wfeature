package lgt

import "testing"

// A Host does things with a frame that take longer than the gap to the next
// one: this project's server hands it to another goroutine to compress. That
// is only safe if the bytes are the Host's own, so the frame it was given must
// not change when the next one is converted.
func TestFrameBelongsToTheCaller(t *testing.T) {
	client := &Client{
		screen:       &framebuffer{width: 2, height: 1, pixels: make([]uint16, 2), screen: true},
		frameRGBA:    make([]byte, 2*1*4),
		framePending: true,
	}
	session := &Session{client: client}

	client.screen.pixels[0] = 0xf800 // red
	first, _, _, ok := session.Frame()
	if !ok {
		t.Fatal("a pending frame was not answered")
	}
	if first[0] != 0xff || first[1] != 0 {
		t.Fatalf("the first frame is %v, want a red first pixel", first[:4])
	}

	client.screen.pixels[0] = 0x001f // blue
	client.framePending = true
	second, _, _, _ := session.Frame()

	if second[2] != 0xff {
		t.Fatalf("the second frame is %v, want a blue first pixel", second[:4])
	}
	if first[0] != 0xff || first[2] != 0 {
		t.Fatalf("converting the next frame rewrote the last one: %v", first[:4])
	}
	if &first[0] == &second[0] {
		t.Fatal("two frames share one array")
	}
}

package hqx

import (
	"hash/fnv"
	"math/rand"
	"testing"
)

// The reference comparison in reference_test.go is the real correctness check,
// but it needs a Rust toolchain and the original source. These lock the same
// behaviour in so an ordinary test run still catches a regression: the digests
// below were taken from the implementation on the day it was verified against
// the reference pixel for pixel.
func TestScaleDigestsAreStable(t *testing.T) {
	for _, testCase := range []struct {
		factor int
		want   uint64
	}{
		{2, 0x36f13401bac9f85d},
		{3, 0x625ce95bb9d4fd24},
		{4, 0x7fcafe34b32ac1f4},
	} {
		scaled, err := Scale(digestImage(), 32, 32, testCase.factor)
		if err != nil {
			t.Fatalf("hq%dx: %v", testCase.factor, err)
		}
		if got := digest(scaled); got != testCase.want {
			t.Errorf("hq%dx digest = %#016x, want %#016x", testCase.factor, got, testCase.want)
		}
	}
}

func TestScaleAnswersTheMagnifiedSize(t *testing.T) {
	for _, factor := range []int{2, 3, 4} {
		scaled, err := Scale(make([]uint32, 7*5), 7, 5, factor)
		if err != nil {
			t.Fatal(err)
		}
		if want := 7 * factor * 5 * factor; len(scaled) != want {
			t.Errorf("hq%dx of 7x5 produced %d pixels, want %d", factor, len(scaled), want)
		}
	}
}

// A flat image has no edges to reshape, so every output pixel is the input
// color. This catches a case that blends where it should copy.
func TestFlatImageScalesFlat(t *testing.T) {
	const color = uint32(0xff3366cc)
	source := make([]uint32, 16*16)
	for index := range source {
		source[index] = color
	}
	for _, factor := range []int{2, 3, 4} {
		scaled, err := Scale(source, 16, 16, factor)
		if err != nil {
			t.Fatal(err)
		}
		for index, pixel := range scaled {
			if pixel != color {
				t.Fatalf("hq%dx turned a flat image into %#08x at %d", factor, pixel, index)
			}
		}
	}
}

// Every output pixel has to be written. An unwritten one keeps the zero value,
// which against an opaque source is a transparent hole — the exact shape a
// mistranslated case would take.
func TestEveryOutputPixelIsWritten(t *testing.T) {
	source := digestImage()
	for _, factor := range []int{2, 3, 4} {
		scaled, err := Scale(source, 32, 32, factor)
		if err != nil {
			t.Fatal(err)
		}
		for index, pixel := range scaled {
			if pixel>>24 == 0 {
				t.Fatalf("hq%dx left pixel %d unwritten (%#08x)", factor, index, pixel)
			}
		}
	}
}

func TestScaleRejectsBadInput(t *testing.T) {
	if _, err := Scale(make([]uint32, 4), 2, 2, 5); err == nil {
		t.Error("Scale accepted factor 5")
	}
	if _, err := Scale(make([]uint32, 4), 0, 2, 2); err == nil {
		t.Error("Scale accepted a zero width")
	}
	if _, err := Scale(make([]uint32, 3), 2, 2, 2); err == nil {
		t.Error("Scale accepted a source too small for its dimensions")
	}
	if _, _, _, err := ScaleRGBA(make([]byte, 3), 2, 2, 2); err == nil {
		t.Error("ScaleRGBA accepted a byte count that is not width*height*4")
	}
}

func TestScaleRGBAPreservesChannelOrder(t *testing.T) {
	// A flat image keeps its color, so the byte order is visible in the output.
	source := []byte{0x11, 0x22, 0x33, 0xff, 0x11, 0x22, 0x33, 0xff, 0x11, 0x22, 0x33, 0xff, 0x11, 0x22, 0x33, 0xff}
	scaled, width, height, err := ScaleRGBA(source, 2, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if width != 4 || height != 4 {
		t.Fatalf("ScaleRGBA answered %dx%d, want 4x4", width, height)
	}
	for offset := 0; offset+3 < len(scaled); offset += 4 {
		if scaled[offset] != 0x11 || scaled[offset+1] != 0x22 || scaled[offset+2] != 0x33 || scaled[offset+3] != 0xff {
			t.Fatalf("pixel at %d is %v, want [17 34 51 255]", offset/4, scaled[offset:offset+4])
		}
	}
}

// digestImage is deterministic noise over a small palette, which reaches most
// of the decision table.
func digestImage() []uint32 {
	palette := []uint32{0xff000000, 0xffffffff, 0xffe04020, 0xff2040e0, 0xff20c040}
	random := rand.New(rand.NewSource(7))
	pixels := make([]uint32, 32*32)
	for index := range pixels {
		pixels[index] = palette[random.Intn(len(palette))]
	}
	return pixels
}

func digest(pixels []uint32) uint64 {
	hash := fnv.New64a()
	var word [4]byte
	for _, pixel := range pixels {
		word[0], word[1], word[2], word[3] = byte(pixel), byte(pixel>>8), byte(pixel>>16), byte(pixel>>24)
		_, _ = hash.Write(word[:])
	}
	return hash.Sum64()
}

// A Scaler keeps its working buffers between frames, which is only safe while
// every one of them is written from end to end before it is read. Nothing in
// the filter says so out loud, so this says it: a Scaler magnifying a stream
// of different pictures has to answer exactly what a fresh one would, picture
// for picture. A plane that carried a pixel of the last frame forward would
// show up on the second image and on nothing else.
func TestAReusedScalerAnswersWhatAFreshOneDoes(t *testing.T) {
	pictures := make([][]byte, 4)
	random := rand.New(rand.NewSource(9))
	for index := range pictures {
		picture := make([]byte, 24*16*4)
		for offset := range picture {
			// Flat runs with detail in them, which is the shape a game frame
			// has and the shape the pattern table actually branches on.
			if offset%7 == 0 {
				picture[offset] = byte(random.Intn(256))
			} else {
				picture[offset] = byte(offset * 3)
			}
		}
		pictures[index] = picture
	}

	for _, factor := range []int{2, 3, 4} {
		var scaler Scaler
		for index, picture := range pictures {
			reused, width, height, err := scaler.ScaleRGBA(picture, 24, 16, factor)
			if err != nil {
				t.Fatalf("hq%dx picture %d through a reused scaler: %v", factor, index, err)
			}
			fresh, freshWidth, freshHeight, err := ScaleRGBA(picture, 24, 16, factor)
			if err != nil {
				t.Fatalf("hq%dx picture %d through a fresh scaler: %v", factor, index, err)
			}
			if width != freshWidth || height != freshHeight {
				t.Fatalf("hq%dx picture %d is %dx%d reused and %dx%d fresh",
					factor, index, width, height, freshWidth, freshHeight)
			}
			for offset := range fresh {
				if reused[offset] != fresh[offset] {
					t.Fatalf("hq%dx picture %d differs at byte %d: %#x reused, %#x fresh",
						factor, index, offset, reused[offset], fresh[offset])
				}
			}
		}
	}
}

// The picture a Scaler answers with leaves for a Host to hold — this project's
// server hands it to another goroutine to compress — so it cannot be one of
// the buffers the next frame writes over.
func TestAScalerDoesNotReuseThePictureItAnsweredWith(t *testing.T) {
	picture := make([]byte, 8*8*4)
	for offset := range picture {
		picture[offset] = byte(offset)
	}
	var scaler Scaler

	first, _, _, err := scaler.ScaleRGBA(picture, 8, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	held := append([]byte(nil), first...)

	for offset := range picture {
		picture[offset] = byte(255 - offset)
	}
	second, _, _, err := scaler.ScaleRGBA(picture, 8, 8, 2)
	if err != nil {
		t.Fatal(err)
	}

	if &first[0] == &second[0] {
		t.Fatal("two frames were magnified into one array")
	}
	for offset := range held {
		if first[offset] != held[offset] {
			t.Fatalf("magnifying the next frame rewrote the last one at byte %d", offset)
		}
	}
}

// A scaler that has been magnifying one size has buffers sized for it, and a
// window resize or a platform with a different screen must not be answered out
// of them.
func TestAScalerFollowsAChangeOfSize(t *testing.T) {
	var scaler Scaler
	large := make([]byte, 16*16*4)
	for offset := range large {
		large[offset] = byte(offset)
	}
	if _, _, _, err := scaler.ScaleRGBA(large, 16, 16, 4); err != nil {
		t.Fatal(err)
	}

	small := make([]byte, 4*4*4)
	for offset := range small {
		small[offset] = byte(offset * 5)
	}
	reused, width, height, err := scaler.ScaleRGBA(small, 4, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	if width != 8 || height != 8 {
		t.Fatalf("a 4x4 picture at hq2x came out %dx%d", width, height)
	}
	if len(reused) != 8*8*4 {
		t.Fatalf("the picture is %d bytes, want %d", len(reused), 8*8*4)
	}
	fresh, _, _, err := ScaleRGBA(small, 4, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	for offset := range fresh {
		if reused[offset] != fresh[offset] {
			t.Fatalf("a resized scaler differs at byte %d", offset)
		}
	}
}

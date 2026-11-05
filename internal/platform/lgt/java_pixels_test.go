package lgt

import (
	"encoding/binary"
	"testing"
)

const (
	javaGetPixelKey     = javaGraphicsClass + ".getPixel(II)I"
	javaGetRGBPixelsKey = javaGraphicsClass + ".getRGBPixels(IIII[III)V"
	javaSetRGBPixelsKey = javaGraphicsClass + ".setRGBPixels(IIII[III)V"
	javaGetPixelsKey    = javaGraphicsClass + ".getPixels(IIII[BII)V"
	javaSetPixelsKey    = javaGraphicsClass + ".setPixels(IIII[BII)V"
)

// screenGraphicsFixture is a Graphics over the fixture client's screen, which
// is 16 by 8.
func screenGraphicsFixture(t *testing.T) (*Client, *framebuffer, uint32) {
	t.Helper()
	client := fixtureClient(t)
	screen, err := client.screenSurface()
	if err != nil {
		t.Fatal(err)
	}
	graphics, err := client.newJavaGraphics(screen.handle)
	if err != nil {
		t.Fatal(err)
	}
	return client, screen, graphics
}

// javaWord is a Java int as the argument word it is passed in.
func javaWord(value int32) uint32 { return uint32(value) }

// newTestIntArray is an int[] this platform built, holding the values.
func newTestIntArray(t *testing.T, client *Client, values ...uint32) uint32 {
	t.Helper()
	array, err := client.newJavaIntArray(values)
	if err != nil {
		t.Fatal(err)
	}
	return array
}

// testArrayWords reads an int[] back.
func testArrayWords(t *testing.T, client *Client, array uint32) []uint32 {
	t.Helper()
	block, length, err := client.javaArrayBlock(array)
	if err != nil {
		t.Fatal(err)
	}
	words := make([]uint32, length)
	for index := range words {
		if words[index], err = client.readWord(block + javaArrayLengthWords*4 + uint32(index)*4); err != nil {
			t.Fatal(err)
		}
	}
	return words
}

// getPixel reads one point back as 0x00RRGGBB, in the Graphics' own
// coordinates, without regard to the clip, and zero off the surface — KTF's
// answers.
func TestJavaGetPixelReadsAPointAsRGB(t *testing.T) {
	client, screen, graphics := screenGraphicsFixture(t)
	screen.pixels[2*screen.width+3] = rgb565(0xff, 0x80, 0x10)
	if got := mustJavaCall(t, client, javaGetPixelKey, graphics, 3, 2); got != 0xff8210 {
		t.Errorf("getPixel(3, 2) = %#x, want 0xff8210, each channel widened as MC_grpGetRGBFromPixel widens it", got)
	}
	mustJavaCall(t, client, javaGraphicsClass+".translate(II)V", graphics, 1, 1)
	mustJavaCall(t, client, javaGraphicsClass+".setClip(IIII)V", graphics, 10, 10, 1, 1)
	if got := mustJavaCall(t, client, javaGetPixelKey, graphics, 2, 1); got != 0xff8210 {
		t.Errorf("getPixel(2, 1) after translate(1, 1) = %#x, want the same point", got)
	}
	for _, point := range [][2]int32{{-2, 0}, {15, 0}, {0, 7}, {-0x80000000, 0x7fffffff}} {
		if got := mustJavaCall(t, client, javaGetPixelKey, graphics, uint32(point[0]), uint32(point[1])); got != 0 {
			t.Errorf("getPixel%v off the surface = %#x, want zero", point, got)
		}
	}
}

// The RGB pair moves a rectangle of 0x00RRGGBB words between the surface and an
// int[], from an offset and with the last argument as bytes per line. Neither
// side is clipped, as on KTF; what lies off the surface reads as zero and takes
// no write; and a write is the runtime's copy of the surface from then on.
func TestJavaRGBPixelsMoveARectangle(t *testing.T) {
	client, screen, graphics := screenGraphicsFixture(t)
	before := append([]uint16(nil), screen.pixels...)
	mustJavaCall(t, client, javaGraphicsClass+".setClip(IIII)V", graphics, 0, 0, 1, 1)
	// Two rows of three, eight bytes of pitch past the twelve a row needs: the
	// rows start at 1 and at 6 of the array.
	source := newTestIntArray(t, client,
		0xdead, 0xff0000, 0x00ff00, 0x0000ff, 0xdead, 0xdead, 0xffffff, 0x808080, 0x123456, 0xdead)
	mustJavaCall(t, client, javaSetRGBPixelsKey, graphics, 2, 1, 3, 2, source, 1, 20)
	want := map[[2]int]uint16{
		{2, 1}: 0xf800, {3, 1}: 0x07e0, {4, 1}: 0x001f,
		{2, 2}: 0xffff, {3, 2}: 0x8410, {4, 2}: 0x11aa,
	}
	for y := 0; y < screen.height; y++ {
		for x := 0; x < screen.width; x++ {
			expected, written := want[[2]int{x, y}]
			if !written {
				expected = before[y*screen.width+x]
			}
			if got := screen.pixels[y*screen.width+x]; got != expected {
				t.Fatalf("pixel (%d, %d) = %#x, want %#x", x, y, got, expected)
			}
		}
	}
	if !screen.drawnHere {
		t.Error("a write did not make the runtime's copy of the surface the correct one")
	}

	// Read the same rectangle back, translated, from the start of an array of
	// rows of exactly its width; the words past it are left alone.
	mustJavaCall(t, client, javaGraphicsClass+".translate(II)V", graphics, javaWord(-1), 0)
	target := newTestIntArray(t, client, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7)
	mustJavaCall(t, client, javaGetRGBPixelsKey, graphics, 3, 1, 3, 2, target, 0, 0)
	got := testArrayWords(t, client, target)
	wantWords := []uint32{0xff0000, 0x00ff00, 0x0000ff, 0xffffff, 0x848284, 0x103452, 7, 7, 7, 7}
	for index := range wantWords {
		if got[index] != wantWords[index] {
			t.Fatalf("getRGBPixels read %#x, want %#x", got, wantWords)
		}
	}
	// A row off the left edge of the surface reads as zero.
	mustJavaCall(t, client, javaGetRGBPixelsKey, graphics, javaWord(-1), 1, 2, 1, target, 0, 0)
	if got := testArrayWords(t, client, target)[:2]; got[0] != 0 || got[1] != 0 {
		t.Errorf("a row off the left edge read %#x, want zeros", got)
	}
}

// What the guest hands over is checked before anything moves: a null array is
// the NullPointerException, a rectangle the array cannot hold or an offset
// below zero the ArrayIndexOutOfBoundsException, and an array of the wrong
// kind is reported rather than written past. A line too short for a row and a
// rectangle with no area move nothing.
func TestJavaRGBPixelsBoundTheArray(t *testing.T) {
	client, screen, graphics := screenGraphicsFixture(t)
	array := newTestIntArray(t, client, 1, 2, 3, 4, 5, 6)
	for _, key := range []string{javaGetRGBPixelsKey, javaSetRGBPixelsKey} {
		if _, err := javaCall(t, client, key, graphics, 0, 0, 2, 2, 0, 0, 0); thrownClass(err) != javaThrowNullClass {
			t.Errorf("%s with a null array = %v, want NullPointerException", key, err)
		}
		for _, bounds := range [][4]uint32{
			{2, 2, 5, 0},           // two rows from 5 of 6
			{2, 1, 0xffffffff, 0},  // an offset below zero
			{2, 2, 0, 0x7ffffffc},  // a line too long for the array
			{2, 0x7fffffff, 0, 16}, // rows past the end
		} {
			if _, err := javaCall(t, client, key, graphics, 0, 0, bounds[0], bounds[1], array, bounds[2], bounds[3]); thrownClass(err) != javaThrowArrayClass {
				t.Errorf("%s %v = %v, want ArrayIndexOutOfBoundsException", key, bounds, err)
			}
		}
		bytes, err := client.newJavaByteArray(make([]byte, 64))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := javaCall(t, client, key, graphics, 0, 0, 2, 2, bytes, 0, 0); err == nil || thrownClass(err) != "" {
			t.Errorf("%s with a byte[] = %v, want a refusal that is no guest exception", key, err)
		}
	}
	before := append([]uint16(nil), screen.pixels...)
	for _, shape := range [][4]uint32{{3, 1, 0, 8}, {0, 2, 0, 0}, {2, 0, 0, 0}, {2, 1, 0, 0xfffffffc}} {
		mustJavaCall(t, client, javaSetRGBPixelsKey, graphics, 0, 0, shape[0], shape[1], array, shape[2], shape[3])
		mustJavaCall(t, client, javaGetRGBPixelsKey, graphics, 0, 0, shape[0], shape[1], array, shape[2], shape[3])
	}
	for index := range before {
		if screen.pixels[index] != before[index] {
			t.Fatal("a call that moves nothing changed the surface")
		}
	}
	if got := testArrayWords(t, client, array); got[0] != 1 || got[5] != 6 {
		t.Errorf("a call that moves nothing changed the array to %v", got)
	}
}

// The byte pair moves the same rectangle in the surface's own format: each
// pixel its two framebuffer bytes, low byte first, with a byte pitch.
func TestJavaPixelsMoveTheSurfaceFormat(t *testing.T) {
	client, screen, graphics := screenGraphicsFixture(t)
	data := make([]byte, 12)
	for index, pixel := range []uint16{0x1234, 0xabcd} {
		binary.LittleEndian.PutUint16(data[2+index*2:], pixel)
	}
	for index, pixel := range []uint16{0xf800, 0x07e0} {
		binary.LittleEndian.PutUint16(data[8+index*2:], pixel)
	}
	source, err := client.newJavaByteArray(data)
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, javaSetPixelsKey, graphics, 5, 3, 2, 2, source, 2, 6)
	for point, want := range map[[2]int]uint16{{5, 3}: 0x1234, {6, 3}: 0xabcd, {5, 4}: 0xf800, {6, 4}: 0x07e0} {
		if got := screen.pixels[point[1]*screen.width+point[0]]; got != want {
			t.Errorf("pixel %v = %#x, want %#x", point, got, want)
		}
	}
	if !screen.drawnHere {
		t.Error("a write did not make the runtime's copy of the surface the correct one")
	}

	// A pitch of zero is rows of exactly the width; past the right edge the
	// bytes read as zero.
	target, err := client.newJavaByteArray(make([]byte, 8))
	if err != nil {
		t.Fatal(err)
	}
	mustJavaCall(t, client, javaGetPixelsKey, graphics, 5, 3, 2, 2, target, 0, 0)
	got, err := client.readJavaArrayBytes(target)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0x34, 0x12, 0xcd, 0xab, 0x00, 0xf8, 0xe0, 0x07}; string(got) != string(want) {
		t.Errorf("getPixels read % x, want % x", got, want)
	}
	mustJavaCall(t, client, javaGetPixelsKey, graphics, 15, 3, 2, 1, target, 0, 0)
	if got, _ := client.readJavaArrayBytes(target); got[2] != 0 || got[3] != 0 {
		t.Errorf("a pixel past the right edge read % x, want zeros", got[:4])
	}

	if _, err := javaCall(t, client, javaGetPixelsKey, graphics, 0, 0, 1, 1, 0, 0, 0); thrownClass(err) != javaThrowNullClass {
		t.Errorf("getPixels with a null array = %v, want NullPointerException", err)
	}
	for _, bounds := range [][4]uint32{{2, 2, 1, 0}, {1, 1, 0xffffffff, 0}, {1, 2, 0, 0xfffffffe}, {4, 2, 0, 8}} {
		if _, err := javaCall(t, client, javaSetPixelsKey, graphics, 0, 0, bounds[0], bounds[1], target, bounds[2], bounds[3]); thrownClass(err) != javaThrowArrayClass {
			t.Errorf("setPixels %v = %v, want ArrayIndexOutOfBoundsException", bounds, err)
		}
	}
	// A line shorter than a row would have rows written over one another;
	// it moves nothing, as the RGB pair's short line does.
	before := append([]uint16(nil), screen.pixels...)
	mustJavaCall(t, client, javaSetPixelsKey, graphics, 0, 0, 2, 2, target, 0, 3)
	for index := range before {
		if screen.pixels[index] != before[index] {
			t.Fatal("a line shorter than a row moved pixels")
		}
	}
	ints := newTestIntArray(t, client, 0, 0)
	if _, err := javaCall(t, client, javaGetPixelsKey, graphics, 0, 0, 1, 1, ints, 0, 0); err == nil || thrownClass(err) != "" {
		t.Errorf("getPixels with an int[] = %v, want a refusal that is no guest exception", err)
	}
}

package ktf

import (
	"context"
	"encoding/binary"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// newTestNativePlatform builds a client over a synthetic package. The module is
// a single `bx lr`, because these tests drive the platform's own handlers
// rather than a guest: what they exercise is the contract each slot answers
// with, which is the half a real module would otherwise have to be present to
// reach.
func newTestNativePlatform(t *testing.T, files map[string][]byte) *NativePlatform {
	t.Helper()
	return newTestNativePlatformOn(t, files, NewManualClock(time.Time{}))
}

// newTestNativePlatformOn is the same with a clock the test drives.
func newTestNativePlatformOn(t *testing.T, files map[string][]byte, clock Clock) *NativePlatform {
	t.Helper()
	module := make([]byte, 4)
	// bx lr, in ARM.
	binary.LittleEndian.PutUint32(module, 0xe12fff1e)
	archive := &NativeArchive{
		Info:   NativeInfo{Sections: [][]uint32{{nativePageSize}}},
		Module: module,
		Files:  files,
	}
	client, err := LoadNativeClient(archive, armcore.CoreOptions{})
	if err != nil {
		t.Fatalf("load synthetic native client: %v", err)
	}
	platform := NewNativePlatform(client, archive, clock)
	if err := platform.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}
	return platform
}

// call runs one handler with the arguments in r0 upwards.
func nativeCall(t *testing.T, handler NativeSlotHandler, arguments ...uint32) uint32 {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	for index, argument := range arguments {
		if err := thread.SetRegister(index, argument); err != nil {
			t.Fatalf("set r%d: %v", index, err)
		}
	}
	result, err := handler(thread)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return result
}

// nativeString writes a terminated string into guest memory and returns it.
func nativeString(t *testing.T, platform *NativePlatform, text string) uint32 {
	t.Helper()
	address, err := platform.client.Allocate(uint32(len(text) + 1))
	if err != nil {
		t.Fatalf("allocate %d bytes: %v", len(text)+1, err)
	}
	if err := platform.client.core.Memory().Write(address, append([]byte(text), 0)); err != nil {
		t.Fatalf("write string: %v", err)
	}
	return address
}

func nativeRead(t *testing.T, platform *NativePlatform, address uint32, length int) []byte {
	t.Helper()
	data := make([]byte, length)
	if err := platform.client.core.Memory().Read(address, data); err != nil {
		t.Fatalf("read %d bytes at %#x: %v", length, address, err)
	}
	return data
}

// TestNativeLibrarySlots covers the C string and memory library at the bottom
// of the platform table. The module leans on it for everything it builds, so a
// slot that answers the wrong way shows up as a name it cannot open rather
// than as a failure here.
func TestNativeLibrarySlots(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	source := nativeString(t, platform, "abcdef")
	destination, err := platform.client.Allocate(32)
	if err != nil {
		t.Fatal(err)
	}

	if got := nativeCall(t, platform.memorySet, destination, 'z', 4); got != destination {
		t.Errorf("fill returned %#x, want the destination %#x", got, destination)
	}
	if got := string(nativeRead(t, platform, destination, 4)); got != "zzzz" {
		t.Errorf("fill wrote %q, want %q", got, "zzzz")
	}
	if got := nativeCall(t, platform.memoryCopy, destination, source, 3); got != destination {
		t.Errorf("copy returned %#x, want %#x", got, destination)
	}
	if got := string(nativeRead(t, platform, destination, 4)); got != "abcz" {
		t.Errorf("copy wrote %q, want %q", got, "abcz")
	}
	if got := nativeCall(t, platform.stringCopy, destination, source); got != destination {
		t.Errorf("string copy returned %#x, want %#x", got, destination)
	}
	if got := nativeCall(t, platform.stringSize, destination); got != 6 {
		t.Errorf("length = %d, want 6", got)
	}
	nativeCall(t, platform.stringJoin, destination, source)
	if got := string(nativeRead(t, platform, destination, 12)); got != "abcdefabcdef" {
		t.Errorf("join wrote %q, want %q", got, "abcdefabcdef")
	}

	format := nativeString(t, platform, "n=%d s=%s")
	written := nativeCall(t, platform.format, destination, format, 7, source)
	rendered := string(nativeRead(t, platform, destination, int(written)))
	if rendered != "n=7 s=abcdef" {
		t.Errorf("format wrote %q, want %q", rendered, "n=7 s=abcdef")
	}
}

// TestNativeLibraryRefusesUnterminatedString keeps a missing terminator a
// refusal rather than a scan of the address space.
func TestNativeLibraryRefusesUnterminatedString(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	const length = nativeMaxString + 16
	address, err := platform.client.Allocate(length)
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.client.core.Memory().Write(address, []byte(strings.Repeat("x", length))); err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.NewContext())
	if err := thread.SetRegister(0, address); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.stringSize(thread); err == nil {
		t.Fatal("a string with no terminator was measured")
	}
}

// TestNativeFileInterface covers what the title's own file wrapper asks for:
// it tests whether a name is there, creates it when it is not, opens it, and
// then reads, seeks and writes through the object it was handed.
func TestNativeFileInterface(t *testing.T) {
	platform := newTestNativePlatform(t, map[string][]byte{"a title/data.bin": []byte("0123456789")})
	name := nativeString(t, platform, "data.bin")

	if got := nativeCall(t, platform.fileExists, 0, name); got != 1 {
		t.Errorf("exists = %d, want 1 for a name the package carries", got)
	}
	missing := nativeString(t, platform, "save.dat")
	if got := nativeCall(t, platform.fileExists, 0, missing); got != 0 {
		t.Errorf("exists = %d, want 0 for a name it does not", got)
	}

	record, err := platform.client.Allocate(nativeFileRecordSize)
	if err != nil {
		t.Fatal(err)
	}
	// Zero is what worked: a later module calls this and treats any other
	// answer as a failure worth asking nativeFileLastError about.
	if got := nativeCall(t, platform.fileInformation, 0, name, record); got != 0 {
		t.Errorf("information = %d, want 0 for a name the package carries", got)
	}
	if got := nativeCall(t, platform.fileInformation, 0, missing, record); got == 0 {
		t.Error("information answered success for a name the package does not carry")
	}
	if got := nativeCall(t, platform.lastFileError, 0); got == 0 {
		t.Error("the error report named no failure after one")
	}
	if got := nativeCall(t, platform.fileInformation, 0, name, record); got != 0 {
		t.Errorf("information = %d, want 0", got)
	}
	if got := nativeCall(t, platform.lastFileError, 0); got != 0 {
		t.Errorf("the error report = %d after a call that worked, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(nativeRead(t, platform, record+nativeFileLengthOffset, 4)); got != 10 {
		t.Errorf("length = %d, want 10", got)
	}

	file := nativeCall(t, platform.openFile, 0, name, nativeModeRead)
	if file == 0 {
		t.Fatal("opening a name the package carries was refused")
	}
	buffer, err := platform.client.Allocate(16)
	if err != nil {
		t.Fatal(err)
	}
	if got := nativeCall(t, platform.readFile, file, buffer, 4); got != 4 {
		t.Errorf("read %d bytes, want 4", got)
	}
	if got := string(nativeRead(t, platform, buffer, 4)); got != "0123" {
		t.Errorf("read %q, want %q", got, "0123")
	}
	if got := nativeCall(t, platform.seekFile, file, nativeSeekStart, 8); got != 0 {
		t.Errorf("seek answered %d, want 0", got)
	}
	if got := nativeCall(t, platform.readFile, file, buffer, 8); got != 2 {
		t.Errorf("read %d bytes past the end, want the 2 that are left", got)
	}
	// A read-only file refuses a write the way a short write reads.
	if got := nativeCall(t, platform.writeFile, file, buffer, 2); got != 0 {
		t.Errorf("write to a read-only file returned %d, want 0", got)
	}
	nativeCall(t, platform.closeFile, file)
	if len(platform.files) != 0 {
		t.Errorf("%d files still open after a close", len(platform.files))
	}
}

// TestNativeFileCreatesOnWrite is the gate a title's start-up depends on: it
// makes a scratch file to find out whether the handset has room, and a refused
// create is what puts "not enough file system memory" on its screen.
func TestNativeFileCreatesOnWrite(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	name := nativeString(t, platform, "scratch.dat")
	file := nativeCall(t, platform.openFile, 0, name, 2)
	if file == 0 {
		t.Fatal("opening a missing name for writing was refused")
	}
	payload := nativeString(t, platform, "written")
	if got := nativeCall(t, platform.writeFile, file, payload, 7); got != 7 {
		t.Fatalf("wrote %d bytes, want 7", got)
	}
	nativeCall(t, platform.closeFile, file)

	if got := nativeCall(t, platform.fileExists, 0, name); got != 1 {
		t.Errorf("exists = %d after a write, want 1", got)
	}
	reopened := nativeCall(t, platform.openFile, 0, name, nativeModeRead)
	if reopened == 0 {
		t.Fatal("reopening what was written was refused")
	}
	buffer, err := platform.client.Allocate(16)
	if err != nil {
		t.Fatal(err)
	}
	if got := nativeCall(t, platform.readFile, reopened, buffer, 16); got != 7 {
		t.Fatalf("read %d bytes back, want 7", got)
	}
	if got := string(nativeRead(t, platform, buffer, 7)); got != "written" {
		t.Errorf("read back %q, want %q", got, "written")
	}
}

// TestNativeScreenFill covers the colour format the title's own palette
// established, and the null rectangle its first call passes.
func TestNativeScreenFill(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	rectangle, err := platform.client.Allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	record := make([]byte, 8)
	binary.LittleEndian.PutUint16(record[0:], 2)
	binary.LittleEndian.PutUint16(record[2:], 3)
	binary.LittleEndian.PutUint16(record[4:], 4)
	binary.LittleEndian.PutUint16(record[6:], 5)
	if err := platform.client.core.Memory().Write(rectangle, record); err != nil {
		t.Fatal(err)
	}
	// 0x0000ff00 is the blue of the title's own table.
	nativeCall(t, platform.drawRectangle, 0, rectangle, nativeNoColour, 0x0000ff00)
	frame, _ := platform.Frame()
	if got := frame.RGBAAt(2, 3); got.B != 0xff || got.R != 0 || got.G != 0 {
		t.Errorf("pixel inside the rectangle = %+v, want blue", got)
	}
	if got := frame.RGBAAt(6, 3); got.B != 0 {
		t.Errorf("pixel past the rectangle = %+v, want untouched", got)
	}
	if platform.Draws() != 1 {
		t.Errorf("draws = %d, want 1", platform.Draws())
	}
	nativeCall(t, platform.present, 0)
	if _, presents := platform.Frame(); presents != 1 {
		t.Errorf("presents = %d, want 1", presents)
	}
}

// buildBitmap assembles the shape the title builds in memory: a file header, a
// 40-byte information header, a palette and bottom-up rows.
func buildBitmap(width, height int, palette []color.RGBA, rows []byte) []byte {
	const informationSize = 0x28
	pixels := bitmapFileHeaderSize + informationSize + len(palette)*4
	out := make([]byte, pixels+len(rows))
	out[0], out[1] = 'B', 'M'
	binary.LittleEndian.PutUint32(out[2:], uint32(len(out)))
	binary.LittleEndian.PutUint32(out[bitmapPixelOffsetField:], uint32(pixels))
	binary.LittleEndian.PutUint32(out[bitmapInformationSize:], informationSize)
	binary.LittleEndian.PutUint32(out[bitmapWidthField:], uint32(int32(width)))
	binary.LittleEndian.PutUint32(out[bitmapHeightField:], uint32(int32(height)))
	binary.LittleEndian.PutUint16(out[0x1a:], 1)
	binary.LittleEndian.PutUint16(out[bitmapBitsPerPixelField:], 8)
	binary.LittleEndian.PutUint32(out[bitmapPaletteSizeField:], uint32(len(palette)))
	for index, entry := range palette {
		at := bitmapFileHeaderSize + informationSize + index*4
		out[at], out[at+1], out[at+2] = entry.B, entry.G, entry.R
	}
	copy(out[pixels:], rows)
	return out
}

// TestNativeImageFactoryAndBlit covers the pair that puts the title's own
// artwork on the screen: the factory decodes the bitmap the title built, and
// the blit draws a region of it, leaving the transparent colour behind.
func TestNativeImageFactoryAndBlit(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	palette := []color.RGBA{
		{R: 0xff, G: 0x00, B: 0xff, A: 0xff}, // the colour that is not drawn
		{R: 0x10, G: 0x20, B: 0x30, A: 0xff},
		{R: 0x40, G: 0x50, B: 0x60, A: 0xff},
	}
	// Two rows of two pixels, bottom-up, each row padded to four bytes.
	rows := []byte{
		1, 2, 0, 0, // the bottom row of the image
		0, 2, 0, 0, // the top row: one transparent, one solid
	}
	bitmap := buildBitmap(2, 2, palette, rows)
	address, err := platform.client.Allocate(uint32(len(bitmap)))
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.client.core.Memory().Write(address, bitmap); err != nil {
		t.Fatal(err)
	}

	// A class the platform does not build is refused rather than failed.
	if got := nativeCall(t, platform.createObject, nativeClassImage+1, address); got != 0 {
		t.Errorf("an unknown class answered %#x, want 0", got)
	}
	object := nativeCall(t, platform.createObject, nativeClassImage, address)
	if object == 0 {
		t.Fatal("the factory refused a bitmap")
	}
	// The object's first word points at the bitmap, because the title reads
	// that word itself on one of its paths.
	if got, err := platform.client.ReadWord(object); err != nil || got != address {
		t.Errorf("object word 0 = %#x (%v), want the bitmap at %#x", got, err, address)
	}
	decoded := platform.images[object].frame
	if got := decoded.RGBAAt(0, 0); got != palette[0] {
		t.Errorf("top-left = %+v, want the transparent entry %+v", got, palette[0])
	}
	if got := decoded.RGBAAt(0, 1); got != palette[1] {
		t.Errorf("bottom-left = %+v, want %+v", got, palette[1])
	}

	// Blit the whole image to (5,5). The transparent pixel leaves the frame
	// as it was; the others land.
	frame, _ := platform.Frame()
	frame.SetRGBA(5, 5, color.RGBA{R: 1, G: 2, B: 3, A: 0xff})
	platform.drawImage(platform.images[object], NativeBlit{X: 5, Y: 5, Width: 2, Height: 2, Pixels: object})
	if got := frame.RGBAAt(5, 5); got != (color.RGBA{R: 1, G: 2, B: 3, A: 0xff}) {
		t.Errorf("under the transparent pixel = %+v, want what was there", got)
	}
	if got := frame.RGBAAt(6, 5); got != palette[2] {
		t.Errorf("beside it = %+v, want %+v", got, palette[2])
	}
	if got := frame.RGBAAt(5, 6); got != palette[1] {
		t.Errorf("below it = %+v, want %+v", got, palette[1])
	}
}

// TestNativeImageRefusesWhatItCannotDecode keeps a format this platform does
// not read a refusal by name rather than a screen full of noise.
func TestNativeImageRefusesWhatItCannotDecode(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	bitmap := buildBitmap(2, 2, []color.RGBA{{}}, []byte{0, 0, 0, 0, 0, 0, 0, 0})
	binary.LittleEndian.PutUint16(bitmap[bitmapBitsPerPixelField:], 2)
	address, err := platform.client.Allocate(uint32(len(bitmap)))
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.client.core.Memory().Write(address, bitmap); err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.NewContext())
	if err := thread.SetRegister(0, nativeClassImage); err != nil {
		t.Fatal(err)
	}
	if err := thread.SetRegister(1, address); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.createObject(thread); err == nil || !strings.Contains(err.Error(), "bits per pixel") {
		t.Fatalf("error = %v, want one naming the depth", err)
	}
}

// TestNativeKeyCodesAreTheOnesTheTitleActsOn pins the translation from the
// WIPI codes every Host here already speaks to the codes this package's title
// compares against. The pad values are what driving the title's own menus
// proved; the digits are the block its numbered items answer to.
func TestNativeKeyCodesAreTheOnesTheTitleActsOn(t *testing.T) {
	for _, test := range []struct {
		from int32
		to   uint32
		what string
	}{
		{KeyUp, 0xe032, "up"},
		{KeyDown, 0xe038, "down"},
		{KeyLeft, 0xe034, "left"},
		{KeyRight, 0xe036, "right"},
		{KeyFire, 0xe035, "select"},
		{KeyClear, 0xe030, "clear"},
		{'0', 0xe021, "the zero key"},
		{'1', 0xe022, "the one key"},
		{'9', 0xe02a, "the nine key"},
	} {
		got, ok := nativeKeyCode(test.from)
		if !ok || got != test.to {
			t.Errorf("%s: code %d became %#x (%v), want %#x", test.what, test.from, got, ok, test.to)
		}
	}
	// A key the handset has no code for is refused rather than sent as itself:
	// an unmapped value is indistinguishable from a mapping that is wrong.
	for _, key := range []int32{KeyLeftSoft, KeyCall, KeyVolumeUp, '*', '#'} {
		if _, ok := nativeKeyCode(key); ok {
			t.Errorf("code %d was translated; it has no established code here", key)
		}
	}
}

// TestNativeSavesOutliveTheSession covers the store behind the title's own
// files: what it writes is kept, and what it wrote before wins over the copy
// the package shipped.
func TestNativeSavesOutliveTheSession(t *testing.T) {
	store := memorySaveStore{}
	platform := newTestNativePlatform(t, map[string][]byte{"a title/config.dat": []byte("shipped")})
	platform.AttachSaves(store)

	name := nativeString(t, platform, "config.dat")
	file := nativeCall(t, platform.openFile, 0, name, 2)
	if file == 0 {
		t.Fatal("opening a shipped file for writing was refused")
	}
	payload := nativeString(t, platform, "written!")
	if got := nativeCall(t, platform.writeFile, file, payload, 8); got != 8 {
		t.Fatalf("wrote %d bytes, want 8", got)
	}
	nativeCall(t, platform.closeFile, file)
	if got, ok := store["fs/config.dat"]; !ok || string(got) != "written!" {
		t.Fatalf("store holds %q (%v), want the written bytes", got, ok)
	}

	// A second session over the same store reads back what the first wrote,
	// and not what the package carries.
	second := newTestNativePlatform(t, map[string][]byte{"a title/config.dat": []byte("shipped")})
	second.AttachSaves(store)
	contents, ok := second.contents("config.dat")
	if !ok || string(contents) != "written!" {
		t.Fatalf("second session read %q (%v), want the save", contents, ok)
	}
	if platform.StoreFailures() != 0 {
		t.Errorf("%d writes were refused", platform.StoreFailures())
	}
}

// memorySaveStore is a SaveStore with no filesystem behind it.
type memorySaveStore map[string][]byte

func (store memorySaveStore) LoadSave(name string) ([]byte, bool) {
	data, ok := store[name]
	return data, ok
}

func (store memorySaveStore) StoreSave(name string, data []byte) error {
	store[name] = append([]byte(nil), data...)
	return nil
}

// TestNativeSoundRefusesWhatItCannotPlay covers the clip path's refusals. A
// title has no way to be told its sound did not play, so a clip this platform
// cannot read is silence and a counter rather than a stopped run.
func TestNativeSoundRefusesWhatItCannotPlay(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	platform.AttachAudio(nil)
	address, err := platform.client.Allocate(64)
	if err != nil {
		t.Fatal(err)
	}
	// Something that is not a SMAF file at all.
	if err := platform.client.core.Memory().Write(address, []byte("not a clip")); err != nil {
		t.Fatal(err)
	}
	if got := nativeCall(t, platform.setClip, 0, 0, address); got != 0 {
		t.Errorf("an unreadable clip answered %d, want 0", got)
	}
	// A SMAF header naming more than this platform will read.
	header := make([]byte, 8)
	copy(header, "MMMD")
	binary.BigEndian.PutUint32(header[4:], maxNativeClip)
	if err := platform.client.core.Memory().Write(address, header); err != nil {
		t.Fatal(err)
	}
	if got := nativeCall(t, platform.setClip, 0, 0, 0); got != 0 {
		t.Errorf("a null clip answered %d, want 0", got)
	}
	if got := nativeCall(t, platform.setClip, 0, 0, address); got != 0 {
		t.Errorf("an oversized clip answered %d, want 0", got)
	}
	// A null pointer is not a refusal — there was nothing to refuse — so the
	// two unreadable clips are what is counted.
	if platform.ClipRefusals() != 2 {
		t.Errorf("refusals = %d, want the two unreadable clips", platform.ClipRefusals())
	}
	// Playing and stopping with nothing loaded is not a failure either.
	if got := nativeCall(t, platform.playClip, 0); got != 1 {
		t.Errorf("playing nothing answered %d, want 1", got)
	}
	if got := nativeCall(t, platform.stopClip, 0); got != 1 {
		t.Errorf("stopping nothing answered %d, want 1", got)
	}
}

// TestNativeTimedServiceReportsItsEnd covers the service the title starts with
// a duration and then waits on: the flag it sets is cleared only by what its
// listener hears, so a platform that never reports the end leaves it waiting.
func TestNativeTimedServiceReportsItsEnd(t *testing.T) {
	clock := NewManualClock(time.Time{})
	platform := newTestNativePlatformOn(t, nil, clock)
	// A listener the module would have registered on the timed source.
	platform.listeners = append(platform.listeners, NativeListener{Source: nativeInterfaceTimed})
	if got := nativeCall(t, platform.startTimed, 0, 250); got != 1 {
		t.Fatalf("starting answered %d, want 1", got)
	}
	if err := platform.advanceTimed(context.Background()); err != nil {
		t.Fatalf("before the time is up: %v", err)
	}
	if !platform.timedRunning {
		t.Fatal("the service stopped before its time was up")
	}
	clock.Advance(300 * time.Millisecond)
	// The listener has no function to call, so delivering is a no-op; what is
	// under test is that the platform stops waiting exactly once.
	if err := platform.advanceTimed(context.Background()); err != nil {
		t.Fatalf("at the deadline: %v", err)
	}
	if platform.timedRunning {
		t.Error("the service is still running past its deadline")
	}
	if got := nativeCall(t, platform.startTimed, 0, 250); got != 1 {
		t.Fatal("restarting was refused")
	}
	if got := nativeCall(t, platform.stopTimed, 0); got != 1 || platform.timedRunning {
		t.Error("stopping did not cancel it")
	}
}

// TestNativeSpeedMovesTheTitlesOwnClock covers the setting a Host offers a
// person. The title paces itself by reading the clock, so what a multiplier
// has to change is that clock — and what a Host waits between callbacks has to
// come back down by the same factor, or the game would be called at the rate
// it was written for however fast its clock ran.
func TestNativeSpeedMovesTheTitlesOwnClock(t *testing.T) {
	source := NewManualClock(time.Time{})
	platform := newTestNativePlatformOn(t, nil, source)
	// The title registers its frame: an interval, a function and a context.
	nativeCall(t, platform.schedule, 0, 50, 0x1000, 0)
	if wait := platform.UntilNextFrame(); wait != 50*time.Millisecond {
		t.Fatalf("wait at the written speed = %v, want 50ms", wait)
	}

	platform.SetSpeed(2)
	if speed := platform.Speed(); speed != 2 {
		t.Fatalf("speed = %v, want 2", speed)
	}
	if wait := platform.UntilNextFrame(); wait != 25*time.Millisecond {
		t.Errorf("wait at twice the speed = %v, want 25ms", wait)
	}

	// Ten milliseconds of the Host's time is twenty of the title's, which is
	// what a title measuring its own elapsed time reads.
	before := platform.clock.Now()
	source.Advance(10 * time.Millisecond)
	if seen := platform.clock.Now().Sub(before); seen != 20*time.Millisecond {
		t.Errorf("the title saw %v pass in 10ms, want 20ms", seen)
	}
	if wait := platform.UntilNextFrame(); wait != 15*time.Millisecond {
		t.Errorf("wait after 10ms = %v, want 15ms", wait)
	}
	source.Advance(15 * time.Millisecond)
	if wait := platform.UntilNextFrame(); wait != 0 {
		t.Errorf("wait once due = %v, want 0", wait)
	}

	// A multiplier outside the range clamps, and nothing selects the speed the
	// title was written for as surely as asking for none.
	platform.SetSpeed(0)
	if speed := platform.Speed(); speed != 1 {
		t.Errorf("speed after asking for none = %v, want 1", speed)
	}
}

// TestNativeSpeedTakesEverySettingAHostOffers covers the values the browser's
// own control carries. They are inside the range every platform here clamps
// to, so what this asserts is that none of them is quietly turned into
// another: a setting a person picks and does not get is worse than no setting.
func TestNativeSpeedTakesEverySettingAHostOffers(t *testing.T) {
	for _, multiplier := range []float64{0.25, 0.5, 0.75, 1, 1.5, 2, 4} {
		source := NewManualClock(time.Time{})
		platform := newTestNativePlatformOn(t, nil, source)
		platform.SetSpeed(multiplier)
		if speed := platform.Speed(); speed != multiplier {
			t.Errorf("speed = %v, want the %v that was asked for", speed, multiplier)
		}
		nativeCall(t, platform.schedule, 0, 48, 0x1000, 0)
		// A frame 48ms away on the title's clock is 48ms divided by the rate
		// on the Host's, which is what decides how often the game is stepped.
		want := time.Duration(float64(48*time.Millisecond) / multiplier)
		if wait := platform.UntilNextFrame(); wait != want {
			t.Errorf("at %vx the wait is %v, want %v", multiplier, wait, want)
		}
	}
}

// TestNativeSavesReachTheStoreAtABoundary covers what a title's write burst
// costs. The local one fills a scratch file 64 bytes at a time, and a store
// call per chunk rewrites the whole file once per chunk.
func TestNativeSavesReachTheStoreAtABoundary(t *testing.T) {
	store := &countingSaveStore{saves: map[string][]byte{}}
	platform := newTestNativePlatform(t, nil)
	platform.AttachSaves(store)

	name := nativeString(t, platform, "burst.dat")
	object := nativeCall(t, platform.openFile, 0, name, 2)
	if object == 0 {
		t.Fatal("opening for writing did not create the file")
	}
	chunk := nativeString(t, platform, "0123456789abcdef")
	for round := 0; round < 8; round++ {
		nativeCall(t, platform.writeFile, object, chunk, 16)
	}
	if store.stores != 0 {
		t.Errorf("%d store writes during the burst, want none until a boundary", store.stores)
	}
	nativeCall(t, platform.closeFile, object)
	if store.stores != 1 {
		t.Errorf("%d store writes at the close, want 1", store.stores)
	}
	if got := len(store.saves[nativeSaveKey("burst.dat")]); got != 128 {
		t.Errorf("stored %d bytes, want the 128 the title wrote", got)
	}
}

// TestNativeTruncatingOpenEmptiesTheFile covers the mode the specification
// names MC_FILE_OPEN_WRTRUNC. A shorter save written over a longer one would
// otherwise keep the tail of the one it replaced.
func TestNativeTruncatingOpenEmptiesTheFile(t *testing.T) {
	platform := newTestNativePlatform(t, map[string][]byte{"settings.dat": []byte("a much longer previous save")})
	name := nativeString(t, platform, "settings.dat")

	object := nativeCall(t, platform.openFile, 0, name, nativeModeWriteTruncate)
	if object == 0 {
		t.Fatal("the truncating open was refused")
	}
	chunk := nativeString(t, platform, "short")
	nativeCall(t, platform.writeFile, object, chunk, 5)
	nativeCall(t, platform.closeFile, object)

	if got, _ := platform.contents("settings.dat"); string(got) != "short" {
		t.Errorf("file = %q, want %q", got, "short")
	}

	// An ordinary write open keeps what is there, because the mode that empties
	// a file is the one that says so.
	object = nativeCall(t, platform.openFile, 0, name, 2)
	if got := platform.files[object]; got == nil || string(got.data) != "short" {
		t.Errorf("an ordinary write open did not keep the file it opened")
	}
}

// countingSaveStore counts what reaches it as well as keeping it.
type countingSaveStore struct {
	saves  map[string][]byte
	stores int
}

func (store *countingSaveStore) StoreSave(name string, data []byte) error {
	store.stores++
	store.saves[name] = append([]byte(nil), data...)
	return nil
}

func (store *countingSaveStore) LoadSave(name string) ([]byte, bool) {
	data, ok := store.saves[name]
	return data, ok
}

// TestNativeMappedSizeIsTheModuleSOwnLength keeps the loader from asking the
// information file for a number it can compute. An earlier pass required the
// file to name the module's page-rounded length and refused a package that did
// not; the tag it matched carries the same constant in three archives whose
// modules are three different sizes, so the requirement only turned two of them
// away.
func TestNativeMappedSizeIsTheModuleSOwnLength(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		length int
		want   uint32
	}{
		{name: "a module inside one page", length: 8, want: nativePageSize},
		{name: "a module over a page boundary", length: nativePageSize + 1, want: 2 * nativePageSize},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// The header says 0x25000 whatever the module is, the way all three
			// local archives do.
			archive := &NativeArchive{
				Module: make([]byte, testCase.length),
				Info:   NativeInfo{Sections: [][]uint32{{0x25000}}},
			}
			mapped, err := nativeMappedSize(archive)
			if err != nil {
				t.Fatalf("mapped size: %v", err)
			}
			if mapped != testCase.want {
				t.Errorf("mapped = %#x, want %#x", mapped, testCase.want)
			}
		})
	}
}

// TestNativeAllocatorReusesReleasedBlocks keeps a title that allocates and
// frees for a whole session from growing the arena for ever.
func TestNativeAllocatorReusesReleasedBlocks(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	first, err := platform.client.Allocate(64)
	if err != nil {
		t.Fatal(err)
	}
	platform.client.Free(first)
	second, err := platform.client.Allocate(64)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Errorf("second allocation at %#x, want the released %#x", second, first)
	}
	// A pointer the platform never handed out is ignored rather than refused.
	platform.client.Free(0xdeadbeef)
}

// TestNativeAllocationsAreEightAligned covers what the procedure call standard
// asks of a block a module stores a double into. The arena underneath aligns
// to four, so the rounding has to happen where the platform's own allocator
// hands one out — and it has to hold for every block rather than the first,
// which is what an odd size followed by another allocation shows.
func TestNativeAllocationsAreEightAligned(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	previous := uint32(0)
	for _, size := range []uint32{1, 3, 4, 5, 12, 13, 64, 100} {
		address, err := platform.client.Allocate(size)
		if err != nil {
			t.Fatalf("allocate %d bytes: %v", size, err)
		}
		if address%nativeBlockAlignment != 0 {
			t.Errorf("allocation of %d bytes at %#x, which is not %d-aligned", size, address, nativeBlockAlignment)
		}
		if previous != 0 && address <= previous {
			t.Errorf("allocation of %d bytes at %#x overlaps the block at %#x", size, address, previous)
		}
		previous = address
	}
}

// A title calls the platform for as long as it runs, so the ordered call log
// is bounded while the per-slot counts -- the answer the summary exists to
// give -- stay whole. The two used to be the same list, which meant a session
// paid for the summary by never giving the log back.
func TestNativeCallLogIsBoundedButTheCountsAreNot(t *testing.T) {
	client := &NativeClient{}
	const beyond = maxNativeRecordedCalls + 500

	for index := 0; index < beyond; index++ {
		client.recordCall(NativeCall{
			Surface: NativePlatformTable,
			Offset:  0x40,
			Slot:    16,
			Caller:  uint32(0x1000 + index),
			Served:  true,
		}, false)
	}

	if got := len(client.Calls()); got != maxNativeRecordedCalls {
		t.Errorf("ordered log kept %d calls, want it bounded at %d", got, maxNativeRecordedCalls)
	}
	if got := client.CallCount(); got != beyond {
		t.Errorf("CallCount() = %d, want %d", got, beyond)
	}

	summary := client.SlotSummary()
	if len(summary) != 1 {
		t.Fatalf("summary has %d rows, want one", len(summary))
	}
	// The count is the whole run's, not the window's: a slot called a hundred
	// thousand times is what says implementing it matters.
	if summary[0].Count != beyond {
		t.Errorf("slot counted %d calls, want %d", summary[0].Count, beyond)
	}
	if !summary[0].Served {
		t.Error("the slot was answered every time and the summary says it was a trap")
	}
	// First names the code to disassemble, so it has to survive the window
	// scrolling past the call it came from.
	if summary[0].First != 0x1000 {
		t.Errorf("first caller = %#x, want %#x", summary[0].First, 0x1000)
	}

	// A call that failed is the one a reader opened the log for, so it is kept
	// whatever the window already holds.
	client.recordCall(NativeCall{
		Surface: NativePlatformTable,
		Offset:  0x80,
		Slot:    32,
		Caller:  0xdead,
	}, true)
	kept := client.Calls()
	if last := kept[len(kept)-1]; last.Slot != 32 || last.Caller != 0xdead {
		t.Errorf("the failed call was dropped; last kept is slot %d from %#x", last.Slot, last.Caller)
	}
}

// A route waits on what the screen is doing — settled, or changed — and asks
// that question every tick. The digest is what makes it affordable, so what it
// has to answer is that an unchanged screen keeps its value and a drawn pixel
// moves it.
func TestNativeFrameDigestTracksTheScreen(t *testing.T) {
	platform := newTestNativePlatform(t, nil)

	blank := platform.FrameDigest()
	if blank == 0 {
		t.Fatal("a started platform has a screen, and its digest should not be the zero answer")
	}
	if again := platform.FrameDigest(); again != blank {
		t.Errorf("digest of an unchanged screen moved: %#x then %#x", blank, again)
	}

	rectangle, err := platform.client.Allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	record := make([]byte, 8)
	binary.LittleEndian.PutUint16(record[0:], 2)
	binary.LittleEndian.PutUint16(record[2:], 3)
	binary.LittleEndian.PutUint16(record[4:], 4)
	binary.LittleEndian.PutUint16(record[6:], 5)
	if err := platform.client.core.Memory().Write(rectangle, record); err != nil {
		t.Fatal(err)
	}
	nativeCall(t, platform.drawRectangle, 0, rectangle, nativeNoColour, 0x0000ff00)

	if drawn := platform.FrameDigest(); drawn == blank {
		t.Error("the screen was drawn into and the digest did not move; a route would wait forever")
	}
}

// TestNativeReallocateKeepsWhatFits covers the platform table's third
// allocator. The module reads a pointer out of its own structure, asks for one
// byte more than the string it is about to write, stores the answer back and
// gives up if it is null — so what this has to get right is that the bytes
// already there survive the move.
func TestNativeReallocateKeepsWhatFits(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	first, err := platform.client.Allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.client.core.Memory().Write(first, []byte("12345678")); err != nil {
		t.Fatal(err)
	}
	grown := nativeCall(t, platform.reallocate, first, 16)
	if grown == 0 {
		t.Fatal("growing a block answered nothing")
	}
	if got := nativeRead(t, platform, grown, 8); string(got) != "12345678" {
		t.Errorf("the grown block holds %q, want what was there", got)
	}
	// Growing gives the old block back, and the tail past what was carried is
	// clear the way a fresh allocation is.
	if _, ok := platform.client.blocks[first]; ok {
		t.Error("the block that was grown was not given back")
	}
	for _, value := range nativeRead(t, platform, grown+8, 8) {
		if value != 0 {
			t.Errorf("the tail of a grown block holds %#x, want it clear", value)
			break
		}
	}
	// Shrinking keeps the front, a null pointer allocates, and a zero size is
	// a free — which is what the library it stands in for does.
	shrunk := nativeCall(t, platform.reallocate, grown, 4)
	if got := nativeRead(t, platform, shrunk, 4); string(got) != "1234" {
		t.Errorf("the shrunk block holds %q, want the front of it", got)
	}
	if fresh := nativeCall(t, platform.reallocate, 0, 8); fresh == 0 {
		t.Error("reallocating a null pointer answered nothing")
	}
	if got := nativeCall(t, platform.reallocate, shrunk, 0); got != 0 {
		t.Errorf("reallocating to nothing answered %#x, want 0", got)
	}
	if _, ok := platform.client.blocks[shrunk]; ok {
		t.Error("reallocating to nothing did not give the block back")
	}
}

// TestNativeImageDecodesFourBitsPerPixel covers the depth a later module's
// artwork is at. Two pixels share a byte and the left one is the high nibble,
// and an odd width still occupies a whole byte before the row is padded — which
// is why the padding is computed from the bits rather than from a byte count.
func TestNativeImageDecodesFourBitsPerPixel(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	palette := []color.RGBA{
		{R: 0x10, G: 0x20, B: 0x30, A: 0xff},
		{R: 0x40, G: 0x50, B: 0x60, A: 0xff},
		{R: 0x70, G: 0x80, B: 0x90, A: 0xff},
	}
	// Three pixels a row, bottom-up: the first row of the file is the bottom
	// row of the picture.
	bitmap := buildBitmap(3, 2, palette, []byte{
		0x12, 0x00, 0x00, 0x00,
		0x01, 0x20, 0x00, 0x00,
	})
	binary.LittleEndian.PutUint16(bitmap[bitmapBitsPerPixelField:], 4)

	address, err := platform.client.Allocate(uint32(len(bitmap)))
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.client.core.Memory().Write(address, bitmap); err != nil {
		t.Fatal(err)
	}
	decoded, err := platform.decodeBitmap(address)
	if err != nil {
		t.Fatalf("decode a four bit bitmap: %v", err)
	}
	for _, want := range []struct {
		x, y  int
		index int
	}{
		{x: 0, y: 0, index: 0}, {x: 1, y: 0, index: 1}, {x: 2, y: 0, index: 2},
		{x: 0, y: 1, index: 1}, {x: 1, y: 1, index: 2}, {x: 2, y: 1, index: 0},
	} {
		if got := decoded.RGBAAt(want.x, want.y); got != palette[want.index] {
			t.Errorf("pixel %d,%d = %v, want palette entry %d %v", want.x, want.y, got, want.index, palette[want.index])
		}
	}
}

// TestNativeFileStatusAnswersTheOpenFile covers the record a later module asks
// an open file for. It reads the length out of the third word and then reads
// that many bytes in a loop, so a platform that leaves this unanswered does not
// fail — it loops for ever.
func TestNativeFileStatusAnswersTheOpenFile(t *testing.T) {
	platform := newTestNativePlatform(t, map[string][]byte{"a title/data.bin": []byte("0123456789")})
	name := nativeString(t, platform, "data.bin")
	file := nativeCall(t, platform.openFile, 0, name, nativeModeRead)
	if file == 0 {
		t.Fatal("opening a name the package carries was refused")
	}
	record, err := platform.client.Allocate(nativeFileRecordSize)
	if err != nil {
		t.Fatal(err)
	}
	if got := nativeCall(t, platform.fileStatus, file, record); got == 0 {
		t.Error("the status of an open file answered nothing")
	}
	if got := binary.LittleEndian.Uint32(nativeRead(t, platform, record+nativeFileLengthOffset, 4)); got != 10 {
		t.Errorf("length = %d, want 10", got)
	}
}

// TestNativeInterfaceObjectAnswersItsReferenceCount covers the two slots every
// object in this protocol carries. A module that drops an interface it is done
// with reaches the second of them, and a trap there ends the run.
func TestNativeInterfaceObjectAnswersItsReferenceCount(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	client := platform.client
	// A number nothing else serves, so what answers is what this builds.
	const identifier = 0x0103028a
	if _, err := client.InterfaceObject(identifier); err != nil {
		t.Fatalf("build the interface object: %v", err)
	}
	surface := nativeInterfaceSurface(identifier)
	for _, offset := range []uint32{nativeObjectAddRef, nativeObjectRelease} {
		handler, ok := client.served[nativeSlotKey{surface: surface, slot: offset / 4}]
		if !ok {
			t.Fatalf("offset %#x of a new interface object is a trap", offset)
		}
		if got := nativeCall(t, handler); got == 0 {
			t.Errorf("offset %#x answered 0, want the count", offset)
		}
	}
	// A slot the platform serves itself is not replaced. The file interface's
	// drop is that: it is served before any module runs.
	if _, err := client.InterfaceObject(nativeInterfaceSound); err != nil {
		t.Fatalf("build the sound interface object: %v", err)
	}
	if _, ok := client.served[nativeSlotKey{
		surface: nativeInterfaceSurface(nativeInterfaceSound),
		slot:    nativeSoundSetClip / 4,
	}]; !ok {
		t.Error("building the object took a slot the platform had already served")
	}
}

// TestNativeLoaderPlantsWhatAModuleLooksForBelowItself covers the two words
// under the image and the room above the stack pointer. All three are things a
// module reads without asking, so nothing in a run names them when they are
// wrong: a later module compares the version word against 0x10000 and returns
// without writing its out parameter when it differs, and one copies a fixed
// 0x400 bytes out of a frame it declared as 0x58.
func TestNativeLoaderPlantsWhatAModuleLooksForBelowItself(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	memory := platform.client.core.Memory()

	below := make([]byte, 8)
	if err := memory.Read(ImageBase-8, below); err != nil {
		t.Fatalf("read the words below the image: %v", err)
	}
	if got := binary.LittleEndian.Uint32(below); got != nativeInterfaceVersion {
		t.Errorf("the word at ImageBase-8 = %#x, want %#x", got, nativeInterfaceVersion)
	}
	if got := binary.LittleEndian.Uint32(below[4:]); got == 0 {
		t.Error("the word at ImageBase-4 names no platform table")
	}

	stack, err := platform.client.thread.Register(armcore.RegisterSP)
	if err != nil {
		t.Fatal(err)
	}
	if got := ThreadStackBase + uint32(ThreadStackSize) - stack; got != nativeStackHeadroom {
		t.Errorf("the entry stack pointer leaves %#x above it, want %#x", got, nativeStackHeadroom)
	}
	// The room has to be mapped as well as reserved: what a module does with it
	// is read it.
	if err := memory.Read(stack, make([]byte, nativeStackHeadroom)); err != nil {
		t.Errorf("read the room above the entry stack pointer: %v", err)
	}
}

// nativeGateModule is a module that opens the way the later generation opens:
// it loads the word two below its own image and compares it against the
// version this loader plants. Nothing executes it — what it is for is the
// question "which generation is asking", which is answered from the bytes.
func nativeGateModule() []byte {
	module := make([]byte, 0x40)
	for _, instruction := range []struct {
		at   int
		word uint32
	}{
		{at: 0x00, word: 0xe92d400e}, // push {r1, r2, r3, lr}
		{at: 0x04, word: 0xe1a0c002}, // mov ip, r2
		{at: 0x08, word: 0xe59f2038}, // ldr r2, [pc, #0x38]
		{at: 0x0c, word: 0xe08f2002}, // add r2, pc, r2
		{at: 0x10, word: 0xe5122008}, // ldr r2, [r2, #-8]
		{at: 0x14, word: 0xe3520b40}, // cmp r2, #0x10000
		{at: 0x18, word: 0xe12fff1e}, // bx lr
	} {
		binary.LittleEndian.PutUint32(module[instruction.at:], instruction.word)
	}
	return module
}

// TestAsksForInterfaceVersionTellsTheGenerationsApart covers the question the
// file interface's answer depends on. The two generations of this package
// disagree about what a call means, and the module that asks what AEE it is
// running on is the one that means the later thing.
func TestAsksForInterfaceVersionTellsTheGenerationsApart(t *testing.T) {
	later := &NativeArchive{Module: nativeGateModule()}
	if !later.AsksForInterfaceVersion() {
		t.Error("a module that gates on the version was read as one that does not")
	}
	// The 2005 module's own opening, which never reads the word.
	earlier := make([]byte, 0x40)
	for index, word := range []uint32{
		0xe92d400e, // push {r1, r2, r3, lr}
		0xe3a03000, // mov r3, #0
		0xe58d3000, // str r3, [sp]
		0xe58d3004, // str r3, [sp, #4]
		0xe1a03002, // mov r3, r2
		0xe1a02001, // mov r2, r1
		0xe1a01000, // mov r1, r0
		0xe3a00014, // mov r0, #0x14
	} {
		binary.LittleEndian.PutUint32(earlier[index*4:], word)
	}
	if (&NativeArchive{Module: earlier}).AsksForInterfaceVersion() {
		t.Error("a module that never reads the word was read as one that gates on it")
	}
	// A module too short to hold the pair is the earlier answer rather than a
	// read past its end.
	if (&NativeArchive{Module: []byte{1, 2, 3}}).AsksForInterfaceVersion() {
		t.Error("a module shorter than the pair answered as though it carried it")
	}
	if (*NativeArchive)(nil).AsksForInterfaceVersion() {
		t.Error("no archive answered as though it carried the gate")
	}
}

// TestNativeFileTestAnswersTheGenerationThatAsked covers the call the two
// generations read the opposite ways round. Both readings were checked by
// running them: the later modules take zero as "the name is there", and the
// 2005 module takes non-zero as "the name is there" and loses its save when it
// is told otherwise.
func TestNativeFileTestAnswersTheGenerationThatAsked(t *testing.T) {
	files := map[string][]byte{"a title/data.bin": []byte("0123456789")}
	for _, testCase := range []struct {
		name           string
		module         []byte
		there, missing uint32
	}{
		{name: "the 2005 module", module: nil, there: 1, missing: 0},
		{name: "a module that asks for a version", module: nativeGateModule(), there: 0, missing: nativeFileFailed},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			platform := newTestNativePlatform(t, files)
			if testCase.module != nil {
				platform.archive.Module = testCase.module
			}
			there := nativeString(t, platform, "data.bin")
			missing := nativeString(t, platform, "nothing.bin")
			if got := nativeCall(t, platform.fileExists, 0, there); got != testCase.there {
				t.Errorf("a name the package carries answered %d, want %d", got, testCase.there)
			}
			if got := nativeCall(t, platform.fileExists, 0, missing); got != testCase.missing {
				t.Errorf("a name it does not answered %d, want %d", got, testCase.missing)
			}
		})
	}
}

// TestNativePostedEventsComeBackOnTheNextTick covers how the later generation
// runs at all. Its start-up ends by posting an event to itself, and a platform
// that drops the post leaves a title that has finished starting up with
// nothing to do next. Delivery is on the next tick and not inside the call:
// the module posts from inside the handler that is running.
func TestNativePostedEventsComeBackOnTheNextTick(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	thread := armcore.NewThread(armcore.NewContext())
	// The first four arguments are in registers and the last two on the stack,
	// which is where the calling standard puts them.
	stack := ThreadStackBase + uint32(ThreadStackSize) - 0x100
	for index, value := range []uint32{0, 0, 0x103267f, 0x7009} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
		t.Fatal(err)
	}
	spilled := make([]byte, 8)
	binary.LittleEndian.PutUint32(spilled, 1)
	binary.LittleEndian.PutUint32(spilled[4:], 0x2a)
	if err := platform.client.core.Memory().Write(stack, spilled); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.postEvent(thread); err != nil {
		t.Fatalf("post an event: %v", err)
	}
	if len(platform.posted) != 1 {
		t.Fatalf("posted %d events, want 1", len(platform.posted))
	}
	want := nativePostedEvent{Class: 0x103267f, Event: 0x7009, First: 1, Secon: 0x2a}
	if platform.posted[0] != want {
		t.Errorf("posted %+v, want %+v", platform.posted[0], want)
	}
	// Delivery needs an application to deliver to, and a run that has none is
	// a run that has not created one yet rather than one to fail.
	if err := platform.deliverPosted(context.Background()); err != nil {
		t.Fatalf("deliver with no application: %v", err)
	}
	if len(platform.posted) != 1 {
		t.Error("the queue was emptied with nothing to deliver it to")
	}
	// The bound is what turns a title posting to itself for ever into a report
	// rather than memory that never comes back.
	platform.posted = make([]nativePostedEvent, maxNativePostedEvents)
	if _, err := platform.postEvent(thread); err == nil {
		t.Error("a title posting past the bound was not reported")
	}
}

// TestNativeResumeRunsOnceAndIsNotRearmed covers the other half of the same
// contract. The title asks to be called once more; a platform that kept
// calling would be running a frame the title never asked for.
func TestNativeResumeRunsOnceAndIsNotRearmed(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	// The module's own entry is `bx lr`, so it is something this can call.
	if got := nativeCall(t, platform.resume, 0, ImageBase, 0x1234); got == 0 {
		t.Fatal("asking to be resumed was refused")
	}
	if len(platform.resumes) != 1 || platform.resumes[0] != (nativeResume{Function: ImageBase, Context: 0x1234}) {
		t.Fatalf("resumes = %+v, want the one that was asked for", platform.resumes)
	}
	if err := platform.deliverResumes(context.Background()); err != nil {
		t.Fatalf("deliver a resume: %v", err)
	}
	if len(platform.resumes) != 0 {
		t.Error("a resume was left queued after it ran")
	}
	// A null function is nothing to call rather than a jump to zero.
	if got := nativeCall(t, platform.resume, 0, 0, 0); got != 0 {
		t.Errorf("a resume with no function answered %d, want 0", got)
	}
	if len(platform.resumes) != 0 {
		t.Error("a resume with no function was queued")
	}
}

// TestNativeScreenColourIsRecorded covers the pair a later module sets before
// it draws its own text. Nothing draws with them yet, so what this pins is
// that the call is not silently losing them.
func TestNativeScreenColourIsRecorded(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	nativeCall(t, platform.screenColour, 0, 2, 0xffffff00)
	nativeCall(t, platform.screenColour, 0, 1, 0)
	if got := platform.Colours(); len(got) != 2 || got[2] != 0xffffff00 || got[1] != 0 {
		t.Errorf("colours = %v, want the two the title set", got)
	}
}

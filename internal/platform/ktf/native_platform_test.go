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
	if got := nativeCall(t, platform.fileInformation, 0, name, record); got != 1 {
		t.Errorf("information = %d, want 1", got)
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
	binary.LittleEndian.PutUint16(bitmap[bitmapBitsPerPixelField:], 4)
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

// TestNativeMappedSizeNeedsTheNamedSize keeps the loader from mapping a size it
// worked out for itself: the information file naming the module's own length is
// the anchor that says the file and the module belong together.
func TestNativeMappedSizeNeedsTheNamedSize(t *testing.T) {
	archive := &NativeArchive{Module: make([]byte, 8), Info: NativeInfo{Sections: [][]uint32{{0x2000}}}}
	if _, err := nativeMappedSize(archive); err == nil {
		t.Fatal("an information file naming a size the module is not parsed")
	}
	archive.Info.Sections = [][]uint32{{nativePageSize}}
	mapped, err := nativeMappedSize(archive)
	if err != nil {
		t.Fatalf("mapped size: %v", err)
	}
	if mapped != nativePageSize {
		t.Errorf("mapped = %#x, want %#x", mapped, nativePageSize)
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

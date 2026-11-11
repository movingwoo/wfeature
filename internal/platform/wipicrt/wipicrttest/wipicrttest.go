// Package wipicrttest builds a WIPI-C engine with no platform above it, for
// the engine's own tests, and holds the helpers those tests share with the
// tests of the platforms built on the engine. Test helpers cannot cross a
// package boundary from a _test.go file, so the ones both sides need live here.
//
// It is imported by tests alone; nothing in a build of the emulator reaches it.
package wipicrttest

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// ErrGuestExited is what the test platform's guest answers when it exits.
var ErrGuestExited = errors.New("the test guest exited")

// Platform is the platform a test engine runs under. Its values are KTF's,
// the platform the engine's tests were first written under: its names and
// its handle layout.
var Platform = wipicrt.Platform{
	Name:            "KTF",
	CName:           "KTF WIPI C",
	CallName:        "KTF AOT",
	ExitError:       ErrGuestExited,
	HandleFirstWord: 4,
	HandleData:      8,
}

// ImageSize is how much guest memory a test client's image takes from
// wipicrt.ImageBase.
const ImageSize = 0xe0

// NewRuntime builds a client on a fresh core and a runtime for it.
func NewRuntime(t testing.TB) (*wipicrt.Client, *wipicrt.Runtime) {
	t.Helper()
	return NewRuntimeWith(t, armcore.CoreOptions{}, nil)
}

// NewPacedRuntime is NewRuntime with the client on clock at speed.
func NewPacedRuntime(t testing.TB, clock wipicrt.Clock, speed float64) (*wipicrt.Client, *wipicrt.Runtime) {
	t.Helper()
	return NewRuntimeWith(t, armcore.CoreOptions{}, func(client *wipicrt.Client) {
		client.Clock = clock
		client.SetSpeed(speed)
	})
}

// NewRuntimeWith builds the core with options and lets configure set the
// client up before the runtime is made over it. The runtime's supervisor
// handler answers the WIPI-C calls guest code makes, the one category of
// supervisor call the engine serves for every platform.
func NewRuntimeWith(t testing.TB, options armcore.CoreOptions, configure func(*wipicrt.Client)) (*wipicrt.Client, *wipicrt.Runtime) {
	t.Helper()
	core := armcore.NewCore(options)
	if err := core.Memory().Map(wipicrt.ImageBase, ImageSize, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	if err := core.Memory().Map(wipicrt.ThreadStackBase, wipicrt.ThreadStackSize, armcore.PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	initial := armcore.NewContext()
	initial.Registers[armcore.RegisterSP] = wipicrt.ThreadStackBase + uint32(wipicrt.ThreadStackSize)
	client := wipicrt.NewClient(Platform, core, armcore.NewThread(initial), ImageSize)
	if configure != nil {
		configure(client)
	}
	var runtime *wipicrt.Runtime
	runtime, err := wipicrt.New(client, wipicrt.RuntimeHooks{
		Supervisor: func(ctx context.Context, thread *armcore.Thread, call armcore.SupervisorCall) error {
			return supervisor(runtime, ctx, thread, call)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Runtime = runtime
	return client, runtime
}

func supervisor(runtime *wipicrt.Runtime, ctx context.Context, thread *armcore.Thread, call armcore.SupervisorCall) error {
	id, err := thread.Register(12)
	if err != nil {
		return err
	}
	previousThread, previousContext := runtime.CurrentThread, runtime.CurrentContext
	runtime.CurrentThread, runtime.CurrentContext = thread, ctx
	defer func() {
		runtime.CurrentThread, runtime.CurrentContext = previousThread, previousContext
	}()
	if call.Immediate != wipicrt.SVCCategoryWIPIC {
		return fmt.Errorf("the test runtime has no supervisor call category %d (id %#x)", call.Immediate, id)
	}
	result, err := runtime.HandleWIPICCall(thread, id)
	if err != nil {
		return err
	}
	return thread.SetRegister(0, result)
}

// ReadBytes reads size bytes of guest memory at address.
func ReadBytes(t testing.TB, client *wipicrt.Client, address uint32, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	if err := client.Core().Memory().Read(address, data); err != nil {
		t.Fatal(err)
	}
	return data
}

// WriteGuestWords is the little-endian word writer these tests build records
// with.
func WriteGuestWords(t testing.TB, runtime *wipicrt.Runtime, address uint32, words ...uint32) {
	t.Helper()
	data := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	if err := runtime.Client.Core().Memory().Write(address, data); err != nil {
		t.Fatalf("write guest words at %#x: %v", address, err)
	}
}

// FinishesWithin runs a drawing call that extreme guest arguments once turned
// into a host loop with no practical end, and fails instead of hanging the
// test binary when it still does.
func FinishesWithin(t testing.TB, limit time.Duration, call func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(limit):
		t.Fatalf("call still running after %v", limit)
	}
}

// ReadSurfacePixels reads a 16-bit framebuffer's pixels row by row.
func ReadSurfacePixels(t testing.TB, runtime *wipicrt.Runtime, framebuffer wipicrt.WIPICFramebuffer) []uint16 {
	t.Helper()
	data := make([]byte, framebuffer.Bpl*framebuffer.Height)
	if err := runtime.Client.Core().Memory().Read(framebuffer.Pixels, data); err != nil {
		t.Fatal(err)
	}
	pixels := make([]uint16, framebuffer.Width*framebuffer.Height)
	for y := uint32(0); y < framebuffer.Height; y++ {
		for x := uint32(0); x < framebuffer.Width; x++ {
			pixels[y*framebuffer.Width+x] = binary.LittleEndian.Uint16(data[y*framebuffer.Bpl+x*2:])
		}
	}
	return pixels
}

// WriteSurfacePixels writes a 16-bit framebuffer's pixels row by row.
func WriteSurfacePixels(t testing.TB, runtime *wipicrt.Runtime, framebuffer wipicrt.WIPICFramebuffer, pixels []uint16) {
	t.Helper()
	data := make([]byte, framebuffer.Bpl*framebuffer.Height)
	for y := uint32(0); y < framebuffer.Height; y++ {
		for x := uint32(0); x < framebuffer.Width; x++ {
			binary.LittleEndian.PutUint16(data[y*framebuffer.Bpl+x*2:], pixels[y*framebuffer.Width+x])
		}
	}
	if err := runtime.Client.Core().Memory().Write(framebuffer.Pixels, data); err != nil {
		t.Fatal(err)
	}
}

// ReferenceLine is the Bresenham loop both line calls ran before they were
// clipped, in a width that ordinary coordinates never overflow.
func ReferenceLine(x0, y0, x1, y1 int64, plot func(x, y int64)) {
	deltaX, deltaY := x1-x0, y1-y0
	if deltaX < 0 {
		deltaX = -deltaX
	}
	if deltaY < 0 {
		deltaY = -deltaY
	}
	stepX, stepY := int64(1), int64(1)
	if x0 > x1 {
		stepX = -1
	}
	if y0 > y1 {
		stepY = -1
	}
	difference := deltaX - deltaY
	for {
		plot(x0, y0)
		if x0 == x1 && y0 == y1 {
			return
		}
		doubled := 2 * difference
		if doubled > -deltaY {
			difference -= deltaY
			x0 += stepX
		}
		if doubled < deltaX {
			difference += deltaX
			y0 += stepY
		}
	}
}

// NetCall drives one network function with the arguments a guest would pass.
func NetCall(t testing.TB, runtime *wipicrt.Runtime, function uint32, arguments ...uint32) uint32 {
	t.Helper()
	callContext := armcore.NewContext()
	for index, value := range arguments {
		callContext.Registers[index] = value
	}
	result, err := runtime.HandleWIPICTableCall(armcore.NewThread(callContext), wipicrt.WIPICTableNet, function)
	if err != nil {
		t.Fatalf("net function %d: %v", function, err)
	}
	return result
}

// DatabaseCall drives one WIPI C database slot with a name in r0.
func DatabaseCall(t testing.TB, runtime *wipicrt.Runtime, function uint32, name string, extra ...uint32) uint32 {
	t.Helper()
	const nameAddress = wipicrt.PlatformDataBase + 0xa000
	if err := runtime.Client.Core().Memory().Write(nameAddress, append([]byte(name), 0)); err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.Context{})
	if err := thread.SetRegister(0, nameAddress); err != nil {
		t.Fatal(err)
	}
	for index, value := range extra {
		if err := thread.SetRegister(index+1, value); err != nil {
			t.Fatal(err)
		}
	}
	result, err := runtime.HandleWIPICFileCall(thread, function)
	if err != nil {
		t.Fatalf("database function %d: %v", function, err)
	}
	return result
}

// MediaCall drives one media function with the arguments a guest would pass.
func MediaCall(t testing.TB, runtime *wipicrt.Runtime, function uint32, arguments ...uint32) uint32 {
	t.Helper()
	context := armcore.NewContext()
	for index, value := range arguments {
		context.Registers[index] = value
	}
	result, err := runtime.HandleWIPICMediaCall(armcore.NewThread(context), function)
	if err != nil {
		t.Fatalf("media function %d error = %v", function, err)
	}
	return result
}

// OneNoteSMAF is the smallest file the sound path plays: a mobile score track
// with a single note. It is built here rather than shipped as a fixture for
// the reason every other fixture in this repository is authored — nothing with
// someone else's provenance goes in the tree.
func OneNoteSMAF() []byte {
	sequence := []byte{
		0x05, 0x90, 60, 100, 0x05,
		0x00, 0xff, 0x2f, 0x00,
	}
	track := append([]byte{2, 0, 2, 2}, make([]byte, 16)...)
	track = append(track, SMAFChunk("Mtsq", sequence)...)
	body := SMAFChunk("MTR\x00", track)
	file := make([]byte, 8)
	copy(file, "MMMD")
	length := uint32(len(body) + 2)
	file[4], file[5], file[6], file[7] = byte(length>>24), byte(length>>16), byte(length>>8), byte(length)
	return append(append(file, body...), 0, 0)
}

// SMAFChunk is a SMAF chunk: its four-byte tag, its big-endian length and
// payload.
func SMAFChunk(tag string, payload []byte) []byte {
	header := make([]byte, 8)
	copy(header, tag)
	length := uint32(len(payload))
	header[4], header[5], header[6], header[7] = byte(length>>24), byte(length>>16), byte(length>>8), byte(length)
	return append(header, payload...)
}

// CountingSink counts what reached the Host, which is the only way to tell a
// sound that played from one that was accepted and dropped.
type CountingSink struct {
	NoteOns int
	Waves   int
}

func (sink *CountingSink) PlayWave(uint8, uint32, []int16) { sink.Waves++ }
func (sink *CountingSink) MIDINoteOn(_, _, _ uint8)        { sink.NoteOns++ }
func (sink *CountingSink) MIDINoteOff(_, _, _ uint8)       {}
func (sink *CountingSink) MIDIProgramChange(_, _ uint8)    {}
func (sink *CountingSink) MIDIControlChange(_, _, _ uint8) {}
func (sink *CountingSink) MIDIPitchBend(_ uint8, _ uint16) {}
func (sink *CountingSink) MIDISysEx([]byte)                {}

// EncodePalettedPNG builds the shape the games' sprite assets have: a palette
// image whose first entry is fully transparent, which PNG carries as a tRNS
// chunk rather than as pixel data. Column 0 uses that entry and column 1 a
// solid colour.
func EncodePalettedPNG(t testing.TB, width, height int) []byte {
	t.Helper()
	palette := color.Palette{
		color.NRGBA{R: 0, G: 0, B: 0, A: 0},
		color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	}
	source := image.NewPaletted(image.Rect(0, 0, width, height), palette)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x == 0 {
				source.SetColorIndex(x, y, 0)
				continue
			}
			source.SetColorIndex(x, y, 1)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// GuestBytes puts Host data where a guest pointer can reach it, which is what
// every WIPI-C media call takes.
func GuestBytes(t testing.TB, runtime *wipicrt.Runtime, data []byte) uint32 {
	t.Helper()
	address, err := runtime.AllocateBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

// UnreadableSaves is a directory save store whose named keys cannot be read:
// what a save the operating system will not hand over answers — its
// permissions, a failing disk — on every system and for any user. Everything
// else is the directory store's.
type UnreadableSaves struct {
	*wipicrt.DirectorySaveStore
	// Keys are the save keys, scope included ("db/slot3.dat"), whose reads
	// fail.
	Keys map[string]bool
}

func (store UnreadableSaves) refuse(name string) error {
	return fmt.Errorf("open %s: permission denied", name)
}

// LoadSave is ReadSave without its error, as the directory store's is.
func (store UnreadableSaves) LoadSave(name string) ([]byte, bool) {
	data, found, err := store.ReadSave(name)
	return data, found && err == nil
}

// ReadSave fails for a named key.
func (store UnreadableSaves) ReadSave(name string) ([]byte, bool, error) {
	if store.Keys[name] {
		return nil, false, store.refuse(name)
	}
	return store.DirectorySaveStore.ReadSave(name)
}

// ReadSaveLimit fails for a named key, as ReadSave does.
func (store UnreadableSaves) ReadSaveLimit(name string, limit int64) ([]byte, bool, error) {
	if store.Keys[name] {
		return nil, false, store.refuse(name)
	}
	return store.DirectorySaveStore.ReadSaveLimit(name, limit)
}

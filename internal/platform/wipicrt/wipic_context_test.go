package wipicrt_test

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
)

func TestWIPICClipPresenceMatchesGuestReadback(t *testing.T) {
	_, runtime := wipicrttest.NewRuntime(t)
	record, err := runtime.AllocateBytes(make([]byte, wipicrt.WIPICGraphicsContextSize))
	if err != nil {
		t.Fatal(err)
	}
	bounds, err := runtime.AllocateWords([]uint32{34, 50, 206, 100})
	if err != nil {
		t.Fatal(err)
	}
	output, err := runtime.AllocateBytes(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	call := func(set bool, index, value uint32) error {
		thread := armcore.NewThread(armcore.NewContext())
		thread.SetRegister(0, record)
		thread.SetRegister(1, index)
		thread.SetRegister(2, value)
		_, err := runtime.WIPICAccessGraphicsContext(thread, set)
		return err
	}
	for _, value := range []uint32{bounds, 0, bounds} {
		if err := call(true, 0, value); err != nil {
			t.Fatal(err)
		}
		// The SDK checks the leading word before asking GetContext for bounds.
		words, err := runtime.ReadAOTWords(record, 1, "clip presence")
		if err != nil || (words[0] != 0) != (value != 0) {
			t.Fatalf("clip pointer %#x: presence=%v err=%v", value, words, err)
		}
		if err := call(true, 1, 0xffff); err != nil {
			t.Fatal(err)
		}
		if err := call(false, 0, output); err != nil {
			t.Fatal(err)
		}
		got, err := runtime.ReadAOTWords(output, 4, "clip readback")
		if err != nil {
			t.Fatal(err)
		}
		want := []uint32{0, 0, 0, 0}
		if value != 0 {
			want = []uint32{34, 50, 206, 100}
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("clip readback = %v, want %v", got, want)
			}
		}
	}
	before := make([]byte, wipicrt.WIPICGraphicsContextSize)
	runtime.Client.Core().Memory().Read(record, before)
	if err := call(true, 0, 0xfffffffc); err == nil {
		t.Fatal("invalid clip pointer accepted")
	}
	after := make([]byte, len(before))
	runtime.Client.Core().Memory().Read(record, after)
	if string(before) != string(after) || binary.LittleEndian.Uint32(after[runtime.WIPICForegroundOffset():]) != 0xffff {
		t.Fatal("failed clip update changed the context")
	}
}

func TestWIPICEmptyClipDoesNotDrawOutsideWidget(t *testing.T) {
	_, runtime := wipicrttest.NewRuntime(t)
	record, _ := runtime.AllocateBytes(make([]byte, wipicrt.WIPICGraphicsContextSize))
	// Nested widget drawing can intersect down to an empty rectangle.
	wipicrttest.WriteGuestWords(t, runtime, record, 1, 0, 0)
	clip, err := runtime.WIPICReadContextClip(record)
	if err != nil {
		t.Fatal(err)
	}
	sourceHandle, _ := runtime.NewWIPICFramebufferRecord(2, 1)
	targetHandle, _ := runtime.NewWIPICFramebufferRecord(2, 1)
	source, _ := runtime.ReadWIPICFramebuffer(sourceHandle)
	target, _ := runtime.ReadWIPICFramebuffer(targetHandle)
	wipicrttest.WriteGuestWords(t, runtime, source.Pixels, 0xffffffff)
	if err := runtime.WIPICBlitClipped(target, clip, wipicrt.WIPICPixelOp{}, 0, 0, 2, 1, source, 0, 0, wipicrt.BlitOpacity{}); err != nil {
		t.Fatal(err)
	}
	pixels, _ := runtime.ReadAOTWords(target.Pixels, 1, "clipped pixels")
	if pixels[0] != 0 {
		t.Fatalf("empty clip copied pixels: %#x", pixels[0])
	}
	if _, _, _, _, drawn := target.ClipRect(clip, 0, 0, 2, 1); drawn {
		t.Fatal("empty clip permits a rectangle fill")
	}
	wipicrttest.WriteGuestWords(t, runtime, record, 0)
	clip, err = runtime.WIPICReadContextClip(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.WIPICBlitClipped(target, clip, wipicrt.WIPICPixelOp{}, 0, 0, 2, 1, source, 0, 0, wipicrt.BlitOpacity{}); err != nil {
		t.Fatal(err)
	}
	pixels, _ = runtime.ReadAOTWords(target.Pixels, 1, "unclipped pixels")
	if pixels[0] != 0xffffffff {
		t.Fatalf("disabled clip left pixels %#x", pixels[0])
	}
}

// contextCall runs MC_grpSetContext or MC_grpGetContext on one field.
func contextCall(t *testing.T, runtime *wipicrt.Runtime, set bool, record, index, value uint32) {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	thread.SetRegister(0, record)
	thread.SetRegister(1, index)
	thread.SetRegister(2, value)
	if _, err := runtime.WIPICAccessGraphicsContext(thread, set); err != nil {
		t.Fatal(err)
	}
}

// TestWIPICGraphicsContextIsTheHandsetsRecord pins where MC_grpSetContext puts
// each field. A title that draws an image itself reads the clip as four words
// from 0x04, the transparent pixel from 0x1c and the origin from 0x24, out of
// the record MC_grpSetContext filled; with the clip stored as 16-bit pairs it
// read a top edge past the screen and drew none of its title screen.
func TestWIPICGraphicsContextIsTheHandsetsRecord(t *testing.T) {
	_, runtime := wipicrttest.NewRuntime(t)
	record, err := runtime.AllocateBytes(bytesOf(0xaa, 0x48))
	if err != nil {
		t.Fatal(err)
	}
	clip, _ := runtime.AllocateWords([]uint32{34, 50, 206, 100})
	origin, _ := runtime.AllocateWords([]uint32{3, 0xfffffffc})
	// MC_grpInitContext clears the 0x40-byte record and nothing past it.
	init := armcore.NewThread(armcore.NewContext())
	init.SetRegister(0, record)
	if err := callInit(runtime, init); err != nil {
		t.Fatal(err)
	}
	cleared := wipicrttest.ReadBytes(t, runtime.Client, record, 0x48)
	for index, value := range cleared {
		if want := byte(0); index >= wipicrt.WIPICGraphicsContextSize {
			want = 0xaa
			if value != want {
				t.Fatalf("init cleared byte %#x past the record", index)
			}
		} else if value != want {
			t.Fatalf("init left byte %#x = %#x", index, value)
		}
	}
	contextCall(t, runtime, true, record, 0, clip)
	contextCall(t, runtime, true, record, 1, 0xf800)
	contextCall(t, runtime, true, record, 2, 0x07e0)
	contextCall(t, runtime, true, record, 3, 0xf81f)
	contextCall(t, runtime, true, record, 4, 0x80)
	contextCall(t, runtime, true, record, 7, 8)
	contextCall(t, runtime, true, record, 8, 1)
	contextCall(t, runtime, true, record, 10, origin)
	words, err := runtime.ReadAOTWords(record, 16, "context")
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{1, 34, 50, 206, 100, 0xf800, 0x07e0, 0xf81f, 0x80, 3, 0xfffffffc, 0, 0x80, 8, 1, 0}
	for index := range want {
		if words[index] != want[index] {
			t.Fatalf("context word %#x = %#x, want %#x (record %#x)", index*4, words[index], want[index], words)
		}
	}
	read, err := runtime.WIPICReadContextClip(record)
	if err != nil {
		t.Fatal(err)
	}
	if read != (wipicrt.WIPICClip{Enabled: true, Left: 34, Top: 50, Right: 206, Bottom: 100}) {
		t.Fatalf("clip read back as %+v", read)
	}
	output, _ := runtime.AllocateBytes(make([]byte, 16))
	contextCall(t, runtime, false, record, 10, output)
	if got, _ := runtime.ReadAOTWords(output, 2, "origin"); got[0] != 3 || got[1] != 0xfffffffc {
		t.Fatalf("origin read back as %#x", got)
	}
}

// TestWIPICAlphaIsThePixelOperationsParameter pins the specification's "setting
// the alpha sets the pixel operation's parameter with it". Titles set the
// alpha and then their own operation, and the operation reads what the alpha
// was given as its third argument.
func TestWIPICAlphaIsThePixelOperationsParameter(t *testing.T) {
	_, runtime := wipicrttest.NewRuntime(t)
	record, _ := runtime.AllocateBytes(make([]byte, wipicrt.WIPICGraphicsContextSize))
	function := installPixelOperation(t, runtime, wipicrt.ImageBase+0x40)
	contextCall(t, runtime, true, record, 4, 0xff00f81f)
	contextCall(t, runtime, true, record, 5, function)
	op, err := runtime.WIPICReadContextPixelOp(record)
	if err != nil {
		t.Fatal(err)
	}
	if op.Function != function || op.Param != 0xff00f81f {
		t.Fatalf("pixel operation = %+v, want the alpha as its parameter", op)
	}
	output, _ := runtime.AllocateBytes(make([]byte, 4))
	contextCall(t, runtime, false, record, 4, output)
	if got, _ := runtime.ReadAOTWords(output, 1, "alpha"); got[0] != 0xff00f81f {
		t.Fatalf("alpha read back as %#x", got[0])
	}
}

// TestWIPICLegacyGraphicsContextKeepsItsRecord pins the record a session
// restored from an earlier checkpoint keeps: its contexts were written as
// 16-bit pairs with the foreground at 12, and reading them as the handset's
// would draw with the clip's corner as the colour.
func TestWIPICLegacyGraphicsContextKeepsItsRecord(t *testing.T) {
	_, runtime := wipicrttest.NewRuntime(t)
	runtime.LegacyGraphicsContext = true
	record, _ := runtime.AllocateBytes(bytesOf(0xaa, 0x40))
	init := armcore.NewThread(armcore.NewContext())
	init.SetRegister(0, record)
	if err := callInit(runtime, init); err != nil {
		t.Fatal(err)
	}
	if got := wipicrttest.ReadBytes(t, runtime.Client, record+52, 1)[0]; got != 0xaa {
		t.Fatalf("the earlier record's init cleared past its 52 bytes: %#x", got)
	}
	clip, _ := runtime.AllocateWords([]uint32{34, 50, 206, 100})
	contextCall(t, runtime, true, record, 0, clip)
	contextCall(t, runtime, true, record, 1, 0xf800)
	words, _ := runtime.ReadAOTWords(record, 4, "context")
	if words[1] != 50<<16|34 || words[2] != 100<<16|206 || words[3] != 0xf800 {
		t.Fatalf("earlier record = %#x", words)
	}
	if runtime.WIPICForegroundOffset() != 12 {
		t.Fatalf("earlier foreground at %d", runtime.WIPICForegroundOffset())
	}
	read, _ := runtime.WIPICReadContextClip(record)
	if read != (wipicrt.WIPICClip{Enabled: true, Left: 34, Top: 50, Right: 206, Bottom: 100}) {
		t.Fatalf("earlier clip read back as %+v", read)
	}
}

func bytesOf(value byte, count int) []byte {
	data := make([]byte, count)
	for index := range data {
		data[index] = value
	}
	return data
}

// callInit runs MC_grpInitContext on the record in r0.
func callInit(runtime *wipicrt.Runtime, thread *armcore.Thread) error {
	_, err := runtime.HandleWIPICTableCall(thread, wipicrt.WIPICTableGraphics, 5)
	return err
}

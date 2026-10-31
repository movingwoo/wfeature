package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

func TestWIPICClipPresenceMatchesGuestReadback(t *testing.T) {
	_, runtime := newTestRuntime(t)
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
	runtime.client.Core().Memory().Read(record, before)
	if err := call(true, 0, 0xfffffffc); err == nil {
		t.Fatal("invalid clip pointer accepted")
	}
	after := make([]byte, len(before))
	runtime.client.Core().Memory().Read(record, after)
	if string(before) != string(after) || binary.LittleEndian.Uint32(after[12:]) != 0xffff {
		t.Fatal("failed clip update changed the context")
	}
}

func TestWIPICEmptyClipDoesNotDrawOutsideWidget(t *testing.T) {
	_, runtime := newTestRuntime(t)
	record, _ := runtime.AllocateBytes(make([]byte, wipicrt.WIPICGraphicsContextSize))
	// Nested widget drawing can intersect down to an empty rectangle.
	writeGuestWords(t, runtime, record, 1, 0, 0)
	clip, err := runtime.WIPICReadContextClip(record)
	if err != nil {
		t.Fatal(err)
	}
	sourceHandle, _ := runtime.NewWIPICFramebufferRecord(2, 1)
	targetHandle, _ := runtime.NewWIPICFramebufferRecord(2, 1)
	source, _ := runtime.ReadWIPICFramebuffer(sourceHandle)
	target, _ := runtime.ReadWIPICFramebuffer(targetHandle)
	writeGuestWords(t, runtime, source.Pixels, 0xffffffff)
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
	writeGuestWords(t, runtime, record, 0)
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

package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func TestWIPICClipPresenceMatchesGuestReadback(t *testing.T) {
	_, runtime := newTestRuntime(t)
	record, err := runtime.allocateBytes(make([]byte, wipicGraphicsContextSize))
	if err != nil {
		t.Fatal(err)
	}
	bounds, err := runtime.allocateWords([]uint32{34, 50, 206, 100})
	if err != nil {
		t.Fatal(err)
	}
	output, err := runtime.allocateBytes(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	call := func(set bool, index, value uint32) error {
		thread := armcore.NewThread(armcore.NewContext())
		thread.SetRegister(0, record)
		thread.SetRegister(1, index)
		thread.SetRegister(2, value)
		_, err := runtime.wipicAccessGraphicsContext(thread, set)
		return err
	}
	for _, value := range []uint32{bounds, 0, bounds} {
		if err := call(true, 0, value); err != nil {
			t.Fatal(err)
		}
		// The SDK checks the leading word before asking GetContext for bounds.
		words, err := runtime.readAOTWords(record, 1, "clip presence")
		if err != nil || (words[0] != 0) != (value != 0) {
			t.Fatalf("clip pointer %#x: presence=%v err=%v", value, words, err)
		}
		if err := call(true, 1, 0xffff); err != nil {
			t.Fatal(err)
		}
		if err := call(false, 0, output); err != nil {
			t.Fatal(err)
		}
		got, err := runtime.readAOTWords(output, 4, "clip readback")
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
	before := make([]byte, wipicGraphicsContextSize)
	runtime.client.core.Memory().Read(record, before)
	if err := call(true, 0, 0xfffffffc); err == nil {
		t.Fatal("invalid clip pointer accepted")
	}
	after := make([]byte, len(before))
	runtime.client.core.Memory().Read(record, after)
	if string(before) != string(after) || binary.LittleEndian.Uint32(after[12:]) != 0xffff {
		t.Fatal("failed clip update changed the context")
	}
}

func TestWIPICEmptyClipDoesNotDrawOutsideWidget(t *testing.T) {
	_, runtime := newTestRuntime(t)
	record, _ := runtime.allocateBytes(make([]byte, wipicGraphicsContextSize))
	// Nested widget drawing can intersect down to an empty rectangle.
	writeGuestWords(t, runtime, record, 1, 0, 0)
	clip, err := runtime.wipicReadContextClip(record)
	if err != nil {
		t.Fatal(err)
	}
	sourceHandle, _ := runtime.newWIPICFramebufferRecord(2, 1)
	targetHandle, _ := runtime.newWIPICFramebufferRecord(2, 1)
	source, _ := runtime.readWIPICFramebuffer(sourceHandle)
	target, _ := runtime.readWIPICFramebuffer(targetHandle)
	writeGuestWords(t, runtime, source.pixels, 0xffffffff)
	if err := runtime.wipicBlitClipped(target, clip, wipicPixelOp{}, 0, 0, 2, 1, source, 0, 0, blitOpacity{}); err != nil {
		t.Fatal(err)
	}
	pixels, _ := runtime.readAOTWords(target.pixels, 1, "clipped pixels")
	if pixels[0] != 0 {
		t.Fatalf("empty clip copied pixels: %#x", pixels[0])
	}
	if _, _, _, _, drawn := target.clipRect(clip, 0, 0, 2, 1); drawn {
		t.Fatal("empty clip permits a rectangle fill")
	}
	writeGuestWords(t, runtime, record, 0)
	clip, err = runtime.wipicReadContextClip(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.wipicBlitClipped(target, clip, wipicPixelOp{}, 0, 0, 2, 1, source, 0, 0, blitOpacity{}); err != nil {
		t.Fatal(err)
	}
	pixels, _ = runtime.readAOTWords(target.pixels, 1, "unclipped pixels")
	if pixels[0] != 0xffffffff {
		t.Fatalf("disabled clip left pixels %#x", pixels[0])
	}
}

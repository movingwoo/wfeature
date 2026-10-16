package ktf

import (
	"context"
	"image/color"
	"testing"
)

// TestNativeClipRefusedWhileSoundingEndsTheSound covers a clip the decoder
// refuses being set while another is sounding. The old clip is closed to make
// room, so nothing is sounding any more, and the end has to be reported: the
// module clears its own "sounding" flag only when it hears it, and a refusal
// that left the platform's flag set kept that end from ever being delivered.
func TestNativeClipRefusedWhileSoundingEndsTheSound(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	platform.AttachAudio(nil)
	put := func(data []byte) uint32 {
		t.Helper()
		address, err := platform.client.Allocate(uint32(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		if err := platform.client.core.Memory().Write(address, data); err != nil {
			t.Fatal(err)
		}
		return address
	}
	if got := nativeCall(t, platform.setClip, 0, 0, put(oneNoteSMAF())); got != 1 {
		t.Fatalf("a playable clip answered %d", got)
	}
	nativeCall(t, platform.playClip)
	if !platform.sounding {
		t.Fatal("the fixture clip is not sounding")
	}
	// A SMAF header over a body with nothing playable in it.
	refused := []byte{'M', 'M', 'M', 'D', 0, 0, 0, 2, 0, 0}
	if got := nativeCall(t, platform.setClip, 0, 0, put(refused)); got != 0 {
		t.Fatalf("an unplayable clip answered %d, want 0", got)
	}
	if err := platform.advanceSound(context.Background()); err != nil {
		t.Fatal(err)
	}
	if platform.sounding {
		t.Fatal("the sound the refused clip closed still reads as sounding")
	}
}

// TestNativeDestroyObjectFreesTheBitmapCopy covers the platform's own copy of
// an image's bitmap, which the factory allocates beside the object. Destroying
// the object freed only the object, so every image a title destroyed left its
// bitmap in the native arena for the rest of the session.
func TestNativeDestroyObjectFreesTheBitmapCopy(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	bitmap := buildBitmap(1, 1, []color.RGBA{{A: 255}, {R: 11, A: 255}}, []byte{1, 0, 0, 0})
	address, err := platform.client.Allocate(uint32(len(bitmap)))
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.client.core.Memory().Write(address, bitmap); err != nil {
		t.Fatal(err)
	}
	object := nativeCall(t, platform.createObject, nativeClassImage, address)
	if object == 0 {
		t.Fatal("the factory refused the bitmap")
	}
	kept := platform.images[object].data
	if got := nativeCall(t, platform.destroyObject, object); got != 1 {
		t.Fatalf("destroy answered %d", got)
	}
	if _, live := platform.client.blocks[object]; live {
		t.Error("the destroyed object is still allocated")
	}
	if _, live := platform.client.blocks[kept]; live {
		t.Error("the destroyed image's bitmap copy is still allocated")
	}
}

// TestNativeRunLogsAreBounded covers the lists the platform keeps of what a
// title did — the text it drew, the files it opened, the resources nobody
// answered. Each grew by one entry a call for the whole session; like the
// ordered call log they now keep the first entries a report reads.
func TestNativeRunLogsAreBounded(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	text := nativeTextAt(t, platform, []byte("A\x00"))
	name := nativeString(t, platform, "missing.dat")
	for range maxNativeRecordedNotes + 8 {
		nativeTextCall(t, platform, 0x8000, text, -1, 0, 0)
		nativeCall(t, platform.openFile, 0, name, nativeModeRead)
		platform.note("unanswered")
	}
	if got := len(platform.Messages()); got != maxNativeRecordedNotes {
		t.Errorf("messages kept %d entries, want %d", got, maxNativeRecordedNotes)
	}
	if got := len(platform.FileOpens()); got != maxNativeRecordedNotes {
		t.Errorf("file opens kept %d entries, want %d", got, maxNativeRecordedNotes)
	}
	if got := len(platform.ResourceNotes()); got != maxNativeRecordedNotes {
		t.Errorf("resource notes kept %d entries, want %d", got, maxNativeRecordedNotes)
	}
}

// TestNativeListenerRegistrationReplacesTheSourcesListener covers a module
// registering on a source again. Only one listener per source is ever called,
// so a second registration is the one that has to be: it replaces the first
// rather than queuing behind it, which also keeps the list from growing with
// every registration.
func TestNativeListenerRegistrationReplacesTheSourcesListener(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	for context := uint32(1); context <= 3; context++ {
		nativeCall(t, platform.addListener(nativeInterfaceSound), 0, 0x1000, context)
	}
	nativeCall(t, platform.addListener(nativeInterfaceTimed), 0, 0x2000, 9)
	if got := len(platform.Listeners()); got != 2 {
		t.Fatalf("listeners = %d entries, want one per source", got)
	}
	listener, ok := platform.listenerFor(nativeInterfaceSound)
	if !ok || listener.Context != 3 {
		t.Fatalf("sound listener = %+v (%t), want the latest registration", listener, ok)
	}
	if listener, ok := platform.listenerFor(nativeInterfaceTimed); !ok || listener.Context != 9 {
		t.Fatalf("timed listener = %+v (%t)", listener, ok)
	}
}

package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// The earlier package's titles compare a key event against a pressed and a
// released application event, and there is no repeated one. A Host that
// forwards the operating system's keyboard repeat — which every browser does
// for a held key — must therefore deliver nothing rather than a second press.
func TestARepeatIsNotASecondPress(t *testing.T) {
	for _, test := range []struct {
		eventType int32
		pressed   bool
		deliver   bool
	}{
		{wipicrt.KeyPressed, true, true},
		{wipicrt.KeyReleased, false, true},
		{wipicrt.KeyRepeated, false, false},
	} {
		pressed, deliver, ok := nativeKeyEvent(test.eventType)
		if !ok {
			t.Errorf("key event type %d is not translated", test.eventType)
			continue
		}
		if deliver != test.deliver || (deliver && pressed != test.pressed) {
			t.Errorf("type %d became pressed=%v deliver=%v, want pressed=%v deliver=%v",
				test.eventType, pressed, deliver, test.pressed, test.deliver)
		}
	}
	if _, _, ok := nativeKeyEvent(wipicrt.KeyRepeated + 1); ok {
		t.Fatal("an event type this platform has no key event for was translated")
	}
}

// TestNativeListenerRegistrationKeepsTheSourcesFirstListener covers a module
// registering on a source again. The first registration stays the one called,
// as it always was and as it is in a checkpoint that recorded several, and the
// list does not grow with every registration.
func TestNativeListenerRegistrationKeepsTheSourcesFirstListener(t *testing.T) {
	platform := newTestNativePlatform(t, nil)
	for context := uint32(1); context <= 3; context++ {
		nativeCall(t, platform.addListener(nativeInterfaceSound), 0, 0x1000, context)
	}
	nativeCall(t, platform.addListener(nativeInterfaceTimed), 0, 0x2000, 9)
	if got := len(platform.Listeners()); got != 2 {
		t.Fatalf("listeners = %d entries, want one per source", got)
	}
	listener, ok := platform.listenerFor(nativeInterfaceSound)
	if !ok || listener.Context != 1 {
		t.Fatalf("sound listener = %+v (%t), want the first registration", listener, ok)
	}
	if listener, ok := platform.listenerFor(nativeInterfaceTimed); !ok || listener.Context != 9 {
		t.Fatalf("timed listener = %+v (%t)", listener, ok)
	}
}

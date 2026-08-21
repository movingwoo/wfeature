package ktf

import "testing"

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
		{KeyPressed, true, true},
		{KeyReleased, false, true},
		{KeyRepeated, false, false},
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
	if _, _, ok := nativeKeyEvent(KeyRepeated + 1); ok {
		t.Fatal("an event type this platform has no key event for was translated")
	}
}

package lgt

import "testing"

// The pad this platform hands the shared rule is the four navigation keys and
// the four digits a keypad prints them on. The rest of the keypad is actions,
// where an extra press would be an extra action.
func TestThePadIsTheNavigationKeysAndTheDigitsUnderThem(t *testing.T) {
	for _, code := range []uint32{0xFFFFFFFF, 0xFFFFFFFE, 0xFFFFFFFD, 0xFFFFFFFC, '2', '4', '6', '8'} {
		if !isPadKey(code) {
			t.Errorf("%#x is not counted as the pad", code)
		}
	}
	// 5 is confirm on every one of these titles, and one draws its skill
	// shortcuts on the corner digits.
	for _, code := range []uint32{'1', '3', '5', '7', '9', '0', '*', '#', 0xFFFFFFFB} {
		if isPadKey(code) {
			t.Errorf("%#x is counted as the pad", code)
		}
	}
}

// The reported defect, end to end through this platform's events: a keyboard
// holds two directions and lets the first go, and the title must not be told
// about a release of a direction it has already moved on from.
func TestARollingKeyboardReachesTheGuestAsAPad(t *testing.T) {
	const (
		left = uint32(0xFFFFFFFD)
		up   = uint32(0xFFFFFFFF)
	)
	session := &Session{client: &Client{}}
	session.SendKey(true, left)
	session.SendKey(true, up)
	session.SendKey(false, left)
	session.SendKey(false, up)
	want := []pendingEvent{
		{kind: EventKeyPressed, param1: left},
		{kind: EventKeyPressed, param1: up},
		{kind: EventKeyReleased, param1: up},
	}
	got := session.client.events
	if len(got) != len(want) {
		t.Fatalf("delivered %v, want %v", got, want)
	}
	for index := range got {
		if got[index].kind != want[index].kind || got[index].param1 != want[index].param1 {
			t.Fatalf("event %d was %v, want %v (all: %v)", index, got[index], want[index], got)
		}
	}
}

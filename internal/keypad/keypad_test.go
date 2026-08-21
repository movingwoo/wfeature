package keypad

import "testing"

// The pad in these tests is four codes and nothing else, which is what a
// platform supplies.
func newPad() *Pad {
	return &Pad{IsPad: func(code int32) bool {
		return code == up || code == down || code == left || code == right
	}}
}

const (
	up    int32 = 1
	down  int32 = 2
	left  int32 = 3
	right int32 = 4
	fire  int32 = 9
)

type step struct {
	pressed bool
	code    int32
}

func TestThePadReportsOneDirection(t *testing.T) {
	for name, test := range map[string]struct {
		send []step
		want []Event
	}{
		"one key is a press and a release": {
			send: []step{{true, up}, {false, up}},
			want: []Event{{true, up}, {false, up}},
		},
		"rolling onto the next direction says nothing about the one left behind": {
			send: []step{{true, left}, {true, up}, {false, left}},
			want: []Event{{true, left}, {true, up}},
		},
		"the pad comes up once, naming the direction the guest was given": {
			send: []step{{true, left}, {true, up}, {false, left}, {false, up}},
			want: []Event{{true, left}, {true, up}, {false, up}},
		},
		"letting go of the direction in use falls back to the one still held": {
			send: []step{{true, down}, {true, left}, {false, left}, {false, down}},
			want: []Event{{true, down}, {true, left}, {true, down}, {false, down}},
		},
		"a key that is not the pad is delivered as it happens": {
			send: []step{{true, up}, {true, fire}, {false, fire}, {false, up}},
			want: []Event{{true, up}, {true, fire}, {false, fire}, {false, up}},
		},
		"a Host that presses a held key twice holds it once": {
			send: []step{{true, up}, {true, up}, {true, right}, {false, right}, {false, up}},
			want: []Event{{true, up}, {true, up}, {true, right}, {true, up}, {false, up}},
		},
		"a release with nothing held is the Host's to explain, not ours to drop": {
			send: []step{{false, up}},
			want: []Event{{false, up}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			pad := newPad()
			var got []Event
			for _, s := range test.send {
				got = append(got, pad.Key(s.pressed, s.code)...)
			}
			if len(got) != len(test.want) {
				t.Fatalf("delivered %v, want %v", got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Fatalf("event %d was %v, want %v (all: %v)", index, got[index], test.want[index], got)
				}
			}
		})
	}
}

// Turning on the spot must not press a direction twice: a title reads two
// presses close together as the double tap that means dash, and a player who
// never dashed said so within a minute of a build that did.
func TestTurningOnTheSpotPressesEachDirectionOnce(t *testing.T) {
	pad := newPad()
	presses := map[int32]int{}
	// Press the next direction, then let the previous one go — the order a
	// thumb and a keyboard both produce, taken from a player's own run.
	order := []int32{right, up, left, down, right, up}
	previous := int32(0)
	deliver := func(events []Event) {
		for _, event := range events {
			if event.Pressed {
				presses[event.Code]++
			}
		}
	}
	for _, code := range order {
		deliver(pad.Key(true, code))
		if previous != 0 {
			deliver(pad.Key(false, previous))
		}
		previous = code
	}
	deliver(pad.Key(false, previous))
	for _, code := range []int32{up, down, left, right} {
		want := 0
		for _, pressed := range order {
			if pressed == code {
				want++
			}
		}
		if presses[code] != want {
			t.Errorf("key %d was pressed %d times, want %d", code, presses[code], want)
		}
	}
}

func TestForgetDropsWhatIsHeld(t *testing.T) {
	pad := newPad()
	pad.Key(true, left)
	pad.Forget()
	got := pad.Key(false, left)
	if len(got) != 1 || got[0] != (Event{Pressed: false, Code: left}) {
		t.Fatalf("after Forget a release delivered %v", got)
	}
}

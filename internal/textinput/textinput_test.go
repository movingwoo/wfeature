package textinput

import (
	"testing"
	"time"
)

// now advances in guest time; the editor is given the clock rather than
// reading one, so a Host batching ticks types the same text as a live one.
func at(millis int) time.Time {
	return time.Unix(0, 0).Add(time.Duration(millis) * time.Millisecond)
}

func TestMultiTapCyclesAndCommits(t *testing.T) {
	state := New("", 0)
	// Three presses of 2 inside the delay cycle a → b → c.
	state.Key('2', at(0))
	state.Key('2', at(100))
	state.Key('2', at(200))
	if state.Text() != "c" {
		t.Fatalf("text = %q, want %q", state.Text(), "c")
	}
	// Waiting past the delay commits, so the next press starts a new letter.
	state.Key('2', at(2000))
	if state.Text() != "ca" {
		t.Fatalf("text after the pause = %q, want %q", state.Text(), "ca")
	}
	// A different key always starts a new letter, even inside the delay.
	state.Key('3', at(2100))
	state.Key('3', at(2200))
	if state.Text() != "cae" {
		t.Fatalf("text = %q, want a new letter from a different key", state.Text())
	}
}

func TestCycleWrapsThroughTheDigit(t *testing.T) {
	state := New("", 0)
	for index, want := range []string{"a", "b", "c", "2", "a"} {
		state.Key('2', at(index*100))
		if state.Text() != want {
			t.Fatalf("press %d = %q, want %q", index+1, state.Text(), want)
		}
	}
}

func TestModeKeyCyclesCharacterSets(t *testing.T) {
	state := New("", 0)
	if state.ModeLabel() != "abc" {
		t.Fatalf("initial mode = %q", state.ModeLabel())
	}
	state.Key('*', at(0))
	if state.ModeLabel() != "ABC" {
		t.Fatalf("mode after one star = %q", state.ModeLabel())
	}
	state.Key('2', at(100))
	if state.Text() != "A" {
		t.Fatalf("uppercase text = %q", state.Text())
	}
	state.Key('*', at(200))
	if state.ModeLabel() != "123" {
		t.Fatalf("mode after two stars = %q", state.ModeLabel())
	}
	// Numeric mode types the digit, and does not cycle.
	state.Key('2', at(300))
	state.Key('2', at(320))
	if state.Text() != "A22" {
		t.Fatalf("numeric text = %q", state.Text())
	}
	// The third star returns to lowercase.
	state.Key('*', at(400))
	if state.ModeLabel() != "abc" {
		t.Fatalf("mode after three stars = %q", state.ModeLabel())
	}
}

func TestBackspaceAndCaret(t *testing.T) {
	state := New("abc", 0)
	if state.Caret() != 3 {
		t.Fatalf("caret = %d, want the end", state.Caret())
	}
	if !state.Key('#', at(0)) {
		t.Fatal("backspace reported no change")
	}
	if state.Text() != "ab" {
		t.Fatalf("text after backspace = %q", state.Text())
	}
	state.MoveCaret(-1)
	state.Key('5', at(100)) // 'j'
	if state.Text() != "ajb" {
		t.Fatalf("insertion at the caret = %q, want %q", state.Text(), "ajb")
	}
	// Backspacing an empty field reports no change rather than underflowing.
	empty := New("", 0)
	if empty.Backspace() {
		t.Fatal("backspace on an empty field reported a change")
	}
}

func TestMaxLengthIsEnforced(t *testing.T) {
	state := New("", 2)
	state.Key('2', at(0))
	state.Key('3', at(1000))
	if state.Text() != "ad" {
		t.Fatalf("text = %q", state.Text())
	}
	// The third character does not fit and nothing changes.
	if state.Key('4', at(2000)) {
		t.Fatal("a key past the limit reported a change")
	}
	if state.Text() != "ad" {
		t.Fatalf("text past the limit = %q", state.Text())
	}
	// A longer initial value is truncated rather than accepted.
	if trimmed := New("abcdef", 3); trimmed.Text() != "abc" {
		t.Fatalf("initial value = %q, want it truncated", trimmed.Text())
	}
	// Lowering the limit truncates and moves the caret back inside.
	state.SetMaxRunes(1)
	if state.Text() != "a" || state.Caret() != 1 {
		t.Fatalf("after SetMaxRunes = %q caret %d", state.Text(), state.Caret())
	}
}

func TestNavigationKeysAreIgnored(t *testing.T) {
	state := New("hi", 0)
	// A game's own navigation keys reach a focused field too; inserting them
	// would put junk in the text.
	for _, key := range []rune{'A', 0, 0x1b} {
		if state.Key(key, at(0)) {
			t.Fatalf("key %q was accepted", key)
		}
	}
	if state.Text() != "hi" {
		t.Fatalf("text = %q", state.Text())
	}
}

func TestMovingTheCaretEndsTheCycle(t *testing.T) {
	state := New("", 0)
	state.Key('2', at(0))
	state.MoveCaret(-1)
	state.MoveCaret(1)
	// The cycle ended, so the next press inserts rather than replacing.
	state.Key('2', at(100))
	if state.Text() != "aa" {
		t.Fatalf("text = %q, want the cycle to have ended", state.Text())
	}
}

func TestUTF16LimitKeepsWholeCharactersAcrossEditing(t *testing.T) {
	state := NewUTF16("AB🙂", 4)
	if state.Key('2', at(0)) || state.Text() != "AB🙂" {
		t.Fatalf("insertion exceeded UTF-16 limit: %q", state.Text())
	}
	if !state.Backspace() || state.Text() != "AB" || state.Caret() != 2 {
		t.Fatalf("backspace split a character: %q caret %d", state.Text(), state.Caret())
	}
	state.MoveCaret(-1)
	state.Key('2', at(100))
	state.Key('2', at(200))
	state.Key('3', at(300))
	if state.Text() != "AbdB" || state.Caret() != 3 || state.Key('4', at(400)) {
		t.Fatalf("caret insertion or cycling changed capacity: %q caret %d", state.Text(), state.Caret())
	}
	state.SetText("A🙂B")
	state.SetMaxUTF16Units(2)
	if state.Text() != "A" || state.Caret() != 1 {
		t.Fatalf("lowered limit split a character: %q caret %d", state.Text(), state.Caret())
	}
	state.Key('2', at(500))
	if state.Text() != "Aa" {
		t.Fatalf("capacity after truncation: %q", state.Text())
	}
	state.SetMaxUTF16Units(0)
	state.SetText("🙂🙂🙂")
	if !state.Key('2', at(600)) || state.Text() != "🙂🙂🙂a" {
		t.Fatalf("unlimited UTF-16 editor: %q", state.Text())
	}
}

func TestUTF16InitialLimitAndRuneCompatibility(t *testing.T) {
	for _, tc := range []struct {
		text  string
		limit int
		want  string
	}{
		{"🙂B", 1, ""},
		{"🙂B", 2, "🙂"},
		{"🙂B", 3, "🙂B"},
		{"한글AB", 3, "한글A"},
	} {
		state := NewUTF16(tc.text, tc.limit)
		if state.Text() != tc.want {
			t.Errorf("NewUTF16(%q, %d) = %q, want %q", tc.text, tc.limit, state.Text(), tc.want)
		}
	}
	state := New("A🙂B", 3)
	state.SetMaxRunes(2)
	if state.Text() != "A🙂" {
		t.Fatalf("rune limit changed: %q", state.Text())
	}
}

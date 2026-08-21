// Package textinput implements the keypad text entry a handset had: multi-tap
// on the numeric keys, with a mode key that cycles between letters, numbers
// and symbols.
//
// It lives outside any platform because MIDP's TextBox and TextField and KTF's
// lwc text components all have the same gap and the same keypad; one
// implementation means a game on either platform types the same way.
//
// Multi-tap is not an invented contract: pressing 2 three times gives "c" on
// every handset that shipped a numeric keypad, and the timeout that commits a
// character when the same key is pressed again is what makes "cab" typable.
package textinput

import (
	"strings"
	"time"
)

// Mode is which character set the keypad produces.
type Mode uint8

const (
	// ModeLowercase and ModeUppercase are the letter modes.
	ModeLowercase Mode = iota
	ModeUppercase
	// ModeNumeric types the digit on the key.
	ModeNumeric
	modeCount
)

// keypad is the character each numeric key cycles through, in the order a
// handset produced them. Key 1 carries the punctuation, key 0 the space.
var keypad = map[rune]string{
	'1': ".,?!'\"1-()@/:_",
	'2': "abc2",
	'3': "def3",
	'4': "ghi4",
	'5': "jkl5",
	'6': "mno6",
	'7': "pqrs7",
	'8': "tuv8",
	'9': "wxyz9",
	'0': " 0",
}

// CommitDelay is how long the same key waits before the next press starts a
// new character instead of cycling the current one. It is guest time, not wall
// time, so a Host batching ticks types the same text as one running live.
//
// It is exported because one platform's text component keeps its own text and
// takes the edits rather than the keys — see the SKVM text component handler,
// which runs this same cycle against a component it cannot read.
const CommitDelay = 900 * time.Millisecond

// State is one text field's editing state: the text, where the caret is, and
// the multi-tap cycle in progress.
type State struct {
	text  []rune
	caret int
	mode  Mode

	// The multi-tap cycle in progress: which key it belongs to, how far
	// through that key's characters it has gone, and which character position
	// it owns. cycleKey is zero once the character is committed.
	cycleKey   rune
	cyclePos   int
	cycleCaret int
	lastKey    time.Time
	maxRunes   int
}

// New returns an editor over an initial value.
func New(initial string, maxRunes int) *State {
	runes := []rune(initial)
	if maxRunes > 0 && len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return &State{text: runes, caret: len(runes), maxRunes: maxRunes, cycleCaret: -1}
}

// Text is the current value.
func (state *State) Text() string { return string(state.text) }

// Caret is the insertion point, in characters.
func (state *State) Caret() int { return state.caret }

// Mode is the active character set.
func (state *State) Mode() Mode { return state.mode }

// SetText replaces the value, ending any cycle in progress.
func (state *State) SetText(value string) {
	runes := []rune(value)
	if state.maxRunes > 0 && len(runes) > state.maxRunes {
		runes = runes[:state.maxRunes]
	}
	state.text = runes
	state.caret = len(runes)
	state.commit()
}

// SetMaxRunes changes the limit, truncating if the value no longer fits.
func (state *State) SetMaxRunes(limit int) {
	state.maxRunes = limit
	if limit > 0 && len(state.text) > limit {
		state.text = state.text[:limit]
		if state.caret > limit {
			state.caret = limit
		}
		state.commit()
	}
}

// commit ends the multi-tap cycle, so the next press of the same key inserts
// a new character.
func (state *State) commit() {
	state.cycleKey = 0
	state.cyclePos = 0
	state.cycleCaret = -1
}

// Key handles one key press at a guest time. It reports whether the text
// changed, which is what tells a caller to repaint and to notify a listener.
//
// The key is the character the Host delivered: a digit, '*' or '#'. A key the
// keypad does not carry is ignored rather than inserted, because a game's
// navigation keys reach the field too.
func (state *State) Key(key rune, now time.Time) bool {
	switch key {
	case '*':
		// The mode key cycles the character set, which is what the star key
		// did on a handset.
		state.mode = state.mode.Next()
		state.commit()
		return false
	case '#':
		return state.Backspace()
	}

	characters, ok := Characters(key)
	if !ok {
		return false
	}
	if state.mode == ModeNumeric {
		state.commit()
		return state.insert(key)
	}

	options := []rune(characters)
	// A second press of the same key within the commit delay replaces the
	// character it just produced; anything else starts a new one. That is
	// what makes both "c" (2,2,2) and "ab" (2, wait, 2,2) typable.
	if state.cycleKey == key && now.Sub(state.lastKey) < CommitDelay &&
		state.cycleCaret >= 0 && state.cycleCaret < len(state.text) {
		state.cyclePos = (state.cyclePos + 1) % len(options)
		state.text[state.cycleCaret] = state.applyMode(options[state.cyclePos])
		state.lastKey = now
		return true
	}

	if !state.insert(state.applyMode(options[0])) {
		state.commit()
		return false
	}
	state.cycleKey = key
	state.cyclePos = 0
	state.cycleCaret = state.caret - 1
	state.lastKey = now
	return true
}

func (state *State) applyMode(character rune) rune {
	return state.mode.Apply(character)
}

// Next cycles to the character set the mode key moves to.
func (mode Mode) Next() Mode { return (mode + 1) % modeCount }

// Apply is what a key produces in this mode: the letter as the keypad table
// carries it, or its capital.
func (mode Mode) Apply(character rune) rune {
	if mode == ModeUppercase {
		return []rune(strings.ToUpper(string(character)))[0]
	}
	return character
}

// Characters answers what one keypad key cycles through, in the order a
// handset produced them, and whether the keypad carries that key at all.
func Characters(key rune) (string, bool) {
	characters, ok := keypad[key]
	return characters, ok
}

// insert adds one character at the caret, reporting whether it fit.
func (state *State) insert(character rune) bool {
	if state.maxRunes > 0 && len(state.text) >= state.maxRunes {
		return false
	}
	if state.caret < 0 || state.caret > len(state.text) {
		state.caret = len(state.text)
	}
	state.text = append(state.text, 0)
	copy(state.text[state.caret+1:], state.text[state.caret:])
	state.text[state.caret] = character
	state.caret++
	return true
}

// Backspace deletes the character before the caret.
func (state *State) Backspace() bool {
	state.commit()
	if state.caret <= 0 || len(state.text) == 0 {
		return false
	}
	state.text = append(state.text[:state.caret-1], state.text[state.caret:]...)
	state.caret--
	return true
}

// MoveCaret shifts the insertion point, ending any cycle: a caret that moved
// is no longer sitting on the character being cycled.
func (state *State) MoveCaret(delta int) {
	state.caret += delta
	if state.caret < 0 {
		state.caret = 0
	}
	if state.caret > len(state.text) {
		state.caret = len(state.text)
	}
	state.commit()
}

// ModeLabel names the active mode for a Host that shows an indicator.
func (state *State) ModeLabel() string {
	switch state.mode {
	case ModeUppercase:
		return "ABC"
	case ModeNumeric:
		return "123"
	}
	return "abc"
}

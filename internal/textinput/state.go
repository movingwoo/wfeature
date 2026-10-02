package textinput

import (
	"fmt"
	"slices"
	"time"
	"unicode/utf8"
)

const maxSnapshotRunes = 1 << 20

// StateSnapshot retains editing and an unfinished multi-tap cycle. LastKeyAge
// is relative to the owner's capture clock so restoration can change epochs.
// The owner must serialize key input with capture and adoption.
type StateSnapshot struct {
	Version              uint32
	Text                 []rune
	Caret                int
	Mode                 Mode
	CycleKey             rune
	CyclePos, CycleCaret int
	LastKeySet           bool
	LastKeyAge           time.Duration
	MaxRunes             int32
	UTF16Limit           bool
}

func (state *State) CaptureState(now time.Time) (StateSnapshot, error) {
	if state == nil || len(state.text) > maxSnapshotRunes || int64(state.maxRunes) < -1<<31 || int64(state.maxRunes) > 1<<31-1 {
		return StateSnapshot{}, fmt.Errorf("text editor state exceeds snapshot limits")
	}
	saved := StateSnapshot{Version: 1, Text: state.text, Caret: state.caret, Mode: state.mode, CycleKey: state.cycleKey, CyclePos: state.cyclePos, CycleCaret: state.cycleCaret,
		LastKeySet: !state.lastKey.IsZero(), MaxRunes: int32(state.maxRunes), UTF16Limit: state.utf16Limit}
	if saved.LastKeySet {
		saved.LastKeyAge = now.Sub(state.lastKey)
		if saved.LastKeyAge == -1<<63 || !now.Add(-saved.LastKeyAge).Equal(state.lastKey) {
			return StateSnapshot{}, fmt.Errorf("text editor key time exceeds snapshot range")
		}
	}
	if err := saved.validate(); err != nil {
		return StateSnapshot{}, err
	}
	saved.Text = slices.Clone(state.text)
	return saved, nil
}

func (saved StateSnapshot) validate() error {
	if saved.Version != 1 || len(saved.Text) > maxSnapshotRunes || saved.Caret < 0 || saved.Caret > len(saved.Text) || saved.Mode >= modeCount || saved.LastKeyAge == -1<<63 || !saved.LastKeySet && saved.LastKeyAge != 0 {
		return fmt.Errorf("text editor snapshot shape is invalid")
	}
	units := 0
	for _, character := range saved.Text {
		if !utf8.ValidRune(character) {
			return fmt.Errorf("text editor snapshot has an invalid character")
		}
		units++
		if saved.UTF16Limit && character > 0xffff {
			units++
		}
	}
	if saved.MaxRunes > 0 && units > int(saved.MaxRunes) {
		return fmt.Errorf("text editor snapshot exceeds its field length limit")
	}
	if saved.CycleKey != 0 {
		characters, ok := Characters(saved.CycleKey)
		if !ok || !saved.LastKeySet || saved.Mode == ModeNumeric || saved.CycleCaret < 0 || saved.CycleCaret >= len(saved.Text) || saved.CyclePos < 0 || saved.CyclePos >= utf8.RuneCountInString(characters) {
			return fmt.Errorf("text editor snapshot has an invalid multi-tap cycle")
		}
	}
	return nil
}

// RestoreState creates an independent editor. It does not send keys or commit
// the pending character while reconstructing the saved state.
func RestoreState(saved StateSnapshot, now time.Time) (*State, error) {
	if err := saved.validate(); err != nil {
		return nil, err
	}
	state := &State{text: slices.Clone(saved.Text), caret: saved.Caret, mode: saved.Mode, cycleKey: saved.CycleKey, cyclePos: saved.CyclePos, cycleCaret: saved.CycleCaret,
		maxRunes: int(saved.MaxRunes), utf16Limit: saved.UTF16Limit}
	if saved.LastKeySet {
		state.lastKey = now.Add(-saved.LastKeyAge)
	}
	return state, nil
}

// RebaseClock moves an inactive restored editor from its construction epoch to
// its adoption epoch. The owner must not deliver input between those instants.
func (state *State) RebaseClock(from, to time.Time) {
	if state != nil && !state.lastKey.IsZero() {
		state.lastKey = to.Add(state.lastKey.Sub(from))
	}
}

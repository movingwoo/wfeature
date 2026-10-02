package textinput

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStateSnapshotContinuesMultitapAcrossClockEpochs(t *testing.T) {
	now := time.Unix(1700000000, 0)
	source := NewUTF16("😀", 4)
	source.Key('2', now)
	source.Key('2', now.Add(100*time.Millisecond))
	saved, err := source.CaptureState(now.Add(250 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var parsed StateSnapshot
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		t.Fatal(err)
	}
	freshNow := time.Unix(1900000000, 0)
	restored, err := RestoreState(parsed, freshNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []struct {
		offset time.Duration
		key    rune
	}{{100 * time.Millisecond, '2'}, {2 * time.Second, '2'}, {3 * time.Second, '3'}, {4 * time.Second, '#'}, {5 * time.Second, '*'}, {6 * time.Second, '3'}} {
		want := source.Key(key.key, now.Add(250*time.Millisecond+key.offset))
		got := restored.Key(key.key, freshNow.Add(key.offset))
		if want != got || restored.Text() != source.Text() || restored.Caret() != source.Caret() || restored.Mode() != source.Mode() {
			t.Fatalf("continued key %q differs: %q/%d/%d, want %q/%d/%d", key.key, restored.Text(), restored.Caret(), restored.Mode(), source.Text(), source.Caret(), source.Mode())
		}
	}
	if source.Text() != "😀cD" {
		t.Fatalf("fixture text=%q", source.Text())
	}
	restored.Backspace()
	if source.Text() != "😀cD" {
		t.Fatal("restored editor shares its source text")
	}
	if _, err := restored.CaptureState(freshNow.Add(7 * time.Second)); err != nil {
		t.Fatalf("editor recapture: %v", err)
	}
}

func TestStateSnapshotRejectsMalformedEditor(t *testing.T) {
	now := time.Unix(1700000000, 0)
	state := New("a", 8)
	state.Key('2', now)
	saved, err := state.CaptureState(now)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*StateSnapshot){
		func(s *StateSnapshot) { s.Version++ },
		func(s *StateSnapshot) { s.Caret = -1 },
		func(s *StateSnapshot) { s.Mode = modeCount },
		func(s *StateSnapshot) { s.Text[0] = 0xd800 },
		func(s *StateSnapshot) { s.CycleCaret = len(s.Text) },
		func(s *StateSnapshot) { s.CyclePos = 100 },
		func(s *StateSnapshot) { s.CycleKey = 'x' },
		func(s *StateSnapshot) { s.LastKeySet = false },
		func(s *StateSnapshot) { s.MaxRunes = 1 },
	} {
		bad := saved
		bad.Text = append([]rune(nil), saved.Text...)
		change(&bad)
		if _, err := RestoreState(bad, now); err == nil {
			t.Fatal("malformed editor accepted")
		}
	}
}

package lgt

import (
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

func pausedRepeatCheckpointAudio(t *testing.T) backend.AudioState {
	t.Helper()
	audio := backend.NewAudioWithClock(nil, func() time.Time { return time.Unix(1000, 0) })
	handle, err := audio.LoadEvents([]smaf.Event{{Time: 1, Type: smaf.EventProgramChange, Program: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 2*time.Second, true); err != nil {
		t.Fatal(err)
	}
	if err := audio.Pause(handle, 2*time.Second+500*time.Microsecond); err != nil {
		t.Fatal(err)
	}
	saved, err := audio.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestAudioPauseCheckpointCatchupUsesFrozenGuestTime(t *testing.T) {
	saved := pausedRepeatCheckpointAudio(t)
	if err := validateGuestAudio(saved, time.Hour); err != nil {
		t.Fatalf("an hour spent paused created repeat catchup: %v", err)
	}
	// The same origin without a pause owes more than a million cycles.
	saved.Sounds[0].Paused, saved.Sounds[0].PausedAt = false, 0
	saved.Output.Sounds[0].Paused = false
	if err := validateGuestAudio(saved, time.Hour); err == nil || !strings.Contains(err.Error(), "catch-up") {
		t.Fatalf("active repeat catchup was not refused: %v", err)
	}
}

func TestAudioPauseCheckpointRejectsImpossibleGuestTime(t *testing.T) {
	for _, playing := range []bool{false, true} {
		for _, position := range []string{"before origin", "after guest clock"} {
			saved := pausedRepeatCheckpointAudio(t)
			saved.Sounds[0].Playing = playing
			if position == "before origin" {
				saved.Sounds[0].PausedAt = saved.Sounds[0].StartedAt - time.Nanosecond
			} else {
				saved.Sounds[0].PausedAt = time.Hour + time.Nanosecond
			}
			if err := validateGuestAudio(saved, time.Hour); err == nil || !strings.Contains(err.Error(), "pause clock") {
				t.Fatalf("playing %t, %s: invalid pause was not refused: %v", playing, position, err)
			}
		}
	}
}

func finiteRepeatCheckpointAudio(t *testing.T, count int32, paused bool) backend.AudioState {
	t.Helper()
	now := time.Unix(1000, 0)
	audio := backend.NewAudioWithClock(nil, func() time.Time { return now })
	handle, err := audio.LoadEvents([]smaf.Event{{Time: 1, Type: smaf.EventProgramChange, Program: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.PlayCount(handle, 2*time.Second, count); err != nil {
		t.Fatal(err)
	}
	if paused {
		if err := audio.Pause(handle, 2*time.Second+500*time.Microsecond); err != nil {
			t.Fatal(err)
		}
	}
	// Keep the score pending while the Host clock moves to a later capture.
	now = now.Add(time.Hour)
	saved, err := audio.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestFiniteAudioCheckpointCatchupHonorsRemainingPasses(t *testing.T) {
	for _, test := range []struct {
		name   string
		count  int32
		paused bool
		refuse bool
	}{
		{name: "two pending passes", count: 2},
		{name: "excessive pending passes", count: 1 << 21, refuse: true},
		{name: "paused finite playback", count: 1 << 21, paused: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			saved := finiteRepeatCheckpointAudio(t, test.count, test.paused)
			err := validateGuestAudio(saved, time.Hour)
			if test.refuse {
				if err == nil || !strings.Contains(err.Error(), "catch-up") {
					t.Fatalf("excessive finite catchup was not refused: %v", err)
				}
			} else if err != nil {
				t.Fatalf("bounded finite playback was refused: %v", err)
			}
		})
	}
}

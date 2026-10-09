package ktf

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
	for _, speed := range []float64{0.5, 1, 2} {
		audio := pausedRepeatCheckpointAudio(t)
		saved := clientState{Speed: speed, Audio: &audio}
		saved.Heap.Control.ClockAge = time.Duration(float64(time.Hour) / speed)
		if err := validateClientAudio(saved); err != nil {
			t.Fatalf("speed %g: an hour spent paused created repeat catchup: %v", speed, err)
		}
		// The same origin without a pause owes more than a million cycles.
		audio.Sounds[0].Paused, audio.Sounds[0].PausedAt = false, 0
		audio.Output.Sounds[0].Paused = false
		if err := validateClientAudio(saved); err == nil || !strings.Contains(err.Error(), "catch-up") {
			t.Fatalf("speed %g: active repeat catchup was not refused: %v", speed, err)
		}
	}
}

func TestAudioPauseCheckpointRejectsImpossibleGuestTime(t *testing.T) {
	for _, speed := range []float64{0.5, 1, 2} {
		for _, playing := range []bool{false, true} {
			for _, position := range []string{"before origin", "after guest clock"} {
				audio := pausedRepeatCheckpointAudio(t)
				saved := clientState{Speed: speed, Audio: &audio}
				saved.Heap.Control.ClockAge = time.Duration(float64(time.Hour) / speed)
				audio.Sounds[0].Playing = playing
				if position == "before origin" {
					audio.Sounds[0].PausedAt = audio.Sounds[0].StartedAt - time.Nanosecond
				} else {
					audio.Sounds[0].PausedAt = time.Hour + time.Nanosecond
				}
				if err := validateClientAudio(saved); err == nil || !strings.Contains(err.Error(), "pause clock") {
					t.Fatalf("speed %g, playing %t, %s: invalid pause was not refused: %v", speed, playing, position, err)
				}
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
			for _, speed := range []float64{0.5, 1, 2} {
				audio := finiteRepeatCheckpointAudio(t, test.count, test.paused)
				saved := clientState{Speed: speed, Audio: &audio}
				saved.Heap.Control.ClockAge = time.Duration(float64(time.Hour) / speed)
				err := validateClientAudio(saved)
				if test.refuse {
					if err == nil || !strings.Contains(err.Error(), "catch-up") {
						t.Fatalf("speed %g: excessive finite catchup was not refused: %v", speed, err)
					}
				} else if err != nil {
					t.Fatalf("speed %g: bounded finite playback was refused: %v", speed, err)
				}
			}
		})
	}
}

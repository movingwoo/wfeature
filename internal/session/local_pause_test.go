package session

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in observation of a local archive. No persistent save store is attached.
// This measures lifecycle behavior; it does not assert that a handset pause is
// a snapshot of every guest thread or of the real-time clock.
func TestLocalPauseResumeObservation(t *testing.T) {
	path := os.Getenv("WFEATURE_PAUSE_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_PAUSE_ARCHIVE to an archive under var/games")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "var", "games"))
	if err != nil {
		t.Fatal(err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("archive must be under var/games")
	}
	archive, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	sink := &pauseAudioCounter{}
	running, err := Start(ctx, archive, Options{AudioSink: sink})
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close()
	tick := func(duration time.Duration) {
		t.Helper()
		deadline := time.Now().Add(duration)
		for time.Now().Before(deadline) {
			progress, err := running.Tick(ctx, 32*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			if progress.Exited {
				t.Fatal("guest exited during observation")
			}
			if progress.Wait > 0 {
				time.Sleep(min(progress.Wait, 25*time.Millisecond))
			}
		}
	}
	elapsed := func() time.Duration {
		if running.runtime != nil {
			return running.runtime.GuestElapsed()
		}
		value, _ := running.GuestElapsed()
		return value
	}
	pauseDuration := time.Second
	if value := os.Getenv("WFEATURE_PAUSE_DURATION"); value != "" {
		pauseDuration, err = time.ParseDuration(value)
		if err != nil || pauseDuration <= 0 || pauseDuration > 10*time.Second {
			t.Fatal("pause duration must be in (0, 10s]")
		}
	}
	tick(time.Second)
	for cycle := 0; cycle < 3; cycle++ {
		if err := running.Pause(ctx); err != nil {
			t.Logf("pause callback: %v", err)
		}
		frame, _, _, _ := running.Frame()
		flushes, before := running.Flushes(), elapsed()
		audioBefore := sink.events.Load()
		time.Sleep(pauseDuration)
		after, _, _, _ := running.Frame()
		t.Logf("platform=%s cycle=%d paused_frames=%d picture_changed=%t elapsed_advance=%s paused_audio=%d", running.Platform(), cycle+1, running.Flushes()-flushes, !bytes.Equal(frame, after), elapsed()-before, sink.events.Load()-audioBefore)
		if _, err := running.Tick(ctx, time.Millisecond); !errors.Is(err, ErrPaused) {
			t.Fatalf("paused tick: %v", err)
		}
		if err := running.Resume(ctx); err != nil {
			t.Logf("resume callback: %v", err)
		}
		audioBefore = sink.events.Load()
		if _, err := running.Tick(ctx, 32*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		t.Logf("first_resume_tick_audio=%d", sink.events.Load()-audioBefore)
		tick(time.Second)
	}

}

// Counts sink calls without retaining samples or guest data.
type pauseAudioCounter struct{ events atomic.Uint64 }

func (s *pauseAudioCounter) PlayWave(uint8, uint32, []int16)       { s.events.Add(1) }
func (s *pauseAudioCounter) MIDINoteOn(uint8, uint8, uint8)        { s.events.Add(1) }
func (s *pauseAudioCounter) MIDINoteOff(uint8, uint8, uint8)       { s.events.Add(1) }
func (s *pauseAudioCounter) MIDIProgramChange(uint8, uint8)        { s.events.Add(1) }
func (s *pauseAudioCounter) MIDIControlChange(uint8, uint8, uint8) { s.events.Add(1) }
func (s *pauseAudioCounter) MIDIPitchBend(uint8, uint16)           { s.events.Add(1) }
func (s *pauseAudioCounter) MIDISysEx([]byte)                      { s.events.Add(1) }

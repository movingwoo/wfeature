package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func TestCheckpointNativeRestoresInputPauseAndContinuation(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprintf("paused=%t", paused), func(t *testing.T) {
			store, _ := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "progress", Data: []byte("saved")}})
			s, err := Start(t.Context(), archive, Options{SaveStore: store, Clock: ktf.NewManualClock(time.Time{}), Speed: 2})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			advance := func(n int) {
				t.Helper()
				for range n {
					if !s.SkipToNextDeadline() {
						t.Fatal("no native deadline")
					}
					if _, err := s.Tick(t.Context(), 0); err != nil {
						t.Fatal(err)
					}
				}
			}
			advance(3)
			if err := s.SendKey(t.Context(), KeyPress, '5'); err != nil {
				t.Fatal(err)
			}
			if paused {
				if err := s.Pause(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			data, err := s.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if paused {
				if err := s.Resume(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SendKey(t.Context(), KeyRelease, '5'); err != nil {
				t.Fatal(err)
			}
			released, err := s.ktfNative.Client.ReadWord(testfixture.KTFNativeCheckpointLastEvent)
			if err != nil {
				t.Fatal(err)
			}
			advance(5)
			expected, width, height, flushes := s.Frame()
			steps := s.ktfNative.Client.Steps()
			if err := store.StoreSave("progress", []byte("later")); err != nil {
				t.Fatal(err)
			}
			if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
				t.Fatal(err)
			}
			if s.Paused() != paused || !reflect.DeepEqual(s.HeldKeys(), []int32{'5'}) || !s.CanCheckpoint() || s.Speed() != 2 {
				t.Fatal("native shared state changed")
			}
			if paused {
				if _, err := s.Tick(t.Context(), 0); !errors.Is(err, ErrPaused) {
					t.Fatalf("paused native checkpoint ran: %v", err)
				}
				if err := s.Resume(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.ReleaseHeldInput(t.Context()); err != nil {
				t.Fatal(err)
			}
			if event, err := s.ktfNative.Client.ReadWord(testfixture.KTFNativeCheckpointLastEvent); err != nil || event != released || len(s.HeldKeys()) != 0 {
				t.Fatal("restored native key was not released")
			}
			advance(5)
			actual, w, h, f := s.Frame()
			if !bytes.Equal(actual, expected) || w != width || h != height || f != flushes || s.ktfNative.Client.Steps() != steps {
				t.Fatal("shared native continuation changed")
			}
			if saved, _ := store.LoadSave("progress"); string(saved) != "saved" {
				t.Fatal("shared native load retained later saves")
			}
		})
	}
}

func TestCheckpointNativeRejectsMalformedAudioBeforeReplacingSaves(t *testing.T) {
	archive, err := testfixture.KTFNativeCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		startedAt time.Duration
		repeat    bool
		playing   bool
	}{
		{"unbounded repeat", -time.Duration(1 << 62), true, true},
		{"repeat at zero", 0, true, true},
		{"negative one-shot origin", -time.Nanosecond, false, true},
		{"stopped repeat", 0, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := backend.NewDirectorySaveStore(filepath.Join(t.TempDir(), "owner"))
			if err := store.StoreSave("progress", []byte("saved")); err != nil {
				t.Fatal(err)
			}
			s, err := Start(t.Context(), archive, Options{SaveStore: store, Clock: ktf.NewManualClock(time.Unix(1, 0))})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			data, err := s.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			checkpoint, err := backend.DecodeCheckpoint(data, backend.SaveIdentity(archive))
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]json.RawMessage
			if err := json.Unmarshal(checkpoint.Runtime, &record); err != nil {
				t.Fatal(err)
			}
			var audio backend.AudioState
			if err := backend.DecodeCheckpointRecord(record["Audio"], &audio); err != nil {
				t.Fatal(err)
			}
			audio.Next = 1
			audio.Sounds = []backend.AudioSoundState{{Handle: 1,
				Events: []smaf.Event{{Type: smaf.EventEnd, Time: 1}}, Length: time.Millisecond,
				StartedAt: test.startedAt, Repeat: test.repeat, Playing: test.playing}}
			record["Audio"], err = backend.EncodeCheckpointRecord(audio)
			if err != nil {
				t.Fatal(err)
			}
			record["Clip"], record["Sounding"] = json.RawMessage("1"), json.RawMessage("true")
			checkpoint.Runtime, err = json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			// Recompute the envelope checksum so only semantic validation can refuse it.
			bad, err := backend.EncodeCheckpoint(checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.StoreSave("progress", []byte("current")); err != nil {
				t.Fatal(err)
			}
			if err := s.SendKey(t.Context(), KeyPress, '5'); err != nil {
				t.Fatal(err)
			}
			previous, client := s.ktfNative, s.ktfNative.Client
			steps := client.Steps()
			if err := s.LoadCheckpoint(t.Context(), archive, bad); err == nil {
				t.Fatal("malformed native audio was adopted")
			}
			if s.ktfNative != previous || s.ktfNative.Client != client || client.Steps() != steps || !s.Running() || s.Failed() != nil || !reflect.DeepEqual(s.HeldKeys(), []int32{'5'}) {
				t.Fatal("refused native audio changed the live session")
			}
			if current, found, err := store.ReadSave("progress"); err != nil || !found || string(current) != "current" {
				t.Fatalf("refused native audio changed durable saves: %q, %t, %v", current, found, err)
			}
			if !s.SkipToNextDeadline() {
				t.Fatal("refused native audio removed the current frame schedule")
			}
			if _, err := s.Tick(t.Context(), 0); err != nil {
				t.Fatalf("current session could not continue after refusal: %v", err)
			}
			if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
				t.Fatalf("valid checkpoint could not be loaded after refusal: %v", err)
			}
			if restored, found, err := store.ReadSave("progress"); err != nil || !found || string(restored) != "saved" {
				t.Fatalf("valid retry did not restore saves: %q, %t, %v", restored, found, err)
			}
		})
	}
}

func startCheckpointFixture(t *testing.T) (*Session, []byte, *backend.MemorySaveStore) {
	t.Helper()
	archive, err := testfixture.KTFCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, err := backend.NewMemorySaveStore([]backend.SaveEntry{{Key: "progress", Data: []byte("checkpoint")}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Start(t.Context(), archive, Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, archive, store
}

func checkpointStartupCount(t *testing.T, s *Session) uint32 {
	t.Helper()
	var data [4]byte
	if err := s.KTF().Client.Core().Memory().Read(testfixture.KTFCheckpointStartupCounter, data[:]); err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(data[:])
}

func TestCheckpointRestoresSharedInputClockAndDurableGeneration(t *testing.T) {
	s, archive, store := startCheckpointFixture(t)
	now := time.Unix(1700000000, 0)
	s.now = func() time.Time { return now }
	s.lastTick = now.Add(-5 * time.Millisecond)
	for _, key := range []int32{49, 50} {
		if err := s.SendKey(t.Context(), KeyPress, key); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SendPointer(t.Context(), PointerPress, 12, 34); err != nil {
		t.Fatal(err)
	}
	s.repeat.Holding(s.heldKey())
	s.repeat.Due(450 * time.Millisecond)
	wantRepeat, _ := s.repeat.CaptureState()
	saved, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	s.SetScale(2)
	s.SendKey(t.Context(), KeyRelease, 49)
	s.SendPointer(t.Context(), PointerRelease, 12, 34)
	if err := store.StoreSave("progress", []byte("later")); err != nil {
		t.Fatal(err)
	}
	if err := s.KTF().Client.Core().Memory().Write(testfixture.KTFCheckpointStartupCounter, []byte{9, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	if err := s.LoadCheckpoint(t.Context(), archive, saved); err != nil {
		t.Fatal(err)
	}
	if checkpointStartupCount(t, s) != 1 || !s.Running() || s.Scale() != 2 {
		t.Fatal("load replayed startup, kept old memory or lost presentation settings")
	}
	if data, _ := store.LoadSave("progress"); string(data) != "checkpoint" {
		t.Fatal("load kept the later save generation")
	}
	if got, err := s.repeat.CaptureState(); err != nil || got != wantRepeat || s.guestSinceLastTick() != 5*time.Millisecond {
		t.Fatalf("shared repeat phase or clock changed: %+v, %v", got, err)
	}
	if !reflect.DeepEqual(s.HeldKeys(), []int32{49, 50}) || !s.pointerHeld.Down || s.pointerHeld.X != 12 || s.pointerHeld.Y != 34 {
		t.Fatal("saved input ownership was lost")
	}
	if err := s.ReleaseHeldInput(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(s.HeldKeys()) != 0 || s.pointerHeld.Down {
		t.Fatal("restored input could not be released")
	}
	if _, err := s.CaptureCheckpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointStartsAfterSourceSessionHasClosed(t *testing.T) {
	s, archive, _ := startCheckpointFixture(t)
	if err := s.Pause(t.Context()); err != nil {
		t.Fatal(err)
	}
	saved, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	store, _ := backend.NewMemorySaveStore(nil)
	restored, err := RestoreCheckpoint(t.Context(), archive, saved, Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if checkpointStartupCount(t, restored) != 1 || !restored.Paused() {
		t.Fatal("restored session replayed startup or lost pause state")
	}
	if _, err := restored.Tick(t.Context(), 0); !errors.Is(err, ErrPaused) {
		t.Fatalf("paused checkpoint ran: %v", err)
	}
	if err := restored.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Tick(t.Context(), 0); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointRejectsWrongArchiveCancellationAndUnsupportedPlatform(t *testing.T) {
	s, archive, _ := startCheckpointFixture(t)
	saved, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	previous := s.KTF()
	if err := s.LoadCheckpoint(t.Context(), append(append([]byte(nil), archive...), 0), saved); !errors.Is(err, backend.ErrCheckpointIdentity) {
		t.Fatalf("wrong archive = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.LoadCheckpoint(canceled, archive, saved); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled load = %v", err)
	}
	if s.KTF() != previous || !s.Running() || checkpointStartupCount(t, s) != 1 {
		t.Fatal("refused load changed source")
	}
	other, err := Start(t.Context(), sktFixture(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.CaptureCheckpoint(t.Context()); !errors.Is(err, ErrCheckpointUnsupported) {
		t.Fatalf("unsupported platform = %v", err)
	}
}

func TestCheckpointMalformedSharedStateCannotCommitSaves(t *testing.T) {
	s, archive, store := startCheckpointFixture(t)
	data, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := backend.DecodeCheckpoint(data, backend.SaveIdentity(archive))
	if err != nil {
		t.Fatal(err)
	}
	var saved checkpointSessionState
	if err := backend.DecodeCheckpointRecord(checkpoint.Session, &saved); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSave("progress", []byte("current")); err != nil {
		t.Fatal(err)
	}
	previous := s.KTF()
	for _, test := range []struct {
		name string
		edit func(*checkpointSessionState)
	}{
		{"version", func(s *checkpointSessionState) { s.Version++ }},
		{"speed mismatch", func(s *checkpointSessionState) { s.Speed = 2 }},
		{"repeat clock", func(s *checkpointSessionState) { s.LastTickAge = 1 }},
		{"duplicate holds", func(s *checkpointSessionState) { s.HeldKeys = []int32{49, 49} }},
		{"unowned repeat key", func(s *checkpointSessionState) { s.Pad.Down, s.Pad.DownCode = true, 49 }},
		{"pointer", func(s *checkpointSessionState) { s.Pointer.X = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := saved
			test.edit(&bad)
			checkpoint.Session, err = backend.EncodeCheckpointRecord(bad)
			if err != nil {
				t.Fatal(err)
			}
			data, err := backend.EncodeCheckpoint(checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.LoadCheckpoint(t.Context(), archive, data); err == nil {
				t.Fatal("malformed shared state was adopted")
			}
			if s.KTF() != previous || !s.Running() {
				t.Fatal("malformed shared state displaced the original")
			}
			if data, _ := store.LoadSave("progress"); string(data) != "current" {
				t.Fatal("malformed shared state reached durable saves")
			}
		})
	}
}

func TestCheckpointInputHoldsAreBoundedAndOwned(t *testing.T) {
	s, _, _ := startCheckpointFixture(t)
	for code := int32(1000); code < 1064; code++ {
		if err := s.SendKey(t.Context(), KeyPress, code); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SendKey(t.Context(), KeyPress, 2000); err == nil {
		t.Fatal("unbounded held keys accepted")
	}
	held := s.HeldKeys()
	held[0] = 2000
	if s.HeldKeys()[0] != 1000 {
		t.Fatal("Host key list aliases live session state")
	}
	if err := s.ReleaseHeldInput(t.Context()); err != nil || len(s.HeldKeys()) != 0 {
		t.Fatalf("input release failed: %v", err)
	}
}

func TestCheckpointSubprocess(t *testing.T) {
	archive, err := testfixture.KTFCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	identity := backend.SaveIdentity(archive)
	if root := os.Getenv("WFEATURE_SHARED_CHECKPOINT_FIXTURE"); root != "" {
		store := backend.NewDirectorySaveStore(root)
		data, exists, err := store.LoadCheckpoint(identity)
		if err != nil || !exists {
			t.Fatalf("restarted process slot = %t, %v", exists, err)
		}
		restored, err := RestoreCheckpoint(t.Context(), archive, data, Options{SaveStore: store})
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		if checkpointStartupCount(t, restored) != 1 || !reflect.DeepEqual(restored.HeldKeys(), []int32{49}) {
			t.Fatal("restarted process replayed startup or lost input")
		}
		if data, _ := store.LoadSave("progress"); string(data) != "saved before process exit" {
			t.Fatal("restarted process kept later ordinary saves")
		}
		if err := restored.ReleaseHeldInput(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := restored.Tick(t.Context(), 0); err != nil {
			t.Fatal(err)
		}
		return
	}
	root := filepath.Join(t.TempDir(), "owner")
	store := backend.NewDirectorySaveStore(root)
	if err := store.StoreSave("progress", []byte("saved before process exit")); err != nil {
		t.Fatal(err)
	}
	source, err := Start(t.Context(), archive, Options{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Close)
	if err := source.SendKey(t.Context(), KeyPress, 49); err != nil {
		t.Fatal(err)
	}
	data, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StoreCheckpoint(identity, data); err != nil {
		t.Fatal(err)
	}
	source.Close()
	if err := store.StoreSave("progress", []byte("written after the checkpoint")); err != nil {
		t.Fatal(err)
	}
	program := os.Getenv("WFEATURE_SHARED_CHECKPOINT_RESTORE_BINARY")
	if program == "" {
		program = os.Args[0]
	}
	process := exec.CommandContext(t.Context(), program, "-test.run=^TestCheckpointSubprocess$", "-test.timeout=20s")
	process.Env = append(os.Environ(), "WFEATURE_SHARED_CHECKPOINT_FIXTURE="+root)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("restore shared-session process: %v\n%s", err, output)
	}
}

package lgt

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func (fixture *lgtWIPIListenerFixture) checkpoint() backend.Checkpoint {
	fixture.t.Helper()
	// The helper allocates outside a guest platform call. Model its return,
	// where those construction pins have already been released.
	fixture.client.releaseJavaPins(0)
	checkpoint, err := fixture.session.CaptureCheckpoint(fixture.t.Context())
	if err != nil {
		fixture.t.Fatal(err)
	}
	return checkpoint
}

func decodeLGTMediaTestRecord(t *testing.T, checkpoint backend.Checkpoint) sessionMediaCheckpointState {
	t.Helper()
	var saved sessionMediaCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestLGTWIPIListenerCheckpointLegacyHasNoInventedHistory(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "playing", true: "paused"}[paused], func(t *testing.T) {
			f := newLGTWIPIListenerFixture(t)
			f.setListener(f.listeners[0])
			f.call("play", true, true)
			f.advance(3 * f.duration())
			if paused {
				f.call("pause", true)
			}
			checkpoint := f.checkpoint()
			saved := decodeLGTMediaTestRecord(t, checkpoint)
			// Removing the envelope reproduces the exact strict version-2
			// writer shape, whose clips had listeners but no callback queue.
			var oldReader sessionCheckpointState
			if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &oldReader); err == nil {
				t.Fatal("an older reader silently accepted a callback checkpoint")
			}
			legacy, err := backend.EncodeCheckpointRecord(saved.Session)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint.Runtime = legacy
			restored := f.restore(checkpoint)
			restored.drain()
			restored.wantHistory()
			if paused {
				restored.advance(time.Hour)
				restored.drain()
				restored.wantHistory()
				restored.call("resume", true)
			}
			restored.advance(restored.duration())
			restored.drain()
			if paused {
				restored.wantHistory(restored.event(0, 5), restored.event(0, 1))
			} else {
				restored.wantHistory(restored.event(0, 1))
			}
			// A subsequent save upgrades the recovered ownership once, and
			// loading it again neither replays START nor resets the counter.
			again := restored.restore(restored.checkpoint())
			if again.drain() != 0 {
				t.Fatal("repeated adoption replayed an old event")
			}
		})
	}
}

func TestLGTWIPIListenerCheckpointRestoresGCOwnership(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	f.setListener(f.listeners[0])
	f.call("play", true)
	f.setListener(f.listeners[1])
	f.call("pause", true)
	f.setListener(0)
	f.call("stop", true)
	dropLGTListenerGuestRoots(f)
	restored := f.restore(f.checkpoint())
	sound := restored.client.clips[restored.clip].handle
	collect(t, restored.client)
	collect(t, restored.client)
	wantLGTListenerTracked(t, restored.client, true, restored.clip, restored.listeners[0], restored.listeners[1])
	restored.drain()
	restored.wantHistory(restored.event(0, 2), restored.event(1, 4))
	dropLGTListenerGuestRoots(restored)
	collect(t, restored.client)
	collect(t, restored.client)
	wantLGTListenerTracked(t, restored.client, false, restored.clip, restored.listeners[0], restored.listeners[1])
	wantLGTListenerSoundReleased(t, restored.client, restored.clip, sound)
}

func TestLGTWIPIListenerCheckpointRejectsMalformedMedia(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	f.newClip()
	f.setListener(f.listeners[0])
	f.call("play", true, true)
	f.advance(2 * f.duration())
	f.call("pause", true)
	checkpoint := f.checkpoint()
	for name, mutate := range map[string]func(*sessionMediaCheckpointState){
		"newer envelope":     func(s *sessionMediaCheckpointState) { s.Version++ },
		"old nested session": func(s *sessionMediaCheckpointState) { s.Session.Version-- },
		"missing owner":      func(s *sessionMediaCheckpointState) { s.Media.Clips = nil },
		"duplicate owner":    func(s *sessionMediaCheckpointState) { s.Media.Clips = append(s.Media.Clips, s.Media.Clips[0]) },
		"unknown owner":      func(s *sessionMediaCheckpointState) { s.Media.Clips[0].Object = 1 },
		"future counter":     func(s *sessionMediaCheckpointState) { s.Media.Clips[0].Completed++ },
		"stale counter":      func(s *sessionMediaCheckpointState) { s.Media.Clips[0].Completed-- },
		"unloaded counter":   func(s *sessionMediaCheckpointState) { s.Session.Client.Clips[0].Loaded = false },
		"pause mismatch":     func(s *sessionMediaCheckpointState) { s.Session.Client.Clips[0].Paused = false },
		"repeat mismatch":    func(s *sessionMediaCheckpointState) { s.Session.Client.Clips[0].Repeat = false },
		"volume mismatch":    func(s *sessionMediaCheckpointState) { s.Session.Client.Clips[0].Volume-- },
		"C callback": func(s *sessionMediaCheckpointState) {
			s.Session.Client.Clips[0].Callback = f.word(testfixture.LGTListenerCallbackAddress)
		},
		"C status":                    func(s *sessionMediaCheckpointState) { s.Session.Client.Clips[0].Status = mediaPaused },
		"C queue":                     func(s *sessionMediaCheckpointState) { s.Session.Client.Clips[0].Pending = []uint32{mediaStarted} },
		"current unissued listener":   func(s *sessionMediaCheckpointState) { s.Session.Client.Clips[0].Listener = 1 },
		"current wrong listener type": func(s *sessionMediaCheckpointState) { s.Session.Client.Clips[0].Listener = f.clip },
		"queued missing clip":         func(s *sessionMediaCheckpointState) { s.Media.Events[0].Clip = 1 },
		"queued missing recipient":    func(s *sessionMediaCheckpointState) { s.Media.Events[0].Listener = 0 },
		"queued unissued recipient":   func(s *sessionMediaCheckpointState) { s.Media.Events[0].Listener = 1 },
		"queued wrong recipient type": func(s *sessionMediaCheckpointState) { s.Media.Events[0].Listener = f.clip },
		"unsupported event":           func(s *sessionMediaCheckpointState) { s.Media.Events[0].Code = 6 },
		"missing event":               func(s *sessionMediaCheckpointState) { s.Media.Events[0].Code = 0 },
		"event queue limit": func(s *sessionMediaCheckpointState) {
			s.Media.Events = slices.Repeat(s.Media.Events[:1], maxJavaMediaEvents+1)
		},
		"shared sound owner": func(s *sessionMediaCheckpointState) {
			owner := s.Session.Client.Clips[1].Handle
			s.Session.Client.Clips[1] = s.Session.Client.Clips[0]
			s.Session.Client.Clips[1].Handle = owner
			s.Media.Clips[1].Completed = s.Media.Clips[0].Completed
		},
		"forged raw interface": func(s *sessionMediaCheckpointState) {
			header := f.word(testfixture.LGTListenerClassHandle) - javaClassHeader
			setLGTMediaCheckpointWord(t, &s.Session.Client, header+javaClassInterfaces, 0)
			setLGTMediaCheckpointWord(t, &s.Session.Client, header+javaClassMethodRun, 0)
		},
		"forged raw method": func(s *sessionMediaCheckpointState) {
			run := f.word(f.word(testfixture.LGTListenerClassHandle) - javaClassHeader + javaClassMethodRun)
			setLGTMediaCheckpointWord(t, &s.Session.Client, run+4+javaMethodWords*4+20, 0)
		},
	} {
		t.Run(name, func(t *testing.T) {
			saved := decodeLGTMediaTestRecord(t, checkpoint)
			mutate(&saved)
			damaged := checkpoint
			var err error
			damaged.Runtime, err = backend.EncodeCheckpointRecord(saved)
			if err != nil {
				t.Fatal(err)
			}
			if prepared, err := PrepareSessionCheckpoint(f.archive, damaged, f.session.options); err == nil {
				prepared.Discard()
				t.Fatal("malformed media checkpoint was accepted")
			}
		})
	}
	// The failed preparations touched neither the source queue nor playback.
	f.drain()
	f.wantHistory(f.event(0, 2), f.event(0, 1), f.event(0, 1), f.event(0, 4))
}

func setLGTMediaCheckpointWord(t *testing.T, saved *clientState, address, value uint32) {
	t.Helper()
	for _, page := range saved.Core.Memory.Pages {
		if uint64(address) >= uint64(page.Address) && uint64(address)+4 <= uint64(page.Address)+uint64(len(page.Data)) {
			binary.LittleEndian.PutUint32(page.Data[address-page.Address:], value)
			return
		}
	}
	t.Fatalf("checkpoint has no word at %#x", address)
}

func TestLGTWIPIListenerCheckpointReadersKeepStrictShapes(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	checkpoint := f.checkpoint()
	saved := decodeLGTMediaTestRecord(t, checkpoint)
	legacy, err := backend.EncodeCheckpointRecord(saved.Session)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range [][]byte{checkpoint.Runtime, legacy} {
		for name, damaged := range map[string][]byte{
			"unknown field":   append([]byte(`{"Unknown":0,`), record[1:]...),
			"duplicate field": bytes.Replace(record, []byte(`"Version":`), []byte(`"Version":2,"Version":`), 1),
			"missing field":   bytes.Replace(record, []byte(`"Version":`), []byte(`"OtherVersion":`), 1),
		} {
			if _, _, err := decodeLGTSessionRecord(damaged); err == nil {
				t.Fatalf("strict reader accepted %s", name)
			}
		}
	}
}

func TestLGTWIPIListenerCheckpointKeepsEventsAfterBufferClear(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	f.setListener(f.listeners[0])
	f.call("play", true)
	// Finish by guest time without a Host advance. clearData must reconcile
	// the old pass before disposing of its mixer handle.
	f.client.clock.advance(f.duration())
	if _, err := javaClipClearData(f.client, t.Context(), f.client.thread, []uint32{f.clip}); err != nil {
		t.Fatal(err)
	}
	f.setListener(0)
	restored := f.restore(f.checkpoint())
	for _, current := range []*lgtWIPIListenerFixture{f, restored} {
		current.drain()
		current.wantHistory(current.event(0, 2), current.event(0, 1))
		if current.client.clips[current.clip].loaded || len(current.client.clips[current.clip].data) != 0 {
			t.Fatal("cleared Clip regained its buffer or mixer handle")
		}
	}
}

func (fixture *lgtWIPIListenerFixture) restore(checkpoint backend.Checkpoint) *lgtWIPIListenerFixture {
	fixture.t.Helper()
	prepared, err := PrepareSessionCheckpoint(fixture.archive, checkpoint, fixture.session.options)
	if err != nil {
		fixture.t.Fatal(err)
	}
	defer prepared.Discard()
	if calls, first := prepared.PreparationStoreCalls(); calls != 0 {
		fixture.t.Fatalf("media checkpoint preparation touched saves: %d, %s", calls, first)
	}
	restored, err := prepared.Commit(fixture.t.Context(), nil, fixture.store)
	if err != nil {
		fixture.t.Fatal(err)
	}
	fixture.t.Cleanup(func() { _ = restored.Close(fixture.t.Context()) })
	copy := *fixture
	copy.session, copy.client = restored, restored.client
	if copy.word(testfixture.LGTListenerStartupCounter) != 1 {
		fixture.t.Fatal("media checkpoint restarted the application")
	}
	return &copy
}

func TestLGTWIPIListenerCheckpointPendingRecipients(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	f.setListener(f.listeners[0])
	f.call("play", true)
	f.setListener(f.listeners[1])
	f.call("pause", true)
	f.call("resume", true)
	f.call("stop", true)
	f.setListener(0)
	restored := f.restore(f.checkpoint())
	for _, current := range []*lgtWIPIListenerFixture{f, restored} {
		current.wantHistory()
		if delivered := current.drain(); delivered != 4 {
			t.Fatalf("pending events after checkpoint = %d, want 4", delivered)
		}
		current.wantHistory(current.event(0, 2), current.event(1, 4), current.event(1, 5), current.event(1, 3))
		if delivered := current.drain(); delivered != 0 {
			t.Fatalf("checkpoint replayed %d events twice", delivered)
		}
	}
}

func TestLGTWIPIListenerCheckpointPausedRepeat(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	f.setListener(f.listeners[0])
	f.call("play", true, true)
	f.advance(f.duration() / 2)
	f.call("pause", true)
	restored := f.restore(f.checkpoint())
	for _, current := range []*lgtWIPIListenerFixture{f, restored} {
		current.drain()
		current.advance(time.Hour)
		current.drain()
		current.wantHistory(current.event(0, 2), current.event(0, 4))
		current.call("resume", true)
		current.advance(current.duration() / 2)
		current.drain()
		current.wantHistory(current.event(0, 2), current.event(0, 4), current.event(0, 5), current.event(0, 1))
	}
}

func TestLGTWIPIListenerCheckpointCompletedPasses(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	f.setListener(f.listeners[0])
	f.call("play", true, true)
	f.advance(3 * f.duration())
	restored := f.restore(f.checkpoint())
	for _, current := range []*lgtWIPIListenerFixture{f, restored} {
		current.drain()
		current.wantHistory(current.event(0, 2), current.event(0, 1), current.event(0, 1), current.event(0, 1))
		current.advance(current.duration())
		if count := current.drain(); count != 1 {
			t.Fatalf("the next repeat pass delivered %d events", count)
		}
	}
}

package backend

import (
	"math"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

func finitePlaybackEvents() []smaf.Event {
	return []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 100, Type: smaf.EventNoteOff, Note: 60},
		{Time: 400, Type: smaf.EventEnd},
	}
}

func newCountedAudio(t *testing.T, count int32) (*Audio, *ownedAudioProbe, AudioHandle) {
	t.Helper()
	sink := &ownedAudioProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
	handle, err := audio.LoadEvents(finitePlaybackEvents())
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.PlayCount(handle, 0, count); err != nil {
		t.Fatal(err)
	}
	return audio, sink, handle
}

func requireAudioPlayback(t *testing.T, audio *Audio, handle AudioHandle, at time.Duration, want AudioPlayback) {
	t.Helper()
	got, err := audio.Playback(handle, at)
	if err != nil || got != want {
		t.Fatalf("playback at %v = %+v, %v; want %+v", at, got, err, want)
	}
}

func TestAudioPlayCountCompletesExactPassesOnCoarseAdvance(t *testing.T) {
	for _, count := range []int32{1, 2, 3} {
		t.Run(strconv.Itoa(int(count)), func(t *testing.T) {
			audio, sink, handle := newCountedAudio(t, count)
			audio.Advance(1300 * time.Millisecond)
			want := AudioPlayback{Position: 400 * time.Millisecond, Length: 400 * time.Millisecond, Completed: uint64(count)}
			requireAudioPlayback(t, audio, handle, 1300*time.Millisecond, want)
			if len(sink.ofType(smaf.EventNoteOn)) != int(count) || len(sink.ofType(smaf.EventNoteOff)) != int(count) {
				t.Fatalf("%d passes emitted incorrect note gates: %+v", count, sink.events)
			}
			emitted := len(sink.events)
			requireAudioPlayback(t, audio, handle, 10*time.Second, want)
			if len(sink.events) != emitted {
				t.Fatal("a later query repeated the final completion")
			}
			if _, loaded := audio.Length(handle); !loaded {
				t.Fatal("natural completion reclaimed a reusable clip")
			}
		})
	}
}

func TestAudioPlaybackReportsCurrentPassAndTrailingSilence(t *testing.T) {
	audio, _, handle := newCountedAudio(t, 3)
	for _, step := range []struct {
		at, position time.Duration
		completed    uint64
		playing      bool
	}{
		{0, 0, 0, true},
		{175, 175, 0, true},
		{399, 399, 0, true},
		{400, 0, 1, true},
		{550, 150, 1, true},
		{800, 0, 2, true},
		{1199, 399, 2, true},
		{1200, 400, 3, false},
	} {
		requireAudioPlayback(t, audio, handle, step.at*time.Millisecond, AudioPlayback{
			Playing: step.playing, Position: step.position * time.Millisecond, Length: 400 * time.Millisecond, Completed: step.completed,
		})
	}
}

func TestAudioPlaybackRestartsAndExplicitStopResetsPosition(t *testing.T) {
	audio, _, handle := newCountedAudio(t, 1)
	requireAudioPlayback(t, audio, handle, time.Second, AudioPlayback{
		Position: 400 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 1,
	})
	if err := audio.PlayCount(handle, 2*time.Second, 3); err != nil {
		t.Fatal(err)
	}
	requireAudioPlayback(t, audio, handle, 2*time.Second, AudioPlayback{Playing: true, Length: 400 * time.Millisecond})
	requireAudioPlayback(t, audio, handle, 2650*time.Millisecond, AudioPlayback{
		Playing: true, Position: 250 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 1,
	})
	audio.Stop(handle)
	requireAudioPlayback(t, audio, handle, 3*time.Second, AudioPlayback{Length: 400 * time.Millisecond, Completed: 1})
	if err := audio.PlayCount(handle, 3*time.Second, 1); err != nil {
		t.Fatal(err)
	}
	requireAudioPlayback(t, audio, handle, 3050*time.Millisecond, AudioPlayback{
		Playing: true, Position: 50 * time.Millisecond, Length: 400 * time.Millisecond,
	})
}

func TestAudioPlayCountPauseRetainsPassAndReconfiguresRemainingLoops(t *testing.T) {
	for _, count := range []int32{0, 1, 2, 3, -1} {
		name := "unchanged"
		if count != 0 {
			name = strconv.Itoa(int(count))
		}
		t.Run(name, func(t *testing.T) {
			audio, _, handle := newCountedAudio(t, 3)
			requireAudioPlayback(t, audio, handle, 550*time.Millisecond, AudioPlayback{
				Playing: true, Position: 150 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 1,
			})
			if err := audio.Pause(handle, 550*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				if err := audio.SetLoopCount(handle, count); err != nil {
					t.Fatal(err)
				}
			}
			requireAudioPlayback(t, audio, handle, 10*time.Second, AudioPlayback{
				Paused: true, Position: 150 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 1,
			})
			if err := audio.Resume(handle, 10550*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			requireAudioPlayback(t, audio, handle, 10799*time.Millisecond, AudioPlayback{
				Playing: true, Position: 399 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 1,
			})
			want := AudioPlayback{Position: 400 * time.Millisecond, Length: 400 * time.Millisecond, Completed: uint64(count + 1)}
			if count == 0 {
				want.Completed = 3
			} else if count == -1 {
				want.Playing, want.Position, want.Completed = true, 350*time.Millisecond, 4
			}
			requireAudioPlayback(t, audio, handle, 11950*time.Millisecond, want)
		})
	}
}

func TestAudioPlaybackCheckpointPreservesFiniteProgress(t *testing.T) {
	for _, phase := range []string{"running", "paused", "completed"} {
		t.Run(phase, func(t *testing.T) {
			audio, _, handle := newCountedAudio(t, 3)
			at := 550 * time.Millisecond
			if phase == "completed" {
				at = 1300 * time.Millisecond
			}
			before, err := audio.Playback(handle, at)
			if err != nil {
				t.Fatal(err)
			}
			if phase == "paused" {
				if err := audio.Pause(handle, at); err != nil {
					t.Fatal(err)
				}
				before.Playing, before.Paused = false, true
			}
			saved := captureOwnedAudio(t, audio)
			if saved.Version != audioStateVersion || saved.Sounds[0].Completed != before.Completed || saved.Sounds[0].Position != before.Position {
				t.Fatal("checkpoint omitted finite completion or media position")
			}
			if phase != "completed" && saved.Sounds[0].Remaining != 1 {
				t.Fatalf("remaining future passes = %d, want one", saved.Sounds[0].Remaining)
			}
			encoded, err := EncodeCheckpointRecord(saved)
			if err != nil {
				t.Fatal(err)
			}
			var decoded AudioState
			if err := DecodeCheckpointRecord(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			now := time.Unix(2000, 0)
			sink := &ownedAudioProbe{}
			fresh, err := NewAudioFromStateWithClock(decoded, sink, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Minute)
			fresh.ActivateOutputClock()
			requireAudioPlayback(t, fresh, handle, at, before)
			if len(sink.events) != 0 {
				t.Fatal("restoring progress emitted previously consumed score events")
			}
			end := 1200 * time.Millisecond
			if phase == "paused" {
				if err := fresh.Resume(handle, at+time.Second); err != nil {
					t.Fatal(err)
				}
				end += time.Second
			}
			if phase != "completed" {
				requireAudioPlayback(t, fresh, handle, end-time.Nanosecond, AudioPlayback{
					Playing: true, Position: 400*time.Millisecond - time.Nanosecond, Length: 400 * time.Millisecond, Completed: 2,
				})
			} else {
				end = 2 * time.Second
			}
			requireAudioPlayback(t, fresh, handle, end, AudioPlayback{
				Position: 400 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 3,
			})
		})
	}
}

func TestAudioPlaybackQueryPreservesPausedPeer(t *testing.T) {
	audio, sink, first := newCountedAudio(t, 2)
	second, err := audio.LoadEvents(finitePlaybackEvents())
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.PlayCount(second, 0, 3); err != nil {
		t.Fatal(err)
	}
	if err := audio.Pause(second, 0); err != nil {
		t.Fatal(err)
	}
	before := captureOwnedAudio(t, audio).Sounds[1]
	sink.events = nil
	requireAudioPlayback(t, audio, first, 950*time.Millisecond, AudioPlayback{
		Position: 400 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 2,
	})
	if after := captureOwnedAudio(t, audio).Sounds[1]; !reflect.DeepEqual(before, after) {
		t.Fatal("querying one owner advanced its paused peer's cursor or completion count")
	}
	for _, event := range sink.events {
		if event.sound != first {
			t.Fatal("querying one owner emitted a paused peer event")
		}
	}
}

func TestAudioGlobalAdvancePreservesPausedOwnerAndReclaimsTransient(t *testing.T) {
	audio, sink, first := newCountedAudio(t, 3)
	second, err := audio.LoadEvents(finitePlaybackEvents())
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.PlayCount(second, 0, 2); err != nil {
		t.Fatal(err)
	}
	if err := audio.PlayTransient(finitePlaybackEvents(), 0); err != nil {
		t.Fatal(err)
	}
	if err := audio.Pause(first, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	before := captureOwnedAudio(t, audio)
	transient := before.Sounds[2].Handle
	sink.events, sink.stops = nil, nil
	audio.Advance(950 * time.Millisecond)
	after := captureOwnedAudio(t, audio)
	if len(after.Sounds) != 2 || !reflect.DeepEqual(after.Sounds[0], before.Sounds[0]) {
		t.Fatal("global advance changed the paused owner's cursor or completion count")
	}
	if len(after.Output.Notes) != 1 || after.Output.Notes[0].Sound != first || !reflect.DeepEqual(after.Output.Notes[0], before.Output.Notes[0]) {
		t.Fatal("global advance changed the paused owner's retained note")
	}
	for _, event := range sink.events {
		if event.sound == first {
			t.Fatal("a paused owner emitted a score event")
		}
	}
	if _, loaded := audio.Length(transient); loaded || !slices.Equal(sink.stops, []AudioHandle{transient}) {
		t.Fatal("the completed transient was not reclaimed exactly once")
	}
	requireAudioPlayback(t, audio, second, 950*time.Millisecond, AudioPlayback{
		Position: 400 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 2,
	})
	if err := audio.Resume(first, time.Second); err != nil {
		t.Fatal(err)
	}
	audio.Advance(2300 * time.Millisecond)
	requireAudioPlayback(t, audio, first, 2300*time.Millisecond, AudioPlayback{
		Position: 400 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 3,
	})
}

func TestAudioFiniteCompletionKeepsPCMTailsAndPeerOutput(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &ownedAudioProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	first := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{0, 1, 2, 3, 4, 5, 6, 7}},
		{Time: 400, Type: smaf.EventEnd},
	})
	second := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 67, Velocity: 90},
		{Time: 5000, Type: smaf.EventEnd},
	})
	if err := audio.PlayCount(first, 0, 2); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	sink.stops = nil
	now = now.Add(400 * time.Millisecond)
	requireAudioPlayback(t, audio, first, 400*time.Millisecond, AudioPlayback{Playing: true, Length: 400 * time.Millisecond, Completed: 1})
	now = now.Add(400 * time.Millisecond)
	requireAudioPlayback(t, audio, first, 800*time.Millisecond, AudioPlayback{
		Position: 400 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 2,
	})
	saved := captureOwnedAudio(t, audio)
	if len(sink.stops) != 0 || len(saved.Output.Waves) != 2 || len(saved.Output.Notes) != 1 || saved.Output.Notes[0].Sound != second || !audio.Playing(second) {
		t.Fatal("finite completion cancelled a PCM tail or changed another owner")
	}
	if !slices.Equal(saved.Output.Waves[0].Samples, []int16{3, 4, 5, 6, 7}) || !slices.Equal(saved.Output.Waves[1].Samples, []int16{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatal("finite loop boundaries restarted or discarded PCM tails")
	}
	audio.Stop(first)
	requireAudioPlayback(t, audio, first, time.Second, AudioPlayback{Length: 400 * time.Millisecond, Completed: 2})
	saved = captureOwnedAudio(t, audio)
	if !slices.Equal(sink.stops, []AudioHandle{first}) || len(saved.Output.Waves) != 0 || len(saved.Output.Notes) != 1 || saved.Output.Notes[0].Sound != second {
		t.Fatal("explicit stop did not cancel only the completed owner's tails")
	}
}

func TestAudioPlayCountRefusesInvalidRequestsWithoutMutation(t *testing.T) {
	audio, sink, handle := newCountedAudio(t, 3)
	if _, err := audio.Playback(handle, 550*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []struct {
		name string
		call func() error
	}{
		{"zero play count", func() error { return audio.PlayCount(handle, time.Second, 0) }},
		{"negative play count", func() error { return audio.PlayCount(handle, time.Second, -2) }},
		{"negative origin", func() error { return audio.PlayCount(handle, -time.Nanosecond, 1) }},
		{"overflow origin", func() error { return audio.PlayCount(handle, time.Duration(math.MaxInt64), 1) }},
		{"negative rewind clock", func() error { return audio.Rewind(handle, -time.Nanosecond) }},
		{"overflow rewind clock", func() error { return audio.Rewind(handle, time.Duration(math.MaxInt64)) }},
		{"zero remaining count", func() error { return audio.SetLoopCount(handle, 0) }},
		{"negative remaining count", func() error { return audio.SetLoopCount(handle, -2) }},
		{"backward playback query", func() error { _, err := audio.Playback(handle, 549*time.Millisecond); return err }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			before := captureOwnedAudio(t, audio)
			sink.events, sink.stops = nil, nil
			if err := operation.call(); err == nil {
				t.Fatal("invalid request succeeded")
			}
			if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) || len(sink.events) != 0 || len(sink.stops) != 0 {
				t.Fatal("invalid request changed the running score")
			}
		})
	}
}

func TestAudioPlayCountZeroDurationHasNoRepeatLoop(t *testing.T) {
	audio := NewAudioWithClock(nil, func() time.Time { return time.Unix(1000, 0) })
	handle, err := audio.LoadEvents([]smaf.Event{{Type: smaf.EventEnd}})
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int32{2, -1} {
		before := captureOwnedAudio(t, audio)
		if err := audio.PlayCount(handle, 0, count); err == nil {
			t.Fatalf("zero-duration score accepted %d passes", count)
		}
		if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) {
			t.Fatal("refused zero-duration repeat changed state")
		}
	}
	if err := audio.PlayCount(handle, 0, 1); err != nil {
		t.Fatal(err)
	}
	requireAudioPlayback(t, audio, handle, 0, AudioPlayback{Completed: 1})
	if err := audio.Play(handle, 0, true); err != nil {
		t.Fatal(err)
	}
	requireAudioPlayback(t, audio, handle, 0, AudioPlayback{Completed: 1})
}

func TestAudioPlaybackCheckpointRejectsMalformedProgress(t *testing.T) {
	audio, _, handle := newCountedAudio(t, 3)
	if _, err := audio.Playback(handle, 550*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := audio.Pause(handle, 550*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	saved := captureOwnedAudio(t, audio)
	for _, damage := range []struct {
		name string
		edit func(*AudioSoundState)
	}{
		{"negative remaining", func(s *AudioSoundState) { s.Remaining = -1 }},
		{"remaining exceeds an int count", func(s *AudioSoundState) { s.Remaining = math.MaxInt32 }},
		{"finite and infinite", func(s *AudioSoundState) { s.Repeat = true }},
		{"negative position", func(s *AudioSoundState) { s.Position = -time.Nanosecond }},
		{"position beyond score", func(s *AudioSoundState) { s.Position = s.Length + time.Nanosecond }},
		{"position disagrees with pause", func(s *AudioSoundState) { s.Position-- }},
		{"completed counter overflow", func(s *AudioSoundState) { s.Completed = math.MaxUint64 }},
	} {
		t.Run(damage.name, func(t *testing.T) {
			bad := saved
			bad.Sounds = slices.Clone(saved.Sounds)
			damage.edit(&bad.Sounds[0])
			sink := &ownedAudioProbe{}
			if fresh, err := NewAudioFromState(bad, sink); err == nil || fresh != nil {
				t.Fatal("malformed loop progress was accepted")
			}
			if len(sink.events) != 0 || len(sink.stops) != 0 {
				t.Fatal("malformed loop progress reached the output sink")
			}
		})
	}
}

func TestAudioPlaybackReclaimsOnlyCompletedTransientOwner(t *testing.T) {
	sink := &ownedAudioProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
	reusable := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{0, 1, 2, 3, 4, 5, 6, 7}},
		{Time: 5000, Type: smaf.EventEnd},
	})
	if err := audio.PlayTransient([]smaf.Event{
		{Type: smaf.EventNoteOn, Note: 67, Velocity: 100},
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{4, 5, 6, 7}},
		{Time: 100, Type: smaf.EventEnd},
	}, 0); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	before := captureOwnedAudio(t, audio)
	transient := before.Sounds[1].Handle
	sink.stops = nil
	requireAudioPlayback(t, audio, transient, 100*time.Millisecond, AudioPlayback{
		Position: 100 * time.Millisecond, Length: 100 * time.Millisecond, Completed: 1,
	})
	before.Sounds[0].Position = 100 * time.Millisecond
	after := captureOwnedAudio(t, audio)
	if len(after.Sounds) != 1 || !reflect.DeepEqual(before.Sounds[0], after.Sounds[0]) ||
		len(after.Output.Notes) != 1 || after.Output.Notes[0].Sound != reusable || len(after.Output.Waves) != 1 || after.Output.Waves[0].Sound != reusable ||
		!slices.Equal(sink.stops, []AudioHandle{transient}) {
		t.Fatal("transient progress query retained completed output or changed its peer beyond progress")
	}
	if _, err := audio.Playback(transient, time.Second); err == nil {
		t.Fatal("completed transient retained a reusable handle")
	}
}

func TestAudioPlaybackPausedIdleAndCompletedStatesRemainCapturable(t *testing.T) {
	for _, phase := range []string{"unstarted", "stopped", "completed", "stopped after completion"} {
		t.Run(phase, func(t *testing.T) {
			audio := NewAudioWithClock(nil, func() time.Time { return time.Unix(1000, 0) })
			handle, err := audio.LoadEvents(finitePlaybackEvents())
			if err != nil {
				t.Fatal(err)
			}
			at := 150 * time.Millisecond
			want := AudioPlayback{Paused: true, Length: 400 * time.Millisecond}
			if phase != "unstarted" {
				if err := audio.PlayCount(handle, 0, 1); err != nil {
					t.Fatal(err)
				}
				if phase != "stopped" {
					at, want.Completed = 550*time.Millisecond, 1
				}
				if _, err := audio.Playback(handle, at); err != nil {
					t.Fatal(err)
				}
				if phase == "completed" {
					want.Position = 400 * time.Millisecond
				} else {
					audio.Stop(handle)
				}
			}
			if err := audio.Pause(handle, at); err != nil {
				t.Fatal(err)
			}
			saved := captureOwnedAudio(t, audio)
			fresh, err := NewAudioFromState(saved, nil)
			if err != nil {
				t.Fatal(err)
			}
			requireAudioPlayback(t, fresh, handle, at, want)
		})
	}
}

func TestAudioRewindRetainsCompletedPassesAndFiniteBudget(t *testing.T) {
	audio, _, handle := newCountedAudio(t, 2)
	requireAudioPlayback(t, audio, handle, 550*time.Millisecond, AudioPlayback{
		Playing: true, Position: 150 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 1,
	})
	if err := audio.Rewind(handle, 600*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	requireAudioPlayback(t, audio, handle, 600*time.Millisecond, AudioPlayback{Playing: true, Length: 400 * time.Millisecond, Completed: 1})
	requireAudioPlayback(t, audio, handle, 999*time.Millisecond, AudioPlayback{
		Playing: true, Position: 399 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 1,
	})
	requireAudioPlayback(t, audio, handle, time.Second, AudioPlayback{
		Position: 400 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 2,
	})
	if err := audio.Rewind(handle, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	requireAudioPlayback(t, audio, handle, 2*time.Second, AudioPlayback{Length: 400 * time.Millisecond, Completed: 2})
	if err := audio.PlayCount(handle, 2*time.Second, -1); err != nil {
		t.Fatal(err)
	}
	requireAudioPlayback(t, audio, handle, 2550*time.Millisecond, AudioPlayback{
		Playing: true, Position: 150 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 1,
	})
	if err := audio.Rewind(handle, 2600*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	requireAudioPlayback(t, audio, handle, 3450*time.Millisecond, AudioPlayback{
		Playing: true, Position: 50 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 3,
	})
}

func TestAudioRewindResetsOnlyOwnerAndRetainsPausedLoopBudget(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &resumeAudioProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	events := append([]smaf.Event{{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{0, 1, 2, 3, 4, 5, 6, 7}}}, finitePlaybackEvents()...)
	first := loadOwnedAudio(t, audio, events)
	second := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 67, Velocity: 90},
		{Time: 5000, Type: smaf.EventEnd},
	})
	if err := audio.PlayCount(first, 0, 3); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	now = now.Add(550 * time.Millisecond)
	if err := audio.Pause(first, 550*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	before := captureOwnedAudio(t, audio)
	if len(before.Output.Waves) != 2 {
		t.Fatal("test did not retain PCM from both passes")
	}
	now = now.Add(50 * time.Millisecond)
	sink.stops = nil
	if err := audio.Rewind(first, 600*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	after := captureOwnedAudio(t, audio)
	// Rewind services peers to the command boundary before changing its owner.
	before.Sounds[1].Position = 600 * time.Millisecond
	if !slices.Equal(sink.stops, []AudioHandle{first}) || len(after.Output.Waves) != 0 || len(after.Output.Notes) != 1 || after.Output.Notes[0].Sound != second ||
		!reflect.DeepEqual(before.Sounds[1], after.Sounds[1]) || after.Sounds[0].Remaining != 1 || !after.Output.Sounds[0].Paused {
		t.Fatal("rewind changed its peer or retained old output instead of a frozen zero cursor")
	}
	now = now.Add(10 * time.Second)
	requireAudioPlayback(t, audio, first, 10600*time.Millisecond, AudioPlayback{
		Paused: true, Length: 400 * time.Millisecond, Completed: 1,
	})
	sink.events, sink.resumed = nil, nil
	if err := audio.Resume(first, 10600*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(sink.ofType(smaf.EventWave)) != 0 || len(sink.resumed) != 0 {
		t.Fatal("resuming a rewound cursor reconstructed pre-rewind output")
	}
	requireAudioPlayback(t, audio, first, 10600*time.Millisecond, AudioPlayback{Playing: true, Length: 400 * time.Millisecond, Completed: 1})
	waves := sink.ofType(smaf.EventWave)
	if len(waves) != 1 || waves[0].sound != first || !slices.Equal(waves[0].event.Wave, []int16{0, 1, 2, 3, 4, 5, 6, 7}) {
		t.Fatal("rewound PCM did not start from its first frame")
	}
	requireAudioPlayback(t, audio, first, 11399*time.Millisecond, AudioPlayback{
		Playing: true, Position: 399 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 2,
	})
	requireAudioPlayback(t, audio, first, 11400*time.Millisecond, AudioPlayback{
		Position: 400 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 3,
	})
}

func requireAudioNextCompletion(t *testing.T, audio *Audio, handle AudioHandle, want time.Duration, wantOK bool) {
	t.Helper()
	if got, ok := audio.NextCompletion(handle); got != want || ok != wantOK {
		t.Fatalf("next completion for %d = %v, %v; want %v, %v", handle, got, ok, want, wantOK)
	}
}

func TestAudioNextCompletionTracksPassesWithoutAdvancing(t *testing.T) {
	for _, mode := range []string{"default", "finite", "repeat"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Unix(1000, 0)
			clockReads := 0
			sink := &ownedAudioProbe{}
			audio := NewAudioWithClock(sink, func() time.Time {
				clockReads++
				return now
			})
			handle, err := audio.LoadEvents(finitePlaybackEvents())
			if err != nil {
				t.Fatal(err)
			}
			if mode == "finite" {
				err = audio.PlayCount(handle, 1500*time.Millisecond, 3)
			} else {
				err = audio.Play(handle, 1500*time.Millisecond, mode == "repeat")
			}
			if err != nil {
				t.Fatal(err)
			}
			loadOwnedAudio(t, audio, []smaf.Event{
				{Type: smaf.EventNoteOn, Note: 67, Velocity: 90},
				{Time: 5000, Type: smaf.EventEnd},
			})
			audio.Advance(1550 * time.Millisecond)
			// The Host clock can move beyond a deadline without advancing the
			// guest score. A query must leave that overdue boundary observable.
			now = now.Add(10 * time.Second)
			before := captureOwnedAudio(t, audio)
			events, stops, calls := slices.Clone(sink.events), slices.Clone(sink.stops), slices.Clone(sink.calls)
			reads := clockReads
			for range 2 {
				requireAudioNextCompletion(t, audio, handle, 1900*time.Millisecond, true)
			}
			if clockReads != reads {
				t.Fatal("deadline query sampled the Host clock")
			}
			if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) ||
				!reflect.DeepEqual(events, sink.events) || !slices.Equal(stops, sink.stops) || !slices.Equal(calls, sink.calls) {
				t.Fatal("deadline query changed playback, handles, peer state or sink output")
			}
			audio.Advance(2300 * time.Millisecond)
			if mode == "default" {
				requireAudioNextCompletion(t, audio, handle, 0, false)
				return
			}
			requireAudioNextCompletion(t, audio, handle, 2700*time.Millisecond, true)
			audio.Advance(2700 * time.Millisecond)
			if mode == "finite" {
				requireAudioNextCompletion(t, audio, handle, 0, false)
			} else {
				requireAudioNextCompletion(t, audio, handle, 3100*time.Millisecond, true)
			}
		})
	}
}

func TestAudioNextCompletionFollowsResumedOrigin(t *testing.T) {
	audio, _, handle := newCountedAudio(t, 3)
	requireAudioNextCompletion(t, audio, handle, 400*time.Millisecond, true)
	if err := audio.Pause(handle, 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	requireAudioNextCompletion(t, audio, handle, 0, false)
	if err := audio.Resume(handle, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	requireAudioNextCompletion(t, audio, handle, 10250*time.Millisecond, true)
	audio.Advance(10250 * time.Millisecond)
	requireAudioNextCompletion(t, audio, handle, 10650*time.Millisecond, true)
}

func TestAudioNextCompletionOmitsInactiveAndZeroLengthScores(t *testing.T) {
	requireAudioNextCompletion(t, nil, 1, 0, false)
	audio, sink, handle := newCountedAudio(t, 1)
	requireAudioNextCompletion(t, audio, 0, 0, false)
	requireAudioNextCompletion(t, audio, ^AudioHandle(0), 0, false)
	audio.Stop(handle)
	requireAudioNextCompletion(t, audio, handle, 0, false)
	if err := audio.Close(handle); err != nil {
		t.Fatal(err)
	}
	requireAudioNextCompletion(t, audio, handle, 0, false)
	idle, err := audio.LoadEvents(finitePlaybackEvents())
	if err != nil {
		t.Fatal(err)
	}
	requireAudioNextCompletion(t, audio, idle, 0, false)
	zero := loadOwnedAudio(t, audio, []smaf.Event{{Type: smaf.EventEnd}})
	before := captureOwnedAudio(t, audio)
	events, stops := len(sink.events), len(sink.stops)
	requireAudioNextCompletion(t, audio, zero, 0, false)
	if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) || len(sink.events) != events || len(sink.stops) != stops {
		t.Fatal("querying a zero-length score completed or emitted it")
	}
	if err := audio.PlayTransient(finitePlaybackEvents(), time.Second); err != nil {
		t.Fatal(err)
	}
	saved := captureOwnedAudio(t, audio)
	transient := saved.Sounds[len(saved.Sounds)-1].Handle
	requireAudioNextCompletion(t, audio, transient, 1400*time.Millisecond, true)
	audio.Advance(1400 * time.Millisecond)
	requireAudioNextCompletion(t, audio, transient, 0, false)
}

func TestAudioNextCompletionUsesScoreEndInsteadOfPCMTail(t *testing.T) {
	audio := NewAudioWithClock(nil, func() time.Time { return time.Unix(1000, 0) })
	handle := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{0, 1, 2, 3, 4, 5, 6, 7}},
		{Time: 400, Type: smaf.EventEnd},
	})
	audio.Advance(0)
	requireAudioNextCompletion(t, audio, handle, 400*time.Millisecond, true)
	audio.Advance(400 * time.Millisecond)
	requireAudioNextCompletion(t, audio, handle, 0, false)
	if saved := captureOwnedAudio(t, audio); len(saved.Output.Waves) != 1 || saved.Output.Waves[0].Sound != handle {
		t.Fatal("test did not retain the completed score's PCM tail")
	}
}

func TestAudioNextCompletionRefusesOverflowAfterLastRepresentablePass(t *testing.T) {
	audio, sink, handle := newCountedAudio(t, 2)
	last := time.Duration(math.MaxInt64)
	if err := audio.PlayCount(handle, last-400*time.Millisecond, 2); err != nil {
		t.Fatal(err)
	}
	before := captureOwnedAudio(t, audio)
	requireAudioNextCompletion(t, audio, handle, last, true)
	if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) {
		t.Fatal("querying the maximum deadline changed state")
	}
	// The next finite pass starts at MaxInt64; its end is unrepresentable.
	audio.Advance(last)
	events, stops := len(sink.events), len(sink.stops)
	next, current := audio.next, *audio.sounds[handle]
	requireAudioNextCompletion(t, audio, handle, 0, false)
	if audio.next != next || !reflect.DeepEqual(current, *audio.sounds[handle]) || len(sink.events) != events || len(sink.stops) != stops {
		t.Fatal("overflow query changed playback or output")
	}
}

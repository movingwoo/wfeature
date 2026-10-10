package backend

import (
	"errors"
	"maps"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

func TestAudioResourcesRepeatKeepsOneActiveKey(t *testing.T) {
	audio := NewAudio(nil)
	handle, err := audio.LoadEvents([]smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 1, Type: smaf.EventNoteOn, Note: 60, Velocity: 80},
		{Time: 2, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, true); err != nil {
		t.Fatal(err)
	}
	audio.Advance(500 * time.Millisecond)
	saved := captureOwnedAudio(t, audio)
	if got := saved.Sounds[0].ActiveNotes; !reflect.DeepEqual(got, []AudioNoteState{{Note: 60}}) {
		t.Fatalf("250 repeats retained %d active keys, want one", len(got))
	}
}

func TestAudioResourcesZeroVelocityReleasesOnlyItsActiveKey(t *testing.T) {
	audio := NewAudio(nil)
	loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Type: smaf.EventNoteOn, Note: 64, Velocity: 80},
		{Time: 10, Type: smaf.EventNoteOn, Note: 60, Velocity: 0},
		{Time: 20, Type: smaf.EventEnd},
	})
	audio.Advance(10 * time.Millisecond)
	saved := captureOwnedAudio(t, audio)
	if got := saved.Sounds[0].ActiveNotes; !reflect.DeepEqual(got, []AudioNoteState{{Note: 64}}) {
		t.Fatalf("zero-velocity note-on retained active keys %+v, want only note 64", got)
	}
}

func TestAudioResourcesAdmissionOwnsNestedPayloads(t *testing.T) {
	for _, transient := range []bool{false, true} {
		name := "LoadEvents"
		if transient {
			name = "PlayTransient"
		}
		t.Run(name, func(t *testing.T) {
			sink := &gainAudioProbe{}
			audio := NewAudio(sink)
			waveBacking, sysExBacking := make([]int16, 4096), make([]byte, 4096)
			wave, sysEx := waveBacking[7:9], sysExBacking[5:8]
			copy(wave, []int16{7, -9})
			copy(sysEx, []byte{0xf0, 0x7e, 0xf7})
			events := []smaf.Event{
				{Time: 1, Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 8000, Wave: wave},
				{Time: 1, Type: smaf.EventSysEx, SysEx: sysEx},
				{Time: 1, Type: smaf.EventNoteOn, Note: 60, Velocity: 80},
				{Time: 20, Type: smaf.EventEnd},
			}
			if transient {
				if err := audio.PlayTransient(events, 0); err != nil {
					t.Fatal(err)
				}
			} else {
				loadOwnedAudio(t, audio, events)
			}
			// The caller may reuse both the outer array and tiny views into
			// larger sample/message backing arrays immediately after admission.
			wave[0], sysEx[1], events[0].Time, events[2].Note = 777, 0, 10000, 99
			saved := captureOwnedAudio(t, audio)
			retained := saved.Sounds[0].Events
			if !reflect.DeepEqual(retained[0].Wave, []int16{7, -9}) || !reflect.DeepEqual(retained[1].SysEx, []byte{0xf0, 0x7e, 0xf7}) || retained[0].Time != 1 || retained[2].Note != 60 {
				t.Fatalf("admission retained caller-owned data: wave=%v, SysEx=%x, time=%d, note=%d", retained[0].Wave, retained[1].SysEx, retained[0].Time, retained[2].Note)
			}
			audio.Advance(time.Millisecond)
			waves, messages := sink.ofType(smaf.EventWave), sink.ofType(smaf.EventSysEx)
			if len(waves) != 1 || len(messages) != 1 || !reflect.DeepEqual(waves[0].event.Wave, []int16{7, -9}) || !reflect.DeepEqual(messages[0].event.SysEx, []byte{0xf0, 0x7e, 0xf7}) {
				t.Fatal("caller mutation reached the output")
			}
		})
	}
}

func resourceEvents() []smaf.Event {
	return []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: []int16{7, -9}},
		{Type: smaf.EventSysEx, SysEx: []byte{0xf0, 0x7e, 0xf7}},
		{Time: 10, Type: smaf.EventNoteOff, Note: 60},
		{Time: 10, Type: smaf.EventEnd},
	}
}

func resourceAudio(sink AudioSink) *Audio {
	return NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
}

func resourceRefusal(t *testing.T, audio *Audio, sink *gainAudioProbe, admit func(*Audio) (AudioHandle, error)) {
	t.Helper()
	before := captureOwnedAudio(t, audio)
	events, stops, calls, gains := len(sink.events), len(sink.stops), len(sink.calls), maps.Clone(sink.gains)
	if handle, err := admit(audio); handle != 0 || !errors.Is(err, ErrAudioResourceLimit) {
		t.Fatalf("over-budget admission = %d, %v", handle, err)
	}
	if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(after, before) {
		t.Fatal("resource refusal changed the timeline or consumed a handle")
	}
	if len(sink.events) != events || len(sink.stops) != stops || len(sink.calls) != calls || !reflect.DeepEqual(sink.gains, gains) {
		t.Fatal("resource refusal reached the sink")
	}
}

func admitResourceEvents(audio *Audio) (AudioHandle, error) {
	return audio.LoadEvents(resourceEvents())
}

func TestAudioResourceUsageCountsKeysAndAliasedPayloads(t *testing.T) {
	wave, sysEx := []int16{1, 2, 3}, []byte{0xf0, 1, 0xf7}
	events := []smaf.Event{
		{Type: smaf.EventNoteOn, Channel: 2, Note: 60, Velocity: 100},
		{Type: smaf.EventNoteOn, Channel: 2, Note: 60, Velocity: 40},
		{Type: smaf.EventNoteOn, Channel: 2, Note: 61},
		{Type: smaf.EventNoteOff, Channel: 2, Note: 60},
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: wave},
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 4, Wave: wave},
		{Type: smaf.EventSysEx, SysEx: sysEx},
		{Type: smaf.EventSysEx, SysEx: sysEx},
		{Time: 10, Type: smaf.EventEnd},
	}
	for _, test := range []struct {
		active []AudioNoteState
		want   audioResourceUsage
	}{
		{nil, audioResourceUsage{entries: 10, bytes: 730}},
		{[]AudioNoteState{{Channel: 2, Note: 60}, {Channel: 3, Note: 60}}, audioResourceUsage{entries: 11, bytes: 738}},
	} {
		got, err := soundResourceUsage(events, test.active)
		if err != nil || got != test.want {
			t.Fatalf("usage with active keys %+v = %+v, %v; want %+v", test.active, got, err, test.want)
		}
	}
}

func TestAudioResourcesReserveEveryPossibleActiveKeyBeforePlayback(t *testing.T) {
	var events []smaf.Event
	for channel := range uint8(16) {
		for note := range uint8(128) {
			events = append(events, smaf.Event{Type: smaf.EventNoteOn, Channel: channel, Note: note, Velocity: 100})
		}
	}
	events = append(events, smaf.Event{Time: 1, Type: smaf.EventEnd})
	audio := resourceAudio(nil)
	// 2,049 events plus 2,048 possible held keys, including channel 15/key 127.
	audio.resourceLimits = audioResourceUsage{entries: 4096, bytes: 147648}
	if handle, err := audio.LoadEvents(events); handle != 0 || !errors.Is(err, ErrAudioResourceLimit) {
		t.Fatalf("admission failed to reserve future active keys: handle=%d, err=%v", handle, err)
	}
	audio.resourceLimits.entries++
	handle, err := audio.LoadEvents(events)
	if err != nil || handle != 1 {
		t.Fatalf("exact full-key budget rejected: handle=%d, err=%v", handle, err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(0)
	saved := captureOwnedAudio(t, audio)
	if len(saved.Sounds[0].ActiveNotes) != 2048 || len(saved.Output.Notes) != 24 {
		t.Fatalf("active-key reservations and output voices diverged: keys=%d, voices=%d", len(saved.Sounds[0].ActiveNotes), len(saved.Output.Notes))
	}
}

func TestAudioResourcesAdmissionExactBoundaryAndAtomicRefusal(t *testing.T) {
	for _, mode := range []struct {
		name  string
		cost  audioResourceUsage
		admit func(*Audio) (AudioHandle, error)
	}{
		{"LoadEvents", audioResourceUsage{entries: 6, bytes: 463}, admitResourceEvents},
		{"Load", audioResourceUsage{entries: 4, bytes: 328}, func(audio *Audio) (AudioHandle, error) { return audio.Load(oneNoteSMAF()) }},
		{"PlayTransient", audioResourceUsage{entries: 6, bytes: 463}, func(audio *Audio) (AudioHandle, error) {
			if err := audio.PlayTransient(resourceEvents(), 0); err != nil {
				return 0, err
			}
			return audio.next, nil
		}},
	} {
		for _, field := range []string{"entries", "bytes"} {
			t.Run(mode.name+"/"+field, func(t *testing.T) {
				sink := &gainAudioProbe{}
				audio := resourceAudio(sink)
				if field == "entries" {
					audio.resourceLimits.entries = mode.cost.entries - 1
				} else {
					audio.resourceLimits.bytes = mode.cost.bytes - 1
				}
				resourceRefusal(t, audio, sink, mode.admit)
				if field == "entries" {
					audio.resourceLimits.entries++
				} else {
					audio.resourceLimits.bytes++
				}
				first, err := mode.admit(audio)
				if err != nil || first != 1 {
					t.Fatalf("exact boundary admission = %d, %v", first, err)
				}
				resourceRefusal(t, audio, sink, mode.admit)
				if err := audio.Close(first); err != nil {
					t.Fatal(err)
				}
				if next, err := mode.admit(audio); err != nil || next != 2 {
					t.Fatalf("refusal consumed a handle or Close retained resources: next=%d, err=%v", next, err)
				}
			})
		}
	}
}

func TestAudioResourcesRetainStoppedPausedAndCompletedReusableClips(t *testing.T) {
	sink := &gainAudioProbe{}
	audio := resourceAudio(sink)
	audio.resourceLimits = audioResourceUsage{entries: 18, bytes: 1389}
	first := loadOwnedAudio(t, audio, resourceEvents())
	second := loadOwnedAudio(t, audio, resourceEvents())
	third := loadOwnedAudio(t, audio, resourceEvents())
	audio.Advance(0)
	audio.Stop(first)
	if err := audio.Pause(second, 0); err != nil {
		t.Fatal(err)
	}
	audio.Advance(10 * time.Millisecond)
	if audio.Playing(third) || !audio.Paused(second) {
		t.Fatal("fixture did not reach completed and paused playback")
	}
	resourceRefusal(t, audio, sink, admitResourceEvents)
	for _, closed := range []AudioHandle{first, second} {
		if err := audio.Close(closed); err != nil {
			t.Fatal(err)
		}
		if _, err := admitResourceEvents(audio); err != nil {
			t.Fatalf("Close did not free a retained clip: %v", err)
		}
	}
	audio.StopAll()
	resourceRefusal(t, audio, sink, admitResourceEvents)
	if got := len(captureOwnedAudio(t, audio).Sounds); got != 3 {
		t.Fatalf("StopAll changed the reusable clip count to %d", got)
	}
}

func TestAudioResourcesEveryTransientRemovalReleasesBudget(t *testing.T) {
	for _, route := range []string{"Advance", "Playback", "Stop", "Close", "StopAll"} {
		t.Run(route, func(t *testing.T) {
			sink := &gainAudioProbe{}
			audio := resourceAudio(sink)
			audio.resourceLimits = audioResourceUsage{entries: 8, bytes: 656}
			reusable := loadOwnedAudio(t, audio, transientNote(1000))
			if err := audio.PlayTransient(transientNote(10), 0); err != nil {
				t.Fatal(err)
			}
			handle := captureOwnedAudio(t, audio).Next
			audio.Advance(0)
			resourceRefusal(t, audio, sink, func(audio *Audio) (AudioHandle, error) { return audio.LoadEvents(transientNote(10)) })
			switch route {
			case "Advance":
				audio.Advance(10 * time.Millisecond)
			case "Playback":
				if _, err := audio.Playback(handle, 10*time.Millisecond); err != nil {
					t.Fatal(err)
				}
			case "Stop":
				audio.Stop(handle)
			case "Close":
				if err := audio.Close(handle); err != nil {
					t.Fatal(err)
				}
			case "StopAll":
				audio.StopAll()
			}
			if _, exists := audio.Length(handle); exists {
				t.Fatal("transient handle survived removal")
			}
			if _, exists := audio.Length(reusable); !exists || audio.Playing(reusable) != (route != "StopAll") {
				t.Fatal("transient removal changed the reusable clip incorrectly")
			}
			if err := audio.PlayTransient(transientNote(10), 20*time.Millisecond); err != nil {
				t.Fatalf("transient removal did not free its budget: %v", err)
			}
			if saved := captureOwnedAudio(t, audio); len(saved.Sounds) != 2 || saved.Next != 3 {
				t.Fatalf("new transient changed loaded ownership: %+v", saved.Sounds)
			}
		})
	}
}

func TestAudioResourcesRestoreRecomputesCostsAndUsesDefaultLimits(t *testing.T) {
	source := resourceAudio(nil)
	source.resourceLimits = audioResourceUsage{entries: 6, bytes: 463}
	first := loadOwnedAudio(t, source, resourceEvents())
	source.Advance(0)
	saved := captureOwnedAudio(t, source)
	// A saved held key need not occur in its retained event stream. Reserve
	// that union once, in addition to the positive key already in the stream.
	saved.Sounds[0].ActiveNotes = append(saved.Sounds[0].ActiveNotes, AudioNoteState{Channel: 1, Note: 62})
	saved.Sounds[0].UsedChannels[1] = true
	encoded, err := EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AudioState
	if err := DecodeCheckpointRecord(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	sink := &gainAudioProbe{}
	fresh, err := NewAudioFromStateWithClock(decoded, sink, func() time.Time { return time.Unix(2000, 0) })
	if err != nil {
		t.Fatal(err)
	}
	if fresh.resourceLimits != defaultAudioResourceLimits() || len(sink.events) != 0 || len(sink.gains) != 0 {
		t.Fatal("restore retained test limits or emitted output")
	}
	// The restored cost is 7 entries/471 bytes, not 6/463 and not zero.
	fresh.resourceLimits = audioResourceUsage{entries: 12, bytes: 926}
	resourceRefusal(t, fresh, sink, admitResourceEvents)
	fresh.resourceLimits = audioResourceUsage{entries: 13, bytes: 934}
	if next, err := admitResourceEvents(fresh); err != nil || next != first+1 {
		t.Fatalf("recomputed exact budget = handle %d, error %v", next, err)
	}
	resourceRefusal(t, fresh, sink, admitResourceEvents)
	if err := fresh.Close(first); err != nil {
		t.Fatal(err)
	}
	if _, err := admitResourceEvents(fresh); err != nil {
		t.Fatalf("closing the restored sound did not free its recomputed cost: %v", err)
	}
}

func TestAudioResourcesRestoreRejectsMalformedActiveKeysBeforeEmission(t *testing.T) {
	source := resourceAudio(nil)
	loadOwnedAudio(t, source, resourceEvents())
	source.Advance(0)
	saved := captureOwnedAudio(t, source)
	encoded, err := EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		keys []AudioNoteState
	}{
		{"duplicate", []AudioNoteState{{Note: 60}, {Note: 60}}},
		{"channel", []AudioNoteState{{Channel: 16, Note: 60}}},
		{"note", []AudioNoteState{{Note: 128}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var altered AudioState
			if err := DecodeCheckpointRecord(encoded, &altered); err != nil {
				t.Fatal(err)
			}
			altered.Sounds[0].ActiveNotes = test.keys
			sink := &gainAudioProbe{}
			if fresh, err := NewAudioFromState(altered, sink); fresh != nil || err == nil {
				t.Fatal("restore accepted invalid active keys")
			}
			if len(sink.events) != 0 || len(sink.stops) != 0 || len(sink.calls) != 0 || len(sink.gains) != 0 {
				t.Fatal("invalid restored keys reached the output")
			}
		})
	}
	if after := captureOwnedAudio(t, source); !reflect.DeepEqual(after, saved) {
		t.Fatal("refused restoration changed the source")
	}
}

func TestAudioResourcesConcurrentAdmissionSharesOneBudget(t *testing.T) {
	audio := resourceAudio(nil)
	audio.resourceLimits = audioResourceUsage{entries: 18, bytes: 1389}
	results := make(chan error, 32)
	var group sync.WaitGroup
	for range 32 {
		group.Go(func() {
			_, err := admitResourceEvents(audio)
			results <- err
		})
	}
	group.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, ErrAudioResourceLimit) {
			t.Fatalf("concurrent admission failed unexpectedly: %v", err)
		}
	}
	saved := captureOwnedAudio(t, audio)
	if accepted != 3 || len(saved.Sounds) != 3 || saved.Next != 3 {
		t.Fatalf("shared budget accepted %d, retained %d, next handle %d", accepted, len(saved.Sounds), saved.Next)
	}
}

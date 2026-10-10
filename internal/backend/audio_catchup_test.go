package backend

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

func catchupAudioState(t *testing.T, events []smaf.Event) AudioState {
	t.Helper()
	audio := NewAudio(nil)
	loadOwnedAudio(t, audio, events)
	return captureOwnedAudio(t, audio)
}

func checkCatchupBudget(t *testing.T, saved AudioState, elapsed time.Duration, exact audioCatchupLimits) {
	t.Helper()
	if err := validateAudioState(saved); err != nil {
		t.Fatalf("budget fixture has an invalid saved shape: %v", err)
	}
	before, err := EncodeCheckpointRecord(saved)
	if err != nil {
		t.Fatal(err)
	}
	check := func(limits audioCatchupLimits, accepted bool) {
		t.Helper()
		err := validateAudioCatchup(saved, elapsed, limits)
		if (err == nil) != accepted {
			t.Fatalf("catch-up at %v with %+v: %v; accepted=%v", elapsed, limits, err, accepted)
		}
		after, encodeErr := EncodeCheckpointRecord(saved)
		if encodeErr != nil || !bytes.Equal(before, after) {
			t.Fatal("catch-up validation changed the saved state")
		}
	}
	check(exact, true)
	for _, budget := range []string{"events", "bytes", "notes"} {
		less := exact
		var limit *uint64
		switch budget {
		case "events":
			limit = &less.events
		case "bytes":
			limit = &less.bytes
		case "notes":
			limit = &less.notes
		}
		if *limit != 0 {
			*limit--
			check(less, false)
		}
	}
}

func TestAudioCatchupUsesOnlyDueUnconsumedEvents(t *testing.T) {
	events := make([]smaf.Event, 0, 2401)
	for range 1200 {
		events = append(events, smaf.Event{Type: smaf.EventNoteOn, Note: 60, Velocity: 80}, smaf.Event{Type: smaf.EventNoteOff, Note: 60})
	}
	events = append(events, smaf.Event{Time: 100, Type: smaf.EventEnd})
	audio := NewAudio(nil)
	loadOwnedAudio(t, audio, events)
	audio.Advance(0)
	saved := captureOwnedAudio(t, audio)
	checkCatchupBudget(t, saved, 99*time.Millisecond, audioCatchupLimits{})
	checkCatchupBudget(t, saved, 100*time.Millisecond, audioCatchupLimits{events: 1})
	if err := ValidateAudioCatchup(saved, 99*time.Millisecond); err != nil {
		t.Fatalf("consumed score charged as pending work: %v", err)
	}

	saved = catchupAudioState(t, []smaf.Event{
		{Time: 1, Type: smaf.EventProgramChange},
		{Time: 10, Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 8000, Wave: []int16{1, 2, 3}},
		{Time: 10, Type: smaf.EventSysEx, SysEx: []byte{0xf0, 1, 0xf7}},
		{Time: 20, Type: smaf.EventEnd},
	})
	checkCatchupBudget(t, saved, 9*time.Millisecond, audioCatchupLimits{events: 1})
	checkCatchupBudget(t, saved, 10*time.Millisecond, audioCatchupLimits{events: 3, bytes: 9})
	// Consumed payloads must not be charged again either.
	saved.Sounds[0].Cursor = 3
	checkCatchupBudget(t, saved, 20*time.Millisecond, audioCatchupLimits{events: 1})
}

func TestAudioCatchupEventTimeBoundaries(t *testing.T) {
	saved := catchupAudioState(t, []smaf.Event{
		{Time: 1, Type: smaf.EventProgramChange},
		{Time: 2, Type: smaf.EventProgramChange},
		{Time: 2, Type: smaf.EventProgramChange},
		{Time: 3, Type: smaf.EventEnd},
	})
	for _, test := range []struct {
		elapsed time.Duration
		events  uint64
	}{{0, 0}, {time.Millisecond - 1, 0}, {time.Millisecond, 1}, {2 * time.Millisecond, 3}, {3 * time.Millisecond, 4}} {
		checkCatchupBudget(t, saved, test.elapsed, audioCatchupLimits{events: test.events})
	}
	saved.Sounds[0].StartedAt = 10 * time.Millisecond
	checkCatchupBudget(t, saved, 9*time.Millisecond, audioCatchupLimits{})
	checkCatchupBudget(t, saved, 11*time.Millisecond, audioCatchupLimits{events: 1})
}

func TestAudioCatchupAggregatesOwnersAndRepeatedPayloads(t *testing.T) {
	wave, message := []int16{1, 2, 3}, []byte{0xf0, 1, 0xf7}
	saved := catchupAudioState(t, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 80},
		{Time: 1, Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 8000, Wave: wave},
		{Time: 1, Type: smaf.EventSysEx, SysEx: message},
		{Time: 2, Type: smaf.EventEnd},
	})
	saved.Sounds = append(saved.Sounds, saved.Sounds[0])
	saved.Sounds[1].Handle, saved.Next = 2, 2
	checkCatchupBudget(t, saved, 2*time.Millisecond, audioCatchupLimits{events: 8, bytes: 18, notes: 2})

	saved.Sounds = saved.Sounds[:1]
	saved.Sounds[0].Repeat = true
	// Two complete passes and the third pass through its two payloads.
	checkCatchupBudget(t, saved, 5*time.Millisecond, audioCatchupLimits{events: 11, bytes: 27, notes: 2})
	saved.Sounds[0].Repeat, saved.Sounds[0].Remaining = false, 1
	checkCatchupBudget(t, saved, time.Hour, audioCatchupLimits{events: 8, bytes: 18, notes: 2})
}

func TestAudioCatchupRetriggerKeepsOneHeldKey(t *testing.T) {
	saved := catchupAudioState(t, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 80},
		{Time: 1, Type: smaf.EventNoteOff, Note: 61},
		{Time: 2, Type: smaf.EventEnd},
	})
	saved.Sounds[0].Repeat = true
	// One hundred note-ons, one hundred misses, and ninety-nine End records.
	checkCatchupBudget(t, saved, 199*time.Millisecond, audioCatchupLimits{events: 299, notes: 199})
	if err := ValidateAudioCatchup(saved, 20*time.Second); err != nil {
		t.Fatalf("retriggering one key was charged as growing active storage: %v", err)
	}
}

func TestAudioCatchupChargesOrderedSearchAndReleaseCompaction(t *testing.T) {
	for _, test := range []struct {
		name   string
		events []smaf.Event
		notes  uint64
	}{
		{"first retrigger", []smaf.Event{{Type: smaf.EventNoteOn, Note: 60, Velocity: 80}}, 1},
		{"last retrigger", []smaf.Event{{Type: smaf.EventNoteOn, Channel: 2, Note: 60, Velocity: 80}}, 3},
		{"new key", []smaf.Event{{Type: smaf.EventNoteOn, Channel: 3, Note: 60, Velocity: 80}}, 3},
		{"absent release", []smaf.Event{{Type: smaf.EventNoteOff, Note: 61}}, 3},
		{"front release preserves order", []smaf.Event{{Type: smaf.EventNoteOff, Note: 60}, {Type: smaf.EventNoteOn, Channel: 2, Note: 60, Velocity: 80}}, 5},
		{"middle release preserves order", []smaf.Event{{Type: smaf.EventNoteOff, Channel: 1, Note: 60}, {Type: smaf.EventNoteOn, Channel: 2, Note: 60, Velocity: 80}}, 5},
		{"last release", []smaf.Event{{Type: smaf.EventNoteOff, Channel: 2, Note: 60}}, 3},
		{"zero velocity removes key", []smaf.Event{{Type: smaf.EventNoteOn, Note: 60}, {Type: smaf.EventNoteOn, Note: 60, Velocity: 80}}, 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			saved := catchupAudioState(t, append(test.events, smaf.Event{Time: 1, Type: smaf.EventEnd}))
			saved.Sounds[0].ActiveNotes = []AudioNoteState{{Note: 60}, {Channel: 1, Note: 60}, {Channel: 2, Note: 60}}
			checkCatchupBudget(t, saved, 0, audioCatchupLimits{events: uint64(len(test.events)), notes: test.notes})
		})
	}
}

func TestAudioCatchupRejectsExpensiveDistinctKeyWork(t *testing.T) {
	for _, kind := range []string{"growing keys", "absent releases", "front compaction"} {
		t.Run(kind, func(t *testing.T) {
			var events []smaf.Event
			var active []AudioNoteState
			var work uint64
			switch kind {
			case "growing keys":
				for i := range 1450 {
					events = append(events, smaf.Event{Type: smaf.EventNoteOn, Channel: uint8(i / 128), Note: uint8(i % 128), Velocity: 80})
				}
				work = 1_050_525 // 0 + 1 + ... + 1449 comparisons.
			case "absent releases":
				for i := range 1024 {
					active = append(active, AudioNoteState{Channel: uint8(i / 128), Note: uint8(i % 128)})
				}
				for range 1025 {
					events = append(events, smaf.Event{Type: smaf.EventNoteOff, Channel: 15, Note: 127})
				}
				work = 1_049_600 // Each miss visits all 1024 retained keys.
			case "front compaction":
				for i := range 2048 {
					active = append(active, AudioNoteState{Channel: uint8(i / 128), Note: uint8(i % 128)})
				}
				for i := range 600 {
					events = append(events, smaf.Event{Type: smaf.EventNoteOff, Channel: uint8(i / 128), Note: uint8(i % 128)})
				}
				work = 1_049_100 // 2048 + 2047 + ... + 1449 visits/copies.
			}
			saved := catchupAudioState(t, append(events, smaf.Event{Time: 1, Type: smaf.EventEnd}))
			saved.Sounds[0].ActiveNotes = active
			checkCatchupBudget(t, saved, 0, audioCatchupLimits{events: uint64(len(events)), notes: work})
			if err := ValidateAudioCatchup(saved, 0); err == nil {
				t.Fatal("default note-work budget accepted more than one million visits")
			}
		})
	}
}

func TestAudioCatchupHonorsPausedFiniteAndZeroLengthScores(t *testing.T) {
	audio := NewAudio(nil)
	handle := loadOwnedAudio(t, audio, []smaf.Event{{Time: 1, Type: smaf.EventNoteOn, Note: 60, Velocity: 80}, {Time: 2, Type: smaf.EventEnd}})
	if err := audio.Pause(handle, time.Millisecond/2); err != nil {
		t.Fatal(err)
	}
	saved := captureOwnedAudio(t, audio)
	checkCatchupBudget(t, saved, time.Hour, audioCatchupLimits{})
	saved.Sounds[0].PausedAt, saved.Sounds[0].Position = time.Millisecond, time.Millisecond
	checkCatchupBudget(t, saved, time.Hour, audioCatchupLimits{events: 1})

	saved = catchupAudioState(t, []smaf.Event{{Time: 1, Type: smaf.EventNoteOn, Note: 60, Velocity: 80}, {Time: 2, Type: smaf.EventEnd}})
	saved.Sounds[0].Remaining = 1
	checkCatchupBudget(t, saved, time.Hour, audioCatchupLimits{events: 4, notes: 2})
	saved.Sounds[0].Cursor, saved.Sounds[0].ActiveNotes = 1, []AudioNoteState{{Note: 60}}
	checkCatchupBudget(t, saved, time.Hour, audioCatchupLimits{events: 3, notes: 2})
	saved.Sounds[0].Playing, saved.Sounds[0].ActiveNotes = false, nil
	checkCatchupBudget(t, saved, time.Hour, audioCatchupLimits{})

	saved = catchupAudioState(t, []smaf.Event{{Type: smaf.EventEnd}})
	saved.Sounds[0].Repeat = true
	saved.Sounds[0].ActiveNotes = []AudioNoteState{{Note: 60}, {Note: 61}, {Note: 62}}
	checkCatchupBudget(t, saved, time.Hour, audioCatchupLimits{events: 1, notes: 3})
}

func TestAudioCatchupClockArithmeticDoesNotWrap(t *testing.T) {
	saved := catchupAudioState(t, []smaf.Event{{Time: 1, Type: smaf.EventEnd}})
	saved.Sounds[0].StartedAt, saved.Sounds[0].Repeat = time.Duration(math.MaxInt64)-time.Millisecond, true
	checkCatchupBudget(t, saved, time.Duration(math.MaxInt64)-1, audioCatchupLimits{})
	checkCatchupBudget(t, saved, time.Duration(math.MaxInt64), audioCatchupLimits{events: 1})
	saved = catchupAudioState(t, []smaf.Event{{Time: math.MaxUint32, Type: smaf.EventEnd}})
	saved.Sounds[0].Remaining = 1
	checkCatchupBudget(t, saved, time.Duration(math.MaxInt64), audioCatchupLimits{events: 2})
}

func TestAudioCatchupRejectsMalformedStateWithoutMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*AudioState)
	}{
		{"version", func(s *AudioState) { s.Version++ }},
		{"cursor", func(s *AudioState) { s.Sounds[0].Cursor = -1 }},
		{"length", func(s *AudioState) { s.Sounds[0].Length++ }},
		{"negative origin", func(s *AudioState) { s.Sounds[0].StartedAt = -1 }},
		{"duplicate active key", func(s *AudioState) { s.Sounds[0].ActiveNotes = []AudioNoteState{{Note: 60}, {Note: 60}} }},
		{"active channel", func(s *AudioState) { s.Sounds[0].ActiveNotes = []AudioNoteState{{Channel: 16}} }},
		{"event order", func(s *AudioState) { s.Sounds[0].Events[0].Time = 3 }},
		{"duplicate owner", func(s *AudioState) { s.Sounds = append(s.Sounds, s.Sounds[0]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			saved := catchupAudioState(t, []smaf.Event{{Time: 1, Type: smaf.EventProgramChange}, {Time: 2, Type: smaf.EventEnd}})
			test.mutate(&saved)
			before, _ := EncodeCheckpointRecord(saved)
			if err := ValidateAudioCatchup(saved, 0); err == nil {
				t.Fatal("invalid saved state was accepted")
			}
			after, _ := EncodeCheckpointRecord(saved)
			if !bytes.Equal(before, after) {
				t.Fatal("refusal changed the saved state")
			}
		})
	}
	saved := catchupAudioState(t, []smaf.Event{{Time: 1, Type: smaf.EventEnd}})
	if err := ValidateAudioCatchup(saved, -1); err == nil {
		t.Fatal("negative guest clock was accepted")
	}
	audio := NewAudio(nil)
	handle := loadOwnedAudio(t, audio, []smaf.Event{{Time: 2, Type: smaf.EventEnd}})
	if err := audio.Pause(handle, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAudioCatchup(captureOwnedAudio(t, audio), time.Millisecond-1); err == nil {
		t.Fatal("pause after the guest clock was accepted")
	}
}

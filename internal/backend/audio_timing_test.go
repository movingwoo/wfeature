package backend

import (
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

type audioTimingRecord struct {
	kind  string
	sound AudioHandle
	at    float64
	event smaf.Event
	gain  uint16
	age   time.Duration
}

func TestAudioTimingRetainedOutputMatchesFineAndCoarseGuestJumps(t *testing.T) {
	for _, rate := range []float64{.5, 1, 2} {
		var outputs []AudioOutputState
		for _, coarse := range []bool{false, true} {
			host := time.Unix(1000, 0)
			audio := NewAudioWithClock(nil, func() time.Time { return host })
			if err := audio.SetPlaybackRate(0, rate); err != nil {
				t.Fatal(err)
			}
			samples := make([]int16, 1000)
			for i := range samples {
				samples[i] = int16(i)
			}
			loadOwnedAudio(t, audio, []smaf.Event{
				{Type: smaf.EventNoteOn, Channel: 9, Note: 40, Velocity: 100},
				{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
				{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 1000, Wave: samples},
				{Time: 300, Type: smaf.EventNoteOn, Note: 64, Velocity: 100},
				{Time: 1000, Type: smaf.EventEnd},
			})
			if !coarse {
				audio.Advance(0)
			}
			audio.Advance(300 * time.Millisecond)
			saved := captureOwnedAudio(t, audio)
			outputs = append(outputs, saved.Output)
			age := time.Duration(float64(300*time.Millisecond) / rate)
			for _, note := range saved.Output.Notes {
				if note.Note == 60 && note.Age != age {
					t.Fatalf("rate %g note age = %s, want %s", rate, note.Age, age)
				}
			}
			if len(saved.Output.Waves) != 1 || len(saved.Output.Waves[0].Samples) != 1000-int(age/time.Millisecond) {
				t.Fatalf("rate %g did not trim the already elapsed sample frames", rate)
			}
		}
		if !reflect.DeepEqual(outputs[0], outputs[1]) {
			t.Fatalf("rate %g batch partition changed retained output: %+v / %+v", rate, outputs[0].Notes, outputs[1].Notes)
		}
	}
}

func TestAudioTimingSlowGuestCannotReviveExpiredOutput(t *testing.T) {
	host := time.Unix(1000, 0)
	audio := NewAudioWithClock(nil, func() time.Time { return host })
	loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Channel: 9, Note: 40, Velocity: 100},
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 1000, Wave: make([]int16, 500)},
		{Time: 2000, Type: smaf.EventEnd},
	})
	audio.Advance(0)
	host = host.Add(time.Second)
	before := captureOwnedAudio(t, audio)
	if len(before.Output.Notes) != 1 || len(before.Output.Waves) != 0 {
		t.Fatal("Host elapsed time did not expire drums and PCM")
	}
	audio.Advance(100 * time.Millisecond)
	after := captureOwnedAudio(t, audio)
	if len(after.Output.Notes) != 1 || len(after.Output.Waves) != 0 || after.Output.Notes[0].Age < before.Output.Notes[0].Age {
		t.Fatal("slow guest rewound retained output age")
	}
}

// Record time at the public output operation, not at the preceding clock call.
type audioTimingProbe struct {
	recordingSink
	at      float64
	times   []float64
	records []audioTimingRecord
}

func (sink *audioTimingProbe) AudioTime(seconds float64) {
	sink.at = seconds
	sink.times = append(sink.times, seconds)
}

func (sink *audioTimingProbe) AudioEvent(sound AudioHandle, event smaf.Event) {
	event.Wave, event.SysEx = slices.Clone(event.Wave), slices.Clone(event.SysEx)
	sink.records = append(sink.records, audioTimingRecord{kind: "event", sound: sound, at: sink.at, event: event})
}

func (sink *audioTimingProbe) StopSound(sound AudioHandle) {
	sink.records = append(sink.records, audioTimingRecord{kind: "stop", sound: sound, at: sink.at})
}

func (sink *audioTimingProbe) SoundGain(sound AudioHandle, gain uint16) {
	sink.records = append(sink.records, audioTimingRecord{kind: "gain", sound: sound, at: sink.at, gain: gain})
}

func (sink *audioTimingProbe) ResumeNote(sound AudioHandle, channel, note, velocity uint8, age time.Duration) {
	sink.records = append(sink.records, audioTimingRecord{kind: "resume", sound: sound, at: sink.at,
		event: smaf.Event{Type: smaf.EventNoteOn, Channel: channel, Note: note, Velocity: velocity}, age: age})
}

func (sink *audioTimingProbe) ofKind(kind string) []audioTimingRecord {
	var records []audioTimingRecord
	for _, record := range sink.records {
		if record.kind == kind {
			records = append(records, record)
		}
	}
	return records
}

func requireAudioTiming(t *testing.T, got, want float64) {
	t.Helper()
	if math.IsNaN(got) || math.IsInf(got, 0) || math.Abs(got-want) > 1e-12 {
		t.Fatalf("presentation time = %.12f; want %.12f", got, want)
	}
}

func requireAudioTimingRecords(t *testing.T, got, want []audioTimingRecord) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("output has %d operations; want %d: %+v", len(got), len(want), got)
	}
	for i, record := range got {
		requireAudioTiming(t, record.at, want[i].at)
		record.at = want[i].at
		if !reflect.DeepEqual(record, want[i]) {
			t.Fatalf("operation %d = %+v; want %+v", i, record, want[i])
		}
	}
}

func timingNote(at float64, sound AudioHandle, note, velocity uint8, on bool) audioTimingRecord {
	kind := smaf.EventNoteOff
	if on {
		kind = smaf.EventNoteOn
	}
	return audioTimingRecord{kind: "event", sound: sound, at: at, event: smaf.Event{Type: kind, Note: note, Velocity: velocity}}
}

func TestAudioTimingCoarseAndFineOwnersKeepAbsoluteRepeatDeadlines(t *testing.T) {
	for _, step := range []time.Duration{450 * time.Millisecond, 25 * time.Millisecond} {
		t.Run(step.String(), func(t *testing.T) {
			sink := &audioTimingProbe{}
			audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
			var handles [2]AudioHandle
			for i, events := range [][]smaf.Event{
				{{Time: 50, Type: smaf.EventNoteOn, Note: 60, Velocity: 100}, {Time: 100, Type: smaf.EventNoteOff, Note: 60}, {Time: 200, Type: smaf.EventEnd}},
				{{Time: 25, Type: smaf.EventNoteOn, Note: 65, Velocity: 90}, {Time: 100, Type: smaf.EventNoteOff, Note: 65}, {Time: 150, Type: smaf.EventEnd}},
			} {
				handle, err := audio.LoadEvents(events)
				if err != nil {
					t.Fatal(err)
				}
				if err := audio.PlayCount(handle, time.Second, 2); err != nil {
					t.Fatal(err)
				}
				handles[i] = handle
			}
			for at := time.Second + step; at <= 1450*time.Millisecond; at += step {
				audio.Advance(at)
			}
			var notes []audioTimingRecord
			for _, record := range sink.ofKind("event") {
				if record.event.Type == smaf.EventNoteOn || record.event.Type == smaf.EventNoteOff {
					notes = append(notes, record)
				}
			}
			first, second := handles[0], handles[1]
			requireAudioTimingRecords(t, notes, []audioTimingRecord{
				timingNote(1.025, second, 65, 90, true), timingNote(1.05, first, 60, 100, true),
				timingNote(1.1, first, 60, 0, false), timingNote(1.1, second, 65, 0, false),
				timingNote(1.175, second, 65, 90, true), timingNote(1.25, first, 60, 100, true),
				timingNote(1.25, second, 65, 0, false), timingNote(1.3, first, 60, 0, false),
			})
			requireAudioTiming(t, sink.at, 1.45)
			if len(sink.calls) != 0 {
				t.Fatalf("timed owner output used legacy callbacks: %v", sink.calls)
			}
		})
	}
}

func TestAudioTimingRateChangeDrainsOldRateAndPreservesPayload(t *testing.T) {
	sink := &audioTimingProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
	if err := audio.SetPlaybackRate(0, 2); err != nil {
		t.Fatal(err)
	}
	handle := loadOwnedAudio(t, audio, []smaf.Event{
		{Time: 100, Type: smaf.EventNoteOn, Note: 60, Velocity: 97},
		{Time: 250, Type: smaf.EventPitchBend, Bend: 9216},
		{Time: 400, Type: smaf.EventNoteOff, Note: 60},
		{Time: 600, Type: smaf.EventWave, WaveChannels: 2, SamplingRate: 22050, Wave: []int16{-32768, 32767, -123, 456}},
		{Time: 800, Type: smaf.EventNoteOn, Note: 67, Velocity: 83},
		{Time: 1000, Type: smaf.EventEnd},
	})
	sink.records = nil
	if err := audio.SetPlaybackRate(300*time.Millisecond, 0.5); err != nil {
		t.Fatal(err)
	}
	requireAudioTimingRecords(t, sink.ofKind("event"), []audioTimingRecord{
		timingNote(0.05, handle, 60, 97, true),
		{kind: "event", sound: handle, at: 0.125, event: smaf.Event{Type: smaf.EventPitchBend, Bend: 9216}},
	})
	requireAudioTiming(t, sink.at, 0.15)
	sink.records = nil
	audio.Advance(900 * time.Millisecond)
	requireAudioTimingRecords(t, sink.ofKind("event"), []audioTimingRecord{
		timingNote(0.35, handle, 60, 0, false),
		{kind: "event", sound: handle, at: 0.75, event: smaf.Event{Type: smaf.EventWave, WaveChannels: 2, SamplingRate: 22050, Wave: []int16{-32768, 32767, -123, 456}}},
		timingNote(1.15, handle, 67, 83, true),
	})
	requireAudioTiming(t, sink.at, 1.35)
	before, count := captureOwnedAudio(t, audio), len(sink.records)
	if err := audio.SetPlaybackRate(800*time.Millisecond, 4); err == nil {
		t.Fatal("a rate change accepted an already serviced guest instant")
	}
	if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) || len(sink.records) != count {
		t.Fatal("rejected rate change changed score or output")
	}
	audio.Advance(time.Second)
	requireAudioTiming(t, sink.at, 1.55)
}

func TestAudioTimingPauseResumeAndGainsUseCurrentBoundary(t *testing.T) {
	now := time.Unix(1000, 0)
	sink := &audioTimingProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return now })
	if err := audio.SetPlaybackRate(0, 2); err != nil {
		t.Fatal(err)
	}
	handle := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 1000, Type: smaf.EventNoteOff, Note: 60},
		{Time: 1200, Type: smaf.EventEnd},
	})
	audio.Advance(0)
	sink.records = nil
	now = now.Add(100 * time.Millisecond)
	if err := audio.Pause(handle, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	requireAudioTimingRecords(t, sink.ofKind("stop"), []audioTimingRecord{{kind: "stop", sound: handle, at: 0.1}})
	now = now.Add(150 * time.Millisecond)
	audio.Advance(500 * time.Millisecond)
	audio.SetVolume(80)
	if err := audio.SetSoundVolume(handle, 50); err != nil {
		t.Fatal(err)
	}
	requireAudioTimingRecords(t, sink.ofKind("gain"), []audioTimingRecord{
		{kind: "gain", sound: handle, at: 0.25, gain: 8000},
		{kind: "gain", sound: handle, at: 0.25, gain: 4000},
	})
	now = now.Add(50 * time.Millisecond)
	if err := audio.SetPlaybackRate(600*time.Millisecond, 0.5); err != nil {
		t.Fatal(err)
	}
	now = now.Add(400 * time.Millisecond)
	sink.records = nil
	if err := audio.Resume(handle, 800*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	requireAudioTimingRecords(t, sink.ofKind("resume"), []audioTimingRecord{{kind: "resume", sound: handle, at: 0.7,
		event: smaf.Event{Type: smaf.EventNoteOn, Note: 60, Velocity: 100}, age: 100 * time.Millisecond}})
	for _, record := range sink.records {
		requireAudioTiming(t, record.at, 0.7)
	}
	sink.records = nil
	now = now.Add(1600 * time.Millisecond)
	audio.Advance(1600 * time.Millisecond)
	requireAudioTimingRecords(t, sink.ofKind("event"), []audioTimingRecord{timingNote(2.3, handle, 60, 0, false)})
	audio.Advance(1700 * time.Millisecond)
	audio.Stop(handle)
	requireAudioTimingRecords(t, sink.ofKind("stop"), []audioTimingRecord{{kind: "stop", sound: handle, at: 2.5}})
}

func TestAudioTimingRestoreStartsAtZeroAndKeepsOverdueDeadlines(t *testing.T) {
	now := func() time.Time { return time.Unix(1000, 0) }
	source := NewAudioWithClock(nil, now)
	handle, err := source.LoadEvents([]smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 100, Type: smaf.EventNoteOff, Note: 60},
		{Time: 150, Type: smaf.EventNoteOn, Note: 62, Velocity: 90},
		{Time: 250, Type: smaf.EventNoteOff, Note: 62},
		{Time: 400, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Play(handle, 10*time.Second, false); err != nil {
		t.Fatal(err)
	}
	source.Advance(10 * time.Second)
	saved := captureOwnedAudio(t, source)
	fresh, err := NewAudioFromStateWithClock(saved, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	// Capture preserves unserviced score events. A saved guest clock may be
	// ahead of those events; mapping them before zero preserves their lateness.
	if err := fresh.RebasePlaybackClock(10200*time.Millisecond, 2); err != nil {
		t.Fatal(err)
	}
	if got := captureOwnedAudio(t, fresh); !reflect.DeepEqual(got, saved) {
		t.Fatal("rebasing presentation time changed the portable audio state")
	}
	sink := &audioTimingProbe{at: 123}
	fresh.SetSink(sink)
	requireAudioTiming(t, sink.at, 0)
	fresh.ResumeOutput()
	requireAudioTimingRecords(t, sink.ofKind("resume"), []audioTimingRecord{{kind: "resume", sound: handle,
		event: smaf.Event{Type: smaf.EventNoteOn, Note: 60, Velocity: 100}}})
	for _, record := range sink.records {
		requireAudioTiming(t, record.at, 0)
	}
	sink.records = nil
	fresh.Advance(10250 * time.Millisecond)
	requireAudioTimingRecords(t, sink.ofKind("event"), []audioTimingRecord{
		timingNote(-0.05, handle, 60, 0, false), timingNote(-0.025, handle, 62, 90, true), timingNote(0.025, handle, 62, 0, false),
	})
	requireAudioTiming(t, sink.at, 0.025)
}

func TestAudioTimingNaturalEndUsesItsDeadlineForCancellation(t *testing.T) {
	sink := &audioTimingProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
	if err := audio.PlayTransient([]smaf.Event{
		{Time: 25, Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 100, Type: smaf.EventEnd},
	}, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	sink.records = nil
	audio.Advance(3 * time.Second)
	if len(sink.records) < 3 {
		t.Fatalf("missing natural-end output: %+v", sink.records)
	}
	requireAudioTiming(t, sink.records[0].at, 2.025)
	for _, record := range sink.records[1:] {
		requireAudioTiming(t, record.at, 2.1)
	}
	if stopped := sink.ofKind("stop"); len(stopped) != 1 {
		t.Fatalf("transient natural end emitted %d owner stops", len(stopped))
	}
	requireAudioTiming(t, sink.at, 3)
}

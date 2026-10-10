package backend

import (
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

type audioOrderGate struct {
	sound         AudioHandle
	on            bool
	channel, note uint8
}

func audioOrderGates(events []ownedAudioEvent) []audioOrderGate {
	var gates []audioOrderGate
	for _, record := range events {
		event := record.event
		if event.Type == smaf.EventNoteOn || event.Type == smaf.EventNoteOff {
			gates = append(gates, audioOrderGate{record.sound, event.Type == smaf.EventNoteOn && event.Velocity != 0, event.Channel, event.Note})
		}
	}
	return gates
}

func TestAudioAdvanceMergesOwnersAcrossLoops(t *testing.T) {
	results := make(map[string][]audioOrderGate)
	for _, step := range []struct {
		name string
		at   []int
	}{
		{"coarse", []int{950}},
		{"checkpoint", []int{0, 175, 950}},
		{"deadlines", []int{0, 50, 100, 125, 175, 200, 250, 300, 350, 400, 450, 600, 650, 700, 800, 850, 900, 950}},
	} {
		t.Run(step.name, func(t *testing.T) {
			sink := &ownedAudioProbe{}
			audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
			var handles [3]AudioHandle
			for index, score := range []struct {
				count  int32
				events []smaf.Event
			}{
				{3, []smaf.Event{
					{Time: 50, Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
					{Time: 100, Type: smaf.EventNoteOff, Note: 60},
					{Time: 300, Type: smaf.EventEnd},
				}},
				{-1, []smaf.Event{
					{Type: smaf.EventNoteOn, Note: 65, Velocity: 100},
					{Time: 50, Type: smaf.EventNoteOff, Note: 65},
					{Time: 200, Type: smaf.EventEnd},
				}},
				{1, []smaf.Event{
					{Time: 125, Type: smaf.EventNoteOn, Note: 67, Velocity: 100},
					{Time: 175, Type: smaf.EventNoteOff, Note: 67},
					{Time: 350, Type: smaf.EventNoteOn, Note: 69, Velocity: 100},
					{Time: 400, Type: smaf.EventNoteOff, Note: 69},
					{Time: 950, Type: smaf.EventEnd},
				}},
			} {
				handle, err := audio.LoadEvents(score.events)
				if err != nil {
					t.Fatal(err)
				}
				if err := audio.PlayCount(handle, 0, score.count); err != nil {
					t.Fatal(err)
				}
				handles[index] = handle
			}
			for _, at := range step.at {
				audio.Advance(time.Duration(at) * time.Millisecond)
				if step.name == "checkpoint" && at == 175 {
					var err error
					audio, err = NewAudioFromStateWithClock(captureOwnedAudio(t, audio), sink, func() time.Time { return time.Unix(1000, 0) })
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			first, second, third := handles[0], handles[1], handles[2]
			// Absolute guest deadlines, including repeated passes. Equal times
			// retain handle order; completing one pass must not drain its next.
			want := []audioOrderGate{
				{second, true, 0, 65},                        // 0
				{first, true, 0, 60}, {second, false, 0, 65}, // 50
				{first, false, 0, 60},                      // 100
				{third, true, 0, 67},                       // 125
				{third, false, 0, 67},                      // 175
				{second, true, 0, 65},                      // 200
				{second, false, 0, 65},                     // 250
				{first, true, 0, 60}, {third, true, 0, 69}, // 350
				{first, false, 0, 60}, {second, true, 0, 65}, {third, false, 0, 69}, // 400
				{second, false, 0, 65},                       // 450
				{second, true, 0, 65},                        // 600
				{first, true, 0, 60}, {second, false, 0, 65}, // 650
				{first, false, 0, 60},  // 700
				{second, true, 0, 65},  // 800
				{second, false, 0, 65}, // 850
			}
			got := audioOrderGates(sink.events)
			results[step.name] = got
			if !reflect.DeepEqual(got, want) {
				t.Errorf("global note gates = %+v; want %+v", got, want)
			}
			requireAudioPlayback(t, audio, first, 950*time.Millisecond, AudioPlayback{
				Position: 300 * time.Millisecond, Length: 300 * time.Millisecond, Completed: 3,
			})
			requireAudioPlayback(t, audio, second, 950*time.Millisecond, AudioPlayback{
				Playing: true, Position: 150 * time.Millisecond, Length: 200 * time.Millisecond, Completed: 4,
			})
			requireAudioPlayback(t, audio, third, 950*time.Millisecond, AudioPlayback{
				Position: 950 * time.Millisecond, Length: 950 * time.Millisecond, Completed: 1,
			})
		})
	}
	if !reflect.DeepEqual(results["coarse"], results["deadlines"]) {
		t.Error("batch size changed the cross-owner note sequence")
	}
	if !reflect.DeepEqual(results["coarse"], results["checkpoint"]) {
		t.Error("restoring a checkpoint changed the cross-owner note sequence")
	}
}

func TestAudioAdvanceEqualTimesPreserveOwnerAndEventOrder(t *testing.T) {
	sink := &ownedAudioProbe{}
	audio := NewAudio(sink)
	first := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Type: smaf.EventNoteOn, Note: 62, Velocity: 100},
		{Time: 200, Type: smaf.EventNoteOn, Note: 64, Velocity: 100},
		{Time: 1000, Type: smaf.EventEnd},
	})
	second := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 67, Velocity: 100},
		{Type: smaf.EventNoteOn, Note: 69, Velocity: 100},
		{Time: 200, Type: smaf.EventNoteOn, Note: 71, Velocity: 100},
		{Time: 1000, Type: smaf.EventEnd},
	})
	audio.Advance(950 * time.Millisecond)
	want := []audioOrderGate{
		{first, true, 0, 60}, {first, true, 0, 62},
		{second, true, 0, 67}, {second, true, 0, 69},
		{first, true, 0, 64}, {second, true, 0, 71},
	}
	if got := audioOrderGates(sink.events); !reflect.DeepEqual(got, want) {
		t.Fatalf("equal-time note gates = %+v; want %+v", got, want)
	}
}

func TestAudioAdvanceVoiceBudgetMatchesChronologicalAdmission(t *testing.T) {
	type voiceKey struct {
		sound         AudioHandle
		channel, note uint8
	}
	results := make(map[string][]voiceKey)
	for _, mode := range []string{"coarse", "deadlines"} {
		t.Run(mode, func(t *testing.T) {
			audio := NewAudioWithClock(nil, func() time.Time { return time.Unix(1000, 0) })
			var firstEvents, secondEvents []smaf.Event
			for i := uint32(0); i < 16; i++ {
				firstEvents = append(firstEvents, smaf.Event{Time: 20 + 40*i, Type: smaf.EventNoteOn, Note: 40 + uint8(i), Velocity: 100})
				secondEvents = append(secondEvents, smaf.Event{Time: 40 * i, Type: smaf.EventNoteOn, Note: 64 + uint8(i), Velocity: 100})
			}
			first := loadOwnedAudio(t, audio, append(firstEvents, smaf.Event{Time: 2000, Type: smaf.EventEnd}))
			second := loadOwnedAudio(t, audio, append(secondEvents, smaf.Event{Time: 2000, Type: smaf.EventEnd}))
			if mode == "deadlines" {
				for at := time.Duration(0); at <= 620*time.Millisecond; at += 20 * time.Millisecond {
					audio.Advance(at)
				}
			}
			audio.Advance(950 * time.Millisecond)
			// The first eight of 32 interleaved voices have been stolen. A
			// per-owner drain incorrectly retains 16 keys from its last owner.
			want := []voiceKey{
				{second, 0, 68}, {first, 0, 44}, {second, 0, 69}, {first, 0, 45},
				{second, 0, 70}, {first, 0, 46}, {second, 0, 71}, {first, 0, 47},
				{second, 0, 72}, {first, 0, 48}, {second, 0, 73}, {first, 0, 49},
				{second, 0, 74}, {first, 0, 50}, {second, 0, 75}, {first, 0, 51},
				{second, 0, 76}, {first, 0, 52}, {second, 0, 77}, {first, 0, 53},
				{second, 0, 78}, {first, 0, 54}, {second, 0, 79}, {first, 0, 55},
			}
			var got []voiceKey
			for _, voice := range captureOwnedAudio(t, audio).Output.Notes {
				got = append(got, voiceKey{voice.Sound, voice.Channel, voice.Note})
			}
			results[mode] = got
			if !reflect.DeepEqual(got, want) {
				t.Errorf("retained voices = %+v; want %+v", got, want)
			}
		})
	}
	if !reflect.DeepEqual(results["coarse"], results["deadlines"]) {
		t.Error("batch size changed which 24 voices survived")
	}
}

func TestAudioPlaybackAdvancesAllOwnersInChronologicalOrder(t *testing.T) {
	sink := &ownedAudioProbe{}
	audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
	late := loadOwnedAudio(t, audio, []smaf.Event{
		{Time: 700, Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 800, Type: smaf.EventNoteOff, Note: 60},
		{Time: 900, Type: smaf.EventEnd},
	})
	peer := loadOwnedAudio(t, audio, []smaf.Event{
		{Time: 100, Type: smaf.EventNoteOn, Note: 65, Velocity: 100},
		{Time: 200, Type: smaf.EventNoteOff, Note: 65},
		{Time: 750, Type: smaf.EventNoteOn, Note: 67, Velocity: 100},
		{Time: 825, Type: smaf.EventNoteOff, Note: 67},
		{Time: 850, Type: smaf.EventEnd},
	})
	requireAudioPlayback(t, audio, late, 950*time.Millisecond, AudioPlayback{
		Position: 900 * time.Millisecond, Length: 900 * time.Millisecond, Completed: 1,
	})
	want := []audioOrderGate{
		{peer, true, 0, 65}, {peer, false, 0, 65},
		{late, true, 0, 60}, {peer, true, 0, 67},
		{late, false, 0, 60}, {peer, false, 0, 67},
	}
	if got := audioOrderGates(sink.events); !reflect.DeepEqual(got, want) {
		t.Errorf("progress query emitted gates = %+v; want %+v", got, want)
	}
	// Inspect the unqueried owner without advancing it through another query.
	for _, current := range captureOwnedAudio(t, audio).Sounds {
		if current.Handle == peer && (current.Playing || current.Completed != 1 || current.Position != 850*time.Millisecond) {
			t.Errorf("progress query left peer behind: playing=%v completed=%d position=%v", current.Playing, current.Completed, current.Position)
		}
	}
	emitted := len(sink.events)
	audio.Advance(950 * time.Millisecond)
	if len(sink.events) != emitted {
		t.Fatal("ordinary advance repeated events already serviced by the progress query")
	}
}

func TestAudioResumeAdmitsRetainedVoiceAfterDuePeerNotes(t *testing.T) {
	audio := NewAudioWithClock(&resumeAudioProbe{}, func() time.Time { return time.Unix(1000, 0) })
	owner := loadOwnedAudio(t, audio, []smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 1000, Type: smaf.EventEnd},
	})
	if err := audio.Pause(owner, 0); err != nil {
		t.Fatal(err)
	}
	var notes []smaf.Event
	for i := uint32(0); i < 24; i++ {
		notes = append(notes, smaf.Event{Time: i + 1, Type: smaf.EventNoteOn, Note: uint8(64 + i), Velocity: 100})
	}
	peer := loadOwnedAudio(t, audio, append(notes, smaf.Event{Time: 1000, Type: smaf.EventEnd}))
	if err := audio.Resume(owner, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	// All peer attacks predate this resume. The oldest peer voice is stolen;
	// the retained note must remain newest even without an intervening tick.
	audio.Advance(100 * time.Millisecond)
	voices := captureOwnedAudio(t, audio).Output.Notes
	if len(voices) != 24 || voices[23].Sound != owner || voices[23].Note != 60 {
		t.Fatalf("resume was admitted before older due peer notes: %+v", voices)
	}
	for i, voice := range voices[:23] {
		if voice.Sound != peer || voice.Note != uint8(65+i) {
			t.Fatalf("voice %d = %+v; want retained peer note %d", i, voice, 65+i)
		}
	}
}

func TestAudioPlaybackStateObservesWithoutAdvancing(t *testing.T) {
	audio, sink, handle := newCountedAudio(t, 2)
	before := captureOwnedAudio(t, audio)
	progress, err := audio.PlaybackState(handle)
	want := AudioPlayback{Playing: true, Length: 400 * time.Millisecond}
	if err != nil || progress != want || len(sink.events) != 0 {
		t.Fatalf("unserviced progress = %+v, %v; want %+v without output", progress, err, want)
	}
	if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) {
		t.Fatal("observing an unserviced score advanced its state")
	}
	audio.Advance(950 * time.Millisecond)
	before = captureOwnedAudio(t, audio)
	emitted := len(sink.events)
	progress, err = audio.PlaybackState(handle)
	want = AudioPlayback{Position: 400 * time.Millisecond, Length: 400 * time.Millisecond, Completed: 2}
	if err != nil || progress != want || len(sink.events) != emitted {
		t.Fatalf("serviced progress = %+v, %v; want %+v without repeated output", progress, err, want)
	}
	if after := captureOwnedAudio(t, audio); !reflect.DeepEqual(before, after) {
		t.Fatal("observing serviced progress changed its state")
	}
	if _, err := audio.PlaybackState(handle + 1); err == nil {
		t.Fatal("unknown handle has playback state")
	}
	if _, err := (*Audio)(nil).PlaybackState(handle); err == nil {
		t.Fatal("unconfigured audio has playback state")
	}
}

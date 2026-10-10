package backend

import (
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

// Retain only sink metadata, so byte-budget tests do not duplicate PCM payloads.
type audioAdmissionRecord struct {
	sound    AudioHandle
	kind     smaf.EventType
	at       float64
	channels uint8
	group    uint16
	samples  int
	first    int16
	control  uint8
	value    uint8
	note     uint8
}

type audioAdmissionProbe struct {
	recordingSink
	capable bool
	at      float64
	events  []audioAdmissionRecord
}

func (sink *audioAdmissionProbe) PCMChannels() bool        { return sink.capable }
func (sink *audioAdmissionProbe) AudioTime(at float64)     { sink.at = at }
func (*audioAdmissionProbe) SoundGain(AudioHandle, uint16) {}
func (*audioAdmissionProbe) StopSound(AudioHandle)         {}
func (sink *audioAdmissionProbe) AudioEvent(sound AudioHandle, event smaf.Event) {
	record := audioAdmissionRecord{sound: sound, kind: event.Type, at: sink.at, channels: event.WaveChannels,
		group: event.PCMChannel, samples: len(event.Wave), control: event.Control, value: event.Value, note: event.Note}
	if len(event.Wave) != 0 {
		record.first = event.Wave[0]
	}
	sink.events = append(sink.events, record)
}

func (sink *audioAdmissionProbe) waves() []audioAdmissionRecord {
	var waves []audioAdmissionRecord
	for _, event := range sink.events {
		if event.kind == smaf.EventWave {
			waves = append(waves, event)
		}
	}
	return waves
}

func admissionWave(at uint32, marker int16) smaf.Event {
	return smaf.Event{Time: at, Type: smaf.EventWave, WaveChannels: 1, SamplingRate: 2, Wave: []int16{marker, marker}}
}

func admissionWaves(count int, event smaf.Event) []smaf.Event {
	events := make([]smaf.Event, count)
	for index := range events {
		events[index] = event
	}
	return events
}

func TestAudioPCMAdmissionRefusesNewestBeforeEmission(t *testing.T) {
	sink := &audioAdmissionProbe{capable: true}
	audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
	events := admissionWaves(256, admissionWave(0, 1000))
	events = append(events, admissionWave(1, 30000), smaf.Event{Time: 100, Type: smaf.EventEnd})
	loadOwnedAudio(t, audio, events)
	audio.Advance(0)
	if got := len(sink.waves()); got != 256 {
		t.Fatalf("exact wave limit emitted %d waves; want 256", got)
	}
	audio.Advance(time.Millisecond)
	if got := len(sink.waves()); got != 256 {
		t.Errorf("newest over-limit wave reached the sink: got %d; want 256", got)
	}
	if saved, err := audio.CaptureState(); err != nil || len(saved.Output.Waves) != 256 {
		t.Errorf("refused output must remain representable: retained=%d err=%v", len(saved.Output.Waves), err)
	}
}

func TestAudioPCMAdmissionPreservesControlsPeersAndReplay(t *testing.T) {
	for _, capable := range []bool{true, false} {
		name := "legacy fallback"
		if capable {
			name = "live PCM"
		}
		t.Run(name, func(t *testing.T) {
			sink := &audioAdmissionProbe{capable: capable}
			clock := func() time.Time { return time.Unix(1000, 0) }
			audio := NewAudioWithClock(sink, clock)
			initial := admissionWave(0, 1000)
			initial.PCMChannel = 1
			events := []smaf.Event{{Type: smaf.EventPCMControl, PCMChannel: 1, Control: 10, Value: 0}}
			events = append(events, admissionWaves(256, initial)...)
			rejected := admissionWave(1, 30000)
			rejected.PCMChannel = 1
			events = append(events, rejected,
				smaf.Event{Time: 2, Type: smaf.EventPCMControl, PCMChannel: 1, Control: 7, Value: 64},
				smaf.Event{Time: 3, Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
				smaf.Event{Time: 100, Type: smaf.EventEnd})
			owner := loadOwnedAudio(t, audio, events)
			peer := loadOwnedAudio(t, audio, []smaf.Event{{Type: smaf.EventNoteOn, Note: 67, Velocity: 90}, {Time: 2000, Type: smaf.EventEnd}})
			audio.Advance(3 * time.Millisecond)
			waves := sink.waves()
			if len(waves) != 256 {
				t.Fatalf("over-limit emission: %d waves", len(waves))
			}
			channels, group, samples := uint8(2), uint16(0), 4
			if capable {
				channels, group, samples = 1, 1, 2
			}
			for _, wave := range waves {
				if wave.sound != owner || wave.channels != channels || wave.group != group || wave.samples != samples || wave.first != 1000 {
					t.Fatalf("admission changed accepted PCM or fallback: %+v", wave)
				}
			}
			saved := captureOwnedAudio(t, audio)
			if len(saved.Output.Notes) != 2 || len(saved.Output.Waves) != 256 || len(saved.Output.PCMChannels) != 1 || saved.Output.PCMChannels[0].Volume != 64 {
				t.Fatal("refusal changed an existing peer, MIDI note or later PCM control")
			}
			for _, wave := range saved.Output.Waves {
				if wave.Samples[0] != 1000 {
					t.Fatal("refused payload entered portable output")
				}
			}
			encoded, err := EncodeCheckpointRecord(saved)
			if err != nil {
				t.Fatal(err)
			}
			var decoded AudioState
			if err := DecodeCheckpointRecord(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			reconnected := &audioAdmissionProbe{capable: true}
			audio.SetSink(reconnected)
			audio.ResumeOutput()
			freshSink := &audioAdmissionProbe{capable: true}
			fresh, err := NewAudioFromStateWithClock(decoded, freshSink, clock)
			if err != nil {
				t.Fatal(err)
			}
			fresh.ActivateOutputClock()
			if err := fresh.RebasePlaybackClock(3*time.Millisecond, 1); err != nil {
				t.Fatal(err)
			}
			fresh.ResumeOutput()
			fresh.Advance(4 * time.Millisecond)
			for _, replay := range []*audioAdmissionProbe{reconnected, freshSink} {
				if got := replay.waves(); len(got) != 256 {
					t.Fatalf("reconnect/restore replayed %d waves; want 256", len(got))
				} else {
					for _, wave := range got {
						if wave.first != 1000 || wave.group != 1 || wave.sound != owner {
							t.Fatal("rejected PCM resurfaced or raw data changed on replay")
						}
					}
				}
			}
			audio.Advance(100 * time.Millisecond)
			state, err := audio.PlaybackState(owner)
			if err != nil || state.Playing || state.Completed != 1 || !audio.Playing(peer) {
				t.Fatalf("a refused event changed score completion or its peer: %+v, %v", state, err)
			}
		})
	}
}

func TestAudioPCMAdmissionPauseRetainsCapacityAndResumeDoesNotReadmit(t *testing.T) {
	sink := &audioAdmissionProbe{capable: true}
	audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
	owner := loadOwnedAudio(t, audio, append(admissionWaves(256, admissionWave(0, 1000)), smaf.Event{Time: 3000, Type: smaf.EventEnd}))
	loadOwnedAudio(t, audio, []smaf.Event{admissionWave(500, 30000), admissionWave(1000, 29000), admissionWave(1500, 20000), {Time: 3000, Type: smaf.EventEnd}})
	audio.Advance(0)
	if err := audio.Pause(owner, 0); err != nil {
		t.Fatal(err)
	}
	sink.events = nil
	audio.Advance(500 * time.Millisecond)
	if got := sink.waves(); len(got) != 0 {
		t.Fatalf("paused PCM lost its reserved capacity: %+v", got)
	}
	if saved := captureOwnedAudio(t, audio); len(saved.Output.Waves) != 256 {
		t.Fatal("pause discarded admitted wave records")
	}
	if err := audio.Resume(owner, 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := sink.waves(); len(got) != 256 {
		t.Fatalf("resume re-admitted or lost existing PCM: %d", len(got))
	}
	sink.events = nil
	audio.Advance(time.Second)
	if len(sink.waves()) != 0 {
		t.Fatal("resume failed to extend existing output lifetimes")
	}
	audio.Advance(1500 * time.Millisecond)
	if got := sink.waves(); len(got) != 1 || got[0].first != 20000 {
		t.Fatalf("expired resumed PCM did not release capacity: %+v", got)
	}
}

func TestAudioPCMAdmissionStopCloseAndExpiryReleaseOnlyTheirOwner(t *testing.T) {
	for _, action := range []string{"stop", "close", "expiry"} {
		t.Run(action, func(t *testing.T) {
			sink := &audioAdmissionProbe{capable: true}
			audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
			owner := loadOwnedAudio(t, audio, []smaf.Event{admissionWave(0, 1000), {Time: 3000, Type: smaf.EventEnd}})
			long := admissionWave(0, 2000)
			long.SamplingRate = 1
			peer := loadOwnedAudio(t, audio, append(admissionWaves(255, long), smaf.Event{Time: 3000, Type: smaf.EventEnd}))
			at := uint32(200)
			if action == "expiry" {
				at = 1000
			}
			candidate := loadOwnedAudio(t, audio, []smaf.Event{admissionWave(100, 30000), admissionWave(at, 20000), {Time: 3000, Type: smaf.EventEnd}})
			audio.Advance(100 * time.Millisecond)
			if len(sink.waves()) != 256 {
				t.Fatal("the first excess wave reached the sink")
			}
			switch action {
			case "stop":
				audio.Stop(owner)
			case "close":
				if err := audio.Close(owner); err != nil {
					t.Fatal(err)
				}
			}
			sink.events = nil
			audio.Advance(time.Duration(at) * time.Millisecond)
			if got := sink.waves(); len(got) != 1 || got[0].sound != candidate || got[0].first != 20000 {
				t.Fatalf("capacity was not reused by the next authored event: %+v", got)
			}
			saved := captureOwnedAudio(t, audio)
			if len(saved.Output.Waves) != 256 || !audio.Playing(peer) {
				t.Fatal("releasing one owner changed its peer")
			}
			for _, wave := range saved.Output.Waves {
				if wave.Sound != peer && wave.Sound != candidate || wave.Samples[0] == 30000 {
					t.Fatal("ended or refused PCM retained output")
				}
			}
		})
	}
}

func TestAudioPCMAdmissionFineAndCoarseDeadlinesSelectTheSameWaves(t *testing.T) {
	var results [][]audioAdmissionRecord
	for _, times := range [][]time.Duration{{0, 500 * time.Millisecond, time.Second}, {time.Second}} {
		sink := &audioAdmissionProbe{capable: true}
		audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
		long := admissionWave(0, 1000)
		long.SamplingRate = 1
		loadOwnedAudio(t, audio, append(admissionWaves(255, long), smaf.Event{Time: 2000, Type: smaf.EventEnd}))
		loadOwnedAudio(t, audio, []smaf.Event{admissionWave(0, 2000), {Time: 2000, Type: smaf.EventEnd}})
		accepted := loadOwnedAudio(t, audio, []smaf.Event{admissionWave(500, 30000), admissionWave(1000, 20000), {Time: 2000, Type: smaf.EventEnd}})
		loadOwnedAudio(t, audio, []smaf.Event{admissionWave(1000, 15000), {Time: 2000, Type: smaf.EventEnd}})
		for _, at := range times {
			audio.Advance(at)
		}
		waves := sink.waves()
		if len(waves) != 257 {
			t.Fatalf("authored deadlines changed admission: %d waves; want 257", len(waves))
		}
		if last := waves[len(waves)-1]; last.sound != accepted || last.first != 20000 || last.at != 1 {
			t.Fatalf("equal-deadline owner ordering changed admission: last %+v", last)
		}
		results = append(results, waves)
	}
	if !reflect.DeepEqual(results[0], results[1]) {
		t.Fatal("batch size changed admitted PCM")
	}
}

func TestAudioPCMAdmissionByteChargeSurvivesTrimmedCheckpoint(t *testing.T) {
	const originalBytes = 128 << 10
	samples := make([]int16, originalBytes/2)
	for index := range samples {
		samples[index] = 1000
	}
	long := smaf.Event{Type: smaf.EventWave, PCMChannel: 1, WaveChannels: 1, SamplingRate: 32768, Wave: samples}
	// The old consumer receives twice the raw bytes as onset-rendered stereo.
	// Admission still charges original mono bytes, including after capability changes.
	sink := &audioAdmissionProbe{capable: false}
	audio := NewAudioWithClock(sink, func() time.Time { return time.Unix(1000, 0) })
	pan := smaf.Event{Type: smaf.EventPCMControl, PCMChannel: 1, Control: 10, Value: 0}
	longEvents := append([]smaf.Event{pan}, admissionWaves(255, long)...)
	owner := loadOwnedAudio(t, audio, append(longEvents, smaf.Event{Time: 3000, Type: smaf.EventEnd}))
	short := long
	short.SamplingRate = 131072 // The 256th record releases its slot at 0.5 s.
	loadOwnedAudio(t, audio, []smaf.Event{pan, short, {Time: 3000, Type: smaf.EventEnd}})
	excess := make([]int16, originalBytes)
	for index := range excess {
		excess[index] = 30000
	}
	later := make([]int16, len(excess))
	for index := range later {
		later[index] = 20000
	}
	candidate := loadOwnedAudio(t, audio, []smaf.Event{
		pan,
		{Time: 1500, Type: smaf.EventWave, PCMChannel: 1, WaveChannels: 1, SamplingRate: 65536, Wave: excess},
		{Time: 2500, Type: smaf.EventWave, PCMChannel: 1, WaveChannels: 1, SamplingRate: 65536, Wave: later},
		{Time: 3000, Type: smaf.EventEnd},
	})
	audio.Advance(0)
	if got := sink.waves(); len(got) != 256 {
		t.Fatalf("exact 32 MiB raw admission emitted %d waves; want 256", len(got))
	} else {
		for _, wave := range got {
			if wave.channels != 2 || wave.samples*2 != 2*originalBytes {
				t.Fatal("legacy output did not exercise expanded stereo payloads")
			}
		}
	}
	// Align to complete sample frames so only budget persistence is under test.
	audio.Advance(time.Second)
	if err := audio.Pause(owner, time.Second); err != nil {
		t.Fatal(err)
	}
	saved := captureOwnedAudio(t, audio)
	if saved.Version != audioStateVersion || len(saved.Output.Waves) != 255 {
		t.Fatalf("half-duration capture has version %d and %d waves", saved.Version, len(saved.Output.Waves))
	}
	for _, wave := range saved.Output.Waves {
		if len(wave.Samples)*2 != originalBytes/2 || wave.BudgetBytes != originalBytes {
			t.Fatalf("trimmed PCM lost original admission charge: tail=%d charge=%d", len(wave.Samples)*2, wave.BudgetBytes)
		}
	}
	// A free count slot remains, but a 256 KiB wave exceeds the 128 KiB raw
	// allowance. Recomputing charges from saved tails would incorrectly admit it.
	freshSink := &audioAdmissionProbe{capable: true}
	fresh, err := NewAudioFromStateWithClock(saved, freshSink, func() time.Time { return time.Unix(2000, 0) })
	if err != nil {
		t.Fatal(err)
	}
	fresh.ActivateOutputClock()
	if err := fresh.RebasePlaybackClock(time.Second, 1); err != nil {
		t.Fatal(err)
	}
	for _, run := range []struct {
		name  string
		audio *Audio
		sink  *audioAdmissionProbe
	}{{"live", audio, sink}, {"restored", fresh, freshSink}} {
		run.sink.events = nil
		run.audio.Advance(1500 * time.Millisecond)
		if got := run.sink.waves(); len(got) != 0 {
			t.Fatalf("%s admitted an excess wave after trimming: %+v", run.name, got)
		}
		run.audio.ResumeOutput()
		if len(run.sink.waves()) != 0 {
			t.Fatalf("%s reconstructed paused or rejected PCM", run.name)
		}
		if err := run.audio.Resume(owner, 1500*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if got := run.sink.waves(); len(got) != 255 {
			t.Fatalf("%s reconstructed %d waves; want 255", run.name, len(got))
		} else {
			for _, wave := range got {
				if wave.first != 1000 {
					t.Fatalf("%s reconstructed rejected PCM", run.name)
				}
			}
		}
		run.sink.events = nil
		run.audio.Advance(2500 * time.Millisecond)
		if got := run.sink.waves(); len(got) != 1 || got[0].sound != candidate || got[0].first != 20000 {
			t.Fatalf("%s failed to release original charges at expiry: %+v", run.name, got)
		}
		if saved := captureOwnedAudio(t, run.audio); len(saved.Output.Waves) != 1 || saved.Output.Waves[0].BudgetBytes != 2*originalBytes {
			t.Fatalf("%s retained the wrong post-expiry budget", run.name)
		}
		// Reuse the already loaded payloads to refill almost the whole byte
		// budget. Subtracting only trimmed tail bytes would leave a hidden charge.
		run.audio.Stop(candidate)
		run.sink.events = nil
		if err := run.audio.Play(owner, 2500*time.Millisecond, false); err != nil {
			t.Fatal(err)
		}
		run.audio.Advance(2500 * time.Millisecond)
		if got := len(run.sink.waves()); got != 255 {
			t.Fatalf("%s retained phantom byte charges after expiry and stop: %d waves", run.name, got)
		}
	}
}

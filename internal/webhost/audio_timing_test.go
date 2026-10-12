package webhost

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/session"
)

func presentation(seconds float64) *float64 { return &seconds }

func TestTimedCollectorPreservesDeadlinesAndSilentFrontier(t *testing.T) {
	collector := &audioCollector{}
	audio := backend.NewAudio(collector)
	handle, err := audio.LoadEvents([]smaf.Event{
		{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
		{Time: 5, Type: smaf.EventNoteOff, Note: 60},
		{Time: 35, Type: smaf.EventNoteOn, Note: 64, Velocity: 100},
		{Time: 70, Type: smaf.EventNoteOff, Note: 64},
		{Time: 1000, Type: smaf.EventEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	collector.take()
	audio.Advance(100 * time.Millisecond)
	batch := collector.take()
	var got []float64
	for _, event := range batch {
		if event.At == nil {
			t.Fatalf("missing time: %+v", event)
		}
		got = append(got, *event.At)
	}
	if !reflect.DeepEqual(got, []float64{0, .005, .035, .07, .1}) || batch[len(batch)-1].Kind != audioClock {
		t.Fatalf("coarse tick lost authored spacing: %+v", got)
	}
	audio.Advance(150 * time.Millisecond)
	if silent := collector.take(); len(silent) != 1 || silent[0].Kind != audioClock || *silent[0].At != .15 {
		t.Fatalf("silent tick lost frontier: %+v", silent)
	}
	// A later selector cannot mutate the time stored in an already taken batch.
	if *batch[0].At != 0 || *batch[len(batch)-1].At != .1 {
		t.Fatal("timestamps aliased the current clock")
	}
}

func TestTimedAudioBinarySelectionAndClock(t *testing.T) {
	events := []audioEvent{
		{Kind: audioNoteOn, Note: 60, Velocity: 100, At: presentation(0)},
		{Kind: audioNoteOff, Note: 60, At: presentation(.005)},
		{Kind: audioProgramChange, Program: 42, At: presentation(.005)},
		{Kind: audioNoteOn, Note: 64, Velocity: 90},
		{Kind: audioNoteOff, Note: 64, At: presentation(-.01)},
		{Kind: audioAllOff},
		{Kind: audioNoteResume, Note: 67, Velocity: 80, Age: 65, At: presentation(0)},
		{Kind: audioClock, At: presentation(.1)},
	}
	want := []byte("WFA2")
	timed := func(seconds float64) {
		want = append(want, 0x0b, 1)
		want = binary.BigEndian.AppendUint64(want, math.Float64bits(seconds))
	}
	timed(0)
	want = append(want, 1, 0, 60, 100)
	timed(.005)
	want = append(want, 2, 0, 60, 0, 3, 0, 42, 0x0b, 0, 1, 0, 64, 90)
	timed(-.01)
	want = append(want, 2, 0, 64, 0, 6)
	timed(0)
	want = append(want, 0x0a, 0, 67, 80, 0, 0, 0, 65, 0x0c)
	want = binary.BigEndian.AppendUint64(want, math.Float64bits(.1))
	var definitions audioDefinitions
	if got := definitions.encode(events); !bytes.Equal(got, want) {
		t.Fatalf("timed wire = %x, want %x", got, want)
	}
	if got := definitions.encode(events[:1]); !bytes.Equal(got, want[:18]) {
		t.Fatalf("selection leaked across messages: %x", got)
	}
}

func TestTimedAudioCapabilityKeepsLegacyWireUnchanged(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		for _, timed := range []bool{false, true} {
			runner := stalledRunner(t, 1)
			runner.protocol, runner.soundOwnership, runner.soundResume, runner.soundTiming = protocol, true, true, timed
			events := []audioEvent{{Kind: audioNoteOn, Sound: 7, Note: 60, Velocity: 100, At: presentation(0)}, {Kind: audioClock, At: presentation(.05)}}
			if !runner.sendAudio(slices.Clone(events), false) {
				t.Fatal("batch was not queued")
			}
			message := <-runner.outText
			want := events
			if !timed {
				want = slices.Clone(events[:1])
				want[0].At = nil
			}
			if protocol == protocolStream {
				var definitions audioDefinitions
				if !bytes.Equal(message.binary, definitions.encode(want)) {
					t.Fatalf("binary capability %v changed wire", timed)
				}
			} else {
				var decoded serverMessage
				if err := json.Unmarshal([]byte(message.text), &decoded); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(decoded.Audio, want) {
					t.Fatalf("JSON capability %v: %+v", timed, decoded.Audio)
				}
			}
		}
	}
}

func TestTimedAudioRecoveryIsNegotiatedAndEpochBound(t *testing.T) {
	runner := stalledRunner(t, 1)
	runner.game = &session.Session{}
	runner.outputEpoch.Store(9)
	runner.soundTiming = true
	runner.handle(context.Background(), clientMessage{Kind: clientAudioResume, Epoch: 8})
	if runner.audioNeedsReset {
		t.Fatal("stale timeline requested replay")
	}
	runner.handle(context.Background(), clientMessage{Kind: clientAudioResume, Epoch: 9})
	if !runner.audioNeedsReset || !runner.audioDefinitions.forget {
		t.Fatal("current replay did not invalidate output and definitions")
	}
	runner.audioNeedsReset, runner.soundTiming = false, false
	runner.handle(context.Background(), clientMessage{Kind: clientAudioResume, Epoch: 9})
	if runner.audioNeedsReset {
		t.Fatal("unnegotiated page requested timed replay")
	}
}

// A silent tick tells the page the clock moved at most every
// clockOnlyInterval: the first frontier goes at once, one inside the interval
// waits for a later one, a sound batch is never held back, and a frontier
// after the interval goes at once again.
func TestSilentFrontiersAreSentAtMostEveryInterval(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		runner := stalledRunner(t, 8)
		runner.protocol, runner.soundOwnership, runner.soundResume, runner.soundTiming = protocol, true, true, true
		runner.audio = &audioCollector{}
		frontier := func(at float64) {
			t.Helper()
			if len(runner.outText) != 1 {
				t.Fatalf("protocol %d: %d messages, want the frontier at %v", protocol, len(runner.outText), at)
			}
			message := <-runner.outText
			if protocol == protocolStream {
				want := binary.BigEndian.AppendUint64(append(bytes.Clone(audioMagic), audioOpClock), math.Float64bits(at))
				if !bytes.Equal(message.binary, want) {
					t.Fatalf("frontier wire = %x, want %x", message.binary, want)
				}
				return
			}
			var decoded serverMessage
			if err := json.Unmarshal([]byte(message.text), &decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded.Audio) != 1 || decoded.Audio[0].Kind != audioClock || *decoded.Audio[0].At != at {
				t.Fatalf("frontier = %+v, want the clock at %v", decoded.Audio, at)
			}
		}
		silentTick := func(at float64) {
			runner.audio.AudioTime(at)
			runner.flushAudio()
		}
		silentTick(0)
		frontier(0)
		silentTick(.016)
		silentTick(.033)
		if len(runner.outText) != 0 {
			t.Fatalf("protocol %d: a frontier inside the interval was sent", protocol)
		}
		runner.audio.AudioTime(.05)
		runner.audio.MIDINoteOn(0, 60, 100)
		runner.flushAudio()
		if len(runner.outText) != 1 {
			t.Fatalf("protocol %d: a sound batch inside the interval was held back", protocol)
		}
		<-runner.outText
		runner.audioSentAt = time.Now().Add(-clockOnlyInterval)
		silentTick(.2)
		frontier(.2)
	}
}

// A full queue skips a frontier instead of shedding it. Shed, it would ask
// for a reconstruction, cutting every sounding voice for a message that lost
// nothing; the next frontier is a later one anyway.
func TestAFullQueueSkipsAFrontierWithoutAskingForTheOutput(t *testing.T) {
	runner := stalledRunner(t, 1)
	runner.protocol, runner.soundOwnership, runner.soundResume, runner.soundTiming = protocolStream, true, true, true
	runner.audio = &audioCollector{}
	runner.outText <- outboundMessage{text: "held"}
	runner.audio.AudioTime(.5)
	runner.flushAudio()
	if runner.audioNeedsReset || runner.shed.Load() != 0 {
		t.Fatal("a frontier that found the queue full asked for the output again")
	}
	// A batch with events still counts as lost when it cannot be queued.
	runner.audio.MIDINoteOn(0, 60, 100)
	runner.flushAudio()
	if !runner.audioNeedsReset || runner.shed.Load() != 1 {
		t.Fatal("a shed sound batch did not ask for the output again")
	}
}

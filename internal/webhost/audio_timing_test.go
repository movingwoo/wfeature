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

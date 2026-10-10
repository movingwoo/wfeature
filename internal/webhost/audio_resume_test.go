package webhost

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

func TestAudioResumeAgesAndPausedIsolationAcrossBothProtocols(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		for _, capability := range []string{"legacy", "owned", "resume"} {
			t.Run(fmt.Sprintf("%d/%s", protocol, capability), func(t *testing.T) {
				runner := stalledRunner(t, 1)
				runner.protocol, runner.soundOwnership, runner.soundResume = protocol, capability != "legacy", capability == "resume"
				runner.audio = &audioCollector{}
				now := time.Unix(1000, 0)
				audio := backend.NewAudioWithClock(runner.audio, func() time.Time { return now })
				load := func(note uint8) backend.AudioHandle {
					handle, err := audio.LoadEvents([]smaf.Event{{Type: smaf.EventNoteOn, Note: note, Velocity: 100}, {Time: 60000, Type: smaf.EventEnd}})
					if err != nil {
						t.Fatal(err)
					}
					if err := audio.Play(handle, 0, false); err != nil {
						t.Fatal(err)
					}
					return handle
				}
				first, second := load(60), load(64)
				audio.Advance(0)
				runner.flushAudio()
				drainReplaySignals(t, runner)
				now = now.Add(65 * time.Millisecond)
				if err := audio.Pause(first, 65*time.Millisecond); err != nil {
					t.Fatal(err)
				}
				runner.flushAudio()
				drainReplaySignals(t, runner)
				now = now.Add(10 * time.Second)
				// Reconnection reconstructs only the still-audible peer.
				audio.ResumeOutput()
				runner.flushAudio()
				wantNote := func(handle backend.AudioHandle, note uint8, age uint32) audioEvent {
					kind, sound := audioNoteResume, uint32(handle)
					if !runner.soundResume {
						kind, age = audioNoteOn, 0
					}
					if !runner.soundOwnership {
						sound = 0
					}
					return audioEvent{Kind: kind, Sound: sound, Note: note, Velocity: 100, Age: age}
				}
				notes := func() []audioEvent {
					var result []audioEvent
					for _, event := range drainReplaySignals(t, runner) {
						if event.Kind == audioNoteOn || event.Kind == audioNoteResume {
							result = append(result, event)
						}
					}
					return result
				}
				if got := notes(); !reflect.DeepEqual(got, []audioEvent{wantNote(second, 64, 10065)}) {
					t.Fatalf("reconnect replay = %+v", got)
				}
				if err := audio.Resume(first, 10065*time.Millisecond); err != nil {
					t.Fatal(err)
				}
				runner.flushAudio()
				if got := notes(); !reflect.DeepEqual(got, []audioEvent{wantNote(first, 60, 65)}) {
					t.Fatalf("resumed envelope = %+v, want frozen age and no peer retrigger", got)
				}
			})
		}
	}
}

func TestAudioResumeAgeClampsToPortableMilliseconds(t *testing.T) {
	collector := &audioCollector{}
	for _, age := range []time.Duration{-1, 999 * time.Microsecond, 65 * time.Millisecond, time.Duration(1<<63 - 1)} {
		collector.ResumeNote(0xfedcba98, 9, 40, 100, age)
	}
	for i, event := range collector.take() {
		want := []uint32{0, 0, 65, 1<<32 - 1}[i]
		if event.Kind != audioNoteResume || event.Sound != 0xfedcba98 || event.Age != want {
			t.Fatalf("resume %d = %+v, want age %d", i, event, want)
		}
	}
}

package lgt

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

func TestJavaPauseResumeRetainsRepeatAndStopEndsPause(t *testing.T) {
	client := mediaClient(t, nil)
	clip := &mediaClip{data: oneNoteSound(t)}
	client.clips = map[uint32]*mediaClip{1: clip}
	if got, err := javaPlayerPlay(client, nil, nil, []uint32{1, 1}); got != javaTrue || err != nil {
		t.Fatalf("play = %d, %v", got, err)
	}
	if got, _ := javaPlayerPlay(client, nil, nil, []uint32{1, 0}); got != javaFalse {
		t.Fatal("duplicate play accepted")
	}
	if got, _ := javaPlayerPause(client, nil, nil, []uint32{1}); got != javaTrue {
		t.Fatal("pause failed")
	}
	if client.audio.Playing(clip.handle) {
		t.Fatal("paused clip still playing")
	}
	if got, _ := javaPlayerPause(client, nil, nil, []uint32{1}); got != javaFalse {
		t.Fatal("duplicate pause accepted")
	}
	if got, _ := javaPlayerResume(client, nil, nil, []uint32{1}); got != javaTrue {
		t.Fatal("resume failed")
	}
	client.clock.advance(time.Second)
	client.serviceAudio()
	if !client.audio.Playing(clip.handle) {
		t.Fatal("resume lost repeat")
	}
	if got, _ := javaPlayerResume(client, nil, nil, []uint32{1}); got != javaFalse {
		t.Fatal("duplicate resume accepted")
	}
	javaPlayerPause(client, nil, nil, []uint32{1})
	javaPlayerStop(client, nil, nil, []uint32{1})
	if got, _ := javaPlayerResume(client, nil, nil, []uint32{1}); got != javaFalse {
		t.Fatal("stopped clip resumed")
	}
}

type pauseOutputProbe struct {
	recordingSink
	ons, offs, stops map[backend.AudioHandle]int
	resumed          map[backend.AudioHandle][]time.Duration
}

var _ backend.AudioResumeSink = (*pauseOutputProbe)(nil)

func (sink *pauseOutputProbe) AudioEvent(sound backend.AudioHandle, event smaf.Event) {
	switch event.Type {
	case smaf.EventNoteOn:
		sink.ons[sound]++
	case smaf.EventNoteOff:
		sink.offs[sound]++
	}
}

func (sink *pauseOutputProbe) StopSound(sound backend.AudioHandle) { sink.stops[sound]++ }
func (sink *pauseOutputProbe) ResumeNote(sound backend.AudioHandle, _, _, _ uint8, age time.Duration) {
	sink.resumed[sound] = append(sink.resumed[sound], age)
}

func pauseMediaClient(t *testing.T) (*Client, *pauseOutputProbe, func(time.Duration)) {
	t.Helper()
	sink := &pauseOutputProbe{
		ons: make(map[backend.AudioHandle]int), offs: make(map[backend.AudioHandle]int),
		stops: make(map[backend.AudioHandle]int), resumed: make(map[backend.AudioHandle][]time.Duration),
	}
	client := mediaClient(t, sink)
	now := time.Unix(1000, 0)
	client.audio = backend.NewAudioWithClock(sink, func() time.Time { return now })
	return client, sink, func(delta time.Duration) {
		now = now.Add(delta)
		client.clock.advance(delta)
		client.serviceAudio()
	}
}

func TestJavaPauseResumesRemainingGateThroughGuestEntry(t *testing.T) {
	for _, repeat := range []uint32{0, 1} {
		t.Run(map[uint32]string{0: "once", 1: "repeat"}[repeat], func(t *testing.T) {
			client, sink, advance := pauseMediaClient(t)
			client.clips = map[uint32]*mediaClip{
				1: {data: oneNoteSound(t), volume: mediaMaxVolume},
				2: {data: oneNoteSound(t), volume: mediaMaxVolume},
			}
			call := func(name string, want uint32, arguments ...uint32) {
				t.Helper()
				descriptor := "(Lorg/kwis/msp/media/Clip;)Z"
				if name == "play" {
					descriptor = "(Lorg/kwis/msp/media/Clip;Z)Z"
				}
				const class = "org/kwis/msp/media/Player"
				method, ok := javaPlatformMethods[class+"."+name+descriptor]
				if !ok || method.Words != len(arguments) {
					t.Fatalf("missing Player.%s guest entry", name)
				}
				thread := armcore.NewThread(armcore.NewContext())
				for index, value := range arguments {
					if err := thread.SetRegister(index, value); err != nil {
						t.Fatal(err)
					}
				}
				if err := client.callJavaMethod(t.Context(), thread, class, name+descriptor, method); err != nil {
					t.Fatal(err)
				}
				if got, err := thread.Register(0); err != nil || got != want {
					t.Fatalf("Player.%s = %d, %v; want %d", name, got, err, want)
				}
			}
			call("pause", javaFalse, 1)
			call("resume", javaFalse, 1)
			call("play", javaTrue, 1, repeat)
			call("play", javaTrue, 2, 0)
			first, second := client.clips[1].handle, client.clips[2].handle
			call("resume", javaFalse, 1)
			advance(20 * time.Millisecond) // The authored note starts at 20 ms and ends at 40 ms.
			advance(5 * time.Millisecond)
			if sink.ons[first] != 1 || sink.ons[second] != 1 {
				t.Fatal("both clips did not reach their first note")
			}
			firstStops, secondStops := sink.stops[first], sink.stops[second]
			call("pause", javaTrue, 1)
			call("pause", javaFalse, 1)
			if sink.stops[first] != firstStops+1 || sink.stops[second] != secondStops {
				t.Fatal("pause stopped another owner or stopped the same owner twice")
			}
			advance(time.Second)
			if sink.ons[first] != 1 || sink.offs[first] != 0 || sink.offs[second] != 1 {
				t.Fatal("paused cursor advanced or the other clip failed to finish")
			}
			call("resume", javaTrue, 1)
			call("resume", javaFalse, 1)
			if ages := sink.resumed[first]; len(ages) != 1 || ages[0] != 5*time.Millisecond {
				t.Fatalf("resumed envelope ages = %v, want [5ms]", ages)
			}
			advance(14 * time.Millisecond)
			if sink.offs[first] != 0 {
				t.Fatal("resume shortened the remaining gate")
			}
			advance(time.Millisecond)
			if sink.offs[first] != 1 {
				t.Fatal("resume restarted the gate instead of finishing its remaining 15 ms")
			}
			advance(20 * time.Millisecond)
			if sink.ons[first] != 1+int(repeat) || client.audio.Playing(first) != (repeat != 0) {
				t.Fatal("resume changed the repeat mode or its original boundary")
			}
			if sink.stops[second] != secondStops || len(sink.resumed[second]) != 0 {
				t.Fatal("resuming one clip changed the other owner")
			}
			if repeat != 0 {
				call("pause", javaTrue, 1)
			}
			call("stop", javaTrue, 1)
			call("resume", javaFalse, 1)
			call("pause", javaFalse, 1)
		})
	}
}

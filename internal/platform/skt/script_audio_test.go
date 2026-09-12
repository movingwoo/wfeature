package skt

import (
	"context"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"testing"
	"time"
)

type scriptNoteSink struct {
	backend.AudioSink
	on, off, silenced int
}

func (s *scriptNoteSink) MIDINoteOn(channel, note, velocity uint8)  { s.on++ }
func (s *scriptNoteSink) MIDINoteOff(channel, note, velocity uint8) { s.off++ }
func (s *scriptNoteSink) MIDIControlChange(channel, control, value uint8) {
	if control == 120 || control == 123 {
		s.silenced++
	}
}

func TestScriptAudioClockPauseStopAndRestart(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	sink := &scriptNoteSink{AudioSink: backend.NewRecordingSink(nil)}
	s.audio.SetSink(sink)
	s.vm.Resources = []sgsvm.Resource{{Data: append([]byte{0, 0}, scriptOneNoteSMAF()...)}}
	play := func() {
		t.Helper()
		s.vm.Push(0)
		if err := s.Call(0x90, s.vm); err != nil {
			t.Fatal(err)
		}
	}
	advance := func(ms int) {
		t.Helper()
		if _, err := s.Advance(context.Background(), time.Duration(ms)*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	play()
	advance(10)
	if sink.on != 0 {
		t.Fatal("note emitted before its guest deadline")
	}
	s.Pause()
	advance(200)
	if sink.on != 0 {
		t.Fatal("paused clock emitted note")
	}
	s.Resume()
	advance(15)
	if sink.on != 1 {
		t.Fatalf("note count %d, want 1", sink.on)
	}
	if err := s.Call(0x91, s.vm); err != nil {
		t.Fatal(err)
	}
	if sink.silenced == 0 || s.audio.Playing(s.sound) {
		t.Fatal("stop left the note sounding")
	}
	play()
	advance(25)
	if sink.on != 2 {
		t.Fatal("sound could not restart")
	}
	previous := sink.silenced
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if sink.silenced <= previous || s.audio.Playing(s.sound) {
		t.Fatal("session close left the note sounding")
	}
	advance(200)
	if sink.on != 2 {
		t.Fatal("closed session emitted another note")
	}
}

// Authored single-note fixture shared in shape with the SKT Java audio tests.
func scriptOneNoteSMAF() []byte {
	sequence := []byte{
		0x05, 0x90, 60, 100, 0x05,
		0x00, 0xff, 0x2f, 0x00,
	}
	track := append([]byte{2, 0, 2, 2}, make([]byte, 16)...)
	track = append(track, scriptSMAFChunk("Mtsq", sequence)...)
	body := scriptSMAFChunk("MTR\x00", track)
	file := make([]byte, 8)
	copy(file, "MMMD")
	length := uint32(len(body) + 2)
	file[4], file[5], file[6], file[7] = byte(length>>24), byte(length>>16), byte(length>>8), byte(length)
	return append(append(file, body...), 0, 0)
}

func scriptSMAFChunk(tag string, payload []byte) []byte {
	header := make([]byte, 8)
	copy(header, tag)
	length := uint32(len(payload))
	header[4], header[5], header[6], header[7] = byte(length>>24), byte(length>>16), byte(length>>8), byte(length)
	return append(header, payload...)
}

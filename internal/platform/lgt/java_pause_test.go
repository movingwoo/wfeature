package lgt

import (
	"testing"
	"time"
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

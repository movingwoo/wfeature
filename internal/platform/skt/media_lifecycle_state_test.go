package skt

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestMIDPLifecycleCheckpointRejectsInconsistentPlayerProgress(t *testing.T) {
	fixture := newMediaLifecycleFixture(t, 2)
	fixture.tick(500 * time.Millisecond)
	original, err := fixture.runtime.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"unregistered open player", "excess completions", "invalid loops", "incorrect duration", "excess position", "closed with handle", "wrong listener"} {
		t.Run(name, func(t *testing.T) {
			var state javaCheckpointState
			if err := backend.DecodeCheckpointRecord(original.Runtime, &state); err != nil {
				t.Fatal(err)
			}
			var payload *jvm.HeapPayloadState
			for i := range state.Threads.Heap.Payloads {
				if state.Threads.Heap.Payloads[i].ExternalKind == "skt.player" {
					payload = &state.Threads.Heap.Payloads[i]
					break
				}
			}
			if payload == nil {
				t.Fatal("fixture has no Player payload")
			}
			var player checkpointPlayer
			if err := backend.DecodeCheckpointRecord(payload.Data, &player); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "unregistered open player":
				state.Platform.Players = nil
			case "excess completions":
				player.Completed = state.Audio.Sounds[0].Completed + 1
			case "invalid loops":
				player.Loops = 0
			case "incorrect duration":
				player.Duration += time.Second
			case "excess position":
				player.MediaTime = player.Duration.Microseconds() + 1
			case "closed with handle":
				player.State = playerClosed
			case "wrong listener":
				payload.References[1] = payload.References[0]
			}
			payload.Data, err = backend.EncodeCheckpointRecord(player)
			if err != nil {
				t.Fatal(err)
			}
			broken := original
			broken.Runtime, err = backend.EncodeCheckpointRecord(state)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareJavaCheckpoint(mediaLifecycleJAR, broken, Options{})
			if prepared != nil {
				prepared.Discard()
			}
			if err == nil {
				t.Fatal("inconsistent Player state was accepted")
			}
		})
	}
}

func TestMIDPFiniteCheckpointCatchupBounds(t *testing.T) {
	for _, count := range []int32{2, 1 << 22} {
		audio := backend.NewAudio(nil)
		handle, err := audio.LoadEvents([]smaf.Event{{Time: 1, Type: smaf.EventEnd}})
		if err != nil {
			t.Fatal(err)
		}
		if err := audio.PlayCount(handle, 0, count); err != nil {
			t.Fatal(err)
		}
		saved, err := audio.CaptureState()
		if err != nil {
			t.Fatal(err)
		}
		if err := validateCheckpointAudio(saved, time.Hour); (err == nil) != (count == 2) {
			t.Fatalf("finite count %d catchup = %v", count, err)
		}
		if err := audio.Pause(handle, 500*time.Microsecond); err != nil {
			t.Fatal(err)
		}
		saved, err = audio.CaptureState()
		if err != nil {
			t.Fatal(err)
		}
		if err := validateCheckpointAudio(saved, time.Hour); err != nil {
			t.Fatalf("paused finite count %d: %v", count, err)
		}
	}
}

package skt

import (
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestJavaCheckpointResumesAudioWaitWithoutReplayingPlayback(t *testing.T) {
	for _, repeat := range []bool{false, true} {
		for _, replaced := range []bool{false, true} {
			t.Run(map[bool]string{false: "play", true: "loop"}[repeat]+map[bool]string{false: "/stop", true: "/replaced"}[replaced], func(t *testing.T) {
				archive, err := Open(audioStopJAR)
				if err != nil {
					t.Fatal(err)
				}
				store, _ := backend.NewMemorySaveStore(nil)
				runtime, err := Start(archive, Options{SaveStore: store, Framebuffer: newTestFramebuffer(t, 4, 3), Speed: 0.1})
				if err != nil {
					t.Fatal(err)
				}
				defer runtime.transition("test cleanup", StateDestroyed)
				value, err := runtime.VM.InvokeStatic(skvm.AudioSystemClass, "getAudioClip", "(Ljava/lang/String;)Lcom/skt/m/AudioClip;", jvm.ReferenceValue(runtime.VM.NewString("mmf")))
				if err != nil {
					t.Fatal(err)
				}
				object, _ := value.Reference()
				sound := audioStopSound()
				if _, err = runtime.VM.InvokeVirtual(object, "open", "([BII)V", jvm.ReferenceValue(jvm.NewByteArray(sound)), jvm.IntValue(0), jvm.IntValue(int32(len(sound)))); err != nil {
					t.Fatal(err)
				}
				if err = runtime.VM.SetStaticField("AudioStopMIDlet", "clip", "Lcom/skt/m/AudioClip;", value); err != nil {
					t.Fatal(err)
				}
				loop := int32(0)
				if repeat {
					loop = 1
				}
				if err = runtime.VM.SetStaticField("AudioStopMIDlet", "loop", "Z", jvm.IntValue(loop)); err != nil {
					t.Fatal(err)
				}
				if _, err = runtime.VM.InvokeStatic("AudioStopMIDlet", "begin", "()V"); err != nil {
					t.Fatal(err)
				}
				clip := object.Native.(*audioClipData)
				deadline := time.Now().Add(time.Second)
				for {
					clip.mu.Lock()
					ready := clip.playing != nil
					clip.mu.Unlock()
					if ready {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("audio did not wait")
					}
					time.Sleep(time.Millisecond)
				}
				parked, release, err := runtime.parkCheckpoint(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				if replaced {
					clip.mu.Lock()
					runtime.startPlaying(clip)
					clip.mu.Unlock()
				}
				record, _, err := runtime.captureJavaState(parked)
				if err != nil {
					t.Fatal(err)
				}
				data, err := backend.EncodeCheckpointRecord(record)
				if err != nil {
					t.Fatal(err)
				}
				saved := backend.Checkpoint{Identity: archive.identity, Variant: backend.CheckpointSKTJava, Runtime: data}
				prepared, err := PrepareJavaCheckpoint(audioStopJAR, saved, Options{})
				if err != nil {
					t.Fatal(err)
				}
				defer prepared.Discard()
				restored, err := prepared.Commit(t.Context(), nil, store, newTestFramebuffer(t, 4, 3), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer restored.transition("test cleanup", StateDestroyed)
				value, err = restored.VM.StaticField("AudioStopMIDlet", "clip", "Lcom/skt/m/AudioClip;")
				if err != nil {
					t.Fatal(err)
				}
				object, _ = value.Reference()
				fresh := object.Native.(*audioClipData)
				fresh.mu.Lock()
				generation := fresh.generation
				fresh.mu.Unlock()
				if generation != clip.generation {
					t.Fatal("restoration replayed playback")
				}
				if !replaced {
					if _, err = restored.VM.InvokeVirtual(object, "stop", "()V"); err != nil {
						t.Fatal(err)
					}
				}
				deadline = time.Now().Add(time.Second)
				for invokeFixtureInt(t, restored, "AudioStopMIDlet", "result") == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if got := invokeFixtureInt(t, restored, "AudioStopMIDlet", "result"); got != 2 {
					t.Fatalf("restored audio result=%d", got)
				}
				fresh.mu.Lock()
				stillPlaying := fresh.playing != nil
				fresh.mu.Unlock()
				if replaced && !stillPlaying {
					t.Fatal("old audio wait ended replacement playback")
				}
			})
		}
	}
}

func checkpointLoopAudio(t *testing.T) backend.AudioState {
	t.Helper()
	audio := backend.NewAudio(nil)
	handle, err := audio.LoadEvents([]smaf.Event{{Time: 1, Type: smaf.EventProgramChange, Channel: 0, Program: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = audio.Play(handle, 0, true); err != nil {
		t.Fatal(err)
	}
	state, err := audio.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestCheckpointRejectsUnboundedAudioCatchup(t *testing.T) {
	t.Run("java", func(t *testing.T) {
		store, _ := backend.NewMemorySaveStore(nil)
		source := startJavaCheckpoint(t, store)
		slot, err := source.CaptureCheckpointWithSession(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var state javaCheckpointState
		if err = backend.DecodeCheckpointRecord(slot.Runtime, &state); err != nil {
			t.Fatal(err)
		}
		state.Elapsed = 100 * 365 * 24 * time.Hour
		state.Audio = checkpointLoopAudio(t)
		slot.Runtime, err = backend.EncodeCheckpointRecord(state)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := store.SnapshotSaves()
		prepared, err := PrepareJavaCheckpoint(javaCheckpointJAR, slot, Options{})
		if prepared != nil {
			prepared.Discard()
		}
		if err == nil {
			t.Fatalf("accepted audio requiring %d loop iterations on next tick", state.Elapsed/time.Millisecond)
		}
		after, _ := store.SnapshotSaves()
		if source.State() != StateActive || !reflect.DeepEqual(before, after) {
			t.Fatal("refused audio displaced the source or changed saves")
		}
	})
	t.Run("sgs", func(t *testing.T) {
		archive, source, store, _, _ := newScriptCheckpointTest(t)
		slot := scriptCheckpointCapture(t, source)
		var state scriptCheckpointState
		if err := backend.DecodeCheckpointRecord(slot.Runtime, &state); err != nil {
			t.Fatal(err)
		}
		state.Clock = 100 * 365 * 24 * time.Hour
		state.Audio = checkpointLoopAudio(t)
		var err error
		slot.Runtime, err = backend.EncodeCheckpointRecord(state)
		if err != nil {
			t.Fatal(err)
		}
		writes := store.writes
		prepared, err := PrepareScriptCheckpoint(archive, slot, ScriptOptions{})
		if prepared != nil {
			prepared.Discard()
		}
		if err == nil {
			t.Fatalf("accepted audio requiring %d loop iterations on next tick", state.Clock/time.Millisecond)
		}
		if store.writes != writes {
			t.Fatal("refused audio changed ordinary saves")
		}
		if _, err := source.Tick(t.Context()); err != nil {
			t.Fatalf("refused audio stopped the source: %v", err)
		}
	})
}

func TestCheckpointAudioCatchupBudget(t *testing.T) {
	audio := checkpointLoopAudio(t)
	if err := validateCheckpointAudio(audio, time.Second); err != nil {
		t.Fatalf("bounded repeat refused: %v", err)
	}
	audio.Sounds[0].Playing = false
	if err := validateCheckpointAudio(audio, 100*365*24*time.Hour); err != nil {
		t.Fatalf("old stopped sound refused: %v", err)
	}
	audio.Sounds[0].Playing, audio.Sounds[0].Repeat = true, false
	if err := validateCheckpointAudio(audio, 100*365*24*time.Hour); err != nil {
		t.Fatalf("bounded one-shot refused: %v", err)
	}
	audio.Sounds[0].Repeat = true
	audio.Sounds = append(audio.Sounds, audio.Sounds[0])
	audio.Sounds[1].Handle, audio.Next = 2, 2
	if err := validateCheckpointAudio(audio, (1<<19)*time.Millisecond); err != nil {
		t.Fatalf("exact aggregate pending event limit refused: %v", err)
	}
	if err := validateCheckpointAudio(audio, ((1<<19)+1)*time.Millisecond); err == nil {
		t.Fatal("aggregate pending event limit was not enforced")
	}
	for _, kind := range []smaf.EventType{smaf.EventWave, smaf.EventSysEx} {
		audio = checkpointLoopAudio(t)
		event := &audio.Sounds[0].Events[0]
		event.Type = kind
		if kind == smaf.EventWave {
			event.WaveChannels, event.SamplingRate = 1, 8000
			event.Wave = make([]int16, 1<<19)
		} else {
			event.SysEx = make([]byte, 1<<20)
		}
		if err := validateCheckpointAudio(audio, time.Millisecond); err != nil {
			t.Fatalf("bounded audio data refused: %v", err)
		}
		if err := validateCheckpointAudio(audio, time.Second); err == nil {
			t.Fatalf("type %v accepted over a gigabyte of pending audio data", kind)
		}
	}
}

func TestJavaCheckpointAcceptsConsumedAudioPrefix(t *testing.T) {
	for _, prepare := range []bool{false, true} {
		t.Run(map[bool]string{false: "capture", true: "prepare"}[prepare], func(t *testing.T) {
			source, store := startWIPIDisplayCheckpoint(t)
			clock := installMediaPauseClock(source, newMediaPauseProbe())
			slot, err := source.CaptureCheckpointWithSession(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			const pairs = 1200
			events := make([]smaf.Event, 0, pairs*2+1)
			for i := range uint32(pairs) {
				events = append(events,
					smaf.Event{Time: i*2 + 1, Type: smaf.EventNoteOn, Note: 60, Velocity: 80},
					smaf.Event{Time: i*2 + 2, Type: smaf.EventNoteOff, Note: 60})
			}
			events = append(events, smaf.Event{Time: pairs*2 + 1, Type: smaf.EventEnd})
			handle, err := source.audio.LoadEvents(events)
			if err != nil {
				t.Fatal(err)
			}
			if err = source.audio.Play(handle, 0, false); err != nil {
				t.Fatal(err)
			}
			clock.advance(pairs * 2 * time.Millisecond)
			source.AdvanceAudio()
			audio, err := source.audio.CaptureState()
			if err != nil || len(audio.Sounds) != 1 || audio.Sounds[0].Cursor != pairs*2 || len(audio.Sounds[0].ActiveNotes) != 0 {
				t.Fatalf("authored prefix was not consumed: %v", err)
			}
			before, _ := store.SnapshotSaves()
			if prepare {
				var state javaCheckpointState
				if err = backend.DecodeCheckpointRecord(slot.Runtime, &state); err != nil {
					t.Fatal(err)
				}
				state.Audio, state.Elapsed, state.Instant = audio, source.GuestElapsed(), clock.now().UnixNano()
				if slot.Runtime, err = backend.EncodeCheckpointRecord(state); err != nil {
					t.Fatal(err)
				}
			} else if slot, err = source.CaptureCheckpointWithSession(t.Context(), nil); err != nil {
				t.Fatalf("consumed score prefix prevented capture: %v", err)
			}
			prepared, err := PrepareJavaCheckpoint(audioGainJAR, slot, Options{})
			if err != nil {
				t.Fatalf("consumed score prefix prevented preparation: %v", err)
			}
			defer prepared.Discard()
			if calls, _ := prepared.PreparationStoreCalls(); calls != 0 {
				t.Fatal("detached preparation accessed live saves")
			}
			after, _ := store.SnapshotSaves()
			if source.State() != StateActive || !reflect.DeepEqual(before, after) || prepared.saved.Audio.Sounds[0].Cursor != pairs*2 {
				t.Fatal("checkpoint changed saves, displaced the source or replayed the consumed prefix")
			}
		})
	}
}

func TestCheckpointRejectsQuadraticAudioCatchup(t *testing.T) {
	for _, accumulated := range []bool{false, true} {
		name := "saved keys and absent note-offs"
		if accumulated {
			name = "pending distinct note-ons"
		}
		t.Run(name, func(t *testing.T) {
			archive, source, store, _, _ := newScriptCheckpointTest(t)
			slot := scriptCheckpointCapture(t, source)
			var state scriptCheckpointState
			if err := backend.DecodeCheckpointRecord(slot.Runtime, &state); err != nil {
				t.Fatal(err)
			}
			state.Clock = 1025 * time.Millisecond
			state.Audio = checkpointLoopAudio(t)
			sound := &state.Audio.Sounds[0]
			sound.Events = []smaf.Event{{Time: 1, Type: smaf.EventNoteOff, Channel: 15, Note: 127}}
			if accumulated {
				notes := make([]smaf.Event, 1450, 1451)
				for i := range notes {
					notes[i] = smaf.Event{Type: smaf.EventNoteOn, Channel: uint8(i / 128), Note: uint8(i % 128), Velocity: 64}
				}
				sound.Events = append(notes, sound.Events...)
				state.Clock = time.Millisecond
			} else {
				sound.ActiveNotes = make([]backend.AudioNoteState, 1024)
				for i := range sound.ActiveNotes {
					sound.ActiveNotes[i] = backend.AudioNoteState{Channel: uint8(i / 128), Note: uint8(i % 128)}
				}
			}
			if _, err := backend.NewAudioFromState(state.Audio, nil); err != nil {
				t.Fatalf("adversarial audio must have a valid saved shape: %v", err)
			}
			var err error
			slot.Runtime, err = backend.EncodeCheckpointRecord(state)
			if err != nil {
				t.Fatal(err)
			}
			writes := store.writes
			prepared, err := PrepareScriptCheckpoint(archive, slot, ScriptOptions{})
			if prepared != nil {
				prepared.Discard()
			}
			if err == nil || store.writes != writes {
				t.Fatalf("quadratic note work accepted or saves changed: %v", err)
			}
			if _, err := source.Tick(t.Context()); err != nil {
				t.Fatalf("refused audio stopped the source: %v", err)
			}
		})
	}
}

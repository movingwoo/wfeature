package skt

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// A write through a second handle on the fixture's file lands in the one file
// both handles share, so its close stores both writes, and a quick save or a
// quick load after it has no older buffer to put over them.
func TestJavaCheckpointFlushPreservesLaterWrite(t *testing.T) {
	for _, load := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "load"}[load], func(t *testing.T) {
			store, _ := backend.NewMemorySaveStore(nil)
			source := startJavaCheckpoint(t, store)
			slot, err := source.CaptureCheckpointWithSession(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = source.VM.InvokeStatic("CheckpointMIDlet", "writeCurrent", "(I)V", jvm.IntValue(4)); err != nil {
				t.Fatal(err)
			}
			second := &jvm.Object{ClassName: skvm.XFileClass}
			receiver := jvm.ReferenceValue(second)
			if _, err = source.initXFileName(source.VM, []jvm.Value{receiver, jvm.ReferenceValue(source.VM.NewString("checkpoint")), jvm.IntValue(3)}); err != nil {
				t.Fatal(err)
			}
			if _, err = source.xFileSeek(source.VM, []jvm.Value{receiver, jvm.IntValue(1), jvm.IntValue(0)}); err != nil {
				t.Fatal(err)
			}
			if _, err = source.xFileWrite(source.VM, []jvm.Value{receiver, jvm.ReferenceValue(jvm.NewByteArray([]byte{9})), jvm.IntValue(0), jvm.IntValue(1)}); err != nil {
				t.Fatal(err)
			}
			if _, err = source.xFileClose(source.VM, []jvm.Value{receiver}); err != nil {
				t.Fatal(err)
			}
			before, _ := store.LoadSave("fs/checkpoint")
			if !bytes.Equal(before, []byte{4, 9, 3}) {
				t.Fatalf("the second handle's close stored %v, want both handles' writes", before)
			}
			if load {
				prepared, prepErr := PrepareJavaCheckpoint(javaCheckpointJAR, slot, Options{})
				if prepErr != nil {
					t.Fatal(prepErr)
				}
				defer prepared.Discard()
				restored, loadErr := prepared.Commit(t.Context(), source, store, newTestFramebuffer(t, 32, 24), nil)
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				defer restored.transition("test cleanup", StateDestroyed)
				if _, err = restored.VM.InvokeStatic("CheckpointMIDlet", "readCurrent", "()V"); err != nil {
					t.Fatal(err)
				}
				if got := checkpointJavaInt(t, restored, "readValue"); got != 404 {
					t.Fatalf("restored session read %d, want the later record and file 404", got)
				}
			} else if _, err = source.CaptureCheckpointWithSession(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			after, _ := store.LoadSave("fs/checkpoint")
			if !bytes.Equal(before, after) {
				t.Fatalf("checkpoint changed the later save: before=%v after=%v", before, after)
			}
		})
	}
}

// Handles on one path share its file, so the step before a checkpoint stores
// one current file per path — never an older handle's bytes over a newer
// write, a removal or a write a refused store left dirty — and a store that
// refuses it refuses the step with the file still dirty in the session.
func TestCheckpointFlushStoresTheCurrentFileOfEveryPath(t *testing.T) {
	for _, scenario := range []string{"aliases", "later removal", "failed later write", "open after failed write", "failed stream flush", "refused"} {
		t.Run(scenario, func(t *testing.T) {
			memory, _ := backend.NewMemorySaveStore(nil)
			store := &javaCheckpointFailStore{MemorySaveStore: memory}
			runtime := startJavaCheckpoint(t, store)
			if _, err := runtime.CaptureCheckpointWithSession(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			open := func(name string) jvm.Value {
				t.Helper()
				object := jvm.ReferenceValue(&jvm.Object{ClassName: skvm.XFileClass})
				if _, err := runtime.initXFileName(runtime.VM, []jvm.Value{object, jvm.ReferenceValue(runtime.VM.NewString(name)), jvm.IntValue(3)}); err != nil {
					t.Fatal(err)
				}
				return object
			}
			write := func(file jvm.Value, at int32, value byte) {
				t.Helper()
				if _, err := runtime.xFileSeek(runtime.VM, []jvm.Value{file, jvm.IntValue(at), jvm.IntValue(0)}); err != nil {
					t.Fatal(err)
				}
				if _, err := runtime.xFileWrite(runtime.VM, []jvm.Value{file, jvm.ReferenceValue(jvm.NewByteArray([]byte{value})), jvm.IntValue(0), jvm.IntValue(1)}); err != nil {
					t.Fatal(err)
				}
			}
			first, second := open("/checkpoint"), open("checkpoint")
			write(first, 0, 4)
			write(second, 1, 9)
			want := []byte{4, 9, 3}
			switch scenario {
			case "later removal":
				if _, err := runtime.xFileUnlink(runtime.VM, []jvm.Value{jvm.ReferenceValue(runtime.VM.NewString("checkpoint"))}); err != nil {
					t.Fatal(err)
				}
				want = nil
			case "failed later write", "open after failed write", "failed stream flush":
				store.failWrite = true
				if scenario == "failed stream flush" {
					handle, _ := xFileArgument([]jvm.Value{second})
					if err := runtime.persistWIPIFileStream(handle); err == nil {
						t.Fatal("stream flush unexpectedly succeeded")
					}
				} else if _, err := runtime.xFileFlush(runtime.VM, []jvm.Value{second}); err != nil {
					t.Fatal(err)
				}
				store.failWrite = false
				if scenario == "open after failed write" {
					write(open("/checkpoint"), 2, 7)
					want = []byte{4, 9, 7}
				}
			}
			write(open("another"), 0, 7)
			if scenario == "refused" {
				store.failWrite = true
				before, _ := memory.SnapshotSaves()
				if _, err := runtime.flushCheckpointSaves(t.Context()); !errors.Is(err, backend.ErrCheckpointSaveWrite) {
					t.Fatalf("refused flush: %v", err)
				}
				after, _ := memory.SnapshotSaves()
				handle, _ := xFileArgument([]jvm.Value{first})
				if !reflect.DeepEqual(before, after) || !handle.node.dirty {
					t.Fatal("a refused flush changed the saves or dropped the dirty file")
				}
				store.failWrite = false
			}
			if _, err := runtime.flushCheckpointSaves(t.Context()); err != nil {
				t.Fatal(err)
			}
			if data, _ := memory.LoadSave("fs/checkpoint"); !bytes.Equal(data, want) {
				t.Fatalf("flush stored %v for the shared path, want %v", data, want)
			}
			if data, _ := memory.LoadSave("fs/another"); !bytes.Equal(data, []byte{7}) {
				t.Fatalf("flush stored %v for the unrelated path", data)
			}
			if list, _ := memory.LoadSave(xFileRemovedKey); (scenario == "later removal") != bytes.Equal(list, []byte("checkpoint")) {
				t.Fatalf("removal list after the flush = %q", list)
			}
			runtime.fileMu.Lock()
			dirty := runtime.dirtyFileKeysLocked()
			runtime.fileMu.Unlock()
			if len(dirty) != 0 {
				t.Fatalf("a successful flush left %v dirty", dirty)
			}
		})
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

func TestRMSCloseRetriesOnlyIssuedWrites(t *testing.T) {
	memory, _ := backend.NewMemorySaveStore(nil)
	boundary := &javaCheckpointFailStore{MemorySaveStore: memory}
	runtime := startJavaCheckpoint(t, boundary)
	boundary.failWrite = true
	value, err := runtime.rmsOpenRecordStore(runtime.VM, []jvm.Value{jvm.ReferenceValue(runtime.VM.NewString("fresh")), jvm.IntValue(1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.rmsAddRecord(runtime.VM, []jvm.Value{value, jvm.ReferenceValue(jvm.NewByteArray([]byte{9})), jvm.IntValue(0), jvm.IntValue(1)}); err != nil {
		t.Fatal(err)
	}
	boundary.failWrite = false
	if _, err = runtime.rmsCloseRecordStore(runtime.VM, []jvm.Value{value}); err != nil {
		t.Fatal(err)
	}
	index, _ := memory.LoadSave(rmsIndexKey)
	encoded, _ := memory.LoadSave("rms/fresh")
	records, err := backend.DecodeSaveRecords(encoded)
	if err != nil || !bytes.Contains(index, []byte("fresh")) || len(records) != 1 || !bytes.Equal(records[0], []byte{9}) || len(runtime.pendingSaves) != 0 {
		t.Fatalf("close lost issued writes: index=%q records=%v error=%v", index, records, err)
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

func TestJavaCheckpointCloseKeepsLaterRMSDeletion(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	source := startJavaCheckpoint(t, store)
	slot, err := source.CaptureCheckpointWithSession(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := source.VM.StaticField("CheckpointMIDlet", "store", "Ljavax/microedition/rms/RecordStore;")
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.Reference()
	if _, err = source.VM.InvokeVirtual(object, "closeRecordStore", "()V"); err != nil {
		t.Fatal(err)
	}
	if _, err = source.rmsDeleteRecordStore(source.VM, []jvm.Value{jvm.ReferenceValue(source.VM.NewString("checkpoint"))}); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareJavaCheckpoint(javaCheckpointJAR, slot, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	restored, err := prepared.Commit(t.Context(), source, store, newTestFramebuffer(t, 32, 24), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.transition("test cleanup", StateDestroyed)
	index, _ := store.LoadSave(rmsIndexKey)
	if len(index) != 0 {
		t.Fatalf("deletion already lost at commit: %q", index)
	}
	value, err = restored.VM.StaticField("CheckpointMIDlet", "store", "Ljavax/microedition/rms/RecordStore;")
	if err != nil {
		t.Fatal(err)
	}
	object, _ = value.Reference()
	if _, err = restored.VM.InvokeVirtual(object, "closeRecordStore", "()V"); err != nil {
		t.Fatal(err)
	}
	index, _ = store.LoadSave(rmsIndexKey)
	if len(index) != 0 {
		t.Fatalf("closing unchanged restored handle recreated deleted RMS: %q", index)
	}
	// A subsequent real mutation through the restored open handle may create
	// the store again, with only the current records.
	preparedAgain, err := PrepareJavaCheckpoint(javaCheckpointJAR, slot, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer preparedAgain.Discard()
	mutating, err := preparedAgain.Commit(t.Context(), restored, store, newTestFramebuffer(t, 32, 24), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mutating.transition("test cleanup", StateDestroyed)
	value, err = mutating.VM.StaticField("CheckpointMIDlet", "store", "Ljavax/microedition/rms/RecordStore;")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mutating.rmsAddRecord(mutating.VM, []jvm.Value{value, jvm.ReferenceValue(jvm.NewByteArray([]byte{7})), jvm.IntValue(0), jvm.IntValue(1)}); err != nil {
		t.Fatal(err)
	}
	index, _ = store.LoadSave(rmsIndexKey)
	encoded, _ := store.LoadSave("rms/checkpoint")
	records, err := backend.DecodeSaveRecords(encoded)
	if err != nil || string(index) != "checkpoint" || len(records) != 1 || !bytes.Equal(records[0], []byte{7}) {
		t.Fatalf("new mutation failed to create current records: index=%q records=%v error=%v", index, records, err)
	}
}

package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestCheckpointSKTJavaRestoresPresentedFrameAndInput(t *testing.T) {
	archive := sktFixture(t)
	store, _ := backend.NewMemorySaveStore(nil)
	s, err := Start(t.Context(), archive, Options{SaveStore: store, Width: 32, Height: 24, Speed: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.CanCheckpoint() {
		t.Fatal("Java checkpoint unavailable")
	}
	if err = s.SendKey(t.Context(), KeyPress, '5'); err != nil {
		t.Fatal(err)
	}
	frame, w, h, shown := s.Frame()
	saved, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	old := s.runtime
	if err = store.StoreSave("later", []byte("current")); err != nil {
		t.Fatal(err)
	}
	if err = s.LoadCheckpoint(t.Context(), archive, saved); err != nil {
		t.Fatal(err)
	}
	if s.runtime == old || !s.Running() || s.Speed() != 2 || len(s.HeldKeys()) != 1 {
		t.Fatal("Java shared state not restored")
	}
	got, gw, gh, gshown := s.Frame()
	if gw != w || gh != h || shown != gshown || !bytes.Equal(frame, got) {
		t.Fatal("presented frame changed")
	}
	if current, _ := store.LoadSave("later"); string(current) != "current" {
		t.Fatal("ordinary save changed")
	}
	if err = s.ReleaseHeldInput(t.Context()); err != nil || len(s.HeldKeys()) != 0 {
		t.Fatalf("input release: %v", err)
	}
	if _, err = s.Tick(t.Context(), 0); err != nil {
		t.Fatal(err)
	}
}

func sktJavaCheckpointFixture(t *testing.T) []byte {
	t.Helper()
	archive, err := os.ReadFile(filepath.Join("..", "platform", "skt", "testdata", "checkpoint-skt.zip"))
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func sharedJavaCheckpointInt(t *testing.T, vm *jvm.VM, field string) int32 {
	t.Helper()
	value, err := vm.StaticField("CheckpointMIDlet", field, "I")
	if err != nil {
		t.Fatal(err)
	}
	number, err := value.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return number
}

func waitSharedJavaCheckpointInt(t *testing.T, s *Session, field string, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sharedJavaCheckpointInt(t, s.runtime.VM, field) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("checkpoint fixture %s = %d, want %d", field, sharedJavaCheckpointInt(t, s.runtime.VM, field), want)
}

func startSharedJavaCheckpoint(t *testing.T, archive []byte, store backend.SaveStore) *Session {
	t.Helper()
	s, err := Start(t.Context(), archive, Options{SaveStore: store, Width: 32, Height: 24, Speed: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	waitSharedJavaCheckpointInt(t, s, "before", 1)
	if !s.CanCheckpoint() || sharedJavaCheckpointInt(t, s.runtime.VM, "value") != 0 {
		t.Fatal("Java checkpoint fixture did not initialize")
	}
	if err := s.SendKey(t.Context(), KeyPress, '5'); err != nil {
		t.Fatal(err)
	}
	return s
}

func storeCurrentJavaCheckpointSaves(t *testing.T, store backend.SaveStore) {
	t.Helper()
	for key, data := range map[string][]byte{"rms/checkpoint": backend.EncodeSaveRecords([][]byte{{9}}), "fs/checkpoint": {9, 8, 7}} {
		if err := store.StoreSave(key, data); err != nil {
			t.Fatal(err)
		}
	}
}

type sharedJavaCheckpointFrame struct {
	RGBA    []byte
	Flushes uint64
	Paused  bool
}

func sharedJavaCheckpointFrameOf(t *testing.T, s *Session) sharedJavaCheckpointFrame {
	t.Helper()
	frame, width, height, shown := s.Frame()
	if !shown || width != 32 || height != 24 {
		t.Fatal("checkpoint fixture did not present a frame")
	}
	return sharedJavaCheckpointFrame{RGBA: bytes.Clone(frame), Flushes: s.surface.Flushes(), Paused: s.Paused()}
}

func checkSharedJavaCheckpointContinuation(t *testing.T, s *Session, expected sharedJavaCheckpointFrame) {
	t.Helper()
	if !s.Running() || s.Paused() != expected.Paused || s.Speed() != 2 || !reflect.DeepEqual(s.HeldKeys(), []int32{'5'}) {
		t.Fatal("restored Java session lost lifecycle, speed or input ownership")
	}
	if frame := sharedJavaCheckpointFrameOf(t, s); !reflect.DeepEqual(frame, expected) {
		t.Fatal("restored Java session changed its last presented frame")
	}
	for field, want := range map[string]int32{"starts": 1, "destroys": 0, "before": 1, "after": 0, "value": 1} {
		if got := sharedJavaCheckpointInt(t, s.runtime.VM, field); got != want {
			t.Fatalf("restored %s = %d, want %d without callback or worker prefix replay", field, got, want)
		}
	}
	if _, err := s.runtime.VM.InvokeStatic("CheckpointMIDlet", "readCurrent", "()V"); err != nil {
		t.Fatal(err)
	}
	if got := sharedJavaCheckpointInt(t, s.runtime.VM, "readValue"); got != 909 {
		t.Fatalf("restored RMS and input stream read %d, want current save data 909", got)
	}
	if err := s.ReleaseHeldInput(t.Context()); err != nil || len(s.HeldKeys()) != 0 {
		t.Fatalf("release restored input: %v", err)
	}
	if expected.Paused {
		if _, err := s.Tick(t.Context(), 0); !errors.Is(err, ErrPaused) {
			t.Fatalf("restored paused session tick: %v", err)
		}
		if err := s.Resume(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := sharedJavaCheckpointInt(t, s.runtime.VM, "starts"); got != 2 {
			t.Fatalf("explicit resume called startApp %d times, want 2 total", got)
		}
	}
	value, err := s.runtime.VM.StaticField("CheckpointMIDlet", "worker", "Ljava/lang/Thread;")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := value.Reference()
	if err != nil || worker == nil {
		t.Fatalf("restored worker reference: %v", err)
	}
	if _, err := s.runtime.VM.InvokeVirtual(worker, "interrupt", "()V"); err != nil {
		t.Fatal(err)
	}
	waitSharedJavaCheckpointInt(t, s, "after", 1)
	if got := sharedJavaCheckpointInt(t, s.runtime.VM, "before"); got != 1 {
		t.Fatalf("restored worker prefix executed %d times, want 1", got)
	}
	if _, err := s.Tick(t.Context(), 0); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointSKTJavaRestoresWorkersOverCurrentSaves(t *testing.T) {
	for _, mode := range []string{"active", "paused"} {
		t.Run(mode, func(t *testing.T) {
			archive := sktJavaCheckpointFixture(t)
			store := &saveStoreWatch{base: backend.NewDirectorySaveStore(t.TempDir())}
			s := startSharedJavaCheckpoint(t, archive, store)
			if mode == "paused" {
				if err := s.Pause(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			data, err := s.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			expected := sharedJavaCheckpointFrameOf(t, s)
			if data, ok := store.LoadSave("fs/checkpoint"); !ok || !bytes.Equal(data, []byte{1, 2, 3}) {
				t.Fatal("capture did not flush the already-issued file write")
			}
			storeCurrentJavaCheckpointSaves(t, store)
			before, err := store.base.SnapshotSaves()
			if err != nil {
				t.Fatal(err)
			}
			old, writes := s.runtime, store.writes
			if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
				t.Fatal(err)
			}
			if s.runtime == old || sharedJavaCheckpointInt(t, old.VM, "destroys") != 0 {
				t.Fatal("load retained the source or called its destroyApp")
			}
			checkSharedJavaCheckpointContinuation(t, s, expected)
			after, err := store.base.SnapshotSaves()
			if err != nil || store.writes != writes || !reflect.DeepEqual(after, before) {
				t.Fatalf("restoration or read-only continuation wrote ordinary saves: %v", err)
			}
		})
	}
}

func TestCheckpointSKTJavaRefusesMalformedStateWithoutDisplacingSource(t *testing.T) {
	archive := sktJavaCheckpointFixture(t)
	store := &saveStoreWatch{base: backend.NewDirectorySaveStore(t.TempDir())}
	s := startSharedJavaCheckpoint(t, archive, store)
	data, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original, err := backend.DecodeCheckpoint(data, backend.SaveIdentity(archive))
	if err != nil {
		t.Fatal(err)
	}
	storeCurrentJavaCheckpointSaves(t, store)
	before, err := store.base.SnapshotSaves()
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"clock":           func(s map[string]any) { s["Elapsed"] = -1 },
		"pixels":          func(s map[string]any) { s["Platform"].(map[string]any)["FrameRGBA"] = "" },
		"root bounds":     func(s map[string]any) { s["Platform"].(map[string]any)["MIDlet"] = -1 },
		"serial callback": func(s map[string]any) { s["Platform"].(map[string]any)["Events"] = []string{"Display.callSerially"} },
		"thread version":  func(s map[string]any) { s["Threads"].(map[string]any)["Version"] = 0 },
		"heap roots":      func(s map[string]any) { s["Threads"].(map[string]any)["Heap"].(map[string]any)["Roots"] = []int{} },
	} {
		t.Run(name, func(t *testing.T) {
			checkpoint := original
			var record map[string]any
			decoder := json.NewDecoder(bytes.NewReader(checkpoint.Runtime))
			decoder.UseNumber()
			if err := decoder.Decode(&record); err != nil {
				t.Fatal(err)
			}
			mutate(record)
			checkpoint.Runtime, err = json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			broken, err := backend.EncodeCheckpoint(checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			old, frame, writes := s.runtime, sharedJavaCheckpointFrameOf(t, s), store.writes
			if err := s.LoadCheckpoint(t.Context(), archive, broken); err == nil {
				t.Fatal("malformed Java checkpoint was accepted")
			}
			if s.runtime != old || !s.Running() || s.Failed() != nil || !reflect.DeepEqual(sharedJavaCheckpointFrameOf(t, s), frame) ||
				!reflect.DeepEqual(s.HeldKeys(), []int32{'5'}) || sharedJavaCheckpointInt(t, s.runtime.VM, "destroys") != 0 {
				t.Fatal("refused checkpoint displaced or changed the source")
			}
			after, err := store.base.SnapshotSaves()
			if err != nil || store.writes != writes || !reflect.DeepEqual(after, before) {
				t.Fatalf("refused checkpoint changed ordinary saves: %v", err)
			}
		})
	}
	if err := s.SendKey(t.Context(), KeyPress, '6'); err != nil || sharedJavaCheckpointInt(t, s.runtime.VM, "value") != 2 {
		t.Fatalf("source input after refusal: %v", err)
	}
	if err := s.LoadCheckpoint(t.Context(), archive, data); err != nil {
		t.Fatalf("valid load after refusal: %v", err)
	}
}

func TestCheckpointSKTJavaSubprocess(t *testing.T) {
	const variable = "WFEATURE_SKT_JAVA_CHECKPOINT_FIXTURE"
	if root := os.Getenv(variable); root != "" {
		read := func(name string) []byte {
			data, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
		var expected sharedJavaCheckpointFrame
		if err := json.Unmarshal(read("frame.json"), &expected); err != nil {
			t.Fatal(err)
		}
		store := &saveStoreWatch{base: backend.NewDirectorySaveStore(filepath.Join(root, "saves"))}
		before, err := store.base.SnapshotSaves()
		if err != nil {
			t.Fatal(err)
		}
		s, err := RestoreCheckpoint(t.Context(), read("archive.zip"), read("checkpoint.wfq"), Options{SaveStore: store})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		checkSharedJavaCheckpointContinuation(t, s, expected)
		after, err := store.base.SnapshotSaves()
		if err != nil || store.writes != 0 || !reflect.DeepEqual(after, before) {
			t.Fatalf("fresh process restore or continuation changed ordinary saves: %v", err)
		}
		return
	}
	for _, mode := range []string{"active", "paused"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			archive := sktJavaCheckpointFixture(t)
			store := backend.NewDirectorySaveStore(filepath.Join(root, "saves"))
			source := startSharedJavaCheckpoint(t, archive, store)
			if mode == "paused" {
				if err := source.Pause(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			data, err := source.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			expected, err := json.Marshal(sharedJavaCheckpointFrameOf(t, source))
			if err != nil {
				t.Fatal(err)
			}
			source.Close()
			storeCurrentJavaCheckpointSaves(t, store)
			for name, content := range map[string][]byte{"archive.zip": archive, "checkpoint.wfq": data, "frame.json": expected} {
				if err := os.WriteFile(filepath.Join(root, name), content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			program := os.Getenv("WFEATURE_SHARED_CHECKPOINT_RESTORE_BINARY")
			if program == "" {
				program = os.Args[0]
			}
			process := exec.CommandContext(t.Context(), program, "-test.run=^TestCheckpointSKTJavaSubprocess$", "-test.timeout=20s")
			process.Env = append(os.Environ(), variable+"="+root)
			if output, err := process.CombinedOutput(); err != nil {
				t.Fatalf("Java checkpoint process: %v\n%s", err, output)
			}
		})
	}
}

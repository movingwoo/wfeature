package session

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

func TestCheckpointScriptRestoresExecutionWithoutTouchingLaterSaves(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprint(paused), func(t *testing.T) {
			archive := scriptArchiveFixture(t)
			store, _ := backend.NewMemorySaveStore(nil)
			s, err := Start(t.Context(), archive, Options{SaveStore: store, Width: 3, Height: 2, Speed: 2})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if !s.CanCheckpoint() {
				t.Fatal("script checkpoint unavailable")
			}
			if err = s.SendKey(t.Context(), KeyPress, '5'); err != nil {
				t.Fatal(err)
			}
			if paused {
				if err = s.Pause(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			saved, err := s.CaptureCheckpoint(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			frame, w, h, ok := s.Frame()
			flushes := s.surface.Flushes()
			if !ok {
				t.Fatal("fixture has no presented frame")
			}
			if err = store.StoreSave("nv/data", []byte{9, 0}); err != nil {
				t.Fatal(err)
			}
			old := s.script
			if err = s.LoadCheckpoint(t.Context(), archive, saved); err != nil {
				t.Fatal(err)
			}
			if s.script == old || s.Paused() != paused || s.Speed() != 2 || !reflect.DeepEqual(s.HeldKeys(), []int32{'5'}) {
				t.Fatal("shared script state not restored")
			}
			got, gw, gh, gok := s.Frame()
			if !bytes.Equal(frame, got) || gw != w || gh != h || gok != ok || s.surface.Flushes() != flushes {
				t.Fatal("last presented script frame changed")
			}
			current, found := store.LoadSave("nv/data")
			if !found || !bytes.Equal(current, []byte{9, 0}) {
				t.Fatal("load ran termination or changed later saves")
			}
			if paused {
				if _, err = s.Tick(t.Context(), 0); !errors.Is(err, ErrPaused) {
					t.Fatal("paused restored script executed")
				}
				if err = s.Resume(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.ReleaseHeldInput(t.Context()); err != nil || len(s.HeldKeys()) != 0 {
				t.Fatalf("release: %v", err)
			}
			if _, err = s.script.Advance(t.Context(), 5*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if pixels, _, _, _ := s.Frame(); pixels[0] != 255 {
				t.Fatal("restored guest timer did not present white")
			}
			if err = s.SendKey(t.Context(), KeyPress, '5'); err != nil {
				t.Fatal(err)
			}
			current, _ = store.LoadSave("nv/data")
			if binary.LittleEndian.Uint16(current) != 2 {
				t.Fatalf("startup or key prefix replayed: %v", current)
			}
		})
	}
}

func TestCheckpointScriptRefusesDamageWithoutDisplacingGame(t *testing.T) {
	archive := scriptArchiveFixture(t)
	store, _ := backend.NewMemorySaveStore(nil)
	s, err := Start(t.Context(), archive, Options{SaveStore: store, Width: 3, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SendKey(t.Context(), KeyPress, '5'); err != nil {
		t.Fatal(err)
	}
	data, err := s.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original, err := backend.DecodeCheckpoint(data, backend.SaveIdentity(archive))
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(map[string]any){
		"clock":    func(s map[string]any) { s["Clock"] = -1 },
		"pixels":   func(s map[string]any) { s["Graphics"].(map[string]any)["Pixels"] = "" },
		"random":   func(s map[string]any) { s["Random"] = "AAAA" },
		"resource": func(s map[string]any) { s["VM"].(map[string]any)["Buffers"] = []string{"AA=="} },
		"queue": func(s map[string]any) {
			s["Graphics"].(map[string]any)["Queue"] = []any{map[string]any{"X": 0, "Y": 0, "Mirror": 0}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			checkpoint := original
			var record map[string]any
			if err := json.Unmarshal(checkpoint.Runtime, &record); err != nil {
				t.Fatal(err)
			}
			edit(record)
			checkpoint.Runtime, err = json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			bad, err := backend.EncodeCheckpoint(checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			old, flushes := s.script, s.surface.Flushes()
			if err = s.LoadCheckpoint(t.Context(), archive, bad); err == nil {
				t.Fatal("damaged script state accepted")
			}
			if s.script != old || s.Failed() != nil || !s.Running() || s.surface.Flushes() != flushes {
				t.Fatal("refused slot changed live script")
			}
			current, _ := store.LoadSave("nv/data")
			if len(current) < 2 || binary.LittleEndian.Uint16(current) != 1 {
				t.Fatal("refused slot changed saves")
			}
		})
	}
	if err = s.LoadCheckpoint(t.Context(), archive, data); err != nil {
		t.Fatalf("valid load after refusal: %v", err)
	}
}

func TestCheckpointScriptSubprocess(t *testing.T) {
	const variable = "WFEATURE_SKT_SCRIPT_CHECKPOINT_FIXTURE"
	if root := os.Getenv(variable); root != "" {
		archive, err := os.ReadFile(filepath.Join(root, "archive.zip"))
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, "checkpoint.wfq"))
		if err != nil {
			t.Fatal(err)
		}
		store := backend.NewDirectorySaveStore(filepath.Join(root, "saves"))
		s, err := RestoreCheckpoint(t.Context(), archive, data, Options{SaveStore: store})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		current, _ := store.LoadSave("nv/data")
		if !bytes.Equal(current, []byte{9, 0}) {
			t.Fatal("fresh process replaced current save")
		}
		frame, w, h, ok := s.Frame()
		if !ok || w != 3 || h != 2 || frame[0] != 0 {
			t.Fatal("fresh process lost checkpoint frame")
		}
		if err = s.SendKey(t.Context(), KeyPress, '5'); err != nil {
			t.Fatal(err)
		}
		current, _ = store.LoadSave("nv/data")
		if len(current) < 2 || binary.LittleEndian.Uint16(current) != 2 {
			t.Fatalf("fresh process replayed startup: %v", current)
		}
		return
	}
	root := t.TempDir()
	archive := scriptArchiveFixture(t)
	store := backend.NewDirectorySaveStore(filepath.Join(root, "saves"))
	source, err := Start(t.Context(), archive, Options{SaveStore: store, Width: 3, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if err = source.SendKey(t.Context(), KeyPress, '5'); err != nil {
		t.Fatal(err)
	}
	data, err := source.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	source.Close()
	if err = store.StoreSave("nv/data", []byte{9, 0}); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"archive.zip": archive, "checkpoint.wfq": data} {
		if err = os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	program := os.Getenv("WFEATURE_SHARED_CHECKPOINT_RESTORE_BINARY")
	if program == "" {
		program = os.Args[0]
	}
	process := exec.CommandContext(t.Context(), program, "-test.run=^TestCheckpointScriptSubprocess$", "-test.timeout=20s")
	process.Env = append(os.Environ(), variable+"="+root)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("script checkpoint process: %v\n%s", err, output)
	}
}

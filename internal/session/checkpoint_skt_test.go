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
	"slices"
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
			flushes := s.captureSurface().Flushes()
			if !ok {
				t.Fatal("fixture has no presented frame")
			}
			if err = store.StoreSave("nv/data", []byte{9, 0}); err != nil {
				t.Fatal(err)
			}
			old := s.sktScript()
			if err = s.LoadCheckpoint(t.Context(), archive, saved); err != nil {
				t.Fatal(err)
			}
			if s.sktScript() == old || s.Paused() != paused || s.Speed() != 2 || !reflect.DeepEqual(s.HeldKeys(), []int32{'5'}) {
				t.Fatal("shared script state not restored")
			}
			got, gw, gh, gok := s.Frame()
			if !bytes.Equal(frame, got) || gw != w || gh != h || gok != ok || s.captureSurface().Flushes() != flushes {
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
			if _, err = s.sktScript().Advance(t.Context(), 5*time.Millisecond); err != nil {
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
			old, flushes := s.sktScript(), s.captureSurface().Flushes()
			if err = s.LoadCheckpoint(t.Context(), archive, bad); err == nil {
				t.Fatal("damaged script state accepted")
			}
			if s.sktScript() != old || s.Failed() != nil || !s.Running() || s.captureSurface().Flushes() != flushes {
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

func TestCheckpointWideScriptResumesHeldInputAndTimer(t *testing.T) {
	// Each timer adds 32 to the red channel. Both key edges add eight.
	// Continuing from a checkpoint therefore depends on the saved variable,
	// held key, timer phase, and current drawing surface.
	data := make([]byte, 128)
	data[0], data[5] = 4, 4
	copy(data[10:26], "Timer fixture")
	paint := []uint16{4, 10, 5, 0, 5, 0, 0x92, 0x38, 0x92, 0x5f, 0xff}
	entries := [4][]uint16{
		0: {5, 10, 5, 1, 0x92, 0x95, 0x92, 0x3b, 0x92, 0x5f, 0xff},
		2: append([]uint16{0x7a, 10, 32}, paint...),
		3: append([]uint16{0x7a, 10, 8}, paint...),
	}
	for slot, code := range entries {
		if len(code) == 0 {
			continue
		}
		binary.LittleEndian.PutUint32(data[28+4*slot:], uint32(len(data)))
		for _, op := range code {
			data = binary.LittleEndian.AppendUint16(data, op)
		}
	}
	vd := len(data)
	for range 11 {
		data = append(data, 1, 0, 1, 0, 0, 0, 0, 0)
	}
	vi := len(data)
	data = append(data, make([]byte, 11*4)...)
	for i, offset := range []int{vd, vi, len(data), len(data)} {
		binary.LittleEndian.PutUint32(data[72+i*4:], uint32(offset))
	}
	binary.LittleEndian.PutUint32(data[120:], uint32(len(data)))
	archive := packScriptFixture(t, data)
	saveRoot := t.TempDir()
	store := backend.NewDirectorySaveStore(saveRoot)
	identity := backend.SaveIdentity(archive)
	live, err := Start(t.Context(), archive, Options{Width: 7, Height: 5, SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	advance := func(ms int) {
		t.Helper()
		if _, err := live.sktScript().Advance(t.Context(), time.Duration(ms)*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	frame := func() []byte {
		t.Helper()
		pixels, w, h, ok := live.Frame()
		if !ok || w != 7 || h != 5 || len(pixels) != w*h*4 {
			t.Fatal("missing fixture frame")
		}
		return pixels
	}
	advance(16)
	if err := live.SendKey(t.Context(), KeyPress, '5'); err != nil {
		t.Fatal(err)
	}
	snapshot := frame()
	slot, err := live.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StoreCheckpoint(identity, slot); err != nil {
		t.Fatal(err)
	}
	replay := func() [][]byte {
		t.Helper()
		if err := live.SendKey(t.Context(), KeyRelease, '5'); err != nil {
			t.Fatal(err)
		}
		edges := [][]byte{frame()}
		advance(9)
		edges = append(edges, frame())
		advance(1)
		return append(edges, frame())
	}
	want := replay()
	if bytes.Equal(snapshot, want[0]) || !bytes.Equal(want[0], want[1]) || bytes.Equal(want[1], want[2]) {
		t.Fatal("key edge or pending timer did not change the expected frame")
	}
	slot, found, err := backend.NewDirectorySaveStore(saveRoot).LoadCheckpoint(identity)
	if err != nil || !found {
		t.Fatalf("quicksave slot missing: %v", err)
	}
	if err := live.LoadCheckpoint(t.Context(), archive, slot); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshot, frame()) || !slices.Equal(live.HeldKeys(), []int32{'5'}) || !live.Running() {
		t.Fatal("restored frame, held key or running state differs")
	}
	if got := replay(); !reflect.DeepEqual(want, got) {
		t.Fatal("restored inputs and timer produced different frames")
	}

	// The browser releases keys from the old input epoch before it resumes a
	// paused slot. The guest must observe the release and stop repeating.
	if err := live.LoadCheckpoint(t.Context(), archive, slot); err != nil {
		t.Fatal(err)
	}
	if err := live.Pause(t.Context()); err != nil {
		t.Fatal(err)
	}
	paused, err := live.CaptureCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := live.LoadCheckpoint(t.Context(), archive, paused); err != nil || !live.Paused() {
		t.Fatalf("paused restore: %v", err)
	}
	if err := live.ReleaseHeldInput(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := live.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frame(), want[0]) || len(live.HeldKeys()) != 0 {
		t.Fatal("paused restore lost the release of the saved key")
	}
}

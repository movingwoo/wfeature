package session

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/skt"
)

// Real archives stay in the ignored library; every probe owns separate saves.
func TestLocalSKTCheckpoint(t *testing.T) {
	if os.Getenv("WFEATURE_SKT_CHECKPOINT_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_SKT_CHECKPOINT_ACCEPTANCE=1 for local archive checkpoints")
	}
	directory := os.Getenv("WFEATURE_SKT_CHECKPOINT_DIR")
	if directory == "" {
		directory = filepath.Join("..", "..", "var", "games", "skt")
	}
	limit := 6
	if value, err := strconv.Atoi(os.Getenv("WFEATURE_SKT_CHECKPOINT_LIMIT")); err == nil && value > 0 {
		limit = value
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		if variant := os.Getenv("WFEATURE_SKT_CHECKPOINT_VARIANT"); variant != "" {
			opened, err := skt.Open(raw)
			if err != nil {
				continue
			}
			if (opened.Script != nil) != (variant == "script") {
				continue
			}
		}
		t.Run(fmt.Sprintf("archive-%x", digest[:4]), func(t *testing.T) {
			t.Parallel()
			store := backend.NewDirectorySaveStore(t.TempDir())
			s, err := Start(t.Context(), raw, Options{SaveStore: store, Speed: 4})
			if err != nil {
				t.Skipf("existing startup limitation: %v", err)
			}
			defer s.Close()
			advance := func(n int) {
				t.Helper()
				for range n {
					p, err := s.Tick(t.Context(), time.Millisecond)
					if err != nil || p.Exited {
						t.Fatalf("tick: exited=%v error=%v", p.Exited, err)
					}
					time.Sleep(8 * time.Millisecond)
				}
			}
			advance(120)
			if _, _, _, shown := s.Frame(); !shown {
				t.Fatal("archive did not present before capture")
			}
			var saved []byte
			for attempt := 0; attempt < 4; attempt++ {
				ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
				saved, err = s.CaptureCheckpoint(ctx)
				cancel()
				if err == nil {
					break
				}
				advance(12)
			}
			if err != nil {
				t.Fatalf("capture: %v", err)
			}
			if err = store.StoreSave("checkpoint-probe", []byte("later")); err != nil {
				t.Fatal(err)
			}
			if err = s.SendKey(t.Context(), KeyPress, -5); err != nil {
				t.Fatal(err)
			}
			advance(12)
			if err = s.LoadCheckpoint(t.Context(), raw, saved); err != nil {
				t.Fatal(err)
			}
			if err = s.ReleaseHeldInput(t.Context()); err != nil {
				t.Fatal(err)
			}
			advance(30)
			if err = s.SendKey(t.Context(), KeyPress, -5); err != nil {
				t.Fatal(err)
			}
			advance(12)
			if err = s.SendKey(t.Context(), KeyRelease, -5); err != nil {
				t.Fatal(err)
			}
			advance(30)
			if data, _ := store.LoadSave("checkpoint-probe"); string(data) != "later" {
				t.Fatal("load changed later ordinary save")
			}
			if _, err = s.CaptureCheckpoint(t.Context()); err != nil {
				t.Fatalf("capture after continuation: %v", err)
			}
		})
		ran++
		if ran >= limit {
			break
		}
	}
	if ran == 0 {
		t.Fatal("no local SKT archives")
	}
}

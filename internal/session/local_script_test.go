package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"maps"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testlocal"
)

// Run one fresh installation session. Ticks in captures count completed 16 ms
// passes. Each keypad press is released before the following pass.
func runLocalWideStage(t *testing.T, saves string, inputs map[int]int32, captures ...int) map[int][]byte {
	t.Helper()
	path := testlocal.Value(t, "WFEATURE_SKT_WIDE_STAGE", "the word-script archive with the packaged stage save")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Start(t.Context(), data, Options{SaveStore: backend.NewDirectorySaveStore(saves)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	script := s.sktScript()
	if script == nil {
		t.Fatal("expected an SGS session")
	}
	keys := map[int]int32{800: -5, 1056: -5, 1127: -5, 1193: -5, 1259: -5, 1350: -5}
	maps.Copy(keys, inputs)
	frames := make(map[int][]byte, len(captures))
	for tick := range slices.Max(captures) {
		if key, ok := keys[tick-1]; ok {
			if err := s.SendKey(t.Context(), KeyRelease, key); err != nil {
				t.Fatal(err)
			}
		}
		if key, ok := keys[tick]; ok {
			if err := s.SendKey(t.Context(), KeyPress, key); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := script.Advance(t.Context(), 16*time.Millisecond); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if !slices.Contains(captures, tick+1) {
			continue
		}
		rgba, w, h, ok := s.Frame()
		if !ok || w != 240 || h != 320 || len(rgba) != 4*w*h {
			t.Fatalf("tick %d: missing handset frame", tick)
		}
		frames[tick+1] = rgba
	}
	return frames
}

func TestLocalWideScriptStage(t *testing.T) {
	idle := runLocalWideStage(t, t.TempDir(), nil, 1000, 1100, 1861)
	// Check each menu before advancing to the timed input.
	for tick, want := range map[int]string{
		1000: "db3047c02e2765b36d2a97231f9fa406e2b00064b0329c9b1c2b0adab6ba2646",
		1100: "3f50214591180cf3f8609ae76e659625ea81f48b256601bc94e4f782b1603f8d",
	} {
		header := binary.LittleEndian.AppendUint64(nil, 240)
		header = binary.LittleEndian.AppendUint64(header, 320)
		if got := fmt.Sprintf("%x", sha256.Sum256(append(header, idle[tick]...))); got != want {
			t.Fatalf("tick %d: menu digest %s", tick, got)
		}
	}
	input := runLocalWideStage(t, t.TempDir(), map[int]int32{1850: '5'}, 1861)
	if bytes.Equal(idle[1861], input[1861]) {
		t.Fatal("numeric input did not change the obstacle drawn in the stage")
	}
}

func TestLocalWideScriptStageProgress(t *testing.T) {
	// Select each drawing with the keypad as its corresponding obstacle
	// scrolls into view. OK on the results page advances to the next stage.
	inputs := map[int]int32{
		1871: '1', 1984: '3', 2097: '2', 2202: '2', 2307: '3', 2364: '1',
		2445: '1', 2486: '2', 2535: '5', 2640: '5', 2745: '5', 2858: '3',
		2899: '4', 3012: '4', 3085: '1', 3150: '5', 3191: '5', 3224: '4',
		3265: '2', 3330: '2', 3355: '2', 3428: '4', 3477: '4', 3526: '5',
		3695: '5', 3760: '5', 3785: '5', 3874: '2', 3923: '2', 3964: '4',
		3989: '4', 4650: -5,
	}
	saves := t.TempDir()
	cleared := runLocalWideStage(t, saves, inputs, 4650, 4780)
	reopened := runLocalWideStage(t, saves, nil, 1240, 1510)

	// Scores, audio indicators and character animation vary independently.
	// Compare the rendered headings for the result, map and resumed gameplay.
	const mapHeading = "9b573f109fdcae95227e4647ebe24d08c5ee26bc53b118a416c3219575d6615f"
	for _, check := range []struct {
		name   string
		frame  []byte
		region image.Rectangle
		want   string
	}{
		{"third stage result", cleared[4650], image.Rect(0, 0, 240, 40), "73afc7ebdd26c9d1cf52b4bde6db50cc94dba448b1b28c9694e635812a9cc24d"},
		{"fourth stage map", cleared[4780], image.Rect(0, 0, 65, 20), mapHeading},
		{"reopened map", reopened[1240], image.Rect(0, 0, 65, 20), mapHeading},
		{"fourth stage in play", reopened[1510], image.Rect(55, 118, 178, 157), "3b19f20e896f81781137f904351693a7a5ba617f178ddab0c9d74219a2a486f8"},
	} {
		hash := sha256.New()
		r := check.region
		for y := r.Min.Y; y < r.Max.Y; y++ {
			hash.Write(check.frame[4*(240*y+r.Min.X) : 4*(240*y+r.Max.X)])
		}
		if got := fmt.Sprintf("%x", hash.Sum(nil)); got != check.want {
			t.Errorf("%s: got %s, want %s", check.name, got, check.want)
		}
	}
}

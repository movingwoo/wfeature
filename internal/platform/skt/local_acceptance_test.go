package skt_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/skt"
)

// localAcceptanceTicks is far enough for a title to get past a vendor logo and
// paint something of its own. A tick here is real time — a MIDlet's threads
// sleep against the wall clock and there is no guest clock to multiply — so
// this is about five seconds per archive, and the archives run in parallel.
const localAcceptanceTicks = 300

// TestLocalSKTArchivesBootAndPaint is opt-in because real games are ignored
// local data rather than repository fixtures. The fixture JAR cannot find a
// wrong contract — it is written against what this runtime already believes —
// and the corpus is where every contract that was wrong here came from: a
// static method declared as an instance one, a resource name resolved through
// the wrong package, an image format missing half its header. This is what
// keeps the corrected ones from drifting back.
//
//	WFEATURE_SKT_ACCEPTANCE=1 go test -run TestLocalSKTArchivesBootAndPaint -v ./internal/platform/skt
func TestLocalSKTArchivesBootAndPaint(t *testing.T) {
	if os.Getenv("WFEATURE_SKT_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_SKT_ACCEPTANCE=1 to run ignored local SKT archives")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate SKT acceptance test source")
	}
	directory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "skt")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read local SKT game directory: %v", err)
	}
	ran := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				t.Fatalf("read archive: %v", err)
			}
			archive, err := skt.Open(data)
			if err != nil {
				t.Fatalf("open archive: %v", err)
			}
			framebuffer, err := backend.NewMemoryFramebuffer(240, 320)
			if err != nil {
				t.Fatalf("framebuffer: %v", err)
			}
			// Saves go to the test's own directory: a probe must not read or
			// write the progress a person made playing.
			session, err := skt.Start(archive, skt.Options{
				Framebuffer: framebuffer,
				SaveStore:   backend.NewDirectorySaveStore(t.TempDir()),
			})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			defer session.Destroy(true)

			startedAt := time.Now()
			for tick := 0; tick < localAcceptanceTicks; tick++ {
				state := session.State()
				if state == skt.StateDestroyed || state == skt.StateError {
					break
				}
				session.AdvanceAudio(time.Since(startedAt))
				if err := session.RunPending(); err != nil {
					t.Fatalf("tick %d: %v", tick, err)
				}
				time.Sleep(16 * time.Millisecond)
			}
			if state := session.State(); state == skt.StateError {
				t.Fatalf("the MIDlet ended in error: %v", session.Summary().Error)
			}

			frame, _ := framebuffer.Snapshot()
			lit := 0
			for offset := 0; offset+3 < len(frame.RGBA); offset += 4 {
				if frame.RGBA[offset] != 0 || frame.RGBA[offset+1] != 0 || frame.RGBA[offset+2] != 0 {
					lit++
				}
			}
			if lit == 0 {
				t.Fatal("the presented frame is entirely black")
			}
			t.Logf("lit=%d/%d", lit, frame.Width*frame.Height)
		})
		ran++
	}
	if ran == 0 {
		t.Skip("no local SKT archives")
	}
}

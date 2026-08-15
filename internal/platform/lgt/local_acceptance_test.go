package lgt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// localAcceptanceTicks is far enough for a title to get past its notice screen
// and paint something of its own. Reaching gameplay needs scripted input, which
// is what `wfeature runlgt -key` is for; this probe only asks whether every
// local archive still boots and draws.
const localAcceptanceTicks = 60

// TestLocalLGTArchivesBootAndPaint is opt-in because real games are ignored
// local data rather than repository fixtures, and it is the only test here that
// runs a real module. The fixture cannot find a wrong contract — every one that
// was wrong looked right until an archive asked — so this is what keeps the
// corrected ones from drifting back.
//
//	WFEATURE_LGT_ACCEPTANCE=1 go test -run TestLocalLGTArchivesBootAndPaint -v ./internal/platform/lgt
func TestLocalLGTArchivesBootAndPaint(t *testing.T) {
	if os.Getenv("WFEATURE_LGT_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_LGT_ACCEPTANCE=1 to run ignored local LGT archives")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate LGT acceptance test source")
	}
	directory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "lgt")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read local LGT game directory: %v", err)
	}
	ran := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				t.Fatalf("read archive: %v", err)
			}
			ctx := context.Background()
			session, err := StartSession(ctx, data, SessionOptions{
				// Saves go to the test's own directory: a probe must not read
				// or write the progress a person made playing.
				SaveRoot: t.TempDir(),
				TraceSVC: 16,
			})
			if err != nil {
				if errors.Is(err, ErrJavaAppUnsupported) {
					t.Skip("LGT Java app, which this platform does not support")
				}
				var failure *StartFailure
				if errors.As(err, &failure) && len(failure.Trace) > 0 {
					t.Fatalf("start: %v\n\n%s", err, FormatSVCTrace(failure.Trace))
				}
				t.Fatalf("start: %v", err)
			}
			defer session.Close(ctx)

			for tick := 0; tick < localAcceptanceTicks; tick++ {
				if err := session.Tick(ctx); err != nil {
					if errors.Is(err, ErrGuestExited) {
						break
					}
					t.Fatalf("tick %d: %v\n\n%s", tick, err, FormatSVCTrace(session.SVCTrace()))
				}
			}
			if session.Flushes() == 0 {
				t.Fatal("the title never asked to present a frame")
			}
			frame, width, height, _ := session.Frame()
			if width == 0 || height == 0 {
				t.Fatalf("frame is %dx%d", width, height)
			}
			lit := 0
			for offset := 0; offset+3 < len(frame); offset += 4 {
				if frame[offset] != 0 || frame[offset+1] != 0 || frame[offset+2] != 0 {
					lit++
				}
			}
			if lit == 0 {
				t.Fatal("the presented frame is entirely black")
			}
			t.Logf("flushes=%d lit=%d/%d", session.Flushes(), lit, width*height)
		})
		ran++
	}
	if ran == 0 {
		t.Skip("no local LGT archives")
	}
}

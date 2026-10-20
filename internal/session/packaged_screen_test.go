package session

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// A KTF start that names no screen runs on the handset the archive's
// descriptor declares, and Screen answers that before the title has drawn
// anything: a Host lays its picture out from it. This package's own 240x320
// default used to be passed down as if the Host had asked for it, which left
// the descriptor nothing to decide. A start that names a screen keeps it.
func TestKTFStartsOnTheHandsetItsDescriptorDeclares(t *testing.T) {
	archive, err := testfixture.KTFCheckpointArchiveDeclaring("DisplaySize:176*220\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		width, height int
		wantW, wantH  int
	}{
		{"no screen named", 0, 0, 176, 220},
		{"the default named", DefaultWidth, DefaultHeight, DefaultWidth, DefaultHeight},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := backend.NewMemorySaveStore(nil)
			if err != nil {
				t.Fatal(err)
			}
			running, err := Start(t.Context(), archive, Options{SaveStore: store, Width: test.width, Height: test.height})
			if err != nil {
				t.Fatal(err)
			}
			defer running.Close()
			if width, height := running.Screen(); width != test.wantW || height != test.wantH {
				t.Fatalf("started on %dx%d, want %dx%d", width, height, test.wantW, test.wantH)
			}
		})
	}
}

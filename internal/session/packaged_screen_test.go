package session

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/sgsvm"
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

// The summary says what a start naming no screen takes, which is what a Host
// shows beside its automatic choice: the declaration where the archive has
// one, and the default where it cannot say. It is the same answer the start
// itself reaches.
func TestInspectSaysTheScreenAStartNamingNoneTakes(t *testing.T) {
	for _, test := range []struct {
		name          string
		fields        string
		width, height int
	}{
		{"declared", "DisplaySize:176*220\nResizeType:0\n", 176, 220},
		{"unreadable", "DisplaySize:wide\n", DefaultWidth, DefaultHeight},
		{"left out of the current format", "ResizeType:1\n", DefaultWidth, DefaultHeight},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive, err := testfixture.KTFCheckpointArchiveDeclaring(test.fields)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := Inspect(archive)
			if err != nil {
				t.Fatal(err)
			}
			if summary.ScreenWidth != test.width || summary.ScreenHeight != test.height {
				t.Fatalf("summary says %dx%d, want %dx%d", summary.ScreenWidth, summary.ScreenHeight, test.width, test.height)
			}
			store, err := backend.NewMemorySaveStore(nil)
			if err != nil {
				t.Fatal(err)
			}
			running, err := Start(t.Context(), archive, Options{SaveStore: store})
			if err != nil {
				t.Fatal(err)
			}
			defer running.Close()
			if width, height := running.Screen(); width != summary.ScreenWidth || height != summary.ScreenHeight {
				t.Fatalf("started on %dx%d, but the summary says %dx%d", width, height, summary.ScreenWidth, summary.ScreenHeight)
			}
		})
	}
}

// SKT's answer is shared by the start and the summary, so the screen a page is
// told for its automatic choice is the one a start without a size builds: the
// resource names where they declare a handset, a script's own size, and the
// default where neither says.
func TestSKTScreenIsOneAnswerForTheStartAndTheSummary(t *testing.T) {
	small := &skt.Archive{Entries: map[string][]byte{
		"title/main_logo_176.png": nil, "title/gamevil_176.png": nil, "title/intro_176.dat": nil, "n.class": nil,
	}}
	plain := &skt.Archive{Entries: map[string][]byte{"bg/bg_00.png": nil, "n.class": nil}}
	script := &skt.Archive{Script: &sgsvm.Program{Width: 128, Height: 160}}
	for _, test := range []struct {
		name          string
		archive       *skt.Archive
		options       Options
		width, height int
	}{
		{"names declaring a handset", small, Options{}, 176, 208},
		{"names declaring a handset, size named", small, Options{Width: 240, Height: 320}, 240, 320},
		{"nothing declared", plain, Options{}, DefaultWidth, DefaultHeight},
		{"a script", script, Options{}, 128, 160},
	} {
		if width, height := sktScreen(test.archive, test.options); width != test.width || height != test.height {
			t.Errorf("%s: sktScreen = %dx%d, want %dx%d", test.name, width, height, test.width, test.height)
		}
	}
}

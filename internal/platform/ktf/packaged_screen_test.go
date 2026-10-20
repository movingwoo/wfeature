package ktf

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// The descriptor names the handset a title was packaged for, and that is the
// screen it starts on when nobody chose one. Both generations the carrier sold
// for are read as written; a descriptor from before the field existed is a
// 176x220 package; and a field this platform cannot read answers nothing
// rather than a number it guessed at.
func TestPackagedScreenReadsTheDescriptor(t *testing.T) {
	for _, test := range []struct {
		name          string
		properties    map[string]string
		width, height int
		ok            bool
	}{
		{"the smaller generation", map[string]string{"DISPLAYSIZE": "176*220"}, 176, 220, true},
		{"the larger generation", map[string]string{"DISPLAYSIZE": "240*320"}, 240, 320, true},
		{"a taller handset", map[string]string{"DISPLAYSIZE": "240*400"}, 240, 400, true},
		{"an earlier descriptor", map[string]string{"AID": "fixture"}, 176, 220, true},
		{"no properties at all", nil, 176, 220, true},
		{"an empty field", map[string]string{"DISPLAYSIZE": ""}, 0, 0, false},
		{"another separator", map[string]string{"DISPLAYSIZE": "176x220"}, 0, 0, false},
		{"one side", map[string]string{"DISPLAYSIZE": "176*"}, 0, 0, false},
		{"a sign", map[string]string{"DISPLAYSIZE": "+176*220"}, 0, 0, false},
		{"a space", map[string]string{"DISPLAYSIZE": "176 * 220"}, 0, 0, false},
		{"a third side", map[string]string{"DISPLAYSIZE": "176*220*16"}, 0, 0, false},
		{"below any handset", map[string]string{"DISPLAYSIZE": "16*220"}, 0, 0, false},
		{"above any handset", map[string]string{"DISPLAYSIZE": "2048*220"}, 0, 0, false},
		{"a long number", map[string]string{"DISPLAYSIZE": "0000176*220"}, 0, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			width, height, ok := PackagedScreen(Descriptor{Properties: test.properties})
			if width != test.width || height != test.height || ok != test.ok {
				t.Fatalf("PackagedScreen = %dx%d %v, want %dx%d %v", width, height, ok, test.width, test.height, test.ok)
			}
		})
	}
}

// A Host that names a screen keeps it: the page's setting and -screen are how a
// player overrides a declaration, so the descriptor only decides what "no
// answer" means.
func TestSessionScreenIsTheHostsWhenItNamesOne(t *testing.T) {
	declared := Descriptor{Properties: map[string]string{"DISPLAYSIZE": "176*220"}}
	if width, height := sessionScreen(declared, SessionOptions{Width: 240, Height: 320}); width != 240 || height != 320 {
		t.Fatalf("a named screen became %dx%d", width, height)
	}
	if width, height := sessionScreen(declared, SessionOptions{}); width != 176 || height != 220 {
		t.Fatalf("no screen named became %dx%d, want the declared 176x220", width, height)
	}
	unreadable := Descriptor{Properties: map[string]string{"DISPLAYSIZE": "wide"}}
	if width, height := sessionScreen(unreadable, SessionOptions{}); width != 0 || height != 0 {
		t.Fatalf("an unreadable declaration became %dx%d, want the platform's own", width, height)
	}
}

// The declaration reaches a started title, and what the session reports is the
// screen it took rather than the Host's default: a page lays its canvas out from
// that answer before the first picture arrives.
func TestAStartedTitleRunsOnTheHandsetItDeclares(t *testing.T) {
	for _, test := range []struct {
		name          string
		fields        string
		options       SessionOptions
		width, height int
	}{
		{"declared smaller", "DisplaySize:176*220\n", SessionOptions{}, 176, 220},
		{"declared larger", "DisplaySize:240*320\n", SessionOptions{}, 240, 320},
		{"an earlier descriptor", "", SessionOptions{}, 176, 220},
		{"unreadable", "DisplaySize:wide\n", SessionOptions{}, runtimeDisplayPixelWidth, runtimeDisplayPixelHeight},
		{"named by the Host", "DisplaySize:176*220\n", SessionOptions{Width: 240, Height: 400}, 240, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive, err := testfixture.KTFCheckpointArchiveDeclaring(test.fields)
			if err != nil {
				t.Fatal(err)
			}
			store, err := backend.NewMemorySaveStore(nil)
			if err != nil {
				t.Fatal(err)
			}
			test.options.SaveStore = store
			session, err := StartSession(t.Context(), archive, test.options)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if width, height := session.Screen(); width != test.width || height != test.height {
				t.Fatalf("started on %dx%d, want %dx%d", width, height, test.width, test.height)
			}
		})
	}
}

// A quick save remembers the screen its title was running on, and a load puts
// that one back whatever the descriptor says now. A slot taken before
// declarations were read holds the platform's own 240x320, and a title
// restored into a 176x220 screen with memory laid out for 240x320 would draw
// every row at the wrong offset.
func TestARestoredClientKeepsTheScreenItWasSavedOn(t *testing.T) {
	source := newContinuationFixture(t, 1700000000, continuationFixtureOptions{Runnable: true})
	source.start(t)
	saved := captureClientForTest(t, source.client)
	adapters, _, err := captureSaveAdapters(source.client.saveStore)
	if err != nil {
		t.Fatal(err)
	}
	source.client.StopThreads()
	if saved.ScreenWidth != 0 || saved.ScreenHeight != 0 {
		t.Fatalf("the fixture saved %dx%d, want the platform's own", saved.ScreenWidth, saved.ScreenHeight)
	}
	archive := &Archive{Descriptor: Descriptor{AID: "ABCDEF12", Properties: map[string]string{"DISPLAYSIZE": "176*220"}},
		JAR: &JAR{Client: source.client.image}}
	restored, err := restoreSessionClient(archive, saved, adapters, SessionOptions{MaxSteps: saved.Core.MaxSteps,
		Clock: NewManualClock(time.Unix(1800000000, 0)), SaveStore: make(memorySaveStore)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.StopThreads)
	if width, height := restored.Screen(); width != runtimeDisplayPixelWidth || height != runtimeDisplayPixelHeight {
		t.Fatalf("restored onto %dx%d, want the %dx%d it was saved on", width, height,
			runtimeDisplayPixelWidth, runtimeDisplayPixelHeight)
	}
}

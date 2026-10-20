package ktf

import (
	"fmt"
	"strings"
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
		{"the current format without the field", map[string]string{"RESIZETYPE": "1"}, 0, 0, false},
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

// A descriptor whose fields ran past what the parser keeps may have had its
// DisplaySize dropped, so the missing field is not read as the earlier format:
// that would shrink a current package to 176x220 because of where its field
// happened to sit. Parsing keeps the fields it always kept.
func TestADescriptorCutShortSaysNothingAboutItsHandset(t *testing.T) {
	var text strings.Builder
	text.WriteString("AID:fixture\nPID:P0001\nMClass:fixture.Main\n")
	for index := 0; index < maxDescriptorFields; index++ {
		fmt.Fprintf(&text, "Field%d:value\n", index)
	}
	text.WriteString("DisplaySize:240*320\n")
	descriptor, err := ParseDescriptor([]byte(text.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !descriptor.Truncated || len(descriptor.Properties) != maxDescriptorFields {
		t.Fatalf("parsed %d fields, truncated=%v; want %d and true", len(descriptor.Properties), descriptor.Truncated, maxDescriptorFields)
	}
	if _, kept := descriptor.Properties["DISPLAYSIZE"]; kept {
		t.Fatal("the field past the limit was kept")
	}
	if width, height, ok := PackagedScreen(descriptor); ok {
		t.Fatalf("a descriptor cut short answered %dx%d", width, height)
	}
	if width, height := AutomaticScreen(descriptor); width != runtimeDisplayPixelWidth || height != runtimeDisplayPixelHeight {
		t.Fatalf("a descriptor cut short starts on %dx%d, want the platform's own", width, height)
	}

	whole, err := ParseDescriptor([]byte("AID:fixture\nPID:P0001\nMClass:fixture.Main\nAID2:x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if whole.Truncated {
		t.Fatal("a short descriptor reads as cut short")
	}
}

// The screen a start naming none takes is what a Host shows for its automatic
// choice, so it is the declaration where there is one and the platform's own
// where there is not — never a zero a page would have to guess around.
func TestAutomaticScreenIsTheDeclarationOrThePlatforms(t *testing.T) {
	for _, test := range []struct {
		name          string
		properties    map[string]string
		width, height int
	}{
		{"declared", map[string]string{"DISPLAYSIZE": "176*220", "RESIZETYPE": "0"}, 176, 220},
		{"an earlier descriptor", map[string]string{"AID": "fixture"}, 176, 220},
		{"unreadable", map[string]string{"DISPLAYSIZE": "wide"}, runtimeDisplayPixelWidth, runtimeDisplayPixelHeight},
		{"left out", map[string]string{"RESIZETYPE": "1"}, runtimeDisplayPixelWidth, runtimeDisplayPixelHeight},
	} {
		if width, height := AutomaticScreen(Descriptor{Properties: test.properties}); width != test.width || height != test.height {
			t.Errorf("%s: AutomaticScreen = %dx%d, want %dx%d", test.name, width, height, test.width, test.height)
		}
	}
}

// A Host that names a screen keeps it: the page's setting and -screen are how a
// player overrides a declaration, so the descriptor only decides what "no
// answer" means.
func TestSessionScreenIsTheHostsWhenItNamesOne(t *testing.T) {
	declared := Descriptor{Properties: map[string]string{"DISPLAYSIZE": "176*220"}}
	if width, height, fromDescriptor := sessionScreen(declared, SessionOptions{Width: 240, Height: 320}); width != 240 || height != 320 || fromDescriptor {
		t.Fatalf("a named screen became %dx%d (descriptor %v)", width, height, fromDescriptor)
	}
	if width, height, fromDescriptor := sessionScreen(declared, SessionOptions{}); width != 176 || height != 220 || !fromDescriptor {
		t.Fatalf("no screen named became %dx%d (descriptor %v), want the declared 176x220", width, height, fromDescriptor)
	}
	unreadable := Descriptor{Properties: map[string]string{"DISPLAYSIZE": "wide"}}
	if width, height, fromDescriptor := sessionScreen(unreadable, SessionOptions{}); width != 0 || height != 0 || fromDescriptor {
		t.Fatalf("an unreadable declaration became %dx%d (descriptor %v), want the platform's own", width, height, fromDescriptor)
	}
}

// Half a handset, or a negative one, names nothing: passing it through ran the
// title on the platform's own screen while the client kept the unusable pair,
// and every quick save then refused it. The descriptor decides instead, and the
// session that results can be saved.
func TestAScreenThatIsNotTwoSidesNamesNothing(t *testing.T) {
	declared := Descriptor{Properties: map[string]string{"DISPLAYSIZE": "176*220"}}
	for _, named := range []struct{ width, height int }{{176, 0}, {0, 220}, {-1, -1}, {240, -320}} {
		width, height, fromDescriptor := sessionScreen(declared, SessionOptions{Width: named.width, Height: named.height})
		if width != 176 || height != 220 || !fromDescriptor {
			t.Errorf("%dx%d became %dx%d (descriptor %v), want the declared 176x220", named.width, named.height, width, height, fromDescriptor)
		}
		client := &Client{}
		client.SetScreen(named.width, named.height)
		if client.screenWidth != 0 || client.screenHeight != 0 {
			t.Errorf("SetScreen(%d, %d) kept %dx%d, want nothing named", named.width, named.height, client.screenWidth, client.screenHeight)
		}
	}

	archive, err := testfixture.KTFCheckpointArchiveDeclaring("DisplaySize:176*220\n")
	if err != nil {
		t.Fatal(err)
	}
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := StartSession(t.Context(), archive, SessionOptions{SaveStore: store, Width: 176})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if width, height := session.Screen(); width != 176 || height != 220 {
		t.Fatalf("started on %dx%d, want the declared 176x220", width, height)
	}
	if _, err := session.CaptureCheckpoint(t.Context()); err != nil {
		t.Fatalf("a session started with half a screen cannot be saved: %v", err)
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

package lgt

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// A text component holds what it was built with, and the calls that edit it are
// the half a title can use without a widget being drawn: the handset's own
// input method drives them there, and a title that edits its own box drives
// them here.
// The insert half takes a guest char array and is covered on the other
// platform, where an array is a Go object; what is pinned here is the delete
// and the limit, which are the two a range can be wrong in.
func TestTextComponentHoldsAndEditsItsText(t *testing.T) {
	client := fixtureClient(t)
	const component = 0x1000
	state := client.javaWidgetState(component)
	state.text = "abcd"

	state.maxLength = 0
	if _, err := javaTextComponentDelete(client, t.Context(), nil, []uint32{component, 1, 2}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if state.text != "ad" {
		t.Fatalf("after delete(1, 2) the text is %q, want %q", state.text, "ad")
	}
	// A range past the end is clipped rather than refused: the position was
	// computed against a caret this platform does not move.
	if _, err := javaTextComponentDelete(client, t.Context(), nil, []uint32{component, 1, 99}); err != nil {
		t.Fatalf("delete past the end: %v", err)
	}
	if state.text != "a" {
		t.Fatalf("after a clipped delete the text is %q, want %q", state.text, "a")
	}

	// The limit is the one the component was given, and it clips an insert
	// rather than growing past it.
	state.text, state.maxLength = "abc", 4
	if _, err := javaTextComponentSetMaxLength(client, t.Context(), nil, []uint32{component, 4}); err != nil {
		t.Fatal(err)
	}
	if state.maxLength != 4 {
		t.Fatalf("the limit is %d, want 4", state.maxLength)
	}
}

// The automaton a text component owns is the one the title takes off the
// component rather than constructing, so the object word the module was told
// `imHandler` sits in has to name it. A zero there is a NullPointerException in
// the title's own code with nothing here to blame.
func TestATextComponentIsBuiltWithAnInputHandlerInItsField(t *testing.T) {
	client := fixtureClient(t)
	layout := &javaLayout{classes: map[string]*javaLayoutClass{}}
	client.javaLink = &javaLink{layout: layout, surface: &javaSurface{}}
	// The module asked for the field, which is what makes the word one this
	// platform has to fill.
	textComponent := layout.class(javaTextComponentClass)
	textComponent.Fields[javaMemberKey(javaMemberRef{
		Name: "imHandler", Descriptor: "Lorg/kwis/msp/lcdui/InputMethodHandler;"})] = 4
	textComponent.InstanceWords = 5

	class, err := client.preparePlatformJavaClass(javaTextComponentClass)
	if err != nil {
		t.Fatal(err)
	}
	if class.Instance < 5 {
		t.Fatalf("a component's block is %d words, too short for the field at 4", class.Instance)
	}
	component, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.attachInputMethodHandler(component, 3); err != nil {
		t.Fatalf("attachInputMethodHandler: %v", err)
	}
	block, err := client.readWord(component + 8)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := client.readWord(block + 4*4)
	if err != nil {
		t.Fatal(err)
	}
	if handler == 0 {
		t.Fatal("the imHandler word is still null")
	}
	if mode := client.javaWidgetState(handler).mode; mode != 3 {
		t.Fatalf("the handler was built for mode %d, want the component's constraint 3", mode)
	}
	// The listener a title registers on it is kept: a handler that cannot be
	// given one has told the title its own input will never work.
	if _, err := javaInputMethodSetListener(client, t.Context(), nil, []uint32{handler, 0x2000}); err != nil {
		t.Fatal(err)
	}
	if got := client.javaWidgetState(handler).listener; got != 0x2000 {
		t.Fatalf("the handler kept %#x, want the listener it was given", got)
	}
}

// A number read out of text, and the exception the language names for text that
// is not one. A title that guards its own parse with a catch has to be given a
// throw rather than a platform failure.
func TestParsingANumberThrowsWhatTheLanguageNames(t *testing.T) {
	client := fixtureClient(t)
	thread := armcore.NewThread(armcore.NewContext())
	text, err := client.newJavaString("-42")
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.javaParseNumber(thread, text, 10, 32)
	if err != nil {
		t.Fatalf("parseInt(\"-42\"): %v", err)
	}
	if int32(value) != -42 {
		t.Fatalf("parseInt(\"-42\") = %d", int32(value))
	}

	bad, err := client.newJavaString("twelve")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.javaParseNumber(thread, bad, 10, 32)
	var throw *javaUncaughtThrow
	if !errors.As(err, &throw) || throw.Class != javaNumberFormatClass {
		t.Fatalf("parsing %q answered %v, want a NumberFormatException", "twelve", err)
	}
	// A byte is the same parse at a different width, and a value that does not
	// fit is not a byte.
	overflow, err := client.newJavaString("300")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.javaParseNumber(thread, overflow, 10, 8); !errors.As(err, &throw) {
		t.Fatalf("parseByte(\"300\") answered %v, want a NumberFormatException", err)
	}
}

// A file written and never closed still has to arrive. One title writes the
// byte that says its first run is over and calls MC_knlExit on the next line;
// with the write only in a buffer its first-run notice came back for ever.
func TestAWrittenFileSurvivesAnEndingWithoutAClose(t *testing.T) {
	root := t.TempDir()
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{
		Width: 16, Height: 8,
		SaveStore: backend.NewDirectorySaveStore(filepath.Join(root, "saves")),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.files = map[uint32]*openFile{
		1: {name: "startup.dat", data: []byte{1}, writable: true, dirty: true},
		2: {name: "clean.dat", data: []byte{2}, writable: true},
	}
	client.flushOpenFiles()

	if data, ok := client.readFile("startup.dat"); !ok || len(data) != 1 || data[0] != 1 {
		t.Fatalf("the dirty file came back %v ok=%t, want the byte it was written", data, ok)
	}
	// A file nothing wrote to is not rewritten: the flush is for writes that
	// have nowhere else to go.
	if _, ok := client.readFile("clean.dat"); ok {
		t.Fatal("a file that was never written was persisted anyway")
	}
	// Flushing twice writes once.
	if client.files[1].dirty {
		t.Fatal("the flushed file is still marked dirty")
	}
}

// Two Images of one name share the pixels, because a picture loaded from a
// resource is immutable and decoding a second set costs a surface nothing here
// reclaims. **Asking to draw into one ends that** — the sharing is an
// optimisation, and a title that draws into a named picture must not change
// what the next holder of that name loaded.
func TestANamedPictureIsSharedUntilSomethingDrawsIntoIt(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	surface, err := client.newFramebuffer(4, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	surface.pixels[0] = 0x1234
	runtime.decodedImages = map[string]uint32{"name:/a.png": surface.handle}

	first, err := client.newJavaImageOn(surface.handle)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.newJavaImageOn(surface.handle)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two loads of one name answered the same object")
	}
	if runtime.images[first] != runtime.images[second] {
		t.Fatal("two loads of one name did not share the surface")
	}

	drawn, err := client.unshareDecodedSurface(first, surface)
	if err != nil {
		t.Fatalf("unshareDecodedSurface: %v", err)
	}
	if drawn.handle == surface.handle {
		t.Fatal("drawing into a named picture kept the shared surface")
	}
	if drawn.pixels[0] != 0x1234 {
		t.Fatalf("the private copy holds %#x, want the picture that was loaded", drawn.pixels[0])
	}
	drawn.pixels[0] = 0x4321
	if surface.pixels[0] != 0x1234 {
		t.Fatal("drawing into the copy changed the picture the name still hands out")
	}
	if runtime.images[second] != surface.handle {
		t.Fatal("the other holder lost the picture it loaded")
	}
	// A surface the cache is not handing out is left alone: the copy is for
	// the sharing and nothing else.
	own, err := client.newFramebuffer(2, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if same, err := client.unshareDecodedSurface(second, own); err != nil || same != own {
		t.Fatalf("an unshared surface was copied anyway: %v %v", same, err)
	}
}

// The byte form gets the same rule under a different key: a title that decodes
// one picture's bytes twice pays for one surface, and a title that then draws
// into one of them still gets its own copy.
func TestTheSameBytesDecodeIntoOneSurface(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	encoded := encodedTestImage(t, 4, 4)

	key := imageDigestKey(encoded)
	first, err := client.newSharedJavaImage(key, encoded)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.newSharedJavaImage(key, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two decodes of one picture answered the same object")
	}
	if runtime.images[first] != runtime.images[second] {
		t.Fatal("two decodes of one picture did not share the surface")
	}
	if got := len(runtime.decodedImages); got != 1 {
		t.Fatalf("the decode cache holds %d entries, want 1", got)
	}

	// Different bytes are a different picture and cost their own surface. The
	// key is what decides, so the same pixels under another digest are two.
	other := append(append([]byte(nil), encoded...), 0)
	third, err := client.newSharedJavaImage(imageDigestKey(other), encoded)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.images[third] == runtime.images[first] {
		t.Fatal("a second picture was handed the first one's surface")
	}

	shared := client.framebuffers[runtime.images[first]]
	if shared == nil {
		t.Fatal("the shared surface is not in the table")
	}
	drawn, err := client.unshareDecodedSurface(first, shared)
	if err != nil {
		t.Fatal(err)
	}
	if drawn.handle == shared.handle {
		t.Fatal("drawing into a decoded picture kept the shared surface")
	}
	if runtime.images[second] != shared.handle {
		t.Fatal("the other holder lost the picture it decoded")
	}
}

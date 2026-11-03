package skt

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The rule has to fire on the shape the local small-screen archive has — three
// resources whose names end in the width it was built for, and no variant for
// any other — and stay quiet on everything else, because every other local
// archive runs on the default and a wrong answer here would move it off.
func TestPackagedScreenReadsAWidthOnlyWhenTheNamesAgree(t *testing.T) {
	for _, want := range []struct {
		name    string
		entries []string
		width   int
		height  int
		ok      bool
	}{
		{
			name:    "a title packaged for the smaller handset",
			entries: []string{"title/main_logo_176.png", "title/gamevil_176.png", "title/intro_176.dat", "bg/bg_00.png", "eff/eff_exorcist_1.dat", "n.class"},
			width:   176, height: 208, ok: true,
		},
		{
			name:    "a title that declares the width as a directory",
			entries: []string{"img_176/hero.png", "img_176/room.png", "img_176/table0.png", "data/t1", "snd/s0.mmf", "Game.class"},
			width:   176, height: 208, ok: true,
		},
		{
			name:    "one file under a width-suffixed directory is still one",
			entries: []string{"img_176/only.png", "data/t1", "Game.class"},
		},
		{
			name:    "a directory disagreeing with a name is not a declaration",
			entries: []string{"img_176/logo.png", "img_176/menu.png", "title/splash_240.png", "title/logo_240.png"},
		},
		{
			name:    "an archive with no width in any name",
			entries: []string{"bg/bg_10_.png", "eff/eff_dem0.dat", "menu/main_frame.png", "d.class"},
		},
		{
			name:    "two widths disagreeing is not a declaration",
			entries: []string{"title/logo_176.png", "title/logo_176_a.png", "title/logo_240.png", "title/splash_240.png"},
		},
		{
			name:    "one name on its own is a coincidence",
			entries: []string{"tiles/tiles_320.png", "bg/bg_00.png"},
		},
		{
			name:    "a width no handset here offers is read and ignored",
			entries: []string{"title/main_logo_120.png", "title/intro_120.dat"},
		},
		{
			name:    "a class file never counts",
			entries: []string{"pack_176.class", "other_176.class"},
		},
	} {
		t.Run(want.name, func(t *testing.T) {
			entries := make(map[string][]byte, len(want.entries))
			for _, name := range want.entries {
				entries[name] = nil
			}
			width, height, ok := packagedScreenFromNames(entries)
			if ok != want.ok || width != want.width || height != want.height {
				t.Fatalf("packagedScreenFromNames() = %d, %d, %v, want %d, %d, %v", width, height, ok, want.width, want.height, want.ok)
			}
		})
	}
}

func TestPackagedScreenAnswersNothingForNoArchive(t *testing.T) {
	if _, _, ok := PackagedScreen(nil); ok {
		t.Fatal("PackagedScreen(nil) claimed a screen")
	}
}

// A title's own thread rebuilding a List or a ChoiceGroup while the player
// presses keys and the Host paints. Under -race this fails when the guest's
// edits and the Host's reads do not share a lock; without the detector it
// still fails when a stale length is used to index a shorter slice.
func TestScreenEditsFromAGuestThreadDoNotRaceTheHost(t *testing.T) {
	runtime := startUIFixture(t)
	menu := fixtureStatic(t, runtime, "menu", "Ljavax/microedition/lcdui/List;")
	group := fixtureStatic(t, runtime, "group", "Ljavax/microedition/lcdui/ChoiceGroup;")

	for _, target := range []struct {
		name   string
		object *jvm.Object
		show   string
		keys   []int32
	}{
		{name: "List", object: menu, show: "showMenu", keys: []int32{KeyCodeDown, KeyCodeDown, KeyCodeFire, KeyCodeUp}},
		{name: "ChoiceGroup", object: group, show: "showForm", keys: []int32{KeyCodeDown, KeyCodeRight, KeyCodeFire, KeyCodeLeft, KeyCodeUp}},
	} {
		invokeFixtureVoid(t, runtime, "UIMIDlet", target.show)
		var guest sync.WaitGroup
		guest.Add(1)
		failures := make(chan error, 1)
		// The title's thread calls the natives the way its bytecode does,
		// through the classes they are registered on.
		go func() {
			defer guest.Done()
			for round := 0; round < 200; round++ {
				if _, err := runtime.VM.InvokeVirtual(target.object, "deleteAll", "()V"); err != nil {
					failures <- err
					return
				}
				for row := 0; row < 3; row++ {
					if _, err := runtime.VM.InvokeVirtual(target.object, "append", "(Ljava/lang/String;Ljavax/microedition/lcdui/Image;)I",
						jvm.ReferenceValue(runtime.VM.NewString("row")), jvm.ReferenceValue(nil)); err != nil {
						failures <- err
						return
					}
				}
			}
		}()
		for round := 0; round < 200; round++ {
			pressKeySafely(t, runtime, target.keys[round%len(target.keys)])
		}
		guest.Wait()
		select {
		case err := <-failures:
			t.Fatalf("%s edit error = %v", target.name, err)
		default:
		}
	}
}

// A Ticker's text is screen content the Host paints, so its natives wait for
// the lock the painter holds, and a title's thread changing it while the
// screen is painted is no race.
func TestTickerNativesTakeTheScreenLock(t *testing.T) {
	runtime := startUIFixture(t)
	invokeFixtureVoid(t, runtime, "UIMIDlet", "showMenu")
	menu := fixtureStatic(t, runtime, "menu", "Ljavax/microedition/lcdui/List;")
	value, err := runtime.VM.InvokeVirtual(menu, "getTicker", "()Ljavax/microedition/lcdui/Ticker;")
	if err != nil {
		t.Fatal(err)
	}
	ticker, err := value.Reference()
	if err != nil || ticker == nil {
		t.Fatalf("getTicker() = %v, %v", ticker, err)
	}
	text := func(value string) jvm.Value { return jvm.ReferenceValue(runtime.VM.NewString(value)) }
	for _, call := range []struct {
		name string
		run  func() error
	}{
		{"new Ticker", func() error {
			_, err := runtime.VM.NewObject(midp.TickerClass, "(Ljava/lang/String;)V", text("new"))
			return err
		}},
		{"getString", func() error {
			_, err := runtime.VM.InvokeVirtual(ticker, "getString", "()Ljava/lang/String;")
			return err
		}},
		{"setString", func() error {
			_, err := runtime.VM.InvokeVirtual(ticker, "setString", "(Ljava/lang/String;)V", text("set"))
			return err
		}},
	} {
		runtime.textMu.Lock()
		done := make(chan error, 1)
		go func() { done <- call.run() }()
		select {
		case err := <-done:
			runtime.textMu.Unlock()
			t.Fatalf("%s returned (%v) while the screen lock was held", call.name, err)
		case <-time.After(50 * time.Millisecond):
		}
		runtime.textMu.Unlock()
		if err := <-done; err != nil {
			t.Fatalf("%s error = %v", call.name, err)
		}
	}

	var guest sync.WaitGroup
	guest.Add(1)
	failures := make(chan error, 1)
	go func() {
		defer guest.Done()
		for round := 0; round < 200; round++ {
			if _, err := runtime.VM.InvokeVirtual(ticker, "setString", "(Ljava/lang/String;)V", text(strings.Repeat("t", round%7))); err != nil {
				failures <- err
				return
			}
		}
	}()
	for round := 0; round < 200; round++ {
		if err := runtime.RunPending(); err != nil {
			t.Fatalf("RunPending() error = %v", err)
		}
	}
	guest.Wait()
	select {
	case err := <-failures:
		t.Fatalf("setString error = %v", err)
	default:
	}
}

// Splitting an over-long word or trimming an over-long title costs time in
// proportion to the text, not to its square or cube.
func TestScreenTextLayoutIsLinearInTheText(t *testing.T) {
	font := &fontData{fontKey: fontKey{}, scale: 1, height: 10, baseline: 8}
	for name, call := range map[string]func(){
		"wrapText":    func() { wrapText(font, strings.Repeat("가", 6000), 40) },
		"trimToWidth": func() { trimToWidth(font, strings.Repeat("a", 50000), 100) },
	} {
		done := make(chan struct{})
		go func() { call(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s on a long unbroken text did not finish", name)
		}
	}
}

// The linear layout breaks and trims exactly where the re-measuring one did.
func TestScreenTextLayoutMatchesTheRemeasuringOne(t *testing.T) {
	fonts := []*fontData{
		{fontKey: fontKey{}, scale: 1, height: 10, baseline: 8},
		{fontKey: fontKey{style: fontBold}, scale: 2, height: 20, baseline: 16},
		{fontKey: fontKey{face: fontMonospace}, scale: 1, height: 10, baseline: 8},
	}
	alphabet := []rune("aWi.가 나\t\n")
	random := rand.New(rand.NewSource(11))
	for round := 0; round < 2000; round++ {
		font := fonts[round%len(fonts)]
		text := make([]rune, random.Intn(40))
		for index := range text {
			text[index] = alphabet[random.Intn(len(alphabet))]
		}
		width := random.Intn(90) - 5
		if got, want := wrapText(font, string(text), width), referenceWrapText(font, string(text), width); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("wrapText(%q, %d) = %q, want %q", string(text), width, got, want)
		}
		if got, want := trimToWidth(font, string(text), width), referenceTrimToWidth(font, string(text), width); got != want {
			t.Fatalf("trimToWidth(%q, %d) = %q, want %q", string(text), width, got, want)
		}
	}
}

func referenceTrimToWidth(font *fontData, text string, width int) string {
	if font == nil || width <= 0 {
		return text
	}
	runes := []rune(text)
	if font.textWidth(runes) <= width {
		return text
	}
	ellipsis := []rune("…")
	for length := len(runes) - 1; length > 0; length-- {
		if font.textWidth(append(append([]rune(nil), runes[:length]...), ellipsis...)) <= width {
			return string(runes[:length]) + "…"
		}
	}
	return ""
}

func referenceWrapText(font *fontData, text string, width int) []string {
	if text == "" {
		return nil
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		current := ""
		flush := func() {
			lines = append(lines, current)
			current = ""
		}
		for _, word := range strings.Split(paragraph, " ") {
			candidate := word
			if current != "" {
				candidate = current + " " + word
			}
			if font == nil || font.textWidth([]rune(candidate)) <= width {
				current = candidate
				continue
			}
			if current != "" {
				flush()
			}
			remaining := []rune(word)
			for len(remaining) > 0 {
				cut := len(remaining)
				for cut > 1 && font.textWidth(remaining[:cut]) > width {
					cut--
				}
				lines = append(lines, string(remaining[:cut]))
				remaining = remaining[cut:]
			}
			if len(lines) > 0 {
				current = lines[len(lines)-1]
				lines = lines[:len(lines)-1]
			}
		}
		if current != "" {
			flush()
		}
	}
	return lines
}

func fixtureStatic(t *testing.T, runtime *Runtime, name, descriptor string) *jvm.Object {
	t.Helper()
	value, err := runtime.VM.StaticField("UIMIDlet", name, descriptor)
	if err != nil {
		t.Fatalf("UIMIDlet.%s error = %v", name, err)
	}
	object, err := value.Reference()
	if err != nil || object == nil {
		t.Fatalf("UIMIDlet.%s = %v, %v", name, object, err)
	}
	return object
}

func invokeObjectVoid(t *testing.T, runtime *Runtime, object *jvm.Object, method, descriptor string, arguments ...jvm.Value) {
	t.Helper()
	if _, err := runtime.VM.InvokeVirtual(object, method, descriptor, arguments...); err != nil {
		t.Fatalf("%s.%s%s error = %v", object.ClassName, method, descriptor, err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
}

func invokeObjectInt(t *testing.T, runtime *Runtime, object *jvm.Object, method, descriptor string, arguments ...jvm.Value) int32 {
	t.Helper()
	value, err := runtime.VM.InvokeVirtual(object, method, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s.%s%s error = %v", object.ClassName, method, descriptor, err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	number, _ := value.Int32()
	return number
}

// pressKeySafely is pressKey that turns a Host panic into a test failure, so
// one broken press reports itself rather than ending the test binary.
func pressKeySafely(t *testing.T, runtime *Runtime, keyCode int32) {
	t.Helper()
	failure := func() (failure any) {
		defer func() { failure = recover() }()
		if err := runtime.SendKey(KeyPressed, keyCode); err != nil {
			return err
		}
		if err := runtime.RunPending(); err != nil {
			return err
		}
		return nil
	}()
	if failure != nil {
		t.Fatalf("key %d: %v", keyCode, failure)
	}
}

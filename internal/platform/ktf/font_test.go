package ktf

import (
	"context"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// One face for the whole library. The screen size the descriptor declares
// looked like it should choose between two — half these archives say 176x220
// and half 240x320 — but a 240x320 title laying a save slot out as a label and
// a number in columns runs the two together and out of its box when its text
// is drawn at the larger size, and is correct at the smaller. So the metrics
// are the same whatever the archive says its screen was.
func TestEveryGameGetsTheHandsetFace(t *testing.T) {
	for _, declared := range []string{"176*220", "240*320", "", "240x320", "wide*tall"} {
		client := &Client{appProperties: map[string]string{"DISPLAYSIZE": declared}}
		runtime := &initializationRuntime{client: client}
		if got := runtime.fontHeight(); got != 11 {
			t.Fatalf("DisplaySize %q gave Font.getHeight %d, want 11", declared, got)
		}
		if got := runtime.fontBaseline(); got != 8 {
			t.Fatalf("DisplaySize %q gave Font.getBaselinePosition %d, want 8", declared, got)
		}
	}
}

// Font.getHeight and Font.getBaselinePosition are what a guest lays its menus
// out with, so they have to answer from the face the renderer actually draws
// with rather than from a constant beside it.
func TestFontMetricsComeFromTheFace(t *testing.T) {
	runtime := &initializationRuntime{}
	height, err := runtimeFontHeight(runtime, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := runtimeFontBaseline(runtime, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := height.Int32(); value != runtime.fontHeight() {
		t.Fatalf("Font.getHeight = %d, want the face's %d", value, runtime.fontHeight())
	}
	if value, _ := baseline.Int32(); value != runtime.fontBaseline() {
		t.Fatalf("Font.getBaselinePosition = %d, want the face's %d", value, runtime.fontBaseline())
	}
}

// The WIPI-C side asks the same three questions through the graphics table,
// and the answer must not depend on the handle it is holding.
//
// MC_grpGetFont takes a size that is an identifier and not a measurement:
// MC_GRP_FT_SIZE_SMALL is 8, MEDIUM is 0 and LARGE is 16. Answering
// MC_grpGetFontHeight with the handle read those constants as pixel counts, so
// a title that asked for the small font was told its line was eight rows tall,
// laid every menu entry out as an eight-row band, clipped its own text to it —
// and lost the top row of every Korean syllable the eleven-row face drew.
func TestWIPICFontMetricsIgnoreTheHandle(t *testing.T) {
	client, runtime := newTestRuntime(t)
	runtime.currentThread, runtime.currentContext = client.thread, context.Background()

	call := func(function uint32, arguments ...uint32) uint32 {
		t.Helper()
		thread := armcore.NewThread(armcore.NewContext())
		for index, value := range arguments {
			if err := thread.SetRegister(index, value); err != nil {
				t.Fatal(err)
			}
		}
		answer, err := runtime.handleWIPICTableCall(thread, wipicTableGraphics, function)
		if err != nil {
			t.Fatalf("graphics function %d: %v", function, err)
		}
		return answer
	}

	const (
		sizeMedium = 0
		sizeSmall  = 8
		sizeLarge  = 16
	)
	for _, size := range []uint32{sizeMedium, sizeSmall, sizeLarge} {
		handle := call(26, 0, size, 0)
		if handle == 0 {
			t.Fatalf("MC_grpGetFont(size %d) answered no handle", size)
		}
		if got := int32(call(27, handle)); got != runtime.fontHeight() {
			t.Fatalf("MC_grpGetFontHeight for size %d = %d, want the face's %d", size, got, runtime.fontHeight())
		}
		if got := int32(call(28, handle)); got != runtime.fontBaseline() {
			t.Fatalf("MC_grpGetFontAscent for size %d = %d, want the face's %d", size, got, runtime.fontBaseline())
		}
		if got, want := int32(call(29, handle)), runtime.fontHeight()-runtime.fontBaseline(); got != want {
			t.Fatalf("MC_grpGetFontDescent for size %d = %d, want the face's %d", size, got, want)
		}
	}
}

// Whatever those three answer has to hold the glyphs the renderer draws, or a
// game that clips to the line it was promised cuts its own text. Every
// character has to fit between the ascent above the baseline and the descent
// below it.
func TestTheFaceFitsTheMetricsItReports(t *testing.T) {
	runtime := &initializationRuntime{}
	face := runtime.fontFace()
	ascent, descent := runtime.fontBaseline(), runtime.fontHeight()-runtime.fontBaseline()
	for _, character := range []rune("이어하기예아니오각힣ABCgjpqy0123456789") {
		bitmap := face.Render(character)
		if len(bitmap.Rows) == 0 {
			continue
		}
		if top := int32(bitmap.Ascent); top > ascent {
			t.Fatalf("%q rises %d rows above the baseline, past the reported ascent %d", character, top, ascent)
		}
		if bottom := int32(len(bitmap.Rows) - bitmap.Ascent); bottom > descent {
			t.Fatalf("%q falls %d rows below the baseline, past the reported descent %d", character, bottom, descent)
		}
	}
}

// Measurement has to move with the face too: a game that asks how wide a
// string is and then draws it must be told the width it will actually get.
func TestStringWidthMeasuresWithTheDrawnFace(t *testing.T) {
	runtime := &initializationRuntime{}
	text := []rune("한글")
	if got := runtime.graphicsTextWidth(text); got != 20 {
		t.Fatalf("width of %q = %d, want 20", string(text), got)
	}
	if got := runtime.graphicsCharAdvance('A'); got == 0 {
		t.Fatal("Latin must measure too")
	}
}

func TestJavaTextOverlayUsesHalfWidthSpaces(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	state := graphics.Native.(*runtimeGraphicsState)
	draw := func(text string, x int32, color uint16) error {
		state.color = color
		return runtime.graphicsDrawText(state, []rune(text), x, 40, 0)
	}
	want := drawnRegion(t, runtime, state, func() error {
		if err := draw("가나", 30, 0xffff); err != nil {
			return err
		}
		return draw("다라", 50, 0xfc00)
	})
	got := drawnRegion(t, runtime, state, func() error {
		if err := draw("가나다라", 30, 0xffff); err != nil {
			return err
		}
		return draw("    다라", 30, 0xfc00)
	})
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("space-padded overlay differs at pixel %d: %#04x, want %#04x", i, got[i], want[i])
		}
	}
	font := jvm.ReferenceValue(&jvm.Object{})
	space, err := runtimeFontCharWidth(runtime, client.JVM(), []jvm.Value{font, jvm.IntValue(' ')})
	if err != nil {
		t.Fatal(err)
	}
	if width, _ := space.Int32(); width != 5 {
		t.Fatalf("space width = %d, want 5", width)
	}
	for _, text := range []string{"가나다라", "    다라"} {
		value, err := runtimeFontStringWidth(runtime, client.JVM(), []jvm.Value{font, jvm.ReferenceValue(client.JVM().NewString(text))})
		if err != nil {
			t.Fatal(err)
		}
		if width, _ := value.Int32(); width != 40 {
			t.Fatalf("overlay string width = %d, want 40", width)
		}
	}
}

// The overlay replaces printable ASCII with one space, including punctuation
// between words. Proportional punctuation leaves a one-pixel color fringe.
func TestJavaTextOverlayAfterASCII(t *testing.T) {
	for _, prefix := range []string{"가.", "가!", "가1", "가A", "가i"} {
		t.Run(prefix, func(t *testing.T) {
			_, runtime := newTestRuntime(t)
			graphics, err := runtime.newScreenGraphics()
			if err != nil {
				t.Fatal(err)
			}
			state := graphics.Native.(*runtimeGraphicsState)
			draw := func(text string, x int32, color uint16) error {
				state.color = color
				return runtime.graphicsDrawText(state, []rune(text), x, 40, 0)
			}
			want := drawnRegion(t, runtime, state, func() error {
				if err := draw(prefix, 30, 0xffff); err != nil {
					return err
				}
				return draw("나", 45, 0xfc00)
			})
			got := drawnRegion(t, runtime, state, func() error {
				if err := draw(prefix+"나", 30, 0xffff); err != nil {
					return err
				}
				return draw("   나", 30, 0xfc00)
			})
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("overlay differs at pixel %d: %#04x, want %#04x", i, got[i], want[i])
				}
			}
			if got := runtime.graphicsTextWidth([]rune(prefix + "나")); got != 25 {
				t.Fatalf("width = %d, want 25", got)
			}
		})
	}
}

func TestJavaASCIICellMetrics(t *testing.T) {
	client, runtime := newTestRuntime(t)
	font := jvm.ReferenceValue(&jvm.Object{})
	for character := rune(' '); character <= '~'; character++ {
		value, err := runtimeFontCharWidth(runtime, client.JVM(), []jvm.Value{font, jvm.IntValue(int32(character))})
		if err != nil {
			t.Fatal(err)
		}
		if width, _ := value.Int32(); width != 5 {
			t.Fatalf("U+%04X width = %d, want 5", character, width)
		}
	}
	for _, character := range []rune{'\t', '\n', '\x7f', '가', '。'} {
		if got, want := runtime.graphicsCharAdvance(character), int32(runtime.fontFace().Render(character).Advance); got != want {
			t.Fatalf("U+%04X width = %d, want original %d", character, got, want)
		}
	}
}

// Full coverage must write the colour untouched, so text on a grid the font is
// exact at reaches the framebuffer as it did when glyphs were plotted rather
// than blended.
func TestBlendLeavesFullCoverageAlone(t *testing.T) {
	for _, destination := range []uint16{0x0000, 0xffff, 0x1234, 0xf81f} {
		if got := blend565(destination, 0x07e0, 0xff); got != 0x07e0 {
			t.Fatalf("blend over %#04x = %#04x, want the source %#04x", destination, got, 0x07e0)
		}
	}
	if got := blend565(0x1234, 0x07e0, 0); got != 0x1234 {
		t.Fatalf("blend at zero coverage = %#04x, want the destination", got)
	}
}

// A partly covered pixel has to land between the two colours in every channel,
// which is what makes a half-covered stroke read as half a stroke.
func TestBlendMixesEachChannel(t *testing.T) {
	const white, black = uint16(0xffff), uint16(0x0000)
	half := blend565(white, black, 0x80)
	red, green, blue := half>>11&0x1f, half>>5&0x3f, half&0x1f
	if red == 0 || red == 0x1f || green == 0 || green == 0x3f || blue == 0 || blue == 0x1f {
		t.Fatalf("half coverage gave %#04x (r%d g%d b%d), want a value between the two", half, red, green, blue)
	}
	if heavy := blend565(white, black, 0xc0); heavy>>11&0x1f >= red {
		t.Fatalf("coverage 0xc0 gave red %d, want darker than 0x80's %d", heavy>>11&0x1f, red)
	}
}

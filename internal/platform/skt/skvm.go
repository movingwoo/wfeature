package skt

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// SKVM's fixed-point scale: 1.0 is one billion. Every MathFP value a game
// passes or receives is scaled that way, so the constants are the scaled
// forms and every operation has to rescale after multiplying.
const (
	fixedScale  int64 = 1_000_000_000
	fixedPi     int64 = 3_141_592_654
	fixedHalfPi int64 = 1_570_796_327
	fixedTau    int64 = 6_283_185_308
)

// skvmState is the device state the SKVM classes report. None of it drives
// hardware — there is none — but a game that sets a value and reads it back
// must see what it set.
type skvmState struct {
	mu              sync.Mutex
	backlightColor  int32
	backlightOn     bool
	vibrating       bool
	keyToneOn       bool
	audioVolume     int32
	smsListener     *jvm.Object
	textFieldOwner  *jvm.Object
	zBufferEnabled  bool
	backfaceCulling bool
}

type graphics2DData struct {
	graphics *jvm.Object
}

type progressBarData struct {
	title    string
	value    int32
	maxValue int32
}

type smsMessageData struct {
	shortMessage []byte
	sender       string
}

type sisImageData struct {
	width  int32
	height int32
}

type xFileData struct {
	mu       sync.Mutex
	name     string
	mode     int32
	data     []byte
	cursor   int
	open     bool
	writable bool
	// dirty marks a file whose bytes have not reached the Host save store yet;
	// close and flush are what write them.
	dirty bool
}

type xTextFieldData struct {
	text    []rune
	maxSize int32
	focus   bool
	x, y    int32
	width   int32
	height  int32
}

type audioClipData struct {
	mu     sync.Mutex
	handle backend.AudioHandle
	loop   bool
	paused bool
}

func (runtime *Runtime) skvm() *skvmState {
	runtime.skvmOnce.Do(func() {
		// A handset arrives with its backlight and key tone on, which is what
		// a title that reads them before setting them expects to find.
		runtime.skvmState = &skvmState{backlightColor: 0xffffff, audioVolume: 50, backlightOn: true, keyToneOn: true}
	})
	return runtime.skvmState
}

// --- MathFP ---

func fixedMul(a, b int64) int64 {
	return saturate(float64(a) * float64(b) / float64(fixedScale))
}

// saturate converts a computed double back to the fixed representation,
// clamping rather than wrapping: a game that overflows an intermediate should
// see a huge number, not a sign flip.
func saturate(value float64) int64 {
	switch {
	case math.IsNaN(value):
		return 0
	case value >= float64(math.MaxInt64):
		return math.MaxInt64
	case value <= float64(math.MinInt64):
		return math.MinInt64
	}
	return int64(value)
}

func toFloat(value int64) float64 { return float64(value) / float64(fixedScale) }

func fromFloat(value float64) int64 { return saturate(value * float64(fixedScale)) }

func longArgument(arguments []jvm.Value, index int) (int64, error) {
	if index < 0 || index >= len(arguments) {
		return 0, fmt.Errorf("argument %d is missing", index)
	}
	value, err := arguments[index].Int64()
	if err != nil {
		return 0, fmt.Errorf("argument %d: %w", index, err)
	}
	return value, nil
}

func mathFPUnary(operation func(float64) float64) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		value, err := longArgument(arguments, 0)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.LongValue(fromFloat(operation(toFloat(value)))), nil
	}
}

func mathFPBinary(operation func(a, b float64) float64) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		a, err := longArgument(arguments, 0)
		if err != nil {
			return jvm.VoidValue(), err
		}
		b, err := longArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.LongValue(fromFloat(operation(toFloat(a), toFloat(b)))), nil
	}
}

func mathFPAbs(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	value, err := longArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if value < 0 {
		if value == math.MinInt64 {
			return jvm.LongValue(math.MaxInt64), nil
		}
		return jvm.LongValue(-value), nil
	}
	return jvm.LongValue(value), nil
}

// mathFPAdd and mathFPSub work on the scaled integers directly: adding two
// fixed values needs no rescaling, and going through a float would lose the
// low digits of a large one.
func mathFPAdd(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	a, err := longArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	b, err := longArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	sum := a + b
	if (a > 0 && b > 0 && sum < 0) || (a < 0 && b < 0 && sum > 0) {
		if a > 0 {
			return jvm.LongValue(math.MaxInt64), nil
		}
		return jvm.LongValue(math.MinInt64), nil
	}
	return jvm.LongValue(sum), nil
}

func mathFPSub(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	b, err := longArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	negated := jvm.LongValue(-b)
	if b == math.MinInt64 {
		negated = jvm.LongValue(math.MaxInt64)
	}
	return mathFPAdd(nil, []jvm.Value{arguments[0], negated})
}

func mathFPDivide(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	a, err := longArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	b, err := longArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if b == 0 {
		return jvm.VoidValue(), newGuestException("java/lang/ArithmeticException", "division by zero")
	}
	return jvm.LongValue(fromFloat(toFloat(a) / toFloat(b))), nil
}

func mathFPParse(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	value, err := longArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	// parseFP scales a whole number into the fixed representation.
	return jvm.LongValue(saturate(float64(value) * float64(fixedScale))), nil
}

func (runtime *Runtime) mathFPParseString(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	text, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	value, parseErr := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if parseErr != nil {
		return jvm.VoidValue(), newGuestException("java/lang/NumberFormatException", text)
	}
	return jvm.LongValue(fromFloat(value)), nil
}

func mathFPToLong(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	value, err := longArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	// toLong drops the fraction toward zero, which is what a fixed-point
	// library's integer conversion means.
	return jvm.LongValue(value / fixedScale), nil
}

func mathFPRound(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	value, err := longArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.LongValue(fromFloat(math.Round(toFloat(value)))), nil
}

func (runtime *Runtime) mathFPToStringE(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	value, err := longArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(vm.NewString(strconv.FormatFloat(toFloat(value), 'e', -1, 64))), nil
}

func (runtime *Runtime) mathFPToStringLF(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	value, err := longArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	// One Value per parameter regardless of category, so the decimal count
	// is argument 1 even though it follows a long.
	decimals, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if decimals < 0 || decimals > 18 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
			fmt.Sprintf("decimal count %d", decimals))
	}
	return jvm.ReferenceValue(vm.NewString(strconv.FormatFloat(toFloat(value), 'f', int(decimals), 64))), nil
}

// --- Graphics2D ---

func (runtime *Runtime) initGraphics2D(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	graphics, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if graphics == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "Graphics is null")
	}
	if _, ok := graphics.Native.(*graphicsContext); !ok {
		return jvm.VoidValue(), fmt.Errorf("argument 1 is not a Graphics")
	}
	receiver.Native = &graphics2DData{graphics: graphics}
	return jvm.VoidValue(), nil
}

func graphics2DContext(arguments []jvm.Value) (*graphicsContext, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, newGuestException("java/lang/NullPointerException", "Graphics2D is null")
	}
	data, ok := receiver.Native.(*graphics2DData)
	if !ok || data == nil {
		return nil, fmt.Errorf("receiver is not a Graphics2D")
	}
	context, ok := data.graphics.Native.(*graphicsContext)
	if !ok || context == nil {
		return nil, fmt.Errorf("Graphics2D has no Graphics")
	}
	return context, nil
}

func (runtime *Runtime) graphics2DPixel(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphics2DContext(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, err := pointArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, ok := contextPixelIndex(context, x, y)
	if !ok {
		return jvm.IntValue(0), nil
	}
	return jvm.IntValue(int32(context.pixels[index])<<16 |
		int32(context.pixels[index+1])<<8 | int32(context.pixels[index+2])), nil
}

func (runtime *Runtime) setGraphics2DPixel(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphics2DContext(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, err := pointArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	color, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, ok := contextPixelIndex(context, x, y)
	if !ok {
		return jvm.VoidValue(), nil
	}
	context.withDestinationWrite(func() {
		context.pixels[index] = byte(color >> 16)
		context.pixels[index+1] = byte(color >> 8)
		context.pixels[index+2] = byte(color)
		context.pixels[index+3] = 0xff
	})
	return jvm.VoidValue(), nil
}

// graphics2DPixelMask reports whether a pixel is opaque. SKVM's mask is the
// transparency bit, and this runtime's surfaces carry a real alpha channel,
// so the mask is the alpha rather than a separate plane.
func (runtime *Runtime) graphics2DPixelMask(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphics2DContext(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, err := pointArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, ok := contextPixelIndex(context, x, y)
	if !ok || context.pixels[index+3] == 0 {
		return jvm.IntValue(0), nil
	}
	return jvm.IntValue(1), nil
}

func (runtime *Runtime) setGraphics2DPixelMask(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphics2DContext(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, err := pointArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	mask, err := booleanArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	index, ok := contextPixelIndex(context, x, y)
	if !ok {
		return jvm.VoidValue(), nil
	}
	alpha := byte(0)
	if mask {
		alpha = 0xff
	}
	context.withDestinationWrite(func() { context.pixels[index+3] = alpha })
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) graphics2DInvertRect(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphics2DContext(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, width, height, err := rectArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	rect := context.translatedRect(x, y, width, height).intersect(context.clip)
	if rect.empty() {
		return jvm.VoidValue(), nil
	}
	context.withDestinationWrite(func() {
		for row := rect.minY; row < rect.maxY; row++ {
			for column := rect.minX; column < rect.maxX; column++ {
				index := (row*context.width + column) * 4
				context.pixels[index] = 0xff - context.pixels[index]
				context.pixels[index+1] = 0xff - context.pixels[index+1]
				context.pixels[index+2] = 0xff - context.pixels[index+2]
			}
		}
	})
	return jvm.VoidValue(), nil
}

// graphics2DCaptureLCD copies a region of the screen into a new immutable
// Image, which is what a game uses to save what is on the LCD before it draws
// an overlay over it. It is static — the region comes from the screen rather
// than from any Graphics — so the rectangle starts at the first argument.
func (runtime *Runtime) graphics2DCaptureLCD(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	x, y, width, height, err := rectArguments(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.renderMu.Lock()
	defer runtime.renderMu.Unlock()
	full := paintRect{maxX: runtime.frameWidth, maxY: runtime.frameHeight}
	context := &graphicsContext{
		pixels: runtime.frameRGBA, width: runtime.frameWidth, height: runtime.frameHeight,
		deviceClip: full, clip: full, active: true,
	}
	if width <= 0 || height <= 0 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
			"capture width and height must be positive")
	}
	pixels := make([]byte, int(width)*int(height)*4)
	for row := 0; row < int(height); row++ {
		for column := 0; column < int(width); column++ {
			destination := (row*int(width) + column) * 4
			pixels[destination+3] = 0xff
			index, ok := contextPixelIndex(context, x+int32(column), y+int32(row))
			if !ok {
				continue
			}
			copy(pixels[destination:destination+4], context.pixels[index:index+4])
		}
	}
	image, err := newMIDPImage(int(width), int(height), false, pixels)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(image), nil
}

// graphics2DDrawImage blits a source region with one of SKVM's combine modes.
func (runtime *Runtime) graphics2DDrawImage(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphics2DContext(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, err := pointArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	source, err := referenceArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	image, err := midpImageOf(source)
	if err != nil || image == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "Image is null")
	}
	sourceX, sourceY, width, height, err := rectArguments(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	mode, err := intArgument(arguments, 8)
	if err != nil {
		return jvm.VoidValue(), err
	}
	pixels := image.snapshot()
	context.withDestinationWrite(func() {
		for row := int32(0); row < height; row++ {
			for column := int32(0); column < width; column++ {
				sx, sy := sourceX+column, sourceY+row
				if sx < 0 || sy < 0 || int(sx) >= image.width || int(sy) >= image.height {
					continue
				}
				index, ok := contextPixelIndex(context, x+column, y+row)
				if !ok {
					continue
				}
				sourceIndex := (int(sy)*image.width + int(sx)) * 4
				if pixels[sourceIndex+3] == 0 && mode == skvmSourceCopy {
					continue
				}
				for channel := 0; channel < 3; channel++ {
					value := pixels[sourceIndex+channel]
					switch mode {
					case skvmSourceAnd:
						value &= context.pixels[index+channel]
					case skvmSourceOr:
						value |= context.pixels[index+channel]
					case skvmSourceXor:
						value ^= context.pixels[index+channel]
					}
					context.pixels[index+channel] = value
				}
				context.pixels[index+3] = 0xff
			}
		}
	})
	return jvm.VoidValue(), nil
}

// SKVM's Graphics2D combine modes.
const (
	skvmSourceCopy int32 = 0
	skvmSourceAnd  int32 = 1
	skvmSourceOr   int32 = 2
	skvmSourceXor  int32 = 3
)

// contextPixelIndex resolves a translated coordinate to a byte offset, or
// reports that it is outside the clip.
func contextPixelIndex(context *graphicsContext, x, y int32) (int, bool) {
	px := int(x) + int(context.translateX)
	py := int(y) + int(context.translateY)
	if px < context.clip.minX || px >= context.clip.maxX || py < context.clip.minY || py >= context.clip.maxY {
		return 0, false
	}
	index := (py*context.width + px) * 4
	if index < 0 || index+4 > len(context.pixels) {
		return 0, false
	}
	return index, true
}

func pointArguments(arguments []jvm.Value, start int) (int32, int32, error) {
	x, err := intArgument(arguments, start)
	if err != nil {
		return 0, 0, err
	}
	y, err := intArgument(arguments, start+1)
	if err != nil {
		return 0, 0, err
	}
	return x, y, nil
}

// --- device state ---

func (runtime *Runtime) backLightOn(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := intArgument(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.skvm()
	state.mu.Lock()
	state.backlightOn = true
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) backLightOff(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	state := runtime.skvm()
	state.mu.Lock()
	state.backlightOn = false
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) backLightColor(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	state := runtime.skvm()
	state.mu.Lock()
	defer state.mu.Unlock()
	return jvm.IntValue(state.backlightColor), nil
}

func (runtime *Runtime) setBackLightColor(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	color, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.skvm()
	state.mu.Lock()
	state.backlightColor = color
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) vibrationStart(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := intArgument(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.skvm()
	state.mu.Lock()
	state.vibrating = true
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) vibrationStop(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	state := runtime.skvm()
	state.mu.Lock()
	state.vibrating = false
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

// vibrationSupported and vibrationLevels answer the two questions a title asks
// before it vibrates. There is no motor here and Vibration.start only records
// that it was asked, but a vibration is fire-and-forget: nothing comes back
// that a title could be misled by, and one that is told the handset cannot
// vibrate hides the setting rather than leaving it switched off. The level
// count is the handset's ten.
func (runtime *Runtime) vibrationSupported(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(1), nil
}

func (runtime *Runtime) vibrationLevels(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(vibrationLevelCount), nil
}

const vibrationLevelCount = 10

// deviceBeep plays the tone through the same timeline every other sound uses,
// so a beep is audible wherever the rest of the audio is.
func (runtime *Runtime) deviceBeep(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	frequency, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	duration, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	note := midiNoteForFrequency(frequency)
	return runtime.playTone(vm, []jvm.Value{jvm.IntValue(note), jvm.IntValue(duration), jvm.IntValue(100)})
}

// midiNoteForFrequency converts hertz to the nearest MIDI note, because the
// audio sink speaks notes rather than frequencies.
func midiNoteForFrequency(frequency int32) int32 {
	if frequency <= 0 {
		return 69
	}
	note := int32(math.Round(69 + 12*math.Log2(float64(frequency)/440)))
	return min(max(note, 0), 127)
}

func (runtime *Runtime) deviceSetNAI(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, err := intArgument(arguments, 0)
	return jvm.VoidValue(), err
}

// booleanValue renders a Go bool as the int a JVM boolean is.
func booleanValue(value bool) jvm.Value {
	if value {
		return jvm.IntValue(1)
	}
	return jvm.IntValue(0)
}

// The rest of Device is the handset's settings panel: the backlight, the key
// tone, the key repeat rate, the colour depth, and the two calls that install
// a wallpaper or a ringtone. A title turns these on at startup and, in the
// case of the two toggles, reads them back, so the toggles remember what was
// set rather than answering a constant — a game that turns the backlight off
// and sees it on has been told the setting did not take.
func (runtime *Runtime) setDeviceBacklight(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	enabled, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.skvm()
	state.mu.Lock()
	state.backlightOn = enabled != 0
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) deviceBacklightEnabled(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	state := runtime.skvm()
	state.mu.Lock()
	defer state.mu.Unlock()
	return booleanValue(state.backlightOn), nil
}

func (runtime *Runtime) setDeviceKeyTone(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	enabled, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.skvm()
	state.mu.Lock()
	state.keyToneOn = enabled != 0
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) deviceKeyToneEnabled(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	state := runtime.skvm()
	state.mu.Lock()
	defer state.mu.Unlock()
	return booleanValue(state.keyToneOn), nil
}

// deviceAccepted takes the settings whose effect is not observable here: the
// colour mode is fixed by our framebuffer, LCD restore and the key repeat rate
// are the Host's timing rather than the device's, and a game reads none of
// them back. Accepting them is not a claim they did something — it is that
// refusing would stop a startup over a setting with nothing behind it.
func deviceAccepted(parameters int) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		for index := range parameters {
			if _, err := intArgument(arguments, index); err != nil {
				return jvm.VoidValue(), err
			}
		}
		return jvm.VoidValue(), nil
	}
}

func (runtime *Runtime) deviceInvokeWapBrowser(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	// There is no radio and no browser to hand the URL to. The call returns
	// void, so a title cannot tell this apart from a browser that was
	// launched and dismissed.
	_, err := optionalStringArgument(arguments, 0)
	return jvm.VoidValue(), err
}

// deviceInstallRefused answers setSISImage and setMelody. Both install
// something into the handset — a wallpaper, a ringtone — outside the game, and
// both return whether it worked. There is no handset here, so the answer is
// no; claiming success would have a title tell its user it changed a setting
// that never changed.
func (runtime *Runtime) deviceInstallRefused(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := intArgument(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	return booleanValue(false), nil
}

// --- ProgressBar ---

func (runtime *Runtime) initProgressBar(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	title, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Native = &progressBarData{title: title, maxValue: 100}
	return jvm.VoidValue(), nil
}

func progressBarArgument(arguments []jvm.Value) (*progressBarData, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, newGuestException("java/lang/NullPointerException", "ProgressBar is null")
	}
	data, ok := receiver.Native.(*progressBarData)
	if !ok || data == nil {
		return nil, fmt.Errorf("receiver is not a ProgressBar")
	}
	return data, nil
}

func (runtime *Runtime) progressBarValue(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := progressBarArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(data.value), nil
}

func (runtime *Runtime) setProgressBarValue(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := progressBarArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	value, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.value = min(max(value, 0), data.maxValue)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) progressBarMaxValue(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := progressBarArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(data.maxValue), nil
}

func (runtime *Runtime) setProgressBarMaxValue(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := progressBarArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	value, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if value <= 0 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
			fmt.Sprintf("max value %d", value))
	}
	data.maxValue = value
	data.value = min(data.value, value)
	return jvm.VoidValue(), nil
}

// --- audio ---

func (runtime *Runtime) getAudioClip(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	kind, err := optionalStringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	clip, err := vm.NewObject(skvm.RuntimeAudioClipClass, "(Ljava/lang/String;)V",
		jvm.ReferenceValue(vm.NewString(kind)))
	if err != nil {
		return jvm.VoidValue(), err
	}
	clip.Native = &audioClipData{}
	return jvm.ReferenceValue(clip), nil
}

func (runtime *Runtime) audioVolume(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	state := runtime.skvm()
	state.mu.Lock()
	defer state.mu.Unlock()
	return jvm.IntValue(state.audioVolume), nil
}

// The volume calls come in two shapes: one that names an audio format and one
// that does not. A handset offered both and a title picks whichever it was
// written against — all three SKT titles here take the format-taking shape —
// so both reach the same single volume. The format itself changes nothing:
// there is one output, and a per-format mixer would be inventing a device
// distinction no game can observe.
func (runtime *Runtime) audioVolumeForFormat(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if err := requireFormat(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.audioVolume(vm, nil)
}

func (runtime *Runtime) setAudioVolumeForFormat(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if err := requireFormat(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	return runtime.setAudioVolume(vm, arguments[1:])
}

// maxAudioVolume is the top of the scale setAudioVolume clamps to. A game
// asks for it to work out the step its own slider takes, so answering with
// anything but the range actually in force — 0 in particular — gives it a
// scale it cannot use.
const maxAudioVolume int32 = 100

func (runtime *Runtime) maxAudioVolume(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if err := requireFormat(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(maxAudioVolume), nil
}

// requireFormat rejects a null format the way the handset did. The value is
// not otherwise used; see audioVolumeForFormat.
func requireFormat(arguments []jvm.Value, index int) error {
	reference, err := referenceArgument(arguments, index)
	if err != nil {
		return err
	}
	if reference == nil {
		return newGuestException("java/lang/NullPointerException", "audio format is null")
	}
	return nil
}

func (runtime *Runtime) setAudioVolume(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	volume, err := intArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.skvm()
	state.mu.Lock()
	state.audioVolume = min(max(volume, 0), 100)
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

func audioClipArgument(arguments []jvm.Value) (*audioClipData, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, newGuestException("java/lang/NullPointerException", "AudioClip is null")
	}
	data, ok := receiver.Native.(*audioClipData)
	if !ok || data == nil {
		data = &audioClipData{}
		receiver.Native = data
	}
	return data, nil
}

func (runtime *Runtime) audioClipOpen(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	clip, err := audioClipArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data, err := recordBytesArgument(arguments, 1, 2, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	audio := runtime.audioTimeline()
	if audio == nil {
		return jvm.VoidValue(), nil
	}
	handle, loadErr := audio.Load(data)
	if loadErr != nil {
		return jvm.VoidValue(), newGuestException(skvm.UnsupportedFormatExceptionClass, loadErr.Error())
	}
	clip.mu.Lock()
	previous := clip.handle
	clip.handle = handle
	clip.mu.Unlock()
	if previous != 0 {
		_ = audio.Close(previous)
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) audioClipAction(action string) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		clip, err := audioClipArgument(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		audio := runtime.audioTimeline()
		clip.mu.Lock()
		handle := clip.handle
		switch action {
		case "play":
			clip.loop, clip.paused = false, false
		case "loop":
			clip.loop, clip.paused = true, false
		case "pause":
			clip.paused = true
		case "resume":
			clip.paused = false
		case "stop":
			clip.paused = false
		}
		loop := clip.loop
		clip.mu.Unlock()
		if audio == nil || handle == 0 {
			return jvm.VoidValue(), nil
		}
		switch action {
		case "play", "loop", "resume":
			// Resume restarts rather than continuing: the timeline tracks a
			// start instant, not a paused offset, and pretending otherwise
			// would report a position the sink is not at.
			if err := audio.Play(handle, runtime.audioNow(), loop); err != nil {
				return jvm.VoidValue(), newGuestException(skvm.UnsupportedFormatExceptionClass, err.Error())
			}
		case "pause", "stop":
			audio.Stop(handle)
		case "close":
			_ = audio.Close(handle)
			clip.mu.Lock()
			clip.handle = 0
			clip.mu.Unlock()
		}
		return jvm.VoidValue(), nil
	}
}

// --- SMS, Call, PhoneBook: no radio behind any of them ---

func (runtime *Runtime) smsGet(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.ReferenceValue(nil), nil
}

func (runtime *Runtime) smsGetInto(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(0), nil
}

func (runtime *Runtime) smsSend(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	// Refusing is the honest answer: a game told the message was sent waits
	// for a reply that cannot arrive.
	return jvm.IntValue(0), nil
}

func (runtime *Runtime) smsListener(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	state := runtime.skvm()
	state.mu.Lock()
	defer state.mu.Unlock()
	return jvm.ReferenceValue(state.smsListener), nil
}

func (runtime *Runtime) setSMSListener(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	listener, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.skvm()
	state.mu.Lock()
	state.smsListener = listener
	state.mu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) initSMSMessage(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	message := &smsMessageData{}
	if array, _ := referenceArgument(arguments, 1); array != nil {
		if _, values, snapErr := jvm.ArraySnapshot(array); snapErr == nil {
			for _, value := range values {
				raw, _ := value.Int32()
				message.shortMessage = append(message.shortMessage, byte(raw))
			}
		}
	}
	message.sender, _ = optionalStringArgument(arguments, 2)
	receiver.Native = message
	return jvm.VoidValue(), nil
}

func smsMessageArgument(arguments []jvm.Value) (*smsMessageData, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, newGuestException("java/lang/NullPointerException", "SMSMessage is null")
	}
	data, ok := receiver.Native.(*smsMessageData)
	if !ok || data == nil {
		data = &smsMessageData{}
		receiver.Native = data
	}
	return data, nil
}

func (runtime *Runtime) smsShortMessage(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := smsMessageArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(data.shortMessage) == 0 {
		return jvm.ReferenceValue(nil), nil
	}
	array, err := newByteArray(vm, data.shortMessage)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(array), nil
}

func (runtime *Runtime) smsSender(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := smsMessageArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if data.sender == "" {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(vm.NewString(data.sender)), nil
}

func nullReference(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.ReferenceValue(nil), nil
}

func zeroInt(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(0), nil
}

func zeroByte(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(0), nil
}

func (runtime *Runtime) emptyStringArray(vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return stringArray(vm, nil)
}

// --- XDisplay / Toolkit / XTextField ---

// xDisplayRefresh is how a title on this vendor says the screen is ready: it
// draws into the Graphics whenever it likes and then pushes, rather than
// waiting to be asked for a paint. What it does here is mark the screen ready
// and return; the Host pass presents it.
//
// **It used to present on the spot, and one title family calls it about a
// hundred times per Host pass** — 496,029 calls in 4,000 ticks, against
// another title's 3.7. Their loop draws as fast as the machine will let it and
// pushes every time round, so every call allocated a copy of the framebuffer
// and handed it to a surface that copied it again, for a picture no Host could
// show: the CLI reads the surface at a tick boundary and the page sends at most
// one frame per tick.
//
// **What the call still does on the spot is take the picture.** The refresh is
// the game's own frame boundary — it is the moment the guest thread says the
// screen is complete, and it is that thread calling — so copying then is what
// makes the picture whole. Waiting until the Host pass to copy shows whatever
// the game happened to be halfway through drawing, which is a torn frame and,
// worse, a different one every run. What waits for the pass is the *present*.
//
// One reusable buffer holds it, so a hundred pushes a pass cost a hundred
// memcpys and no allocation, and the Host is handed one frame. That is a fifth
// of the host CPU off that family's runs with the pictures unchanged.
//
// The rectangle is validated and then ignored, as it was before: this
// runtime's surface is presented whole.
func (runtime *Runtime) xDisplayRefresh(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, _, _, _, err := rectArguments(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	runtime.renderMu.Lock()
	if len(runtime.refreshFrame) != len(runtime.frameRGBA) {
		runtime.refreshFrame = make([]byte, len(runtime.frameRGBA))
	}
	copy(runtime.refreshFrame, runtime.frameRGBA)
	runtime.renderMu.Unlock()

	runtime.displayMu.Lock()
	runtime.refreshPending = true
	runtime.displayMu.Unlock()
	return jvm.VoidValue(), nil
}

// presentRefresh shows the picture a title pushed with XDisplay.refresh, once
// per Host pass. A pass with nothing pushed since the last one costs nothing.
func (runtime *Runtime) presentRefresh() error {
	runtime.displayMu.Lock()
	pending := runtime.refreshPending
	runtime.refreshPending = false
	runtime.displayMu.Unlock()
	if !pending {
		return nil
	}
	runtime.renderMu.Lock()
	frame := backend.Frame{
		Width:  runtime.frameWidth,
		Height: runtime.frameHeight,
		RGBA:   runtime.refreshFrame,
	}
	// The surface copies what it is given and keeps nothing, so the buffer is
	// handed over rather than copied again. It is read under the same lock the
	// refresh writes it under.
	err := runtime.framebuffer.Present(frame)
	runtime.renderMu.Unlock()
	if err != nil {
		return fmt.Errorf("XDisplay.refresh: %w", err)
	}
	return nil
}

// xDisplayDrawImageEx is the vendor's blit with a source rectangle, which the
// MIDP Graphics of the day had no form of. Thirteen call sites in one local
// title settle the arguments: a Graphics to draw on, a second Image that is
// null at every one of them, the destination point, the source image and its
// rectangle, and a mode that is zero at every one of them.
//
// **The second Image and a non-zero mode are unexercised.** A non-null image
// there is drawn as if it were absent and a mode is ignored, because the only
// alternative on no evidence is to refuse a draw a title is making — and the
// title that makes it is the one this was written for. What either means is
// the note in docs/skvm.md rather than a guess in this body.
func (runtime *Runtime) xDisplayDrawImageEx(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	context, err := graphicsArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, err := pointArguments(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	source, err := midpImageArgument(arguments, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	sourceX, sourceY, width, height, err := rectArguments(arguments, 5)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := intArgument(arguments, 9); err != nil {
		return jvm.VoidValue(), err
	}
	pixels := source.snapshot()
	context.withDestinationWrite(func() {
		for row := int32(0); row < height; row++ {
			for column := int32(0); column < width; column++ {
				sx, sy := sourceX+column, sourceY+row
				if sx < 0 || sy < 0 || int(sx) >= source.width || int(sy) >= source.height {
					continue
				}
				index := (int(sy)*source.width + int(sx)) * 4
				context.blendPixel(int64(x+column), int64(y+row),
					pixels[index], pixels[index+1], pixels[index+2], pixels[index+3])
			}
		}
	})
	return jvm.VoidValue(), nil
}

// xDisplayCopyLCD copies what is on the screen into an image, which is what a
// title takes before it draws an overlay it means to undo. It is the same
// answer Graphics2D.captureLCD gives, written into an image the caller already
// owns rather than into a new one.
func (runtime *Runtime) xDisplayCopyLCD(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := graphicsArgument(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	target, err := midpImageArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, width, height, err := rectArguments(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.renderMu.Lock()
	defer runtime.renderMu.Unlock()
	target.mu.Lock()
	defer target.mu.Unlock()
	for row := int32(0); row < height && int(row) < target.height; row++ {
		for column := int32(0); column < width && int(column) < target.width; column++ {
			screenX, screenY := x+column, y+row
			if screenX < 0 || screenY < 0 ||
				int(screenX) >= runtime.frameWidth || int(screenY) >= runtime.frameHeight {
				continue
			}
			from := (int(screenY)*runtime.frameWidth + int(screenX)) * 4
			to := (int(row)*target.width + int(column)) * 4
			copy(target.rgba[to:to+4], runtime.frameRGBA[from:from+4])
		}
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) toolkitScreenWidth(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(int32(runtime.frameWidth)), nil
}

func (runtime *Runtime) toolkitScreenHeight(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(int32(runtime.frameHeight)), nil
}

// toolkitDrawString draws into the Host frame directly, because SKVM's
// Toolkit is a static entry point with no Graphics to draw through.
func (runtime *Runtime) toolkitDrawString(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	text, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, err := pointArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	font, _ := fontReceiver(runtime.fontObject(fontSystem, fontPlain, fontMedium))
	runtime.renderMu.Lock()
	defer runtime.renderMu.Unlock()
	full := paintRect{maxX: runtime.frameWidth, maxY: runtime.frameHeight}
	context := &graphicsContext{
		pixels: runtime.frameRGBA, width: runtime.frameWidth, height: runtime.frameHeight,
		deviceClip: full, clip: full, color: screenForeground, active: true,
	}
	font.render(context, []rune(text), int64(x), int64(y))
	return jvm.VoidValue(), nil
}

// toolkitScreenGraphics answers the Graphics com.xce.lcdui.Toolkit publishes as
// a static field. It is the one screen Graphics a Canvas paint is handed —
// see screenGraphics for why this vendor has only one — clipped to the whole
// screen, because a title reading the field is about to draw outside a paint.
func (runtime *Runtime) toolkitScreenGraphics(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	runtime.renderMu.Lock()
	defer runtime.renderMu.Unlock()
	full := paintRect{maxX: runtime.frameWidth, maxY: runtime.frameHeight}
	return jvm.ReferenceValue(runtime.screenGraphics(full)), nil
}

func (runtime *Runtime) initXTextField(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Native = &xTextFieldData{maxSize: 64}
	return jvm.VoidValue(), nil
}

func xTextFieldArgument(arguments []jvm.Value) (*xTextFieldData, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, newGuestException("java/lang/NullPointerException", "XTextField is null")
	}
	data, ok := receiver.Native.(*xTextFieldData)
	if !ok || data == nil {
		data = &xTextFieldData{maxSize: 64}
		receiver.Native = data
	}
	return data, nil
}

func (runtime *Runtime) xTextFieldText(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := xTextFieldArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(vm.NewString(string(data.text))), nil
}

func (runtime *Runtime) setXTextFieldText(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := xTextFieldArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	text, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runes := []rune(text)
	if int32(len(runes)) > data.maxSize {
		runes = runes[:data.maxSize]
	}
	data.text = runes
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) xTextFieldMaxSize(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := xTextFieldArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(data.maxSize), nil
}

func (runtime *Runtime) setXTextFieldMaxSize(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := xTextFieldArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	size, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if size <= 0 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
			fmt.Sprintf("max size %d", size))
	}
	data.maxSize = size
	if int32(len(data.text)) > size {
		data.text = data.text[:size]
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) xTextFieldFocus(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := xTextFieldArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if data.focus {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

func (runtime *Runtime) setXTextFieldFocus(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := xTextFieldArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	focus, err := booleanArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.focus = focus
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) setXTextFieldBounds(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := xTextFieldArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, width, height, err := rectArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.x, data.y, data.width, data.height = x, y, width, height
	return jvm.VoidValue(), nil
}

// xTextFieldInputChar is the only way characters reach the field: there is no
// on-screen input method, so the game supplies them.
func (runtime *Runtime) xTextFieldInputChar(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := xTextFieldArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	character, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if int32(len(data.text)) >= data.maxSize {
		return jvm.VoidValue(), nil
	}
	data.text = append(data.text, rune(uint16(character)))
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) xTextFieldPaint(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := xTextFieldArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	context, err := graphicsReceiver(arguments[1:])
	if err != nil {
		return jvm.VoidValue(), err
	}
	font, err := fontReceiver(context.font)
	if err != nil || font == nil {
		font, _ = fontReceiver(runtime.fontObject(fontSystem, fontPlain, fontMedium))
	}
	previous := context.color
	context.color = screenForeground
	context.withDestinationWrite(func() {
		font.render(context, data.text, int64(data.x)+int64(context.translateX), int64(data.y)+int64(context.translateY))
	})
	context.color = previous
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) ignoreVoid(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), nil
}

// --- m3d ---

// object3DData holds a mesh and its accumulated transform. The matrix is the
// only thing a game reads back, because there is no rasterizer here.
type object3DData struct {
	name      string
	vertices  [][3]int32
	triangles [][4]int32
	matrix    [3][4]int64
}

func identityMatrix() [3][4]int64 {
	return [3][4]int64{
		{fixedScale, 0, 0, 0},
		{0, fixedScale, 0, 0},
		{0, 0, fixedScale, 0},
	}
}

func (runtime *Runtime) initObject3D(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver.Native = &object3DData{name: name, matrix: identityMatrix()}
	return jvm.VoidValue(), nil
}

func object3DArgument(arguments []jvm.Value) (*object3DData, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, newGuestException("java/lang/NullPointerException", "Object3D is null")
	}
	data, ok := receiver.Native.(*object3DData)
	if !ok || data == nil {
		data = &object3DData{matrix: identityMatrix()}
		receiver.Native = data
	}
	return data, nil
}

func (runtime *Runtime) object3DName(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := object3DArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(vm.NewString(data.name)), nil
}

func (runtime *Runtime) setObject3DName(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := object3DArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, err := optionalStringArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.name = name
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) object3DAddVertex(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := object3DArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	y, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	z, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(data.vertices) >= maxMeshElements {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "too many vertices")
	}
	data.vertices = append(data.vertices, [3]int32{x, y, z})
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) object3DAddTriangle(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := object3DArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	values := [4]int32{}
	for index := range values {
		value, valueErr := intArgument(arguments, index+1)
		if valueErr != nil {
			return jvm.VoidValue(), valueErr
		}
		values[index] = value
	}
	for _, vertex := range values[:3] {
		if vertex < 0 || int(vertex) >= len(data.vertices) {
			return jvm.VoidValue(), newGuestException("java/lang/IndexOutOfBoundsException",
				fmt.Sprintf("triangle vertex %d of %d", vertex, len(data.vertices)))
		}
	}
	if len(data.triangles) >= maxMeshElements {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalStateException", "too many triangles")
	}
	data.triangles = append(data.triangles, values)
	return jvm.VoidValue(), nil
}

// maxMeshElements bounds one mesh so a runaway loop cannot make the runtime
// allocate without limit.
const maxMeshElements = 1 << 20

func (runtime *Runtime) object3DSetVertices(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := object3DArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	axes, err := intArrayArguments(arguments, 1, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.vertices = data.vertices[:0]
	for index := range axes[0] {
		data.vertices = append(data.vertices, [3]int32{axes[0][index], axes[1][index], axes[2][index]})
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) object3DSetTriangles(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := object3DArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	columns, err := intArrayArguments(arguments, 1, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.triangles = data.triangles[:0]
	for index := range columns[0] {
		data.triangles = append(data.triangles, [4]int32{
			columns[0][index], columns[1][index], columns[2][index], columns[3][index],
		})
	}
	return jvm.VoidValue(), nil
}

// intArrayArguments reads a run of int[] arguments and insists they are the
// same length, because a mesh built from ragged columns is a silent corruption
// rather than a visible error.
func intArrayArguments(arguments []jvm.Value, start, count int) ([][]int32, error) {
	columns := make([][]int32, count)
	for index := 0; index < count; index++ {
		_, values, err := primitiveArrayArgument(arguments, start+index, jvm.TypeInt)
		if err != nil {
			return nil, err
		}
		column := make([]int32, len(values))
		for position, value := range values {
			raw, rawErr := value.Int32()
			if rawErr != nil {
				return nil, rawErr
			}
			column[position] = raw
		}
		if index > 0 && len(column) != len(columns[0]) {
			return nil, newGuestException("java/lang/IllegalArgumentException",
				"mesh arrays have different lengths")
		}
		if len(column) > maxMeshElements {
			return nil, newGuestException("java/lang/IllegalArgumentException", "mesh array is too large")
		}
		columns[index] = column
	}
	return columns, nil
}

func (runtime *Runtime) object3DTranslate(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := object3DArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, z, err := tripleArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data.matrix[0][3] += int64(x)
	data.matrix[1][3] += int64(y)
	data.matrix[2][3] += int64(z)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) object3DScale(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := object3DArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, z, err := tripleArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	for column := 0; column < 4; column++ {
		data.matrix[0][column] = fixedMul(data.matrix[0][column], int64(x))
		data.matrix[1][column] = fixedMul(data.matrix[1][column], int64(y))
		data.matrix[2][column] = fixedMul(data.matrix[2][column], int64(z))
	}
	return jvm.VoidValue(), nil
}

// object3DRotate composes X, then Y, then Z rotations onto the transform.
// Angles are in the same fixed-point radians MathFP uses.
func (runtime *Runtime) object3DRotate(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := object3DArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	x, y, z, err := tripleArguments(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	for _, rotation := range []struct {
		axis  int
		angle float64
	}{{0, toFloat(int64(x))}, {1, toFloat(int64(y))}, {2, toFloat(int64(z))}} {
		if rotation.angle == 0 {
			continue
		}
		data.matrix = multiplyMatrix(rotationMatrix(rotation.axis, rotation.angle), data.matrix)
	}
	return jvm.VoidValue(), nil
}

func rotationMatrix(axis int, angle float64) [3][4]int64 {
	sin, cos := fromFloat(math.Sin(angle)), fromFloat(math.Cos(angle))
	matrix := identityMatrix()
	switch axis {
	case 0:
		matrix[1][1], matrix[1][2] = cos, -sin
		matrix[2][1], matrix[2][2] = sin, cos
	case 1:
		matrix[0][0], matrix[0][2] = cos, sin
		matrix[2][0], matrix[2][2] = -sin, cos
	case 2:
		matrix[0][0], matrix[0][1] = cos, -sin
		matrix[1][0], matrix[1][1] = sin, cos
	}
	return matrix
}

// multiplyMatrix composes two affine transforms held as three rows of four.
func multiplyMatrix(left, right [3][4]int64) [3][4]int64 {
	var result [3][4]int64
	for row := 0; row < 3; row++ {
		for column := 0; column < 4; column++ {
			sum := int64(0)
			for index := 0; index < 3; index++ {
				sum += fixedMul(left[row][index], right[index][column])
			}
			if column == 3 {
				sum += left[row][3]
			}
			result[row][column] = sum
		}
	}
	return result
}

func tripleArguments(arguments []jvm.Value, start int) (int32, int32, int32, error) {
	x, err := intArgument(arguments, start)
	if err != nil {
		return 0, 0, 0, err
	}
	y, err := intArgument(arguments, start+1)
	if err != nil {
		return 0, 0, 0, err
	}
	z, err := intArgument(arguments, start+2)
	if err != nil {
		return 0, 0, 0, err
	}
	return x, y, z, nil
}

func (runtime *Runtime) object3DMatrixRow(row int) jvm.NativeMethod {
	return func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		data, err := object3DArgument(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, 4)
		if err != nil {
			return jvm.VoidValue(), err
		}
		values := make([]jvm.Value, 4)
		for column := 0; column < 4; column++ {
			values[column] = jvm.IntValue(int32(min(max(data.matrix[row][column],
				int64(math.MinInt32)), int64(math.MaxInt32))))
		}
		if err := jvm.SetArrayRange(array, 0, values); err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.ReferenceValue(array), nil
	}
}

func (runtime *Runtime) graphics3DFlag(read bool, field func(*skvmState) *bool) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		state := runtime.skvm()
		state.mu.Lock()
		defer state.mu.Unlock()
		target := field(state)
		if read {
			if *target {
				return jvm.IntValue(1), nil
			}
			return jvm.IntValue(0), nil
		}
		value, err := booleanArgument(arguments, 1)
		if err != nil {
			return jvm.VoidValue(), err
		}
		*target = value
		return jvm.VoidValue(), nil
	}
}

// --- SISImage ---

func sisImageArgument(arguments []jvm.Value) (*sisImageData, error) {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, newGuestException("java/lang/NullPointerException", "SISImage is null")
	}
	data, ok := receiver.Native.(*sisImageData)
	if !ok || data == nil {
		data = &sisImageData{}
		receiver.Native = data
	}
	return data, nil
}

func (runtime *Runtime) sisCreateBuffer(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	data, err := sisImageArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	width, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	height, err := intArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if width <= 0 || height <= 0 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
			"buffer sizes must be positive")
	}
	data.width, data.height = width, height
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) sisDimension(read func(*sisImageData) int32) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		data, err := sisImageArgument(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.IntValue(read(data)), nil
	}
}

// sisFrame answers no image. The SIS container format is not decoded — see
// docs/skvm.md — and inventing a blank frame would let a game draw nothing
// while believing it drew a sprite.
func (runtime *Runtime) sisFrame(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	id, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if id != 0 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
			fmt.Sprintf("frame %d is not loaded", id))
	}
	return jvm.ReferenceValue(nil), nil
}

func (runtime *Runtime) sisRequiredBufferSize(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := recordBytesArgument(arguments, 0, 1, 2); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(0), nil
}

package ktf

import (
	"encoding/json"
	"image"
	"image/color"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestHeapGraphicsPreservesTransparentColorAndSharedMask(t *testing.T) {
	client, runtime := newTestRuntime(t)
	bitmap := image.NewNRGBA(image.Rect(-1, 3, 2, 5))
	bitmap.SetNRGBA(-1, 3, color.NRGBA{R: 255, G: 19, B: 127, A: 0})
	bitmap.SetNRGBA(0, 3, color.NRGBA{R: 211, G: 59, B: 73, A: 117})
	bitmap.SetNRGBA(1, 3, color.NRGBA{R: 127, G: 255, B: 11, A: 255})
	decoded := withOpacity(bitmap)
	imageObject := &jvm.Object{ClassName: "org/kwis/msp/lcdui/Image", Native: decoded, Fields: make(map[string]jvm.Value)}
	handle, err := runtime.imageFramebufferHandle(imageObject)
	if err != nil {
		t.Fatal(err)
	}
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	state := graphics.Native.(*runtimeGraphicsState)
	state.clipX, state.clipY, state.clipWidth, state.clipHeight = 5, 6, 13, 17
	state.translateX, state.translateY, state.alpha, state.xorMode = -7, 19, 93, true
	state.color, state.rgb, state.strokeStyle = 0x1234, 0x765432, 1
	saved, err := runtime.captureHeapState([]*jvm.Object{imageObject, graphics})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Opacities) != 1 {
		t.Fatalf("shared mask count=%d", len(saved.Opacities))
	}
	memory, err := client.core.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var parsed runtimeHeapState
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	freshClient, fresh := newTestRuntime(t)
	freshClient.core, err = armcore.NewCoreFromState(memory, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	roots, err := fresh.restoreHeapState(parsed)
	if err != nil {
		t.Fatal(err)
	}
	restored := roots[0].Native.(image.Image)
	if restored.Bounds() != decoded.Bounds() {
		t.Fatal("image origin changed")
	}
	for y := decoded.Bounds().Min.Y; y < decoded.Bounds().Max.Y; y++ {
		for x := decoded.Bounds().Min.X; x < decoded.Bounds().Max.X; x++ {
			want, got := decoded.At(x, y), restored.At(x, y)
			wr, wg, wb, wa := want.RGBA()
			gr, gg, gb, ga := got.RGBA()
			if wr != gr || wg != gg || wb != gb || wa != ga || color.NRGBAModel.Convert(want) != color.NRGBAModel.Convert(got) || imagePixel565(decoded, x, y) != imagePixel565(restored, x, y) {
				t.Fatalf("restored pixel differs at %d,%d", x, y)
			}
		}
	}
	if got := roots[1].Native.(*runtimeGraphicsState); got == state || *got != *state {
		t.Fatal("graphics state differs or is shared with its source")
	}
	mask := fresh.framebufferOpacityOf(handle)
	if mask != imageOpacityOf(restored) || mask == imageOpacityOf(decoded) {
		t.Fatal("image/framebuffer mask alias was lost")
	}
	mask.markOpaque(0, 0)
	if !imageOpacityOf(restored).opaqueAt(0, 0) || imageOpacityOf(decoded).opaqueAt(0, 0) {
		t.Fatal("restored mask mutation did not preserve sharing and independence")
	}
	if _, err := fresh.captureHeapState(roots); err != nil {
		t.Fatalf("recapture restored image: %v", err)
	}
}

func TestHeapImageColorModelsAndMalformedRecords(t *testing.T) {
	_, runtime := newTestRuntime(t)
	context := &heapNativeContext{runtime: runtime, opacityIDs: make(map[*imageOpacity]uint32), restored: []*imageOpacity{nil}}
	palette := color.Palette{color.NRGBA{R: 201, G: 7, B: 19, A: 0}, color.RGBA{R: 42, G: 21, B: 17, A: 93}, color.NRGBA64{R: 0x1234, G: 0x5678, B: 0x9abc, A: 0x1234}}
	bitmap := image.NewPaletted(image.Rect(0, 0, 3, 1), palette)
	copy(bitmap.Pix, []byte{0, 1, 2})
	data, err := context.captureImage(bitmap)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := context.restoreImage(data)
	if err != nil {
		t.Fatal(err)
	}
	for x := 0; x < 3; x++ {
		want, got := bitmap.At(x, 0), restored.At(x, 0)
		wr, wg, wb, wa := want.RGBA()
		gr, gg, gb, ga := got.RGBA()
		if wr != gr || wg != gg || wb != gb || wa != ga || color.NRGBAModel.Convert(want) != color.NRGBAModel.Convert(got) {
			t.Fatalf("pixel %d color contract differs", x)
		}
	}
	for _, change := range []func([]byte){
		func(b []byte) { b[8] = 0 }, func(b []byte) { b[16] = 1 },
		func(b []byte) { b[20] = 3 }, func(b []byte) { b[22] = 1 },
	} {
		bad := append([]byte(nil), data...)
		change(bad)
		if _, err := context.restoreImage(bad); err == nil {
			t.Fatal("malformed image record accepted")
		}
	}
	for n := range data {
		if _, err := context.restoreImage(data[:n]); err == nil {
			t.Fatalf("truncated image length %d accepted", n)
		}
	}
}

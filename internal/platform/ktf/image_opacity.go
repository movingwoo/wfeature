package ktf

import (
	stdimage "image"
	"image/color"
)

// Encoded images carry per-pixel transparency the guest framebuffers cannot:
// a PNG palette entry marked transparent by tRNS, or an alpha channel. Guest
// pixels are 16-bit colour with no alpha, so the transparency travels beside
// them as an imageOpacity and every draw path consults it. Without one a
// sprite paints its transparent border as whatever colour the encoding left
// there, which is the black box around a drawn character.
//
// Opacity is a yes/no answer rather than a blend: the assets these games ship
// mark a pixel either fully drawn or fully absent, and the 16-bit target has
// nowhere to keep a fraction.
type imageOpacity struct {
	width  int
	height int
	opaque []bool
}

// opaqueAt reports whether a source pixel is drawn. Coordinates outside the
// recorded extent count as drawn, so a mask never suppresses pixels a caller
// reads from beyond the image the encoding described.
func (opacity *imageOpacity) opaqueAt(x, y int) bool {
	if opacity == nil {
		return true
	}
	if x < 0 || y < 0 || x >= opacity.width || y >= opacity.height {
		return true
	}
	return opacity.opaque[y*opacity.width+x]
}

// region answers the opacity of one rectangle as a mask of that size, which is
// what a sub-image or a copy inherits from the image it came from.
func (opacity *imageOpacity) region(x, y, width, height int) *imageOpacity {
	if opacity == nil || width <= 0 || height <= 0 {
		return nil
	}
	result := &imageOpacity{width: width, height: height, opaque: make([]bool, width*height)}
	transparent := false
	for line := 0; line < height; line++ {
		for column := 0; column < width; column++ {
			drawn := opacity.opaqueAt(x+column, y+line)
			result.opaque[line*width+column] = drawn
			if !drawn {
				transparent = true
			}
		}
	}
	if !transparent {
		return nil
	}
	return result
}

// decodedImage pairs decoded pixels with the transparency their encoding
// declared. It is an image.Image itself, so the paths that only read colour
// keep treating it as the decoded image it wraps.
type decodedImage struct {
	stdimage.Image
	opacity *imageOpacity
}

// withOpacity records which pixels of a decoded image are drawn, wrapping it
// only when the encoding actually left some out. A fully opaque image is
// returned untouched so the common case costs nothing beyond the scan.
func withOpacity(decoded stdimage.Image) stdimage.Image {
	if decoded == nil {
		return nil
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return decoded
	}
	opacity := &imageOpacity{width: width, height: height, opaque: make([]bool, width*height)}
	transparent := false
	for line := 0; line < height; line++ {
		for column := 0; column < width; column++ {
			_, _, _, alpha := decoded.At(bounds.Min.X+column, bounds.Min.Y+line).RGBA()
			drawn := alpha != 0
			opacity.opaque[line*width+column] = drawn
			if !drawn {
				transparent = true
			}
		}
	}
	if !transparent {
		return decoded
	}
	return decodedImage{Image: decoded, opacity: opacity}
}

// imageOpacityOf answers the transparency a decoded image carries, or nil when
// every pixel is drawn.
func imageOpacityOf(decoded stdimage.Image) *imageOpacity {
	wrapped, ok := decoded.(decodedImage)
	if !ok {
		return nil
	}
	return wrapped.opacity
}

// setFramebufferOpacity records the transparency of one guest framebuffer, so
// blits out of it skip the pixels the source encoding left undrawn. A nil mask
// clears the record: the framebuffer is fully drawn.
func (runtime *initializationRuntime) setFramebufferOpacity(handle uint32, opacity *imageOpacity) {
	if runtime == nil {
		return
	}
	if opacity == nil {
		delete(runtime.framebufferOpacity, handle)
		return
	}
	if runtime.framebufferOpacity == nil {
		runtime.framebufferOpacity = make(map[uint32]*imageOpacity)
	}
	runtime.framebufferOpacity[handle] = opacity
}

// framebufferOpacityOf answers a framebuffer's recorded transparency, or nil
// when every one of its pixels draws.
func (runtime *initializationRuntime) framebufferOpacityOf(handle uint32) *imageOpacity {
	if runtime == nil {
		return nil
	}
	return runtime.framebufferOpacity[handle]
}

// markOpaque records that a pixel of a framebuffer was drawn into. A blit that
// skips its source's absent pixels leaves the destination's own pixels there,
// so only the pixels actually written change the destination's opacity.
func (opacity *imageOpacity) markOpaque(x, y int) {
	if opacity == nil {
		return
	}
	if x < 0 || y < 0 || x >= opacity.width || y >= opacity.height {
		return
	}
	opacity.opaque[y*opacity.width+x] = true
}

// blitOpacity carries the transparency of the two framebuffers a blit joins:
// the source's absent pixels are skipped, and the destination's own mask
// records the pixels the blit drew.
type blitOpacity struct {
	source      *imageOpacity
	destination *imageOpacity
}

// imagePixel565 answers a decoded pixel in the guest's 16-bit colour, keeping
// the colour of a pixel the encoding declared transparent.
//
// Go hands colours back premultiplied, so `RGBA()` reports a transparent pixel
// as black whatever the artist put there. The opacity mask above is what the
// platform's own draw paths consult, but a guest that reads these pixels
// directly — through MC_GrpImage's framebuffer — has only the colour, and a
// title that keys on its art's declared transparent colour needs to find it.
// See lgt.md, "A transparent pixel still has a colour", where the same
// collapse put a solid box behind every sprite on a screen.
func imagePixel565(decoded stdimage.Image, x, y int) uint16 {
	pixel := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
	return uint16(pixel.R>>3)<<11 | uint16(pixel.G>>2)<<5 | uint16(pixel.B>>3)
}

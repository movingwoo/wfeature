package wipicrt

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
type ImageOpacity struct {
	Width  int
	Height int
	Opaque []bool
}

// OpaqueAt reports whether a source pixel is drawn. Coordinates outside the
// recorded extent count as drawn, so a mask never suppresses pixels a caller
// reads from beyond the image the encoding described.
func (opacity *ImageOpacity) OpaqueAt(x, y int) bool {
	if opacity == nil {
		return true
	}
	if x < 0 || y < 0 || x >= opacity.Width || y >= opacity.Height {
		return true
	}
	return opacity.Opaque[y*opacity.Width+x]
}

// Region answers the opacity of one rectangle as a mask of that size, which is
// what a sub-image or a copy inherits from the image it came from.
func (opacity *ImageOpacity) Region(x, y, width, height int) *ImageOpacity {
	if opacity == nil || width <= 0 || height <= 0 {
		return nil
	}
	result := &ImageOpacity{Width: width, Height: height, Opaque: make([]bool, width*height)}
	transparent := false
	for line := 0; line < height; line++ {
		for column := 0; column < width; column++ {
			drawn := opacity.OpaqueAt(x+column, y+line)
			result.Opaque[line*width+column] = drawn
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

// DecodedImage pairs decoded pixels with the transparency their encoding
// declared. It is an image.Image itself, so the paths that only read colour
// keep treating it as the decoded image it wraps.
type DecodedImage struct {
	stdimage.Image
	Opacity *ImageOpacity
}

// WithOpacity records which pixels of a decoded image are drawn, wrapping it
// only when the encoding actually left some out. A fully opaque image is
// returned untouched so the common case costs nothing beyond the scan.
func WithOpacity(decoded stdimage.Image) stdimage.Image {
	if decoded == nil {
		return nil
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return decoded
	}
	opacity := &ImageOpacity{Width: width, Height: height, Opaque: make([]bool, width*height)}
	transparent := false
	for line := 0; line < height; line++ {
		for column := 0; column < width; column++ {
			_, _, _, alpha := decoded.At(bounds.Min.X+column, bounds.Min.Y+line).RGBA()
			drawn := alpha != 0
			opacity.Opaque[line*width+column] = drawn
			if !drawn {
				transparent = true
			}
		}
	}
	if !transparent {
		return decoded
	}
	return DecodedImage{Image: decoded, Opacity: opacity}
}

// ImageOpacityOf answers the transparency a decoded image carries, or nil when
// every pixel is drawn.
func ImageOpacityOf(decoded stdimage.Image) *ImageOpacity {
	wrapped, ok := decoded.(DecodedImage)
	if !ok {
		return nil
	}
	return wrapped.Opacity
}

// SetFramebufferOpacity records the transparency of one guest framebuffer, so
// blits out of it skip the pixels the source encoding left undrawn. A nil mask
// clears the record: the framebuffer is fully drawn.
func (runtime *Runtime) SetFramebufferOpacity(handle uint32, opacity *ImageOpacity) {
	if runtime == nil {
		return
	}
	if opacity == nil {
		delete(runtime.FramebufferOpacity, handle)
		return
	}
	if runtime.FramebufferOpacity == nil {
		runtime.FramebufferOpacity = make(map[uint32]*ImageOpacity)
	}
	runtime.FramebufferOpacity[handle] = opacity
}

// FramebufferOpacityOf answers a framebuffer's recorded transparency, or nil
// when every one of its pixels draws.
func (runtime *Runtime) FramebufferOpacityOf(handle uint32) *ImageOpacity {
	if runtime == nil {
		return nil
	}
	return runtime.FramebufferOpacity[handle]
}

// MarkOpaque records that a pixel of a framebuffer was drawn into. A blit that
// skips its source's absent pixels leaves the destination's own pixels there,
// so only the pixels actually written change the destination's opacity.
func (opacity *ImageOpacity) MarkOpaque(x, y int) {
	if opacity == nil {
		return
	}
	if x < 0 || y < 0 || x >= opacity.Width || y >= opacity.Height {
		return
	}
	opacity.Opaque[y*opacity.Width+x] = true
}

// BlitOpacity carries the transparency of the two framebuffers a blit joins:
// the source's absent pixels are skipped, and the destination's own mask
// records the pixels the blit drew.
type BlitOpacity struct {
	Source      *ImageOpacity
	Destination *ImageOpacity
}

// ImagePixel565 answers a decoded pixel in the guest's 16-bit colour, keeping
// the colour of a pixel the encoding declared transparent.
//
// Go hands colours back premultiplied, so `RGBA()` reports a transparent pixel
// as black whatever the artist put there. The opacity mask above is what the
// platform's own draw paths consult, but a guest that reads these pixels
// directly — through MC_GrpImage's framebuffer — has only the colour, and a
// title that keys on its art's declared transparent colour needs to find it.
// See docs/history/lgt.md, "A transparent pixel still has a colour", where the same
// collapse put a solid box behind every sprite on a screen.
func ImagePixel565(decoded stdimage.Image, x, y int) uint16 {
	pixel := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
	return uint16(pixel.R>>3)<<11 | uint16(pixel.G>>2)<<5 | uint16(pixel.B>>3)
}

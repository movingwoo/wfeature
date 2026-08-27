package lgt

import (
	stdimage "image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// An MC_GrpImage here is a framebuffer with a transparency mask beside it.
// That is not a shortcut: MC_grpGetImageFrameBuffer exists precisely because
// an image is one, and the title that reaches this asks for the framebuffer
// and then for its raw pointer, so an image that was not addressable memory
// would fail at that call instead of this one. Sharing the handle space keeps
// MC_grpGetFrameBuffer* working on an image without a second lookup.
//
// MC_grpCreateImage(MC_GrpImage* out, M_Uint32 bufID, off, len) writes the new
// image through its first argument and returns a status. Getting that backwards
// is silent: the caller reads whatever its stack slot held and carries on with
// a handle that was never one, which is what the failing title did for a
// hundred instructions before it asked to allocate a negative number of bytes.
const (
	// imageDone is MC_GRP_IMAGE_DONE: the source has been decoded and no
	// further frames remain. Everything here decodes in one pass, so it is
	// both what a create answers and what a request for the next frame does.
	imageDone int32 = 1
	// imageBadFormat is M_E_BADFORMAT, for data that is not an image this
	// build decodes.
	imageBadFormat int32 = -20
)

// Image property indices. The specification lists animation flags first and
// the dimensions after them; four and five are width and height, which is what
// the titles on both WIPI platforms here read them as.
const (
	imagePropertyAnimated  = 0
	imagePropertyDelay     = 1
	imagePropertyLoopCount = 2
	imagePropertyWidth     = 4
	imagePropertyHeight    = 5
	imagePropertyBPP       = 6
)

// maxImageBytes bounds one encoded image. The largest local title's title
// screen is under a hundred kilobytes; this leaves room without letting a
// corrupt length allocate the heap.
const maxImageBytes = 8 << 20

// createImage serves MC_grpCreateImage.
func (client *Client) createImage(thread *armcore.Thread) error {
	out, err := thread.Register(0)
	if err != nil {
		return err
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return err
	}
	offset, err := thread.Register(2)
	if err != nil {
		return err
	}
	length, err := thread.Register(3)
	if err != nil {
		return err
	}
	// A caller that passes no output pointer could not read the image back, so
	// there is nothing to create.
	if out == 0 || buffer == 0 || int32(length) <= 0 || uint32(length) > maxImageBytes {
		return answerCode(thread, imageBadFormat)
	}
	encoded := make([]byte, length)
	if err := client.core.Memory().Read(buffer+offset, encoded); err != nil {
		return answerCode(thread, imageBadFormat)
	}
	decoded, err := decodeImage(encoded)
	if err != nil {
		if client.logger != nil {
			prefix := encoded
			if len(prefix) > 8 {
				prefix = prefix[:8]
			}
			client.logger.Debug("LGT image cannot be decoded", "bytes", length, "prefix", prefix, "error", err)
		}
		// The out-parameter is cleared, because the specification says a failed
		// create leaves a null image there and a caller that checks the status
		// is not the only caller there is.
		if err := client.writeWord(out, 0); err != nil {
			return err
		}
		return answerCode(thread, imageBadFormat)
	}
	image, err := client.framebufferFromImage(decoded)
	if err != nil {
		return err
	}
	if err := client.writeWord(out, image.handle); err != nil {
		return err
	}
	return answerCode(thread, imageDone)
}

// framebufferFromImage converts a decoded image into a surface the guest can
// address, keeping the transparency the encoding declared beside the pixels:
// RGB565 has no alpha channel to carry it.
//
// **A transparent pixel keeps its own colour.** That is the whole reason the
// conversion is not a plain `RGBA()`: Go returns colours premultiplied, so
// every pixel an encoding declared transparent arrives as black whatever the
// artist put there, and the colour is the only thing a guest reading these
// pixels has to go on. These titles do not hand their sprites to the platform
// to blit — one takes `MC_grpGetImageFrameBuffer`, then the raw pointer, and
// runs its own blitter over the words, skipping the colour its own art
// declares transparent. Collapse that colour to black and the key never
// matches, so the blitter paints the sprite's whole rectangle: a character on
// a solid box instead of a character. See lgt.md, "A transparent pixel still
// has a colour".
func (client *Client) framebufferFromImage(decoded stdimage.Image) (*framebuffer, error) {
	bounds := decoded.Bounds()
	buffer, err := client.newFramebuffer(bounds.Dx(), bounds.Dy(), false)
	if err != nil {
		return nil, err
	}
	var opaque []bool
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			// NRGBA is the un-premultiplied form, which is what carries a
			// transparent pixel's colour through. For a palette entry the
			// conversion is the entry itself.
			pixel := color.NRGBAModel.Convert(decoded.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			buffer.pixels[y*bounds.Dx()+x] = uint16(pixel.R>>3)<<11 | uint16(pixel.G>>2)<<5 | uint16(pixel.B>>3)
			// Half is the threshold rather than "not fully opaque": these
			// encodings carry a one-bit mask in an eight-bit channel, and the
			// values that land between are the edges of an anti-aliased sprite.
			if pixel.A < 0x80 {
				if opaque == nil {
					opaque = make([]bool, len(buffer.pixels))
					for index := range opaque {
						opaque[index] = true
					}
				}
				opaque[y*bounds.Dx()+x] = false
			}
		}
	}
	buffer.opaque = opaque
	if err := client.syncToGuest(buffer); err != nil {
		return nil, err
	}
	return buffer, nil
}

// decodeImage decodes what a title packages. BMP and the handset's own bitmap
// are separate because the standard library registers neither. An image this
// build cannot read is reported rather than replaced with a blank, which a
// game would draw as a sprite that is simply invisible.
func decodeImage(encoded []byte) (stdimage.Image, error) {
	if len(encoded) >= 2 && encoded[0] == 'B' && encoded[1] == 'M' {
		// The header can name a transparent palette entry, which is the only
		// transparency a BMP has. See wipic.DecodeBitmap.
		return wipic.DecodeBitmap(encoded)
	}
	if wipic.IsLBMP(encoded) {
		// LCD pixels behind a 24-byte header. See wipic.DecodeLBMP.
		return wipic.DecodeLBMP(encoded)
	}
	// Everything the standard library registers, with the one tolerance a
	// title's own edit of a picture needs. See wipic.DecodeStandard.
	decoded, _, err := wipic.DecodeStandard(encoded)
	return decoded, err
}

// decodeNextImage serves MC_grpDecodeNextImage, which walks an animated
// source a frame at a time. Nothing here decodes an animated encoding, so an
// image is whole as soon as it is created and this reports that there is no
// next frame — which is the answer for the still images the local titles load,
// and the one a caller stops looping on.
func (client *Client) decodeNextImage(thread *armcore.Thread) error {
	handle, err := thread.Register(0)
	if err != nil {
		return err
	}
	if client.framebuffer(handle) == nil {
		return answerCode(thread, imageBadFormat)
	}
	return answerCode(thread, imageDone)
}

// destroyImage serves MC_grpDestroyImage. An image is a framebuffer, so this
// is MC_grpDestroyOffScreenFrameBuffer with the LCD guarded the same way.
func (client *Client) destroyImage(thread *armcore.Thread) error {
	handle, err := thread.Register(0)
	if err != nil {
		return err
	}
	client.releaseSurface(client.framebuffer(handle))
	return answerCode(thread, wipiSuccess)
}

// imageProperty serves MC_grpGetImageProperty.
func (client *Client) imageProperty(thread *armcore.Thread) error {
	handle, err := thread.Register(0)
	if err != nil {
		return err
	}
	property, err := thread.Register(1)
	if err != nil {
		return err
	}
	buffer := client.framebuffer(handle)
	if buffer == nil {
		return answerCode(thread, wipiError)
	}
	switch property {
	case imagePropertyWidth:
		return thread.SetRegister(0, uint32(buffer.width))
	case imagePropertyHeight:
		return thread.SetRegister(0, uint32(buffer.height))
	case imagePropertyBPP:
		return thread.SetRegister(0, 16)
	case imagePropertyAnimated, imagePropertyDelay, imagePropertyLoopCount:
		// Nothing here decodes an animated encoding, so every image is one
		// frame. See the animation entry in the deliberate omissions.
		return thread.SetRegister(0, 0)
	}
	return thread.SetRegister(0, 0)
}

// drawImage blits an image region, skipping the pixels its encoding declared
// transparent. It is MC_grpCopyFrameBuffer's argument list exactly, and the
// mask is the whole difference between the two.
func (client *Client) drawImage(context *graphicsContext, values []int32) error {
	destinationX, destinationY := int(values[0]), int(values[1])
	width, height := int(values[2]), int(values[3])
	source := client.framebuffer(uint32(values[4]))
	sourceX, sourceY := int(values[5]), int(values[6])
	if source == nil || width <= 0 || height <= 0 {
		return nil
	}
	// An image the guest wrote through its own framebuffer pointer is read
	// back first, the same as any other source surface — over the rows this
	// blit reads, which is what the destination's own band already does.
	if err := client.syncRowsFromGuest(source, rowsAt(sourceY, height)); err != nil {
		return err
	}
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			sx, sy := sourceX+column, sourceY+row
			if sx < 0 || sy < 0 || sx >= source.width || sy >= source.height {
				continue
			}
			index := sy*source.width + sx
			if source.opaque != nil {
				// The encoding said which pixels are transparent, and that is
				// the answer: a picture that carries a mask of its own is
				// allowed to draw in the mask colour, and keying it here would
				// punch holes in art the artist chose.
				if !source.opaque[index] {
					continue
				}
				context.put(destinationX+column, destinationY+row, source.pixels[index])
				continue
			}
			// With no declared transparency the mask colour is the only
			// transparency the surface has. This is the case for the surfaces
			// a title builds itself, which is where it matters.
			pixel := source.pixels[index]
			if pixel == maskPixel {
				continue
			}
			context.put(destinationX+column, destinationY+row, pixel)
		}
	}
	return nil
}

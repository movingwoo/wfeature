package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// handleDraw services the graphics slots that write pixels.
//
// Every one of them starts with the destination surface and ends with a
// pointer to the game's own MC_GrpContext — `MC_grpFillRect(dst, x, y, w, h,
// pgc)` — and the colour comes out of that context rather than from an
// argument of its own. Reading the first argument as a context and the last as
// a colour is what a title's fills looked like here before: the lookup missed,
// the call was refused, and every rectangle a game asked for went nowhere.
func (client *Client) handleDraw(ctx context.Context, thread *armcore.Thread, slot uint32) error {
	// Argument N is register N for the first four and then the stack, which is
	// the ARM procedure call standard.
	argument := func(index int) (int32, error) {
		if index < 4 {
			value, regErr := thread.Register(index)
			return int32(value), regErr
		}
		stack, regErr := thread.Register(armcore.RegisterSP)
		if regErr != nil {
			return 0, regErr
		}
		word, readErr := client.readWord(stack + uint32(index-4)*4)
		return int32(word), readErr
	}

	surface, err := argument(0)
	if err != nil {
		return err
	}
	target := client.framebuffer(uint32(surface))
	if target == nil {
		return answerCode(thread, wipiError)
	}
	values, err := readArguments(argument, drawArgumentCount(slot))
	if err != nil {
		return err
	}
	// The context is the last argument of every one of these but the pixel
	// read, which has none.
	contextPointer := uint32(0)
	if slot != slotGetRGBPixels {
		contextPointer = uint32(values[len(values)-1])
	}
	drawContext, err := client.contextFor(ctx, thread, target, contextPointer)
	if err != nil {
		return err
	}
	// A Clet may have written the surface directly since the last call, so
	// the runtime's copy is refreshed before it draws and written back after.
	if err := client.syncFromGuest(target); err != nil {
		return err
	}

	switch slot {
	case slotPutPixel:
		drawContext.put(int(values[1]), int(values[2]), drawContext.foreground)

	case slotDrawLine:
		drawContext.line(int(values[1]), int(values[2]), int(values[3]), int(values[4]), drawContext.foreground)

	case slotDrawRect, slotFillRect:
		if slot == slotFillRect {
			drawContext.fill(int(values[1]), int(values[2]), int(values[3]), int(values[4]), drawContext.foreground)
		} else {
			drawContext.rect(int(values[1]), int(values[2]), int(values[3]), int(values[4]), drawContext.foreground)
		}

	case slotDrawString:
		text, err := client.readString(uint32(values[3]), values[4])
		if err != nil {
			return err
		}
		drawContext.text(int(values[1]), int(values[2]), text, drawContext.foreground)

	case slotCopyArea:
		client.copyArea(drawContext, values[1:7])

	case slotCopyFramebuffer:
		if err := client.copyFramebuffer(drawContext, values[1:8]); err != nil {
			return err
		}

	case slotDrawImage:
		if err := client.drawImage(drawContext, values[1:8]); err != nil {
			return err
		}

	case slotGetRGBPixels, slotSetRGBPixels:
		if err := client.transferRGBPixels(drawContext, slot, values[1:7]); err != nil {
			return err
		}

	case slotDrawPolygon, slotFillPolygon:
		xs, ys, err := client.readPolygonPoints(uint32(values[1]), uint32(values[2]), values[3])
		if err != nil {
			return err
		}
		drawContext.polygon(xs, ys, drawContext.foreground, slot == slotFillPolygon)
	}

	// A pixel operation runs guest code, which can fail; put has no way to say
	// so while a draw is in flight.
	if drawContext.err != nil {
		return drawContext.err
	}
	if err := client.syncToGuest(target); err != nil {
		return err
	}
	return answerCode(thread, wipiSuccess)
}

// drawArgumentCount is how many arguments a draw slot takes, the destination
// surface included.
func drawArgumentCount(slot uint32) int {
	switch slot {
	case slotPutPixel:
		return 4
	case slotDrawLine, slotDrawRect, slotFillRect, slotDrawString:
		return 6
	case slotCopyArea:
		return 8
	case slotCopyFramebuffer, slotDrawImage:
		return 9
	case slotGetRGBPixels:
		return 7
	case slotSetRGBPixels:
		return 8
	case slotDrawPolygon, slotFillPolygon:
		return 5
	}
	return 4
}

// maxPolygonPoints bounds a polygon the guest describes. The count arrives as
// a word from the game and the arrays behind it are the game's own memory, so
// a wrong one has to cost a refused call rather than a read of the whole
// address space.
const maxPolygonPoints = 1024

// readPolygonPoints reads the parallel M_Int32 coordinate arrays a polygon
// call names. The specification passes the vertices as two arrays and a count
// rather than as points, which is the shape both the C and the Java sides use.
func (client *Client) readPolygonPoints(xPointer, yPointer uint32, count int32) ([]int, []int, error) {
	if count <= 0 {
		return nil, nil, nil
	}
	if count > maxPolygonPoints {
		return nil, nil, fmt.Errorf("LGT polygon point count %d exceeds %d", count, maxPolygonPoints)
	}
	xs := make([]int, count)
	ys := make([]int, count)
	for index := int32(0); index < count; index++ {
		x, err := client.readWord(xPointer + uint32(index)*4)
		if err != nil {
			return nil, nil, err
		}
		y, err := client.readWord(yPointer + uint32(index)*4)
		if err != nil {
			return nil, nil, err
		}
		xs[index], ys[index] = int(int32(x)), int(int32(y))
	}
	return xs, ys, nil
}

// readString reads the string a draw call names. A length of -1 means the
// string is terminated rather than counted, which is how the shared WIPI
// surface spells "measure it yourself".
func (client *Client) readString(pointer uint32, length int32) (string, error) {
	if pointer == 0 {
		return "", nil
	}
	if length < 0 {
		return client.readCText(pointer)
	}
	if length == 0 {
		return "", nil
	}
	text := make([]byte, length)
	if err := client.core.Memory().Read(pointer, text); err != nil {
		return "", err
	}
	return decodeEUCKR(text), nil
}

// readCText is readCString for a run of text rather than for a name: the bytes
// a handset draws are EUC-KR, and rendering them as themselves puts a
// codepoint-marked box on the screen for every half of every syllable. One
// title's whole notice screen read that way. See decodeEUCKR for why the plain
// reader does not do this.
func (client *Client) readCText(address uint32) (string, error) {
	text, err := client.readCString(address)
	if err != nil {
		return "", err
	}
	return decodeEUCKR([]byte(text)), nil
}

func readArguments(read func(int) (int32, error), count int) ([]int32, error) {
	values := make([]int32, count)
	for index := 0; index < count; index++ {
		value, err := read(index)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

// copyArea moves a rectangle inside one surface. The copy goes through a
// snapshot so an overlapping move does not read pixels it has already
// written.
func (client *Client) copyArea(context *graphicsContext, values []int32) {
	destinationX, destinationY := int(values[0]), int(values[1])
	width, height := int(values[2]), int(values[3])
	sourceX, sourceY := int(values[4]), int(values[5])
	if context.target == nil || width <= 0 || height <= 0 {
		return
	}
	snapshot := append([]uint16(nil), context.target.pixels...)
	stride := context.target.width
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			sx, sy := sourceX+column, sourceY+row
			if sx < 0 || sy < 0 || sx >= context.target.width || sy >= context.target.height {
				continue
			}
			context.put(destinationX+column, destinationY+row, snapshot[sy*stride+sx])
		}
	}
}

// maskPixel is the colour a surface copy leaves behind: pure magenta, which
// RGB565 spells 0xf81f. It is the transparent colour this API is used with,
// and it is a property of the platform rather than of a call — nothing in
// MC_grpCopyFrameBuffer's arguments or in MC_GrpContext carries one, and the
// titles here never set the context's transparency field.
//
// What settles it is what a caller has to be able to do. One engine renders
// text by clearing a strip offscreen to this colour, drawing the glyph into
// it through the framebuffer pointer, and copying the glyph's cell onto a
// background it must not erase; its outlined-text path is the same copy run
// four more times at one-pixel offsets. An opaque copy would put a magenta
// box behind every character on every screen, and the game has no other blit
// to reach for.
const maskPixel = uint16(0xf81f)

// copyFramebuffer blits a rectangle of another surface into this one, leaving
// the mask colour behind:
// `MC_grpCopyFrameBuffer(dst, dx, dy, w, h, src, sx, sy, pgc)`.
func (client *Client) copyFramebuffer(context *graphicsContext, values []int32) error {
	destinationX, destinationY := int(values[0]), int(values[1])
	width, height := int(values[2]), int(values[3])
	source := client.framebuffer(uint32(values[4]))
	sourceX, sourceY := int(values[5]), int(values[6])
	if source == nil || width <= 0 || height <= 0 {
		return nil
	}
	if err := client.syncFromGuest(source); err != nil {
		return err
	}
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			sx, sy := sourceX+column, sourceY+row
			if sx < 0 || sy < 0 || sx >= source.width || sy >= source.height {
				continue
			}
			pixel := source.pixels[sy*source.width+sx]
			if pixel == maskPixel {
				continue
			}
			context.put(destinationX+column, destinationY+row, pixel)
		}
	}
	return nil
}

// transferRGBPixels moves a rectangle between a guest int array and the
// surface, in the 0x00RRGGBB form the WIPI spec uses:
// `MC_grpGetRGBPixels(dst, x, y, w, h, pd, ipl)`.
func (client *Client) transferRGBPixels(context *graphicsContext, slot uint32, values []int32) error {
	x, y := int(values[0]), int(values[1])
	width, height := int(values[2]), int(values[3])
	pointer := uint32(values[4])
	bytesPerLine := int(values[5])
	if pointer == 0 || width <= 0 || height <= 0 {
		return nil
	}
	if bytesPerLine <= 0 {
		bytesPerLine = width * 4
	}
	if uint64(width)*uint64(height) > maxPixelTransfer {
		return fmt.Errorf("LGT pixel transfer of %dx%d exceeds the limit", width, height)
	}
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			address := pointer + uint32(row*bytesPerLine+column*4)
			if slot == slotGetRGBPixels {
				pixel := uint16(0)
				if context.target != nil {
					sx, sy := x+column, y+row
					if sx >= 0 && sy >= 0 && sx < context.target.width && sy < context.target.height {
						pixel = context.target.pixels[sy*context.target.width+sx]
					}
				}
				red, green, blue := unpack565(pixel)
				if err := client.writeWord(address, red<<16|green<<8|blue); err != nil {
					return err
				}
				continue
			}
			word, err := client.readWord(address)
			if err != nil {
				return err
			}
			context.put(x+column, y+row, rgb565(word>>16&0xff, word>>8&0xff, word&0xff))
		}
	}
	return nil
}

const maxPixelTransfer = 1 << 22

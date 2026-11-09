package lgt

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A Graphics' pixels read and written as numbers: `getPixel`, the
// `getRGBPixels`/`setRGBPixels` pair over an `int[]` of 0x00RRGGBB words, and
// the `getPixels`/`setPixels` pair over a `byte[]` in the surface's own
// format.
//
// **These answer what KTF's Graphics answers, on this platform's surfaces.**
// The rectangle is taken from the Graphics' translated origin; the clip, the
// alpha and the XOR mode do not apply; a pixel outside the surface reads as
// zero and takes no write. The specification says the two writes are clipped
// and the two reads are not, and KTF clips neither, so neither is clipped
// here.
//
// The pixels read are the runtime's copy of the surface, which is the correct
// one for a Java title (see javaDraw), and a write marks the surface drawn
// here the way a draw does, so a later blit does not read it back out of guest
// memory over what was written.
//
// What the guest hands over is bounded before anything is read: the array has
// to be one this platform issued, of the element type the call names, and the
// rectangle has to fit inside it. A null array is the NullPointerException and
// a rectangle the array cannot hold the ArrayIndexOutOfBoundsException, which
// the specification names for all four and KTF's RGB pair throws.

// rgbOfPixel is an LCD pixel as the 0x00RRGGBB word the Java calls answer,
// each channel widened the way MC_grpGetRGBFromPixel widens it.
func rgbOfPixel(pixel uint16) uint32 {
	red, green, blue := unpack565(pixel)
	return red<<16 | green<<8 | blue
}

// javaPixelTarget answers a Graphics' state and the surface it draws on.
func (client *Client) javaPixelTarget(object uint32) (*javaGraphics, *framebuffer, error) {
	state, err := client.javaGraphicsState(object)
	if err != nil {
		return nil, nil, err
	}
	target := client.framebuffer(state.surface)
	if target == nil {
		return nil, nil, fmt.Errorf("the surface %#x is not one this platform holds", state.surface)
	}
	return state, target, nil
}

// javaTypedArray answers where an array of the named type keeps its elements
// and how many it has. A null array is the NullPointerException; an object
// that is not an array of that type is a defect of the caller's rather than
// the language's, and is reported as one.
func (client *Client) javaTypedArray(
	thread *armcore.Thread, object uint32, descriptor, where string,
) (javaArrayView, error) {
	if object == 0 {
		return javaArrayView{}, client.throwJavaPlatform(thread, javaThrowNullClass, ": "+where)
	}
	class, err := client.javaArrayClassOf(object)
	if err != nil {
		return javaArrayView{}, err
	}
	if class.Name != descriptor {
		return javaArrayView{}, fmt.Errorf("%s was handed a %s where it takes a %s", where, class.Name, descriptor)
	}
	return client.javaArrayElements(object)
}

// javaIntElements reads the first count elements of an `int[]`, which the
// caller has bounded by the array's own length.
func (client *Client) javaIntElements(view javaArrayView, count uint32) ([]int32, error) {
	if count > view.Length {
		return nil, fmt.Errorf("%d elements of an array of %d", count, view.Length)
	}
	data := make([]byte, uint64(count)*4)
	if err := client.core.Memory().Read(view.Block+javaArrayLengthWords*4, data); err != nil {
		return nil, err
	}
	values := make([]int32, count)
	for index := range values {
		values[index] = int32(binary.LittleEndian.Uint32(data[index*4:]))
	}
	return values, nil
}

// pixelRow is the part of one row of a rectangle that lies on a surface: the
// surface row, and the columns of the rectangle from first up to last.
type pixelRow struct {
	y           int64
	first, last int64
	inside      bool
}

// pixelRowOn places row `line` of a rectangle whose top left is (x, y) in
// surface coordinates and that is `width` wide.
func pixelRowOn(target *framebuffer, x, y, width, line int64) pixelRow {
	row := pixelRow{y: y + line}
	if row.y < 0 || row.y >= int64(target.height) {
		return row
	}
	row.first = max(0, -x)
	row.last = min(width, int64(target.width)-x)
	row.inside = row.first < row.last
	return row
}

// javaGetPixel is `getPixel(x, y)`: the pixel at that point of the Graphics'
// translated coordinates as 0x00RRGGBB, and zero off the surface, which is
// what KTF answers for one it cannot read.
func javaGetPixel(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	state, target, err := client.javaPixelTarget(arguments[0])
	if err != nil {
		return 0, err
	}
	x := int64(int32(arguments[1])) + int64(state.translateX)
	y := int64(int32(arguments[2])) + int64(state.translateY)
	if x < 0 || y < 0 || x >= int64(target.width) || y >= int64(target.height) {
		return 0, nil
	}
	return rgbOfPixel(target.pixels[y*int64(target.width)+x]), nil
}

// javaTransferRGBPixels is `getRGBPixels` and `setRGBPixels(x, y, w, h,
// pixels, offset, bpl)`: a rectangle of the surface as 0x00RRGGBB words, row
// by row from `offset` in the array.
//
// **The last argument is bytes per line**, as the specification says and KTF
// reads it, so one row starts bpl/4 elements after the one before; zero means
// rows of exactly w. A line too short to hold a row moves nothing, which is
// KTF's answer and the C call's specification. A rectangle with no area moves
// nothing either. A read writes the whole row of the array, zero where the
// rectangle is off the surface.
func javaTransferRGBPixels(write bool) func(
	*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(
		client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
	) (uint32, error) {
		where := "getRGBPixels"
		if write {
			where = "setRGBPixels"
		}
		state, target, err := client.javaPixelTarget(arguments[0])
		if err != nil {
			return 0, err
		}
		if arguments[5] == 0 {
			return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": "+where)
		}
		width, height := int64(int32(arguments[3])), int64(int32(arguments[4]))
		if width <= 0 || height <= 0 {
			return 0, nil
		}
		view, err := client.javaTypedArray(thread, arguments[5], "[I", where)
		if err != nil {
			return 0, err
		}
		offset, bytesPerLine := int64(int32(arguments[6])), int32(arguments[7])
		pitch := width
		if bytesPerLine != 0 {
			pitch = int64(bytesPerLine / 4)
		}
		if pitch < width {
			return 0, nil
		}
		last := offset + (height-1)*pitch + width
		if offset < 0 || last > int64(view.Length) {
			return 0, client.throwJavaPlatform(thread, javaThrowArrayClass, fmt.Sprintf(
				": %s of %dx%d from %d at %d bytes a line in an array of %d",
				where, width, height, offset, bytesPerLine, view.Length))
		}
		left := int64(int32(arguments[1])) + int64(state.translateX)
		top := int64(int32(arguments[2])) + int64(state.translateY)
		memory := client.core.Memory()
		row := make([]byte, width*4)
		for line := range height {
			address := view.Block + javaArrayLengthWords*4 + uint32(offset+line*pitch)*4
			span := pixelRowOn(target, left, top, width, line)
			if write {
				if !span.inside {
					continue
				}
				if err := memory.Read(address+uint32(span.first)*4, row[span.first*4:span.last*4]); err != nil {
					return 0, err
				}
				for column := span.first; column < span.last; column++ {
					value := binary.LittleEndian.Uint32(row[column*4:])
					target.pixels[span.y*int64(target.width)+left+column] =
						rgb565(value>>16&0xff, value>>8&0xff, value&0xff)
				}
				continue
			}
			clear(row)
			if span.inside {
				for column := span.first; column < span.last; column++ {
					binary.LittleEndian.PutUint32(row[column*4:],
						rgbOfPixel(target.pixels[span.y*int64(target.width)+left+column]))
				}
			}
			if err := memory.Write(address, row); err != nil {
				return 0, err
			}
		}
		if write {
			target.drawnHere = true
		}
		return 0, nil
	}
}

// javaTransferPixels is `getPixels` and `setPixels(x, y, w, h, pixels,
// offset, bpl)`: the same rectangle the RGB pair moves, in the surface's own
// format rather than in 0x00RRGGBB words. The specification leaves the format
// to the handset and ties it to setPixel; the LCD here is 16 bits a pixel, so
// a pixel is its two bytes as they lie in the framebuffer, low byte first, and
// `bpl` is a byte pitch, which is how KTF's Graphics moves them too.
//
// A pitch of zero means rows of exactly w pixels. A pitch below zero, an
// offset below zero or a rectangle that runs past the end of the array is the
// ArrayIndexOutOfBoundsException the specification names, where KTF stops the
// title, and a null array is the NullPointerException, where KTF stops too.
//
// **A line too short to hold a row moves nothing**, as it does for the RGB
// pair. KTF moves such rows over one another; here that would be work in the
// square of the array's length done in host code, where no instruction budget
// reaches, for a call no title has a reason to make.
func javaTransferPixels(write bool) func(
	*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(
		client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
	) (uint32, error) {
		where := "getPixels"
		if write {
			where = "setPixels"
		}
		state, target, err := client.javaPixelTarget(arguments[0])
		if err != nil {
			return 0, err
		}
		if arguments[5] == 0 {
			return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": "+where)
		}
		width, height := int64(int32(arguments[3])), int64(int32(arguments[4]))
		if width <= 0 || height <= 0 {
			return 0, nil
		}
		view, err := client.javaTypedArray(thread, arguments[5], "[B", where)
		if err != nil {
			return 0, err
		}
		offset, pitch := int64(int32(arguments[6])), int64(int32(arguments[7]))
		if pitch == 0 {
			pitch = width * 2
		}
		if pitch >= 0 && pitch < width*2 {
			return 0, nil
		}
		last := offset + (height-1)*pitch + width*2
		if offset < 0 || pitch < 0 || last > int64(view.Length) {
			return 0, client.throwJavaPlatform(thread, javaThrowArrayClass, fmt.Sprintf(
				": %s of %dx%d from %d at %d bytes a line in an array of %d",
				where, width, height, offset, pitch, view.Length))
		}
		left := int64(int32(arguments[1])) + int64(state.translateX)
		top := int64(int32(arguments[2])) + int64(state.translateY)
		memory := client.core.Memory()
		row := make([]byte, width*2)
		for line := range height {
			address := view.Block + javaArrayLengthWords*4 + uint32(offset+line*pitch)
			span := pixelRowOn(target, left, top, width, line)
			if write {
				if !span.inside {
					continue
				}
				if err := memory.Read(address+uint32(span.first)*2, row[span.first*2:span.last*2]); err != nil {
					return 0, err
				}
				for column := span.first; column < span.last; column++ {
					target.pixels[span.y*int64(target.width)+left+column] =
						binary.LittleEndian.Uint16(row[column*2:])
				}
				continue
			}
			clear(row)
			if span.inside {
				for column := span.first; column < span.last; column++ {
					binary.LittleEndian.PutUint16(row[column*2:],
						target.pixels[span.y*int64(target.width)+left+column])
				}
			}
			if err := memory.Write(address, row); err != nil {
				return 0, err
			}
		}
		if write {
			target.drawnHere = true
		}
		return 0, nil
	}
}

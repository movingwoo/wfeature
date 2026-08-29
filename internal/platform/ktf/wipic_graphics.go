package ktf

import (
	"encoding/binary"
	"fmt"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"unicode/utf16"

	// KTF archives carry BMP assets, including ones renamed to .dat.

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/curve"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// wipicFramebuffer mirrors the guest MC_GrpFrameBuffer record. Buffer is the
// indirect allocation handle whose payload holds the pixels.
type wipicFramebuffer struct {
	width  uint32
	height uint32
	bpl    uint32
	bpp    uint32
	buffer uint32
	pixels uint32
}

const maxWIPICFramebufferBytes = 8 << 20

// maxWIPICFramebufferSide bounds one dimension of a surface this platform
// creates. It is a guard on the arithmetic rather than a policy: what a
// framebuffer may cost is maxWIPICFramebufferBytes, and a picture that is very
// wide and one row tall costs almost nothing.
const maxWIPICFramebufferSide = 1 << 16

// readWIPICDrawSurface reads a frame buffer a drawing call was handed, and
// reports whether there is one at all. This API is C, and the handle is a
// pointer: a null one is the game saying it has no surface there, not a
// corrupt one. It happens for real between scenes — a title that leaves its
// world for the main menu clears the image slots the scene owned while its
// own frame timer is still running, so the next frame paints from a slot that
// is now null, and the frame after that is the menu.
//
// Failing the call ends the session over a state the game leaves in one frame,
// which is how a title's own "return to the main menu" became a dead
// emulator. Nothing is drawn instead, which is what the specification already
// says for every other way a drawing call can have nothing to copy — a
// non-positive width or height, a source rectangle outside the source. A
// handle that is not null and not valid still fails: that one is a bug, and
// the counter here is what separates the two in a report.
func (runtime *initializationRuntime) readWIPICDrawSurface(handle uint32, call string) (wipicFramebuffer, bool, error) {
	if handle == 0 {
		runtime.countDiagnostic("grp " + call + " on a null surface")
		return wipicFramebuffer{}, false, nil
	}
	framebuffer, err := runtime.readWIPICFramebuffer(handle)
	if err != nil {
		// **A surface the guest destroyed and drew again is the guest's own
		// use-after-free, and on a handset it is not a failure.** The block's
		// pixels are still where they were until something else takes the
		// span, so the title draws its old picture and carries on; here the
		// arena has already reissued the memory, so the record decodes as
		// whatever now lives there. One title destroys an image and blits it
		// on a later frame, and the depth it read back was a heap address.
		// Drawing nothing is the honest version of undefined contents, and it
		// is what the specification already says for a source whose depth is
		// not the screen's.
		if runtime.wasReleasedWIPIC(handle) {
			runtime.countDiagnostic("grp " + call + " on a destroyed surface")
			return wipicFramebuffer{}, false, nil
		}
		// Which surface of the call, and which handle. A blit takes two, and a
		// record that does not decode says nothing about which argument was
		// misread without them — the same message for a destination and for a
		// source is a report that has to be reproduced before it can be read.
		return wipicFramebuffer{}, false, fmt.Errorf("%s %#x: %w", call, handle, err)
	}
	return framebuffer, true, nil
}

// readWIPICFramebuffer validates a framebuffer handle far enough that later
// row writes stay inside the mapped pixel allocation.
func (runtime *initializationRuntime) readWIPICFramebuffer(handle uint32) (wipicFramebuffer, error) {
	// An image names its framebuffer, and that framebuffer is a record of its
	// own; nothing here names a third. The bound is what stops a record the
	// guest wrote itself — one whose first word is its own handle — from
	// running this down until the Go stack ends.
	return runtime.readWIPICSurface(handle, 2)
}

func (runtime *initializationRuntime) readWIPICSurface(handle uint32, depth int) (wipicFramebuffer, error) {
	recordBase, err := runtime.readAOTWords(handle, 1, "framebuffer handle")
	if err != nil {
		return wipicFramebuffer{}, err
	}
	fields, err := runtime.readAOTWords(recordBase[0]+8, 5, "framebuffer record")
	if err != nil {
		return wipicFramebuffer{}, err
	}
	if inner, isImage := runtime.wipicImageFramebuffer(handle); isImage {
		if depth <= 0 {
			return wipicFramebuffer{}, fmt.Errorf("KTF image record at %#x names a framebuffer that names another", handle)
		}
		return runtime.readWIPICSurface(inner, depth-1)
	}
	framebuffer := wipicFramebuffer{
		width:  fields[0],
		height: fields[1],
		bpl:    fields[2],
		bpp:    fields[3],
		buffer: fields[4],
	}
	if framebuffer.bpp != 16 {
		return wipicFramebuffer{}, fmt.Errorf("KTF framebuffer depth %d is not supported (%s)", framebuffer.bpp, runtime.describeWIPICSurfaceWords(handle, recordBase[0], fields))
	}
	if framebuffer.width == 0 || framebuffer.height == 0 || framebuffer.bpl < framebuffer.width*2 {
		return wipicFramebuffer{}, fmt.Errorf("KTF framebuffer %dx%d bpl %d is invalid (%s)", framebuffer.width, framebuffer.height, framebuffer.bpl, runtime.describeWIPICSurfaceWords(handle, recordBase[0], fields))
	}
	if uint64(framebuffer.bpl)*uint64(framebuffer.height) > maxWIPICFramebufferBytes {
		return wipicFramebuffer{}, fmt.Errorf("KTF framebuffer %dx%d exceeds size limit", framebuffer.width, framebuffer.height)
	}
	bufferBase, err := runtime.readAOTWords(framebuffer.buffer, 1, "framebuffer pixel handle")
	if err != nil {
		return wipicFramebuffer{}, err
	}
	framebuffer.pixels = bufferBase[0] + 8
	return framebuffer, nil
}

// describeWIPICSurfaceWords prints the words a surface record was read from.
// A record that does not decode is a question about which structure the handle
// really points at, and the five numbers answer it where the one field that
// failed the check cannot: a depth that is a heap address says the fields are
// shifted or the handle is not this kind of record at all.
func (runtime *initializationRuntime) describeWIPICSurfaceWords(handle, recordBase uint32, fields []uint32) string {
	description := fmt.Sprintf("handle=%#x record=%#x words=%#x", handle, recordBase, fields)
	// The words the handle itself sits in say what kind of structure it is,
	// which the record's five fields cannot: a handle this platform issued
	// points four bytes into its own allocation, and one the guest built
	// points wherever the guest put it.
	if around, err := runtime.readAOTWords(handle, 8, "surface neighbourhood"); err == nil {
		description += fmt.Sprintf(" at=%#x", around)
	}
	// Whether the platform issued the handle, and whether the record's first
	// word is one it issued, is what separates "this is not an image record"
	// from "this is an image whose framebuffer this platform has forgotten".
	_, issuedHandle := runtime.wipicAllocations[handle]
	_, issuedFirst := runtime.wipicAllocations[fields[0]]
	description += fmt.Sprintf(" issued=%v first-issued=%v", issuedHandle, issuedFirst)
	return description
}

// wipicImageFramebuffer answers the framebuffer an MC_GrpImage record names,
// and reports false for a handle that is a framebuffer record itself.
//
// **An MC_GrpImage names its framebuffer rather than inlining it**, and which
// of the two a handle is is decided by the record's first word: a framebuffer
// record starts with a width, and an image record starts with a handle this
// platform issued. The two cannot be confused — a width is at most a couple of
// thousand and a handle is an arena address — and the test is the allocation
// table rather than the magnitude, so it is exact rather than a threshold.
//
// A title's own code is what settled the layout. It reads its image through
// the handset's macros: take the image handle, follow it to the record, read
// word zero as a framebuffer handle, follow *that* to its record and switch on
// the depth field — `(bpp << 24) >> 27`, which is 2 for the 16-bit surfaces
// here and 4 for 32-bit. With the framebuffer fields inlined, word zero was
// the width, and the title dereferenced 74 and faulted on its first frame.
func (runtime *initializationRuntime) wipicImageFramebuffer(handle uint32) (uint32, bool) {
	base, err := runtime.readAOTWords(handle, 1, "image handle")
	if err != nil {
		return 0, false
	}
	fields, err := runtime.readAOTWords(base[0]+8, 1, "image record")
	if err != nil {
		return 0, false
	}
	if _, issued := runtime.wipicAllocations[fields[0]]; !issued {
		return 0, false
	}
	return fields[0], true
}

// wipicClip is a graphics context's clipping rectangle, in the destination
// framebuffer's own coordinates: the top-left corner is inside it and the
// bottom-right corner is not, which is what the specification says.
//
// A context that has never been given one holds zeroes, because
// MC_grpInitContext zeroes the record and nothing there knows what framebuffer
// the context will be used against. An empty rectangle therefore means "no
// clip" rather than "draw nothing" — the reading that keeps a title which
// never sets one drawing exactly as it did.
type wipicClip struct {
	left, top, right, bottom int32
}

func (clip wipicClip) empty() bool {
	return clip.right <= clip.left || clip.bottom <= clip.top
}

// contains reports whether one pixel survives the clip.
func (clip wipicClip) contains(x, y int32) bool {
	if clip.empty() {
		return true
	}
	return x >= clip.left && x < clip.right && y >= clip.top && y < clip.bottom
}

// clipRect intersects a requested rectangle with the framebuffer bounds and
// the context's clip, and reports whether anything remains.
func (framebuffer wipicFramebuffer) clipRect(clip wipicClip, x, y, width, height int32) (int32, int32, int32, int32, bool) {
	if width <= 0 || height <= 0 {
		return 0, 0, 0, 0, false
	}
	right := int64(x) + int64(width)
	bottom := int64(y) + int64(height)
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if right > int64(framebuffer.width) {
		right = int64(framebuffer.width)
	}
	if bottom > int64(framebuffer.height) {
		bottom = int64(framebuffer.height)
	}
	if !clip.empty() {
		if int64(clip.left) > int64(x) {
			x = clip.left
		}
		if int64(clip.top) > int64(y) {
			y = clip.top
		}
		if int64(clip.right) < right {
			right = int64(clip.right)
		}
		if int64(clip.bottom) < bottom {
			bottom = int64(clip.bottom)
		}
	}
	if int64(x) >= right || int64(y) >= bottom {
		return 0, 0, 0, 0, false
	}
	return x, y, int32(right - int64(x)), int32(bottom - int64(y)), true
}

func (runtime *initializationRuntime) wipicReadContextForeground(contextAddress uint32) (uint32, error) {
	words, err := runtime.readAOTWords(contextAddress+12, 1, "graphics context foreground")
	if err != nil {
		return 0, err
	}
	return words[0], nil
}

// wipicReadContextClip reads the four 16-bit clip bounds out of the record.
// Every drawing call that takes a graphics context is clipped by it: one title
// draws a tile by blitting its whole 320x128 tile sheet at an offset and
// letting the clip cut the one cell out, so ignoring the clip painted the
// entire sheet over the scene several dozen times a frame.
func (runtime *initializationRuntime) wipicReadContextClip(contextAddress uint32) (wipicClip, error) {
	if contextAddress == 0 {
		return wipicClip{}, nil
	}
	words, err := runtime.readAOTWords(contextAddress+4, 2, "graphics context clip")
	if err != nil {
		return wipicClip{}, err
	}
	return wipicClip{
		left:   int32(int16(words[0])),
		top:    int32(int16(words[0] >> 16)),
		right:  int32(int16(words[1])),
		bottom: int32(int16(words[1] >> 16)),
	}, nil
}

// wipicRectangleCall reads the framebuffer, foreground pixel, and rectangle
// the MC_grpFillRect and MC_grpDrawRect argument shape shares. It reports
// whether there is a surface to draw on at all.
func (runtime *initializationRuntime) wipicRectangleCall(thread *armcore.Thread, call string) (wipicFramebuffer, uint32, wipicClip, wipicPixelOp, [4]int32, bool, error) {
	registers := make([]uint32, 6)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return wipicFramebuffer{}, 0, wipicClip{}, wipicPixelOp{}, [4]int32{}, false, err
		}
		registers[index] = value
	}
	framebuffer, ok, err := runtime.readWIPICDrawSurface(registers[0], call)
	if err != nil || !ok {
		return wipicFramebuffer{}, 0, wipicClip{}, wipicPixelOp{}, [4]int32{}, false, err
	}
	pixel, err := runtime.wipicReadContextForeground(registers[5])
	if err != nil {
		return wipicFramebuffer{}, 0, wipicClip{}, wipicPixelOp{}, [4]int32{}, false, err
	}
	clip, err := runtime.wipicReadContextClip(registers[5])
	if err != nil {
		return wipicFramebuffer{}, 0, wipicClip{}, wipicPixelOp{}, [4]int32{}, false, err
	}
	op, err := runtime.wipicReadContextPixelOp(registers[5])
	if err != nil {
		return wipicFramebuffer{}, 0, wipicClip{}, wipicPixelOp{}, [4]int32{}, false, err
	}
	rectangle := [4]int32{int32(registers[1]), int32(registers[2]), int32(registers[3]), int32(registers[4])}
	return framebuffer, pixel, clip, op, rectangle, true, nil
}

// fillFramebufferRect writes one solid rectangle, clipped to the framebuffer.
func (runtime *initializationRuntime) fillFramebufferRect(framebuffer wipicFramebuffer, clip wipicClip, op wipicPixelOp, pixel uint32, x, y, width, height int32) error {
	x, y, width, height, ok := framebuffer.clipRect(clip, x, y, width, height)
	if !ok {
		return nil
	}
	if op.active() {
		for line := int32(0); line < height; line++ {
			for column := int32(0); column < width; column++ {
				address := framebuffer.pixels + uint32(y+line)*framebuffer.bpl + uint32(x+column)*2
				if err := runtime.blendPixel(op, address, uint16(pixel)); err != nil {
					return fmt.Errorf("fill KTF framebuffer pixel %d,%d: %w", x+column, y+line, err)
				}
			}
		}
		return nil
	}
	row := make([]byte, int(width)*2)
	for column := int32(0); column < width; column++ {
		binary.LittleEndian.PutUint16(row[column*2:], uint16(pixel))
	}
	memory := runtime.client.core.Memory()
	for line := int32(0); line < height; line++ {
		address := framebuffer.pixels + uint32(y+line)*framebuffer.bpl + uint32(x)*2
		if err := memory.Write(address, row); err != nil {
			return fmt.Errorf("fill KTF framebuffer row %d: %w", line, err)
		}
	}
	return nil
}

// wipicFillRect implements MC_grpFillRect with the context foreground pixel.
func (runtime *initializationRuntime) wipicFillRect(thread *armcore.Thread) (uint32, error) {
	framebuffer, pixel, clip, op, rectangle, ok, err := runtime.wipicRectangleCall(thread, "fill rect")
	if err != nil || !ok {
		return 0, err
	}
	return 0, runtime.fillFramebufferRect(framebuffer, clip, op, pixel, rectangle[0], rectangle[1], rectangle[2], rectangle[3])
}

// wipicDrawRect implements MC_grpDrawRect.
func (runtime *initializationRuntime) wipicDrawRect(thread *armcore.Thread) (uint32, error) {
	framebuffer, pixel, clip, op, rectangle, ok, err := runtime.wipicRectangleCall(thread, "draw rect")
	if err != nil || !ok {
		return 0, err
	}
	return 0, runtime.outlineFramebufferRect(framebuffer, clip, op, pixel, rectangle[0], rectangle[1], rectangle[2], rectangle[3])
}

// wipicArcCall reads the argument shape MC_grpDrawArc and MC_grpFillArc share:
// the rectangle the arc is inscribed in, then its start angle and its extent.
//
//	void MC_grpDrawArc(MC_GrpFrameBuffer dst, M_Int32 x, M_Int32 y,
//	                   M_Int32 w, M_Int32 h, M_Int32 s, M_Int32 e,
//	                   MC_GrpContext *pgc)
//
// Eight arguments, so the last four arrive on the stack; wipicArgument already
// knows where. The context is the eighth rather than the sixth, which is the
// one thing that stops this from being the rectangle reader with two extra
// words on the end.
func (runtime *initializationRuntime) wipicArcCall(thread *armcore.Thread, call string) (wipicFramebuffer, uint32, wipicClip, wipicPixelOp, [4]int32, [2]int32, bool, error) {
	registers := make([]uint32, 8)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return wipicFramebuffer{}, 0, wipicClip{}, wipicPixelOp{}, [4]int32{}, [2]int32{}, false, err
		}
		registers[index] = value
	}
	framebuffer, ok, err := runtime.readWIPICDrawSurface(registers[0], call)
	if err != nil || !ok {
		return wipicFramebuffer{}, 0, wipicClip{}, wipicPixelOp{}, [4]int32{}, [2]int32{}, false, err
	}
	context := registers[7]
	pixel, err := runtime.wipicReadContextForeground(context)
	if err != nil {
		return wipicFramebuffer{}, 0, wipicClip{}, wipicPixelOp{}, [4]int32{}, [2]int32{}, false, err
	}
	clip, err := runtime.wipicReadContextClip(context)
	if err != nil {
		return wipicFramebuffer{}, 0, wipicClip{}, wipicPixelOp{}, [4]int32{}, [2]int32{}, false, err
	}
	op, err := runtime.wipicReadContextPixelOp(context)
	if err != nil {
		return wipicFramebuffer{}, 0, wipicClip{}, wipicPixelOp{}, [4]int32{}, [2]int32{}, false, err
	}
	bounds := [4]int32{int32(registers[1]), int32(registers[2]), int32(registers[3]), int32(registers[4])}
	angles := [2]int32{int32(registers[5]), int32(registers[6])}
	return framebuffer, pixel, clip, op, bounds, angles, true, nil
}

// wipicDrawArc implements MC_grpDrawArc: the curve alone, since the pie's two
// straight edges belong to the fill.
func (runtime *initializationRuntime) wipicDrawArc(thread *armcore.Thread) (uint32, error) {
	framebuffer, pixel, clip, op, bounds, angles, ok, err := runtime.wipicArcCall(thread, "draw arc")
	if err != nil || !ok {
		return 0, err
	}
	return 0, curve.DrawArc(bounds[0], bounds[1], bounds[2], bounds[3], angles[0], angles[1],
		runtime.framebufferCurveEmit(framebuffer, clip, op, pixel))
}

// wipicFillArc implements MC_grpFillArc: the pie slice the arc cuts out of its
// bounding ellipse.
func (runtime *initializationRuntime) wipicFillArc(thread *armcore.Thread) (uint32, error) {
	framebuffer, pixel, clip, op, bounds, angles, ok, err := runtime.wipicArcCall(thread, "fill arc")
	if err != nil || !ok {
		return 0, err
	}
	return 0, curve.FillArc(bounds[0], bounds[1], bounds[2], bounds[3], angles[0], angles[1],
		runtime.framebufferCurveEmit(framebuffer, clip, op, pixel))
}

// framebufferCurveEmit fills each span a curve hands out through the same
// clipped rectangle fill every other drawing call uses, so the context's clip
// and its pixel operation apply to a curve exactly as they do to a rectangle.
func (runtime *initializationRuntime) framebufferCurveEmit(framebuffer wipicFramebuffer, clip wipicClip, op wipicPixelOp, pixel uint32) curve.Emit {
	return func(span curve.Span) error {
		return runtime.fillFramebufferRect(framebuffer, clip, op, pixel, span.X, span.Y, span.Width, 1)
	}
}

// outlineFramebufferRect draws the border of the same rectangle
// fillFramebufferRect would fill, so the edges are the w by h box's own outer
// pixels rather than a ring one pixel outside it. A rectangle too thin to have
// an interior is the solid line it describes; drawing it as four edges would
// plot its pixels twice.
func (runtime *initializationRuntime) outlineFramebufferRect(framebuffer wipicFramebuffer, clip wipicClip, op wipicPixelOp, pixel uint32, x, y, width, height int32) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	if width <= 2 || height <= 2 {
		return runtime.fillFramebufferRect(framebuffer, clip, op, pixel, x, y, width, height)
	}
	edges := [4][4]int32{
		{x, y, width, 1},
		{x, y + height - 1, width, 1},
		{x, y + 1, 1, height - 2},
		{x + width - 1, y + 1, 1, height - 2},
	}
	for _, edge := range edges {
		if err := runtime.fillFramebufferRect(framebuffer, clip, op, pixel, edge[0], edge[1], edge[2], edge[3]); err != nil {
			return err
		}
	}
	return nil
}

// wipicReadString reads guest text for MC_grpDrawString variants. A null
// pointer draws nothing (one title lays out menus with one), a positive
// length reads that many bytes or UTF-16 units, and -1 reads to the
// terminator like the original runtime.
func (runtime *initializationRuntime) wipicReadString(pointer uint32, length int32, unicode bool) ([]rune, error) {
	if pointer == 0 || length == 0 || length < -1 {
		return nil, nil
	}
	memory := runtime.client.core.Memory()
	unit := uint32(1)
	if unicode {
		unit = 2
	}
	var data []byte
	if length > 0 {
		data = make([]byte, uint32(length)*unit)
		if err := memory.Read(pointer, data); err != nil {
			return nil, fmt.Errorf("read KTF draw string: %w", err)
		}
	} else {
		for offset := uint32(0); offset < 1024*unit; offset += unit {
			chunk := make([]byte, unit)
			if err := memory.Read(pointer+offset, chunk); err != nil {
				return nil, fmt.Errorf("read KTF draw string: %w", err)
			}
			if chunk[0] == 0 && (unit == 1 || chunk[1] == 0) {
				break
			}
			data = append(data, chunk...)
		}
	}
	if unicode {
		units := make([]uint16, len(data)/2)
		for index := range units {
			units[index] = binary.LittleEndian.Uint16(data[index*2:])
		}
		return utf16.Decode(units), nil
	}
	return []rune(decodeEUCKR(data)), nil
}

// wipicDrawString implements MC_grpDrawString and its unicode variant: text
// drawn with the context foreground pixel, the y coordinate at the baseline.
func (runtime *initializationRuntime) wipicDrawString(thread *armcore.Thread, unicode bool) (uint32, error) {
	registers := make([]uint32, 6)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	text, err := runtime.wipicReadString(registers[3], int32(registers[4]), unicode)
	if err != nil || len(text) == 0 {
		return 0, err
	}
	framebuffer, ok, err := runtime.readWIPICDrawSurface(registers[0], "draw string")
	if err != nil || !ok {
		return 0, err
	}
	pixel, err := runtime.wipicReadContextForeground(registers[5])
	if err != nil {
		return 0, err
	}
	clip, err := runtime.wipicReadContextClip(registers[5])
	if err != nil {
		return 0, err
	}
	op, err := runtime.wipicReadContextPixelOp(registers[5])
	if err != nil {
		return 0, err
	}
	memory := runtime.client.core.Memory()
	// Coverage rather than a bit per pixel, for the reason in glyph.Face: on
	// the 12-dot grid a glyph does not land on whole pixels, and rounding each
	// one to drawn or not draws it a stroke too heavy.
	plot := func(x, y int32, coverage uint8) error {
		if coverage == 0 || x < 0 || y < 0 || uint32(x) >= framebuffer.width || uint32(y) >= framebuffer.height {
			return nil
		}
		if !clip.contains(x, y) {
			return nil
		}
		address := framebuffer.pixels + uint32(y)*framebuffer.bpl + uint32(x)*2
		var data [2]byte
		if coverage != 0xff {
			if err := memory.Read(address, data[:]); err != nil {
				return err
			}
		}
		return runtime.blendPixel(op, address, blend565(binary.LittleEndian.Uint16(data[:]), uint16(pixel), coverage))
	}
	cursor := int32(registers[1])
	baseline := int32(registers[2])
	face := runtime.fontFace()
	for _, character := range text {
		bitmap := face.Render(character)
		if character != ' ' && character != '\t' {
			for row := range bitmap.Rows {
				for column := 0; column < bitmap.Width; column++ {
					if err := plot(cursor+int32(column), baseline-int32(bitmap.Ascent)+int32(row),
						bitmap.Coverage(row, column)); err != nil {
						return 0, err
					}
				}
			}
		}
		cursor += int32(bitmap.Advance)
	}
	return 0, nil
}

// wipicPutPixel implements MC_grpPutPixel.
func (runtime *initializationRuntime) wipicPutPixel(thread *armcore.Thread) (uint32, error) {
	registers := make([]uint32, 4)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	framebuffer, ok, err := runtime.readWIPICDrawSurface(registers[0], "put pixel")
	if err != nil || !ok {
		return 0, err
	}
	pixel, err := runtime.wipicReadContextForeground(registers[3])
	if err != nil {
		return 0, err
	}
	clip, err := runtime.wipicReadContextClip(registers[3])
	if err != nil {
		return 0, err
	}
	op, err := runtime.wipicReadContextPixelOp(registers[3])
	if err != nil {
		return 0, err
	}
	x, y := int32(registers[1]), int32(registers[2])
	if x < 0 || y < 0 || uint32(x) >= framebuffer.width || uint32(y) >= framebuffer.height {
		return 0, nil
	}
	if !clip.contains(x, y) {
		return 0, nil
	}
	address := framebuffer.pixels + uint32(y)*framebuffer.bpl + uint32(x)*2
	if err := runtime.blendPixel(op, address, uint16(pixel)); err != nil {
		return 0, fmt.Errorf("write KTF pixel at %d,%d: %w", x, y, err)
	}
	return 0, nil
}

// wipicDrawLine implements MC_grpDrawLine with Bresenham stepping.
func (runtime *initializationRuntime) wipicDrawLine(thread *armcore.Thread) (uint32, error) {
	registers := make([]uint32, 6)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	framebuffer, ok, err := runtime.readWIPICDrawSurface(registers[0], "draw line")
	if err != nil || !ok {
		return 0, err
	}
	pixel, err := runtime.wipicReadContextForeground(registers[5])
	if err != nil {
		return 0, err
	}
	clip, err := runtime.wipicReadContextClip(registers[5])
	if err != nil {
		return 0, err
	}
	op, err := runtime.wipicReadContextPixelOp(registers[5])
	if err != nil {
		return 0, err
	}
	plot := func(x, y int32) error {
		if x < 0 || y < 0 || uint32(x) >= framebuffer.width || uint32(y) >= framebuffer.height {
			return nil
		}
		if !clip.contains(x, y) {
			return nil
		}
		address := framebuffer.pixels + uint32(y)*framebuffer.bpl + uint32(x)*2
		return runtime.blendPixel(op, address, uint16(pixel))
	}
	x0, y0 := int32(registers[1]), int32(registers[2])
	x1, y1 := int32(registers[3]), int32(registers[4])
	deltaX := x1 - x0
	if deltaX < 0 {
		deltaX = -deltaX
	}
	deltaY := y1 - y0
	if deltaY < 0 {
		deltaY = -deltaY
	}
	stepX := int32(1)
	if x0 > x1 {
		stepX = -1
	}
	stepY := int32(1)
	if y0 > y1 {
		stepY = -1
	}
	difference := deltaX - deltaY
	for {
		if err := plot(x0, y0); err != nil {
			return 0, err
		}
		if x0 == x1 && y0 == y1 {
			return 0, nil
		}
		doubled := 2 * difference
		if doubled > -deltaY {
			difference -= deltaY
			x0 += stepX
		}
		if doubled < deltaX {
			difference += deltaX
			y0 += stepY
		}
	}
}

// wipicCopyFramebuffer implements MC_grpCopyFrameBuffer: copy a rectangle from
// a source framebuffer to a destination position.
func (runtime *initializationRuntime) wipicCopyFramebuffer(thread *armcore.Thread) (uint32, error) {
	registers := make([]uint32, 9)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	destination, ok, err := runtime.readWIPICDrawSurface(registers[0], "copy frame buffer destination")
	if err != nil || !ok {
		return 0, err
	}
	source, ok, err := runtime.readWIPICDrawSurface(registers[5], "copy frame buffer source")
	if err != nil || !ok {
		return 0, err
	}
	clip, err := runtime.wipicReadContextClip(registers[8])
	if err != nil {
		return 0, err
	}
	op, err := runtime.wipicReadContextPixelOp(registers[8])
	if err != nil {
		return 0, err
	}
	destinationX, destinationY := int32(registers[1]), int32(registers[2])
	width, height := int32(registers[3]), int32(registers[4])
	sourceX, sourceY := int32(registers[6]), int32(registers[7])
	memory := runtime.client.core.Memory()
	for line := int32(0); line < height; line++ {
		fromY := sourceY + line
		toY := destinationY + line
		if fromY < 0 || toY < 0 || uint32(fromY) >= source.height || uint32(toY) >= destination.height {
			continue
		}
		for column := int32(0); column < width; column++ {
			fromX := sourceX + column
			toX := destinationX + column
			if fromX < 0 || toX < 0 || uint32(fromX) >= source.width || uint32(toX) >= destination.width {
				continue
			}
			if !clip.contains(toX, toY) {
				continue
			}
			var data [2]byte
			if err := memory.Read(source.pixels+uint32(fromY)*source.bpl+uint32(fromX)*2, data[:]); err != nil {
				return 0, err
			}
			address := destination.pixels + uint32(toY)*destination.bpl + uint32(toX)*2
			if err := runtime.blendPixel(op, address, binary.LittleEndian.Uint16(data[:])); err != nil {
				return 0, err
			}
		}
	}
	return 0, nil
}

// decodeGuestImage decodes the encoded image formats KTF archives carry. BMP
// assets often hold nonzero reserved header fields, which defeats standard
// format sniffing, so the BM signature routes to the BMP decoder directly.
// The decoded image carries the transparency its encoding declared, because
// the 16-bit guest framebuffers it is drawn through cannot.
func decodeGuestImage(encoded []byte) (stdimage.Image, error) {
	if len(encoded) >= 2 && encoded[0] == 'B' && encoded[1] == 'M' {
		// BMP carries no alpha channel, so the transparency a title declared
		// in the header is applied first and then read back out the same way
		// as any other encoding's. See wipic.DecodeBitmap.
		decoded, err := wipic.DecodeBitmap(encoded)
		if err != nil {
			return nil, err
		}
		return withOpacity(decoded), nil
	}
	if wipic.IsLBMP(encoded) {
		// The handset's own bitmap: LCD pixels with a 24-byte header and no
		// encoding the standard library would recognise. See wipic.DecodeLBMP.
		decoded, err := wipic.DecodeLBMP(encoded)
		if err != nil {
			return nil, err
		}
		return withOpacity(decoded), nil
	}
	decoded, _, err := wipic.DecodeStandard(encoded)
	if err != nil {
		return nil, err
	}
	return withOpacity(decoded), nil
}

// encodeFramebufferRegion encodes one rectangle of a framebuffer as the BMP
// bytes MC_grpEncodeImage and Graphics.encodeImage both answer with. The
// region is clipped to the framebuffer, because a title that asks for the
// whole screen from a card that is smaller than it is asking for what it can
// see rather than for a failure.
func (runtime *initializationRuntime) encodeFramebufferRegion(framebuffer wipicFramebuffer, x, y, width, height int32) ([]byte, error) {
	if x < 0 {
		width, x = width+x, 0
	}
	if y < 0 {
		height, y = height+y, 0
	}
	if right := int64(x) + int64(width); right > int64(framebuffer.width) {
		width = int32(int64(framebuffer.width) - int64(x))
	}
	if bottom := int64(y) + int64(height); bottom > int64(framebuffer.height) {
		height = int32(int64(framebuffer.height) - int64(y))
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("KTF encode region %dx%d is empty", width, height)
	}
	picture := stdimage.NewRGBA(stdimage.Rect(0, 0, int(width), int(height)))
	memory := runtime.client.core.Memory()
	row := make([]byte, int(width)*2)
	for line := int32(0); line < height; line++ {
		if err := memory.Read(framebuffer.pixels+uint32(y+line)*framebuffer.bpl+uint32(x)*2, row); err != nil {
			return nil, fmt.Errorf("read KTF framebuffer row %d: %w", line, err)
		}
		for column := int32(0); column < width; column++ {
			rgb := rgbFromPixel565(binary.LittleEndian.Uint16(row[column*2:]))
			offset := picture.PixOffset(int(column), int(line))
			picture.Pix[offset+0] = byte(rgb >> 16)
			picture.Pix[offset+1] = byte(rgb >> 8)
			picture.Pix[offset+2] = byte(rgb)
			picture.Pix[offset+3] = 0xff
		}
	}
	return wipic.EncodeBitmap(picture)
}

// wipicEncodeImage implements MC_grpEncodeImage(src, x, y, w, h, *len). The
// result is a buffer the caller owns — the specification says it comes from
// MC_knlCalloc — so it is one of this platform's allocations and the caller
// frees it the same way.
func (runtime *initializationRuntime) wipicEncodeImage(thread *armcore.Thread) (uint32, error) {
	registers := make([]uint32, 6)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	framebuffer, err := runtime.readWIPICFramebuffer(registers[0])
	if err != nil {
		return 0, err
	}
	encoded, err := runtime.encodeFramebufferRegion(framebuffer,
		int32(registers[1]), int32(registers[2]), int32(registers[3]), int32(registers[4]))
	if err != nil {
		runtime.countDiagnostic("encode image refused")
		return 0, nil
	}
	handle, err := runtime.allocateWIPIC(uint32(len(encoded)))
	if err != nil {
		return 0, err
	}
	if err := runtime.client.core.Memory().Write(handle+wipicAllocationOverhead, encoded); err != nil {
		return 0, fmt.Errorf("write KTF encoded image: %w", err)
	}
	// The length is an out parameter, and a caller that passes null for it has
	// asked not to be told.
	if pointer := registers[5]; pointer != 0 {
		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], uint32(len(encoded)))
		if err := runtime.client.core.Memory().Write(pointer, size[:]); err != nil {
			return 0, fmt.Errorf("write KTF encoded image length: %w", err)
		}
	}
	return handle, nil
}

// newWIPICFramebufferRecord builds one MC_GrpFrameBuffer record plus its pixel
// allocation and returns the record handle.
func (runtime *initializationRuntime) newWIPICFramebufferRecord(width, height uint32) (uint32, error) {
	// **The bound that matters is the total, not the side.** A per-dimension
	// cap of 2048 refused a title's 2464x32 sprite strip — 154KB, and nothing
	// a handset would have minded — while allowing 2048x2048, which is 8MB.
	// The side bound that remains is only there to keep the multiplication
	// below obviously in range.
	if width == 0 || height == 0 || width > maxWIPICFramebufferSide || height > maxWIPICFramebufferSide {
		return 0, fmt.Errorf("KTF framebuffer size %dx%d is out of range", width, height)
	}
	if uint64(width)*2*uint64(height) > maxWIPICFramebufferBytes {
		return 0, fmt.Errorf("KTF framebuffer %dx%d exceeds size limit", width, height)
	}
	bytesPerLine := width * 2
	buffer, err := runtime.allocateWIPIC(bytesPerLine * height)
	if err != nil {
		return 0, fmt.Errorf("allocate KTF pixel buffer: %w", err)
	}
	record, err := runtime.allocateWIPIC(20)
	if err != nil {
		return 0, fmt.Errorf("allocate KTF framebuffer record: %w", err)
	}
	fields := []uint32{width, height, bytesPerLine, 16, buffer}
	data := make([]byte, len(fields)*4)
	for index, word := range fields {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	if err := runtime.client.core.Memory().Write(record+wipicAllocationOverhead, data); err != nil {
		return 0, fmt.Errorf("write KTF framebuffer record: %w", err)
	}
	return record, nil
}

// destroyWIPICFramebufferRecord gives one framebuffer record and the pixel
// allocation it names back to the arena. Both came from allocateWIPIC, so both
// go back through the same path MC_knlFree uses.
//
// The screen framebuffer is left alone: the specification says a destroy call
// handed the screen framebuffer does nothing, and this platform hands out one
// cached record for it, so freeing it would take the screen away from every
// later caller rather than from the one that asked.
//
// A handle whose record does not read back is counted and ignored, and a
// destroy of a block this platform never issued releases nothing — which is
// what makes a double destroy safe rather than a block handed to two owners,
// since the first one dropped the handle from the allocation table. That is the
// same judgement MC_knlFree makes: a handset would not end the program over a
// program's own error.
func (runtime *initializationRuntime) destroyWIPICFramebufferRecord(handle uint32) {
	if handle == 0 || handle == runtime.screenFramebuffer {
		return
	}
	// An image record names its framebuffer rather than inlining it, so the
	// record it names goes back with it. Without this every image a title
	// creates and destroys leaks its framebuffer record and, worse, the pixel
	// allocation that record names.
	if inner, isImage := runtime.wipicImageFramebuffer(handle); isImage {
		runtime.destroyWIPICFramebufferRecord(inner)
		runtime.releaseWIPICIfOwned(handle)
		return
	}
	framebuffer, err := runtime.readWIPICFramebuffer(handle)
	if err != nil {
		runtime.countDiagnostic("destroy of an unreadable framebuffer")
		return
	}
	runtime.releaseWIPICIfOwned(framebuffer.buffer)
	runtime.releaseWIPICIfOwned(handle)
}

// wipicDestroyOffScreenFrameBuffer implements
// MC_grpDestroyOffScreenFrameBuffer. The call returns nothing.
//
// It has to actually free. One local title draws each frame into an off-screen
// buffer it creates and destroys again — eight times a tick — and while the
// destroy was a no-op the arena grew by a quarter of a megabyte every second
// and the session died inside ten minutes of play at "platform initialization
// data space exhausted", with nothing on screen to say why.
func (runtime *initializationRuntime) wipicDestroyOffScreenFrameBuffer(thread *armcore.Thread) (uint32, error) {
	handle, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	runtime.destroyWIPICFramebufferRecord(handle)
	return 0, nil
}

// wipicDestroyImage implements MC_grpDestroyImage: the image record, the pixels
// it decoded into, and the encoded source buffer the guest handed to
// MC_grpCreateImage all go back.
//
// The source buffer is the platform's to free rather than the caller's. The
// specification says so of MC_grpCreateImage — the buffer passed in "is
// released inside the image function", which is why it has to be an
// MC_knlCalloc identifier — and a local title relies on it: it callocs a buffer
// per image and never frees one, so its allocation count tracked its image
// count exactly.
func (runtime *initializationRuntime) wipicDestroyImage(thread *armcore.Thread) (uint32, error) {
	handle, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	if handle == 0 {
		return 0, nil
	}
	runtime.releaseImageSourceBuffer(handle)
	runtime.destroyWIPICFramebufferRecord(handle)
	return 0, nil
}

// releaseImageSourceBuffer frees the encoded buffer an MC_GrpImage record names,
// before the record itself goes back and takes the word naming it with it.
//
// The word is only read when the block is a whole image record. A handle that
// is something smaller — an off-screen framebuffer handed to the wrong destroy
// is the case that happens — has no word thirteen of its own, and reading past
// the block would find whatever the next allocation holds. That is not merely
// nonsense: the neighbouring blocks here are framebuffer records, whose fields
// are themselves handles, so a stray word could name a live block and free
// something the game is still drawing with.
func (runtime *initializationRuntime) releaseImageSourceBuffer(handle uint32) {
	const imageRecordWords = 17
	if runtime.wipicAllocations[handle] < uint64(wipicAllocationOverhead)+imageRecordWords*4 {
		return
	}
	fields, err := runtime.readAOTWords(handle, 1, "image handle")
	if err != nil {
		return
	}
	record, err := runtime.readAOTWords(fields[0]+8, 14, "image record")
	if err != nil {
		return
	}
	runtime.releaseWIPICIfOwned(record[13])
}

// releaseWIPICIfOwned frees a block this platform handed out and ignores
// anything else. The silence is the point: a Java array reaches the image calls
// as a handle too, and counting every one of those as a stray free would bury
// the diagnostic that names a real one.
func (runtime *initializationRuntime) releaseWIPICIfOwned(handle uint32) {
	if handle == 0 {
		return
	}
	if _, ok := runtime.wipicAllocations[handle]; !ok {
		return
	}
	runtime.freeWIPIC(handle)
}

// fillWIPICFramebuffer writes one pixel value over a framebuffer's whole
// pixel allocation.
func (runtime *initializationRuntime) fillWIPICFramebuffer(handle uint32, pixel uint16) error {
	framebuffer, err := runtime.readWIPICFramebuffer(handle)
	if err != nil {
		return err
	}
	row := make([]byte, framebuffer.width*2)
	for column := uint32(0); column < framebuffer.width; column++ {
		binary.LittleEndian.PutUint16(row[column*2:], pixel)
	}
	memory := runtime.client.core.Memory()
	for line := uint32(0); line < framebuffer.height; line++ {
		if err := memory.Write(framebuffer.pixels+line*framebuffer.bpl, row); err != nil {
			return fmt.Errorf("fill KTF framebuffer row %d: %w", line, err)
		}
	}
	return nil
}

// wipicCreateImage implements MC_grpCreateImage: decode the encoded image
// bytes into a 16-bit framebuffer and publish the MC_GrpImage record.
func (runtime *initializationRuntime) wipicCreateImage(thread *armcore.Thread) (uint32, error) {
	registers := make([]uint32, 4)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	targetPointer, dataHandle, offset, length := registers[0], registers[1], registers[2], registers[3]
	if length == 0 || uint64(length) > maxPlatformAllocation {
		return wipicErrorInvalid, nil
	}
	dataBase, err := runtime.readAOTWords(dataHandle, 1, "image data handle")
	if err != nil {
		return 0, err
	}
	encoded := make([]byte, length)
	if err := runtime.client.core.Memory().Read(dataBase[0]+8+offset, encoded); err != nil {
		return 0, fmt.Errorf("read KTF image data: %w", err)
	}
	decoded, err := decodeGuestImage(encoded)
	if err != nil {
		prefix := encoded
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		runtime.countDiagnostic(fmt.Sprintf("image decode error %v len=%d prefix=%x", err, length, prefix))
		return wipicErrorInvalid, nil
	}
	bounds := decoded.Bounds()
	width, height := uint32(bounds.Dx()), uint32(bounds.Dy())
	framebuffer, err := runtime.newWIPICFramebufferRecord(width, height)
	if err != nil {
		return 0, err
	}
	target, err := runtime.readWIPICFramebuffer(framebuffer)
	if err != nil {
		return 0, err
	}
	// The record's own mask framebuffer stays empty, which is what a guest
	// reading MC_GrpImage expects of an image without one; the transparency
	// the encoding declared is kept beside the pixels instead.
	runtime.setFramebufferOpacity(framebuffer, imageOpacityOf(decoded))
	memory := runtime.client.core.Memory()
	row := make([]byte, int(width)*2)
	for y := uint32(0); y < height; y++ {
		for x := uint32(0); x < width; x++ {
			pixel := imagePixel565(decoded, bounds.Min.X+int(x), bounds.Min.Y+int(y))
			binary.LittleEndian.PutUint16(row[x*2:], pixel)
		}
		if err := memory.Write(target.pixels+y*target.bpl, row); err != nil {
			return 0, fmt.Errorf("write KTF decoded image row %d: %w", y, err)
		}
	}
	// MC_GrpImage: the image's framebuffer, its mask, animation state, and the
	// source buffer reference.
	record := make([]uint32, 17)
	// Word zero is the image's framebuffer and word one is its mask, both as
	// handles: see readWIPICFramebuffer for the title whose macros say so. The
	// mask stays absent, which is what a guest reading MC_GrpImage expects of
	// an image that has none.
	record[0] = framebuffer
	record[13] = dataHandle
	record[14] = offset
	record[16] = length
	image, err := runtime.allocateWIPIC(uint32(len(record)) * 4)
	if err != nil {
		return 0, err
	}
	data := make([]byte, len(record)*4)
	for index, word := range record {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	if err := memory.Write(image+wipicAllocationOverhead, data); err != nil {
		return 0, fmt.Errorf("write KTF image record: %w", err)
	}
	// A caller reads the image as a framebuffer through the image handle — and
	// MC_grpDrawImage is always given the image handle, never the inner one.
	// The transparency has to answer to both, or every C-side image blit runs
	// fully opaque: one title's map objects each came out inside a black
	// rectangle that way.
	runtime.setFramebufferOpacity(image, imageOpacityOf(decoded))
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], image)
	if err := memory.Write(targetPointer, word[:]); err != nil {
		return 0, fmt.Errorf("write KTF image result at %#x: %w", targetPointer, err)
	}
	return 1, nil
}

// wipicDrawImage implements MC_grpDrawImage as a bounded rectangle copy from
// the image's framebuffer.
func (runtime *initializationRuntime) wipicDrawImage(thread *armcore.Thread) (uint32, error) {
	registers := make([]uint32, 9)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	destination, ok, err := runtime.readWIPICDrawSurface(registers[0], "draw image destination")
	if err != nil {
		return 0, fmt.Errorf("%w%s", err, runtime.callerSite(thread))
	}
	if !ok {
		return 0, nil
	}
	source, ok, err := runtime.readWIPICDrawSurface(registers[5], "draw image source")
	if err != nil {
		return 0, fmt.Errorf("%w%s", err, runtime.callerSite(thread))
	}
	if !ok {
		return 0, nil
	}
	clip, err := runtime.wipicReadContextClip(registers[8])
	if err != nil {
		return 0, err
	}
	op, err := runtime.wipicReadContextPixelOp(registers[8])
	if err != nil {
		return 0, err
	}
	return 0, runtime.wipicBlitClipped(
		destination, clip, op, int32(registers[1]), int32(registers[2]),
		int32(registers[3]), int32(registers[4]),
		source, int32(registers[6]), int32(registers[7]),
		blitOpacity{
			source:      runtime.framebufferOpacityOf(registers[5]),
			destination: runtime.framebufferOpacityOf(registers[0]),
		},
	)
}

// wipicBlit copies a rectangle between guest framebuffers. Pixels the source's
// encoding left undrawn are skipped so the destination keeps what it held
// there, and the destination's own mask records the pixels that were drawn.
func (runtime *initializationRuntime) wipicBlit(destination wipicFramebuffer, destinationX, destinationY, width, height int32, source wipicFramebuffer, sourceX, sourceY int32, opacity blitOpacity) error {
	return runtime.wipicBlitClipped(destination, wipicClip{}, wipicPixelOp{}, destinationX, destinationY, width, height, source, sourceX, sourceY, opacity)
}

// wipicBlitClipped is the same copy bounded by a graphics context's clip. The
// Java side has a clip of its own and comes through wipicBlit, which passes an
// empty one.
func (runtime *initializationRuntime) wipicBlitClipped(destination wipicFramebuffer, clip wipicClip, op wipicPixelOp, destinationX, destinationY, width, height int32, source wipicFramebuffer, sourceX, sourceY int32, opacity blitOpacity) error {
	memory := runtime.client.core.Memory()
	// A copy inside one surface reads pixels the same copy is about to write.
	// Reading them straight from memory smears the leading edge across the
	// rectangle, so an overlapping self-copy reads from a snapshot taken
	// before the first write — which is the "as if through a temporary
	// buffer" a copy within one frame buffer is specified to be.
	snapshot, err := runtime.blitSourceSnapshot(destination, destinationX, destinationY, width, height, source, sourceX, sourceY)
	if err != nil {
		return err
	}
	for line := int32(0); line < height; line++ {
		fromY := sourceY + line
		toY := destinationY + line
		if fromY < 0 || toY < 0 || uint32(fromY) >= source.height || uint32(toY) >= destination.height {
			continue
		}
		for column := int32(0); column < width; column++ {
			fromX := sourceX + column
			toX := destinationX + column
			if fromX < 0 || toX < 0 || uint32(fromX) >= source.width || uint32(toX) >= destination.width {
				continue
			}
			if !clip.contains(toX, toY) {
				continue
			}
			if !opacity.source.opaqueAt(int(fromX), int(fromY)) {
				continue
			}
			var data [2]byte
			if snapshot != nil {
				copy(data[:], snapshot[(int(line)*int(width)+int(column))*2:])
			} else if err := memory.Read(source.pixels+uint32(fromY)*source.bpl+uint32(fromX)*2, data[:]); err != nil {
				return err
			}
			address := destination.pixels + uint32(toY)*destination.bpl + uint32(toX)*2
			if err := runtime.blendPixel(op, address, binary.LittleEndian.Uint16(data[:])); err != nil {
				return err
			}
			opacity.destination.markOpaque(int(toX), int(toY))
		}
	}
	return nil
}

// blitSourceSnapshot reads the source rectangle when a copy overlaps itself
// inside one surface, and answers nil when it does not — a copy between two
// surfaces, or one whose rectangles do not meet, reads memory directly and
// pays nothing. The snapshot is laid out as the copy's own rows, so a pixel at
// (column, line) sits at (line*width+column)*2 whether or not it was inside
// the surface; pixels outside are never read from it.
func (runtime *initializationRuntime) blitSourceSnapshot(destination wipicFramebuffer, destinationX, destinationY, width, height int32, source wipicFramebuffer, sourceX, sourceY int32) ([]byte, error) {
	if source.pixels != destination.pixels || width <= 0 || height <= 0 {
		return nil, nil
	}
	if sourceX == destinationX && sourceY == destinationY {
		return nil, nil // a copy onto itself changes nothing to read
	}
	if sourceX+width <= destinationX || destinationX+width <= sourceX ||
		sourceY+height <= destinationY || destinationY+height <= sourceY {
		return nil, nil
	}
	memory := runtime.client.core.Memory()
	snapshot := make([]byte, int(width)*int(height)*2)
	for line := int32(0); line < height; line++ {
		fromY := sourceY + line
		if fromY < 0 || uint32(fromY) >= source.height {
			continue
		}
		left, right := sourceX, sourceX+width
		if left < 0 {
			left = 0
		}
		if right > int32(source.width) {
			right = int32(source.width)
		}
		if left >= right {
			continue
		}
		offset := (int(line)*int(width) + int(left-sourceX)) * 2
		row := snapshot[offset : offset+int(right-left)*2]
		if err := memory.Read(source.pixels+uint32(fromY)*source.bpl+uint32(left)*2, row); err != nil {
			return nil, err
		}
	}
	return snapshot, nil
}

// wipicCopyArea implements MC_grpCopyArea, a rectangle copy within one
// framebuffer.
func (runtime *initializationRuntime) wipicCopyArea(thread *armcore.Thread) (uint32, error) {
	registers := make([]uint32, 8)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	framebuffer, ok, err := runtime.readWIPICDrawSurface(registers[0], "copy area")
	if err != nil || !ok {
		return 0, err
	}
	opacity := runtime.framebufferOpacityOf(registers[0])
	clip, err := runtime.wipicReadContextClip(registers[7])
	if err != nil {
		return 0, err
	}
	op, err := runtime.wipicReadContextPixelOp(registers[7])
	if err != nil {
		return 0, err
	}
	return 0, runtime.wipicBlitClipped(
		framebuffer, clip, op, int32(registers[1]), int32(registers[2]),
		int32(registers[3]), int32(registers[4]),
		framebuffer, int32(registers[5]), int32(registers[6]),
		blitOpacity{source: opacity, destination: opacity},
	)
}

// wipicTransferRGBPixels implements MC_grpGetRGBPixels and MC_grpSetRGBPixels:
// rows of 0x00RRGGBB words spaced a caller-chosen byte pitch apart.
func (runtime *initializationRuntime) wipicTransferRGBPixels(thread *armcore.Thread, write bool) (uint32, error) {
	registers := make([]uint32, 7)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	framebuffer, ok, err := runtime.readWIPICDrawSurface(registers[0], "transfer RGB pixels")
	if err != nil || !ok {
		return 0, err
	}
	x, y := int32(registers[1]), int32(registers[2])
	width, height := int32(registers[3]), int32(registers[4])
	buffer := registers[5]
	pitch := int32(registers[6])
	if width <= 0 || height <= 0 {
		return 0, nil
	}
	if pitch < width*4 || width > 4096 || height > 4096 {
		return 0, nil
	}
	memory := runtime.client.core.Memory()
	row := make([]byte, int(width)*4)
	for line := int32(0); line < height; line++ {
		rowAddress := buffer + uint32(line)*uint32(pitch)
		if write {
			if err := memory.Read(rowAddress, row); err != nil {
				return 0, fmt.Errorf("read KTF RGB row %d: %w", line, err)
			}
		}
		for column := int32(0); column < width; column++ {
			sourceX, sourceY := x+column, y+line
			inside := sourceX >= 0 && sourceY >= 0 && uint32(sourceX) < framebuffer.width && uint32(sourceY) < framebuffer.height
			pixelAddress := framebuffer.pixels + uint32(sourceY)*framebuffer.bpl + uint32(sourceX)*2
			if write {
				if !inside {
					continue
				}
				rgb := binary.LittleEndian.Uint32(row[column*4:])
				pixel := uint16(rgb>>16&0xff)>>3<<11 | uint16(rgb>>8&0xff)>>2<<5 | uint16(rgb&0xff)>>3
				var data [2]byte
				binary.LittleEndian.PutUint16(data[:], pixel)
				if err := memory.Write(pixelAddress, data[:]); err != nil {
					return 0, err
				}
				continue
			}
			var rgb uint32
			if inside {
				var data [2]byte
				if err := memory.Read(pixelAddress, data[:]); err != nil {
					return 0, err
				}
				pixel := binary.LittleEndian.Uint16(data[:])
				rgb = uint32(pixel>>11&0x1f)<<3<<16 | uint32(pixel>>5&0x3f)<<2<<8 | uint32(pixel&0x1f)<<3
			}
			binary.LittleEndian.PutUint32(row[column*4:], rgb)
		}
		if !write {
			if err := memory.Write(rowAddress, row); err != nil {
				return 0, fmt.Errorf("write KTF RGB row %d: %w", line, err)
			}
		}
	}
	return 0, nil
}

// presentScreen records that the guest flushed the screen. The conversion the
// Host needs is not done here.
//
// These games flush far faster than any display refreshes — a menu can manage
// a few hundred a second — and converting the RGB565 screen to RGBA is a read
// and a rewrite of every pixel on it. Doing that per flush spent most of it on
// frames no Host would ever ask for, so the work moved to where the asking
// happens: Client.Frame converts, once, for the Host that actually takes a
// frame. A run of flushes with no Host collection between them now costs one
// conversion instead of one each.
func (runtime *initializationRuntime) presentScreen() error {
	if runtime.screenFramebuffer == 0 {
		// Nothing was drawn through the screen framebuffer yet.
		return nil
	}
	runtime.client.framePending = true
	runtime.client.flushCount++
	// A flush from outside the card paint is the title publishing a frame of
	// its own. Which of the two paths owns the screen is decided in
	// paintTopCard, and this is the half of the evidence it cannot see.
	if !runtime.repaintServicing {
		runtime.guestFlushedOwnFrame = true
	}
	runtime.countDiagnostic("flush lcd")
	return nil
}

// convertScreen turns the guest's RGB565 screen buffer into the Host-owned
// RGBA frame. The caller holds the run lock.
func (runtime *initializationRuntime) convertScreen() error {
	if runtime.screenFramebuffer == 0 {
		return nil
	}
	framebuffer, err := runtime.readWIPICFramebuffer(runtime.screenFramebuffer)
	if err != nil {
		return err
	}
	width, height := int(framebuffer.width), int(framebuffer.height)
	row := make([]byte, width*2)
	frame := runtime.client.frame
	if len(frame) != width*height*4 {
		frame = make([]byte, width*height*4)
	}
	memory := runtime.client.core.Memory()
	for y := 0; y < height; y++ {
		if err := memory.Read(framebuffer.pixels+uint32(y)*framebuffer.bpl, row); err != nil {
			return fmt.Errorf("read KTF screen row %d: %w", y, err)
		}
		for x := 0; x < width; x++ {
			pixel := binary.LittleEndian.Uint16(row[x*2:])
			offset := (y*width + x) * 4
			frame[offset] = byte((pixel >> 11 & 0x1f) << 3)
			frame[offset+1] = byte((pixel >> 5 & 0x3f) << 2)
			frame[offset+2] = byte((pixel & 0x1f) << 3)
			frame[offset+3] = 0xff
		}
	}
	runtime.client.frame = frame
	runtime.client.frameWidth = width
	runtime.client.frameHeight = height
	runtime.client.framePending = false
	return nil
}

// wipicArgument reads the ARM calling convention: the first four words arrive
// in registers and the rest on the stack.
func (runtime *initializationRuntime) wipicArgument(thread *armcore.Thread, index int) (uint32, error) {
	if index < 4 {
		return thread.Register(index)
	}
	stack, err := thread.Register(armcore.RegisterSP)
	if err != nil {
		return 0, err
	}
	words, err := runtime.readAOTWords(stack+uint32(index-4)*4, 1, "WIPI C stack argument")
	if err != nil {
		return 0, err
	}
	return words[0], nil
}

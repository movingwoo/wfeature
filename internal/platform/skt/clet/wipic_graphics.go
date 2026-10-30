package clet

import (
	"encoding/binary"
	"fmt"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math/bits"

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
	fields, err := runtime.readAOTWords(runtime.wipicHandleData(recordBase[0]), 5, "framebuffer record")
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
	// The record is guest memory, so the line check is made wide enough that
	// a width of 2^31 or more cannot wrap width*2 under a tiny bpl.
	if framebuffer.width == 0 || framebuffer.height == 0 || uint64(framebuffer.bpl) < uint64(framebuffer.width)*2 {
		return wipicFramebuffer{}, fmt.Errorf("KTF framebuffer %dx%d bpl %d is invalid (%s)", framebuffer.width, framebuffer.height, framebuffer.bpl, runtime.describeWIPICSurfaceWords(handle, recordBase[0], fields))
	}
	if uint64(framebuffer.bpl)*uint64(framebuffer.height) > maxWIPICFramebufferBytes {
		return wipicFramebuffer{}, fmt.Errorf("KTF framebuffer %dx%d exceeds size limit", framebuffer.width, framebuffer.height)
	}
	bufferBase, err := runtime.readAOTWords(framebuffer.buffer, 1, "framebuffer pixel handle")
	if err != nil {
		return wipicFramebuffer{}, err
	}
	framebuffer.pixels = runtime.wipicHandleData(bufferBase[0])
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
	fields, err := runtime.readAOTWords(runtime.wipicHandleData(base[0]), 1, "image record")
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
// The leading context word distinguishes an explicitly empty clip from a
// disabled clip. Nonempty bounds also remain usable in directly written
// contexts whose caller does not populate that word.
type wipicClip struct {
	left, top, right, bottom int32
	enabled                  bool
}

func (clip wipicClip) empty() bool {
	return clip.right <= clip.left || clip.bottom <= clip.top
}

func (clip wipicClip) active() bool { return clip.enabled || !clip.empty() }

// contains reports whether one pixel survives the clip.
func (clip wipicClip) contains(x, y int32) bool {
	if !clip.active() {
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
	if clip.active() {
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
	words, err := runtime.readAOTWords(contextAddress, 3, "graphics context clip")
	if err != nil {
		return wipicClip{}, err
	}
	return wipicClip{
		enabled: words[0] != 0,
		left:    int32(int16(words[1])),
		top:     int32(int16(words[1] >> 16)),
		right:   int32(int16(words[2])),
		bottom:  int32(int16(words[2] >> 16)),
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
	return 0, curve.DrawArcWithin(framebuffer.curveBounds(clip), bounds[0], bounds[1], bounds[2], bounds[3], angles[0], angles[1],
		runtime.framebufferCurveEmit(framebuffer, clip, op, pixel))
}

// wipicFillArc implements MC_grpFillArc: the pie slice the arc cuts out of its
// bounding ellipse.
func (runtime *initializationRuntime) wipicFillArc(thread *armcore.Thread) (uint32, error) {
	framebuffer, pixel, clip, op, bounds, angles, ok, err := runtime.wipicArcCall(thread, "fill arc")
	if err != nil || !ok {
		return 0, err
	}
	return 0, curve.FillArcWithin(framebuffer.curveBounds(clip), bounds[0], bounds[1], bounds[2], bounds[3], angles[0], angles[1],
		runtime.framebufferCurveEmit(framebuffer, clip, op, pixel))
}

// curveBounds is the part of the plane fillFramebufferRect can reach: the
// framebuffer met with the context's clip, as clipRect computes it. The arc's
// box comes from the guest and can be far larger than any surface, so the
// curve walks only this part of it.
func (framebuffer wipicFramebuffer) curveBounds(clip wipicClip) curve.Bounds {
	bounds := curve.Bounds{Right: int64(framebuffer.width), Bottom: int64(framebuffer.height)}
	if clip.active() {
		bounds.Left, bounds.Top = max(bounds.Left, int64(clip.left)), max(bounds.Top, int64(clip.top))
		bounds.Right, bounds.Bottom = min(bounds.Right, int64(clip.right)), min(bounds.Bottom, int64(clip.bottom))
	}
	return bounds
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
		// The length is the guest's, so the range has to exist before a host
		// buffer that size does; a garbage length otherwise asks the host for
		// up to 4 GiB just to be refused by the read.
		size := uint64(length) * uint64(unit)
		if err := memory.ValidateRange(pointer, size, armcore.PermissionRead); err != nil {
			return nil, fmt.Errorf("read KTF draw string: %w", err)
		}
		data = make([]byte, size)
		if err := memory.Read(pointer, data); err != nil {
			return nil, fmt.Errorf("read KTF draw string: %w", err)
		}
	} else {
		// Up to 1024 units: a longer run draws its first 1024 rather than
		// failing.
		text, _, err := readGuestString(memory, pointer, 1024*unit, unit)
		if err != nil {
			return nil, fmt.Errorf("read KTF draw string: %w", err)
		}
		data = text
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

// wipicDrawLine implements MC_grpDrawLine with Bresenham stepping, walked only
// across the surface; see clippedLine.
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
	return 0, clippedLine(int32(registers[1]), int32(registers[2]), int32(registers[3]), int32(registers[4]),
		0, 0, int64(framebuffer.width), int64(framebuffer.height), runtime.framebufferPlot(framebuffer, clip, op, pixel))
}

// framebufferPlot answers the one-pixel draw a line walks with: inside the
// surface and the context's clip, through the context's pixel operation.
func (runtime *initializationRuntime) framebufferPlot(framebuffer wipicFramebuffer, clip wipicClip, op wipicPixelOp, pixel uint32) func(x, y int32) error {
	return func(x, y int32) error {
		if x < 0 || y < 0 || uint32(x) >= framebuffer.width || uint32(y) >= framebuffer.height {
			return nil
		}
		if !clip.contains(x, y) {
			return nil
		}
		address := framebuffer.pixels + uint32(y)*framebuffer.bpl + uint32(x)*2
		return runtime.blendPixel(op, address, uint16(pixel))
	}
}

// maxWIPICPolygonPoints bounds the vertices a polygon call reads. The arrays
// and the count are the guest's; the LGT runtime reads to the same bound.
const maxWIPICPolygonPoints = 1024

// wipicPolygon implements MC_grpDrawPolygon and MC_grpFillPolygon, graphics
// entries 37 and 38, after postEvent in the specification's order:
//
//	void MC_grpDrawPolygon(MC_GrpFrameBuffer dst, M_Int32 *xPoints,
//	                       M_Int32 *yPoints, M_Int32 nPoints, MC_GrpContext *pgc)
//
// The outline is each edge, the last back to the first, drawn as drawLine
// draws one; the fill is the scanline the LGT runtime and this runtime's Java
// Graphics fill with (curve.FillPolygonWithin). Fewer than two vertices draw
// nothing. No local title calls either.
func (runtime *initializationRuntime) wipicPolygon(thread *armcore.Thread, fill bool) (uint32, error) {
	call := "draw polygon"
	if fill {
		call = "fill polygon"
	}
	registers := make([]uint32, 5)
	for index := range registers {
		value, err := runtime.wipicArgument(thread, index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	framebuffer, ok, err := runtime.readWIPICDrawSurface(registers[0], call)
	if err != nil || !ok {
		return 0, err
	}
	count := int32(registers[3])
	if count < 2 {
		return 0, nil
	}
	if count > maxWIPICPolygonPoints {
		return 0, fmt.Errorf("KTF %s of %d points: %d is the most read", call, count, maxWIPICPolygonPoints)
	}
	xs, err := runtime.wipicReadInt32s(registers[1], count, call)
	if err != nil {
		return 0, err
	}
	ys, err := runtime.wipicReadInt32s(registers[2], count, call)
	if err != nil {
		return 0, err
	}
	pixel, err := runtime.wipicReadContextForeground(registers[4])
	if err != nil {
		return 0, err
	}
	clip, err := runtime.wipicReadContextClip(registers[4])
	if err != nil {
		return 0, err
	}
	op, err := runtime.wipicReadContextPixelOp(registers[4])
	if err != nil {
		return 0, err
	}
	if fill {
		return 0, curve.FillPolygonWithin(framebuffer.curveBounds(clip), xs, ys,
			runtime.framebufferCurveEmit(framebuffer, clip, op, pixel))
	}
	plot := runtime.framebufferPlot(framebuffer, clip, op, pixel)
	for index := range xs {
		next := (index + 1) % len(xs)
		if err := clippedLine(xs[index], ys[index], xs[next], ys[next],
			0, 0, int64(framebuffer.width), int64(framebuffer.height), plot); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

// wipicReadInt32s reads count M_Int32 values from guest memory.
func (runtime *initializationRuntime) wipicReadInt32s(pointer uint32, count int32, call string) ([]int32, error) {
	data := make([]byte, int(count)*4)
	if err := runtime.client.core.Memory().Read(pointer, data); err != nil {
		return nil, fmt.Errorf("read KTF %s points: %w", call, err)
	}
	values := make([]int32, count)
	for index := range values {
		values[index] = int32(binary.LittleEndian.Uint32(data[index*4:]))
	}
	return values, nil
}

// clippedLine walks the Bresenham line MC_grpDrawLine and Graphics.drawLine
// have always drawn, but only across the stretch of it whose major-axis
// coordinate lies inside [left, right) by [top, bottom). plot still decides
// pixel by pixel; the window only keeps the walk from counting through a span
// the guest passed that reaches thousands of millions of pixels off the
// surface, and the 64-bit arithmetic keeps the endpoints at Integer.MIN_VALUE
// and MAX_VALUE from wrapping the loop into one that never ends.
//
// The major axis advances on every step of this loop and the minor one has
// advanced floor((2*steps*minor + major - 1) / (2*major)) after a given number
// of them, so the walk can start where the window does with the error term the
// loop would have carried there.
func clippedLine(x0, y0, x1, y1 int32, left, top, right, bottom int64, plot func(x, y int32) error) error {
	deltaX := int64(x1) - int64(x0)
	if deltaX < 0 {
		deltaX = -deltaX
	}
	deltaY := int64(y1) - int64(y0)
	if deltaY < 0 {
		deltaY = -deltaY
	}
	stepX := int64(1)
	if x0 > x1 {
		stepX = -1
	}
	stepY := int64(1)
	if y0 > y1 {
		stepY = -1
	}
	yMajor := deltaY > deltaX
	origin, low, high, direction := int64(x0), left, right, stepX
	if yMajor {
		origin, low, high, direction = int64(y0), top, bottom, stepY
	}
	first, last := int64(0), max(deltaX, deltaY)
	if direction > 0 {
		first, last = max(first, low-origin), min(last, high-1-origin)
	} else {
		first, last = max(first, origin-(high-1)), min(last, origin-low)
	}
	if first > last {
		return nil
	}
	var stepsX, stepsY int64
	if yMajor {
		stepsX, stepsY = lineMinorSteps(first, deltaY, deltaX), first
	} else {
		stepsX, stepsY = first, lineMinorSteps(first, deltaX, deltaY)
	}
	x, y := int64(x0)+stepX*stepsX, int64(y0)+stepY*stepsY
	// The products can pass 2^63 for the widest lines; the difference they
	// make is small, so the wrapped arithmetic still lands on it exactly.
	difference := (stepsY+1)*deltaX - (stepsX+1)*deltaY
	for step := first; ; step++ {
		if err := plot(int32(x), int32(y)); err != nil {
			return err
		}
		if step == last {
			return nil
		}
		doubled := 2 * difference
		if doubled > -deltaY {
			difference -= deltaY
			x += stepX
		}
		if doubled < deltaX {
			difference += deltaX
			y += stepY
		}
	}
}

// lineMinorSteps answers how far the minor axis of a line has moved after
// steps steps along its major axis. Both spans can reach 2^32-1, so the
// product is taken at 128 bits; the quotient is at most minor.
func lineMinorSteps(steps, major, minor int64) int64 {
	if major == 0 {
		return 0
	}
	high, low := bits.Mul64(uint64(2*steps), uint64(minor))
	low, carry := bits.Add64(low, uint64(major-1), 0)
	quotient, _ := bits.Div64(high+carry, low, uint64(2*major))
	return int64(quotient)
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
	destinationX, destinationY, width, height, sourceX, sourceY, ok := clipCopy(destination, source,
		int32(registers[1]), int32(registers[2]), int32(registers[3]), int32(registers[4]), int32(registers[6]), int32(registers[7]))
	if !ok {
		return 0, nil
	}
	// This call has never taken the snapshot wipicBlitClipped does: a copy
	// inside one surface reads each source pixel as the copy reaches it, so
	// an overlapping one carries what it has already drawn along with it.
	return 0, runtime.drawFramebufferCopy(&framebufferCopy{
		destination: destination, source: source, clip: clip, op: op,
		destinationX: destinationX, destinationY: destinationY, sourceX: sourceX, sourceY: sourceY,
		width: width, height: height,
	})
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

// wipicDecodeNextImage implements MC_grpDecodeNextImage(dst), graphics entry
// 34. Every image here is decoded whole when it is created, so a surface has
// no next frame: MC_GRP_IMAGE_DONE, as the LGT runtime answers it. A handle
// that is no surface is M_E_BADFORMAT. No local title calls it.
func (runtime *initializationRuntime) wipicDecodeNextImage(thread *armcore.Thread) (uint32, error) {
	handle, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	if handle == 0 {
		return wipicErrorBadFormat, nil
	}
	if _, err := runtime.readWIPICFramebuffer(handle); err != nil {
		return wipicErrorBadFormat, nil
	}
	return wipicImageDone, nil
}

// releaseImageSourceBuffer releases only the source allocation the image still
// owns. The guest can free it earlier and its address can then name another
// live allocation; rereading word thirteen would free that unrelated object.
// Ownership is also absent for a framebuffer passed to the image destructor.
func (runtime *initializationRuntime) releaseImageSourceBuffer(handle uint32) {
	source := runtime.imageSourceBuffers[handle]
	delete(runtime.imageSourceBuffers, handle)
	runtime.releaseWIPICIfOwned(source)
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
	if err := runtime.client.core.Memory().Read(runtime.wipicHandleData(dataBase[0])+offset, encoded); err != nil {
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
	if _, owned := runtime.wipicAllocations[dataHandle]; owned {
		if runtime.imageSourceBuffers == nil {
			runtime.imageSourceBuffers = make(map[uint32]uint32)
		}
		runtime.imageSourceBuffers[image] = dataHandle
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
	destinationX, destinationY, width, height, sourceX, sourceY, ok := clipCopy(destination, source, destinationX, destinationY, width, height, sourceX, sourceY)
	if !ok {
		return nil
	}
	// A copy inside one surface reads pixels the same copy is about to write.
	// Reading them straight from memory smears the leading edge across the
	// rectangle, so an overlapping self-copy reads from a snapshot taken
	// before the first write — which is the "as if through a temporary
	// buffer" a copy within one frame buffer is specified to be.
	snapshot, err := runtime.blitSourceSnapshot(destination, destinationX, destinationY, width, height, source, sourceX, sourceY)
	if err != nil {
		return err
	}
	return runtime.drawFramebufferCopy(&framebufferCopy{
		destination: destination, source: source, clip: clip, op: op, opacity: opacity, snapshot: snapshot,
		destinationX: destinationX, destinationY: destinationY, sourceX: sourceX, sourceY: sourceY,
		width: width, height: height,
	})
}

// framebufferCopy is one rectangle copy clipCopy has narrowed, so every row
// and column of it lands inside both surfaces. snapshot is the source
// rectangle read before the copy began, laid out as blitSourceSnapshot
// describes, or nil when the copy reads its source from memory as it goes.
type framebufferCopy struct {
	destination, source        wipicFramebuffer
	clip                       wipicClip
	op                         wipicPixelOp
	opacity                    blitOpacity
	snapshot                   []byte
	destinationX, destinationY int32
	sourceX, sourceY           int32
	width, height              int32
}

// copyRowSpan is how many pixels of a row are read and written at once. It
// bounds the scratch a row needs, so the scratch stays on the stack whatever
// width a surface record claims, and a 240-pixel screen row is one span.
const copyRowSpan = 256

// copyRowScratch is the host copy of one span: its source pixels, the
// destination pixels an operation combines them with, and which of them the
// source's transparency lets through.
type copyRowScratch struct {
	source      [copyRowSpan * 2]byte
	destination [copyRowSpan * 2]byte
	drawn       [copyRowSpan]bool
}

// drawFramebufferCopy draws a narrowed copy a span of a row at a time.
//
// **What it draws, the order it asks a guest operation in, and every byte and
// transparency mark it leaves behind are those of the pixel-at-a-time walk it
// replaced**, which is still here as copyPixels. A span is read whole,
// combined on the host and written back as one store per run of drawn pixels
// wherever that cannot answer differently from the walk, and walked wherever
// it could; copyRow says which is which.
//
// The walk crossed into guest memory two or three times a pixel, and each
// Memory.Read or Memory.Write takes the memory lock and validates its range
// however few bytes it moves, so that fixed cost was the copy. On an M1,
// BenchmarkWIPICBlitRows put a 32x32 sprite at 33µs walked and 1.3µs by rows,
// the same sprite with transparent corners at 30µs and 2.7µs, through a guest
// operation whose answers are cached at 53µs and 10µs, and a 240x320 screen
// at 2.5ms and 19µs.
func (runtime *initializationRuntime) drawFramebufferCopy(blit *framebufferCopy) error {
	if !blit.rowsAddressable() {
		for line := int32(0); line < blit.height; line++ {
			if err := runtime.copyPixels(blit, line, 0, blit.width); err != nil {
				return err
			}
		}
		return nil
	}
	var scratch copyRowScratch
	for line := int32(0); line < blit.height; line++ {
		first, end, ok := blit.clippedColumns(line)
		if !ok {
			continue
		}
		for start := first; start < end; start += copyRowSpan {
			stop := min(start+copyRowSpan, end)
			drawn, err := runtime.copyRow(blit, line, start, stop, &scratch)
			if err != nil {
				return err
			}
			if start+drawn < stop {
				if err := runtime.copyPixels(blit, line, start+drawn, stop); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// rowsAddressable reports whether every row of the copy is a plain run of
// bytes in both surfaces. clipCopy keeps every pixel inside both, so the only
// copy that fails this is one whose record places its pixels so near the top
// of the address space that the walk's 32-bit address sums wrap past it — a
// record no allocation of this platform's makes — and that copy is walked a
// pixel at a time, exactly as it always was.
func (blit *framebufferCopy) rowsAddressable() bool {
	if blit.width <= 0 || blit.height <= 0 ||
		blit.destinationX < 0 || blit.destinationY < 0 || blit.sourceX < 0 || blit.sourceY < 0 ||
		int64(blit.destinationX)+int64(blit.width) > int64(blit.destination.width) ||
		int64(blit.destinationY)+int64(blit.height) > int64(blit.destination.height) ||
		int64(blit.sourceX)+int64(blit.width) > int64(blit.source.width) ||
		int64(blit.sourceY)+int64(blit.height) > int64(blit.source.height) {
		return false
	}
	if framebufferRectangleEnd(blit.destination, blit.destinationX, blit.destinationY, blit.width, blit.height) > 1<<32 {
		return false
	}
	return blit.snapshot != nil ||
		framebufferRectangleEnd(blit.source, blit.sourceX, blit.sourceY, blit.width, blit.height) <= 1<<32
}

// framebufferRectangleEnd is one past the last byte a rectangle of a surface
// reaches, as a 64-bit sum.
func framebufferRectangleEnd(framebuffer wipicFramebuffer, x, y, width, height int32) uint64 {
	return uint64(framebuffer.pixels) + uint64(y+height-1)*uint64(framebuffer.bpl) + uint64(x+width)*2
}

// clippedColumns answers the columns of one row of the copy that survive the
// clip, as [first, end) in the copy's own columns, and false when none do. The
// walk tests every pixel against the clip, and what a rectangle keeps of a row
// is one stretch of it, so the pixels outside cost nothing here.
func (blit *framebufferCopy) clippedColumns(line int32) (int32, int32, bool) {
	first, end := int64(0), int64(blit.width)
	if blit.clip.active() {
		toY := blit.destinationY + line
		if toY < blit.clip.top || toY >= blit.clip.bottom {
			return 0, 0, false
		}
		first = max(first, int64(blit.clip.left)-int64(blit.destinationX))
		end = min(end, int64(blit.clip.right)-int64(blit.destinationX))
	}
	if first >= end {
		return 0, 0, false
	}
	return int32(first), int32(end), true
}

// copyRow draws columns [first, end) of one row, all of them inside the clip,
// from one read of the source span — and, when an operation combines pixels,
// one of the destination span — and one store per run of drawn pixels. It
// answers how many leading columns it drew, and the caller walks the rest.
//
// It draws none, having changed nothing, when reading the span whole could
// answer differently from the walk:
//
//   - The source comes from memory and its span starts before the
//     destination's and reaches into it. The walk reads each source pixel
//     after writing the pixels to its left, so it reads what this row has
//     just drawn; MC_grpCopyFrameBuffer inside one surface does exactly that.
//     A source span that starts at or after the destination's is never
//     reached by the writes to a pixel's left, and a disjoint one by none.
//   - The source's transparency mask is the destination's own, the row reads
//     and marks the same line, and the copy moves right by less than the
//     span. The walk marks each pixel it draws before testing the next, so a
//     pixel to the right can be let through by a mark this row made.
//   - Either span does not read back. The walk reads only the pixels it
//     draws, and meets a fault, if there is one, at the pixel that has it.
//
// **A guest operation is only ever run by the walk.** A span is drawn here up
// to the first pair the operation has not answered yet — no guest code runs
// before that, so nothing can move under the span — and the walk takes over at
// that pair, asking the guest with the same arguments in the same order the
// walk always did.
func (runtime *initializationRuntime) copyRow(blit *framebufferCopy, line, first, end int32, scratch *copyRowScratch) (int32, error) {
	count := int(end - first)
	fromX, fromY := blit.sourceX+first, blit.sourceY+line
	toX, toY := blit.destinationX+first, blit.destinationY+line
	sourceAddress := blit.source.pixels + uint32(fromY)*blit.source.bpl + uint32(fromX)*2
	destinationAddress := blit.destination.pixels + uint32(toY)*blit.destination.bpl + uint32(toX)*2
	if blit.snapshot == nil && sourceAddress < destinationAddress &&
		uint64(sourceAddress)+uint64(count)*2 > uint64(destinationAddress) {
		return 0, nil
	}
	if mask := blit.opacity.source; mask != nil && mask == blit.opacity.destination && fromY == toY {
		if shift := int64(blit.destinationX) - int64(blit.sourceX); shift > 0 && shift < int64(count) {
			return 0, nil
		}
	}
	memory := runtime.client.core.Memory()
	var source []byte
	if blit.snapshot != nil {
		offset := (int(line)*int(blit.width) + int(first)) * 2
		source = blit.snapshot[offset : offset+count*2]
	} else {
		source = scratch.source[:count*2]
		if memory.Read(sourceAddress, source) != nil {
			return 0, nil
		}
	}
	// Which pixels the source's transparency lets through; nil when it has
	// none, and every pixel is drawn.
	var drawn []bool
	if mask := blit.opacity.source; mask != nil {
		drawn = scratch.drawn[:count]
		for column := range drawn {
			drawn[column] = mask.opaqueAt(int(fromX)+column, int(fromY))
		}
	}
	pixels := source
	if blit.op.active() {
		pixels = scratch.destination[:count*2]
		if memory.Read(destinationAddress, pixels) != nil {
			return 0, nil
		}
		for column := 0; column < count; column++ {
			if drawn != nil && !drawn[column] {
				continue
			}
			value, answered := runtime.cachedPixelOp(blit.op,
				binary.LittleEndian.Uint16(source[column*2:]), binary.LittleEndian.Uint16(pixels[column*2:]))
			if !answered {
				count = column
				break
			}
			binary.LittleEndian.PutUint16(pixels[column*2:], value)
		}
	}
	for column := 0; column < count; {
		if drawn != nil && !drawn[column] {
			column++
			continue
		}
		run := count
		if drawn != nil {
			run = column + 1
			for run < count && drawn[run] {
				run++
			}
		}
		if err := runtime.writeCopyRun(blit, destinationAddress, pixels, column, run, toX, toY); err != nil {
			return int32(run), err
		}
		column = run
	}
	return int32(count), nil
}

// writeCopyRun stores the drawn columns [first, end) of a span, whose first
// pixel is at address, and marks them in the destination's transparency mask
// as the walk marks each pixel it writes. A run the surface refuses is written
// again a pixel at a time, so the pixels before the refused one are drawn and
// marked and the error names that pixel: where the walk would have stopped.
func (runtime *initializationRuntime) writeCopyRun(blit *framebufferCopy, address uint32, pixels []byte, first, end int, toX, toY int32) error {
	memory := runtime.client.core.Memory()
	mask := blit.opacity.destination
	if memory.Write(address+uint32(first)*2, pixels[first*2:end*2]) != nil {
		for column := first; column < end; column++ {
			if err := memory.Write(address+uint32(column)*2, pixels[column*2:column*2+2]); err != nil {
				return err
			}
			mask.markOpaque(int(toX)+column, int(toY))
		}
		return nil
	}
	if mask != nil {
		for column := first; column < end; column++ {
			mask.markOpaque(int(toX)+column, int(toY))
		}
	}
	return nil
}

// copyPixels draws columns [first, end) of one row of a copy a pixel at a
// time: each pixel's source read, operation, write and mark happen before the
// next pixel is looked at. Every copy was drawn this way before rows were read
// whole, and copyRow hands it each stretch where reading whole could differ.
func (runtime *initializationRuntime) copyPixels(blit *framebufferCopy, line, first, end int32) error {
	memory := runtime.client.core.Memory()
	fromY := blit.sourceY + line
	toY := blit.destinationY + line
	if fromY < 0 || toY < 0 || uint32(fromY) >= blit.source.height || uint32(toY) >= blit.destination.height {
		return nil
	}
	for column := first; column < end; column++ {
		fromX := blit.sourceX + column
		toX := blit.destinationX + column
		if fromX < 0 || toX < 0 || uint32(fromX) >= blit.source.width || uint32(toX) >= blit.destination.width {
			continue
		}
		if !blit.clip.contains(toX, toY) {
			continue
		}
		if !blit.opacity.source.opaqueAt(int(fromX), int(fromY)) {
			continue
		}
		var data [2]byte
		if blit.snapshot != nil {
			copy(data[:], blit.snapshot[(int(line)*int(blit.width)+int(column))*2:])
		} else if err := memory.Read(blit.source.pixels+uint32(fromY)*blit.source.bpl+uint32(fromX)*2, data[:]); err != nil {
			return err
		}
		address := blit.destination.pixels + uint32(toY)*blit.destination.bpl + uint32(toX)*2
		if err := runtime.blendPixel(blit.op, address, binary.LittleEndian.Uint16(data[:])); err != nil {
			return err
		}
		blit.opacity.destination.markOpaque(int(toX), int(toY))
	}
	return nil
}

// clipCopy narrows a copy to the rows and columns that both surfaces hold, and
// reports whether any remain. The copy loops already skip every other pixel one
// at a time, so the narrowed copy draws the same pixels in the same order; what
// changes is that those loops, and the snapshot an overlapping copy takes, are
// sized by the surfaces rather than by a width and height the guest may have
// passed from garbage.
func clipCopy(destination, source wipicFramebuffer, destinationX, destinationY, width, height, sourceX, sourceY int32) (int32, int32, int32, int32, int32, int32, bool) {
	span := func(to, from, length int32, toSize, fromSize uint32) (int64, int64) {
		first := max(0, -int64(to), -int64(from))
		end := min(int64(length), int64(toSize)-int64(to), int64(fromSize)-int64(from))
		return first, end
	}
	firstColumn, endColumn := span(destinationX, sourceX, width, destination.width, source.width)
	firstLine, endLine := span(destinationY, sourceY, height, destination.height, source.height)
	if firstColumn >= endColumn || firstLine >= endLine {
		return 0, 0, 0, 0, 0, 0, false
	}
	return int32(int64(destinationX) + firstColumn), int32(int64(destinationY) + firstLine),
		int32(endColumn - firstColumn), int32(endLine - firstLine),
		int32(int64(sourceX) + firstColumn), int32(int64(sourceY) + firstLine), true
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
	runtime.recordScreenFlush()
	return runtime.publishIntermediateFrame()
}

func (runtime *initializationRuntime) recordScreenFlush() {
	if runtime.screenFramebuffer == 0 {
		// Nothing was drawn through the screen framebuffer yet.
		return
	}
	runtime.client.framePending = true
	runtime.client.partialFrame = false
	runtime.client.flushCount++
	// A flush from outside the card paint is the title publishing a frame of
	// its own. Which of the two paths owns the screen is decided in
	// paintTopCard, and this is the half of the evidence it cannot see.
	if !runtime.repaintServicing {
		runtime.guestFlushedOwnFrame = true
	}
	// A frame published from a guest thread makes that thread's next declared
	// wait a frame period rather than any other kind of sleep. See
	// frameLoopPeriod, which is the only reader and the only thing that clears
	// it. The Host's own round paint runs on the client thread and sets
	// nothing here, because a wait declared there is already a frame period by
	// the time it is answered — see deferClientWait.
	runtime.countDiagnostic("flush lcd")
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

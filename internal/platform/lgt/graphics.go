package lgt

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/glyph"
)

// framebuffer is one drawable surface. LGT games reach the LCD's pixels
// directly through the OEM framebuffer accessors, so the pixels live in guest
// memory as well as here: `address` is where the guest sees them.
type framebuffer struct {
	handle  uint32
	width   int
	height  int
	address uint32
	pixels  []uint16
	// screen marks the LCD itself, which is the only surface MC_grpFlushLcd
	// presents.
	screen bool
	// opaque is the transparency an image's encoding declared, one entry per
	// pixel, and nil when every pixel is drawn — which is every surface that
	// is not a decoded image. RGB565 has no alpha channel to carry it. See
	// wipic_image.go.
	opaque []bool
}

// bytesPerLine is what MC_grpGetFrameBufferBpl reports: RGB565 is two bytes a
// pixel.
func (buffer *framebuffer) bytesPerLine() int { return buffer.width * 2 }

// graphicsContext is the clip, colour and font state a draw call reads. It is
// not owned by this platform: MC_GrpContext is a structure the game allocates
// and hands to every draw call, so this is a view read out of guest memory for
// the duration of one call rather than a registered object.
type graphicsContext struct {
	target      *framebuffer
	clipX       int
	clipY       int
	clipWidth   int
	clipHeight  int
	foreground  uint16
	background  uint16
	fontHeight  int
	transparent bool
	// xor draws the difference between the colour and what is already there,
	// which is the mode a Java title sets with `Graphics.setXORMode`.
	xor bool
	// op is the pixel operation the game installed in this context, and the
	// three fields below are what running it needs. The specification makes
	// the drawing modes exclusive — setting one cancels the other — so xor
	// wins over an operation rather than compounding with it.
	//
	// The call's context and thread are held here rather than passed down
	// because every draw path funnels through put, and this structure already
	// lives for exactly one platform call: it is a view of guest memory read
	// on entry and discarded on return, not a registered object.
	op     pixelOp
	client *Client
	ctx    context.Context
	thread *armcore.Thread
	// err is the first failure a pixel operation reported. put has no way to
	// return one and a draw that silently kept going would write the rest of
	// its pixels unblended, so the draw slot checks this before it answers.
	err error
}

// The MC_GrpContext layout and the field identifiers MC_grpSetContext selects
// with. The identifiers are confirmed by the titles here: one sets 1 six
// hundred times while drawing (a colour, in this LCD's 16-bit pixel format), 0
// with a pointer when it changes what it is allowed to touch, and 7 once at
// startup with a font. The rest of the layout is carried along because the
// structure is the game's own and the offsets have to agree with whatever else
// it does to it.
const (
	grpContextMask        = 0
	grpContextClip        = 4 // four uint16: left, top, right, bottom
	grpContextForeground  = 12
	grpContextBackground  = 16
	grpContextTransparent = 20
	grpContextAlpha       = 24
	grpContextOffset      = 28 // two uint16: x, y
	grpContextPixelOp     = 32
	grpContextParam1      = 36
	grpContextReserved    = 40
	grpContextFont        = 44
	grpContextStyle       = 48
	grpContextSize        = 52
)

const (
	grpFieldClip = iota
	grpFieldForeground
	grpFieldBackground
	grpFieldTransparent
	grpFieldAlpha
	grpFieldPixelOp
	grpFieldParam1
	grpFieldFont
	grpFieldStyle
	grpFieldXorMode
	grpFieldOffset
	grpFieldOutline
)

// guestClock is the game's timeline. It advances with the session rather than
// with the wall clock, so a Host batching ticks sees the same sequence as one
// running in real time, only faster — the same decision KTF made.
//
// It also advances with the work the guest does, and that part is not a
// refinement: a title's loading screen spins on MC_knlCurrentTime waiting for
// a deadline to pass, and it does that inside one Clet call. A clock that only
// the Host moves cannot move during a call, so the wait never ends and the run
// dies on the instruction ceiling with the same timestamp on every read. Time
// that advances with instructions retired is also what the handset had.
//
// The work part is measured from the last tick rather than from the start, so
// the two do not compound over a run: a tick sets the floor and moves the
// baseline, and instructions carry the clock forward from there until the next
// one. Within a tick, a frame that does more work is a frame that took longer,
// which is the same thing a handset would have reported.
//
// **A floor is the larger of the two, not their sum.** A tick is how much time
// the Host says has passed; the work is how much of it the guest was busy for.
// A frame that spent 43ms of guest work inside a 50ms tick took 50ms — 7ms of
// it idle — and adding them made it 93. That ran every title here at about
// 1.9x speed, which is invisible in a screenshot and wrong in every animation,
// timer and cutscene. Only when the work overruns the tick does it win, which
// is the case that matters for a spin-wait: a loading screen that burns three
// seconds of instructions inside one call moves the clock three seconds.
type guestClock struct {
	mu       sync.Mutex
	elapsed  time.Duration
	origin   time.Time
	steps    func() uint64
	baseline uint64
}

// guestInstructionsPerMillisecond is the rate the work clock runs at — the
// speed of the handset this platform is standing in for. It decides two
// things: what a spin-wait costs (a game waiting 100ms spends fifteen million
// instructions on it, well inside one Clet call's three billion), and, because
// a frame's computation is charged to the guest's own clock, **whether a title
// gets the frame period it asks for**.
//
// It is a variable rather than a constant only so the measurement that picked
// it can be repeated: `TestRateSweep` sets it and puts it back. Nothing in the
// running emulator writes it.
//
// The number is 150,000 because that is where the local corpus stops being
// held back by it — see `docs/lgt.md`, "What the rate has to be for a title to
// get the period it asks for".
var guestInstructionsPerMillisecond uint64 = 150_000

func newGuestClock(steps func() uint64) *guestClock {
	return &guestClock{
		origin: time.Date(2008, time.January, 1, 0, 0, 0, 0, time.UTC),
		steps:  steps,
	}
}

func (clock *guestClock) advance(delta time.Duration) time.Duration {
	clock.mu.Lock()
	// The tick is a floor rather than an addition: the guest was busy for the
	// work and idle for the rest of the tick, so the tick is what passed unless
	// the work ran past it. Taking the larger of the two is also what keeps
	// time from going backwards at the boundary, since a reader inside the tick
	// has already seen elapsed plus the work.
	applied := delta
	if work := clock.workSince(); work > delta {
		applied = work
	}
	clock.elapsed += applied
	if clock.steps != nil {
		clock.baseline = clock.steps()
	}
	clock.mu.Unlock()
	return applied
}

// workSince is how much time the instructions retired since the last tick
// stand for. The caller holds the lock.
func (clock *guestClock) workSince() time.Duration {
	if clock.steps == nil {
		return 0
	}
	steps := clock.steps()
	if steps <= clock.baseline {
		return 0
	}
	return time.Duration((steps-clock.baseline)/guestInstructionsPerMillisecond) * time.Millisecond
}

func (clock *guestClock) now() time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.elapsed + clock.workSince()
}

func (clock *guestClock) millis() int64 {
	return clock.now().Milliseconds()
}

func (clock *guestClock) unixMillis() int64 {
	return clock.origin.Add(clock.now()).UnixMilli()
}

// timer is one MC_knlSetTimer registration.
// timer is one MCTimer. A timer is named by the address of the guest structure
// the game hands to MC_knlDefTimer, not by a handle this platform invents: the
// specification's MC_knlSetTimer and MC_knlUnsetTimer both take that same
// pointer back, and the callback is registered against it.
type timer struct {
	structure uint32
	callback  uint32
	param     uint32
	dueAt     time.Duration
	armed     bool
}

// A surface's handle is the address of a record in guest memory, because
// `MC_GrpImage` is a `void *` in the specification and one title reads it as
// one: it dereferences the handle a create wrote and stores into what it finds.
// A counter would have satisfied every accessor here and still died on that
// load — an unmapped read at the small integer the counter had reached.
//
// Word zero is what `MC_grpGetImageFrameBuffer` answers, so a title that takes
// the framebuffer out of the structure gets what the accessor would have given
// it; the three words after it are the data pointer, the bytes per line and the
// dimensions the specification says a framebuffer holds, in that order. The
// rest of the record is a vendor's business and is left zero: what matters is
// that it is mapped and the game's own, so a store into a field this platform
// never chose lands in the image rather than in whatever came after it.
const (
	surfaceRecordBytes       = 0x40
	surfaceRecordFrameBuffer = 0x00
	surfaceRecordData        = 0x04
	surfaceRecordBytesPerRow = 0x08
	surfaceRecordWidth       = 0x0c
	surfaceRecordHeight      = 0x10
)

// newFramebuffer allocates a surface and maps its pixels into guest memory so
// a Clet can write them directly.
func (client *Client) newFramebuffer(width, height int, screen bool) (*framebuffer, error) {
	if width <= 0 || height <= 0 || width > 4096 || height > 4096 {
		return nil, fmt.Errorf("LGT framebuffer %dx%d is outside the supported range", width, height)
	}
	address, err := client.allocate(uint64(width) * uint64(height) * 2)
	if err != nil {
		return nil, err
	}
	buffer := &framebuffer{
		width:   width,
		height:  height,
		address: address,
		pixels:  make([]uint16, width*height),
		screen:  screen,
	}
	if err := client.mapSurface(buffer); err != nil {
		return nil, err
	}
	return buffer, nil
}

// mapSurface gives a surface the guest record its handle names, and registers
// it under that address.
func (client *Client) mapSurface(buffer *framebuffer) error {
	record, err := client.allocate(surfaceRecordBytes)
	if err != nil {
		return err
	}
	words := make([]uint32, surfaceRecordBytes/4)
	words[surfaceRecordFrameBuffer/4] = record
	words[surfaceRecordData/4] = buffer.address
	words[surfaceRecordBytesPerRow/4] = uint32(buffer.bytesPerLine())
	words[surfaceRecordWidth/4] = uint32(buffer.width)
	words[surfaceRecordHeight/4] = uint32(buffer.height)
	for index, word := range words {
		if err := client.writeWord(record+uint32(index)*4, word); err != nil {
			return err
		}
	}
	buffer.handle = record
	client.framebuffers[record] = buffer
	return nil
}

// releaseSurface drops a surface and both of the blocks it holds: the record
// its handle names and the pixels the record points at.
func (client *Client) releaseSurface(buffer *framebuffer) {
	if buffer == nil || buffer.screen {
		return
	}
	client.arena.release(buffer.address)
	client.arena.release(buffer.handle)
	delete(client.framebuffers, buffer.handle)
}

func (client *Client) takeHandle() uint32 {
	handle := client.nextHandle
	client.nextHandle++
	return handle
}

// syncFromGuest reads a surface's pixels back out of guest memory. A Clet that
// drew straight into the framebuffer has bypassed every MC_grp call, so the
// runtime's copy is only correct if it re-reads before it uses them.
func (client *Client) syncFromGuest(buffer *framebuffer) error {
	if buffer == nil || buffer.address == 0 {
		return nil
	}
	raw := make([]byte, len(buffer.pixels)*2)
	if err := client.core.Memory().Read(buffer.address, raw); err != nil {
		return fmt.Errorf("read LGT framebuffer at %#x: %w", buffer.address, err)
	}
	for index := range buffer.pixels {
		buffer.pixels[index] = uint16(raw[index*2]) | uint16(raw[index*2+1])<<8
	}
	return nil
}

// syncToGuest writes the runtime's pixels back, which is what a draw call
// done here has to do so the guest sees its own surface change.
func (client *Client) syncToGuest(buffer *framebuffer) error {
	if buffer == nil || buffer.address == 0 {
		return nil
	}
	raw := make([]byte, len(buffer.pixels)*2)
	for index, pixel := range buffer.pixels {
		raw[index*2] = byte(pixel)
		raw[index*2+1] = byte(pixel >> 8)
	}
	if err := client.core.Memory().Write(buffer.address, raw); err != nil {
		return fmt.Errorf("write LGT framebuffer at %#x: %w", buffer.address, err)
	}
	return nil
}

// clipped reports whether a point is inside a context's clip and the surface.
func (context *graphicsContext) clipped(x, y int) bool {
	if context.target == nil {
		return true
	}
	if x < 0 || y < 0 || x >= context.target.width || y >= context.target.height {
		return true
	}
	return x < context.clipX || y < context.clipY ||
		x >= context.clipX+context.clipWidth || y >= context.clipY+context.clipHeight
}

func (context *graphicsContext) put(x, y int, color uint16) {
	if context.clipped(x, y) {
		return
	}
	index := y*context.target.width + x
	if context.xor {
		context.target.pixels[index] ^= color
		return
	}
	if context.op.active() {
		if context.err != nil {
			return
		}
		result, err := context.client.applyPixelOp(
			context.ctx, context.thread, context.op, context.target.pixels[index], color)
		if err != nil {
			context.err = err
			return
		}
		context.target.pixels[index] = result
		return
	}
	context.target.pixels[index] = color
}

func (context *graphicsContext) fill(x, y, width, height int, color uint16) {
	for row := y; row < y+height; row++ {
		for column := x; column < x+width; column++ {
			context.put(column, row, color)
		}
	}
}

// line draws with the integer Bresenham the rest of this runtime uses.
func (context *graphicsContext) line(x1, y1, x2, y2 int, color uint16) {
	deltaX, deltaY := abs(x2-x1), -abs(y2-y1)
	stepX, stepY := 1, 1
	if x1 > x2 {
		stepX = -1
	}
	if y1 > y2 {
		stepY = -1
	}
	err := deltaX + deltaY
	for step := 0; step <= deltaX-deltaY+1; step++ {
		context.put(x1, y1, color)
		if x1 == x2 && y1 == y2 {
			return
		}
		doubled := 2 * err
		if doubled >= deltaY {
			err += deltaY
			x1 += stepX
		}
		if doubled <= deltaX {
			err += deltaX
			y1 += stepY
		}
	}
}

func (context *graphicsContext) rect(x, y, width, height int, color uint16) {
	if width <= 0 || height <= 0 {
		return
	}
	context.line(x, y, x+width-1, y, color)
	context.line(x, y+height-1, x+width-1, y+height-1, color)
	context.line(x, y, x, y+height-1, color)
	context.line(x+width-1, y, x+width-1, y+height-1, color)
}

// polygon outlines or fills a closed polygon given as the parallel coordinate
// arrays the API passes. The fill is the even-odd scanline rule, the same one
// the KTF runtime's polygon takes, so a shape drawn through either platform
// covers the same pixels.
func (context *graphicsContext) polygon(xs, ys []int, color uint16, fill bool) {
	count := min(len(xs), len(ys))
	if count < 2 {
		return
	}
	if !fill {
		for index := 0; index < count; index++ {
			next := (index + 1) % count
			context.line(xs[index], ys[index], xs[next], ys[next], color)
		}
		return
	}
	top, bottom := ys[0], ys[0]
	for _, y := range ys[:count] {
		top, bottom = min(top, y), max(bottom, y)
	}
	crossings := make([]int, 0, count)
	for y := top; y <= bottom; y++ {
		crossings = crossings[:0]
		for index := 0; index < count; index++ {
			next := (index + 1) % count
			y0, y1 := ys[index], ys[next]
			if y0 == y1 || y < min(y0, y1) || y >= max(y0, y1) {
				continue
			}
			x0, x1 := xs[index], xs[next]
			crossings = append(crossings, x0+(y-y0)*(x1-x0)/(y1-y0))
		}
		sort.Ints(crossings)
		for index := 0; index+1 < len(crossings); index += 2 {
			context.fill(crossings[index], y, crossings[index+1]-crossings[index]+1, 1, color)
		}
	}
}

// textFace is the pixel face every LGT title's text is drawn and measured
// with, and it is the small one for the same reason KTF's is — see that
// runtime's fontFace. These titles declare 240x320 and the large face is what
// a 240x320 screen suggested, but a game reads its own layout out of the
// metrics rather than out of the screen: at 16 dots a notice box sized by its
// title for eleven syllables holds ten, and the eleventh wraps or runs off the
// right edge. The same strings fit inside the same boxes at the handset
// metrics, which is what settles it here as it did there. This is one face for
// the Clet path and the Java path alike, because both draw through here.
func textFace() *glyph.Face { return glyph.Handset() }

// text draws through the shared glyph package, so LGT, KTF and SKT render
// the same characters from the same font. The y coordinate is the top of the
// line, which is what MC_grpDrawString means, so each glyph is placed from
// its own ascent.
func (context *graphicsContext) text(x, y int, value string, color uint16) {
	face := textFace()
	cursor := x
	baseline := y + face.Ascent
	for _, symbol := range value {
		bitmap := face.Render(symbol)
		for row, bits := range bitmap.Rows {
			for column := 0; column < bitmap.Width; column++ {
				if bits&(1<<uint(bitmap.Width-1-column)) == 0 {
					continue
				}
				context.put(cursor+column, baseline-bitmap.Ascent+row, color)
			}
		}
		cursor += bitmap.Advance
	}
}

// textWidth is the advance sum, which MC_grpGetStringWidth answers with.
func textWidth(value string) int {
	face := textFace()
	total := 0
	for _, symbol := range value {
		total += face.Render(symbol).Advance
	}
	return total
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// rgb565 packs an 8-bit-per-channel colour the way the LCD stores it.
func rgb565(red, green, blue uint32) uint16 {
	return uint16((red&0xf8)<<8 | (green&0xfc)<<3 | (blue&0xf8)>>3)
}

// unpack565 is the inverse, which MC_grpGetRGBFromPixel answers with.
func unpack565(pixel uint16) (uint32, uint32, uint32) {
	red := uint32(pixel>>11) & 0x1f
	green := uint32(pixel>>5) & 0x3f
	blue := uint32(pixel) & 0x1f
	return red<<3 | red>>2, green<<2 | green>>4, blue<<3 | blue>>2
}

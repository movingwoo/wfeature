package lgt

import (
	"context"
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// LGT's WIPI C slots are a flat index whose module bases follow the spec's
// own section numbering, 100 (0x64) per module: 0x64 is the kernel (5.1.1),
// 0xc8 graphics (5.1.2), 0x190 the filesystem (5.1.4), 0x3e8 the C standard
// library. Everything below 0x64 is LGT's own OEM block, and that is where
// the framebuffer accessors live — the ones Gamevil titles draw nearly
// everything through.
const (
	slotCletRegister uint32 = 0x03

	slotFramebufferPointer uint32 = 0x32
	slotFramebufferWidth   uint32 = 0x33
	slotFramebufferHeight  uint32 = 0x34
	slotFramebufferBpl     uint32 = 0x35
	slotFramebufferBpp     uint32 = 0x36

	slotPrintk  uint32 = 0x64
	slotSprintk uint32 = 0x65
	// The kernel block's slots land exactly on the specification's own
	// function order — printk first at the block base, then alloc at 0x75
	// through getResource at 0x81, fourteen in a row. MC_knlExit is the fifth
	// function, so it is 0x68. It was 0x6b here, which is the eighth,
	// MC_knlGetParentProgramID: a title asking who launched it would have been
	// answered by ending its own session, and one asking to exit would have
	// been told the slot does not exist. No title here reaches either, so this
	// is a correction made from the numbering rather than from a failure.
	slotExit            uint32 = 0x68
	slotGetCurProgramID uint32 = 0x6a
	slotAlloc           uint32 = 0x75
	slotCalloc          uint32 = 0x76
	slotFree            uint32 = 0x77
	slotTotalMemory     uint32 = 0x78
	slotFreeMemory      uint32 = 0x79
	slotDefTimer        uint32 = 0x7a
	slotSetTimer        uint32 = 0x7b
	slotUnsetTimer      uint32 = 0x7c
	slotCurrentTime     uint32 = 0x7d
	slotGetProperty     uint32 = 0x7e
	slotSetProperty     uint32 = 0x7f
	slotGetResourceID   uint32 = 0x80
	slotGetResource     uint32 = 0x81

	// slotProgramApplicationID answers the application id of a program, as a
	// string, given the numeric id MC_knlGetCurProgramID answers with. The
	// specification does not name it and neither does the original runtime,
	// but what the callers do settles it: one title asks its own id, formats
	// the answer with "%s", uppercases it, and compares it with an eight
	// character constant compiled into the module — the same value its archive
	// declares as its AID. See the archive check in `applicationID`.
	slotProgramApplicationID uint32 = 0x97

	slotGetImageProperty     uint32 = 0xc8
	slotGetImageFramebuffer  uint32 = 0xc9
	slotGetScreenFramebuffer uint32 = 0xca
	slotDestroyOffscreen     uint32 = 0xcb
	slotCreateOffscreen      uint32 = 0xcc
	slotInitContext          uint32 = 0xcd
	slotSetContext           uint32 = 0xce
	slotGetContext           uint32 = 0xcf
	slotPutPixel             uint32 = 0xd0
	slotDrawLine             uint32 = 0xd1
	slotDrawRect             uint32 = 0xd2
	slotFillRect             uint32 = 0xd3
	slotCopyFramebuffer      uint32 = 0xd4
	slotDrawImage            uint32 = 0xd5
	slotCopyArea             uint32 = 0xd7
	slotDrawString           uint32 = 0xda
	slotGetRGBPixels         uint32 = 0xdc
	slotSetRGBPixels         uint32 = 0xdd
	slotFlushLcd             uint32 = 0xde
	slotGetPixelFromRGB      uint32 = 0xdf
	slotGetRGBFromPixel      uint32 = 0xe0
	slotGetDisplayInfo       uint32 = 0xe1
	slotRepaint              uint32 = 0xe2
	slotGetFont              uint32 = 0xe3
	slotGetFontHeight        uint32 = 0xe4
	slotGetFontAscent        uint32 = 0xe5
	slotGetFontDescent       uint32 = 0xe6
	slotGetStringWidth       uint32 = 0xe7
	slotCreateImage          uint32 = 0xe9
	slotDestroyImage         uint32 = 0xea
	slotDecodeNextImage      uint32 = 0xeb
	slotPostEvent            uint32 = 0xee
	// The graphics block ends with the two polygon calls, which is where the
	// specification's function order puts them once the block's two unnamed
	// slots are counted — one before `MC_grpCopyArea` and one before
	// `MC_grpPostEvent`, both of which the identified slots around them fix.
	// A title's tutorial confirms the reading: it calls `0xf0` with a surface,
	// two stack addresses, a point count of four and a context, which is
	// `MC_grpFillPolygon(dst, xPoints, yPoints, nPoints, pgc)` and nothing
	// else in the block.
	slotDrawPolygon     uint32 = 0xef
	slotFillPolygon     uint32 = 0xf0
	slotFsOpen          uint32 = 0x190
	slotFsRead          uint32 = 0x191
	slotFsWrite         uint32 = 0x192
	slotFsClose         uint32 = 0x193
	slotFsSeek          uint32 = 0x194
	slotFsFileAttribute uint32 = 0x195
	slotFsRemove        uint32 = 0x196
	slotFsMkDir         uint32 = 0x198
	slotFsRmDir         uint32 = 0x199
	slotFsTotalSpace    uint32 = 0x19b
	slotFsAvailable     uint32 = 0x19c
	slotFsIsExist       uint32 = 0x1a0
	// The network block starts at MC_netConnect and follows the
	// specification's order, which the set a title imports confirms: connect,
	// close, socket connect/write/read/close and the two callback setters are
	// exactly a socket client's needs.
	slotNetConnect       uint32 = 0x258
	slotNetClose         uint32 = 0x259
	slotNetSocketConnect uint32 = 0x25b
	slotNetSocketWrite   uint32 = 0x25c
	slotNetSocketRead    uint32 = 0x25d
	slotNetSocketClose   uint32 = 0x25e
	slotNetSetReadCB     uint32 = 0x265
	slotNetSetWriteCB    uint32 = 0x266
	// Block nine is the utility block, in the specification's own order, the
	// same rule the identified blocks follow. Two calls named it: one title's
	// authenticating screen passes 0x385 a port and keeps the result as a
	// halfword, and passes 0x388 a pointer to the string "218.50.3.88" and
	// keeps the result as a word — a host-to-network short and a dotted-quad
	// address, which is the second and the fifth of the six utility functions.
	slotUtilHtonl       uint32 = 0x384
	slotUtilHtons       uint32 = 0x385
	slotUtilNtohl       uint32 = 0x386
	slotUtilNtohs       uint32 = 0x387
	slotUtilInetAddrInt uint32 = 0x388
	slotUtilInetAddrStr uint32 = 0x389
	slotBackLight       uint32 = 0x578
	slotVibrator        uint32 = 0x4c1
	slotClipCreate      uint32 = 0x4b0
	slotClipFree        uint32 = 0x4b1
	slotClipPlay        uint32 = 0x4ba
	slotClipStop        uint32 = 0x4bd
	slotSetMuteState    uint32 = 0x4d1
	slotGetMuteState    uint32 = 0x4d2
)

// directColorType is MH_GRP_DIRECT_COLOR_TYPE, the colour type of a display
// that addresses pixels directly instead of through a palette.
const directColorType uint32 = 1 << 0

// The WIPI error codes a slot answers with.
const (
	wipiSuccess     int32 = 0
	wipiError       int32 = -1
	wipiNoEntry     int32 = -12
	wipiShortBuffer int32 = -18
)

// answerCode writes a signed WIPI result into r0. It exists because the
// result constants are typed int32 and Go will not convert a negative typed
// constant to uint32 inline.
func answerCode(thread *armcore.Thread, code int32) error {
	return thread.SetRegister(0, uint32(code))
}

// knownWIPICSlot reports whether this platform implements a slot. The import
// table hands out a stub either way — a module resolves everything it might
// use at startup, and refusing there would stop a game over a function it
// never calls — so this only decides whether reaching the slot is an error.
func knownWIPICSlot(slot uint32) bool {
	switch slot {
	case slotCletRegister, slotFramebufferPointer, slotFramebufferWidth,
		slotFramebufferHeight, slotFramebufferBpl, slotFramebufferBpp,
		slotPrintk, slotSprintk, slotGetCurProgramID, slotExit, slotAlloc, slotCalloc,
		slotFree, slotTotalMemory, slotFreeMemory, slotDefTimer, slotSetTimer,
		slotUnsetTimer, slotCurrentTime, slotGetProperty, slotSetProperty,
		slotGetResourceID, slotGetResource, slotProgramApplicationID,
		slotGetImageProperty,
		slotGetImageFramebuffer, slotGetScreenFramebuffer, slotDestroyOffscreen,
		slotCreateOffscreen, slotInitContext, slotSetContext, slotGetContext,
		slotPutPixel, slotDrawLine, slotDrawRect, slotFillRect,
		slotCopyFramebuffer, slotDrawImage, slotCopyArea, slotDrawString,
		slotGetRGBPixels, slotSetRGBPixels, slotFlushLcd, slotGetPixelFromRGB,
		slotGetRGBFromPixel, slotGetDisplayInfo, slotRepaint, slotGetFont,
		slotGetFontHeight, slotGetFontAscent, slotGetFontDescent,
		slotGetStringWidth, slotCreateImage, slotDestroyImage,
		slotDecodeNextImage, slotPostEvent, slotDrawPolygon, slotFillPolygon,
		slotIMGetSupportedModeCount, slotIMGetSupportedModes,
		slotIMSetCurrentMode, slotIMGetCurrentMode, slotIMHandleInput,
		slotFsOpen, slotFsRead, slotFsWrite,
		slotFsClose, slotFsSeek, slotFsFileAttribute, slotFsRemove,
		slotFsMkDir, slotFsRmDir, slotFsTotalSpace, slotFsAvailable, slotFsIsExist,
		slotNetConnect, slotNetClose, slotNetSocketConnect, slotNetSocketWrite,
		slotNetSocketRead, slotNetSocketClose, slotNetSetReadCB, slotNetSetWriteCB,
		slotUtilHtonl, slotUtilHtons, slotUtilNtohl, slotUtilNtohs,
		slotUtilInetAddrInt, slotUtilInetAddrStr,
		slotBackLight, slotVibrator, slotClipCreate, slotClipFree,
		slotClipGetType, slotClipPutData, slotClipClearData, slotClipGetVolume,
		slotClipSetVolume, slotClipPlay, slotClipPause, slotClipResume,
		slotClipStop, slotGetVolume, slotSetVolume, slotClipSetWaterMark,
		slotGetDefaultVolume, slotClipAllocPlayer,
		slotClipFreePlayer, slotSetSourceVolume, slotGetSourceVolume,
		slotSetMuteState, slotGetMuteState:
		return true
	}
	// The component block is served whole: every one of its slots is either
	// refused with a code the caller can read or accepted as the no-op it is.
	return slot >= slotUicCreateApplicationContext && slot <= slotUicLast
}

// handleWIPICSVC services one WIPI C call.
func (client *Client) handleWIPICSVC(ctx context.Context, thread *armcore.Thread, slot uint32) error {
	argument := func(index int) (uint32, error) { return thread.Register(index) }
	answer := func(value uint32) error { return thread.SetRegister(0, value) }
	answerInt := func(value int32) error { return thread.SetRegister(0, uint32(value)) }
	// A 64-bit result comes back in r0:r1, low word first. Writing r1 costs a
	// slot that returns 32 bits nothing, since r0 through r3 are scratch across
	// a call either way, but leaving it unwritten costs a slot that returns 64
	// bits everything: the caller reads whatever the register happened to hold.
	answer64 := func(value int64) error {
		if err := thread.SetRegister(0, uint32(value)); err != nil {
			return err
		}
		return thread.SetRegister(1, uint32(uint64(value)>>32))
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	switch slot {
	case slotCletRegister:
		table, err := argument(0)
		if err != nil {
			return err
		}
		return answerInt(client.registerClet(table))

	case slotFramebufferPointer, slotFramebufferWidth, slotFramebufferHeight, slotFramebufferBpl:
		handle, err := argument(0)
		if err != nil {
			return err
		}
		buffer := client.framebuffer(handle)
		if buffer == nil {
			return answerInt(wipiError)
		}
		switch slot {
		case slotFramebufferPointer:
			return answer(buffer.address)
		case slotFramebufferWidth:
			return answer(uint32(buffer.width))
		case slotFramebufferHeight:
			return answer(uint32(buffer.height))
		}
		return answer(uint32(buffer.bytesPerLine()))

	case slotFramebufferBpp:
		// Unlike its neighbours this one takes no argument: it reports the
		// depth of the LCD, not of a framebuffer.
		return answer(16)

	case slotPrintk:
		pointer, err := argument(0)
		if err != nil {
			return err
		}
		format, err := client.readCString(pointer)
		if err != nil {
			// A debug line that cannot be read is not worth failing over.
			return answerInt(wipiSuccess)
		}
		// The rendered line is what a game logs its own state and failures in;
		// the format string alone hides exactly the values that say why. A
		// format the subset cannot render still names the call site.
		text := format
		if rendered, renderErr := client.wipicFormat(thread, []byte(format), 1); renderErr == nil {
			text = string(rendered)
		}
		if client.logger != nil {
			client.logger.Debug("LGT printk", "text", text)
		}
		return answerInt(wipiSuccess)

	case slotSprintk:
		written, err := client.wipicSprintk(thread)
		if err != nil {
			return err
		}
		return answer(written)

	case slotGetCurProgramID:
		return answer(client.programID())

	case slotProgramApplicationID:
		identity, err := argument(0)
		if err != nil {
			return err
		}
		address, err := client.applicationID(identity)
		if err != nil {
			return err
		}
		return answer(address)

	case slotExit:
		client.exited = true
		return ErrGuestExited

	case slotAlloc, slotCalloc:
		// MC_knlCalloc takes one size, not C's (count, size) pair — it is
		// "alloc and zero" rather than an array allocator. Multiplying in a
		// second argument that is really the caller's stack turns a small
		// request into one that cannot be served, and the game then walks into
		// the null it was handed.
		size, err := argument(0)
		if err != nil {
			return err
		}
		address, ok := client.heap.allocate(uint64(size))
		if !ok {
			return answer(0)
		}
		if slot == slotCalloc {
			if err := client.core.Memory().Write(address, make([]byte, size)); err != nil {
				return err
			}
		}
		return answer(address)

	case slotFree:
		address, err := argument(0)
		if err != nil {
			return err
		}
		client.heap.release(address)
		return answerInt(wipiSuccess)

	case slotTotalMemory:
		return answer(uint32(client.heap.capacity()))

	case slotFreeMemory:
		return answer(uint32(client.heap.capacity() - client.heap.used()))

	case slotCurrentTime:
		// MC_knlCurrentTime is `M_Int64 MC_knlCurrentTime()` and its unit is
		// milliseconds since 1970, so the answer is a 64-bit epoch time rather
		// than a 32-bit count since the run started. Both halves matter: a
		// title's loading screen subtracts two of these as 64-bit values and
		// spins until the difference passes a deadline, so a stale high word
		// makes the difference nonsense and the wait never ends.
		return answer64(client.clock.unixMillis())

	case slotDefTimer:
		return client.defineTimer(thread)

	case slotSetTimer:
		return answerInt(client.setTimer(thread))

	case slotUnsetTimer:
		structure, err := argument(0)
		if err != nil {
			return err
		}
		if entry := client.timers[structure]; entry != nil {
			entry.armed = false
		}
		return answerInt(wipiSuccess)

	case slotGetProperty:
		// The answers are shared with the other WIPI platform: the question is
		// about the handset, not about which runtime is being asked. Titles
		// here read the model and the phone number while deciding which path
		// to take, and refusing both left one of them dereferencing a buffer
		// it thought had been filled in.
		nameAddress, err := argument(0)
		if err != nil {
			return err
		}
		outAddress, err := argument(1)
		if err != nil {
			return err
		}
		size, err := argument(2)
		if err != nil {
			return err
		}
		name, err := client.readCString(nameAddress)
		if err != nil {
			return answerInt(wipiError)
		}
		value, known := wipic.SystemProperties[name]
		if !known {
			if client.logger != nil {
				client.logger.Debug("LGT unknown system property", "name", name)
			}
			return answerInt(wipiError)
		}
		if uint64(len(value))+1 > uint64(size) {
			return answerInt(wipiError)
		}
		if err := client.core.Memory().Write(outAddress, append([]byte(value), 0)); err != nil {
			return err
		}
		return answerInt(wipiSuccess)

	case slotSetProperty:
		return answerInt(wipiSuccess)

	case slotGetResourceID, slotGetResource:
		return client.handleResource(thread, slot)

	case slotGetScreenFramebuffer:
		pointer, err := argument(0)
		if err != nil {
			return err
		}
		if _, err := client.screenSurface(); err != nil {
			return err
		}
		if pointer != 0 {
			if err := client.writeFramebufferInfo(pointer, client.screen); err != nil {
				return err
			}
		}
		return answer(client.screen.handle)

	case slotCreateOffscreen:
		width, err := argument(0)
		if err != nil {
			return err
		}
		height, err := argument(1)
		if err != nil {
			return err
		}
		buffer, err := client.newFramebuffer(int(width), int(height), false)
		if err != nil {
			return err
		}
		return answer(buffer.handle)

	case slotDestroyOffscreen:
		handle, err := argument(0)
		if err != nil {
			return err
		}
		client.releaseSurface(client.framebuffer(handle))
		return answerInt(wipiSuccess)

	case slotInitContext:
		pointer, err := argument(0)
		if err != nil {
			return err
		}
		return answerInt(client.initContext(pointer))

	case slotSetContext, slotGetContext:
		return answerInt(client.transferContextField(thread, slot))

	case slotPutPixel, slotDrawLine, slotDrawRect, slotFillRect, slotDrawString,
		slotCopyArea, slotCopyFramebuffer, slotDrawImage, slotGetRGBPixels,
		slotSetRGBPixels, slotDrawPolygon, slotFillPolygon:
		return client.handleDraw(ctx, thread, slot)

	case slotFlushLcd, slotRepaint:
		if err := client.syncFromGuest(client.screen); err != nil {
			return err
		}
		client.framePending = true
		client.flushes++
		return answerInt(wipiSuccess)

	case slotGetPixelFromRGB:
		red, err := argument(0)
		if err != nil {
			return err
		}
		green, err := argument(1)
		if err != nil {
			return err
		}
		blue, err := argument(2)
		if err != nil {
			return err
		}
		return answer(uint32(rgb565(red, green, blue)))

	case slotGetRGBFromPixel:
		pixel, err := argument(0)
		if err != nil {
			return err
		}
		red, green, blue := unpack565(uint16(pixel))
		return answer(red<<16 | green<<8 | blue)

	case slotGetDisplayInfo:
		// MC_grpGetDisplayInfo(lcd, pdi): the display index comes first and the
		// structure second, the same way MC_grpRepaint leads with the LCD.
		// Reading the first argument as the pointer wrote nothing at all — the
		// index is zero for the primary display — and a title then read its own
		// uninitialised structure. That is not a call that fails: one title took
		// four bytes a pixel out of the stack residue and drew every screen at
		// twice its width, on top of a framebuffer whose other half it never
		// touched.
		pointer, err := argument(1)
		if err != nil {
			return err
		}
		if pointer == 0 {
			return answer(0)
		}
		// MC_GrpDisplayInfo, in its declared order. The masks are red, blue,
		// green — not the usual order, and the structure is the only place the
		// LCD's pixel layout is written down for the game.
		for index, value := range []uint32{
			16, 16,
			uint32(client.screen.width), uint32(client.screen.height),
			uint32(client.screen.bytesPerLine()),
			directColorType,
			0xf800, 0x001f, 0x07e0,
		} {
			if err := client.writeWord(pointer+uint32(index)*4, value); err != nil {
				return err
			}
		}
		// One is "this display exists", which is what a caller branches on.
		return answer(1)

	case slotGetFont:
		return answer(1)

	case slotGetFontHeight:
		return answer(uint32(defaultFontHeight()))

	case slotGetFontAscent:
		return answer(uint32(defaultFontAscent()))

	case slotGetFontDescent:
		return answer(uint32(defaultFontHeight() - defaultFontAscent()))

	case slotGetStringWidth:
		pointer, err := argument(1)
		if err != nil {
			return err
		}
		// Measured as text, because it is measuring what drawString will draw.
		text, err := client.readCText(pointer)
		if err != nil {
			return err
		}
		return answer(uint32(textWidth(text)))

	case slotCreateImage:
		return client.createImage(thread)

	case slotDestroyImage:
		return client.destroyImage(thread)

	case slotDecodeNextImage:
		return client.decodeNextImage(thread)

	case slotGetImageProperty:
		return client.imageProperty(thread)

	case slotGetImageFramebuffer:
		// An image is a framebuffer here, so it answers with itself. See
		// wipic_image.go.
		handle, err := argument(0)
		if err != nil {
			return err
		}
		if client.framebuffer(handle) == nil {
			return answerInt(wipiError)
		}
		return answer(handle)

	case slotPostEvent:
		kind, err := argument(1)
		if err != nil {
			return err
		}
		first, err := argument(2)
		if err != nil {
			return err
		}
		second, err := argument(3)
		if err != nil {
			return err
		}
		if len(client.events) < maxQueuedEvents {
			client.events = append(client.events, pendingEvent{kind: kind, param1: first, param2: second})
		}
		return answerInt(wipiSuccess)

	case slotIMGetSupportedModeCount, slotIMGetSupportedModes,
		slotIMSetCurrentMode, slotIMGetCurrentMode, slotIMHandleInput:
		return client.handleInputMethod(thread, slot)

	case slotFsOpen, slotFsRead, slotFsWrite, slotFsClose, slotFsSeek,
		slotFsFileAttribute, slotFsRemove, slotFsMkDir, slotFsRmDir,
		slotFsTotalSpace, slotFsAvailable, slotFsIsExist:
		return client.handleFile(thread, slot)

	case slotNetConnect:
		// The one call in the block that reports through a callback rather
		// than through its return value. See wipic_net.go.
		return answerInt(client.connectNetwork(thread))

	case slotNetSocketConnect, slotNetSocketWrite,
		slotNetSocketRead, slotNetSocketClose, slotNetSetReadCB, slotNetSetWriteCB:
		// There is no network. Reporting an error is what the game's own
		// state machine handles; claiming a connection would make it wait
		// for data that never arrives. The rest of the block answers the same
		// way, because a title that is refused a connection still tears down
		// what it had started, and stopping it there would turn a handled
		// refusal into a crash.
		return answerInt(wipiError)

	case slotNetClose:
		// MC_netClose returns void, so there is no failure to report. It does
		// end the dials that have not reported yet.
		client.cancelNetConnects()
		return answerInt(wipiSuccess)

	case slotUtilHtonl, slotUtilNtohl:
		// The guest is little-endian, so both directions are the same swap.
		value, err := argument(0)
		if err != nil {
			return err
		}
		return answer(bits.ReverseBytes32(value))

	case slotUtilHtons, slotUtilNtohs:
		value, err := argument(0)
		if err != nil {
			return err
		}
		return answer(uint32(bits.ReverseBytes16(uint16(value))))

	case slotUtilInetAddrInt:
		// A dotted quad becomes the address in network byte order, which on
		// this guest is the first octet in the low byte. -1 is the documented
		// failure, and a name that is not a dotted quad is one: nothing here
		// resolves names.
		pointer, err := argument(0)
		if err != nil {
			return err
		}
		text, err := client.readCString(pointer)
		if err != nil {
			return err
		}
		address, ok := parseDottedQuad(text)
		if !ok {
			return answerInt(-1)
		}
		return answer(address)

	case slotUtilInetAddrStr:
		// The other direction, into the caller's buffer. It answers nothing.
		value, err := argument(0)
		if err != nil {
			return err
		}
		pointer, err := argument(1)
		if err != nil {
			return err
		}
		text := fmt.Sprintf("%d.%d.%d.%d", value&0xff, value>>8&0xff, value>>16&0xff, value>>24&0xff)
		if err := client.core.Memory().Write(pointer, append([]byte(text), 0)); err != nil {
			return err
		}
		return answerInt(wipiSuccess)

	case slotBackLight:
		return answerInt(wipiSuccess)

	case slotClipCreate, slotClipFree, slotClipGetType, slotClipPutData,
		slotClipClearData, slotClipGetVolume, slotClipSetVolume, slotClipPlay,
		slotClipPause, slotClipResume, slotClipStop, slotGetVolume, slotSetVolume,
		slotVibrator, slotClipSetWaterMark, slotGetDefaultVolume,
		slotClipAllocPlayer, slotClipFreePlayer,
		slotSetSourceVolume, slotGetSourceVolume, slotSetMuteState, slotGetMuteState:
		return client.handleMedia(thread, slot)
	}
	if slot >= slotUicCreateApplicationContext && slot <= slotUicLast {
		return client.handleUIC(thread, slot)
	}
	if reason, accepted := acceptedUnknownSlots[slot]; accepted {
		if client.logger != nil {
			client.logger.Debug("LGT unknown slot accepted", "slot", slot, "reason", reason)
		}
		// None of the remaining ones has a caller that dereferences what it
		// answers, so success is the whole answer. A slot whose caller does
		// needs a shape instead, argued for where it is recorded.
		return answerInt(wipiSuccess)
	}
	return fmt.Errorf("unimplemented LGT WIPI C slot %#x", slot)
}

// The font metrics the three font slots answer with. They are read off the
// face the renderer actually draws with rather than fixed here, because a game
// lays its own boxes out of what these answer and then draws into them: a
// height that disagrees with the glyphs is a layout that disagrees with the
// text inside it. See textFace.
func defaultFontHeight() int { return textFace().Height() }
func defaultFontAscent() int { return textFace().Ascent }

// registerClet stores the game's entry point table.
func (client *Client) registerClet(table uint32) int32 {
	if table == 0 {
		return wipiError
	}
	functions := CletFunctions{Address: table}
	targets := []*uint32{
		&functions.Start, &functions.Pause, &functions.Resume,
		&functions.Destroy, &functions.Paint, &functions.HandleEvent,
	}
	for index, target := range targets {
		value, err := client.readWord(table + uint32(index)*4)
		if err != nil {
			return wipiError
		}
		*target = value
	}
	client.clet = functions
	if client.logger != nil {
		// The entry points a Clet registers are the whole of what the platform
		// may call it through, and a zero among them is a thing the title
		// cannot be asked to do: a Clet with no event entry takes no keys at
		// all, which from the outside is a screen that will not advance.
		client.logger.Debug("LGT clet registered",
			"start", functions.Start, "pause", functions.Pause,
			"resume", functions.Resume, "destroy", functions.Destroy,
			"paint", functions.Paint, "event", functions.HandleEvent)
	}
	return wipiSuccess
}

// screenSurface answers the LCD as a surface the drawing calls can find, giving
// it a handle and guest pixels the first time anything asks. Both sides of the
// platform reach it this way: a Clet through MC_grpGetScreenFrameBuffer, and a
// Java title through the Graphics its card paints with.
func (client *Client) screenSurface() (*framebuffer, error) {
	if client.screen.handle != 0 {
		return client.screen, nil
	}
	address, err := client.allocate(uint64(client.screen.width) * uint64(client.screen.height) * 2)
	if err != nil {
		return nil, err
	}
	client.screen.address = address
	if err := client.mapSurface(client.screen); err != nil {
		return nil, err
	}
	return client.screen, nil
}

func (client *Client) framebuffer(handle uint32) *framebuffer {
	return client.framebuffers[handle]
}

// writeFramebufferInfo fills the { pointer, width, height, bpl, bpp } block a
// Clet passes to MC_grpGetScreenFrameBuffer.
func (client *Client) writeFramebufferInfo(pointer uint32, buffer *framebuffer) error {
	values := []uint32{
		buffer.address, uint32(buffer.width), uint32(buffer.height),
		uint32(buffer.bytesPerLine()), 16,
	}
	for index, value := range values {
		if err := client.writeWord(pointer+uint32(index)*4, value); err != nil {
			return err
		}
	}
	return nil
}

// programID is the numeric identity MC_knlGetCurProgramID answers with. The
// Gamevil engine pairs it with MC_grpPostEvent to post its own message codes
// to itself, so it only has to be stable.
func (client *Client) programID() uint32 {
	identity := client.archive.Descriptor.PID
	if identity == "" {
		identity = client.archive.Descriptor.AID
	}
	hash := uint32(2166136261)
	for _, symbol := range []byte(identity) {
		hash ^= uint32(symbol)
		hash *= 16777619
	}
	return hash
}

// applicationID answers the address of the application id string belonging to
// the program `identity` names, or zero when this platform is not hosting that
// program.
//
// This is the call an anti-piracy check asks. A title reads its own id, asks
// for the id string, uppercases it, and compares it with the application id
// compiled into its module; a copy that reached a handset any other way than
// through the store answers something else and the title stops at a notice
// screen instead of booting. The honest answer is therefore the archive's own
// declared AID: it is what a handset that downloaded this archive would say,
// and it is read out of the archive rather than chosen here.
//
// An archive that declares no AID gets a null, which is what the caller
// already handles — it tests the pointer before formatting it.
func (client *Client) applicationID(identity uint32) (uint32, error) {
	if identity != client.programID() {
		return 0, nil
	}
	if client.applicationIDAddress != 0 {
		return client.applicationIDAddress, nil
	}
	name := client.archive.Descriptor.AID
	if name == "" {
		return 0, nil
	}
	address, err := client.allocateBytes(append([]byte(name), 0))
	if err != nil {
		return 0, err
	}
	client.applicationIDAddress = address
	return address, nil
}

// initContext fills a game-owned MC_GrpContext with the defaults the
// specification gives it: `void MC_grpInitContext(MC_GrpContext *pgc)`. The
// structure belongs to the game — this platform never holds one — so the whole
// call is a write into guest memory. It used to answer a handle to an object of
// this platform's own, which meant every later call named a structure this
// platform had never heard of and every draw through it was refused.
func (client *Client) initContext(pointer uint32) int32 {
	if pointer == 0 {
		return wipiError
	}
	for offset := uint32(0); offset < grpContextSize; offset += 4 {
		if err := client.writeWord(pointer+offset, 0); err != nil {
			return wipiError
		}
	}
	// A fresh context may draw anywhere on the LCD and draws in white. The clip
	// is four uint16 — left, top, right, bottom — so the first word is already
	// zero and only the far corner has to be written.
	corner := uint32(client.screen.height-1)<<16 | uint32(client.screen.width-1)
	if err := client.writeWord(pointer+grpContextClip+4, corner); err != nil {
		return wipiError
	}
	if err := client.writeWord(pointer+grpContextForeground, 0xffff); err != nil {
		return wipiError
	}
	if err := client.writeWord(pointer+grpContextAlpha, 0xff); err != nil {
		return wipiError
	}
	return wipiSuccess
}

// transferContextField services MC_grpSetContext and MC_grpGetContext, which
// are `void f(MC_GrpContext *pgc, M_Int32 index, void *pv)`: one named field at
// a time. Clip and offset are rectangles behind a pointer; everything else is
// the word itself.
func (client *Client) transferContextField(thread *armcore.Thread, slot uint32) int32 {
	pointer, err := thread.Register(0)
	if err != nil {
		return wipiError
	}
	field, err := thread.Register(1)
	if err != nil {
		return wipiError
	}
	value, err := thread.Register(2)
	if err != nil {
		return wipiError
	}
	if pointer == 0 {
		return wipiError
	}
	offset, words := contextFieldOffset(field)
	if words == 0 {
		// A field this platform does not carry is accepted rather than
		// refused: the call has no return value for a game to read, and the
		// fields that matter to the ones here are all below.
		return wipiSuccess
	}
	for index := uint32(0); index < words; index++ {
		if slot == slotSetContext {
			source := value
			if offset == grpContextClip || offset == grpContextOffset {
				word, readErr := client.readWord(value + index*4)
				if readErr != nil {
					return wipiError
				}
				source = word
			}
			if err := client.writeWord(pointer+offset+index*4, source); err != nil {
				return wipiError
			}
			continue
		}
		word, readErr := client.readWord(pointer + offset + index*4)
		if readErr != nil {
			return wipiError
		}
		if err := client.writeWord(value+index*4, word); err != nil {
			return wipiError
		}
	}
	return wipiSuccess
}

// contextFieldOffset maps a field identifier to its place in the structure and
// how many words it occupies. A zero count means this platform does not carry
// the field.
func contextFieldOffset(field uint32) (uint32, uint32) {
	switch field {
	case grpFieldClip:
		return grpContextClip, 2
	case grpFieldForeground:
		return grpContextForeground, 1
	case grpFieldBackground:
		return grpContextBackground, 1
	case grpFieldTransparent:
		return grpContextTransparent, 1
	case grpFieldAlpha:
		return grpContextAlpha, 1
	case grpFieldPixelOp:
		return grpContextPixelOp, 1
	case grpFieldParam1:
		return grpContextParam1, 1
	case grpFieldFont:
		return grpContextFont, 1
	case grpFieldStyle:
		return grpContextStyle, 1
	case grpFieldOffset:
		return grpContextOffset, 1
	}
	return 0, 0
}

// contextFor reads a game-owned MC_GrpContext for the duration of one draw
// call. A clip that does not describe a rectangle is taken as the whole
// surface, because a game that never set one would otherwise draw nothing.
func (client *Client) contextFor(
	ctx context.Context, thread *armcore.Thread, target *framebuffer, pointer uint32,
) (*graphicsContext, error) {
	context := &graphicsContext{
		target:     target,
		clipWidth:  target.width,
		clipHeight: target.height,
		foreground: 0xffff,
		fontHeight: defaultFontHeight(),
		client:     client,
		ctx:        ctx,
		thread:     thread,
	}
	if pointer == 0 {
		return context, nil
	}
	words := make([]uint32, grpContextSize/4)
	for index := range words {
		word, err := client.readWord(pointer + uint32(index)*4)
		if err != nil {
			return nil, err
		}
		words[index] = word
	}
	context.foreground = uint16(words[grpContextForeground/4])
	context.background = uint16(words[grpContextBackground/4])
	left, top := int(words[grpContextClip/4]&0xffff), int(words[grpContextClip/4]>>16)
	right, bottom := int(words[(grpContextClip+4)/4]&0xffff), int(words[(grpContextClip+4)/4]>>16)
	if right > left && bottom > top {
		context.clipX, context.clipY = left, top
		context.clipWidth, context.clipHeight = right-left+1, bottom-top+1
	}
	context.op = client.readContextPixelOp(
		words[grpContextPixelOp/4], words[grpContextParam1/4])
	return context, nil
}

// defineTimer registers a callback against a guest timer structure:
// `void MC_knlDefTimer(MCTimer *tm, TIMERCB cb)`. The callback belongs to the
// structure rather than to a handle, because MC_knlSetTimer is handed the same
// pointer and nothing else — a title that defines a timer once and arms it a
// thousand times passes only `tm`.
func (client *Client) defineTimer(thread *armcore.Thread) error {
	structure, err := thread.Register(0)
	if err != nil {
		return err
	}
	callback, err := thread.Register(1)
	if err != nil {
		return err
	}
	if structure == 0 {
		return answerCode(thread, wipiError)
	}
	entry := client.timers[structure]
	if entry == nil {
		entry = &timer{structure: structure}
		client.timers[structure] = entry
	}
	entry.callback = callback
	entry.armed = false
	// The structure's own first word carries the callback on a handset, and a
	// title is free to read it back.
	if err := client.writeWord(structure, callback); err != nil {
		return err
	}
	// The specification gives this one no return value; answering zero is what
	// a caller that ignores it sees either way.
	return answerCode(thread, wipiSuccess)
}

// setTimer arms a timer: `M_Int32 MC_knlSetTimer(MCTimer *tm, M_Int64 timeout,
// void *parm)`. The 64-bit timeout occupies r1 and r2 without the even-register
// alignment a modern ABI would give it, so the parameter lands in r3 — which is
// how these modules are built, and reading r3 as the callback instead runs the
// parameter as code. The timer fires once; a repeating one is re-armed by its
// own callback.
func (client *Client) setTimer(thread *armcore.Thread) int32 {
	structure, err := thread.Register(0)
	if err != nil {
		return wipiError
	}
	low, err := thread.Register(1)
	if err != nil {
		return wipiError
	}
	high, err := thread.Register(2)
	if err != nil {
		return wipiError
	}
	param, err := thread.Register(3)
	if err != nil {
		return wipiError
	}
	entry := client.timers[structure]
	if entry == nil {
		entry = &timer{structure: structure}
		client.timers[structure] = entry
	}
	if entry.callback == 0 {
		// An unregistered timer has nothing to call. Reading the structure
		// covers a title that filled it in itself.
		if callback, readErr := client.readWord(structure); readErr == nil {
			entry.callback = callback
		}
	}
	timeout := uint64(high)<<32 | uint64(low)
	period := time.Duration(timeout) * time.Millisecond
	if period < minTimerPeriod {
		// See minTimerPeriod: a timer shorter than the display can show is
		// answered at the resolution the specification says a handset's own
		// timer has.
		period = minTimerPeriod
	}
	entry.param = param
	entry.dueAt = time.Duration(client.clock.millis())*time.Millisecond + period
	entry.armed = entry.callback != 0
	return wipiSuccess
}

// nextTimerDue is how long from now the earliest armed timer comes due, and
// whether there is one. A negative answer means one is already due.
//
// This is what a tick's length is taken from. A title's frame loop here is a
// timer that re-arms itself at the end of every frame, so the interval it asks
// for is the frame rate it wants, and a tick that always represents the same
// span rounds that interval up to a multiple of itself. The cost is not
// subtle: one title asks for 46ms, arms it a few milliseconds into a 50ms
// tick, lands 5ms past the next one and waits a whole further tick — 100ms of
// guest time for a frame it wanted at 55, which is half speed. Another asks
// for 1ms, meaning "as soon as you can", and gets 50.
func (client *Client) nextTimerDue() (time.Duration, bool) {
	now := time.Duration(client.clock.millis()) * time.Millisecond
	client.mu.Lock()
	defer client.mu.Unlock()
	var earliest time.Duration
	found := false
	for _, entry := range client.timers {
		if !entry.armed {
			continue
		}
		if wait := entry.dueAt - now; !found || wait < earliest {
			earliest, found = wait, true
		}
	}
	return earliest, found
}

// serviceTimers fires the timers that are due. It runs between guest calls,
// never inside one, so a timer callback cannot reenter the guest mid-frame.
func (client *Client) serviceTimers(ctx context.Context) error {
	now := time.Duration(client.clock.millis()) * time.Millisecond
	client.mu.Lock()
	var due []*timer
	for _, entry := range client.timers {
		if entry.armed && entry.dueAt <= now {
			due = append(due, entry)
		}
	}
	for _, entry := range due {
		entry.armed = false
	}
	client.mu.Unlock()

	// The callback is `void (*)(MCTimer *tm, void *parm)`, so it is handed the
	// structure it was defined against and the parameter the arming call set.
	for _, entry := range due {
		if _, err := client.call(ctx, entry.callback, []uint32{entry.structure, entry.param}); err != nil {
			return fmt.Errorf("run LGT timer callback at %#x: %w", entry.callback, err)
		}
	}
	return nil
}

// parseDottedQuad reads an IPv4 address in the only form MC_utilInetAddrInt
// takes, and answers it the way a little-endian guest's inet_addr does: the
// first octet in the low byte, so the word's bytes in memory are the address in
// network order.
func parseDottedQuad(text string) (uint32, bool) {
	parts := strings.Split(strings.TrimSpace(text), ".")
	if len(parts) != 4 {
		return 0, false
	}
	var address uint32
	for index, part := range parts {
		octet, err := strconv.ParseUint(part, 10, 32)
		if err != nil || octet > 255 {
			return 0, false
		}
		address |= uint32(octet) << (8 * index)
	}
	return address, true
}

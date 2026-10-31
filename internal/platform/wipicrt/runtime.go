package wipicrt

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Runtime is the WIPI-C engine one program runs on: its memory identifiers,
// framebuffers and images, timers, files and databases, media clips, input
// method and event queue, and the handlers that answer the WIPI-C tables over
// them. KTF and SKT Clet programs both run on it; a platform's own runtime
// embeds it and answers the calls only it has through Hooks.
type Runtime struct {
	// Hooks are the platform's answers to what this engine does not own;
	// see platform.go.
	Hooks RuntimeHooks

	Client *Client
	Arena  *GuestArena
	// arenaShadow is the debug-build copy of the arena's free bytes, and
	// shadowWindow the buffer its check reads guest memory through; both nil in
	// release builds. See arena_shadow.go.
	arenaShadow    *arenaShadow
	shadowWindow   []byte
	ShadowedBlocks uint64
	CheckedBlocks  uint64
	// hookWindow is the scratch space the C-library routines copy through, kept
	// between calls. See clet_clib.go.
	hookWindow []byte

	// CodeCursor is the next free address in the stub arena, and stubs/stubKeys
	// map a (category,id) stub to its address and back.
	CodeCursor uint64
	Stubs      map[uint64]uint32
	StubKeys   map[uint32]uint64

	// CurrentThread and currentContext are the guest thread and Host context a
	// call is being serviced on, set while an entry or a call runs.
	CurrentThread  *armcore.Thread
	CurrentContext context.Context

	// VirtualBaseMillis and clockBase anchor the guest's timeline to the
	// session clock at the moment the runtime was built. See guestMillis.
	VirtualBaseMillis int64
	ClockBase         time.Time

	// Trace and CallCounts are the diagnostic ring and per-name counter a Host
	// reads to see which boundary a stuck program is crossing. See diag.go.
	Trace      TraceRing
	CallCounts map[DiagEvent]uint32

	SaveReadError error

	// The WIPI-C memory block bookkeeping: the arena size of each live
	// MC_knlAlloc block by its identifier, the image source buffers an image
	// record owns, and the addresses freeWIPIC last gave back.
	WIPICAllocations   map[uint32]uint64
	ImageSourceBuffers map[uint32]uint32
	ReleasedWIPIC      map[uint32]struct{}
	ReleasedWIPICOrder []uint32

	// ScreenFramebuffer is the handle MC_grpGetScreenFrameBuffer answers, and
	// framebufferOpacity the transparency of image framebuffers whose encoding
	// declared undrawn pixels. See wipic_graphics.go.
	ScreenFramebuffer  uint32
	FramebufferOpacity map[uint32]*ImageOpacity
	PixelOps           *PixelOpCache

	// PendingTimers is the live timer set a program arms its frames from, and
	// RoundTimers the part of a service round's batch not reached yet.
	PendingTimers []WIPICTimer
	RoundTimers   []WIPICTimer

	// The WIPI-C filesystem table's open files, and the storage bookkeeping the
	// file calls keep. See wipic_filesystem.go.
	CFiles              map[string]*RuntimeCFile
	CFileHandles        map[uint32]*RuntimeCFileHandle
	NextCDatabaseHandle uint32
	GuestFiles          map[string][]byte
	GuestFileWrites     map[string]uint64
	RemovedFiles        map[string]bool
	MadeDirectories     map[string]bool
	RemovedCDatabases   map[string]bool

	// WIPICClips holds each WIPI-C media clip by its guest address, in creation
	// order, and wipicMutedSources what MC_mdaSetMuteState was told per source.
	// See wipic_media.go.
	WIPICClips        map[uint32]*WIPICMediaClip
	WIPICClipOrder    []uint32
	WIPICMutedSources map[uint32]bool

	// UserMemoryInterface is the handset library table handed to a program
	// that asked for it by name, and UserMemoryPools the buffers it manages.
	// See wipic_usermem.go.
	UserMemoryInterface uint32
	UserMemoryPools     map[uint32]*UserMemoryPool

	// InputModeTableAddress is the `M_Char **` the input-method table answers
	// with, and CInput the Host's side of the input method. See im.go.
	InputModeTableAddress uint32
	CInput                CInputState

	// Events is the WIPI event queue a program drains through its own event
	// handler, and GuestEventLoop records that it does. See events.go.
	Events         []GuestEvent
	GuestEventLoop bool

	// Leds is the indicator-light mask a title last set; this handset has none.
	Leds int32

	// AOTCallDepth guards re-entrant guest-call nesting per thread; see
	// EnterAOTCall in bridge.go.
	AOTCallDepth map[*armcore.Thread]uint32
	// DetachedCFiles holds WIPI-C files whose names were open at a checkpoint
	// and that the store had nothing for when the load bound it. See
	// wipic_filesystem.go.
	DetachedCFiles map[string]*RuntimeCFile
	// PendingNetCallbacks are the MC_netConnect failures owed to callers. See
	// wipic_net.go.
	PendingNetCallbacks []WIPICNetCallback
	// RepaintPending, RepaintServicing and GuestFlushedOwnFrame track the frame
	// state the graphics handlers read: a repaint asked for, a repaint being
	// served, and a frame the guest flushed itself.
	RepaintPending       bool
	RepaintServicing     bool
	GuestFlushedOwnFrame bool

	// noteScratch is the buffer readCString hands Hooks.NoteRead.
	noteScratch []byte

	// CollectAt is the arena high-water mark a platform's collector triggers
	// at next; a platform with no collector leaves it at the floor.
	CollectAt uint64
}

// New builds the engine for a client: the platform data arena and the stub
// arena the platform's callbacks live in, with the guest's timeline anchored
// at the client's clock. It runs no guest code. Hooks.Supervisor is required.
func New(client *Client, hooks RuntimeHooks) (*Runtime, error) {
	if client == nil || client.core == nil {
		return nil, fmt.Errorf("%s client is not initialized", client.platformName())
	}
	if hooks.Supervisor == nil {
		return nil, fmt.Errorf("%s runtime has no supervisor call handler", client.platformName())
	}
	memory := client.core.Memory()
	if err := memory.Map(PlatformDataBase, PlatformDataSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map %s platform initialization data: %w", client.platformName(), err)
	}
	if err := memory.Map(PlatformCodeBase, PlatformCodeSize, armcore.PermissionReadExecute); err != nil {
		return nil, fmt.Errorf("map %s platform callback stubs: %w", client.platformName(), err)
	}
	runtime := &Runtime{
		Hooks:      hooks,
		Client:     client,
		Arena:      NewGuestArena(PlatformDataBase, PlatformDataSize),
		CollectAt:  CollectionFloor,
		CodeCursor: uint64(PlatformCodeBase),
		Stubs:      make(map[uint64]uint32),
		StubKeys:   make(map[uint32]uint64),
		// The guest's absolute time follows the clock the Host supplied, not
		// the wall: a Host running a manual clock is asking for a run it can
		// repeat, and an epoch read from the wall makes every timestamp the
		// guest sees differ between two runs of the same script.
		VirtualBaseMillis: client.Now().UnixMilli(),
		ClockBase:         client.Now(),
		Trace:             TraceRing{Limit: client.TraceLimit},
	}
	runtime.InstallArenaShadow()
	return runtime, nil
}

// DescribeGuestWord names what a word the guest handed the platform is, for
// failure messages: what the platform knows it to be, or the number it is.
func (runtime *Runtime) DescribeGuestWord(word uint32) string {
	if word == 0 {
		return "null"
	}
	if runtime.Hooks.DescribeGuestWord != nil {
		return runtime.Hooks.DescribeGuestWord(word)
	}
	return fmt.Sprintf("%#x", word)
}

// CountDiagnostic records a bounded per-name event count so acceptance probes
// can report which runtime boundary a looping client is crossing. When the
// Host enabled tracing it also retains the event in order, because a stuck
// game is diagnosed by the sequence of boundaries it crossed, not the totals.
func (runtime *Runtime) CountDiagnostic(name string) {
	runtime.RecordDiagnostic(DiagEvent{Text: name})
}

// RecordDiagnostic counts an event that has not been given a name yet. The busy
// boundaries take this path, because composing the name they would be counted
// under costs more than the crossing does; see diagEvent.
func (runtime *Runtime) RecordDiagnostic(event DiagEvent) {
	runtime.Trace.Record(event)
	if runtime.CallCounts == nil {
		runtime.CallCounts = make(map[DiagEvent]uint32)
	}
	if _, seen := runtime.CallCounts[event]; seen {
		runtime.CallCounts[event]++
		return
	}
	// A rare failure event is recorded for its call site and register dump —
	// collapsing those away would discard the only thing worth having — so it
	// keeps its full name right up to the ceiling.
	if len(runtime.CallCounts) < DiagnosticNameLimit ||
		(event.keepsDetail() && len(runtime.CallCounts) < DiagnosticNameCeiling) {
		runtime.CallCounts[event]++
		return
	}
	// Ordinary names carry the guest call site that produced them, so a busy
	// game exhausts the name budget on one method called from hundreds of
	// addresses. Collapsing the site keeps the event itself countable instead
	// of dropping it all into one opaque bucket.
	collapsed := event.collapse()
	if _, seen := runtime.CallCounts[collapsed]; !seen && len(runtime.CallCounts) >= DiagnosticNameCeiling {
		runtime.CallCounts[DiagEvent{Text: "diagnostic overflow"}]++
		return
	}
	runtime.CallCounts[collapsed]++
}

// diagnosticDetailPrefixes are the events whose value is in their detail: a
// guest exception, a stubbed or failing boundary, or the exit. They are rare,
// so keeping every distinct one cannot crowd out the ordinary traffic.
var diagnosticDetailPrefixes = []string{
	"throw ",
	"raise ",
	"kernel exit",
	"wipic stub ",
	"printk ",
	"save store error ",
	"equals ",
	"getBytes ",
	"aotcall ",
	"checktype ",
	"arena use after free ",
}

func DiagnosticKeepsDetail(name string) bool {
	if strings.Contains(name, "error") {
		return true
	}
	for _, prefix := range diagnosticDetailPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// CollapseDiagnosticName drops the " @0x<address>" call site diagnostics append
// to most event names, leaving the event itself.
func CollapseDiagnosticName(name string) string {
	if site := strings.LastIndex(name, " @0x"); site > 0 {
		return name[:site]
	}
	return name
}

// GuestMillis is the wall-clock time the guest sees. It starts at the Host's
// clock and then tracks the session clock, scaled by the speed multiplier, so
// the timeline the guest measures agrees with the waits it is actually
// granted: at 2x speed a 50ms sleep returns after 25ms of session time and the
// guest reads 50ms as having passed, and at 1x the two are the same thing.
//
// Tracking a clock rather than accumulating declared waits also means a guest
// that polls the time without sleeping still sees it move, which is what lets
// a busy-wait loop terminate.
func (runtime *Runtime) GuestMillis() int64 {
	if runtime == nil {
		return time.Now().UnixMilli()
	}
	// Every guest-visible read of the clock arrives here, which is what makes
	// this the place to count them: a service call that spends a step window
	// asking the time is waiting rather than computing, and the Host charges
	// the two differently. See continueHostService.
	runtime.Client.GuestClockReads++
	// A Host that moves its own clock cannot move it from inside the call the
	// guest is asking from, so the guest's own execution moves it instead. See
	// advanceBatchClockTo.
	runtime.Client.AdvanceBatchClockTo(runtime.Client.core.Steps())
	elapsed := runtime.Client.Now().Sub(runtime.ClockBase)
	if elapsed < 0 {
		elapsed = 0
	}
	scaled := float64(elapsed) * runtime.Client.SpeedOrDefault()
	return runtime.VirtualBaseMillis + int64(time.Duration(scaled)/time.Millisecond)
}

// RecordedMillis is guestMillis for a time the Host writes down for itself,
// such as when a database was last written, rather than one the guest asked
// for. It counts no read and moves no clock: guestMillis counts every call as
// the guest waiting on the time, which changes how the step budget charges the
// call it came from, so reading it for bookkeeping moved a title's timing
// (docs/history/ktf.md#ktf-storage-review-2026-10-22).
func (runtime *Runtime) RecordedMillis() int64 {
	if runtime == nil {
		return time.Now().UnixMilli()
	}
	return runtime.VirtualBaseMillis + int64(runtime.GuestElapsed()/time.Millisecond)
}

// GuestElapsed is how much guest time has passed since the runtime was built,
// which is the timeline audio playback runs on. guestMillis answers the same
// clock as a wall-clock date, which is what the guest's own time APIs want.
func (runtime *Runtime) GuestElapsed() time.Duration {
	if runtime == nil {
		return 0
	}
	elapsed := runtime.Client.Now().Sub(runtime.ClockBase)
	if elapsed < 0 {
		return 0
	}
	return time.Duration(float64(elapsed) * runtime.Client.SpeedOrDefault())
}

func (runtime *Runtime) HandleWIPICCall(thread *armcore.Thread, id uint32) (result uint32, err error) {
	defer func() {
		if err == nil && runtime.SaveReadError != nil {
			err = runtime.SaveReadError
		}
	}()
	runtime.RecordDiagnostic(DiagEvent{Kind: DiagWIPICCall, Nums: [5]uint32{id}})
	if id >= 1<<16 {
		return runtime.HandleWIPICTableCall(thread, id>>16, id&0xffff)
	}
	switch id {
	case wipicKernelReserved1:
		// The reserved slot answers the platform's WIPI-C interface table where
		// it has one, for its module path; a platform without one refuses it.
		if runtime.Hooks.ReservedKernelSlot != nil {
			return runtime.Hooks.ReservedKernelSlot(), nil
		}
		return WIPICErrorInvalid, nil
	case WIPICKernelAlloc, WIPICKernelCalloc:
		size, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		address, err := runtime.AllocateWIPIC(size)
		if err != nil {
			// A refused allocation is nearly always a size the game computed
			// wrongly rather than an arena that ran out, and the size alone
			// does not say where it came from. The stub returns with `bx lr`,
			// so naming the call site turns it into an address to disassemble.
			return 0, fmt.Errorf("%w%s", err, runtime.CallerSite(thread))
		}
		return address, nil
	case WIPICKernelFree:
		id, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		runtime.FreeWIPIC(id)
		// The specification declares MC_knlFree void, so r0 on return is not
		// part of its contract and no correct caller may read it. Some do
		// anyway: a title ends an initialization step with `MC_knlFree(buf)`
		// in tail position and its caller tests the result, so writing zero
		// into r0 turned "the font loaded" into "the font failed" and stopped
		// the game before its first frame. Leaving r0 as the caller set it is
		// what a routine that never touches its return register does, and it
		// costs nothing precisely because the value is undefined.
		return id, nil
	case WIPICKernelGetTotalMemory:
		return uint32(runtime.Arena.ReportedTotal()), nil
	case WIPICKernelGetFreeMemory:
		return uint32(runtime.Arena.ReportedFree()), nil
	case WIPICKernelPrintk:
		return runtime.wipicPrintk(thread)
	case WIPICKernelSprintk:
		return runtime.wipicSprintk(thread)
	case WIPICKernelGetCurProgramID:
		return runtime.programID(), nil
	case WIPICKernelGetAccessLevel:
		return wipicAccessLevel, nil
	case WIPICKernelGetExecNames:
		return runtime.wipicGetExecNames(thread)
	case WIPICKernelLoad:
		// No library is installed beside the running program — the listing
		// above names only the program itself — so there is nothing to load
		// under any name: M_E_INVALID, the answer for a name that does not
		// resolve.
		runtime.CountDiagnostic("load library")
		return WIPICErrorInvalid, nil
	case WIPICKernelGetProgramName:
		buffer, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		size, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		name := runtime.Client.ProgramName
		if name == "" {
			name = "wfeature"
		}
		if uint64(len(name))+1 > uint64(size) {
			return WIPICErrorShortBuf, nil
		}
		if err := runtime.Client.core.Memory().Write(buffer, append([]byte(name), 0)); err != nil {
			return 0, fmt.Errorf("write %s program name: %w", runtime.platformName(), err)
		}
		return 0, nil
	case WIPICKernelDefTimer:
		return runtime.wipicDefTimer(thread)
	case WIPICKernelSetTimer:
		return runtime.WIPICSetTimer(thread)
	case WIPICKernelUnsetTimer:
		return runtime.WIPICUnsetTimer(thread)
	case WIPICKernelCurrentTime:
		millis := uint64(runtime.GuestMillis())
		if err := thread.SetRegister(1, uint32(millis>>32)); err != nil {
			return 0, err
		}
		return uint32(millis), nil
	case WIPICKernelGetSysProperty:
		return runtime.WIPICGetSystemProperty(thread)
	case WIPICKernelSetSysProperty:
		return runtime.wipicSetSystemProperty(thread)
	case WIPICKernelGetResourceID:
		return runtime.wipicGetResourceID(thread)
	case WIPICKernelGetResource:
		return runtime.WIPICGetResource(thread)
	case WIPICKernelExit:
		// MC_knlExit ends the program; the Host observes the platform's
		// ExitError and tears the session down instead of treating it as a
		// failure. The
		// caller is named for the same reason a failure names one: an ending
		// nothing can locate reads as a broken game rather than a finished
		// one. The stub returns with `bx lr`, so LR is still the caller.
		runtime.CountDiagnostic("kernel exit")
		link, err := thread.Register(armcore.RegisterLR)
		if err != nil {
			return 0, runtime.Client.Platform.ExitError
		}
		return 0, fmt.Errorf("MC_knlExit from %#x: %w", link, runtime.Client.Platform.ExitError)
	case wipicKernelGetDLLInterface:
		return runtime.WIPICGetDLLInterface(thread)
	default:
		// The stub returns with `bx lr`, so LR still holds the caller's return
		// address. Naming it turns "some kernel slot is missing" into an address
		// that can be disassembled in the image.
		return 0, fmt.Errorf("%s SVC id %#x is not implemented%s", runtime.platformCName(), id, runtime.CallerSite(thread))
	}
}

// programID is the identifier MC_knlGetCurProgramID answers with. A handset
// runs many programs and hands each one a number; this platform runs exactly
// one, so the only property the answer needs is to be stable — a title asks
// for it once and hands it straight back to a call that names a program, and
// what it names has to still be this one. It is derived from the archive's own
// identifier rather than chosen here so that two different archives never
// answer the same number, and it is forced away from zero because zero is what
// a title reads as "no program".
func (runtime *Runtime) programID() uint32 {
	name := runtime.Client.ProgramName
	hash := uint32(2166136261)
	for _, symbol := range []byte(name) {
		hash ^= uint32(symbol)
		hash *= 16777619
	}
	if hash == 0 {
		hash = 1
	}
	return hash
}

func (runtime *Runtime) CallerSite(thread *armcore.Thread) string {
	lr, err := thread.Register(armcore.RegisterLR)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(" (called from %#x)", lr)
}

// CallerMark renders the same call site in the ` @0x` form the diagnostic
// counter understands, for events that are counted rather than raised.
// diagEvent.collapse strips exactly this suffix, so an event can carry the
// address that produced it without a caller in a loop spending the whole name
// budget on one boundary.
func (runtime *Runtime) CallerMark(thread *armcore.Thread) string {
	lr, err := thread.Register(armcore.RegisterLR)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(" @%#x", lr)
}

func (runtime *Runtime) HandleWIPICTableCall(thread *armcore.Thread, table, function uint32) (uint32, error) {
	switch {
	case table == wipicTableUserMemory:
		return runtime.HandleUserMemoryCall(thread, function)
	case table == WIPICTableUtil:
		return runtime.handleWIPICUtilCall(thread, function)
	case table == WIPICTableMisc && function == 0:
		// MC_miscBackLight controls the handset backlight; the browser host
		// keeps the screen lit.
		return 0, nil
	case table == WIPICTableGraphics && function == 0:
		// MC_grpGetImageProperty: 4 is width and 5 is height.
		handle, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		property, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		image, ok, err := runtime.ReadWIPICDrawSurface(handle, "get image property")
		if err != nil || !ok {
			// An image that is not there has no width and no height, which is
			// the same answer as a property this call does not know.
			return 0, err
		}
		runtime.CountDiagnostic(fmt.Sprintf("getImageProperty %d", property))
		switch property {
		case 4:
			return image.Width, nil
		case 5:
			return image.Height, nil
		default:
			return 0, nil
		}
	case table == WIPICTableGraphics && function == 1:
		// MC_grpGetImageFrameBuffer: an image record names its framebuffer in
		// its first word, so the call answers that handle. A caller that hands
		// the answer straight back to a drawing call reaches the same pixels
		// either way — every read of a surface follows the naming — but a
		// caller reading the framebuffer's own fields through the handset's
		// macros needs the framebuffer rather than the image.
		handle, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		if inner, isImage := runtime.wipicImageFramebuffer(handle); isImage {
			return inner, nil
		}
		return handle, nil
	case table == WIPICTableGraphics && function == 2:
		return runtime.WIPICGetScreenFramebuffer()
	case table == WIPICTableGraphics && function == 4:
		// MC_grpCreateOffScreenFrameBuffer.
		width, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		height, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		return runtime.NewWIPICFramebufferRecord(width, height)
	case table == WIPICTableGraphics && function == 3:
		return runtime.wipicDestroyOffScreenFrameBuffer(thread)
	case table == WIPICTableGraphics && function == 8:
		return runtime.wipicPutPixel(thread)
	case table == WIPICTableGraphics && function == 9:
		return runtime.WIPICDrawLine(thread)
	case table == WIPICTableGraphics && function == 10:
		return runtime.wipicDrawRect(thread)
	case table == WIPICTableGraphics && function == 11:
		return runtime.wipicFillRect(thread)
	case table == WIPICTableGraphics && function == 12:
		return runtime.WIPICCopyFramebuffer(thread)
	case table == WIPICTableGraphics && function == 13:
		return runtime.wipicDrawImage(thread)
	case table == WIPICTableGraphics && function == 14:
		return runtime.WIPICCopyArea(thread)
	case table == WIPICTableGraphics && function == 15:
		return runtime.wipicDrawArc(thread)
	case table == WIPICTableGraphics && function == 16:
		return runtime.wipicFillArc(thread)
	case table == WIPICTableGraphics && function == 17:
		return runtime.wipicDrawString(thread, false)
	case table == WIPICTableGraphics && function == 18:
		return runtime.wipicDrawString(thread, true)
	case table == WIPICTableGraphics && function == 19:
		return runtime.wipicTransferRGBPixels(thread, false)
	case table == WIPICTableGraphics && function == 20:
		return runtime.wipicTransferRGBPixels(thread, true)
	case table == WIPICTableGraphics && function == 21:
		return 0, runtime.flushLCD(thread)
	case table == WIPICTableGraphics && function == 5:
		return runtime.wipicInitGraphicsContext(thread)
	case table == WIPICTableGraphics && function == 6:
		return runtime.WIPICAccessGraphicsContext(thread, true)
	case table == WIPICTableGraphics && function == 7:
		return runtime.WIPICAccessGraphicsContext(thread, false)
	case table == WIPICTableGraphics && function == 22:
		return runtime.wipicPixelFromRGB(thread)
	case table == WIPICTableGraphics && function == 23:
		return runtime.wipicRGBFromPixel(thread)
	case table == WIPICTableGraphics && function == 24:
		return runtime.WIPICGetDisplayInfo(thread)
	case table == WIPICTableGraphics && function == 25:
		// MC_grpRepaint(lcd, x, y, w, h) queues a repaint rather than painting
		// here; the region is discarded because a card is repainted whole.
		runtime.RepaintPending = true
		if timer := runtime.Client.ActiveTimer; timer != nil && runtime.Hooks.RepaintCard != nil {
			timer.PaintedCard = runtime.Hooks.RepaintCard()
		}
		runtime.PostRepaintEvent()
		return 0, nil
	case table == WIPICTableGraphics && function == 26:
		// MC_grpGetFont returns an opaque font handle. The size it is asked
		// for is one of three identifiers rather than a pixel count —
		// MC_GRP_FT_SIZE_SMALL is 8, MEDIUM is 0 and LARGE is 16 — and the
		// handle carries it back so that two different requests stay two
		// different handles. What the handle *measures* is the question the
		// three metric slots below answer, and there is one face to measure.
		size, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		if int32(size) < 1 {
			size = uint32(runtime.FontHeight())
		}
		return size, nil
	case table == WIPICTableGraphics && function == 27:
		// MC_grpGetFontHeight, MC_grpGetFontAscent and MC_grpGetFontDescent
		// answer for the face the renderer actually draws with, whatever
		// handle they are handed. Echoing the handle instead is what made a
		// title's menu look sliced: it asked for the small font, was told
		// "8 pixels tall" because MC_GRP_FT_SIZE_SMALL happens to be 8, laid
		// each menu entry out as an eight-row band and clipped its text to it
		// — and the eleven-row face drew a Korean syllable whose top row fell
		// one row above that band and was cut off. Only a screen that clips
		// showed it, so the wide-spaced screens looked right and every menu
		// and dialogue box lost the top of its text.
		return uint32(runtime.FontHeight()), nil
	case table == WIPICTableGraphics && function == 28:
		return uint32(runtime.FontBaseline()), nil
	case table == WIPICTableGraphics && function == 29:
		return uint32(runtime.FontHeight() - runtime.FontBaseline()), nil
	case table == WIPICTableGraphics && function == 30:
		// MC_grpGetStringWidth measures the EUC-KR string with the real glyph
		// advances, matching what MC_grpDrawString will draw.
		pointer, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		length, err := thread.Register(2)
		if err != nil {
			return 0, err
		}
		text, err := runtime.WIPICReadString(pointer, int32(length), false)
		if err != nil {
			return 0, err
		}
		return uint32(runtime.GraphicsTextWidth(text)), nil
	case table == WIPICTableGraphics && function == 31:
		// MC_grpGetUnicodeStringWidth(font, str, len) measures the UTF-16
		// text entry 18 draws, the entry after its EUC-KR twin in the
		// specification's order. No local title calls it.
		pointer, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		length, err := thread.Register(2)
		if err != nil {
			return 0, err
		}
		text, err := runtime.WIPICReadString(pointer, int32(length), true)
		if err != nil {
			return 0, err
		}
		return uint32(runtime.GraphicsTextWidth(text)), nil
	case table == WIPICTableGraphics && function == 32:
		return runtime.WIPICCreateImage(thread)
	case table == WIPICTableGraphics && function == 33:
		return runtime.wipicDestroyImage(thread)
	case table == WIPICTableGraphics && function == 34:
		return runtime.wipicDecodeNextImage(thread)
	case table == WIPICTableGraphics && function == 35:
		return runtime.wipicEncodeImage(thread)
	case table == WIPICTableGraphics && function == 36:
		return runtime.wipicPostEvent(thread)
	case table == WIPICTableGraphics && function == 37:
		return runtime.wipicPolygon(thread, false)
	case table == WIPICTableGraphics && function == 38:
		return runtime.wipicPolygon(thread, true)
	case table == wipicTableRecordDatabase && runtime.Hooks.RecordDatabase != nil:
		return runtime.Hooks.RecordDatabase(thread, function)
	case table == WIPICTableFilesystem:
		return runtime.HandleWIPICFileCall(thread, function)
	case table == WIPICTableMedia:
		return runtime.HandleWIPICMediaCall(thread, function)
	case table == WIPICTableNet:
		return runtime.handleWIPICNetCall(thread, function)
	case table == wipicTableUIC:
		// The UI component table draws platform widgets — text fields, menus,
		// soft keys — over the game's screen. Nothing in the local archives
		// calls it, and a widget answered with a handle but never drawn would
		// leave a game waiting for input from something invisible, so a
		// creation call fails and the rest are accepted no-ops.
		runtime.CountDiagnostic(fmt.Sprintf("wipic uic function %d", function))
		if function == 0 {
			return WipiErrorCode, nil
		}
		return 0, nil
	case table == WIPICTableInputMethod:
		return runtime.HandleWIPICInputMethodCall(thread, function)
	case table >= 4 && table != WIPICTableFilesystem:
		// Tables 5-6, 8, and 12-17 are stubbed in the original runtime; calls
		// are accepted, counted, and answer zero.
		//
		// The count carries the guest address it was called from, for the same
		// reason the unimplemented-table error does: a table number never
		// appears in the guest's own code, so a stub line naming only a number
		// cannot be investigated at all, while one naming an address can be
		// disassembled. A stub answers rather than fails, so this is the only
		// place the address is ever recorded.
		runtime.CountDiagnostic(fmt.Sprintf("wipic stub table %d function %d%s",
			table, function, runtime.CallerMark(thread)))
		return 0, nil
	}
	// The stub returns with `bx lr`, so LR still holds the guest's return
	// address. Naming it turns "some table is missing" into an address to
	// disassemble, which is the only way to find out what an unnumbered table
	// is for: the interface is an array of function pointers and the game
	// indexes it, so the table number never appears in the guest's own code.
	return 0, fmt.Errorf("%s table %d function %d is not implemented%s", runtime.platformCName(),
		table, function, runtime.CallerSite(thread))
}

func (runtime *Runtime) wipicInitGraphicsContext(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	if err := runtime.Client.core.Memory().Write(address, make([]byte, WIPICGraphicsContextSize)); err != nil {
		return 0, fmt.Errorf("initialize %s graphics context at %#x: %w", runtime.platformName(), address, err)
	}
	return 0, nil
}

// WIPICAccessGraphicsContext implements MC_grpSetContext and MC_grpGetContext,
// which copy one field between the context record and the caller's value
// pointer or immediate.
func (runtime *Runtime) WIPICAccessGraphicsContext(thread *armcore.Thread, set bool) (uint32, error) {
	contextAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	operation, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	value, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	memory := runtime.Client.core.Memory()
	// Single-word fields set from the immediate value and get through a
	// pointer.
	transfer := func(fieldOffset, size uint32, indirect bool) error {
		if set {
			data := make([]byte, size)
			if indirect {
				if err := memory.Read(value, data); err != nil {
					return err
				}
			} else {
				binary.LittleEndian.PutUint32(data, value)
			}
			return memory.Write(contextAddress+fieldOffset, data)
		}
		data := make([]byte, size)
		if err := memory.Read(contextAddress+fieldOffset, data); err != nil {
			return err
		}
		return memory.Write(value, data)
	}
	// The clip and the offset are arrays of `M_Int32` in the caller's memory
	// and pairs of 16-bit fields in the record, so they are widened and
	// narrowed rather than copied. Copying them verbatim reads a four-int clip
	// as two of them — which is what this did, and it left `MC_grpDrawImage`
	// with a rectangle whose bottom-right corner was whatever the record had
	// been zeroed to.
	transferInts := func(fieldOffset uint32, count int) error {
		data := make([]byte, count*2)
		if set {
			// **A null array clears the field**, and one title's own code is
			// what says so. Its clip setter computes the rectangle it wants,
			// and when that rectangle turns out to be the whole destination
			// surface it calls this with a null pointer instead of the array
			// — the same call, with the array argument zeroed. A cleared clip
			// is exactly how a context that has never been given one reads
			// here (see wipicClip): an empty rectangle means "no clip" rather
			// than "draw nothing", because MC_grpInitContext zeroes the record
			// and no title that never sets a clip could draw otherwise. So
			// zeroing the field is both what the caller asked for and what
			// this platform already answers to. The same reading serves the
			// origin offset, whose zero is the origin.
			if value == 0 {
				return memory.Write(contextAddress+fieldOffset, data)
			}
			words := make([]byte, count*4)
			if err := memory.Read(value, words); err != nil {
				return err
			}
			for index := 0; index < count; index++ {
				word := int32(binary.LittleEndian.Uint32(words[index*4:]))
				binary.LittleEndian.PutUint16(data[index*2:], uint16(int16(word)))
			}
			return memory.Write(contextAddress+fieldOffset, data)
		}
		if err := memory.Read(contextAddress+fieldOffset, data); err != nil {
			return err
		}
		words := make([]byte, count*4)
		for index := 0; index < count; index++ {
			field := int16(binary.LittleEndian.Uint16(data[index*2:]))
			binary.LittleEndian.PutUint32(words[index*4:], uint32(int32(field)))
		}
		return memory.Write(value, words)
	}
	var accessErr error
	switch operation {
	case 0:
		accessErr = transferInts(4, 4) // clip bounds
		if set && accessErr == nil {
			// The SDK checks this word before reading the clip through
			// GetContext. Leaving it zero makes nested drawing restore the
			// full surface instead of the caller's clipping rectangle.
			var enabled uint32
			if value != 0 {
				enabled = 1
			}
			accessErr = runtime.WriteWord(contextAddress, enabled)
		}
	case 1:
		accessErr = transfer(12, 4, false) // foreground pixel
	case 2:
		accessErr = transfer(16, 4, false) // background pixel
	case 3:
		accessErr = transfer(20, 4, false) // transparent pixel
	case 4:
		accessErr = transfer(24, 4, false) // alpha
	case 5:
		accessErr = transfer(WIPICContextPixelOpOffset, 4, false) // pixel-op function
	case 6:
		accessErr = transfer(wipicContextPixelParamOffset, 4, false) // pixel-op parameter
	case 7:
		accessErr = transfer(wipicContextFontOffset, 4, false) // font
	case 8:
		accessErr = transfer(wipicContextStyleOffset, 4, false) // style
	case 10:
		accessErr = transferInts(28, 2) // offset pair
	default:
		return 0, nil
	}
	if accessErr != nil {
		return 0, fmt.Errorf("access %s graphics context field %d at %#x with value %#x%s: %w", runtime.platformName(),
			operation, contextAddress, value, runtime.CallerSite(thread), accessErr)
	}
	return 0, nil
}

// wipicPixelFromRGB converts 8-bit color components to the RGB565 pixel the
// 16-bit screen mode reports.
func (runtime *Runtime) wipicPixelFromRGB(thread *armcore.Thread) (uint32, error) {
	red, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	green, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	blue, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	return (red&0xff)>>3<<11 | (green&0xff)>>2<<5 | (blue&0xff)>>3, nil
}

func (runtime *Runtime) wipicRGBFromPixel(thread *armcore.Thread) (uint32, error) {
	pixel, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	memory := runtime.Client.core.Memory()
	components := []uint32{
		(pixel >> 11 & 0x1f) << 3,
		(pixel >> 5 & 0x3f) << 2,
		(pixel & 0x1f) << 3,
	}
	for index, component := range components {
		target, registerErr := thread.Register(index + 1)
		if registerErr != nil {
			return 0, registerErr
		}
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], component)
		if err := memory.Write(target, word[:]); err != nil {
			return 0, fmt.Errorf("write %s pixel component %d: %w", runtime.platformName(), index, err)
		}
	}
	return pixel, nil
}

// WIPICGetScreenFramebuffer lazily builds the single MC_GrpFrameBuffer record
// {width, height, bpl, bpp, buf} whose pixel buffer guest code writes
// directly. Host presentation reads the same buffer when the flush path is
// connected.
func (runtime *Runtime) WIPICGetScreenFramebuffer() (uint32, error) {
	if runtime.ScreenFramebuffer != 0 {
		return runtime.ScreenFramebuffer, nil
	}
	width, height := runtime.ScreenSize()
	record, err := runtime.NewWIPICFramebufferRecord(uint32(width), uint32(height))
	if err != nil {
		return 0, fmt.Errorf("create %s screen framebuffer: %w", runtime.platformName(), err)
	}
	runtime.ScreenFramebuffer = record
	return record, nil
}

// ScreenSize is the handset this game was told it runs on. It is the one
// answer the screen framebuffer and MC_grpGetDisplayInfo both read, because a
// game that is given a width by one and a stride by the other writes its rows
// at the wrong offsets; see Client.SetScreen.
func (runtime *Runtime) ScreenSize() (int, int) {
	if runtime == nil {
		return RuntimeDisplayPixelWidth, RuntimeDisplayPixelHeight
	}
	return runtime.Client.ScreenSize()
}

// WIPICGetDisplayInfo answers MC_grpGetDisplayInfo with the 16-bit 565 screen
// this game runs on — 240x320, the one the original runtime reports, unless
// the Host named another handset. The struct stores the red, blue, and green
// masks in that order.
func (runtime *Runtime) WIPICGetDisplayInfo(thread *armcore.Thread) (uint32, error) {
	outAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	width, height := runtime.ScreenSize()
	info := []uint32{
		16,                // bpp
		16,                // depth
		uint32(width),     // width
		uint32(height),    // height
		2 * uint32(width), // bytes per line
		1,                 // direct color
		0xf800,            // red mask
		0x001f,            // blue mask
		0x07e0,            // green mask
	}
	data := make([]byte, len(info)*4)
	for index, word := range info {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	if err := runtime.Client.core.Memory().Write(outAddress, data); err != nil {
		return 0, fmt.Errorf("write %s display info at %#x: %w", runtime.platformName(), outAddress, err)
	}
	return 1, nil
}

func (runtime *Runtime) wipicDefTimer(thread *armcore.Thread) (uint32, error) {
	pointer, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	callback, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], callback)
	if err := runtime.Client.core.Memory().Write(pointer, word[:]); err != nil {
		return 0, fmt.Errorf("write %s timer record at %#x: %w", runtime.platformName(), pointer, err)
	}
	return 0, nil
}

func (runtime *Runtime) WIPICSetTimer(thread *armcore.Thread) (uint32, error) {
	pointer, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	low, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	high, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	param, err := thread.Register(3)
	if err != nil {
		return 0, err
	}
	callback, err := runtime.ReadAOTWords(pointer, 1, "timer record")
	if err != nil {
		return 0, err
	}
	delay := uint64(high)<<32 | uint64(low)
	// A guest that asks for an absurd delay is asking for a timer that never
	// fires in a session; clamping keeps the deadline arithmetic in range
	// without changing any delay a game actually uses.
	wait := time.Duration(min(delay, MaxTimerDelayMillis)) * time.Millisecond
	armed := WIPICTimer{
		Pointer:  pointer,
		Callback: callback[0],
		Param:    param,
		Delay:    delay,
		Due:      runtime.Client.FramePeriodDeadline(wait),
	}
	// One timer record is one timer. MC_knlUnsetTimer cancels by record
	// address and cannot mean anything else, so arming a record that is
	// already queued re-arms that timer rather than adding a second one with
	// the same identity. A title whose frame loop re-arms the same record
	// every tick without ever unsetting it otherwise grew the queue by one
	// entry a tick until the ceiling below refused the call and the run died
	// with 256 queued entries that were all identical.
	//
	// Only a queued WIPI-C entry is replaced. A java/util/Timer entry carries
	// a task instead of a callback and is cancelled through its own Timer, so
	// it is never the record this call names. An entry that has already been
	// handed to a service round is not in the queue at all -- ServiceTimers
	// takes the whole slice and re-queues what it did not run -- so a callback
	// that re-arms itself while it is running still queues one entry, which is
	// the timer it just asked for. A record later in that round's batch is
	// the same timer as well: its stale entry is cancelled there, and the
	// re-arm is queued like any other.
	runtime.CancelRoundTimers(func(timer WIPICTimer) bool { return timer.Task == nil && timer.Pointer == pointer })
	for index, timer := range runtime.PendingTimers {
		if timer.Task == nil && timer.Pointer == pointer {
			runtime.PendingTimers[index] = armed
			return 0, nil
		}
	}
	if len(runtime.PendingTimers) >= MaxPendingTimers {
		return 0, fmt.Errorf("%s pending timer count exceeds %d", runtime.platformName(), MaxPendingTimers)
	}
	runtime.PendingTimers = append(runtime.PendingTimers, armed)
	return 0, nil
}

func (runtime *Runtime) WIPICUnsetTimer(thread *armcore.Thread) (uint32, error) {
	pointer, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	remaining := runtime.PendingTimers[:0]
	for _, timer := range runtime.PendingTimers {
		if timer.Pointer != pointer {
			remaining = append(remaining, timer)
		}
	}
	runtime.PendingTimers = remaining
	runtime.CancelRoundTimers(func(timer WIPICTimer) bool { return timer.Pointer == pointer })
	return 0, nil
}

// CancelRoundTimers zeroes the entries a running timer round has still to
// reach that a predicate selects, and reports how many it cancelled. The
// round skips a zeroed entry and does not queue it again.
func (runtime *Runtime) CancelRoundTimers(match func(WIPICTimer) bool) int {
	cancelled := 0
	for index, timer := range runtime.RoundTimers {
		if (timer.Callback != 0 || timer.Task != nil) && match(timer) {
			runtime.RoundTimers[index] = WIPICTimer{}
			cancelled++
		}
	}
	return cancelled
}

// requeueTimers puts the entries a round did not reach back on the queue,
// leaving out the ones a callback cancelled meanwhile.
func (runtime *Runtime) requeueTimers(rest []WIPICTimer) {
	for _, timer := range rest {
		if timer.Callback != 0 || timer.Task != nil {
			runtime.PendingTimers = append(runtime.PendingTimers, timer)
		}
	}
}

// AllocateWIPIC lays out the original MC_knlAlloc indirect record inside the
// bounded platform arena and records the block so MC_knlFree can give it back.
//
// The payload is cleared rather than assumed clear. The arena used to be
// bump-only, so a block was a piece of a freshly mapped region and calloc's
// zero fill came for free; now that released blocks are handed out again, the
// zero fill the specification promises has to be written.
func (runtime *Runtime) AllocateWIPIC(size uint32) (uint32, error) {
	if size == 0 {
		return 0, nil
	}
	total := uint64(size) + uint64(WIPICAllocationOverhead)
	address, err := runtime.Allocate(total)
	if err != nil {
		return 0, err
	}
	if err := runtime.ClearGuestRange(address, total); err != nil {
		return 0, fmt.Errorf("clear %s allocation at %#x: %w", runtime.platformCName(), address, err)
	}
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:], address+runtime.wipicHandleFirstWord())
	binary.LittleEndian.PutUint32(header[4:], size)
	if err := runtime.Client.core.Memory().Write(address, header); err != nil {
		return 0, fmt.Errorf("write %s allocation header at %#x: %w", runtime.platformCName(), address, err)
	}
	if runtime.WIPICAllocations == nil {
		runtime.WIPICAllocations = make(map[uint32]uint64)
	}
	runtime.WIPICAllocations[address] = total
	// The span is live again, so it is no longer a release anyone can be
	// reading through.
	delete(runtime.ReleasedWIPIC, address)
	return address, nil
}

// wipicHandleData answers where a memory identifier's data starts, given the
// word the identifier points at. The identifier is the specification's
// indirect handle and its data pointer is MC_GETDPTR, whose expansion is the
// handset's, so the answer follows the platform running, and an identifier
// the guest built itself is read the way its own macros built it. See
// Platform.HandleData.
func (runtime *Runtime) wipicHandleData(first uint32) uint32 {
	return first + runtime.Client.Platform.HandleData
}

// wipicHandleFirstWord is what allocateWIPIC stores in an identifier's first
// word, as an offset from the identifier. Either way the data starts at the
// same place, wipicAllocationOverhead bytes in, so everything this runtime
// writes there itself is the same on every platform. See
// Platform.HandleFirstWord.
func (runtime *Runtime) wipicHandleFirstWord() uint32 {
	return runtime.Client.Platform.HandleFirstWord
}

// ClearGuestRange writes zeros over one guest range in bounded chunks, so the
// fill costs a fixed buffer rather than one the size of the allocation.
func (runtime *Runtime) ClearGuestRange(address uint32, size uint64) error {
	memory := runtime.Client.core.Memory()
	for offset := uint64(0); offset < size; {
		span := min(size-offset, uint64(len(wipicZeroFill)))
		if err := memory.Write(address+uint32(offset), wipicZeroFill[:span]); err != nil {
			return err
		}
		offset += span
	}
	return nil
}

// FreeWIPIC implements MC_knlFree: the block a memory identifier names goes
// back to the arena to be handed out again. Without this a title that
// allocates and frees a working buffer every frame — which is what the drawing
// loop of at least one local title does, sixteen times a tick — walks the
// arena's whole 64 MiB and stops the run at a screen it was only sitting on.
//
// Freeing something this platform never handed out is counted and ignored
// rather than fatal. The specification makes it the program's own error, a
// double free reaches here as an identifier that has already been dropped, and
// a handset would not end the program over either.
func (runtime *Runtime) FreeWIPIC(id uint32) {
	if id == 0 {
		return
	}
	size, ok := runtime.WIPICAllocations[id]
	if !ok {
		runtime.CountDiagnostic("wipic free of an unknown allocation")
		return
	}
	delete(runtime.WIPICAllocations, id)
	for image, source := range runtime.ImageSourceBuffers {
		if image == id || source == id {
			delete(runtime.ImageSourceBuffers, image)
		}
	}
	if runtime.Hooks.Freed != nil {
		runtime.Hooks.Freed(id)
	}
	// Transparency is recorded against the handle of the framebuffer or image
	// that lives in the block, so a released address must not carry its mask
	// into whatever is allocated there next.
	runtime.SetFramebufferOpacity(id, nil)
	runtime.noteReleasedWIPIC(id)
	runtime.Arena.Release(id, size)
}

func (runtime *Runtime) noteReleasedWIPIC(id uint32) {
	if runtime.ReleasedWIPIC == nil {
		runtime.ReleasedWIPIC = make(map[uint32]struct{}, MaxReleasedWIPIC)
	}
	if _, already := runtime.ReleasedWIPIC[id]; already {
		return
	}
	if len(runtime.ReleasedWIPICOrder) >= MaxReleasedWIPIC {
		delete(runtime.ReleasedWIPIC, runtime.ReleasedWIPICOrder[0])
		runtime.ReleasedWIPICOrder = runtime.ReleasedWIPICOrder[1:]
	}
	runtime.ReleasedWIPIC[id] = struct{}{}
	runtime.ReleasedWIPICOrder = append(runtime.ReleasedWIPICOrder, id)
}

// WasReleasedWIPIC reports that the guest freed this address and nothing has
// been allocated at it since.
func (runtime *Runtime) WasReleasedWIPIC(id uint32) bool {
	_, released := runtime.ReleasedWIPIC[id]
	return released
}

// wipicResource resolves a guest resource name against the attached archive
// entries. Names stay raw bytes, so the original encoding matches the archive
// entry names without transcoding.
func (runtime *Runtime) wipicResource(name string) ([]byte, bool) {
	if data, ok := runtime.Client.Resources[name]; ok {
		return data, true
	}
	trimmed := strings.TrimPrefix(name, "/")
	data, ok := runtime.Client.Resources[trimmed]
	return data, ok
}

// wipicSetSystemProperty refuses every property with M_E_INVALID, the
// specification's answer for information that cannot be set. The handset's
// identity is not the title's to change — the Java HandsetProperty setter
// answers false for the same reason — and a value accepted here would have
// to be remembered and answered back by the getter for the rest of the run.
func (runtime *Runtime) wipicSetSystemProperty(thread *armcore.Thread) (uint32, error) {
	// The value is never read: nothing keeps it, so a value too long for a
	// diagnostic is no reason to fail a call that refuses it anyway.
	idAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.ReadCString(idAddress, 128)
	if err != nil {
		return 0, fmt.Errorf("read %s system property id: %w", runtime.platformName(), err)
	}
	runtime.CountDiagnostic("sysprop set " + name)
	return WIPICErrorInvalid, nil
}

func (runtime *Runtime) WIPICGetSystemProperty(thread *armcore.Thread) (uint32, error) {
	idAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	outAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	bufferSize, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	name, err := runtime.ReadCString(idAddress, 128)
	if err != nil {
		return 0, fmt.Errorf("read %s system property id: %w", runtime.platformName(), err)
	}
	runtime.CountDiagnostic("sysprop " + name)
	value, ok := runtime.Client.SystemProperty(name)
	if !ok {
		return WIPICErrorInvalid, nil
	}
	if uint64(len(value))+1 > uint64(bufferSize) {
		return WIPICErrorShortBuf, nil
	}
	if err := runtime.Client.core.Memory().Write(outAddress, append([]byte(value), 0)); err != nil {
		return 0, fmt.Errorf("write %s system property %q: %w", runtime.platformName(), name, err)
	}
	return 0, nil
}

func (runtime *Runtime) wipicGetResourceID(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	sizeAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, err := runtime.ReadCString(nameAddress, 512)
	if err != nil {
		return 0, fmt.Errorf("read %s resource name: %w", runtime.platformName(), err)
	}
	runtime.CountDiagnostic("resource " + name)
	data, ok := runtime.wipicResource(name)
	writeSize := func(size uint32) error {
		if sizeAddress == 0 {
			return nil
		}
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], size)
		return runtime.Client.core.Memory().Write(sizeAddress, word[:])
	}
	if !ok {
		if err := writeSize(0); err != nil {
			return 0, fmt.Errorf("write %s resource size: %w", runtime.platformName(), err)
		}
		return WIPICErrorNotFound, nil
	}
	handle, err := runtime.AllocateBytes(append([]byte(name), 0))
	if err != nil {
		return 0, err
	}
	if err := writeSize(uint32(len(data))); err != nil {
		return 0, fmt.Errorf("write %s resource size: %w", runtime.platformName(), err)
	}
	return handle, nil
}

func (runtime *Runtime) WIPICGetResource(thread *armcore.Thread) (uint32, error) {
	handle, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	bufferSize, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	// A game that does not check MC_knlGetResourceID hands its error code
	// straight back as the handle, and a negative handle read as an address
	// faults the guest rather than failing the call. The parentheses matter:
	// `&` and `<<` share a precedence level in Go and associate left, so
	// `handle&1<<31` is `(handle&1)<<31` — zero for every even handle, which
	// is every error code there is.
	if handle&(1<<31) != 0 {
		return WIPICErrorGeneric, nil
	}
	name, err := runtime.ReadCString(handle, 512)
	if err != nil {
		return 0, fmt.Errorf("read %s resource handle: %w", runtime.platformName(), err)
	}
	data, ok := runtime.wipicResource(name)
	if !ok {
		return WIPICErrorNotFound, nil
	}
	if uint64(len(data)) > uint64(bufferSize) {
		return WIPICErrorGeneric, nil
	}
	base, err := runtime.ReadAOTWords(buffer, 1, "WIPI C resource buffer handle")
	if err != nil {
		return 0, err
	}
	if err := runtime.Client.core.Memory().Write(runtime.wipicHandleData(base[0]), data); err != nil {
		return 0, fmt.Errorf("write %s resource %q: %w", runtime.platformName(), name, err)
	}
	return 0, nil
}

func (runtime *Runtime) Stub(category, id uint32) (uint32, error) {
	if category > 0xff {
		return 0, fmt.Errorf("%s SVC category %d exceeds Thumb immediate range", runtime.platformName(), category)
	}
	key := uint64(category)<<32 | uint64(id)
	if address := runtime.Stubs[key]; address != 0 {
		return address, nil
	}
	const stubSize = uint64(SVCStubSize)
	if runtime.CodeCursor+stubSize > uint64(PlatformCodeBase)+PlatformCodeSize {
		return 0, fmt.Errorf("%s platform callback stub space exhausted", runtime.platformName())
	}
	address := uint32(runtime.CodeCursor)
	if err := runtime.Client.core.Memory().Load(address, SVCStub(category, id)); err != nil {
		return 0, fmt.Errorf("load %s platform callback stub: %w", runtime.platformName(), err)
	}
	runtime.CodeCursor += stubSize
	runtime.Stubs[key] = address | 1
	runtime.StubKeys[address|1] = key
	return address | 1, nil
}

func (runtime *Runtime) Allocate(size uint64) (uint32, error) {
	if size > MaxPlatformAllocation {
		return 0, fmt.Errorf("%s platform allocation %d exceeds %d bytes", runtime.platformName(), size, MaxPlatformAllocation)
	}
	address, ok := runtime.Arena.Allocate(size)
	if !ok {
		// The arena being full is the collector's second trigger: what would
		// have made room may already be unreachable and merely waiting for the
		// growth trigger to come round. See collectForAllocation.
		if runtime.collectForAllocation() {
			address, ok = runtime.Arena.Allocate(size)
		}
	}
	if !ok {
		return 0, fmt.Errorf("%s platform initialization data space exhausted", runtime.platformName())
	}
	return address, nil
}

func (runtime *Runtime) AllocateWords(words []uint32) (uint32, error) {
	data := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	return runtime.AllocateBytes(data)
}

func (runtime *Runtime) AllocateBytes(data []byte) (uint32, error) {
	address, err := runtime.Allocate(uint64(len(data)))
	if err != nil {
		return 0, err
	}
	if err := runtime.Client.core.Memory().Write(address, data); err != nil {
		return 0, fmt.Errorf("write %s platform data at %#x: %w", runtime.platformName(), address, err)
	}
	return address, nil
}

func (runtime *Runtime) ReadCString(address uint32, limit uint32) (string, error) {
	memory := runtime.Client.core.Memory()
	// The walk reads a name onto the stack, so the string made of it is the
	// only allocation a name shorter than the buffer costs.
	var buffer [GuestStringBuffer]byte
	length, terminated, err := ScanGuestString(memory, address, limit, 1, &buffer)
	if err != nil {
		return "", err
	}
	if !terminated {
		return "", fmt.Errorf("string at %#x exceeds %d bytes", address, limit)
	}
	var data []byte
	if length < GuestStringBuffer {
		data = buffer[:length]
	} else {
		data = make([]byte, length)
		if err := memory.Read(address, data); err != nil {
			return "", err
		}
	}
	if runtime.Hooks.NoteRead != nil {
		// The hook is handed a copy, so the bytes read stay on the stack: a
		// string passed straight to a call the compiler cannot see would cost
		// every reader an allocation, hook or none.
		runtime.noteScratch = append(runtime.noteScratch[:0], data...)
		runtime.Hooks.NoteRead(address, runtime.noteScratch, true)
	}
	return string(data), nil
}

// ReadBoundedString reads at most limit bytes, stopping early at a terminator.
// Running to the bound is the expected end rather than an error: the caller
// asked for a fixed count of bytes, not for a C string.
func (runtime *Runtime) ReadBoundedString(address, limit uint32) ([]byte, error) {
	data, _, err := ReadGuestString(runtime.Client.core.Memory(), address, limit, 1)
	return data, err
}

// ScanGuestString is the walk every KTF reader of a terminated guest string
// makes. It finds where the string at address ends — at its first zero unit,
// or after limit bytes, whichever comes first — and answers how many bytes
// come before that end and whether the end was a terminator. A unit is a byte,
// or two for UTF-16 text, whose terminator is a zero halfword; limit is
// counted in bytes, a whole number of units. A string shorter than buffer is
// in buffer on return; for any other, buffer holds whatever the walk read into
// it last.
//
// It reads a chunk at a time. A byte at a time, every byte paid a whole memory
// read — its lock and its validation — and registering a class reads every
// name the class has: on a light KTF title that was a third of the server's
// CPU, and the strings drawn every frame paid it again. The answers are the
// unit-at-a-time ones: a chunk that cannot be read whole, because it runs past
// the end of a mapping the string ends inside, is read again a unit at a time,
// so the string reads as before and an unreadable unit before the terminator
// fails with the error that unit's own read gives.
//
// A string that reaches the top of the address space without ending is
// refused rather than carried on at address zero. Neither KTF client maps the
// first page of the address space or the last, so no string a title built can
// run off the top, and a pointer that wraps is better refused than followed
// into memory that was never a string.
func ScanGuestString(memory *armcore.Memory, address, limit, unit uint32, buffer *[GuestStringBuffer]byte) (uint32, bool, error) {
	// A unit is one byte or two, so a whole number of them is a mask away.
	whole := ^(uint64(unit) - 1)
	walk := uint64(limit) & whole
	size := uint64(guestStringChunk)
	for offset := uint64(0); offset < walk; {
		current := uint64(address) + offset
		if current+uint64(unit) > uint64(1)<<32 {
			return 0, false, ErrGuestStringOverflow
		}
		// The string is read into place while it fits in buffer, so it is
		// there if it ends before buffer does. Past that the caller reads it
		// again in one piece, and buffer is only where each chunk lands.
		start := uint64(0)
		if offset < GuestStringBuffer {
			start = offset
		}
		span := min(size, GuestStringBuffer-start, walk-offset, uint64(1)<<32-current) & whole
		part := buffer[start : start+span]
		end := -1
		if err := memory.Read(uint32(current), part); err != nil {
			// The chunk runs past the end of a mapping, which the string
			// may end before.
			for index := 0; index < len(part); index += int(unit) {
				piece := part[index : index+int(unit)]
				if err := memory.Read(uint32(current)+uint32(index), piece); err != nil {
					return 0, false, err
				}
				if piece[0] == 0 && (unit == 1 || piece[1] == 0) {
					end = index
					break
				}
			}
		} else if unit == 1 {
			end = bytes.IndexByte(part, 0)
		} else {
			end = guestStringWideEnd(part)
		}
		if end >= 0 {
			return uint32(offset) + uint32(end), true, nil
		}
		offset += span
		size = GuestStringBuffer
	}
	return uint32(walk), false, nil
}

// guestStringWideEnd answers where the first zero UTF-16 unit in part starts,
// or -1 when part holds none.
func guestStringWideEnd(part []byte) int {
	for index := 0; index+1 < len(part); index += 2 {
		if part[index] == 0 && part[index+1] == 0 {
			return index
		}
	}
	return -1
}

// ReadGuestString reads the guest string at address as scanGuestString walks
// it, into a slice of its own exactly as long as the string, and reports
// whether it met the terminator. The slice has room for one byte more,
// because a string read to be copied back into the guest goes back with its
// terminator after it.
func ReadGuestString(memory *armcore.Memory, address, limit, unit uint32) ([]byte, bool, error) {
	var buffer [GuestStringBuffer]byte
	length, terminated, err := ScanGuestString(memory, address, limit, unit, &buffer)
	if err != nil {
		return nil, false, err
	}
	data := make([]byte, length, int(length)+1)
	if length < GuestStringBuffer {
		copy(data, buffer[:length])
	} else if err := memory.Read(address, data); err != nil {
		return nil, false, err
	}
	return data, terminated, nil
}

// wipicGetExecNames answers MC_knlGetExecNames, which lists the executables a
// program installed on this handset carries:
//
//	M_Int32 MC_knlGetExecNames(char *program, M_Int32, M_Int32,
//	                           char *out, M_Int32 outLength)
//
// A handset holds one program here — the archive that is running — so the
// listing names it, and asking about anything else answers that there is
// nothing installed under that name.
//
// **The listing's layout is reconstructed from its one caller**, because
// nothing describes it: neither the specification nor the original runtime. That
// caller reads the last 21 characters of what it gets, requires an eight
// character token to appear there twice nine apart, and keeps the token as the
// program's identity — which it then compares with the application id compiled
// into its own image. So one entry is `<AID> <AID> ...`: the program id and
// the name of its executable, which for a KTF archive are the same string,
// since the JAR, the client image and the program are all named by the AID.
//
// Everything about that is stated rather than assumed to generalise. A second
// title that reads this listing differently would be evidence this is wrong,
// and there is none here either way.
func (runtime *Runtime) wipicGetExecNames(thread *armcore.Thread) (uint32, error) {
	program, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	out, err := thread.Register(3)
	if err != nil {
		return 0, err
	}
	length, err := runtime.WIPICArgument(thread, 4)
	if err != nil {
		return 0, err
	}
	wanted, err := runtime.ReadCString(program, 512)
	if err != nil {
		return 0, fmt.Errorf("read %s program name: %w", runtime.platformName(), err)
	}
	installed := runtime.Client.ProgramName
	if installed == "" || out == 0 {
		return 0, nil
	}
	if wanted != "" && !strings.EqualFold(wanted, installed) {
		// Nothing is installed under that name, which is not an error: the
		// caller asked whether a program is there and the answer is no.
		return 0, nil
	}
	listing := installed + execNameSeparator + installed + execNameTrailer
	if uint64(len(listing))+1 > uint64(length) {
		return WIPICErrorShortBuf, nil
	}
	if err := runtime.Client.core.Memory().Write(out, append([]byte(listing), 0)); err != nil {
		return 0, fmt.Errorf("write %s executable names: %w", runtime.platformName(), err)
	}
	runtime.CountDiagnostic(fmt.Sprintf("exec names for %q -> %q", wanted, listing))
	// One program is installed, and the caller only checks that this is
	// positive before reading the listing.
	return 1, nil
}

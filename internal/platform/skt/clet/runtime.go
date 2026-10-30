package clet

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// initializationRuntime is the WIPI-C runtime a Clet calls into. It is the
// WIPI-C half of the KTF engine's runtime, without the Java, module and LCDUI
// state a Clet has none of: a Clet is a native program the platform calls at
// six entry points, so there is no interpreted bytecode, no card stack and no
// AOT metadata here. The handlers in this package are the KTF WIPI-C handlers,
// which is why the fields they read keep their names. See docs/skt-native.md.
type initializationRuntime struct {
	client *Client
	arena  *guestArena
	// arenaShadow is the debug-build copy of the arena's free bytes, and
	// shadowWindow the buffer its check reads guest memory through; both nil in
	// release builds. See arena_shadow.go.
	arenaShadow    *arenaShadow
	shadowWindow   []byte
	shadowedBlocks uint64
	checkedBlocks  uint64
	// hookWindow is the scratch space the C-library routines copy through, kept
	// between calls. See clet_clib.go.
	hookWindow []byte

	// codeCursor is the next free address in the stub arena, and stubs/stubKeys
	// map a (category,id) stub to its address and back.
	codeCursor uint64
	stubs      map[uint64]uint32
	stubKeys   map[uint32]uint64

	// currentThread and currentContext are the guest thread and Host context a
	// call is being serviced on, set while an entry or a call runs.
	currentThread  *armcore.Thread
	currentContext context.Context

	// virtualBaseMillis and clockBase anchor the guest's timeline to the
	// session clock at the moment the runtime was built. See guestMillis.
	virtualBaseMillis int64
	clockBase         time.Time

	// trace and callCounts are the diagnostic ring and per-name counter a Host
	// reads to see which boundary a stuck Clet is crossing. See diag.go.
	trace      traceRing
	callCounts map[diagEvent]uint32

	saveReadError error

	// The WIPI-C memory block bookkeeping: the arena size of each live
	// MC_knlAlloc block by its identifier, the image source buffers an image
	// record owns, and the addresses freeWIPIC last gave back.
	wipicAllocations   map[uint32]uint64
	imageSourceBuffers map[uint32]uint32
	releasedWIPIC      map[uint32]struct{}
	releasedWIPICOrder []uint32

	// screenFramebuffer is the handle MC_grpGetScreenFrameBuffer answers, and
	// framebufferOpacity the transparency of image framebuffers whose encoding
	// declared undrawn pixels. See wipic_graphics.go.
	screenFramebuffer  uint32
	framebufferOpacity map[uint32]*imageOpacity
	pixelOps           *pixelOpCache

	// pendingTimers is the live timer set a Clet arms its frames from, and
	// roundTimers the part of a service round's batch not reached yet.
	pendingTimers []wipicTimer
	roundTimers   []wipicTimer

	// The WIPI-C filesystem table's open files, and the storage bookkeeping the
	// file calls keep. See wipic_filesystem.go.
	cFiles              map[string]*runtimeCFile
	cFileHandles        map[uint32]*runtimeCFileHandle
	nextCDatabaseHandle uint32
	guestFiles          map[string][]byte
	guestFileWrites     map[string]uint64
	removedFiles        map[string]bool
	madeDirectories     map[string]bool
	removedCDatabases   map[string]bool

	// wipicClips holds each WIPI-C media clip by its guest address, in creation
	// order, and wipicMutedSources what MC_mdaSetMuteState was told per source.
	// See wipic_media.go.
	wipicClips        map[uint32]*wipicMediaClip
	wipicClipOrder    []uint32
	wipicMutedSources map[uint32]bool

	// userMemoryInterface is the handset library table handed to a Clet that
	// asked for it by name, and userMemoryPools the buffers it manages. See
	// wipic_usermem.go.
	userMemoryInterface uint32
	userMemoryPools     map[uint32]*userMemoryPool

	// inputModeTableAddress is the `M_Char **` the input-method table answers
	// with, and cInput the Host's side of the input method. See im.go.
	inputModeTableAddress uint32
	cInput                cInputState

	// events is the WIPI event queue a Clet drains through its own event entry,
	// and guestEventLoop records that it does. See events.go.
	events         []guestEvent
	guestEventLoop bool

	// leds is the indicator-light mask a title last set; this handset has none.
	leds int32

	// aotCallDepth guards re-entrant guest-call nesting per thread; see
	// enterAOTCall in bridge.go.
	aotCallDepth map[*armcore.Thread]uint32
	// detachedCFiles holds WIPI-C files whose names were open at a checkpoint; a
	// Clet has no checkpoint, so it stays empty. See wipic_filesystem.go.
	detachedCFiles map[string]*runtimeCFile
	// pendingNetCallbacks are the MC_netConnect failures owed to callers; net is
	// refused for a Clet, so none accumulate. See wipic_net.go.
	pendingNetCallbacks []wipicNetCallback
	// repaintPending, repaintServicing and guestFlushedOwnFrame track the frame
	// state the copied graphics handlers read; a Clet drives its own paint entry.
	repaintPending       bool
	repaintServicing     bool
	guestFlushedOwnFrame bool

	// collectAt is the arena high-water mark the KTF collector triggered at; a
	// Clet has no Java heap, so it is only the initial floor.
	collectAt uint64
}

// newInitializationRuntime builds the WIPI-C runtime for a Clet client: the
// platform data arena and the stub arena the root table's veneers live in.
// Nothing of the KTF Java startup applies — there is no VM, no class context
// and no interface to initialise.
func newInitializationRuntime(client *Client) (*initializationRuntime, error) {
	if client == nil || client.core == nil {
		return nil, fmt.Errorf("SKT Clet client is not initialized")
	}
	memory := client.core.Memory()
	if err := memory.Map(platformDataBase, platformDataSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map SKT Clet platform data: %w", err)
	}
	if err := memory.Map(platformCodeBase, platformCodeSize, armcore.PermissionReadExecute); err != nil {
		return nil, fmt.Errorf("map SKT Clet platform stubs: %w", err)
	}
	runtime := &initializationRuntime{
		client:             client,
		arena:              newGuestArena(platformDataBase, platformDataSize),
		collectAt:          collectionFloor,
		codeCursor:         uint64(platformCodeBase),
		stubs:              make(map[uint64]uint32),
		stubKeys:           make(map[uint32]uint64),
		wipicAllocations:   make(map[uint32]uint64),
		imageSourceBuffers: make(map[uint32]uint32),
		releasedWIPIC:      make(map[uint32]struct{}),
		framebufferOpacity: make(map[uint32]*imageOpacity),
		wipicClips:         make(map[uint32]*wipicMediaClip),
		wipicMutedSources:  make(map[uint32]bool),
		cFiles:             make(map[string]*runtimeCFile),
		cFileHandles:       make(map[uint32]*runtimeCFileHandle),
		detachedCFiles:     make(map[string]*runtimeCFile),
		aotCallDepth:       make(map[*armcore.Thread]uint32),
		guestFiles:         make(map[string][]byte),
		guestFileWrites:    make(map[string]uint64),
		removedFiles:       make(map[string]bool),
		madeDirectories:    make(map[string]bool),
		removedCDatabases:  make(map[string]bool),
		userMemoryPools:    make(map[uint32]*userMemoryPool),
		virtualBaseMillis:  client.now().UnixMilli(),
		clockBase:          client.now(),
		trace:              traceRing{limit: client.traceLimit},
	}
	runtime.installArenaShadow()
	return runtime, nil
}

// handleSupervisorCall answers the supervisor calls a Clet makes: the root
// table's veneers (svcCategoryClet) and the one semihosting immediate a title's
// C runtime carries. A Clet makes no other kind.
func (runtime *initializationRuntime) handleSupervisorCall(ctx context.Context, thread *armcore.Thread, call armcore.SupervisorCall) error {
	id, err := thread.Register(12)
	if err != nil {
		return err
	}
	previousThread, previousContext := runtime.currentThread, runtime.currentContext
	runtime.currentThread, runtime.currentContext = thread, ctx
	defer func() {
		runtime.currentThread, runtime.currentContext = previousThread, previousContext
	}()
	var result uint32
	switch call.Immediate {
	case svcCategoryClet:
		result, err = runtime.handleCletCall(thread, id)
	case svcSemihosting:
		result, err = runtime.handleCletSemihosting(thread)
	default:
		err = fmt.Errorf("unknown SKT Clet SVC category %d with id %#x", call.Immediate, id)
	}
	if err != nil {
		return err
	}
	return thread.SetRegister(0, result)
}

// describeGuestWord names what a word the guest handed the platform is, for
// failure messages. A Clet has no bound objects, so a non-zero word is reported
// as the number it is.
func (runtime *initializationRuntime) describeGuestWord(word uint32) string {
	if word == 0 {
		return "null"
	}
	return fmt.Sprintf("%#x", word)
}

// EXTRACTED-METHODS-BELOW
// countDiagnostic records a bounded per-name event count so acceptance probes
// can report which runtime boundary a looping client is crossing. When the
// Host enabled tracing it also retains the event in order, because a stuck
// game is diagnosed by the sequence of boundaries it crossed, not the totals.
func (runtime *initializationRuntime) countDiagnostic(name string) {
	runtime.recordDiagnostic(diagEvent{text: name})
}

// recordDiagnostic counts an event that has not been given a name yet. The busy
// boundaries take this path, because composing the name they would be counted
// under costs more than the crossing does; see diagEvent.
func (runtime *initializationRuntime) recordDiagnostic(event diagEvent) {
	runtime.trace.record(event)
	if runtime.callCounts == nil {
		runtime.callCounts = make(map[diagEvent]uint32)
	}
	if _, seen := runtime.callCounts[event]; seen {
		runtime.callCounts[event]++
		return
	}
	// A rare failure event is recorded for its call site and register dump —
	// collapsing those away would discard the only thing worth having — so it
	// keeps its full name right up to the ceiling.
	if len(runtime.callCounts) < diagnosticNameLimit ||
		(event.keepsDetail() && len(runtime.callCounts) < diagnosticNameCeiling) {
		runtime.callCounts[event]++
		return
	}
	// Ordinary names carry the guest call site that produced them, so a busy
	// game exhausts the name budget on one method called from hundreds of
	// addresses. Collapsing the site keeps the event itself countable instead
	// of dropping it all into one opaque bucket.
	collapsed := event.collapse()
	if _, seen := runtime.callCounts[collapsed]; !seen && len(runtime.callCounts) >= diagnosticNameCeiling {
		runtime.callCounts[diagEvent{text: "diagnostic overflow"}]++
		return
	}
	runtime.callCounts[collapsed]++
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

func diagnosticKeepsDetail(name string) bool {
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

// collapseDiagnosticName drops the " @0x<address>" call site diagnostics append
// to most event names, leaving the event itself.
func collapseDiagnosticName(name string) string {
	if site := strings.LastIndex(name, " @0x"); site > 0 {
		return name[:site]
	}
	return name
}

// guestMillis is the wall-clock time the guest sees. It starts at the Host's
// clock and then tracks the session clock, scaled by the speed multiplier, so
// the timeline the guest measures agrees with the waits it is actually
// granted: at 2x speed a 50ms sleep returns after 25ms of session time and the
// guest reads 50ms as having passed, and at 1x the two are the same thing.
//
// Tracking a clock rather than accumulating declared waits also means a guest
// that polls the time without sleeping still sees it move, which is what lets
// a busy-wait loop terminate.
func (runtime *initializationRuntime) guestMillis() int64 {
	if runtime == nil {
		return time.Now().UnixMilli()
	}
	// Every guest-visible read of the clock arrives here, which is what makes
	// this the place to count them: a service call that spends a step window
	// asking the time is waiting rather than computing, and the Host charges
	// the two differently. See continueHostService.
	runtime.client.guestClockReads++
	// A Host that moves its own clock cannot move it from inside the call the
	// guest is asking from, so the guest's own execution moves it instead. See
	// advanceBatchClockTo.
	runtime.client.advanceBatchClockTo(runtime.client.core.Steps())
	elapsed := runtime.client.now().Sub(runtime.clockBase)
	if elapsed < 0 {
		elapsed = 0
	}
	scaled := float64(elapsed) * runtime.client.speedOrDefault()
	return runtime.virtualBaseMillis + int64(time.Duration(scaled)/time.Millisecond)
}

// recordedMillis is guestMillis for a time the Host writes down for itself,
// such as when a database was last written, rather than one the guest asked
// for. It counts no read and moves no clock: guestMillis counts every call as
// the guest waiting on the time, which changes how the step budget charges the
// call it came from, so reading it for bookkeeping moved a title's timing
// (docs/history/ktf.md#ktf-storage-review-2026-10-22).
func (runtime *initializationRuntime) recordedMillis() int64 {
	if runtime == nil {
		return time.Now().UnixMilli()
	}
	return runtime.virtualBaseMillis + int64(runtime.guestElapsed()/time.Millisecond)
}

// guestElapsed is how much guest time has passed since the runtime was built,
// which is the timeline audio playback runs on. guestMillis answers the same
// clock as a wall-clock date, which is what the guest's own time APIs want.
func (runtime *initializationRuntime) guestElapsed() time.Duration {
	if runtime == nil {
		return 0
	}
	elapsed := runtime.client.now().Sub(runtime.clockBase)
	if elapsed < 0 {
		return 0
	}
	return time.Duration(float64(elapsed) * runtime.client.speedOrDefault())
}

func (runtime *initializationRuntime) handleWIPICCall(thread *armcore.Thread, id uint32) (result uint32, err error) {
	defer func() {
		if err == nil && runtime.saveReadError != nil {
			err = runtime.saveReadError
		}
	}()
	runtime.recordDiagnostic(diagEvent{kind: diagWIPICCall, nums: [5]uint32{id}})
	if id >= 1<<16 {
		return runtime.handleWIPICTableCall(thread, id>>16, id&0xffff)
	}
	switch id {
	case wipicKernelReserved1:
		// Slot 33 answers the WIPI-C interface table for the KTF module path;
		// no Clet reaches it, and a Clet has no such table.
		return wipicErrorInvalid, nil
	case wipicKernelAlloc, wipicKernelCalloc:
		size, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		address, err := runtime.allocateWIPIC(size)
		if err != nil {
			// A refused allocation is nearly always a size the game computed
			// wrongly rather than an arena that ran out, and the size alone
			// does not say where it came from. The stub returns with `bx lr`,
			// so naming the call site turns it into an address to disassemble.
			return 0, fmt.Errorf("%w%s", err, runtime.callerSite(thread))
		}
		return address, nil
	case wipicKernelFree:
		id, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		runtime.freeWIPIC(id)
		// The specification declares MC_knlFree void, so r0 on return is not
		// part of its contract and no correct caller may read it. Some do
		// anyway: a title ends an initialization step with `MC_knlFree(buf)`
		// in tail position and its caller tests the result, so writing zero
		// into r0 turned "the font loaded" into "the font failed" and stopped
		// the game before its first frame. Leaving r0 as the caller set it is
		// what a routine that never touches its return register does, and it
		// costs nothing precisely because the value is undefined.
		return id, nil
	case wipicKernelGetTotalMemory:
		return uint32(runtime.arena.reportedTotal()), nil
	case wipicKernelGetFreeMemory:
		return uint32(runtime.arena.reportedFree()), nil
	case wipicKernelPrintk:
		return runtime.wipicPrintk(thread)
	case wipicKernelSprintk:
		return runtime.wipicSprintk(thread)
	case wipicKernelGetCurProgramID:
		return runtime.programID(), nil
	case wipicKernelGetAccessLevel:
		return wipicAccessLevel, nil
	case wipicKernelGetExecNames:
		return runtime.wipicGetExecNames(thread)
	case wipicKernelLoad:
		// No library is installed beside the running program — the listing
		// above names only the program itself — so there is nothing to load
		// under any name: M_E_INVALID, the answer for a name that does not
		// resolve.
		runtime.countDiagnostic("load library")
		return wipicErrorInvalid, nil
	case wipicKernelGetProgramName:
		buffer, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		size, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		name := runtime.client.programName
		if name == "" {
			name = "wfeature"
		}
		if uint64(len(name))+1 > uint64(size) {
			return wipicErrorShortBuf, nil
		}
		if err := runtime.client.core.Memory().Write(buffer, append([]byte(name), 0)); err != nil {
			return 0, fmt.Errorf("write KTF program name: %w", err)
		}
		return 0, nil
	case wipicKernelDefTimer:
		return runtime.wipicDefTimer(thread)
	case wipicKernelSetTimer:
		return runtime.wipicSetTimer(thread)
	case wipicKernelUnsetTimer:
		return runtime.wipicUnsetTimer(thread)
	case wipicKernelCurrentTime:
		millis := uint64(runtime.guestMillis())
		if err := thread.SetRegister(1, uint32(millis>>32)); err != nil {
			return 0, err
		}
		return uint32(millis), nil
	case wipicKernelGetSysProperty:
		return runtime.wipicGetSystemProperty(thread)
	case wipicKernelSetSysProperty:
		return runtime.wipicSetSystemProperty(thread)
	case wipicKernelGetResourceID:
		return runtime.wipicGetResourceID(thread)
	case wipicKernelGetResource:
		return runtime.wipicGetResource(thread)
	case wipicKernelExit:
		// MC_knlExit ends the program; the Host observes ErrGuestExited and
		// tears the session down instead of treating it as a failure. The
		// caller is named for the same reason a failure names one: an ending
		// nothing can locate reads as a broken game rather than a finished
		// one. The stub returns with `bx lr`, so LR is still the caller.
		runtime.countDiagnostic("kernel exit")
		link, err := thread.Register(armcore.RegisterLR)
		if err != nil {
			return 0, ErrGuestExited
		}
		return 0, fmt.Errorf("MC_knlExit from %#x: %w", link, ErrGuestExited)
	case wipicKernelGetDLLInterface:
		return runtime.wipicGetDLLInterface(thread)
	default:
		// The stub returns with `bx lr`, so LR still holds the caller's return
		// address. Naming it turns "some kernel slot is missing" into an address
		// that can be disassembled in the image.
		return 0, fmt.Errorf("KTF WIPI C SVC id %#x is not implemented%s", id, runtime.callerSite(thread))
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
func (runtime *initializationRuntime) programID() uint32 {
	name := runtime.client.programName
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

func (runtime *initializationRuntime) callerSite(thread *armcore.Thread) string {
	lr, err := thread.Register(armcore.RegisterLR)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(" (called from %#x)", lr)
}

// callerMark renders the same call site in the ` @0x` form the diagnostic
// counter understands, for events that are counted rather than raised.
// diagEvent.collapse strips exactly this suffix, so an event can carry the
// address that produced it without a caller in a loop spending the whole name
// budget on one boundary.
func (runtime *initializationRuntime) callerMark(thread *armcore.Thread) string {
	lr, err := thread.Register(armcore.RegisterLR)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(" @%#x", lr)
}

func (runtime *initializationRuntime) handleWIPICTableCall(thread *armcore.Thread, table, function uint32) (uint32, error) {
	switch {
	case table == wipicTableUserMemory:
		return runtime.handleUserMemoryCall(thread, function)
	case table == wipicTableUtil:
		return runtime.handleWIPICUtilCall(thread, function)
	case table == wipicTableMisc && function == 0:
		// MC_miscBackLight controls the handset backlight; the browser host
		// keeps the screen lit.
		return 0, nil
	case table == wipicTableGraphics && function == 0:
		// MC_grpGetImageProperty: 4 is width and 5 is height.
		handle, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		property, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		image, ok, err := runtime.readWIPICDrawSurface(handle, "get image property")
		if err != nil || !ok {
			// An image that is not there has no width and no height, which is
			// the same answer as a property this call does not know.
			return 0, err
		}
		runtime.countDiagnostic(fmt.Sprintf("getImageProperty %d", property))
		switch property {
		case 4:
			return image.width, nil
		case 5:
			return image.height, nil
		default:
			return 0, nil
		}
	case table == wipicTableGraphics && function == 1:
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
	case table == wipicTableGraphics && function == 2:
		return runtime.wipicGetScreenFramebuffer()
	case table == wipicTableGraphics && function == 4:
		// MC_grpCreateOffScreenFrameBuffer.
		width, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		height, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		return runtime.newWIPICFramebufferRecord(width, height)
	case table == wipicTableGraphics && function == 3:
		return runtime.wipicDestroyOffScreenFrameBuffer(thread)
	case table == wipicTableGraphics && function == 8:
		return runtime.wipicPutPixel(thread)
	case table == wipicTableGraphics && function == 9:
		return runtime.wipicDrawLine(thread)
	case table == wipicTableGraphics && function == 10:
		return runtime.wipicDrawRect(thread)
	case table == wipicTableGraphics && function == 11:
		return runtime.wipicFillRect(thread)
	case table == wipicTableGraphics && function == 12:
		return runtime.wipicCopyFramebuffer(thread)
	case table == wipicTableGraphics && function == 13:
		return runtime.wipicDrawImage(thread)
	case table == wipicTableGraphics && function == 14:
		return runtime.wipicCopyArea(thread)
	case table == wipicTableGraphics && function == 15:
		return runtime.wipicDrawArc(thread)
	case table == wipicTableGraphics && function == 16:
		return runtime.wipicFillArc(thread)
	case table == wipicTableGraphics && function == 17:
		return runtime.wipicDrawString(thread, false)
	case table == wipicTableGraphics && function == 18:
		return runtime.wipicDrawString(thread, true)
	case table == wipicTableGraphics && function == 19:
		return runtime.wipicTransferRGBPixels(thread, false)
	case table == wipicTableGraphics && function == 20:
		return runtime.wipicTransferRGBPixels(thread, true)
	case table == wipicTableGraphics && function == 21:
		return 0, runtime.flushLCD(thread)
	case table == wipicTableGraphics && function == 5:
		return runtime.wipicInitGraphicsContext(thread)
	case table == wipicTableGraphics && function == 6:
		return runtime.wipicAccessGraphicsContext(thread, true)
	case table == wipicTableGraphics && function == 7:
		return runtime.wipicAccessGraphicsContext(thread, false)
	case table == wipicTableGraphics && function == 22:
		return runtime.wipicPixelFromRGB(thread)
	case table == wipicTableGraphics && function == 23:
		return runtime.wipicRGBFromPixel(thread)
	case table == wipicTableGraphics && function == 24:
		return runtime.wipicGetDisplayInfo(thread)
	case table == wipicTableGraphics && function == 25:
		// MC_grpRepaint(lcd, x, y, w, h) queues a repaint rather than painting
		// here; the region is discarded because a card is repainted whole.
		runtime.repaintPending = true
		runtime.postRepaintEvent()
		return 0, nil
	case table == wipicTableGraphics && function == 26:
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
			size = uint32(runtime.fontHeight())
		}
		return size, nil
	case table == wipicTableGraphics && function == 27:
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
		return uint32(runtime.fontHeight()), nil
	case table == wipicTableGraphics && function == 28:
		return uint32(runtime.fontBaseline()), nil
	case table == wipicTableGraphics && function == 29:
		return uint32(runtime.fontHeight() - runtime.fontBaseline()), nil
	case table == wipicTableGraphics && function == 30:
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
		text, err := runtime.wipicReadString(pointer, int32(length), false)
		if err != nil {
			return 0, err
		}
		return uint32(runtime.graphicsTextWidth(text)), nil
	case table == wipicTableGraphics && function == 31:
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
		text, err := runtime.wipicReadString(pointer, int32(length), true)
		if err != nil {
			return 0, err
		}
		return uint32(runtime.graphicsTextWidth(text)), nil
	case table == wipicTableGraphics && function == 32:
		return runtime.wipicCreateImage(thread)
	case table == wipicTableGraphics && function == 33:
		return runtime.wipicDestroyImage(thread)
	case table == wipicTableGraphics && function == 34:
		return runtime.wipicDecodeNextImage(thread)
	case table == wipicTableGraphics && function == 35:
		return runtime.wipicEncodeImage(thread)
	case table == wipicTableGraphics && function == 36:
		return runtime.wipicPostEvent(thread)
	case table == wipicTableGraphics && function == 37:
		return runtime.wipicPolygon(thread, false)
	case table == wipicTableGraphics && function == 38:
		return runtime.wipicPolygon(thread, true)
	case table == wipicTableFilesystem:
		return runtime.handleWIPICFileCall(thread, function)
	case table == wipicTableMedia:
		return runtime.handleWIPICMediaCall(thread, function)
	case table == wipicTableNet:
		return runtime.handleWIPICNetCall(thread, function)
	case table == wipicTableUIC:
		// The UI component table draws platform widgets — text fields, menus,
		// soft keys — over the game's screen. Nothing in the local archives
		// calls it, and a widget answered with a handle but never drawn would
		// leave a game waiting for input from something invisible, so a
		// creation call fails and the rest are accepted no-ops.
		runtime.countDiagnostic(fmt.Sprintf("wipic uic function %d", function))
		if function == 0 {
			return wipiErrorCode, nil
		}
		return 0, nil
	case table == wipicTableInputMethod:
		return runtime.handleWIPICInputMethodCall(thread, function)
	case table >= 4 && table != wipicTableFilesystem:
		// Tables 5-6, 8, and 12-17 are stubbed in the original runtime; calls
		// are accepted, counted, and answer zero.
		//
		// The count carries the guest address it was called from, for the same
		// reason the unimplemented-table error does: a table number never
		// appears in the guest's own code, so a stub line naming only a number
		// cannot be investigated at all, while one naming an address can be
		// disassembled. A stub answers rather than fails, so this is the only
		// place the address is ever recorded.
		runtime.countDiagnostic(fmt.Sprintf("wipic stub table %d function %d%s",
			table, function, runtime.callerMark(thread)))
		return 0, nil
	}
	// The stub returns with `bx lr`, so LR still holds the guest's return
	// address. Naming it turns "some table is missing" into an address to
	// disassemble, which is the only way to find out what an unnumbered table
	// is for: the interface is an array of function pointers and the game
	// indexes it, so the table number never appears in the guest's own code.
	return 0, fmt.Errorf("KTF WIPI C table %d function %d is not implemented%s",
		table, function, runtime.callerSite(thread))
}

func (runtime *initializationRuntime) wipicInitGraphicsContext(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	if err := runtime.client.core.Memory().Write(address, make([]byte, wipicGraphicsContextSize)); err != nil {
		return 0, fmt.Errorf("initialize KTF graphics context at %#x: %w", address, err)
	}
	return 0, nil
}

// wipicAccessGraphicsContext implements MC_grpSetContext and MC_grpGetContext,
// which copy one field between the context record and the caller's value
// pointer or immediate.
func (runtime *initializationRuntime) wipicAccessGraphicsContext(thread *armcore.Thread, set bool) (uint32, error) {
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
	memory := runtime.client.core.Memory()
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
			accessErr = runtime.writeWord(contextAddress, enabled)
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
		accessErr = transfer(wipicContextPixelOpOffset, 4, false) // pixel-op function
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
		return 0, fmt.Errorf("access KTF graphics context field %d at %#x with value %#x%s: %w",
			operation, contextAddress, value, runtime.callerSite(thread), accessErr)
	}
	return 0, nil
}

// wipicPixelFromRGB converts 8-bit color components to the RGB565 pixel the
// 16-bit screen mode reports.
func (runtime *initializationRuntime) wipicPixelFromRGB(thread *armcore.Thread) (uint32, error) {
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

func (runtime *initializationRuntime) wipicRGBFromPixel(thread *armcore.Thread) (uint32, error) {
	pixel, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	memory := runtime.client.core.Memory()
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
			return 0, fmt.Errorf("write KTF pixel component %d: %w", index, err)
		}
	}
	return pixel, nil
}

// wipicGetScreenFramebuffer lazily builds the single MC_GrpFrameBuffer record
// {width, height, bpl, bpp, buf} whose pixel buffer guest code writes
// directly. Host presentation reads the same buffer when the flush path is
// connected.
func (runtime *initializationRuntime) wipicGetScreenFramebuffer() (uint32, error) {
	if runtime.screenFramebuffer != 0 {
		return runtime.screenFramebuffer, nil
	}
	width, height := runtime.screenSize()
	record, err := runtime.newWIPICFramebufferRecord(uint32(width), uint32(height))
	if err != nil {
		return 0, fmt.Errorf("create KTF screen framebuffer: %w", err)
	}
	runtime.screenFramebuffer = record
	return record, nil
}

// screenSize is the handset this game was told it runs on. It is the one
// answer the screen framebuffer and MC_grpGetDisplayInfo both read, because a
// game that is given a width by one and a stride by the other writes its rows
// at the wrong offsets; see Client.SetScreen.
func (runtime *initializationRuntime) screenSize() (int, int) {
	if runtime == nil {
		return runtimeDisplayPixelWidth, runtimeDisplayPixelHeight
	}
	return runtime.client.screenSize()
}

// wipicGetDisplayInfo answers MC_grpGetDisplayInfo with the 16-bit 565 screen
// this game runs on — 240x320, the one the original runtime reports, unless
// the Host named another handset. The struct stores the red, blue, and green
// masks in that order.
func (runtime *initializationRuntime) wipicGetDisplayInfo(thread *armcore.Thread) (uint32, error) {
	outAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	width, height := runtime.screenSize()
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
	if err := runtime.client.core.Memory().Write(outAddress, data); err != nil {
		return 0, fmt.Errorf("write KTF display info at %#x: %w", outAddress, err)
	}
	return 1, nil
}

func (runtime *initializationRuntime) wipicDefTimer(thread *armcore.Thread) (uint32, error) {
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
	if err := runtime.client.core.Memory().Write(pointer, word[:]); err != nil {
		return 0, fmt.Errorf("write KTF timer record at %#x: %w", pointer, err)
	}
	return 0, nil
}

func (runtime *initializationRuntime) wipicSetTimer(thread *armcore.Thread) (uint32, error) {
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
	callback, err := runtime.readAOTWords(pointer, 1, "timer record")
	if err != nil {
		return 0, err
	}
	delay := uint64(high)<<32 | uint64(low)
	// A guest that asks for an absurd delay is asking for a timer that never
	// fires in a session; clamping keeps the deadline arithmetic in range
	// without changing any delay a game actually uses.
	wait := time.Duration(min(delay, maxTimerDelayMillis)) * time.Millisecond
	armed := wipicTimer{
		pointer:  pointer,
		callback: callback[0],
		param:    param,
		delay:    delay,
		due:      runtime.client.framePeriodDeadline(wait),
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
	runtime.cancelRoundTimers(func(timer wipicTimer) bool { return timer.pointer == pointer })
	for index, timer := range runtime.pendingTimers {
		if timer.pointer == pointer {
			runtime.pendingTimers[index] = armed
			return 0, nil
		}
	}
	if len(runtime.pendingTimers) >= maxPendingTimers {
		return 0, fmt.Errorf("KTF pending timer count exceeds %d", maxPendingTimers)
	}
	runtime.pendingTimers = append(runtime.pendingTimers, armed)
	return 0, nil
}

func (runtime *initializationRuntime) wipicUnsetTimer(thread *armcore.Thread) (uint32, error) {
	pointer, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	remaining := runtime.pendingTimers[:0]
	for _, timer := range runtime.pendingTimers {
		if timer.pointer != pointer {
			remaining = append(remaining, timer)
		}
	}
	runtime.pendingTimers = remaining
	runtime.cancelRoundTimers(func(timer wipicTimer) bool { return timer.pointer == pointer })
	return 0, nil
}

// cancelRoundTimers zeroes the entries a running timer round has still to
// reach that a predicate selects, and reports how many it cancelled. The
// round skips a zeroed entry and does not queue it again.
func (runtime *initializationRuntime) cancelRoundTimers(match func(wipicTimer) bool) int {
	cancelled := 0
	for index, timer := range runtime.roundTimers {
		if timer.callback != 0 && match(timer) {
			runtime.roundTimers[index] = wipicTimer{}
			cancelled++
		}
	}
	return cancelled
}

// requeueTimers puts the entries a round did not reach back on the queue,
// leaving out the ones a callback cancelled meanwhile.
func (runtime *initializationRuntime) requeueTimers(rest []wipicTimer) {
	for _, timer := range rest {
		if timer.callback != 0 {
			runtime.pendingTimers = append(runtime.pendingTimers, timer)
		}
	}
}

// allocateWIPIC lays out the original MC_knlAlloc indirect record inside the
// bounded platform arena and records the block so MC_knlFree can give it back.
//
// The payload is cleared rather than assumed clear. The arena used to be
// bump-only, so a block was a piece of a freshly mapped region and calloc's
// zero fill came for free; now that released blocks are handed out again, the
// zero fill the specification promises has to be written.
func (runtime *initializationRuntime) allocateWIPIC(size uint32) (uint32, error) {
	if size == 0 {
		return 0, nil
	}
	total := uint64(size) + uint64(wipicAllocationOverhead)
	address, err := runtime.allocate(total)
	if err != nil {
		return 0, err
	}
	if err := runtime.clearGuestRange(address, total); err != nil {
		return 0, fmt.Errorf("clear KTF WIPI C allocation at %#x: %w", address, err)
	}
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:], address+runtime.wipicHandleFirstWord())
	binary.LittleEndian.PutUint32(header[4:], size)
	if err := runtime.client.core.Memory().Write(address, header); err != nil {
		return 0, fmt.Errorf("write KTF WIPI C allocation header at %#x: %w", address, err)
	}
	if runtime.wipicAllocations == nil {
		runtime.wipicAllocations = make(map[uint32]uint64)
	}
	runtime.wipicAllocations[address] = total
	// The span is live again, so it is no longer a release anyone can be
	// reading through.
	delete(runtime.releasedWIPIC, address)
	return address, nil
}

// wipicHandleData answers where a memory identifier's data starts, given the
// word the identifier points at. The identifier is the specification's
// indirect handle and its data pointer is MC_GETDPTR, whose expansion is the
// handset's: on KTF the word points at the size word, eight bytes before the
// data, and on SKT it points at the data itself — one SKT title copies a
// resource out of `*id` and reads a framebuffer's fields at `**id`. So the
// answer follows the program running, and an identifier the guest built itself
// is read the way its own macros built it.
func (runtime *initializationRuntime) wipicHandleData(first uint32) uint32 {
	if runtime.client.clet != nil {
		return first
	}
	return first + 8
}

// wipicHandleFirstWord is what allocateWIPIC stores in an identifier's first
// word, as an offset from the identifier: the size word four bytes on for KTF,
// and for a Clet the data, twelve bytes on. Either way the data starts at the
// same place, wipicAllocationOverhead bytes in, so everything this runtime
// writes there itself is the same for both.
func (runtime *initializationRuntime) wipicHandleFirstWord() uint32 {
	if runtime.client.clet != nil {
		return wipicAllocationOverhead
	}
	return 4
}

// clearGuestRange writes zeros over one guest range in bounded chunks, so the
// fill costs a fixed buffer rather than one the size of the allocation.
func (runtime *initializationRuntime) clearGuestRange(address uint32, size uint64) error {
	memory := runtime.client.core.Memory()
	for offset := uint64(0); offset < size; {
		span := min(size-offset, uint64(len(wipicZeroFill)))
		if err := memory.Write(address+uint32(offset), wipicZeroFill[:span]); err != nil {
			return err
		}
		offset += span
	}
	return nil
}

// freeWIPIC implements MC_knlFree: the block a memory identifier names goes
// back to the arena to be handed out again. Without this a title that
// allocates and frees a working buffer every frame — which is what the drawing
// loop of at least one local title does, sixteen times a tick — walks the
// arena's whole 64 MiB and stops the run at a screen it was only sitting on.
//
// Freeing something this platform never handed out is counted and ignored
// rather than fatal. The specification makes it the program's own error, a
// double free reaches here as an identifier that has already been dropped, and
// a handset would not end the program over either.
func (runtime *initializationRuntime) freeWIPIC(id uint32) {
	if id == 0 {
		return
	}
	size, ok := runtime.wipicAllocations[id]
	if !ok {
		runtime.countDiagnostic("wipic free of an unknown allocation")
		return
	}
	delete(runtime.wipicAllocations, id)
	for image, source := range runtime.imageSourceBuffers {
		if image == id || source == id {
			delete(runtime.imageSourceBuffers, image)
		}
	}
	// Transparency is recorded against the handle of the framebuffer or image
	// that lives in the block, so a released address must not carry its mask
	// into whatever is allocated there next.
	runtime.setFramebufferOpacity(id, nil)
	runtime.noteReleasedWIPIC(id)
	runtime.arena.release(id, size)
}

func (runtime *initializationRuntime) noteReleasedWIPIC(id uint32) {
	if runtime.releasedWIPIC == nil {
		runtime.releasedWIPIC = make(map[uint32]struct{}, maxReleasedWIPIC)
	}
	if _, already := runtime.releasedWIPIC[id]; already {
		return
	}
	if len(runtime.releasedWIPICOrder) >= maxReleasedWIPIC {
		delete(runtime.releasedWIPIC, runtime.releasedWIPICOrder[0])
		runtime.releasedWIPICOrder = runtime.releasedWIPICOrder[1:]
	}
	runtime.releasedWIPIC[id] = struct{}{}
	runtime.releasedWIPICOrder = append(runtime.releasedWIPICOrder, id)
}

// wasReleasedWIPIC reports that the guest freed this address and nothing has
// been allocated at it since.
func (runtime *initializationRuntime) wasReleasedWIPIC(id uint32) bool {
	_, released := runtime.releasedWIPIC[id]
	return released
}

// wipicResource resolves a guest resource name against the attached archive
// entries. Names stay raw bytes, so the original encoding matches the archive
// entry names without transcoding.
func (runtime *initializationRuntime) wipicResource(name string) ([]byte, bool) {
	if data, ok := runtime.client.resources[name]; ok {
		return data, true
	}
	trimmed := strings.TrimPrefix(name, "/")
	data, ok := runtime.client.resources[trimmed]
	return data, ok
}

// wipicSetSystemProperty refuses every property with M_E_INVALID, the
// specification's answer for information that cannot be set. The handset's
// identity is not the title's to change — the Java HandsetProperty setter
// answers false for the same reason — and a value accepted here would have
// to be remembered and answered back by the getter for the rest of the run.
func (runtime *initializationRuntime) wipicSetSystemProperty(thread *armcore.Thread) (uint32, error) {
	// The value is never read: nothing keeps it, so a value too long for a
	// diagnostic is no reason to fail a call that refuses it anyway.
	idAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readCString(idAddress, 128)
	if err != nil {
		return 0, fmt.Errorf("read KTF system property id: %w", err)
	}
	runtime.countDiagnostic("sysprop set " + name)
	return wipicErrorInvalid, nil
}

func (runtime *initializationRuntime) wipicGetSystemProperty(thread *armcore.Thread) (uint32, error) {
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
	name, err := runtime.readCString(idAddress, 128)
	if err != nil {
		return 0, fmt.Errorf("read KTF system property id: %w", err)
	}
	runtime.countDiagnostic("sysprop " + name)
	value, ok := runtime.client.systemProperty(name)
	if !ok {
		return wipicErrorInvalid, nil
	}
	if uint64(len(value))+1 > uint64(bufferSize) {
		return wipicErrorShortBuf, nil
	}
	if err := runtime.client.core.Memory().Write(outAddress, append([]byte(value), 0)); err != nil {
		return 0, fmt.Errorf("write KTF system property %q: %w", name, err)
	}
	return 0, nil
}

func (runtime *initializationRuntime) wipicGetResourceID(thread *armcore.Thread) (uint32, error) {
	nameAddress, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	sizeAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readCString(nameAddress, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF resource name: %w", err)
	}
	runtime.countDiagnostic("resource " + name)
	data, ok := runtime.wipicResource(name)
	writeSize := func(size uint32) error {
		if sizeAddress == 0 {
			return nil
		}
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], size)
		return runtime.client.core.Memory().Write(sizeAddress, word[:])
	}
	if !ok {
		if err := writeSize(0); err != nil {
			return 0, fmt.Errorf("write KTF resource size: %w", err)
		}
		return wipicErrorNotFound, nil
	}
	handle, err := runtime.allocateBytes(append([]byte(name), 0))
	if err != nil {
		return 0, err
	}
	if err := writeSize(uint32(len(data))); err != nil {
		return 0, fmt.Errorf("write KTF resource size: %w", err)
	}
	return handle, nil
}

func (runtime *initializationRuntime) wipicGetResource(thread *armcore.Thread) (uint32, error) {
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
		return wipicErrorGeneric, nil
	}
	name, err := runtime.readCString(handle, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF resource handle: %w", err)
	}
	data, ok := runtime.wipicResource(name)
	if !ok {
		return wipicErrorNotFound, nil
	}
	if uint64(len(data)) > uint64(bufferSize) {
		return wipicErrorGeneric, nil
	}
	base, err := runtime.readAOTWords(buffer, 1, "WIPI C resource buffer handle")
	if err != nil {
		return 0, err
	}
	if err := runtime.client.core.Memory().Write(runtime.wipicHandleData(base[0]), data); err != nil {
		return 0, fmt.Errorf("write KTF resource %q: %w", name, err)
	}
	return 0, nil
}

func (runtime *initializationRuntime) stub(category, id uint32) (uint32, error) {
	if category > 0xff {
		return 0, fmt.Errorf("KTF SVC category %d exceeds Thumb immediate range", category)
	}
	key := uint64(category)<<32 | uint64(id)
	if address := runtime.stubs[key]; address != 0 {
		return address, nil
	}
	const stubSize = uint64(svcStubSize)
	if runtime.codeCursor+stubSize > uint64(platformCodeBase)+platformCodeSize {
		return 0, fmt.Errorf("KTF platform callback stub space exhausted")
	}
	address := uint32(runtime.codeCursor)
	if err := runtime.client.core.Memory().Load(address, svcStub(category, id)); err != nil {
		return 0, fmt.Errorf("load KTF platform callback stub: %w", err)
	}
	runtime.codeCursor += stubSize
	runtime.stubs[key] = address | 1
	runtime.stubKeys[address|1] = key
	return address | 1, nil
}

func (runtime *initializationRuntime) allocate(size uint64) (uint32, error) {
	if size > maxPlatformAllocation {
		return 0, fmt.Errorf("KTF platform allocation %d exceeds %d bytes", size, maxPlatformAllocation)
	}
	address, ok := runtime.arena.allocate(size)
	if !ok {
		// The arena being full is the collector's second trigger: what would
		// have made room may already be unreachable and merely waiting for the
		// growth trigger to come round. See collectForAllocation.
		if runtime.collectForAllocation() {
			address, ok = runtime.arena.allocate(size)
		}
	}
	if !ok {
		return 0, fmt.Errorf("KTF platform initialization data space exhausted")
	}
	return address, nil
}

func (runtime *initializationRuntime) allocateWords(words []uint32) (uint32, error) {
	data := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	return runtime.allocateBytes(data)
}

func (runtime *initializationRuntime) allocateBytes(data []byte) (uint32, error) {
	address, err := runtime.allocate(uint64(len(data)))
	if err != nil {
		return 0, err
	}
	if err := runtime.client.core.Memory().Write(address, data); err != nil {
		return 0, fmt.Errorf("write KTF platform data at %#x: %w", address, err)
	}
	return address, nil
}

func (runtime *initializationRuntime) readCString(address uint32, limit uint32) (string, error) {
	memory := runtime.client.core.Memory()
	// The walk reads a name onto the stack, so the string made of it is the
	// only allocation a name shorter than the buffer costs.
	var buffer [guestStringBuffer]byte
	length, terminated, err := scanGuestString(memory, address, limit, 1, &buffer)
	if err != nil {
		return "", err
	}
	if !terminated {
		return "", fmt.Errorf("string at %#x exceeds %d bytes", address, limit)
	}
	var data []byte
	if length < guestStringBuffer {
		data = buffer[:length]
	} else {
		data = make([]byte, length)
		if err := memory.Read(address, data); err != nil {
			return "", err
		}
	}
	return string(data), nil
}

// readBoundedString reads at most limit bytes, stopping early at a terminator.
// Running to the bound is the expected end rather than an error: the caller
// asked for a fixed count of bytes, not for a C string.
func (runtime *initializationRuntime) readBoundedString(address, limit uint32) ([]byte, error) {
	data, _, err := readGuestString(runtime.client.core.Memory(), address, limit, 1)
	return data, err
}

// scanGuestString is the walk every KTF reader of a terminated guest string
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
func scanGuestString(memory *armcore.Memory, address, limit, unit uint32, buffer *[guestStringBuffer]byte) (uint32, bool, error) {
	// A unit is one byte or two, so a whole number of them is a mask away.
	whole := ^(uint64(unit) - 1)
	walk := uint64(limit) & whole
	size := uint64(guestStringChunk)
	for offset := uint64(0); offset < walk; {
		current := uint64(address) + offset
		if current+uint64(unit) > uint64(1)<<32 {
			return 0, false, errGuestStringOverflow
		}
		// The string is read into place while it fits in buffer, so it is
		// there if it ends before buffer does. Past that the caller reads it
		// again in one piece, and buffer is only where each chunk lands.
		start := uint64(0)
		if offset < guestStringBuffer {
			start = offset
		}
		span := min(size, guestStringBuffer-start, walk-offset, uint64(1)<<32-current) & whole
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
		size = guestStringBuffer
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

// readGuestString reads the guest string at address as scanGuestString walks
// it, into a slice of its own exactly as long as the string, and reports
// whether it met the terminator. The slice has room for one byte more,
// because a string read to be copied back into the guest goes back with its
// terminator after it.
func readGuestString(memory *armcore.Memory, address, limit, unit uint32) ([]byte, bool, error) {
	var buffer [guestStringBuffer]byte
	length, terminated, err := scanGuestString(memory, address, limit, unit, &buffer)
	if err != nil {
		return nil, false, err
	}
	data := make([]byte, length, int(length)+1)
	if length < guestStringBuffer {
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
func (runtime *initializationRuntime) wipicGetExecNames(thread *armcore.Thread) (uint32, error) {
	program, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	out, err := thread.Register(3)
	if err != nil {
		return 0, err
	}
	length, err := runtime.wipicArgument(thread, 4)
	if err != nil {
		return 0, err
	}
	wanted, err := runtime.readCString(program, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF program name: %w", err)
	}
	installed := runtime.client.programName
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
		return wipicErrorShortBuf, nil
	}
	if err := runtime.client.core.Memory().Write(out, append([]byte(listing), 0)); err != nil {
		return 0, fmt.Errorf("write KTF executable names: %w", err)
	}
	runtime.countDiagnostic(fmt.Sprintf("exec names for %q -> %q", wanted, listing))
	// One program is installed, and the caller only checks that this is
	// positive before reading the listing.
	return 1, nil
}

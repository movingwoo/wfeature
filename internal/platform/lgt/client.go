package lgt

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"sync"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// The guest address map. The module keeps whatever addresses its ELF names —
// that is the point of ELF loading — so the platform's own regions sit above
// anything a module has been seen to use.
const (
	// heapBase is where MC_knlAlloc hands out memory.
	heapBase uint32 = 0x20000000
	heapSize uint64 = 32 << 20
	// platformDataBase holds the structures the platform writes for the
	// module: the init parameter blocks, framebuffers, resource copies.
	platformDataBase uint32 = 0x30000000
	platformDataSize uint64 = 32 << 20
	// platformCodeBase holds the SVC stubs the import table hands back. It
	// sits above the data arena with a gap: the arena hands out every byte it
	// spans, and a stub arena inside that span would be handed to the module
	// and written over once a title's allocations passed it. Both mappings
	// being in force at once makes that write permitted rather than faulted,
	// so the corruption would only surface later, as a stub decoding into
	// whatever the module stored there.
	platformCodeBase uint32 = 0x33000000
	platformCodeSize uint64 = 1 << 20
	// This expression does not compile if the two are ever made to overlap
	// again: an unsigned constant cannot go negative.
	_ = uint64(platformCodeBase) - (uint64(platformDataBase) + platformDataSize)
	// stackBase is the Clet thread's stack.
	stackBase uint32 = 0x40000000
	stackSize uint64 = 1 << 20
	// returnAddress is the sentinel a platform-initiated call returns to. It
	// is not mapped: execution stops when the PC reaches it.
	returnAddress uint32 = 0x7fff0000
)

// SVC categories. The import table hands out a stub per (category, slot), and
// the category decides which table services it.
// cletQuantum is how many instructions the ARM core retires before it yields
// to the Go scheduler on this platform. See the comment at the Core it is
// passed to.
const cletQuantum = uint32(16384)

const (
	svcCategoryInit   uint32 = 1
	svcCategoryWIPIC  uint32 = 2
	svcCategoryStdlib uint32 = 3
	svcCategoryOEM    uint32 = 4
	svcCategoryJava   uint32 = 5
)

// Init-table slots: the two callbacks the module is handed at entry.
const (
	initSVCGetImportTable    uint32 = 0
	initSVCGetImportFunction uint32 = 1
)

// The import tables a module asks for. Everything else is an error rather
// than a silent zero, because a module handed a null function pointer
// branches to zero and the failure surfaces far from its cause.
const (
	importTableWIPIC  uint32 = 0x1fb
	importTableJava   uint32 = 0x64
	importTableStdlib uint32 = 0x1
	importTableOEM    uint32 = 0x1f8
)

const maxPlatformAllocation uint64 = 8 << 20

// CletFunctions is the entry point table a Clet registers with
// MC_cletRegister. The runtime calls into the game only through these.
type CletFunctions struct {
	Address     uint32
	Start       uint32
	Pause       uint32
	Resume      uint32
	Destroy     uint32
	Paint       uint32
	HandleEvent uint32
}

// Client is one loaded LGT game.
type Client struct {
	core      *armcore.Core
	thread    *armcore.Thread
	archive   *Archive
	module    *Module
	logger    *slog.Logger
	saveStore backend.SaveStore

	mu        sync.Mutex
	arena     *arena
	heap      *arena
	codeCurse uint32
	stubs     map[uint64]uint32
	clet      CletFunctions
	exited    bool

	// screen is the LCD the game draws into, and frame is what the Host
	// takes. A Clet may draw straight into the framebuffer memory, so the
	// conversion happens when the Host asks rather than on every write.
	screen       *framebuffer
	framePending bool
	frameRGBA    []byte
	flushes      uint64

	framebuffers map[uint32]*framebuffer
	timers       map[uint32]*timer
	nextHandle   uint32

	// pixelOps caches what each pixel operation has answered, keyed by the
	// operation and its parameter, so a draw asks the guest once per pair of
	// pixels rather than once per pixel drawn; see pixelop.go.
	pixelOps map[uint64]*pixelOpCache

	// netConnects are the accepted dials whose refusal has not been reported
	// yet; see wipic_net.go for why a dial is accepted at all.
	netConnects []pendingNetConnect

	clock  *guestClock
	events []pendingEvent

	// audio holds the sounds the game loaded and advances them on the guest's
	// own clock, so a run batching ticks hears the same sequence as one on the
	// wall clock, only faster. The clips and the volume and mute levels are the
	// media block's state; see wipic_media.go.
	audio        *backend.Audio
	clips        map[uint32]*mediaClip
	volume       int32
	sourceVolume map[uint32]int32
	sourceMuted  map[uint32]bool

	files map[uint32]*openFile
	// removed is the set of paths MC_fsRemove has deleted, loaded from the
	// store on first use. See fileRemovedKey for why a delete needs a list.
	removed map[string]bool

	traceLive string
	traceOut  io.Writer

	// tmStorage is localtime's result, allocated on first use. ANSI C says the
	// caller does not own it, so there is one per client rather than one per
	// call.
	tmStorage uint32

	// cRandom is the C library's generator, the one `srand` seeds. It is not
	// the generator behind a `java/util/Random`: a title may hold both, and
	// seeding one must not move the other.
	cRandom *rand.Rand

	// inputMode is the automaton's current mode, an index into inputModes, and
	// inputModeTableAddress is the string array MC_imGetSupportedModes answers
	// with, allocated on first use. See wipic_im.go.
	inputMode             uint32
	inputModeTableAddress uint32

	// javaApplication is set once the module resolves anything from the Java
	// interface table. See java.go.
	javaApplication bool
	// javaClasses is the application's own class metadata, and javaLink the
	// platform surface it links against, once each has been handed over. See
	// java_class.go and java_link.go.
	javaClasses *javaClassList
	javaLink    *javaLink
	// javaRun is the object model those two are answered into: the classes this
	// platform has laid out, and the objects it has issued. See java_runtime.go.
	javaRun *javaRuntime
	// javaTry is the stack of open try regions and javaTryBuffers the buffers
	// they are given, one per depth and reused. javaCallDepth is how many nested
	// guest calls the platform is inside, which is what says whether a region is
	// one a throw may jump to. See java_throw.go.
	javaTry        []javaTryFrame
	javaTryBuffers []uint32
	javaCallDepth  int

	// activeJavaWorker is the guest thread whose slice is running, when one is.
	// A `sleep` reaches for it to know whether there is anything to park: the
	// same call from the platform's own thread has nothing. See java_thread.go.
	activeJavaWorker   *javaWorker
	javaThreadsStopped bool

	// applicationIDAddress is the archive's AID as a guest string, allocated
	// on first use. A title asks for it once per boot, but a fresh copy per
	// call would leak, and the caller keeps no ownership of what it gets.
	applicationIDAddress uint32

	// resourceIDs is the id MC_knlGetResourceID has already answered for a
	// name. The id has to be *the same pointer* every time the same name is
	// asked for, because a title uses it as the resource's identity: one here
	// reads its whole resource list at boot, keeps `{id, size}` beside each
	// name, and later looks a resource up by asking for its id again and
	// searching that table for it. A fresh copy per call makes every one of
	// those searches miss. See handleResource in wipic_file.go.
	resourceIDs map[string]uint32

	// trace records recent platform calls when a caller asked for one. It is
	// nil otherwise, which is what keeps an untraced run free of the cost.
	trace *svcTrace

	// imports is every (category, slot) the module has resolved. It is the
	// only list of what a title links against that exists anywhere. See
	// imports.go.
	imports map[[2]uint32]bool
}

// pendingEvent is one Clet event waiting to be delivered.
type pendingEvent struct {
	kind   uint32
	param1 uint32
	param2 uint32
}

// Options configures a load.
type Options struct {
	Logger    *slog.Logger
	SaveStore backend.SaveStore
	// AudioSink is where the media block's sounds go. Nil is silent.
	AudioSink backend.AudioSink
	// Width and Height are the LCD size the game is told about.
	Width  int
	Height int
	// MaxSteps bounds one execution window; the limit hook renews it while a
	// Host service call is in progress, exactly as KTF's does.
	MaxSteps uint64
	// TraceSVC keeps the most recent platform calls for SVCTrace to report.
	// Zero disables the trace. It is a count rather than a flag because a
	// startup makes far more calls than a fault is worth reading back.
	TraceSVC int
	// TraceLive writes calls whose rendered line contains this text to
	// TraceOut as they are serviced. The ring answers "what happened just
	// before this fault"; this answers "what did the title ever do with its
	// save", which the ring cannot, because the interesting call is a hundred
	// thousand calls back by the time anything looks wrong. An empty string
	// matches everything, so it is only read when TraceOut is set.
	TraceLive string
	TraceOut  io.Writer
}

const (
	defaultWidth  = 240
	defaultHeight = 320
)

// Load parses the module, maps it, and prepares the platform regions. It does
// not run anything: Start does.
func Load(archive *Archive, options Options) (*Client, error) {
	if archive == nil {
		return nil, fmt.Errorf("LGT archive is nil")
	}
	module, err := ParseModule(archive.Module)
	if err != nil {
		return nil, err
	}
	low, high := module.Span()
	if high == 0 {
		return nil, fmt.Errorf("LGT module maps nothing")
	}
	if uint64(high) > uint64(heapBase) {
		return nil, fmt.Errorf("LGT module reaches %#x, which overlaps the platform heap at %#x", high, heapBase)
	}

	width, height := options.Width, options.Height
	if width <= 0 || height <= 0 {
		width, height = defaultWidth, defaultHeight
	}

	// A Clet runs long stretches of guest code between platform calls, so the
	// quantum — how many instructions the engine retires before yielding to
	// the Go scheduler — is a cost this platform pays far more of than KTF
	// does. Raising it is worth 4 to 7 percent per instruction retired across
	// the local titles, measured with the instruction count held fixed, and it
	// is not free everywhere: the same change measured on KTF is 3 to 12
	// percent *slower*, which is why it lives here rather than in the default.
	// See docs/armcore.md.
	core := armcore.NewCore(armcore.CoreOptions{MaxSteps: options.MaxSteps, Quantum: cletQuantum})
	memory := core.Memory()

	// One mapping spans the module: ELF sections are page-adjacent and a
	// per-section mapping would leave the gaps between them unmapped, which
	// a module that addresses its own padding would then fault on.
	moduleBase := low &^ 0xfff
	moduleSize := (uint64(high) - uint64(moduleBase) + 0xfff) &^ 0xfff
	if err := memory.Map(moduleBase, moduleSize, armcore.PermissionReadWriteExecute); err != nil {
		return nil, fmt.Errorf("map LGT module: %w", err)
	}
	for _, section := range module.Sections {
		if section.Data == nil {
			// .bss is already zero from the mapping.
			continue
		}
		if err := memory.Load(section.Address, section.Data); err != nil {
			return nil, fmt.Errorf("load LGT section %q at %#x: %w", section.Name, section.Address, err)
		}
	}
	for _, region := range []struct {
		base uint32
		size uint64
		perm armcore.Permission
		what string
	}{
		{heapBase, heapSize, armcore.PermissionReadWrite, "heap"},
		{platformDataBase, platformDataSize, armcore.PermissionReadWrite, "platform data"},
		{platformCodeBase, platformCodeSize, armcore.PermissionReadWriteExecute, "platform code"},
		{stackBase, stackSize, armcore.PermissionReadWrite, "stack"},
	} {
		if err := memory.Map(region.base, region.size, region.perm); err != nil {
			return nil, fmt.Errorf("map LGT %s: %w", region.what, err)
		}
	}

	initial := armcore.NewContext()
	initial.Registers[armcore.RegisterSP] = stackBase + uint32(stackSize)
	client := &Client{
		core:         core,
		thread:       armcore.NewThread(initial),
		archive:      archive,
		module:       module,
		logger:       options.Logger,
		saveStore:    options.SaveStore,
		arena:        newArena(platformDataBase, platformDataSize),
		heap:         newArena(heapBase, heapSize),
		codeCurse:    platformCodeBase,
		stubs:        make(map[uint64]uint32),
		framebuffers: make(map[uint32]*framebuffer),
		timers:       make(map[uint32]*timer),
		nextHandle:   1,
		clock:        newGuestClock(core.Steps),
		files:        make(map[uint32]*openFile),
		clips:        make(map[uint32]*mediaClip),
		// A nil sink is allowed and makes every sound silent, which is what a
		// Host without an audio device wants; the game still runs its whole
		// sound path.
		audio:  backend.NewAudio(options.AudioSink),
		volume: mediaMaxVolume,
	}
	if options.TraceSVC > 0 {
		client.trace = newSVCTrace(options.TraceSVC)
	}
	if options.TraceOut != nil {
		client.traceLive = options.TraceLive
		client.traceOut = options.TraceOut
		// Streaming needs a call to stream; a caller that asked only for the
		// live trace gets the smallest ring that makes one.
		if client.trace == nil {
			client.trace = newSVCTrace(1)
		}
	}
	client.screen = &framebuffer{
		width:  width,
		height: height,
		pixels: make([]uint16, width*height),
		// The LCD is the one surface a destroy has to refuse. It shares a
		// handle space with the offscreen buffers and the images, so this flag
		// is the only thing that tells it apart from them.
		screen: true,
	}
	client.frameRGBA = make([]byte, width*height*4)
	for index := 3; index < len(client.frameRGBA); index += 4 {
		client.frameRGBA[index] = 0xff
	}
	return client, nil
}

// Descriptor is the loaded game's identity.
func (client *Client) Descriptor() Descriptor { return client.archive.Descriptor }

// Start runs the module's entry point and then its initializer, which is what
// registers the Clet. After Start the game's own entry points are known.
func (client *Client) Start(ctx context.Context) error {
	// The module is handed two blocks. The first is scratch it fills in with
	// a pointer to its init struct; the second carries the two callbacks it
	// uses to resolve every platform function it needs.
	paramOne, err := client.allocate(uint64(initParamOneSize))
	if err != nil {
		return err
	}
	getTable, err := client.stub(svcCategoryInit, initSVCGetImportTable)
	if err != nil {
		return err
	}
	getFunction, err := client.stub(svcCategoryInit, initSVCGetImportFunction)
	if err != nil {
		return err
	}
	paramTwo, err := client.allocateWords([]uint32{getTable, getFunction, 0, 0})
	if err != nil {
		return err
	}

	// LGT modules are Thumb, and their ELF header does not say so: every real
	// archive here carries an even e_entry pointing at Thumb code. Taking the
	// low bit at face value runs that code in ARM mode, where the halfword
	// pairs decode as conditional instructions that mostly get skipped — so it
	// does not fault at the entry but a few instructions later, on whichever
	// one the flags happened to let through. Entering in Thumb is what the
	// toolchain meant and what the original runtime did.
	entry := client.module.Entry | 1
	if _, err := client.call(ctx, entry, []uint32{paramOne, paramTwo, 0}); err != nil {
		return fmt.Errorf("run LGT module entry point at %#x: %w", entry, err)
	}

	initStructPointer, err := client.readWord(paramOne + initParamOneStructOffset)
	if err != nil {
		return fmt.Errorf("read LGT init struct pointer: %w", err)
	}
	if initStructPointer == 0 {
		return fmt.Errorf("LGT module entry point left no init struct")
	}
	// InitStruct is { unk1, fn_init, ptr_str_init }.
	initFunction, err := client.readWord(initStructPointer + 4)
	if err != nil {
		return fmt.Errorf("read LGT init function: %w", err)
	}
	if initFunction == 0 {
		return fmt.Errorf("LGT init struct names no initializer")
	}
	if _, err := client.call(ctx, initFunction, nil); err != nil {
		return client.asJavaFailure(fmt.Errorf("run LGT initializer at %#x: %w", initFunction, err))
	}
	if client.clet.Start == 0 && !client.javaApplication {
		return fmt.Errorf("LGT initializer registered no Clet")
	}
	// A Java title registers nothing: its initializer *is* its run — the
	// launcher enters the application inside it and comes back once the game's
	// own thread is started. From here on the platform drives the card it
	// pushed rather than a Clet's entry points.
	return nil
}

const (
	// initParamOneSize is the scratch block the module writes into. Its shape
	// is 512 + 20 bytes of module-private space followed by the init struct
	// pointer.
	initParamOneSize         = 512 + 20 + 4
	initParamOneStructOffset = 512 + 20
)

// StartClet calls the game's startClet entry point, which is where it does
// its own setup and asks for its first paint.
func (client *Client) StartClet(ctx context.Context) error {
	if client.clet.Start == 0 && client.javaApplication {
		return nil
	}
	return client.callClet(ctx, "startClet", client.clet.Start, nil)
}

// PaintClet asks the game to draw.
func (client *Client) PaintClet(ctx context.Context) error {
	if client.clet.Paint == 0 {
		return nil
	}
	return client.callClet(ctx, "paintClet", client.clet.Paint, nil)
}

// PauseClet and ResumeClet are the lifecycle a Host uses when the page is
// hidden.
func (client *Client) PauseClet(ctx context.Context) error {
	if client.clet.Pause == 0 {
		return nil
	}
	return client.callClet(ctx, "pauseClet", client.clet.Pause, nil)
}

func (client *Client) ResumeClet(ctx context.Context) error {
	if client.clet.Resume == 0 {
		return nil
	}
	return client.callClet(ctx, "resumeClet", client.clet.Resume, nil)
}

// DestroyClet ends the game.
func (client *Client) DestroyClet(ctx context.Context) error {
	if client.clet.Destroy == 0 {
		return nil
	}
	return client.callClet(ctx, "destroyClet", client.clet.Destroy, nil)
}

// SendEvent queues one event for the game's handleCletEvent. Events are
// queued rather than delivered inline because a Host may post one while the
// guest is already running.
func (client *Client) SendEvent(kind, param1, param2 uint32) {
	client.mu.Lock()
	if len(client.events) < maxQueuedEvents {
		client.events = append(client.events, pendingEvent{kind: kind, param1: param1, param2: param2})
	}
	client.mu.Unlock()
}

const maxQueuedEvents = 256

// deliverEvents drains the event queue into the game.
func (client *Client) deliverEvents(ctx context.Context) error {
	for {
		client.mu.Lock()
		// A Java title's events go to the card it pushed rather than to a Clet
		// handler it does not have, so the queue drains for it too.
		java := client.javaRun != nil && client.javaRun.card != 0
		if len(client.events) == 0 || (client.clet.HandleEvent == 0 && !java) {
			client.mu.Unlock()
			return nil
		}
		event := client.events[0]
		client.events = client.events[1:]
		handler := client.clet.HandleEvent
		client.mu.Unlock()
		if java {
			switch event.kind {
			case EventKeyPressed, EventKeyReleased:
				if err := client.deliverJavaKey(ctx, event.kind == EventKeyPressed, event.param1); err != nil {
					return err
				}
			}
			continue
		}
		if client.logger != nil {
			client.logger.Debug("LGT clet event delivered",
				"kind", event.kind, "param1", int32(event.param1), "param2", event.param2)
		}
		if err := client.callClet(ctx, "handleCletEvent", handler,
			[]uint32{event.kind, event.param1, event.param2}); err != nil {
			return err
		}
	}
}

func (client *Client) callClet(ctx context.Context, name string, address uint32, arguments []uint32) error {
	if address == 0 {
		return fmt.Errorf("LGT %s is not registered", name)
	}
	if _, err := client.call(ctx, address, arguments); err != nil {
		return fmt.Errorf("run LGT %s at %#x: %w", name, address, err)
	}
	return nil
}

// call runs one guest function to completion, on the platform's own thread.
func (client *Client) call(ctx context.Context, address uint32, arguments []uint32) (uint32, error) {
	return client.callOn(ctx, client.thread, address, arguments)
}

// callOn runs one guest function to completion below a thread's current frame.
//
// **Which thread matters.** A call made while the guest is inside a platform
// call — a callback the module handed over, a class initialiser, a method the
// launcher enters — has to be made on the thread that is running, because that
// is the only one whose stack pointer is where the guest left it. Making it on
// the platform's own thread instead runs the callee from the top of the stack,
// over the frame of the function that is waiting for it: the caller's locals
// come back changed, and the failure surfaces as an argument that was correct
// when it was written and is not when it is read.
func (client *Client) callOn(
	ctx context.Context, thread *armcore.Thread, address uint32, arguments []uint32,
) (uint32, error) {
	if client.exited {
		return 0, ErrGuestExited
	}
	// A try region belongs to the call that opened it: see dropJavaTryFrames.
	client.javaCallDepth++
	defer func() {
		client.javaCallDepth--
		client.dropJavaTryFrames(client.javaCallDepth)
	}()
	summary, err := client.core.Call(ctx, thread, address, returnAddress, arguments, client.handleSupervisorCall)
	if err != nil {
		if client.exited {
			return 0, ErrGuestExited
		}
		return 0, err
	}
	return summary.Context.Registers[0], nil
}

// handleSupervisorCall routes one SVC to the table its category names.
func (client *Client) handleSupervisorCall(ctx context.Context, thread *armcore.Thread, call armcore.SupervisorCall) error {
	// The stub loads its slot number into r12 before trapping, so the SVC
	// immediate only has to name the table.
	slot, err := thread.Register(12)
	if err != nil {
		return fmt.Errorf("read LGT SVC slot: %w", err)
	}
	// The slot is named on the way out as well as on the way in: a slot that
	// fails part way through reports a fault at the stub's address, which says
	// nothing about which platform function was running.
	traced := client.traceEntry(thread, call.Immediate, slot)
	switch call.Immediate {
	case svcCategoryInit:
		err := client.handleInitSVC(ctx, thread, slot)
		client.traceExit(thread, traced, err)
		return err
	case svcCategoryWIPIC:
		err := client.handleWIPICSVC(ctx, thread, slot)
		client.traceExit(thread, traced, err)
		if err != nil {
			return wrapSlotError("WIPI C", slot, err)
		}
		return nil
	case svcCategoryStdlib:
		err := client.handleStdlibSVC(ctx, thread, slot)
		client.traceExit(thread, traced, err)
		if err != nil {
			return wrapSlotError("stdlib", slot, err)
		}
		return nil
	case svcCategoryOEM:
		err := client.handleOEMSVC(ctx, thread, slot)
		client.traceExit(thread, traced, err)
		if err != nil {
			return wrapSlotError("OEM", slot, err)
		}
		return nil
	case svcCategoryJava:
		err := client.handleJavaSVC(ctx, thread, slot)
		client.traceExit(thread, traced, err)
		if err != nil {
			return wrapSlotError("java", slot, err)
		}
		return nil
	}
	return fmt.Errorf("unknown LGT SVC category %d (slot %#x)", call.Immediate, slot)
}

// wrapSlotError names the slot a failure came from, unless the failure is the
// guest asking to exit or the slot already naming itself.
func wrapSlotError(table string, slot uint32, err error) error {
	if errors.Is(err, ErrGuestExited) {
		return err
	}
	return fmt.Errorf("LGT %s slot %#x: %w", table, slot, err)
}

func (client *Client) handleInitSVC(ctx context.Context, thread *armcore.Thread, slot uint32) error {
	switch slot {
	case initSVCGetImportTable:
		// The module asks the platform to acknowledge a table id; the id
		// itself is the handle it then passes back.
		table, err := thread.Register(0)
		if err != nil {
			return err
		}
		return thread.SetRegister(0, table)
	case initSVCGetImportFunction:
		table, err := thread.Register(0)
		if err != nil {
			return err
		}
		index, err := thread.Register(1)
		if err != nil {
			return err
		}
		address, err := client.importFunction(table, index)
		if err != nil {
			return err
		}
		return thread.SetRegister(0, address)
	}
	return fmt.Errorf("unknown LGT init SVC slot %#x", slot)
}

// importFunction resolves one platform function for the module. An unknown
// table is an error rather than a null pointer: a module handed zero branches
// to zero, and the failure then surfaces far from its cause.
func (client *Client) importFunction(table, index uint32) (uint32, error) {
	switch table {
	case importTableWIPIC:
		// A slot this platform does not implement still gets a stub: the
		// module resolves everything it might use at startup, and refusing
		// here would stop a game over a function it never calls. The stub
		// reports the gap when it is actually reached.
		if !knownWIPICSlot(index) && !unknownSlotAccepted(index) && client.logger != nil {
			client.logger.Debug("LGT unimplemented WIPI C slot resolved", "slot", index)
		}
		client.recordImport(svcCategoryWIPIC, index)
		return client.stub(svcCategoryWIPIC, index)
	case importTableStdlib:
		client.recordImport(svcCategoryStdlib, index)
		return client.stub(svcCategoryStdlib, index)
	case importTableOEM:
		if index == oemJavaFunction {
			client.recordImport(svcCategoryJava, javaAuxiliarySlot(table, index))
			return client.stub(svcCategoryJava, javaAuxiliarySlot(table, index))
		}
		if !knownOEMSlot(index) && client.logger != nil {
			client.logger.Debug("LGT unknown OEM slot resolved", "slot", index)
		}
		client.recordImport(svcCategoryOEM, index)
		return client.stub(svcCategoryOEM, index)
	case importTableJava:
		// LGT Java apps are AOT-compiled to native ARM and hand the runtime
		// their class metadata through this table. Running one is not
		// implemented — see docs/lgt.md — but the table resolves, so the
		// title reaches the call that would hand the metadata over and this
		// platform can record what it was going to say.
		client.recordImport(svcCategoryJava, index)
		return client.javaInterfaceFunction(index)
	}
	if javaAuxiliaryTables[table] {
		client.recordImport(svcCategoryJava, javaAuxiliarySlot(table, index))
		return client.stub(svcCategoryJava, javaAuxiliarySlot(table, index))
	}
	return 0, fmt.Errorf("unknown LGT import table %#x (function %#x)", table, index)
}

// stub builds, and caches, a Thumb trampoline that raises an SVC carrying the
// slot number. It is the same shape KTF uses: push a scratch register, load
// the slot word that follows the code, svc, return.
func (client *Client) stub(category, slot uint32) (uint32, error) {
	if category > 0xff {
		return 0, fmt.Errorf("LGT SVC category %d exceeds the Thumb immediate range", category)
	}
	key := uint64(category)<<32 | uint64(slot)
	client.mu.Lock()
	defer client.mu.Unlock()
	if address := client.stubs[key]; address != 0 {
		return address, nil
	}
	const stubSize = uint32(16)
	if uint64(client.codeCurse)+uint64(stubSize) > uint64(platformCodeBase)+platformCodeSize {
		return 0, fmt.Errorf("LGT platform stub space exhausted")
	}
	address := client.codeCurse
	stub := []byte{
		0x10, 0xb4, // push {r4}
		0x02, 0x4c, // ldr  r4, [pc, #8]
		0xa4, 0x46, // mov  ip, r4
		0x10, 0xbc, // pop  {r4}
		byte(category), 0xdf, // svc #category
		0x70, 0x47, // bx   lr
		byte(slot), byte(slot >> 8), byte(slot >> 16), byte(slot >> 24),
	}
	if err := client.core.Memory().Load(address, stub); err != nil {
		return 0, fmt.Errorf("load LGT platform stub: %w", err)
	}
	client.codeCurse += stubSize
	client.stubs[key] = address | 1
	return address | 1, nil
}

// --- memory helpers ---

func (client *Client) allocate(size uint64) (uint32, error) {
	if size > maxPlatformAllocation {
		return 0, fmt.Errorf("LGT platform allocation %d exceeds %d bytes", size, maxPlatformAllocation)
	}
	address, ok := client.arena.allocate(size)
	if !ok {
		return 0, fmt.Errorf("LGT platform data space exhausted")
	}
	return address, nil
}

// allocateBytes copies bytes into the platform data region and answers the
// address, for the handles a slot has to hand the guest.
func (client *Client) allocateBytes(data []byte) (uint32, error) {
	address, err := client.allocate(uint64(len(data)))
	if err != nil {
		return 0, err
	}
	if err := client.core.Memory().Write(address, data); err != nil {
		return 0, fmt.Errorf("write LGT platform data at %#x: %w", address, err)
	}
	return address, nil
}

func (client *Client) allocateWords(words []uint32) (uint32, error) {
	data := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	address, err := client.allocate(uint64(len(data)))
	if err != nil {
		return 0, err
	}
	if err := client.core.Memory().Write(address, data); err != nil {
		return 0, fmt.Errorf("write LGT platform data at %#x: %w", address, err)
	}
	return address, nil
}

func (client *Client) readWord(address uint32) (uint32, error) {
	var word [4]byte
	if err := client.core.Memory().Read(address, word[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(word[:]), nil
}

func (client *Client) readHalfword(address uint32) (uint16, error) {
	var half [2]byte
	if err := client.core.Memory().Read(address, half[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(half[:]), nil
}

func (client *Client) writeWord(address, value uint32) error {
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	return client.core.Memory().Write(address, word[:])
}

// readCString reads a NUL-terminated guest string with a bound, because a
// pointer into uninitialized memory would otherwise read to the end of the
// mapping.
func (client *Client) readCString(address uint32) (string, error) {
	if address == 0 {
		return "", nil
	}
	const limit = 4096
	buffer := make([]byte, 0, 64)
	one := make([]byte, 1)
	for offset := uint32(0); offset < limit; offset++ {
		if err := client.core.Memory().Read(address+offset, one); err != nil {
			return "", err
		}
		if one[0] == 0 {
			return string(buffer), nil
		}
		buffer = append(buffer, one[0])
	}
	return "", fmt.Errorf("LGT string at %#x is not terminated within %d bytes", address, limit)
}

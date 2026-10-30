package clet

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// A Clet is a WIPI-C program with no Java around it: the platform calls its
// entry points, and the program reaches the platform through a root table of
// interface tables whose address the loader writes into the program's own
// data. SKT's native packages carry one (docs/skt-native.md).
//
// It runs on this runtime rather than beside it because the functions behind
// those tables are the ones this runtime already serves the KTF titles that
// call into C: allocations are the same indirect handles, a framebuffer is the
// same record, the graphics context is the same 52 bytes, and nearly every
// function sits at the same number. What differs is small and lives in this
// file and clet_tables.go: the order of the tables in the root, the few
// function numbers that moved, the six entry points that replace a Jlet, and
// handleCletEvent, which replaces the card stack as the place events go.
//
// Every branch on client.clet is taken only by a client LoadCletClient built;
// a KTF client never sets it, so none of this reaches a KTF title.

// CletProgram is a WIPI-C program laid out for loading. The code is placed at
// ImageBase; every address in it is absolute and already relocated for that.
type CletProgram struct {
	// Name identifies the executable in diagnostics and in a checkpoint.
	Name string
	// ProgramName is what MC_knlGetProgramName answers.
	ProgramName string
	// Identity is the save identity of the package the program came from
	// (backend.SaveIdentity), which a checkpoint is bound to.
	Identity [32]byte
	// SaveOwner names the save directory under a SaveRoot.
	SaveOwner string
	// Code is the code with its read-only data, placed at ImageBase.
	Code []byte
	// Data is placed at DataAddress and followed by BSSSize zero bytes.
	Data        []byte
	DataAddress uint32
	BSSSize     uint32
	// RootSlot is the data word the root table's address is written to.
	RootSlot uint32
	Entries  CletEntries
	// Resources answer MC_knlGetResourceID and MC_knlGetResource; Files are
	// the program's own files, which its storage held on the handset.
	Resources map[string][]byte
	Files     map[string][]byte
	// Authentication is the compatibility the loader applied to the code,
	// which the session reports as its authentication status.
	Authentication backend.AuthenticationStatus
}

// CletEntries are the program's entry points, absolute Thumb addresses. Zero
// is an entry the program does not have; only Start is required.
type CletEntries struct {
	Start, Pause, Resume, Destroy, Paint, Event uint32
}

// Clet event types, as handleCletEvent receives them. A key arrives one past
// the type a Java card's keyNotify gets — press 2 and release 3 — which is
// what both local titles compare against: one dispatches 2 and 3 to its press
// and release handlers, the other files them as its own key events 0 and 1.
const (
	CletEventKeyPressed  int32 = 2
	CletEventKeyReleased int32 = 3
)

// cletState is what a client running a Clet keeps beside the runtime.
type cletState struct {
	program CletProgram
	// root is the root table the loader wrote into RootSlot.
	root uint32
	// destroyStarted makes the destroy entry one-way, as destroyApp is for a
	// Jlet: an exit or a failure inside it must not run it twice.
	destroyStarted bool
	// repaint is set by MC_grpRepaint, and the next round calls the paint
	// entry for it.
	repaint bool
	// timeRecord is the struct tm localtime and gmtime answer in, built on
	// first use; tokenNext is where strtok continues. See clet_clib.go.
	timeRecord uint32
	tokenNext  uint32
}

const (
	// cletRootSlots is how many tables the root holds. The SKT layout's last
	// is the C library at 14; see clet_tables.go.
	cletRootSlots = 15
	// cletTableFunctions bounds each table. The largest, graphics, is 39.
	cletTableFunctions = 64
	// maxCletDataBSS bounds the program's data and BSS the way a KTF image
	// and its BSS are bounded.
	maxCletDataBSS = 64 << 20
)

// LoadCletClient maps a Clet into a fresh core: the code at ImageBase, the data
// and its zero-filled BSS at their own address, and the client thread's stack.
// It runs no guest code.
func LoadCletClient(program CletProgram, options armcore.CoreOptions) (*Client, error) {
	if err := program.validate(); err != nil {
		return nil, err
	}
	core := armcore.NewCore(options)
	memory := core.Memory()
	if err := memory.Map(ImageBase, uint64(len(program.Code)), armcore.PermissionReadWriteExecute); err != nil {
		return nil, fmt.Errorf("map Clet %q code: %w", program.Name, err)
	}
	if err := memory.Load(ImageBase, program.Code); err != nil {
		return nil, fmt.Errorf("load Clet %q code: %w", program.Name, err)
	}
	if err := memory.Map(program.DataAddress, uint64(len(program.Data))+uint64(program.BSSSize), armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map Clet %q data: %w", program.Name, err)
	}
	if err := memory.Load(program.DataAddress, program.Data); err != nil {
		return nil, fmt.Errorf("load Clet %q data: %w", program.Name, err)
	}
	if err := memory.Map(ThreadStackBase, ThreadStackSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map Clet stack: %w", err)
	}
	initial := armcore.NewContext()
	initial.Registers[armcore.RegisterSP] = ThreadStackBase + uint32(ThreadStackSize)
	client := &Client{
		subscriberNumber: HandsetNumber(),
		core:             core,
		thread:           armcore.NewThread(initial),
		// The image a KTF client is told about is the code alone: it is what
		// a pixel operation's function pointer has to lie in, what the cheat
		// panel labels as the client, and what a checkpoint is bound to.
		image:  ClientImage{Name: program.Name, Data: program.Code},
		mapped: uint64(len(program.Code)),
		entry:  program.Entries.Start,
		clet:   &cletState{program: program},
	}
	return client, nil
}

func (program CletProgram) validate() error {
	if program.Name == "" {
		return fmt.Errorf("Clet has no name")
	}
	if len(program.Code) == 0 {
		return fmt.Errorf("Clet %q has no code", program.Name)
	}
	codeEnd := uint64(ImageBase) + uint64(len(program.Code))
	dataEnd := uint64(program.DataAddress) + uint64(len(program.Data)) + uint64(program.BSSSize)
	if uint64(program.DataAddress) < codeEnd || len(program.Data)+int(program.BSSSize) == 0 {
		return fmt.Errorf("Clet %q data at %#x overlaps its code or is empty", program.Name, program.DataAddress)
	}
	if uint64(len(program.Data))+uint64(program.BSSSize) > maxCletDataBSS || dataEnd > uint64(ThreadStackBase) {
		return fmt.Errorf("Clet %q data and BSS of %d bytes at %#x do not fit below the stack", program.Name, uint64(len(program.Data))+uint64(program.BSSSize), program.DataAddress)
	}
	if program.RootSlot%4 != 0 || program.RootSlot < program.DataAddress || uint64(program.RootSlot)+4 > uint64(program.DataAddress)+uint64(len(program.Data)) {
		return fmt.Errorf("Clet %q root slot %#x is not a data word", program.Name, program.RootSlot)
	}
	entries := []uint32{program.Entries.Start, program.Entries.Pause, program.Entries.Resume, program.Entries.Destroy, program.Entries.Paint, program.Entries.Event}
	for _, entry := range entries {
		if entry == 0 {
			continue
		}
		if entry&1 == 0 || uint64(entry) < uint64(ImageBase) || uint64(entry) >= codeEnd {
			return fmt.Errorf("Clet %q entry %#x is not a Thumb address in its code", program.Name, entry)
		}
	}
	if program.Entries.Start == 0 {
		return fmt.Errorf("Clet %q has no start entry", program.Name)
	}
	return nil
}

// IsClet reports whether the client runs a Clet rather than a KTF image.
func (client *Client) IsClet() bool { return client != nil && client.clet != nil }

// SupportsCheckpoint reports whether this session has a quick-save codec. A
// Clet does not: the checkpoint codec captures a KTF Java or module client's
// state (its AOT metadata and heap), which a Clet has none of, and rebuilds it
// from the descriptor archive a Clet does not carry. A Host asks this before
// offering quick save, so a Clet is not offered one rather than being offered
// one its capture then refuses. See docs/skt-native.md.
func (session *Session) SupportsCheckpoint() bool {
	return session != nil && session.Client != nil && !session.Client.IsClet()
}

// newCletSessionClient attaches the Host boundaries a session needs, the way
// newSessionClient does for a KTF archive, without running guest code.
func newCletSessionClient(program CletProgram, options SessionOptions) (*Client, error) {
	client, err := LoadCletClient(program, sessionCoreOptions(options))
	if err != nil {
		return nil, err
	}
	if options.Debug != nil {
		options.Debug(client.core)
	}
	client.clock = options.Clock
	if client.clock == nil {
		client.clock = wallClock{}
	}
	client.SetSpeed(options.Speed)
	if options.Width > 0 && options.Height > 0 {
		client.SetScreen(options.Width, options.Height)
	}
	client.SetDiagnostics(options.TraceLimit, options.Logger)
	client.audio = backend.NewAudioWithClock(options.AudioSink, client.now)
	client.audio.SetLogger(options.Logger)
	_ = client.audio.SetPlaybackRate(0, options.Speed)
	client.frameSink = options.FrameSink
	client.threadSliceSteps = options.ThreadSliceSteps
	client.serviceSteps, client.serviceWait = options.ServiceSteps, options.ServiceWait
	client.SetProgramName(program.ProgramName)
	client.authentication = program.Authentication
	client.AttachResources(program.Resources)
	client.AttachFilesystem(program.Files)
	if options.SaveStore != nil {
		client.AttachSaveStore(options.SaveStore)
	} else if options.SaveRoot != "" && program.SaveOwner != "" {
		client.AttachSaveStore(NewDirectorySaveStore(filepath.Join(options.SaveRoot, program.SaveOwner)))
	}
	return client, nil
}

// prepareClet builds the runtime a Clet calls into and hands the program its
// root table. Nothing of a KTF image's start-up applies: there is no Java
// context to build, no interface to initialize, and no C routine in the image
// is hooked, because the patterns those hooks match are another compiler's.
func (client *Client) prepareClet() error {
	if client.prepared != nil {
		return nil
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		return err
	}
	root, err := runtime.makeCletRoot()
	if err != nil {
		return err
	}
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], root)
	if err := client.core.Memory().Write(client.clet.program.RootSlot, word[:]); err != nil {
		return fmt.Errorf("write Clet root table: %w", err)
	}
	client.clet.root = root
	client.prepared, client.runtime, client.initializationStarted = runtime, runtime, true
	return nil
}

// makeCletRoot builds the root table: one table of stubs per slot, each stub
// naming its slot and function so handleCletCall can route it. A slot no local
// title calls still gets a table, so reaching one fails with the slot and the
// caller named rather than with a fault at an address near zero.
func (runtime *initializationRuntime) makeCletRoot() (uint32, error) {
	roots := make([]uint32, cletRootSlots)
	for slot := range roots {
		entries := make([]uint32, cletTableFunctions)
		for function := range entries {
			stub, err := runtime.stub(svcCategoryClet, uint32(slot)<<16|uint32(function))
			if err != nil {
				return 0, err
			}
			entries[function] = stub
		}
		table, err := runtime.allocateWords(entries)
		if err != nil {
			return 0, err
		}
		roots[slot] = table
	}
	return runtime.allocateWords(roots)
}

// callClet runs one entry on the client thread under a Host service scope,
// which is the allowance every Host call into the guest has. Arguments past
// the fourth are not needed by any entry.
func (client *Client) callClet(ctx context.Context, entry uint32, arguments ...uint32) error {
	if entry == 0 {
		return nil
	}
	runtime := client.runtime
	defer client.beginHostService(ctx)()
	previousThread, previousContext := runtime.currentThread, runtime.currentContext
	runtime.currentThread, runtime.currentContext = client.thread, ctx
	defer func() {
		runtime.currentThread, runtime.currentContext = previousThread, previousContext
	}()
	run, err := client.core.Call(ctx, client.thread, entry, ReturnAddress, arguments, runtime.handleSupervisorCall)
	if err != nil {
		if fault, isFault := guestFault(err); isFault {
			if report := runtime.describeGuestFault(run.Context, fault); report != "" {
				runtime.countDiagnostic("guest fault " + report)
				return fmt.Errorf("Clet entry %#x: %w: %s", entry, err, report)
			}
		}
		return fmt.Errorf("Clet entry %#x: %w", entry, err)
	}
	return nil
}

// startClet hands the program its root table and runs its start entry, which
// is where a title loads what it needs and arms the timer its frames come
// from. The start entry takes the argument count and vector a launcher gives;
// nothing launches these with arguments.
func (client *Client) startClet(ctx context.Context) error {
	if err := client.prepareClet(); err != nil {
		return err
	}
	client.run.Lock()
	defer client.run.Unlock()
	return client.callClet(ctx, client.clet.program.Entries.Start, 0, 0)
}

// cletLifecycle runs the pause, resume or destroy entry. A program without the
// entry has nothing to call. Destroy is one-way, like destroyApp for a Jlet.
func (client *Client) cletLifecycle(ctx context.Context, entry uint32, destroy bool) error {
	if client == nil || client.core == nil || client.thread == nil {
		return fmt.Errorf("KTF client is not initialized")
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil || client.workersStopped {
		return nil
	}
	if destroy {
		if client.clet.destroyStarted {
			return nil
		}
		client.clet.destroyStarted = true
	}
	return client.callClet(ctx, entry)
}

// sendCletEvent hands one event to handleCletEvent. The caller holds the run
// lock.
func (client *Client) sendCletEvent(ctx context.Context, kind, first, second int32) error {
	return client.callClet(ctx, client.clet.program.Entries.Event, uint32(kind), uint32(first), uint32(second))
}

// sendCletKey delivers a key the way a handset gives a Clet one: straight to
// handleCletEvent, press and release, and no repeat — neither local title
// reads one, and nothing says which type a repeat would arrive as.
func (client *Client) sendCletKey(ctx context.Context, eventType, key int32) error {
	var kind int32
	switch eventType {
	case KeyPressed:
		kind = CletEventKeyPressed
	case KeyReleased:
		kind = CletEventKeyReleased
	default:
		return nil
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil {
		return fmt.Errorf("KTF client initialization has not completed")
	}
	return client.sendCletEvent(ctx, kind, key, 0)
}

// serviceCletEvents delivers what MC_grpPostEvent queued, the events that were
// waiting when the round began, each to handleCletEvent with the type and the
// two parameters the program posted. A handler that posts again queues for the
// next round.
func (client *Client) serviceCletEvents(ctx context.Context) (int, error) {
	client.run.Lock()
	defer client.run.Unlock()
	runtime := client.runtime
	if runtime == nil || len(runtime.events) == 0 {
		return 0, nil
	}
	delivered := 0
	for waiting := len(runtime.events); waiting > 0; waiting-- {
		event, ok := runtime.nextGuestEvent()
		if !ok {
			break
		}
		delivered++
		if event.kind != eventKindNotify {
			continue
		}
		if err := client.sendCletEvent(ctx, event.param1, event.param2, event.param3); err != nil {
			return delivered, err
		}
	}
	return delivered, nil
}

// serviceCletPaint calls the paint entry when the program asked for a repaint.
// Neither local title draws there — both paint entries return at once and the
// frames come from their timers — but the entry is the specification's answer
// to MC_grpRepaint and a title that does draw there needs it.
func (client *Client) serviceCletPaint(ctx context.Context) (bool, error) {
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil || !client.clet.repaint {
		return false, nil
	}
	client.clet.repaint = false
	width, height := client.screenSize()
	if err := client.callClet(ctx, client.clet.program.Entries.Paint, 0, 0, uint32(width), uint32(height)); err != nil {
		return true, err
	}
	return true, nil
}

// tickClet is a service round for a Clet: the timers its frames come from,
// the events it posted itself, a repaint it asked for, and the audio and cheat
// work every round does. There are no guest threads, cards or Java objects to
// service.
func (session *Session) tickClet(ctx context.Context) (bool, error) {
	timerLimit := session.options.TimerLimit
	if timerLimit <= 0 {
		timerLimit = sessionDefaultTimerLimit
	}
	client := session.Client
	client.costs.Rounds++

	phase := client.phaseClock()
	ranTimers, err := client.ServiceTimers(ctx, timerLimit)
	client.sincePhase(phase, &client.costs.Timers)
	if err != nil {
		client.log("Clet timer service failed", "error", err)
		return ranTimers > 0, err
	}

	phase = client.phaseClock()
	ranEvents, err := client.serviceCletEvents(ctx)
	client.sincePhase(phase, &client.costs.Events)
	if err != nil {
		client.log("Clet event service failed", "error", err)
		return ranTimers+ranEvents > 0, err
	}

	phase = client.phaseClock()
	painted, err := client.serviceCletPaint(ctx)
	client.sincePhase(phase, &client.costs.Paint)
	if err != nil {
		client.log("Clet paint failed", "error", err)
		return true, err
	}

	phase = client.phaseClock()
	cheatErr := session.serviceCheat()
	client.sincePhase(phase, &client.costs.Cheat)
	if cheatErr != nil {
		return true, cheatErr
	}

	phase = client.phaseClock()
	audioErr := client.serviceAudio()
	client.sincePhase(phase, &client.costs.Audio)
	if audioErr != nil {
		return true, audioErr
	}
	return ranTimers > 0 || ranEvents > 0 || painted, nil
}

// StartCletSession loads a Clet and runs its start entry.
func StartCletSession(ctx context.Context, program CletProgram, options SessionOptions) (*Session, error) {
	client, err := newCletSessionClient(program, options)
	if err != nil {
		return nil, err
	}
	client.log("Clet session loading", "program", program.Name, "max_steps", client.core.MaxSteps())
	failed := func(err error) (*Session, error) {
		return nil, &StartFailure{Err: err, Diagnostics: (&Session{Client: client}).Diagnostics()}
	}
	if err := client.startClet(ctx); err != nil {
		client.log("Clet start failed", "error", err)
		if errors.Is(err, ErrGuestExited) {
			return nil, err
		}
		return failed(fmt.Errorf("start Clet %s: %w", program.Name, err))
	}
	client.log("Clet started", "program", program.Name)
	return &Session{Client: client, options: options, archiveIdentity: program.Identity}, nil
}

// handleCletSemihosting answers the one semihosting call a local title's C
// runtime carries, `svc #0xab` with r0 = 3, which writes one character to a
// debugger. A handset has no debugger attached; the character is dropped and
// the call answers as a semihosting host that took it would. Any other
// operation answers -1, the semihosting failure.
func (runtime *initializationRuntime) handleCletSemihosting(thread *armcore.Thread) (uint32, error) {
	operation, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	runtime.countDiagnostic(fmt.Sprintf("Clet semihosting operation %#x", operation))
	switch operation {
	case semihostingWriteCharacter, semihostingWriteString:
		return 0, nil
	default:
		return 0xffffffff, nil
	}
}

const (
	semihostingWriteCharacter = 3
	semihostingWriteString    = 4
)

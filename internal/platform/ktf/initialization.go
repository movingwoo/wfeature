package ktf

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/wipic"
)

const (
	platformDataBase uint32 = 0x30000000
	platformDataSize uint64 = 64 << 20
	// The stub arena sits above the data arena, and the gap between them is
	// deliberate. The allocator hands out every byte of the data arena, so a
	// stub arena inside that span is one long-running title away from being
	// allocated to the game and written over — and because both mappings are
	// in force at once, the write is permitted rather than faulted.
	platformCodeBase uint32 = 0x35000000
	platformCodeSize uint64 = 1 << 20

	// This expression does not compile if the stub arena is ever moved back
	// inside the data arena: an unsigned constant cannot go negative.
	_ = uint64(platformCodeBase) - (uint64(platformDataBase) + platformDataSize)

	maxPlatformAllocation uint64 = 4 << 20
	maxJavaStringUnits    uint32 = 1 << 16
	maxJavaArrayElements  uint32 = 1 << 20
	maxAOTCallDepth       uint32 = 64

	svcCategoryInit          uint32 = 1
	svcCategoryJavaInterface uint32 = 2
	svcCategoryWIPIC         uint32 = 3
	svcCategoryRuntimeJava   uint32 = 4
	// svcCategoryBinaryHook answers a C runtime routine recognized in the
	// guest image and replaced with a native one. See binary_hooks.go.
	svcCategoryBinaryHook uint32 = 5

	initSVCGetInterface  uint32 = 0
	initSVCJavaThrow     uint32 = 1
	initSVCJavaCheckType uint32 = 2
	initSVCJavaNew       uint32 = 3
	initSVCJavaArrayNew  uint32 = 4
	initSVCJavaClassLoad uint32 = 5
	initSVCAlloc         uint32 = 6

	javaSVCJump1          uint32 = 7
	javaSVCJump2          uint32 = 8
	javaSVCJump3          uint32 = 9
	javaSVCGetMethod      uint32 = 10
	javaSVCGetField       uint32 = 11
	javaSVCUnknown4       uint32 = 12
	javaSVCUnknown5       uint32 = 13
	javaSVCMonitorEnter   uint32 = 14
	javaSVCMonitorExit    uint32 = 15
	javaSVCRegisterClass  uint32 = 16
	javaSVCRegisterString uint32 = 17
	javaSVCCallNative     uint32 = 18

	wipicKernelAlloc          uint32 = 20
	wipicKernelCalloc         uint32 = 21
	wipicKernelFree           uint32 = 22
	wipicKernelGetTotalMemory uint32 = 23
	wipicKernelGetFreeMemory  uint32 = 24
	wipicKernelPrintk         uint32 = 0
	wipicKernelSprintk        uint32 = 1
	wipicKernelGetExecNames   uint32 = 2
	wipicKernelExit           uint32 = 7
	wipicKernelGetAccessLevel uint32 = 13
	wipicKernelGetProgramName uint32 = 14
	wipicKernelDefTimer       uint32 = 25
	wipicKernelSetTimer       uint32 = 26
	wipicKernelUnsetTimer     uint32 = 27
	wipicKernelCurrentTime    uint32 = 28
	wipicKernelGetSysProperty uint32 = 29
	wipicKernelGetResourceID  uint32 = 31
	wipicKernelGetResource    uint32 = 32
	wipicKernelReserved1      uint32 = 33
	// Slot 36 answers MC_knlGetDLLInterface: a handset library is asked for by
	// name and version, and the caller receives a table of function pointers.
	wipicKernelGetDLLInterface uint32 = 36

	// MC_knlAlloc returns an indirect handle: the first word points at the
	// handle's own base-plus-four record, the second keeps the payload size,
	// and payload data starts twelve bytes in.
	wipicAllocationOverhead uint32 = 12

	// wipicAccessLevel is what MC_knlGetAccessLevel reports: a bitmask of the
	// API groups this program is permitted to use.
	//
	// Every group is granted, because the question is what the handset permits
	// rather than what this platform then does about a particular call. Those
	// are not the same question, and answering the second one here cost a
	// title everything: it reads the mask during startup, requires `& 0xbc` to
	// equal `0xbc`, and stops at its own error screen when it does not. It
	// never reaches a network call to be refused at, so withholding the
	// network bit did not save it an attempt — it ended the game.
	//
	// This used to withhold the network and serial bits, on the reasoning that
	// a game which checks the bit before it dials skips the attempt instead of
	// handling a refusal. Nothing in the local library does that: granting
	// every group leaves every other title drawing the same frames and making
	// no network call it was not already making, measured title by title. The
	// refusal still happens where it always did, at the call.
	wipicAccessLevel uint32 = 0xff

	wipicErrorNotFound uint32 = 0xfffffff4 // M_E_NOENT (-12)
	wipicErrorInvalid  uint32 = 0xfffffff7 // M_E_INVALID (-9)
	wipicErrorShortBuf uint32 = 0xffffffee // M_E_SHORTBUF (-18)
	wipicErrorBadParam uint32 = 0xffffffea // M_E_BADRECID (-22)
	wipicErrorGeneric  uint32 = 0xffffffff // -1
)

// InitializationCallbacks summarizes the platform callbacks reached while a
// client performs its interface and WIPI initialization functions.
type InitializationCallbacks struct {
	Allocations       uint32
	RegisteredClasses uint32
	RegisteredStrings uint32
	LoadedClasses     uint32
	RuntimeJavaCalls  uint32
}

// InitializationSummary keeps each bounded guest call separate so Hosts can
// report which startup stage failed and how much work it retired.
type InitializationSummary struct {
	Interface armcore.RunSummary
	WIPI      armcore.RunSummary
	Callbacks InitializationCallbacks
}

// Initialize calls the validated interface and WIPI initialization functions.
// The current callback surface is intentionally limited to startup and normal
// AOT calls. Raw class metadata, native strings, and object/array allocations
// are bound to the shared Go JVM; native calls support normal returns and raw
// AOT handlers support low-level typed exception unwind.
func (client *Client) Initialize(ctx context.Context, executableAddress uint32) (InitializationSummary, error) {
	if client == nil || client.core == nil || client.thread == nil {
		return InitializationSummary{}, fmt.Errorf("KTF client is not initialized")
	}
	client.run.Lock()
	defer client.run.Unlock()
	if client.initializationStarted {
		return InitializationSummary{}, fmt.Errorf("KTF client initialization already started")
	}
	client.initializationStarted = true

	executable, err := client.readExecutable(executableAddress)
	if err != nil {
		return InitializationSummary{}, err
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		return InitializationSummary{}, err
	}
	parameters, err := runtime.prepare()
	if err != nil {
		return InitializationSummary{}, err
	}
	// The image is scanned as it now sits in guest memory rather than as it
	// arrived, so the entry's self-relocation is already accounted for and the
	// addresses hooked are the ones that will execute.
	if _, err := runtime.installImageHooks(); err != nil {
		return InitializationSummary{}, err
	}

	var summary InitializationSummary
	summary.Interface, err = client.core.Call(
		ctx,
		client.thread,
		executable.Interface.Functions.Init,
		ReturnAddress,
		parameters,
		runtime.handleSupervisorCall,
	)
	if err != nil {
		summary.Callbacks = runtime.callbacks
		return summary, fmt.Errorf("execute KTF interface init: %w", err)
	}
	if result := summary.Interface.Context.Registers[0]; result != 0 {
		summary.Callbacks = runtime.callbacks
		return summary, fmt.Errorf("KTF interface init returned %#x", result)
	}

	summary.WIPI, err = client.core.Call(
		ctx,
		client.thread,
		executable.Init,
		ReturnAddress,
		nil,
		runtime.handleSupervisorCall,
	)
	summary.Callbacks = runtime.callbacks
	if err != nil {
		return summary, fmt.Errorf("execute KTF WIPI init: %w", err)
	}
	if result := summary.WIPI.Context.Registers[0]; result != 0 {
		return summary, fmt.Errorf("KTF WIPI init returned %#x", result)
	}
	client.runtime = runtime
	client.executable = executable
	return summary, nil
}

type initializationRuntime struct {
	client *Client
	arena  *guestArena
	// poisonWindow is the buffer the arena's use-after-free check reads
	// through; see arena_poison.go. It is nil in release builds, where
	// poisonedBlocks and checkedBlocks also stay zero.
	poisonWindow []byte
	// inputModeTableAddress is the `M_Char **` the input-method table answers
	// with; see wipic_im.go. It is built once and kept.
	inputModeTableAddress uint32
	// poisonedBlocks and checkedBlocks are how much the detector covered.
	// They are reported because a clean report otherwise cannot be told apart
	// from a detector that never ran.
	poisonedBlocks      uint64
	checkedBlocks       uint64
	codeCursor          uint64
	stubs               map[uint64]uint32
	classes             map[string]uint32
	loadedClasses       map[string]uint32
	nativeMethods       map[uint32]runtimeJavaInvocation
	nextNativeMethod    uint32
	initializedClasses  map[uint32]bool
	databases           map[string]*runtimeDataBaseStore
	cDatabases          map[string]*runtimeCDatabase
	cDatabaseHandles    map[uint32]*runtimeCDatabaseHandle
	nextCDatabaseHandle uint32
	// The record databases are the other storage table; see
	// wipic_record_database.go for why they are kept apart from the streams.
	recordDatabases          map[string]*runtimeRecordDatabase
	recordDatabaseHandles    map[uint32]*runtimeRecordDatabaseHandle
	nextRecordDatabaseHandle uint32
	displayCards             []*jvm.Object
	dockedCard               *jvm.Object
	pendingTimers            []wipicTimer
	// pendingNetCallbacks are the MC_netConnect failures owed to callers that
	// registered one; see wipic_net.go.
	pendingNetCallbacks []wipicNetCallback
	pendingThreads      []*jvm.Object
	// pendingSerial holds Display.callSerially Runnables. They are dispatched
	// one per idle pass rather than as fast as the Host can turn rounds; see
	// runtimeDisplayCallSerially and serialDispatchInterval.
	pendingSerial    []*jvm.Object
	serialDueAt      time.Time
	repaintPending   bool
	repaintServicing bool
	// events is the WIPI event queue a game drains itself with
	// EventQueue.getNextEvent; guestEventLoop records that it does, which moves
	// key delivery from the Host's direct card dispatch to the queue.
	events         []guestEvent
	guestEventLoop bool
	jletListeners  []*jvm.Object
	grabbedKeys    map[int32]*jvm.Object
	activeJlet     *jvm.Object
	guestFiles     map[string][]byte
	// removedFiles is the set of paths unlink has deleted, loaded from the
	// store on first use. See guestFileRemovedKey for why a delete needs a
	// list of its own.
	removedFiles map[string]bool
	// removedCDatabases is the same list for the WIPI C database block. See
	// databaseRemovedKey.
	removedCDatabases map[string]bool
	// clips holds each org.kwis.msp.media.Clip's sound bytes and, once played,
	// its loaded handle. See runtime_media.go for why they are not guest fields.
	clips map[*jvm.Object]*clipState
	// wipicClips is the same thing for the WIPI C media block, whose clips are
	// guest addresses rather than Java objects. wipicClipOrder is their
	// creation order, which is what bounds how many keep their bytes. See
	// wipic_media.go.
	wipicClips     map[uint32]*wipicMediaClip
	wipicClipOrder []uint32
	runtimeObjects map[string]*jvm.Object
	classAliases   map[uint32]uint32
	// aotCallDepth is nesting per guest call stack, not per runtime. A guest
	// thread parks with its whole nested stack intact and its depth still
	// counted, so one shared counter grows with every parked worker and the
	// client thread's own paint then trips a limit none of them reached. Each
	// ARM thread carries its own depth, which is what the limit is about.
	aotCallDepth       map[*armcore.Thread]uint32
	resultBindingDepth uint32
	currentThread      *armcore.Thread
	pixelOps           *pixelOpCache
	currentContext     context.Context
	// virtualBaseMillis and clockBase anchor the guest's timeline to the
	// session clock at the moment the runtime was built. See guestMillis.
	virtualBaseMillis int64
	clockBase         time.Time
	kernelInterface   uint32
	javaInterface     uint32
	wipicInterface    uint32
	jvmContext        uint32
	exceptionContext  uint32
	screenFramebuffer uint32
	callbacks         InitializationCallbacks
	callCounts        map[diagEvent]uint32
	trace             traceRing
	// objects tracks live guest object allocations for the collector, and
	// collectAt is the arena size that triggers the next cycle.
	objects   map[uint32]objectRecord
	collectAt uint64
	// classSummaries caches the name, superclass, and interfaces of guest class
	// records for the type check, which walks them on every instanceof.
	classSummaries map[uint32]aotClassSummary
	// framebufferOpacity holds the transparency of the image framebuffers
	// whose encoding declared some pixels undrawn; 16-bit guest pixels have
	// no alpha channel to carry it themselves.
	framebufferOpacity map[uint32]*imageOpacity
	// wipicAllocations is the arena size of every live MC_knlAlloc block,
	// keyed by the memory identifier the guest was handed. MC_knlFree has no
	// other way to know how much to give back, and a key this map does not
	// hold is the only thing that separates a real release from a double free
	// or a pointer that never came from here.
	wipicAllocations map[uint32]uint64
	// userMemoryInterface is the handset library table handed to a game that
	// asked for it by name, and userMemoryPools the buffers it manages. See
	// wipic_usermem.go.
	userMemoryInterface uint32
	userMemoryPools     map[uint32]*userMemoryPool
}

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

const (
	// diagnosticNameLimit bounds how many distinct call-site-qualified names
	// are counted, so a guest cannot grow the map without limit.
	diagnosticNameLimit = 4096
	// diagnosticNameCeiling leaves headroom past that budget for the collapsed
	// names, which are bounded by the classes and methods the game actually
	// has rather than by the addresses calling them.
	diagnosticNameCeiling = 2 * diagnosticNameLimit
)

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
	elapsed := runtime.client.now().Sub(runtime.clockBase)
	if elapsed < 0 {
		elapsed = 0
	}
	scaled := float64(elapsed) * runtime.client.speedOrDefault()
	return runtime.virtualBaseMillis + int64(time.Duration(scaled)/time.Millisecond)
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

func newInitializationRuntime(client *Client) (*initializationRuntime, error) {
	if client == nil || client.core == nil || client.vm == nil {
		return nil, fmt.Errorf("KTF client is not initialized")
	}
	memory := client.core.Memory()
	if err := memory.Map(platformDataBase, platformDataSize, armcore.PermissionReadWrite); err != nil {
		return nil, fmt.Errorf("map KTF platform initialization data: %w", err)
	}
	if err := memory.Map(platformCodeBase, platformCodeSize, armcore.PermissionReadExecute); err != nil {
		return nil, fmt.Errorf("map KTF platform callback stubs: %w", err)
	}
	runtime := &initializationRuntime{
		client:            client,
		arena:             newGuestArena(platformDataBase, platformDataSize),
		objects:           make(map[uint32]objectRecord),
		collectAt:         collectionFloor,
		codeCursor:        uint64(platformCodeBase),
		stubs:             make(map[uint64]uint32),
		classes:           make(map[string]uint32),
		loadedClasses:     make(map[string]uint32),
		nativeMethods:     make(map[uint32]runtimeJavaInvocation),
		runtimeObjects:    make(map[string]*jvm.Object),
		classAliases:      make(map[uint32]uint32),
		grabbedKeys:       make(map[int32]*jvm.Object),
		virtualBaseMillis: time.Now().UnixMilli(),
		clockBase:         client.now(),
		trace:             traceRing{limit: client.traceLimit},
	}
	runtime.installArenaPoison()
	if err := runtime.registerRuntimeJavaNatives(); err != nil {
		return nil, err
	}
	client.vm.SetAOTInvoker(runtime.invokeAOTFromJVM)
	return runtime, nil
}

// invokeAOTFromJVM runs a guest AOT method on behalf of interpreted bytecode.
// It nests on the guest thread suspended at the current supervisor call, so
// it is only reachable while a supervisor call is being handled.
func (runtime *initializationRuntime) invokeAOTFromJVM(className, name, descriptor string, arguments []jvm.Value) (jvm.Value, error) {
	thread, ctx := runtime.currentThread, runtime.currentContext
	if thread == nil || ctx == nil {
		return jvm.VoidValue(), fmt.Errorf("KTF AOT method %s.%s%s invoked without an active guest thread", className, name, descriptor)
	}
	runtime.recordDiagnostic(diagEvent{kind: diagJVMToAOT, name: className, target: name, descriptor: descriptor})
	metadata, ok := runtime.client.vm.AOTClass(className)
	if !ok {
		return jvm.VoidValue(), fmt.Errorf("KTF AOT class %s is not registered", className)
	}
	method, found, err := runtime.client.vm.FindAOTMethod(metadata.Address, name, descriptor)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !found {
		// The registry stops at the first superclass the title has not
		// registered yet; the guest's own records do not.
		method, found, err = runtime.aotMethodFromGuestRecords(metadata.Address, name, descriptor)
		if err != nil {
			return jvm.VoidValue(), err
		}
	}
	if !found {
		return jvm.VoidValue(), fmt.Errorf("KTF AOT method not found: %s.%s%s", className, name, descriptor)
	}
	methodType, err := jvm.ParseMethodDescriptor(descriptor)
	if err != nil {
		return jvm.VoidValue(), err
	}
	types := methodType.Parameters
	if method.AccessFlags&0x0008 == 0 {
		types = append([]jvm.Type{{Kind: jvm.TypeReference, ClassName: className}}, types...)
	}
	if len(arguments) != len(types) {
		return jvm.VoidValue(), fmt.Errorf("KTF AOT method %s.%s%s expected %d arguments, got %d", className, name, descriptor, len(types), len(arguments))
	}
	// References crossing into guest code need guest layouts. Array contents
	// need nothing: a bound array's elements already are the guest's bytes.
	raw := make([]uint32, 0, len(arguments)+1)
	for index, value := range arguments {
		if value.Kind() == jvm.ValueReference {
			object, referenceErr := value.Reference()
			if referenceErr != nil {
				return jvm.VoidValue(), referenceErr
			}
			if object != nil {
				if err := runtime.ensureResultBound(object); err != nil {
					return jvm.VoidValue(), err
				}
			}
		}
		words, wordsErr := aotValueWords(value, types[index], runtime.client.vm)
		if wordsErr != nil {
			return jvm.VoidValue(), fmt.Errorf("KTF AOT argument %d for %s.%s%s: %w", index, className, name, descriptor, wordsErr)
		}
		raw = append(raw, words...)
	}
	result, _, err := runtime.runAOTMethod(ctx, thread, method, methodType, raw)
	if err != nil {
		if errors.Is(err, armcore.ErrThreadState) {
			runtime.countDiagnostic(fmt.Sprintf("jvm->aot state error: current=%p state=%s root=%p root-state=%s", thread, thread.State(), runtime.client.thread, runtime.client.thread.State()))
		}
		var uncaught *UncaughtAOTException
		if errors.As(err, &uncaught) && uncaught.Exception != nil {
			// Interpreted try/catch handles the guest exception like any
			// other JVM exception.
			return jvm.VoidValue(), uncaught.Exception
		}
		return jvm.VoidValue(), err
	}
	return result, nil
}

func (runtime *initializationRuntime) prepare() ([]uint32, error) {
	var err error
	runtime.kernelInterface, err = runtime.makeKernelInterface()
	if err != nil {
		return nil, err
	}
	runtime.javaInterface, err = runtime.makeJavaInterface()
	if err != nil {
		return nil, err
	}
	runtime.wipicInterface, err = runtime.makeWIPICInterface()
	if err != nil {
		return nil, err
	}
	runtime.jvmContext, err = runtime.allocateWords(make([]uint32, 3+128))
	if err != nil {
		return nil, err
	}
	runtime.exceptionContext, err = runtime.allocateWords(make([]uint32, 9))
	if err != nil {
		return nil, err
	}
	if err := runtime.client.core.RegisterThreadLocalWord(runtime.exceptionContext + javaExceptionHead); err != nil {
		return nil, fmt.Errorf("register KTF Java exception handler head: %w", err)
	}
	param0, err := runtime.allocateWords([]uint32{0})
	if err != nil {
		return nil, err
	}
	param1, err := runtime.allocateWords([]uint32{runtime.exceptionContext})
	if err != nil {
		return nil, err
	}
	param3, err := runtime.allocateWords([]uint32{
		0, 0, 0, 0,
		'Z', 'C', 'F', 'D', 'B', 'S', 'I', 'J',
	})
	if err != nil {
		return nil, err
	}
	param4, err := runtime.makeInitCallbacks()
	if err != nil {
		return nil, err
	}
	return []uint32{param0, param1, runtime.jvmContext, param3, param4}, nil
}

func (runtime *initializationRuntime) makeInitCallbacks() (uint32, error) {
	getInterface, err := runtime.stub(svcCategoryInit, initSVCGetInterface)
	if err != nil {
		return 0, err
	}
	javaThrow, err := runtime.stub(svcCategoryInit, initSVCJavaThrow)
	if err != nil {
		return 0, err
	}
	javaCheckType, err := runtime.stub(svcCategoryInit, initSVCJavaCheckType)
	if err != nil {
		return 0, err
	}
	javaNew, err := runtime.stub(svcCategoryInit, initSVCJavaNew)
	if err != nil {
		return 0, err
	}
	javaArrayNew, err := runtime.stub(svcCategoryInit, initSVCJavaArrayNew)
	if err != nil {
		return 0, err
	}
	javaClassLoad, err := runtime.stub(svcCategoryInit, initSVCJavaClassLoad)
	if err != nil {
		return 0, err
	}
	allocate, err := runtime.stub(svcCategoryInit, initSVCAlloc)
	if err != nil {
		return 0, err
	}
	return runtime.allocateWords([]uint32{
		getInterface,
		javaThrow,
		0,
		0,
		javaCheckType,
		javaNew,
		javaArrayNew,
		0,
		javaClassLoad,
		0,
		0,
		allocate,
	})
}

func (runtime *initializationRuntime) makeKernelInterface() (uint32, error) {
	words := make([]uint32, 65)
	for index := range words {
		stub, err := runtime.stub(svcCategoryWIPIC, uint32(index))
		if err != nil {
			return 0, err
		}
		words[index] = stub
	}
	return runtime.allocateWords(words)
}

func (runtime *initializationRuntime) makeJavaInterface() (uint32, error) {
	words := make([]uint32, 13)
	for index, id := range []uint32{
		javaSVCJump1,
		javaSVCJump2,
		javaSVCJump3,
		javaSVCGetMethod,
		javaSVCGetField,
		javaSVCUnknown4,
		javaSVCUnknown5,
		javaSVCMonitorEnter,
		javaSVCMonitorExit,
		javaSVCRegisterClass,
		javaSVCRegisterString,
		javaSVCCallNative,
	} {
		stub, err := runtime.stub(svcCategoryJavaInterface, id)
		if err != nil {
			return 0, err
		}
		words[index+1] = stub
	}
	return runtime.allocateWords(words)
}

// wipicTableFunctions bounds each WIPI C interface table. The largest original
// interface is the graphics table, well under this count.
const wipicTableFunctions = 64

func (runtime *initializationRuntime) makeWIPICInterface() (uint32, error) {
	words := make([]uint32, 17)
	for index := range words {
		tableID := uint32(index + 1)
		entries := make([]uint32, wipicTableFunctions)
		for function := range entries {
			stub, err := runtime.stub(svcCategoryWIPIC, tableID<<16|uint32(function))
			if err != nil {
				return 0, err
			}
			entries[function] = stub
		}
		table, err := runtime.allocateWords(entries)
		if err != nil {
			return 0, err
		}
		words[index] = table
	}
	return runtime.allocateWords(words)
}

func (runtime *initializationRuntime) handleSupervisorCall(ctx context.Context, thread *armcore.Thread, call armcore.SupervisorCall) error {
	id, err := thread.Register(12)
	if err != nil {
		return err
	}
	// Interpreted JVM code reached from this handler may dispatch back into
	// guest AOT methods; it nests on the suspended thread recorded here.
	previousThread, previousContext := runtime.currentThread, runtime.currentContext
	runtime.currentThread, runtime.currentContext = thread, ctx
	defer func() {
		runtime.currentThread, runtime.currentContext = previousThread, previousContext
	}()
	var result uint32
	switch call.Immediate {
	case svcCategoryInit:
		result, err = runtime.handleInitCall(thread, id)
	case svcCategoryJavaInterface:
		result, err = runtime.handleJavaCall(ctx, thread, id)
		var unwind *aotExceptionUnwind
		if errors.As(err, &unwind) {
			return runtime.resumeAOTException(thread, unwind)
		}
	case svcCategoryWIPIC:
		result, err = runtime.handleWIPICCall(thread, id)
	case svcCategoryBinaryHook:
		result, err = runtime.handleBinaryHookCall(thread, id)
	case svcCategoryRuntimeJava:
		result, err = runtime.handleRuntimeJavaCall(thread, id)
		var unwind *aotExceptionUnwind
		if errors.As(err, &unwind) {
			return runtime.resumeAOTException(thread, unwind)
		}
	default:
		err = fmt.Errorf("unknown KTF SVC category %d with id %#x", call.Immediate, id)
	}
	if err != nil {
		return err
	}
	return thread.SetRegister(0, result)
}

func (runtime *initializationRuntime) handleInitCall(thread *armcore.Thread, id uint32) (uint32, error) {
	switch id {
	case initSVCGetInterface:
		address, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		name, err := runtime.readCString(address, 128)
		if err != nil {
			return 0, fmt.Errorf("read KTF interface name: %w", err)
		}
		switch name {
		case "WIPIC_knlInterface":
			return runtime.kernelInterface, nil
		case "WIPI_JBInterface":
			return runtime.javaInterface, nil
		default:
			return 0, nil
		}
	case initSVCJavaThrow:
		return runtime.throwAOTException(thread)
	case initSVCJavaCheckType:
		return runtime.checkAOTType(thread)
	case initSVCJavaNew:
		runtime.callbacks.Allocations++
		return runtime.newAOTInstance(thread)
	case initSVCJavaArrayNew:
		runtime.callbacks.Allocations++
		return runtime.newAOTArray(thread)
	case initSVCJavaClassLoad:
		return runtime.loadJavaClass(thread)
	case initSVCAlloc:
		size, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		runtime.callbacks.Allocations++
		return runtime.allocate(uint64(size))
	default:
		return 0, fmt.Errorf("unknown KTF init SVC id %#x", id)
	}
}

func (runtime *initializationRuntime) handleJavaCall(ctx context.Context, thread *armcore.Thread, id uint32) (uint32, error) {
	switch id {
	case javaSVCUnknown4, javaSVCUnknown5, javaSVCMonitorEnter, javaSVCMonitorExit:
		return 0, nil
	case javaSVCJump1, javaSVCJump2, javaSVCJump3:
		return runtime.callAOTJump(ctx, thread, id)
	case javaSVCGetMethod:
		return runtime.getAOTMethod(thread)
	case javaSVCGetField:
		return runtime.getAOTField(thread)
	case javaSVCRegisterClass:
		return runtime.registerAOTClass(ctx, thread)
	case javaSVCRegisterString:
		return runtime.registerJavaString(thread)
	case javaSVCCallNative:
		return runtime.callAOTNative(ctx, thread)
	default:
		return 0, fmt.Errorf("unknown KTF Java interface SVC id %#x", id)
	}
}

func (runtime *initializationRuntime) callAOTJump(ctx context.Context, thread *armcore.Thread, id uint32) (uint32, error) {
	registers := make([]uint32, 4)
	for index := range registers {
		value, err := thread.Register(index)
		if err != nil {
			return 0, err
		}
		registers[index] = value
	}
	var address uint32
	var arguments []uint32
	switch id {
	case javaSVCJump1:
		address = registers[1]
		arguments = []uint32{registers[0], 0, 0}
	case javaSVCJump2:
		address = registers[2]
		arguments = []uint32{registers[0], registers[1], 0}
	case javaSVCJump3:
		address = registers[3]
		arguments = []uint32{registers[0], registers[1], registers[2]}
	default:
		return 0, fmt.Errorf("unknown KTF Java jump id %#x", id)
	}
	if address == 0 {
		return 0, fmt.Errorf("KTF Java jump target is null")
	}
	if lr, lrErr := thread.Register(armcore.RegisterLR); lrErr == nil {
		runtime.recordDiagnostic(diagEvent{
			kind:    diagJump,
			nums:    [5]uint32{id - javaSVCJump1 + 1, address, arguments[0], arguments[1], arguments[2]},
			site:    lr,
			hasSite: true,
		})
	}
	if err := runtime.enterAOTCall(); err != nil {
		return 0, err
	}
	defer runtime.leaveAOTCall()
	summary, err := runtime.client.core.Call(ctx, thread, address, ReturnAddress, arguments, runtime.handleSupervisorCall)
	if err != nil {
		return 0, fmt.Errorf("execute KTF AOT Java jump %#x: %w", id, err)
	}
	return summary.Context.Registers[0], nil
}

func (runtime *initializationRuntime) callAOTNative(ctx context.Context, thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	if address == 0 {
		return 0, fmt.Errorf("KTF AOT native call target is null")
	}
	dataAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	if dataAddress&3 != 0 {
		return 0, fmt.Errorf("KTF AOT native call data pointer %#x is not word-aligned", dataAddress)
	}

	// Validate both permissions before entering guest code. Rewriting the same
	// bytes avoids a native side effect followed by a late result-write fault.
	var original [8]byte
	if err := runtime.client.core.Memory().Read(dataAddress, original[:]); err != nil {
		return 0, fmt.Errorf("read KTF AOT native call data at %#x: %w", dataAddress, err)
	}
	if err := runtime.client.core.Memory().Write(dataAddress, original[:]); err != nil {
		return 0, fmt.Errorf("validate writable KTF AOT native call data at %#x: %w", dataAddress, err)
	}
	if err := runtime.enterAOTCall(); err != nil {
		return 0, err
	}
	defer runtime.leaveAOTCall()

	summary, err := runtime.client.core.Call(
		ctx,
		thread,
		address,
		ReturnAddress,
		[]uint32{dataAddress, dataAddress},
		runtime.handleSupervisorCall,
	)
	if err != nil {
		return 0, fmt.Errorf("execute KTF AOT native call at %#x: %w", address, err)
	}
	var result [8]byte
	binary.LittleEndian.PutUint32(result[:4], summary.Context.Registers[0])
	if err := runtime.client.core.Memory().Write(dataAddress, result[:]); err != nil {
		return 0, fmt.Errorf("write KTF AOT native call result at %#x: %w", dataAddress, err)
	}
	return dataAddress, nil
}

func (runtime *initializationRuntime) enterAOTCall() error {
	if runtime.aotCallDepth[runtime.currentThread] >= maxAOTCallDepth {
		return fmt.Errorf("KTF AOT guest call nesting exceeds %d", maxAOTCallDepth)
	}
	if runtime.aotCallDepth == nil {
		runtime.aotCallDepth = make(map[*armcore.Thread]uint32)
	}
	runtime.aotCallDepth[runtime.currentThread]++
	return nil
}

func (runtime *initializationRuntime) leaveAOTCall() {
	thread := runtime.currentThread
	if depth := runtime.aotCallDepth[thread]; depth > 1 {
		runtime.aotCallDepth[thread] = depth - 1
		return
	}
	delete(runtime.aotCallDepth, thread)
}

func (runtime *initializationRuntime) resumeAOTException(thread *armcore.Thread, unwind *aotExceptionUnwind) error {
	if unwind == nil {
		return fmt.Errorf("KTF AOT Java exception unwind is nil")
	}
	if err := thread.SetRegister(0, unwind.contextBase); err != nil {
		return err
	}
	if err := thread.SetRegister(1, unwind.target); err != nil {
		return err
	}
	if err := thread.SetRegister(armcore.RegisterPC, unwind.nextPC); err != nil {
		return err
	}
	return nil
}

func (runtime *initializationRuntime) handleWIPICCall(thread *armcore.Thread, id uint32) (uint32, error) {
	runtime.countDiagnostic(fmt.Sprintf("wipic %#x", id))
	if id >= 1<<16 {
		return runtime.handleWIPICTableCall(thread, id>>16, id&0xffff)
	}
	switch id {
	case wipicKernelReserved1:
		return runtime.wipicInterface, nil
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
		return 0, nil
	case wipicKernelGetTotalMemory:
		return uint32(platformDataSize), nil
	case wipicKernelGetFreeMemory:
		return uint32(runtime.arena.available()), nil
	case wipicKernelPrintk:
		return runtime.wipicPrintk(thread)
	case wipicKernelSprintk:
		return runtime.wipicSprintk(thread)
	case wipicKernelGetAccessLevel:
		return wipicAccessLevel, nil
	case wipicKernelGetExecNames:
		return runtime.wipicGetExecNames(thread)
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
	case wipicKernelGetResourceID:
		return runtime.wipicGetResourceID(thread)
	case wipicKernelGetResource:
		return runtime.wipicGetResource(thread)
	case wipicKernelExit:
		// MC_knlExit ends the program; the Host observes ErrGuestExited and
		// tears the session down instead of treating it as a failure.
		runtime.countDiagnostic("kernel exit")
		return 0, ErrGuestExited
	case wipicKernelGetDLLInterface:
		return runtime.wipicGetDLLInterface(thread)
	default:
		// The stub returns with `bx lr`, so LR still holds the caller's return
		// address. Naming it turns "some kernel slot is missing" into an address
		// that can be disassembled in the image.
		return 0, fmt.Errorf("KTF WIPI C SVC id %#x is not implemented%s", id, runtime.callerSite(thread))
	}
}

// callerSite renders the guest call site of the platform stub currently being
// serviced, for error messages that would otherwise name only a slot number.
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

// WIPI C interface tables follow the original ordering: table 1 util, 2 misc,
// 3 graphics, 4 the input method, 5 record database, 7 database, 9 uic,
// 10 media, 11 net.
//
// Five and seven are both storage. Seven is the stream database — one blob
// addressed by a cursor — and five is the record database, a numbered set. A
// game uses whichever suits what it is storing, and some use both.
//
// Four sits where it does for the same reason the LGT runtime's input-method
// block sits after its graphics block: the specification prints the `MC_im*`
// functions at the end of its graphics section rather than in one of their own.
const (
	wipicTableUtil           = 1
	wipicTableMisc           = 2
	wipicTableGraphics       = 3
	wipicTableInputMethod    = 4
	wipicTableRecordDatabase = 5
	wipicTableDatabase       = 7
	wipicTableUIC            = 9
	wipicTableMedia          = 10
	wipicTableNet            = 11

	// A handset library reached through MC_knlGetDLLInterface is not one of the
	// WIPI C interface tables, but its entries are stubs of the same shape, so
	// they are dispatched through the same category under a table number no
	// interface table can take.
	wipicTableUserMemory = 64
)

// The net table's first two functions are connect and close. Everything a
// game does with a socket goes through them first.
const (
	wipicNetConnect = 0
	wipicNetClose   = 1
)

// wipiErrorCode is the WIPI C failure result. It is what a call that cannot
// be served answers with, and what a game's own error path expects.
const wipiErrorCode uint32 = 0xffffffff

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
		// MC_grpGetImageFrameBuffer: the image record begins with its
		// framebuffer fields, so the image handle doubles as one.
		handle, err := thread.Register(0)
		if err != nil {
			return 0, err
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
		// MC_grpFlushLcd presents the screen pixel buffer to the Host frame.
		return 0, runtime.presentScreen()
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
	case table == wipicTableGraphics && function == 32:
		return runtime.wipicCreateImage(thread)
	case table == wipicTableGraphics && function == 33:
		return runtime.wipicDestroyImage(thread)
	case table == wipicTableGraphics && function == 35:
		return runtime.wipicEncodeImage(thread)
	case table == wipicTableRecordDatabase:
		return runtime.handleWIPICRecordDatabaseCall(thread, function)
	case table == wipicTableDatabase:
		return runtime.handleWIPICDatabaseCall(thread, function)
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
	case table >= 4 && table != wipicTableDatabase:
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

// The MC_GrpContext record is 52 bytes: mask, four 16-bit clip bounds, the
// foreground/background/transparent pixels, alpha, a 16-bit x/y offset pair,
// the pixel-op function with its parameter, one reserved word, font, and
// style.
const wipicGraphicsContextSize = 52

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
		return 0, fmt.Errorf("access KTF graphics context field %d at %#x: %w", operation, contextAddress, accessErr)
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

// wipicTimer is a registered MC_knlSetTimer request. The lifecycle event loop
// services pending timers; the bounded acceptance probes only record them.
type wipicTimer struct {
	pointer  uint32
	callback uint32
	param    uint32
	delay    uint64
	// due is when the callback may run. A timer is the other way a KTF game
	// paces itself, so the delay is a real wait rather than a label.
	due time.Time
	// task and owner are set instead of callback when the timer came from
	// java/util/Timer rather than from MC_knlSetTimer: the task is the object
	// whose run() the service round invokes, and the owner is the Timer that
	// scheduled it, which is what its cancel() has to find. period is how long
	// after each run the next one is due, and zero means it runs once.
	task   *jvm.Object
	owner  *jvm.Object
	period time.Duration
}

const (
	maxPendingTimers = 256
	// maxTimerDelayMillis caps a timer delay at an hour of guest time.
	maxTimerDelayMillis = 60 * 60 * 1000
)

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
	if len(runtime.pendingTimers) >= maxPendingTimers {
		return 0, fmt.Errorf("KTF pending timer count exceeds %d", maxPendingTimers)
	}
	delay := uint64(high)<<32 | uint64(low)
	// A guest that asks for an absurd delay is asking for a timer that never
	// fires in a session; clamping keeps the deadline arithmetic in range
	// without changing any delay a game actually uses.
	wait := time.Duration(min(delay, maxTimerDelayMillis)) * time.Millisecond
	runtime.pendingTimers = append(runtime.pendingTimers, wipicTimer{
		pointer:  pointer,
		callback: callback[0],
		param:    param,
		delay:    delay,
		due:      runtime.client.waitDeadline(wait),
	})
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
	return 0, nil
}

// wipicZeroFill is the run of zero bytes an allocation is cleared with. It is
// read-only, so every caller can share it.
var wipicZeroFill [1024]byte

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
	binary.LittleEndian.PutUint32(header[0:], address+4)
	binary.LittleEndian.PutUint32(header[4:], size)
	if err := runtime.client.core.Memory().Write(address, header); err != nil {
		return 0, fmt.Errorf("write KTF WIPI C allocation header at %#x: %w", address, err)
	}
	if runtime.wipicAllocations == nil {
		runtime.wipicAllocations = make(map[uint32]uint64)
	}
	runtime.wipicAllocations[address] = total
	return address, nil
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
	// Transparency is recorded against the handle of the framebuffer or image
	// that lives in the block, so a released address must not carry its mask
	// into whatever is allocated there next.
	runtime.setFramebufferOpacity(id, nil)
	runtime.arena.release(id, size)
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
	value, ok := wipic.SystemProperties[name]
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
	if handle&1<<31 != 0 {
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
	if err := runtime.client.core.Memory().Write(base[0]+8, data); err != nil {
		return 0, fmt.Errorf("write KTF resource %q: %w", name, err)
	}
	return 0, nil
}

func (runtime *initializationRuntime) loadJavaClass(thread *armcore.Thread) (uint32, error) {
	target, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	nameAddress, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	name, err := runtime.readCString(nameAddress, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF Java class name: %w", err)
	}
	runtime.countDiagnostic("load " + name)
	class, err := runtime.ensureJavaClass(name)
	if err != nil {
		return 0, err
	}
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], class)
	if err := runtime.client.core.Memory().Write(target, data[:]); err != nil {
		return 0, fmt.Errorf("write KTF Java class result at %#x: %w", target, err)
	}
	runtime.callbacks.LoadedClasses++
	return 0, nil
}

func (runtime *initializationRuntime) ensureJavaClass(name string) (uint32, error) {
	if class := runtime.classes[name]; class != 0 {
		return class, nil
	}
	if metadata, ok := runtime.client.vm.AOTClass(name); ok {
		if err := runtime.bindAOTClassObject(metadata.Address, name); err != nil {
			return 0, err
		}
		runtime.classes[name] = metadata.Address
		return metadata.Address, nil
	}
	if definition, ok := runtimeJavaClasses[name]; ok {
		return runtime.createRuntimeJavaClass(definition)
	}

	classObject, err := runtime.client.vm.NewClassObject(name)
	if err != nil {
		return 0, fmt.Errorf("create JVM class object for %q: %w", name, err)
	}
	// Every class this fallback makes still inherits Object, and an array
	// class inherits nothing else: its whole method set is Object's. A record
	// without Object's vtable is therefore not a lesser record but a broken
	// one — guest virtual dispatch reads the vtable pointer out of the class
	// and indexes it, so a zero there faults on the first `getClass()` or
	// `equals()` a title calls on an array. See "A class left out of that
	// table still resolves" in docs/ktf.md.
	var parent uint32
	if name != "java/lang/Object" {
		if parent, err = runtime.ensureJavaClass("java/lang/Object"); err != nil {
			return 0, fmt.Errorf("resolve KTF Java class %q parent: %w", name, err)
		}
	}
	vtable, err := runtime.inheritedVTable(parent)
	if err != nil {
		return 0, fmt.Errorf("read KTF Java class %q parent vtable: %w", name, err)
	}
	vtableAddress, err := runtime.allocateWords(append(vtable.pointers(), 0))
	if err != nil {
		return 0, err
	}
	namePointer, err := runtime.allocateBytes(append([]byte(name), 0))
	if err != nil {
		return 0, err
	}
	// An array has no fields, and the guest's array-store check spends that word
	// on the element class instead: it reads the object header's class record,
	// takes the descriptor at +8, and loads +0x14 as the class to check the
	// stored value against. Leaving it zero asks the check about a null class,
	// which can only be answered with the permissive yes. See "Three ways a
	// class record was not a class, and the two titles they stopped" in
	// docs/ktf.md, "The array store check was asked about a null class".
	elementClass, err := runtime.arrayElementClass(name)
	if err != nil {
		return 0, err
	}
	descriptorData := make([]byte, javaDescriptorSize)
	binary.LittleEndian.PutUint32(descriptorData[0:], namePointer)
	binary.LittleEndian.PutUint32(descriptorData[8:], parent)
	binary.LittleEndian.PutUint32(descriptorData[20:], elementClass)
	binary.LittleEndian.PutUint16(descriptorData[28:], 0x21)
	descriptor, err := runtime.allocateBytes(descriptorData)
	if err != nil {
		return 0, err
	}
	class, err := runtime.allocate(javaClassSize)
	if err != nil {
		return 0, err
	}
	classData := make([]byte, javaClassSize)
	binary.LittleEndian.PutUint32(classData[0:], class+4)
	binary.LittleEndian.PutUint32(classData[8:], descriptor)
	binary.LittleEndian.PutUint32(classData[12:], vtableAddress)
	binary.LittleEndian.PutUint16(classData[16:], uint16(len(vtable.entries)))
	binary.LittleEndian.PutUint16(classData[18:], 8)
	if err := runtime.client.core.Memory().Write(class, classData); err != nil {
		return 0, fmt.Errorf("write KTF Java class %q at %#x: %w", name, class, err)
	}
	// The record is the source of truth once it is written, so the metadata is
	// read back from it rather than composed beside it: a vtable the registry
	// does not know about is a vtable the type check and the method lookup
	// cannot use.
	metadata, err := runtime.readAOTClass(class)
	if err != nil {
		return 0, fmt.Errorf("validate KTF Java class %q at %#x: %w", name, class, err)
	}
	if err := runtime.client.vm.RegisterAOTClass(metadata); err != nil {
		return 0, fmt.Errorf("register runtime KTF AOT class %q: %w", name, err)
	}
	if err := runtime.client.vm.BindAOTObject(class, classObject); err != nil {
		return 0, fmt.Errorf("bind KTF Java class %q at %#x: %w", name, class, err)
	}
	runtime.classes[name] = class
	return class, nil
}

// arrayElementClass resolves the class record a reference array's element type
// names, and answers zero for anything else. A primitive array is not a case
// this leaves out by accident: the guest compiles a store into one as a typed
// store with no check at all, so the slot is never read, and a class record
// invented for `I` would only be a record nothing names.
func (runtime *initializationRuntime) arrayElementClass(name string) (uint32, error) {
	if len(name) < 2 || name[0] != '[' {
		return 0, nil
	}
	component := name[1:]
	switch component[0] {
	case '[':
	case 'L':
		if !strings.HasSuffix(component, ";") {
			return 0, fmt.Errorf("KTF array class %q has an unterminated element descriptor", name)
		}
		component = component[1 : len(component)-1]
		if component == "" {
			return 0, fmt.Errorf("KTF array class %q names an empty element class", name)
		}
	default:
		return 0, nil
	}
	// The element class is resolved, never invented. A name this platform does
	// not own belongs to the title, which registers its own record for it later
	// — and a fallback record made here first is the record every later lookup
	// of that name finds, which is a title that stops during its own class
	// construction. An unknown element leaves the word zero, which is the
	// permissive answer this platform gave for every array before.
	if class := runtime.classes[component]; class != 0 {
		return class, nil
	}
	if metadata, ok := runtime.client.vm.AOTClass(component); ok {
		return metadata.Address, nil
	}
	_, owned := runtimeJavaClasses[component]
	if !owned && !strings.HasPrefix(component, "[") {
		return 0, nil
	}
	class, err := runtime.ensureJavaClass(component)
	if err != nil {
		return 0, fmt.Errorf("resolve KTF array class %q element class: %w", name, err)
	}
	return class, nil
}

func (runtime *initializationRuntime) bindAOTClassObject(address uint32, name string) error {
	if object, ok := runtime.client.vm.AOTObject(address); ok {
		if object.ClassName != "java/lang/Class" || object.Native != name {
			return fmt.Errorf("KTF AOT class address %#x is bound to a non-class object", address)
		}
		return nil
	}
	object, err := runtime.client.vm.NewClassObject(name)
	if err != nil {
		return fmt.Errorf("create JVM class object for %q: %w", name, err)
	}
	if err := runtime.client.vm.BindAOTObject(address, object); err != nil {
		return fmt.Errorf("bind KTF Java class %q at %#x: %w", name, address, err)
	}
	return nil
}

func (runtime *initializationRuntime) registerJavaString(thread *armcore.Thread) (uint32, error) {
	address, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	length, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	if length == ^uint32(0) {
		var data [2]byte
		if err := runtime.client.core.Memory().Read(address, data[:]); err != nil {
			return 0, fmt.Errorf("read KTF Java string length at %#x: %w", address, err)
		}
		length = uint32(binary.LittleEndian.Uint16(data[:]))
		address += 2
	}
	if length > maxJavaStringUnits {
		return 0, fmt.Errorf("KTF Java string length %d exceeds %d", length, maxJavaStringUnits)
	}
	data := make([]byte, uint64(length)*2)
	if err := runtime.client.core.Memory().Read(address, data); err != nil {
		return 0, fmt.Errorf("read KTF Java string at %#x: %w", address, err)
	}
	units := make([]uint16, length)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(data[index*2:])
	}
	javaString := runtime.client.vm.NewString(string(utf16.Decode(units)))
	// Strings receive the complete guest object layout: guest code dispatches
	// virtual methods on them and reads the header through the fields pointer.
	classAddress, err := runtime.ensureJavaClass("java/lang/String")
	if err != nil {
		return 0, fmt.Errorf("resolve KTF Java string class: %w", err)
	}
	metadata, ok := runtime.client.vm.AOTClassAt(classAddress)
	if !ok {
		return 0, fmt.Errorf("KTF Java string class at %#x is not registered", classAddress)
	}
	object, err := runtime.allocateAOTObject(metadata, make([]byte, metadata.InstanceSize), javaString)
	if err != nil {
		return 0, fmt.Errorf("allocate KTF Java string object: %w", err)
	}
	runtime.callbacks.RegisteredStrings++
	return object, nil
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
	return address | 1, nil
}

func (runtime *initializationRuntime) allocate(size uint64) (uint32, error) {
	if size > maxPlatformAllocation {
		return 0, fmt.Errorf("KTF platform allocation %d exceeds %d bytes", size, maxPlatformAllocation)
	}
	address, ok := runtime.arena.allocate(size)
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
	data := make([]byte, 0, min(limit, 128))
	for offset := uint32(0); offset < limit; offset++ {
		current := uint64(address) + uint64(offset)
		if current >= uint64(1)<<32 {
			return "", fmt.Errorf("string address overflows guest memory")
		}
		var value [1]byte
		if err := runtime.client.core.Memory().Read(uint32(current), value[:]); err != nil {
			return "", err
		}
		if value[0] == 0 {
			return string(data), nil
		}
		data = append(data, value[0])
	}
	return "", fmt.Errorf("string at %#x exceeds %d bytes", address, limit)
}

// readBoundedString reads at most limit bytes, stopping early at a terminator.
// Running to the bound is the expected end rather than an error: the caller
// asked for a fixed count of bytes, not for a C string.
func (runtime *initializationRuntime) readBoundedString(address, limit uint32) ([]byte, error) {
	data := make([]byte, 0, min(limit, 128))
	for offset := uint32(0); offset < limit; offset++ {
		current := uint64(address) + uint64(offset)
		if current >= uint64(1)<<32 {
			return nil, fmt.Errorf("string address overflows guest memory")
		}
		var value [1]byte
		if err := runtime.client.core.Memory().Read(uint32(current), value[:]); err != nil {
			return nil, err
		}
		if value[0] == 0 {
			break
		}
		data = append(data, value[0])
	}
	return data, nil
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
// nothing describes it: not the specification, not the original runtime, not
// the reference implementation, which does not serve this slot at all. That
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

const (
	// execNameSeparator divides a listing's two columns and execNameTrailer
	// ends the entry. Their values are not known — the one caller never reads
	// them, it only steps over the separator — so they are the least
	// surprising printable choice rather than a claim.
	execNameSeparator = "\t"
	execNameTrailer   = "\t000"
)

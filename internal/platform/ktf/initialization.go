package ktf

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
	"weak"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

const (

	// This expression does not compile if the stub arena is ever moved back
	// inside the data arena: an unsigned constant cannot go negative.
	_ = uint64(wipicrt.PlatformCodeBase) - (uint64(wipicrt.PlatformDataBase) + wipicrt.PlatformDataSize)

	maxJavaStringUnits   uint32 = 1 << 16
	maxJavaArrayElements uint32 = 1 << 20

	svcCategoryInit          uint32 = 1
	svcCategoryJavaInterface uint32 = 2
	svcCategoryRuntimeJava   uint32 = 4
	// svcCategoryBinaryHook answers a C runtime routine recognized in the
	// guest image and replaced with a native one. See binary_hooks.go.
	svcCategoryBinaryHook uint32 = 5
	// svcCategoryModuleInterface answers the interface the older relocatable
	// modules resolve by name before they do anything else. See
	// module_interface.go.
	svcCategoryModuleInterface uint32 = 6
	// svcCategoryModuleJump answers the six routines an older module tail-
	// jumps to through the table its segment header names. See
	// module_link.go.
	svcCategoryModuleJump uint32 = 7
	// A revision-scoped replacement for one camera store; see camera_bounds.go.
	svcCategoryCameraBounds uint32 = 8
	// A revision-scoped dialog coordinate correction; see menu_text.go.
	svcCategoryMenuText uint32 = 9

	initSVCGetInterface  uint32 = 0
	initSVCJavaThrow     uint32 = 1
	initSVCJavaCheckType uint32 = 2
	initSVCJavaNew       uint32 = 3
	initSVCJavaArrayNew  uint32 = 4
	initSVCJavaClassLoad uint32 = 5
	initSVCAlloc         uint32 = 6
	// initSVCJavaThrowObject is the callbacks table's slot 2. It is the other
	// half of the throw pair: slot 1 is handed a class name and makes the
	// exception itself, and this one is handed an exception the guest has
	// already constructed — which is what a title's own `throw e` compiles to.
	// Its number continues the run the two callback categories share rather
	// than filling the gap after initSVCAlloc: the numbers here are unique
	// across those categories, and a reader should not have to check which
	// category a repeated one belongs to.
	initSVCJavaThrowObject uint32 = 19

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

	// M_E_NOENT (-12)
	// M_E_INVALID (-9)
	// M_E_BADFORMAT (-6)
	// MC_GRP_IMAGE_DONE
	// M_E_SHORTBUF (-18)
	// M_E_EXIST (-5)
	// M_E_BADRECID (-22)
	// -1
	// M_E_BADFD (-2)
	// M_E_BADFILENAME (-3)
	// M_E_NOTEMPTY (-15)
	// M_E_LONGNAME (-11)
	// M_E_NOSPACE (-13)
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

// prepareInitialization builds the platform side of the client contract: the
// arena, the callback stubs, the interface tables and the five words the
// client's own initialization function is called with. It is memoized because
// the older relocatable modules need the callback table before their entry
// runs — their entry is handed it — while the current generation is handed a
// size and only reaches it through Initialize.
func (client *Client) prepareInitialization() (*initializationRuntime, []uint32, error) {
	if client.prepared != nil {
		return client.prepared, client.initParameters, nil
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		return nil, nil, err
	}
	parameters, err := runtime.prepare()
	if err != nil {
		return nil, nil, err
	}
	// The image is scanned as it now sits in guest memory rather than as it
	// arrived, so the entry's self-relocation is already accounted for and the
	// addresses hooked are the ones that will execute.
	if _, err := runtime.installImageHooks(); err != nil {
		return nil, nil, err
	}
	if err := runtime.installCameraBoundsCompatibility(); err != nil {
		return nil, nil, err
	}
	if err := runtime.installMenuTextCompatibility(); err != nil {
		return nil, nil, err
	}
	client.setRuntimes(client.runtime, runtime)
	client.initParameters = parameters
	return runtime, parameters, nil
}

// Initialize calls the validated interface and WIPI initialization functions.
// The current callback surface is intentionally limited to startup and normal
// AOT calls. Raw class metadata, native strings, and object/array allocations
// are bound to the shared Go JVM; native calls support normal returns and raw
// AOT handlers support low-level typed exception unwind.
func (client *Client) Initialize(ctx context.Context, executableAddress uint32) (InitializationSummary, error) {
	if client == nil || client.Core() == nil || client.Thread == nil {
		return InitializationSummary{}, fmt.Errorf("KTF client is not initialized")
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.initializationStarted {
		return InitializationSummary{}, fmt.Errorf("KTF client initialization already started")
	}
	client.initializationStarted = true

	executable, err := client.readExecutable(executableAddress)
	if err != nil {
		return InitializationSummary{}, err
	}
	runtime, parameters, err := client.prepareInitialization()
	if err != nil {
		return InitializationSummary{}, err
	}

	var summary InitializationSummary
	summary.Interface, err = client.Core().Call(
		ctx,
		client.Thread,
		executable.Interface.Functions.Init,
		wipicrt.ReturnAddress,
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

	summary.WIPI, err = client.Core().Call(
		ctx,
		client.Thread,
		executable.Init,
		wipicrt.ReturnAddress,
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
	client.setRuntimes(runtime, client.prepared)
	client.executable = executable
	return summary, nil
}

type initializationRuntime struct {
	*wipicrt.Runtime

	client *Client
	// cInputCardOwner is the visible card that owned the C input method's
	// activation, so a later overlay cannot inherit an old C editor merely by
	// requesting a fresh Host snapshot. See cInputCard.
	cInputCardOwner *jvm.Object
	// collecting guards the collector against being entered from inside itself.
	// It releases guest memory, and a release is not an allocation, so this only
	// ever fires if a future edit makes one. See collect.go.
	collecting bool
	// allocationRoots are objects the Host has just made and holds only as an
	// address while it allocates the object that will name them; see
	// holdAllocationRoot in collect.go.
	allocationRoots []uint32
	// javaInput is the Host's side of a title's own input-method field; the
	// open field itself is a runtime object. See runtime_input_method.go.
	javaInput          javaInputState
	classes            map[string]uint32
	loadedClasses      map[string]uint32
	nativeMethods      map[uint32]runtimeJavaInvocation
	nextNativeMethod   uint32
	initializedClasses map[uint32]bool
	databases          map[string]*runtimeDataBaseStore
	// The record databases are the other storage table; see
	// wipic_record_database.go for why they are kept apart from the streams.
	recordDatabases          map[string]*runtimeRecordDatabase
	recordDatabaseHandles    map[uint32]*runtimeRecordDatabaseHandle
	nextRecordDatabaseHandle uint32
	// The three detached tables hold the stores a quick load found nothing
	// for: the name was open when the checkpoint was taken and is not in the
	// store now. Such a store is empty and out of its catalog, so a question
	// about the name is answered from the store, and it is kept here by name
	// so that the name still has one host store: the next open, rename or
	// write of it takes this one. See storage_rebind.go. A Java database the
	// title deleted while an object held it is kept in its table the same
	// way (runtimeDataBaseDeleteStore); the other two are empty in a session
	// that has not been loaded into.
	detachedDatabases       map[string]*runtimeDataBaseStore
	detachedRecordDatabases map[string]*runtimeRecordDatabase
	// restoredStorage is what a checkpoint brought back and a store has not
	// filled yet. It is nil in a running session.
	restoredStorage       *restoredStorage
	displayCards          []*jvm.Object
	dockedCard            *jvm.Object
	cameraBoundsStore     uint32
	menuTextCompatibility bool
	// pendingNetCallbacks are the MC_netConnect failures owed to callers that
	// registered one; see wipic_net.go.
	relayOnline    bool
	relaySocket    *relaySocket
	pendingThreads []*jvm.Object
	// pendingSerial holds Display.callSerially Runnables. They are dispatched
	// one per idle pass rather than as fast as the Host can turn rounds; see
	// runtimeDisplayCallSerially and serialDispatchInterval.
	pendingSerial     []*jvm.Object
	serialDueAt       time.Time
	serialPaintOwners map[*jvm.Object]*jvm.Object
	activeSerialPaint *serialPaintOwner
	// paintTextReads collects the text components the card being painted
	// reads, and paintedText is that set from the last paint that returned.
	// A title that draws its own name field reads it there; see
	// drawnTextComponent. lastPaintedText is the set from the last paint that
	// read any, which the relaxed rule keeps, and lastPaintedCard the card
	// that painted it; see relaxedDrawnTextComponent.
	paintTextReads    []*jvm.Object
	paintTextOverflow bool
	paintedText       []*jvm.Object
	lastPaintedText   []*jvm.Object
	lastPaintedCard   *jvm.Object
	// shellWalkSeen is the visited set of a walk through a shell's
	// containers, empty between walks; see shellTextFields.
	shellWalkSeen map[*jvm.Object]bool
	// guestHasPainted records that the guest has painted a frame itself —
	// Card.repaint and then Card.serviceRepaints — and roundsSinceGuestPaint
	// how many Host rounds ago. A title that drives its own painting is asking
	// for exactly the frames it wants, and the Host's own unconditional round
	// paint stops being a service and becomes an extra frame. See paintTopCard
	// for the window these are read against and why there is one.
	guestHasPainted       bool
	roundsSinceGuestPaint int
	jletListeners         []*jvm.Object
	grabbedKeys           map[int32]*jvm.Object
	kfcOwnedKeys          map[int32]kfcKeyOwner
	activeJlet            *jvm.Object
	// destroyCallbackStarted makes the destroyed transition one-way. It is
	// set before entering guest cleanup so a callback that exits or fails is
	// never repeated by a second Host close.
	destroyCallbackStarted bool
	// fileBatch holds the File writes one guest DataOutputStream call makes
	// until that call returns; see guestFileBatch in runtime_file.go.
	fileBatch *guestFileBatch
	// removedDatabaseLists holds the deletion lists the two storage tables
	// keep, by save key. See recordDatabaseRemovals in wipic_record_database.go.
	removedDatabaseLists map[string]map[string]bool
	// madeGuestDirectories is the same for FileSystem.mkdir, and
	// databaseSizes the record size each Java database was created with. See
	// storage_contract.go.
	madeGuestDirectories map[string]bool
	databaseSizes        map[string]int32
	// clips holds each org.kwis.msp.media.Clip's sound bytes and, once played,
	// its loaded handle. See runtime_media.go for why they are not guest fields.
	clips              map[weak.Pointer[jvm.Object]]*javaClipState
	mediaEvents        []clipEvent
	imageSurfaces      map[weak.Pointer[jvm.Object]]uint32
	runtimeObjects     map[string]*jvm.Object
	classAliases       map[uint32]uint32
	nativeReturnScopes map[*armcore.Thread]*nativeReturnScope
	resultBindingDepth uint32
	kernelInterface    uint32
	javaInterface      uint32
	wipicInterface     uint32
	moduleInterface    uint32
	// moduleContext is the block `fp` points at while an older module runs,
	// and moduleClassByName / linkedModuleClasses are what its published
	// class table becomes. See module_link.go.
	moduleContext       uint32
	moduleJumpTable     uint32
	moduleClassByName   map[string]uint32
	linkedModuleClasses map[uint32]bool
	jvmContext          uint32
	classArena          *wipicrt.GuestArena
	exceptionContext    uint32
	callbacks           InitializationCallbacks
	// objects tracks live guest object allocations for the collector, and
	// collectAt is the arena size that triggers the next cycle.
	objects map[uint32]objectRecord
	// classSummaries caches the name, superclass, and interfaces of guest class
	// records for the type check, which walks them on every instanceof.
	classSummaries map[uint32]aotClassSummary
	// aotClassReads keeps, for each class record registerAOTClass registered,
	// what reading the record read, and aotClassReadBytes what they cost
	// together; aotReading collects one while a read runs. See
	// registerAOTClass.
	aotClassReads     map[uint32]*aotClassRead
	aotClassReadBytes int
	aotReading        *aotClassRead
}

func newInitializationRuntime(client *Client) (*initializationRuntime, error) {
	if client == nil || client.Client == nil || client.Core() == nil || client.vm == nil {
		return nil, fmt.Errorf("KTF client is not initialized")
	}
	runtime := &initializationRuntime{
		client:         client,
		objects:        make(map[uint32]objectRecord),
		classes:        make(map[string]uint32),
		loadedClasses:  make(map[string]uint32),
		nativeMethods:  make(map[uint32]runtimeJavaInvocation),
		runtimeObjects: make(map[string]*jvm.Object),
		classAliases:   make(map[uint32]uint32),
		grabbedKeys:    make(map[int32]*jvm.Object),
	}
	engine, err := wipicrt.New(client.Client, runtime.engineHooks())
	if err != nil {
		return nil, err
	}
	runtime.Runtime = engine
	if err := runtime.registerRuntimeJavaNatives(); err != nil {
		return nil, err
	}
	client.vm.SetContextAOTInvoker(runtime.invokeAOTFromJVMContext)
	return runtime, nil
}

// invokeAOTFromJVM runs a guest AOT method on behalf of interpreted bytecode.
// It nests on the guest thread suspended at the current supervisor call, so
// it is only reachable while a supervisor call is being handled.
// hasAOTMethod reports whether a class carries a method, by the same two-step
// lookup invokeAOTFromJVM uses: the registry first, then the guest's own class
// records, which reach further because the registry stops at the first
// superclass the title has not registered yet.
//
// It exists for the optional calls — a Jlet's lifecycle callbacks — where a
// method that is not there is nothing to call rather than a failure, and
// "method not found" would otherwise have to be recognised from an error
// string.
func (runtime *initializationRuntime) hasAOTMethod(className, name, descriptor string) bool {
	metadata, ok := runtime.client.vm.AOTClass(className)
	if !ok {
		return false
	}
	if _, found, err := runtime.client.vm.FindAOTMethod(metadata.Address, name, descriptor); err == nil && found {
		return true
	}
	_, found, err := runtime.aotMethodFromGuestRecords(metadata.Address, name, descriptor)
	return err == nil && found
}

type jvmInvocationKey struct{}

func (runtime *initializationRuntime) invokeAOTFromJVMContext(call *jvm.Invocation, className, name, descriptor string, arguments []jvm.Value) (jvm.Value, error) {
	previous := runtime.CurrentContext
	if previous == nil {
		return jvm.VoidValue(), fmt.Errorf("KTF JVM reentry has no guest context")
	}
	runtime.CurrentContext = context.WithValue(previous, jvmInvocationKey{}, call)
	defer func() { runtime.CurrentContext = previous }()
	return runtime.invokeAOTFromJVM(className, name, descriptor, arguments)
}

func (runtime *initializationRuntime) invokeAOTFromJVM(className, name, descriptor string, arguments []jvm.Value) (jvm.Value, error) {
	thread, ctx := runtime.CurrentThread, runtime.CurrentContext
	if thread == nil || ctx == nil {
		return jvm.VoidValue(), fmt.Errorf("KTF AOT method %s.%s%s invoked without an active guest thread", className, name, descriptor)
	}
	runtime.RecordDiagnostic(wipicrt.DiagEvent{Kind: wipicrt.DiagJVMToAOT, Name: className, Target: name, Descriptor: descriptor})
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
	result, _, err := runtime.runAOTMethod(ctx, thread, className, method, methodType, raw)
	if err != nil {
		if errors.Is(err, armcore.ErrThreadState) {
			runtime.CountDiagnostic(fmt.Sprintf("jvm->aot state error: current=%p state=%s root=%p root-state=%s", thread, thread.State(), runtime.client.Thread, runtime.client.Thread.State()))
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
	runtime.jvmContext, err = runtime.prepareClassContext()
	if err != nil {
		return nil, err
	}
	runtime.exceptionContext, err = runtime.AllocateWords(make([]uint32, javaExceptionContextWords))
	if err != nil {
		return nil, err
	}
	// A module's handler head is a field of a context that does not exist
	// yet, so it registers its own; see prepareModuleContext.
	if !runtime.client.module {
		if err := runtime.client.Core().RegisterThreadLocalWord(runtime.exceptionHead()); err != nil {
			return nil, fmt.Errorf("register KTF Java exception handler head: %w", err)
		}
		for _, offset := range []uint32{nativeReturnTagOffset, nativeReturnValueOffset} {
			if err := runtime.client.Core().RegisterThreadLocalWord(runtime.exceptionContext + offset); err != nil {
				return nil, fmt.Errorf("register KTF native return word: %w", err)
			}
		}
	}
	param0, err := runtime.AllocateWords([]uint32{0})
	if err != nil {
		return nil, err
	}
	param1, err := runtime.AllocateWords([]uint32{runtime.exceptionContext})
	if err != nil {
		return nil, err
	}
	param3, err := runtime.AllocateWords([]uint32{
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
	getInterface, err := runtime.Stub(svcCategoryInit, initSVCGetInterface)
	if err != nil {
		return 0, err
	}
	javaThrow, err := runtime.Stub(svcCategoryInit, initSVCJavaThrow)
	if err != nil {
		return 0, err
	}
	javaThrowObject, err := runtime.Stub(svcCategoryInit, initSVCJavaThrowObject)
	if err != nil {
		return 0, err
	}
	javaCheckType, err := runtime.Stub(svcCategoryInit, initSVCJavaCheckType)
	if err != nil {
		return 0, err
	}
	javaNew, err := runtime.Stub(svcCategoryInit, initSVCJavaNew)
	if err != nil {
		return 0, err
	}
	javaArrayNew, err := runtime.Stub(svcCategoryInit, initSVCJavaArrayNew)
	if err != nil {
		return 0, err
	}
	javaClassLoad, err := runtime.Stub(svcCategoryInit, initSVCJavaClassLoad)
	if err != nil {
		return 0, err
	}
	allocate, err := runtime.Stub(svcCategoryInit, initSVCAlloc)
	if err != nil {
		return 0, err
	}
	return runtime.AllocateWords([]uint32{
		getInterface,
		javaThrow,
		javaThrowObject,
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
		stub, err := runtime.Stub(wipicrt.SVCCategoryWIPIC, uint32(index))
		if err != nil {
			return 0, err
		}
		words[index] = stub
	}
	return runtime.AllocateWords(words)
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
		stub, err := runtime.Stub(svcCategoryJavaInterface, id)
		if err != nil {
			return 0, err
		}
		words[index+1] = stub
	}
	return runtime.AllocateWords(words)
}

func (runtime *initializationRuntime) makeWIPICInterface() (uint32, error) {
	words := make([]uint32, 17)
	for index := range words {
		tableID := uint32(index + 1)
		entries := make([]uint32, wipicrt.WIPICTableFunctions)
		for function := range entries {
			stub, err := runtime.Stub(wipicrt.SVCCategoryWIPIC, tableID<<16|uint32(function))
			if err != nil {
				return 0, err
			}
			entries[function] = stub
		}
		table, err := runtime.AllocateWords(entries)
		if err != nil {
			return 0, err
		}
		words[index] = table
	}
	return runtime.AllocateWords(words)
}

func (runtime *initializationRuntime) handleSupervisorCall(ctx context.Context, thread *armcore.Thread, call armcore.SupervisorCall) error {
	id, err := thread.Register(12)
	if err != nil {
		return err
	}
	// Interpreted JVM code reached from this handler may dispatch back into
	// guest AOT methods; it nests on the suspended thread recorded here.
	previousThread, previousContext := runtime.CurrentThread, runtime.CurrentContext
	runtime.CurrentThread, runtime.CurrentContext = thread, ctx
	defer func() {
		runtime.CurrentThread, runtime.CurrentContext = previousThread, previousContext
	}()
	// A guest DataOutputStream call holds its File writes until it returns;
	// see guestFileBatch.
	finishFileBatch := runtime.enterGuestFileCall(call.Immediate, id)
	var result uint32
	switch call.Immediate {
	case svcCategoryCameraBounds:
		return runtime.storeCameraBounds(thread, call)
	case svcCategoryMenuText:
		return runtime.storeMenuTextY(thread, call)
	case svcCategoryInit:
		result, err = runtime.handleInitCall(thread, id)
	case svcCategoryJavaInterface:
		result, err = runtime.handleJavaCall(ctx, thread, id)
		var unwind *aotExceptionUnwind
		if errors.As(err, &unwind) {
			return runtime.resumeAOTException(thread, unwind)
		}
	case wipicrt.SVCCategoryWIPIC:
		result, err = runtime.HandleWIPICCall(thread, id)
	case svcCategoryBinaryHook:
		result, err = runtime.handleBinaryHookCall(thread, id)
	case svcCategoryModuleInterface:
		result, err = runtime.handleModuleInterfaceCall(ctx, thread, id)
		var unwind *aotExceptionUnwind
		if errors.As(err, &unwind) {
			return runtime.resumeAOTException(thread, unwind)
		}
	case svcCategoryModuleJump:
		// A module's own call is a Host call like any other, so an exception
		// thrown under one has to be able to leave it: without this a long
		// jump out of a nested module method stopped at the frame that made
		// the call instead of at the frame that caught it.
		result, err = runtime.handleModuleJumpCall(ctx, thread, id)
		var jumpUnwind *aotExceptionUnwind
		if errors.As(err, &jumpUnwind) {
			return runtime.resumeAOTException(thread, jumpUnwind)
		}
	case svcCategoryRuntimeJava:
		result, err = runtime.handleRuntimeJavaCall(thread, id)
		if storeErr := finishFileBatch(); storeErr != nil && err == nil {
			// The call's writes were not stored, so it throws what a write
			// the store refused has always thrown.
			guest, _ := newGuestIOException(storeErr.Error()).(*jvm.GuestException)
			result, err = 0, runtime.raiseGuestException(thread, guest)
		}
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
		name, err := runtime.ReadCString(address, 128)
		if err != nil {
			return 0, fmt.Errorf("read KTF interface name: %w", err)
		}
		switch name {
		case "WIPIC_knlInterface":
			return runtime.kernelInterface, nil
		case "WIPI_JBInterface":
			return runtime.javaInterface, nil
		case moduleInterfaceName:
			return runtime.makeModuleInterface()
		default:
			return 0, nil
		}
	case initSVCJavaThrow:
		return runtime.throwAOTException(thread)
	case initSVCJavaThrowObject:
		return runtime.throwAOTExceptionObject(thread)
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
		return runtime.Allocate(uint64(size))
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
		// The jump number says how many arguments the call carries, and the
		// call site is what says which call it is: a null target is a method
		// pointer the guest read out of its own tables, so the answer is in
		// the code that loaded it rather than here. Naming the receiver's
		// class narrows it further when there is one.
		return 0, fmt.Errorf("KTF Java jump %d target is null, receiver %s%s",
			id-javaSVCJump1+1, runtime.describeAOTWord(registers[0]), runtime.CallerSite(thread))
	}
	if lr, lrErr := thread.Register(armcore.RegisterLR); lrErr == nil {
		runtime.RecordDiagnostic(wipicrt.DiagEvent{
			Kind:    wipicrt.DiagJump,
			Nums:    [5]uint32{id - javaSVCJump1 + 1, address, arguments[0], arguments[1], arguments[2]},
			Site:    lr,
			HasSite: true,
		})
	}
	if err := runtime.EnterAOTCall(); err != nil {
		return 0, err
	}
	defer runtime.LeaveAOTCall(runtime.AOTCallOwner())
	summary, err := runtime.client.Core().Call(ctx, thread, address, wipicrt.ReturnAddress, arguments, runtime.handleSupervisorCall)
	if err != nil {
		return 0, fmt.Errorf("execute KTF AOT Java jump %#x: %w", id, err)
	}
	return runtime.completeAOTJump(thread, summary)
}

func (runtime *initializationRuntime) completeAOTJump(thread *armcore.Thread, summary armcore.RunSummary) (uint32, error) {
	// A `long` or a `double` comes back in both result registers, and the call
	// ran on a context of its own, so r1 has to be carried over as well as r0
	// — a branch-and-link would have left the caller looking at what the
	// callee wrote. Without it every 64-bit answer arrived truncated to its
	// low word: a title that stamped System.currentTimeMillis into a string
	// read a ten-digit number where the handset gives thirteen.
	if err := thread.SetRegister(1, summary.Context.Registers[1]); err != nil {
		return 0, err
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
	if err := runtime.client.Core().Memory().Read(dataAddress, original[:]); err != nil {
		return 0, fmt.Errorf("read KTF AOT native call data at %#x: %w", dataAddress, err)
	}
	if err := runtime.client.Core().Memory().Write(dataAddress, original[:]); err != nil {
		return 0, fmt.Errorf("validate writable KTF AOT native call data at %#x: %w", dataAddress, err)
	}
	if err := runtime.EnterAOTCall(); err != nil {
		return 0, err
	}
	defer runtime.LeaveAOTCall(runtime.AOTCallOwner())

	returnScope, err := runtime.beginNativeReturn(thread)
	if err != nil {
		return 0, fmt.Errorf("prepare KTF native return: %w", err)
	}
	defer returnScope.restore()

	summary, err := runtime.client.Core().Call(
		ctx,
		thread,
		address,
		wipicrt.ReturnAddress,
		[]uint32{dataAddress, dataAddress},
		runtime.handleSupervisorCall,
	)
	if err != nil {
		return 0, fmt.Errorf("execute KTF AOT native call %s: %w", runtime.describeNativeCall(address), err)
	}
	return runtime.completeAOTNative(address, dataAddress, returnScope, summary)
}

func (runtime *initializationRuntime) completeAOTNative(address, dataAddress uint32, returnScope *nativeReturnScope, summary armcore.RunSummary) (uint32, error) {
	// The container is eight bytes because an answer can be sixty-four bits
	// wide, and only then is the second word the high half: a title that
	// stamped System.currentTimeMillis into a string read a ten-digit number
	// where the handset gives thirteen. Every other native leaves it zero,
	// because r1 after a call that returns one word is whatever the callee
	// happened to be holding rather than part of its answer.
	var result [8]byte
	low, err := returnScope.result(summary.Context.Registers[0])
	if err != nil {
		return 0, fmt.Errorf("read KTF native return: %w", err)
	}
	binary.LittleEndian.PutUint32(result[:4], low)
	if runtime.nativeCallIsWide(address) {
		binary.LittleEndian.PutUint32(result[4:], summary.Context.Registers[1])
	}
	if err := runtime.client.Core().Memory().Write(dataAddress, result[:]); err != nil {
		return 0, fmt.Errorf("write KTF AOT native call result at %#x: %w", dataAddress, err)
	}
	return dataAddress, nil
}

// ownsUnwind reports whether a fresh guest call made on thread would own the
// frame a long jump lands on. A resumption loop asks this before restarting
// the guest, because a call it starts inherits thread's stack pointer: a
// handler above that belongs further out, and the unwind has to keep
// travelling rather than be answered here.
func (runtime *initializationRuntime) ownsUnwind(thread *armcore.Thread, unwind *aotExceptionUnwind) bool {
	stackPointer, err := thread.Register(armcore.RegisterSP)
	if err != nil {
		return true
	}
	return unwind.resumableFrom(stackPointer)
}

func (runtime *initializationRuntime) resumeAOTException(thread *armcore.Thread, unwind *aotExceptionUnwind) error {
	if unwind == nil {
		return fmt.Errorf("KTF AOT Java exception unwind is nil")
	}
	// A long jump that lands outside this call has to leave it. Returning the
	// unwind hands it to the next Host call out, which asks the same question
	// of its own entry until one of them owns the frame; the outermost guest
	// call owns the whole stack, so the walk always ends.
	if entry, known := thread.EntryStackPointer(); known && !unwind.resumableFrom(entry) {
		return unwind
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

// callerSite renders the guest call site of the platform stub currently being
// serviced, for error messages that would otherwise name only a slot number.
// describeGuestWord names what a word the guest handed the platform is, as far
// as the runtime can tell: a bound object says its class, and anything else is
// reported as the number it is. It exists for failure messages, where the
// difference between "a null" and "a reference to something" is the whole
// question.
func (runtime *initializationRuntime) describeAOTWord(word uint32) string {
	if word == 0 {
		return "null"
	}
	if object, bound := runtime.client.vm.AOTObject(word); bound && object != nil {
		return fmt.Sprintf("%s at %#x", object.ClassName, word)
	}
	if class, ok := runtime.client.vm.AOTClassAt(word); ok {
		return fmt.Sprintf("class %s at %#x", class.Name, word)
	}
	return fmt.Sprintf("%#x", word)
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
	name, err := runtime.ReadCString(nameAddress, 512)
	if err != nil {
		return 0, fmt.Errorf("read KTF Java class name: %w", err)
	}
	runtime.CountDiagnostic("load " + name)
	class, err := runtime.ensureJavaClass(name)
	if err != nil {
		return 0, err
	}
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], class)
	if err := runtime.client.Core().Memory().Write(target, data[:]); err != nil {
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
	// table still resolves" in docs/history/ktf.md.
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
	vtableAddress, err := runtime.AllocateWords(append(vtable.pointers(), 0))
	if err != nil {
		return 0, err
	}
	namePointer, err := runtime.AllocateBytes(append([]byte(name), 0))
	if err != nil {
		return 0, err
	}
	// An array has no fields, and the guest's array-store check spends that word
	// on the element class instead: it reads the object header's class record,
	// takes the descriptor at +8, and loads +0x14 as the class to check the
	// stored value against. Leaving it zero asks the check about a null class,
	// which can only be answered with the permissive yes. See "Three ways a
	// class record was not a class, and the two titles they stopped" in
	// docs/history/ktf.md, "The array store check was asked about a null class".
	elementClass, err := runtime.arrayElementClass(name)
	if err != nil {
		return 0, err
	}
	descriptorData := make([]byte, javaDescriptorSize)
	binary.LittleEndian.PutUint32(descriptorData[0:], namePointer)
	binary.LittleEndian.PutUint32(descriptorData[8:], parent)
	binary.LittleEndian.PutUint32(descriptorData[20:], elementClass)
	binary.LittleEndian.PutUint16(descriptorData[28:], 0x21)
	descriptor, err := runtime.AllocateBytes(descriptorData)
	if err != nil {
		return 0, err
	}
	class, err := runtime.Allocate(javaClassSize)
	if err != nil {
		return 0, err
	}
	classData := make([]byte, javaClassSize)
	binary.LittleEndian.PutUint32(classData[0:], class+4)
	binary.LittleEndian.PutUint32(classData[8:], descriptor)
	binary.LittleEndian.PutUint32(classData[12:], vtableAddress)
	binary.LittleEndian.PutUint16(classData[16:], uint16(len(vtable.entries)))
	binary.LittleEndian.PutUint16(classData[18:], 8)
	if err := runtime.client.Core().Memory().Write(class, classData); err != nil {
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
		if err := runtime.client.Core().Memory().Read(address, data[:]); err != nil {
			return 0, fmt.Errorf("read KTF Java string length at %#x: %w", address, err)
		}
		length = uint32(binary.LittleEndian.Uint16(data[:]))
		address += 2
	}
	if length > maxJavaStringUnits {
		return 0, fmt.Errorf("KTF Java string length %d exceeds %d", length, maxJavaStringUnits)
	}
	data := make([]byte, uint64(length)*2)
	if err := runtime.client.Core().Memory().Read(address, data); err != nil {
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

// runtimeJavaNativeAt answers the runtime Java method whose stub a native
// call entered at address. Only a method this platform implements is found: a
// native compiled into the module has no stub here. The stub is found by its
// address in stubKeys; every stub has an address of its own. It used to be
// found by walking every stub the runtime had made, once for every native call
// that ended, and on a title that calls natives that often the walk was half
// of the server's CPU.
func (runtime *initializationRuntime) runtimeJavaNativeAt(address uint32) (runtimeJavaMethod, bool) {
	key, ok := runtime.StubKeys[address]
	if !ok || uint32(key>>32) != svcCategoryRuntimeJava {
		return runtimeJavaMethod{}, false
	}
	invocation, ok := runtime.nativeMethods[uint32(key)]
	return invocation.method, ok
}

// nativeCallIsWide reports whether the native call that entered at address
// answers sixty-four bits: a runtime Java method whose return type occupies
// two words. A native compiled into the module returns through the module's
// own convention, and nothing here knows its descriptor, so it is never wide.
// Only the return type is read out of the descriptor, which every native call
// that ends asks for, and which the whole parse would first look up by a hash
// of the descriptor.
func (runtime *initializationRuntime) nativeCallIsWide(address uint32) bool {
	method, ok := runtime.runtimeJavaNativeAt(address)
	if !ok {
		return false
	}
	returnType, err := jvm.ReturnTypeOf(method.descriptor)
	return err == nil && returnType.Slots() == 2
}

// describeNativeCall names the body a native call is entering. Most of these
// addresses are this runtime's own stubs, and a report that gives the address
// alone leaves the one question a nested failure raises — which method is
// re-entering — answerable only by reading the allocator's order. Every Java
// exception that unwinds through a runtime Java native is reported here on its
// way, so the method is found as nativeCallIsWide finds it.
func (runtime *initializationRuntime) describeNativeCall(address uint32) string {
	if method, ok := runtime.runtimeJavaNativeAt(address); ok {
		return fmt.Sprintf("%s.%s%s at %#x", method.class, method.name, method.descriptor, address)
	}
	return fmt.Sprintf("at %#x", address)
}

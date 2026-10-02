package ktf

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestNativeReturnCaptureSurvivesScopeReplacement(t *testing.T) {
	client, runtime := newTestRuntime(t)
	thread := armcore.NewThread(armcore.NewContext())
	if err := client.core.SetThreadLocalWord(thread, runtime.exceptionContext+nativeReturnTagOffset, 2); err != nil {
		t.Fatal(err)
	}
	if err := client.core.SetThreadLocalWord(thread, runtime.exceptionContext+nativeReturnValueOffset, 75); err != nil {
		t.Fatal(err)
	}
	scope, err := runtime.beginNativeReturn(thread)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := runtime.captureNativeReturn(thread)
	if err != nil || saved.Tag != 2 || saved.Value != 75 {
		t.Fatalf("native return capture = %+v, %v", saved, err)
	}
	scope.restore()
	if len(runtime.nativeReturnScopes) != 0 {
		t.Fatal("completed native scope remained registered")
	}
	freshClient, fresh := newTestRuntime(t)
	freshThread := armcore.NewThread(armcore.NewContext())
	if err := freshClient.core.SetThreadLocalWord(freshThread, fresh.exceptionContext+nativeReturnTagOffset, 2); err != nil {
		t.Fatal(err)
	}
	if err := freshClient.core.SetThreadLocalWord(freshThread, fresh.exceptionContext+nativeReturnValueOffset, 17); err != nil {
		t.Fatal(err)
	}
	resumed, err := fresh.resumeNativeReturn(freshThread, saved)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := resumed.result(99); err != nil || value != 17 {
		t.Fatalf("restoration erased the current native answer: %d, %v", value, err)
	}
	resumed.restore()
	if got, err := freshClient.core.ThreadLocalWord(freshThread, fresh.exceptionContext+nativeReturnValueOffset); err != nil || got != 75 {
		t.Fatalf("restored parent environment = %d, %v", got, err)
	}
	if len(fresh.nativeReturnScopes) != 0 {
		t.Fatal("restored native scope remained registered")
	}
}

// This fixture isolates parked ARM/native calls. A fresh VM registers its native
// implementations; guest memory, objects and runtime metadata come from data.
// It does not stand in for a session codec.
type continuationFixture struct {
	client           *Client
	runtime          *initializationRuntime
	clock            *ManualClock
	object           *jvm.Object
	probe, container uint32
	handler          uint32
	options          continuationFixtureOptions
}

type continuationFixtureOptions struct {
	ObjectWait   bool
	Throw        bool
	Runnable     bool
	Graphics     bool
	Storage      bool
	Platform     bool
	Relay        bool
	Module       bool
	ModuleNative bool
	ModuleWait   bool
}

func newContinuationFixture(t *testing.T, epoch int64, options continuationFixtureOptions) continuationFixture {
	t.Helper()
	fresh := newContinuationRestoreFixture(t, epoch, options)
	client, runtime, clock := fresh.client, fresh.runtime, fresh.clock
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}
	if options.Module {
		var err error
		client.moduleSegment, err = runtime.allocate(moduleContextSlotOffset + 4)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.prepareModuleContext(); err != nil {
			t.Fatal(err)
		}
	}
	probe, err := runtime.allocate(16)
	if err != nil {
		t.Fatal(err)
	}
	container, err := runtime.allocateWords([]uint32{123, 456})
	if err != nil {
		t.Fatal(err)
	}
	objectAddress, err := runtime.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	method := runtimeJavaMethod{class: "java/lang/Thread", name: "sleep", descriptor: "(J)V", accessFlags: 9}
	if options.ObjectWait {
		method = runtimeJavaMethod{class: jvm.ObjectClass, name: "wait", descriptor: "(J)V", accessFlags: 1}
	}
	_, err = runtime.runtimeJavaStub(method, false)
	if err != nil {
		t.Fatal(err)
	}
	sleepID := runtime.nextNativeMethod
	if sleepID > 255 {
		t.Fatal("fixture sleep ID needs an immediate")
	}
	var exceptionName, handler uint32
	if options.Throw {
		exceptionName, err = runtime.allocateBytes([]byte("java/lang/Error\x00"))
		if err != nil {
			t.Fatal(err)
		}
		catchClass, err := runtime.ensureJavaClass("java/lang/Error")
		if err != nil {
			t.Fatal(err)
		}
		catch := fixtureARM(t, runtime, func(code *continuationARM) {
			code.literal(4, probe)
			code.literal(13, workerStackBase+uint32(ThreadStackSize)-24)
			code.emit(0xe3a0004d, 0xe5840004, 0xe28dd010, 0xe8bd8010)
		})
		entry, err := runtime.allocateWords([]uint32{0x100, 0x110, catch, catchClass})
		if err != nil {
			t.Fatal(err)
		}
		table, err := runtime.allocateWords([]uint32{entry})
		if err != nil {
			t.Fatal(err)
		}
		methodData := make([]byte, javaMethodSize)
		binary.LittleEndian.PutUint32(methodData[8:], table)
		binary.LittleEndian.PutUint16(methodData[16:], 1)
		methodAddress, err := runtime.allocateBytes(methodData)
		if err != nil {
			t.Fatal(err)
		}
		functions, err := runtime.allocateWords([]uint32{0, catch})
		if err != nil {
			t.Fatal(err)
		}
		words := make([]uint32, javaExceptionHandler/4)
		words[0], words[runtime.exceptionLabel()/4], words[5] = methodAddress, 0x105, functions
		words[runtime.exceptionFrameStack()/4] = workerStackBase + uint32(ThreadStackSize) - 8
		handler, err = runtime.allocateWords(words)
		if err != nil {
			t.Fatal(err)
		}
	}
	inner := fixtureARM(t, runtime, func(code *continuationARM) {
		code.emit(0xe92d4010, 0xe24dd010) // push {r4,lr}; sub sp,sp,#16
		code.literal(4, probe)
		code.emit(0xe5940008, 0xe2800001, 0xe5840008)
		if !options.Module {
			code.literal(4, runtime.exceptionContext)
			code.emit(0xe3a0002a, 0xe5840028, 0xe3a00002, 0xe5840024)
		}
		for i := 0; i < 2; i++ {
			if options.ModuleWait {
				code.literal(0, objectAddress)
				code.emit(0xe3a0c000|moduleJumpWait, 0xef000000|svcCategoryModuleJump)
			} else {
				if options.ObjectWait {
					code.literal(1, objectAddress)
					code.emit(0xe3a0201e, 0xe3a03000)
				} else {
					code.emit(0xe3a0101e, 0xe3a02000)
				}
				code.emit(0xe3a0c000|sleepID, 0xef000000|svcCategoryRuntimeJava)
			}
			code.literal(4, probe)
			code.emit(0xe594000c, 0xe2800001, 0xe584000c)
		}
		if options.Throw {
			code.literal(0, exceptionName)
			code.emit(0xe3a0c000|initSVCJavaThrow, 0xef000000|svcCategoryInit)
		}
		code.emit(0xe3a00063, 0xe3a01007, 0xe28dd010, 0xe8bd8010)
	})
	var moduleMethod uint32
	if options.Module {
		name, err := runtime.allocateBytes([]byte("\x00()J+result\x00"))
		if err != nil {
			t.Fatal(err)
		}
		record := make([]byte, javaMethodSize)
		binary.LittleEndian.PutUint32(record, inner)
		binary.LittleEndian.PutUint32(record[8:], inner)
		binary.LittleEndian.PutUint32(record[12:], name)
		binary.LittleEndian.PutUint16(record[22:], 1)
		moduleMethod, err = runtime.allocateBytes(record)
		if err != nil {
			t.Fatal(err)
		}
	}
	outer := fixtureARM(t, runtime, func(code *continuationARM) {
		code.emit(0xe92d4010, 0xe24dd010)
		code.literal(4, probe)
		code.emit(0xe5940000, 0xe2800001, 0xe5840000)
		if options.Module {
			code.literal(0, moduleMethod)
			code.literal(1, objectAddress)
			code.emit(0xe92d000c) // push {r2,r3}, as the module's invoke helper does
			slot := uint32(moduleJumpInvoke)
			if options.ModuleNative {
				slot = moduleJumpInvokeNative
			}
			code.emit(0xe3a0c000|slot, 0xef000000|svcCategoryModuleJump)
			code.literal(2, container)
			code.emit(0xe5821004, 0xe5840004, 0xe28dd010, 0xe8bd8010)
			return
		}
		code.literal(0, inner)
		code.literal(1, container)
		code.emit(0xe3a0c000|javaSVCCallNative, 0xef000000|svcCategoryJavaInterface)
		code.emit(0xe5900000, 0xe5840004, 0xe28dd010, 0xe8bd8010)
	})
	classAddress, err := runtime.allocate(8)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.vm.RegisterAOTClass(jvm.AOTClassMetadata{Address: classAddress, Name: "test/ContinuationWorker", SuperName: jvm.ObjectClass,
		Methods: []jvm.AOTMethodMetadata{{Address: classAddress + 4, Name: "run", Descriptor: "()V", AccessFlags: 1, Body: outer}}}); err != nil {
		t.Fatal(err)
	}
	object := &jvm.Object{ClassName: "test/ContinuationWorker"}
	if err := client.vm.BindAOTObject(objectAddress, object); err != nil {
		t.Fatal(err)
	}
	if options.Runnable {
		object, err = client.vm.NewObject(jvm.ThreadClass, "(Ljava/lang/Runnable;)V", jvm.ReferenceValue(object))
		if err != nil {
			t.Fatal(err)
		}
	}
	if options.Graphics {
		graphics, err := runtime.newScreenGraphics()
		if err != nil {
			t.Fatal(err)
		}
		graphics.Native.(*runtimeGraphicsState).color = 0x1234
		bitmap := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		bitmap.SetNRGBA(0, 0, color.NRGBA{R: 17, G: 34, B: 51})
		picture := &jvm.Object{ClassName: "org/kwis/msp/lcdui/Image", Native: withOpacity(bitmap), Fields: make(map[string]jvm.Value)}
		if _, err := runtime.imageFramebufferHandle(picture); err != nil {
			t.Fatal(err)
		}
		object.SetFieldValue("snapshotGraphics", jvm.ReferenceValue(graphics))
		object.SetFieldValue("snapshotImage", jvm.ReferenceValue(picture))
	}
	if options.Storage {
		store := &runtimeDataBaseStore{name: "fixture", records: [][]byte{[]byte("saved"), nil, {}}}
		runtime.databases = map[string]*runtimeDataBaseStore{"fixture": store}
		object.SetFieldValue("snapshotDatabase", jvm.ReferenceValue(&jvm.Object{ClassName: "org/kwis/msp/db/DataBase", Native: store}))
		file := &runtimeGuestFile{name: "fixture.bin", data: []byte("cursor"), position: 2}
		object.SetFieldValue("snapshotFile", jvm.ReferenceValue(&jvm.Object{ClassName: "org/kwis/msp/io/File", Native: file}))
		object.SetFieldValue("snapshotStream", jvm.ReferenceValue(&jvm.Object{ClassName: runtimeFileInputStreamClass, Native: file}))
		cFile := &runtimeCFile{name: "fixture-c", data: []byte("stream"), packaged: 6}
		runtime.cFiles = map[string]*runtimeCFile{"fixture-c": cFile}
		runtime.cFileHandles = map[uint32]*runtimeCFileHandle{0x1003: {store: cFile, position: 2}}
		runtime.nextCDatabaseHandle = 5
		rdb := &runtimeRecordDatabase{name: "fixture-r", records: [][]byte{nil, {}, []byte("record")}, recordSize: 19}
		runtime.recordDatabases = map[string]*runtimeRecordDatabase{"fixture-r": rdb}
		runtime.recordDatabaseHandles = map[uint32]*runtimeRecordDatabaseHandle{0x2002: {store: rdb}}
		runtime.nextRecordDatabaseHandle = 8
		runtime.removedFiles = map[string]bool{"deleted": true}
	}
	if options.Platform {
		field := &jvm.Object{ClassName: "test/CheckpointEditor", Fields: make(map[string]jvm.Value)}
		client.FocusTextComponent(field)
		client.TypeKey('2')
		field.Native = client.textEditor
		object.SetFieldValue("snapshotText", jvm.ReferenceValue(field))
		runtime.dockedCard = field
		runtime.pendingSerial = []*jvm.Object{field}
		runtime.serialDueAt = client.now().Add(time.Hour)
		runtime.serialPaintOwners = map[*jvm.Object]*jvm.Object{field: object}
		runtime.runtimeObjects["snapshot-text"] = field
	}
	if options.Relay {
		socket := &relaySocket{}
		socket.input = &jvm.Object{ClassName: runtimeRelayInputClass, Native: socket}
		socket.output = &jvm.Object{ClassName: runtimeRelayOutputClass, Native: socket}
		handshake, _ := encodeRelayFrame(relayFrame{payload: slotMessage(1, 1000, []byte{30})})
		if err := socket.write(handshake); err != nil {
			t.Fatal(err)
		}
		identity, _ := encodeRelayFrame(relayFrame{payload: slotMessage(5, 1400, []byte{3, 11, 22, 33})})
		if err := socket.write(identity[:9]); err != nil {
			t.Fatal(err)
		}
		runtime.relayOnline, runtime.relaySocket = true, socket
		object.SetFieldValue("snapshotRelay", jvm.ReferenceValue(&jvm.Object{ClassName: runtimeRelaySocketClass, Native: socket}))
	}
	t.Cleanup(client.StopThreads)
	return continuationFixture{client: client, runtime: runtime, clock: clock, object: object, probe: probe, container: container, handler: handler, options: options}
}

func defineContinuationFixtureClasses(t *testing.T, client *Client, options continuationFixtureOptions) {
	t.Helper()
	if !options.Platform {
		return
	}
	if err := client.vm.DefineClass(jvm.ClassDefinition{Name: "test/CheckpointEditor", SuperName: jvm.ObjectClass, Access: jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{{Name: "run", Descriptor: "()V", Access: jvm.AccessPublic, Body: func(_ *jvm.Invocation, args []jvm.Value) (jvm.Value, error) {
			object, err := args[0].Reference()
			if err != nil {
				return jvm.VoidValue(), err
			}
			value, _ := object.Fields["serialRuns"].Int32()
			object.SetFieldValue("serialRuns", jvm.IntValue(value+1))
			return jvm.VoidValue(), nil
		}}}}); err != nil {
		t.Fatal(err)
	}
}

func newContinuationRestoreFixture(t *testing.T, epoch int64, options continuationFixtureOptions) continuationFixture {
	t.Helper()
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	clock := NewManualClock(time.Unix(epoch, 0))
	client.clock = clock
	client.module = options.Module
	client.SetSpeed(1)
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	client.runtime = runtime
	defineContinuationFixtureClasses(t, client, options)
	t.Cleanup(client.StopThreads)
	return continuationFixture{client: client, runtime: runtime, clock: clock, options: options}
}

func (fixture continuationFixture) start(t *testing.T) *guestWorker {
	t.Helper()
	var worker *guestWorker
	if fixture.options.Runnable {
		if _, err := fixture.client.vm.InvokeVirtual(fixture.object, "start", "()V"); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.client.ServiceThreads(t.Context(), 0); err != nil {
			t.Fatal(err)
		}
		if len(fixture.client.workers) != 1 {
			t.Fatal("Thread.start did not create a worker")
		}
		worker = fixture.client.workers[0]
	} else {
		var err error
		worker, err = fixture.client.newGuestWorker(fixture.object)
		if err != nil {
			t.Fatal(err)
		}
		fixture.client.workers = []*guestWorker{worker}
	}
	if !fixture.options.Module {
		for offset, value := range map[uint32]uint32{javaExceptionHead: fixture.handler, nativeReturnTagOffset: 2, nativeReturnValueOffset: 75} {
			if err := fixture.client.core.SetThreadLocalWord(worker.armThread, fixture.runtime.exceptionContext+offset, value); err != nil {
				t.Fatal(err)
			}
		}
	} else if err := fixture.client.core.SetThreadLocalWord(worker.armThread, fixture.runtime.exceptionHead(), fixture.handler); err != nil {
		t.Fatal(err)
	}
	if serviced, err := fixture.client.ServiceThreads(t.Context(), 1); err != nil || serviced != 1 {
		t.Fatalf("initial slice = %d, %v", serviced, err)
	}
	if len(fixture.client.workers) == 0 {
		t.Fatal("fixture returned instead of waiting")
	}
	if fixture.options.Runnable {
		// The target was already selected by Thread.run. Loading must finish
		// that call even when the mutable field now names something else.
		if err := fixture.client.vm.SetField(fixture.object, jvm.ThreadClass, "target", "Ljava/lang/Runnable;", jvm.ReferenceValue(nil)); err != nil {
			t.Fatal(err)
		}
	}
	return worker
}

type continuationARM struct {
	words    []uint32
	literals []struct {
		index, register int
		value           uint32
	}
}

func (code *continuationARM) emit(words ...uint32) { code.words = append(code.words, words...) }
func (code *continuationARM) literal(register int, value uint32) {
	code.literals = append(code.literals, struct {
		index, register int
		value           uint32
	}{len(code.words), register, value})
	code.emit(0)
}

func fixtureARM(t *testing.T, runtime *initializationRuntime, build func(*continuationARM)) uint32 {
	t.Helper()
	code := &continuationARM{}
	build(code)
	for _, literal := range code.literals {
		offset := 4 * (len(code.words) - literal.index - 2)
		if offset < 0 || offset > 4095 {
			t.Fatal("fixture literal is out of range")
		}
		code.words[literal.index] = 0xe59f0000 | uint32(literal.register)<<12 | uint32(offset)
		code.words = append(code.words, literal.value)
	}
	address := uint32((runtime.codeCursor + 3) &^ 3)
	data := make([]byte, len(code.words)*4)
	for i, word := range code.words {
		binary.LittleEndian.PutUint32(data[i*4:], word)
	}
	if err := runtime.client.core.Memory().Load(address, data); err != nil {
		t.Fatal(err)
	}
	runtime.codeCursor = uint64(address) + uint64(len(data))
	return address
}

type continuationFixtureState struct {
	Options       continuationFixtureOptions
	Continuation  workerAOTCheckpoint
	Core          armcore.CoreState
	Root          armcore.RootThreadState
	Heap          runtimeHeapState
	Metadata      runtimeMetadataState
	Probe         uint32
	Container     uint32
	Handler       uint32
	StackBase     uint32
	WaitRemaining time.Duration
}

func (fixture continuationFixture) capture(t *testing.T) continuationFixtureState {
	t.Helper()
	client := fixture.client
	client.run.Lock()
	defer client.run.Unlock()
	if len(client.workers) != 1 {
		t.Fatalf("parked workers = %d", len(client.workers))
	}
	worker := client.workers[0]
	continuation, err := fixture.runtime.captureWorkerContinuation(worker)
	if err != nil {
		t.Fatal(err)
	}
	coreState, err := client.core.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	root, err := client.core.CaptureRootThread(worker.armThread)
	if err != nil {
		t.Fatal(err)
	}
	heap, err := fixture.runtime.captureHeapState([]*jvm.Object{worker.javaThread})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := fixture.runtime.captureMetadataState()
	if err != nil {
		t.Fatal(err)
	}
	saved := continuationFixtureState{Options: fixture.options, Continuation: continuation, Core: coreState, Root: root,
		Heap: heap, Metadata: metadata, Probe: fixture.probe, Container: fixture.container, Handler: fixture.handler,
		StackBase: worker.stackBase, WaitRemaining: worker.wakeAt.Sub(client.now())}
	return saved
}

func (fixture *continuationFixture) restore(t *testing.T, saved continuationFixtureState) *guestWorker {
	t.Helper()
	client := fixture.client
	if client.core.Steps() != 0 {
		t.Fatal("fresh fixture executed guest startup")
	}
	core, err := armcore.NewCoreFromState(saved.Core, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	root, err := core.RestoreRootThread(saved.Root, defaultThreadSliceSteps)
	if err != nil {
		t.Fatal(err)
	}
	client.core = core
	if err := fixture.runtime.restoreMetadataState(saved.Metadata); err != nil {
		t.Fatal(err)
	}
	roots, err := fixture.runtime.restoreHeapState(saved.Heap)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0] == nil {
		t.Fatal("restored worker root is missing")
	}
	fixture.object = roots[0]
	fixture.probe, fixture.container, fixture.handler = saved.Probe, saved.Container, saved.Handler
	if fixture.options.Graphics {
		fixture.checkGraphics(t)
	}
	if fixture.options.Storage {
		fixture.checkStorage(t)
	}
	if fixture.options.Platform {
		fixture.checkPlatformRoots(t)
	}
	if fixture.options.Relay {
		fixture.checkRelay(t)
	}
	worker := &guestWorker{javaThread: fixture.object, armThread: root, stackBase: saved.StackBase,
		grant: make(chan struct{}), events: make(chan workerEvent, 1), finished: make(chan struct{}), wakeAt: client.now().Add(saved.WaitRemaining), continuation: &saved.Continuation}
	worker.armThread.SetStepBudget(defaultThreadSliceSteps)
	worker.armThread.SetLimitHook(func(context.Context) error { return worker.parkSlice(true) })
	client.workers = []*guestWorker{worker}
	client.workerStackCount = 1
	go worker.run(client)
	return worker
}

func TestAOTCallCheckpointResumesNativeReturnAndSecondWait(t *testing.T) {
	for _, test := range []struct {
		name    string
		options continuationFixtureOptions
	}{
		{"sleep", continuationFixtureOptions{}},
		{"wait", continuationFixtureOptions{ObjectWait: true}},
		{"exception", continuationFixtureOptions{Throw: true}},
		{"runnable", continuationFixtureOptions{Runnable: true}},
		{"runnable exception", continuationFixtureOptions{Runnable: true, Throw: true}},
		{"graphics", continuationFixtureOptions{Graphics: true}},
		{"storage", continuationFixtureOptions{Storage: true}},
		{"platform", continuationFixtureOptions{Platform: true}},
		{"relay", continuationFixtureOptions{Relay: true}},
		{"module compiled", continuationFixtureOptions{Module: true, Runnable: true}},
		{"module native", continuationFixtureOptions{Module: true, ModuleNative: true}},
		{"module compiled wait", continuationFixtureOptions{Module: true, ModuleWait: true}},
		{"module native wait", continuationFixtureOptions{Module: true, ModuleNative: true, ModuleWait: true, Runnable: true}},
		{"module compiled exception", continuationFixtureOptions{Module: true, Throw: true}},
		{"module native exception", continuationFixtureOptions{Module: true, ModuleNative: true, ModuleWait: true, Throw: true}},
	} {
		t.Run(test.name, func(t *testing.T) { testAOTCallCheckpoint(t, test.options) })
	}
}

func (fixture continuationFixture) checkRelay(t *testing.T) {
	t.Helper()
	socket := fixture.runtime.relaySocket
	object, _ := fixture.object.Fields["snapshotRelay"].Reference()
	if socket == nil || object == nil || object.Native != socket || !fixture.runtime.relayOnline || socket.service.phase != 1 || len(socket.pending) != 9 || len(socket.incoming) == 0 || socket.input == nil || socket.output == nil || socket.input.Native != socket || socket.output.Native != socket {
		t.Fatal("restored relay sharing or queued bytes differ")
	}
}

func (fixture continuationFixture) finishRelay(t *testing.T) {
	t.Helper()
	identity, _ := encodeRelayFrame(relayFrame{payload: slotMessage(5, 1400, []byte{3, 11, 22, 33})})
	if err := fixture.runtime.relaySocket.write(identity[9:]); err != nil {
		t.Fatal(err)
	}
	if fixture.runtime.relaySocket.service.phase != 2 || len(fixture.runtime.relaySocket.pending) != 0 {
		t.Fatal("restored relay did not finish its pending frame")
	}
}

func (fixture continuationFixture) checkPlatformRoots(t *testing.T) {
	t.Helper()
	field, _ := fixture.object.Fields["snapshotText"].Reference()
	if field == nil || fixture.client.focusedText != field || fixture.runtime.dockedCard != field || fixture.runtime.runtimeObjects["snapshot-text"] != field || len(fixture.runtime.pendingSerial) != 1 || fixture.runtime.pendingSerial[0] != field || fixture.runtime.serialPaintOwners[field] != fixture.object {
		t.Fatal("restored platform roots or serial owners differ")
	}
	if fixture.client.textEditor == nil || field.Native != fixture.client.textEditor || fixture.client.textEditor.Text() != "a" {
		t.Fatal("restored editor state or ownership differs")
	}
}

func (fixture continuationFixture) finishSerialCallback(t *testing.T) {
	t.Helper()
	fixture.clock.Advance(time.Hour)
	if serviced, err := fixture.client.ServiceThreads(t.Context(), 1); err != nil || serviced != 1 {
		t.Fatalf("restored serial callback=%d, %v", serviced, err)
	}
	field, _ := fixture.object.Fields["snapshotText"].Reference()
	runs, _ := field.Fields["serialRuns"].Int32()
	if runs != 1 || len(fixture.runtime.pendingSerial) != 0 || len(fixture.runtime.serialPaintOwners) != 0 {
		t.Fatal("restored serial callback did not complete once and release ownership")
	}
}

func (fixture continuationFixture) checkStorage(t *testing.T) {
	t.Helper()
	database, _ := fixture.object.Fields["snapshotDatabase"].Reference()
	file, _ := fixture.object.Fields["snapshotFile"].Reference()
	stream, _ := fixture.object.Fields["snapshotStream"].Reference()
	if database == nil || file == nil || stream == nil {
		t.Fatal("restored storage roots are missing")
	}
	store, ok := database.Native.(*runtimeDataBaseStore)
	if !ok || fixture.runtime.databases["fixture"] != store || len(store.records) != 3 || string(store.records[0]) != "saved" || store.records[1] != nil || store.records[2] == nil {
		t.Fatal("restored database records or handle sharing differ")
	}
	state, ok := file.Native.(*runtimeGuestFile)
	if !ok || stream.Native != state || state.position != 2 || string(state.data) != "cursor" {
		t.Fatal("restored file cursor or stream sharing differs")
	}
	cFile := fixture.runtime.cFiles["fixture-c"]
	openFile := fixture.runtime.cFileHandles[0x1003]
	rdb := fixture.runtime.recordDatabases["fixture-r"]
	openRecord := fixture.runtime.recordDatabaseHandles[0x2002]
	if cFile == nil || openFile == nil || openFile.store != cFile || string(cFile.data) != "stream" || openFile.position != 2 || cFile.packaged != 6 || fixture.runtime.nextCDatabaseHandle != 5 {
		t.Fatal("restored C file table differs")
	}
	if rdb == nil || openRecord == nil || openRecord.store != rdb || rdb.recordSize != 19 || len(rdb.records) != 3 || rdb.records[0] != nil || rdb.records[1] == nil || string(rdb.records[2]) != "record" || fixture.runtime.nextRecordDatabaseHandle != 8 || !fixture.runtime.removedFiles["deleted"] {
		t.Fatal("restored C record table or removal cache differs")
	}
}

func (fixture continuationFixture) checkGraphics(t *testing.T) {
	t.Helper()
	picture, _ := fixture.object.Fields["snapshotImage"].Reference()
	graphics, _ := fixture.object.Fields["snapshotGraphics"].Reference()
	if picture == nil || graphics == nil {
		t.Fatal("restored graphics roots are missing")
	}
	decoded, ok := picture.Native.(image.Image)
	if !ok || color.NRGBAModel.Convert(decoded.At(0, 0)) != (color.NRGBA{R: 17, G: 34, B: 51}) {
		t.Fatal("restored transparent image lost its color")
	}
	handle, err := picture.Fields["guestFramebuffer:I"].Int32()
	if err != nil || imageOpacityOf(decoded) == nil || fixture.runtime.framebufferOpacityOf(uint32(handle)) != imageOpacityOf(decoded) {
		t.Fatal("restored image mask sharing differs")
	}
	if state, ok := graphics.Native.(*runtimeGraphicsState); !ok || state.color != 0x1234 {
		t.Fatal("restored drawing state differs")
	}
}

func testAOTCallCheckpoint(t *testing.T, options continuationFixtureOptions) {
	source := newContinuationFixture(t, 1700000000, options)
	source.start(t)
	saved := source.capture(t)
	delay := 30 * time.Millisecond
	if options.ModuleWait {
		delay = waitSliceWithoutTimeout
	}
	if len(saved.Continuation.Calls) != 2 || saved.WaitRemaining != delay {
		t.Fatalf("initial boundary: calls=%d wait=%v", len(saved.Continuation.Calls), saved.WaitRemaining)
	}
	wantThreadRuns := 0
	if options.Runnable {
		wantThreadRuns = 1
	}
	if saved.Continuation.Execution.ThreadRuns != wantThreadRuns {
		t.Fatalf("Thread.run remainders = %d, want %d", saved.Continuation.Execution.ThreadRuns, wantThreadRuns)
	}
	if scope := saved.Continuation.Calls[0].Native; options.Module && scope != (nativeReturnCheckpoint{}) || !options.Module && (!scope.Active || scope.Tag != 2 || scope.Value != 75) {
		t.Fatalf("captured native parent = %+v", scope)
	}
	source.client.StopThreads()
	if len(source.runtime.nativeReturnScopes) != 0 {
		t.Fatal("source scopes survived worker termination")
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "calls.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	restoreBinary := os.Getenv("WFEATURE_CALL_CHECKPOINT_RESTORE_BINARY")
	if restoreBinary == "" {
		restoreBinary = os.Args[0]
	}
	process := exec.CommandContext(t.Context(), restoreBinary, "-test.run=^TestAOTCallCheckpointSubprocess$", "-test.timeout=20s")
	process.Env = append(os.Environ(), "WFEATURE_CALL_CHECKPOINT_FIXTURE="+path)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("restore subprocess: %v\n%s", err, output)
	}
	for pass := 0; pass < 2; pass++ {
		data, err := json.Marshal(saved)
		if err != nil {
			t.Fatal(err)
		}
		var decoded continuationFixtureState
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		fresh := newContinuationRestoreFixture(t, 1800000000+int64(pass)*1000, options)
		worker := fresh.restore(t, decoded)
		fresh.clock.Advance(delay - time.Millisecond)
		if serviced, err := fresh.client.ServiceThreads(t.Context(), 1); err != nil || serviced != 0 {
			t.Fatalf("early slice = %d, %v", serviced, err)
		}
		fresh.clock.Advance(time.Millisecond)
		if serviced, err := fresh.client.ServiceThreads(t.Context(), 1); err != nil || serviced != 1 {
			t.Fatalf("restored slice = %d, %v", serviced, err)
		}
		probe := readTestBytes(t, fresh.client, fresh.probe, 16)
		if binary.LittleEndian.Uint32(probe) != 1 || binary.LittleEndian.Uint32(probe[8:]) != 1 || binary.LittleEndian.Uint32(probe[12:]) != uint32(pass+1) {
			t.Fatalf("prefix replay or lost wait completion: %x", probe)
		}
		if pass == 0 {
			saved = fresh.capture(t)
			fresh.client.StopThreads()
			continue
		}
		if len(fresh.client.workers) != 0 || len(fresh.runtime.nativeReturnScopes) != 0 || len(fresh.runtime.aotCallDepth) != 0 {
			t.Fatal("completed restored call retained execution state")
		}
		wantResult, wantHigh := uint32(42), uint32(0)
		if options.Module {
			wantResult, wantHigh = 99, 7
		}
		if options.Throw {
			wantResult, wantHigh = 77, 456
		}
		if value := binary.LittleEndian.Uint32(probe[4:]); value != wantResult {
			t.Fatalf("native answer = %d, want %d", value, wantResult)
		}
		if high := binary.LittleEndian.Uint32(readTestBytes(t, fresh.client, fresh.container+4, 4)); high != wantHigh {
			t.Fatalf("native high word = %d", high)
		}
		if !options.Module {
			if value, err := fresh.client.core.ThreadLocalWord(worker.armThread, fresh.runtime.exceptionContext+nativeReturnValueOffset); err != nil || value != 75 {
				t.Fatalf("native cleanup = %d, %v", value, err)
			}
		}
		if options.Platform {
			fresh.finishSerialCallback(t)
		}
		if options.Relay {
			fresh.finishRelay(t)
		}
	}
}

func TestAOTCallCheckpointSubprocess(t *testing.T) {
	path := os.Getenv("WFEATURE_CALL_CHECKPOINT_FIXTURE")
	if path == "" {
		t.Skip("run by the parent call-checkpoint test")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved continuationFixtureState
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	fresh := newContinuationRestoreFixture(t, 1900000000, saved.Options)
	fresh.restore(t, saved)
	if saved.Options.Runnable {
		value, err := fresh.client.vm.InvokeVirtual(fresh.object, "isAlive", "()Z")
		if alive, _ := value.Int32(); err != nil || alive != 1 {
			t.Fatalf("restored worker isAlive=%d, %v", alive, err)
		}
		if _, err := fresh.client.vm.InvokeVirtual(fresh.object, "start", "()V"); err == nil {
			t.Fatal("restored worker started twice")
		}
	}
	for i := 0; i < 2; i++ {
		fresh.clock.Advance(saved.WaitRemaining)
		if serviced, err := fresh.client.ServiceThreads(t.Context(), 1); err != nil || serviced != 1 {
			t.Fatalf("subprocess slice = %d, %v", serviced, err)
		}
	}
	probe := readTestBytes(t, fresh.client, fresh.probe, 16)
	wantResult := uint32(42)
	if saved.Options.Module {
		wantResult = 99
	}
	if saved.Options.Throw {
		wantResult = 77
	}
	for i, want := range []uint32{1, wantResult, 1, 2} {
		if got := binary.LittleEndian.Uint32(probe[i*4:]); got != want {
			t.Fatalf("subprocess word %d = %d, want %d", i, got, want)
		}
	}
	if len(fresh.client.workers) != 0 || len(fresh.runtime.nativeReturnScopes) != 0 || len(fresh.runtime.aotCallDepth) != 0 {
		t.Fatal("subprocess leaked execution state")
	}
	if saved.Options.Runnable {
		value, err := fresh.client.vm.InvokeVirtual(fresh.object, "isAlive", "()Z")
		if alive, _ := value.Int32(); err != nil || alive != 0 {
			t.Fatalf("completed worker isAlive=%d, %v", alive, err)
		}
	}
	if saved.Options.Platform {
		fresh.finishSerialCallback(t)
	}
	if saved.Options.Relay {
		fresh.finishRelay(t)
	}
}

func TestAOTCallCheckpointRefusesIncompleteRecordsBeforeExecution(t *testing.T) {
	source := newContinuationFixture(t, 1700000000, continuationFixtureOptions{})
	worker := source.start(t)
	saved := source.capture(t)
	for _, test := range []struct {
		name   string
		change func([]aotCallCheckpoint)
	}{
		{"child version", func(c []aotCallCheckpoint) { c[1].ARM.Version++ }},
		{"child entry stack", func(c []aotCallCheckpoint) { c[1].ARM.EntryStack -= 4 }},
		{"child budget", func(c []aotCallCheckpoint) { c[1].ARM.Budget++ }},
		{"bridge remainder", func(c []aotCallCheckpoint) { c[0].ARM.Context.Registers[12] = javaSVCRegisterString }},
		{"native cleanup", func(c []aotCallCheckpoint) { c[0].Native.Active = false }},
		{"wait descriptor", func(c []aotCallCheckpoint) { c[1].Wait.Descriptor = "()J" }},
		{"static receiver", func(c []aotCallCheckpoint) { c[1].Wait.Receiver = 1 }},
		{"supervisor category", func(c []aotCallCheckpoint) { c[1].ARM.SupervisorCall.Immediate = 99 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fresh := newContinuationFixture(t, 1800000000, continuationFixtureOptions{})
			calls := append([]aotCallCheckpoint(nil), saved.Continuation.Calls...)
			test.change(calls)
			root := armcore.NewThread(saved.Root.Context)
			root.SetStepBudget(defaultThreadSliceSteps)
			if _, err := fresh.runtime.resumeAOTCalls(t.Context(), root, calls); err == nil {
				t.Fatal("invalid call record was accepted")
			}
			if fresh.client.core.Steps() != 0 || len(root.LiveContexts()) != 1 || len(fresh.runtime.nativeReturnScopes) != 0 || len(fresh.runtime.aotCallDepth) != 0 {
				t.Fatal("refusal executed or retained a call")
			}
		})
	}
	// Refusing an unknown live remainder also leaves the original run usable.
	source.client.run.Lock()
	pending := worker.javaWait
	worker.javaWait = nil
	_, captureErr := source.runtime.captureAOTCalls(worker)
	worker.javaWait = pending
	source.client.run.Unlock()
	if captureErr == nil {
		t.Fatal("unrecorded wait was accepted")
	}
	for i := 0; i < 2; i++ {
		source.clock.Advance(30 * time.Millisecond)
		if _, err := source.client.ServiceThreads(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
	}
	probe := readTestBytes(t, source.client, source.probe, 16)
	for i, want := range []uint32{1, 42, 1, 2} {
		if got := binary.LittleEndian.Uint32(probe[i*4:]); got != want {
			t.Fatalf("refused capture changed source word %d: got %d, want %d", i, got, want)
		}
	}
}

func TestAOTCallCheckpointCaptureKeepsWaitReceiverWeak(t *testing.T) {
	fixture := newContinuationFixture(t, 1700000000, continuationFixtureOptions{ObjectWait: true})
	worker := fixture.start(t)
	address, ok := fixture.client.vm.AOTObjectAddress(fixture.object)
	if !ok {
		t.Fatal("fixture receiver is unbound")
	}
	fixture.client.vm.ReleaseAOTObject(address)
	fixture.client.run.Lock()
	_, err := fixture.runtime.captureWorkerContinuation(worker)
	fixture.client.run.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if fixture.client.vm.AOTObjectPinned(address) {
		t.Fatal("capturing a wait re-pinned its receiver")
	}
}

func TestModuleCheckpointRefusesInvalidRemaindersWithoutChangingSource(t *testing.T) {
	source := newContinuationFixture(t, 1700000000, continuationFixtureOptions{Module: true, ModuleWait: true})
	source.start(t)
	saved := source.capture(t)
	for _, test := range []struct {
		name   string
		change func([]aotCallCheckpoint) []aotCallCheckpoint
	}{
		{"missing child", func(c []aotCallCheckpoint) []aotCallCheckpoint { return c[:1] }},
		{"wait with child", func(c []aotCallCheckpoint) []aotCallCheckpoint {
			c[0].ARM.Context.Registers[12] = moduleJumpWait
			return c
		}},
		{"unknown slot", func(c []aotCallCheckpoint) []aotCallCheckpoint {
			c[1].ARM.Context.Registers[12] = moduleJumpPoll
			return c
		}},
		{"native return scope", func(c []aotCallCheckpoint) []aotCallCheckpoint { c[0].Native.Active = true; return c }},
		{"Java wait scope", func(c []aotCallCheckpoint) []aotCallCheckpoint { c[1].Wait.Active = true; return c }},
		{"spill beyond entry", func(c []aotCallCheckpoint) []aotCallCheckpoint {
			c[0].ARM.EntryStack = c[0].ARM.Context.Registers[armcore.RegisterSP] + 4
			return c
		}},
		{"spill overflow", func(c []aotCallCheckpoint) []aotCallCheckpoint {
			c[0].ARM.Context.Registers[armcore.RegisterSP] = ^uint32(3)
			c[0].ARM.EntryStack = ^uint32(0)
			c[1].ARM.EntryStack = ^uint32(3)
			return c
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := test.change(append([]aotCallCheckpoint(nil), saved.Continuation.Calls...))
			before := source.client.core.Steps()
			root := armcore.NewThread(saved.Root.Context)
			if _, err := source.runtime.resumeAOTCalls(t.Context(), root, calls); err == nil {
				t.Fatal("invalid module continuation was accepted")
			}
			if source.client.core.Steps() != before || len(root.LiveContexts()) != 1 {
				t.Fatal("refused module continuation executed guest code")
			}
		})
	}
	for range 2 {
		source.clock.Advance(waitSliceWithoutTimeout)
		if _, err := source.client.ServiceThreads(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
	}
	probe := readTestBytes(t, source.client, source.probe, 16)
	for index, want := range []uint32{1, 99, 1, 2} {
		if got := binary.LittleEndian.Uint32(probe[index*4:]); got != want {
			t.Fatalf("source word %d = %d, want %d", index, got, want)
		}
	}
}

package lgt

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// The Java checkpoint fixture is the authored Clet given a Java runtime and
// guest threads of its own. Each thread's `run` is assembled here, so what a
// thread is parked in when a checkpoint is taken is chosen rather than found:
// a sleep, a spent budget, a wait, a lock somebody else holds, a static
// initialiser. A real title is in one of the first three almost all the time,
// and the corpus probe covers those; the rest are here because nothing in the
// local library can be relied on to be in them at the tick a probe looks.
const (
	javaFixtureWords uint32 = fixtureDataBase + 0x8c0 // eight counters
	javaFixtureCode  uint32 = fixtureDataBase + 0xc00 // the module's last page, past its sections
)

type javaThreadFixture struct {
	t       *testing.T
	archive []byte
	session *Session
	client  *Client
	next    uint32
	stubs   map[string]uint32
}

func newJavaThreadFixture(t *testing.T, store backend.SaveStore) *javaThreadFixture {
	t.Helper()
	archive := fixtureArchive(t)
	session, err := StartSession(context.Background(), archive, SessionOptions{SaveStore: store, Width: 16, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close(context.Background()) })
	client := session.client
	// The platform surface a module would have handed over, cut down to the
	// two static methods a thread parks in.
	client.javaApplication = true
	client.javaLink = &javaLink{
		surface: &javaSurface{
			Classes:       []javaAPIClass{{Name: javaThreadClass, StaticMethods: javaRun{Start: 0, Count: 2}}},
			StaticMethods: []javaMemberRef{{Name: "sleep", Descriptor: "(J)V"}, {Name: "yield", Descriptor: "()V"}},
		},
		layout: newJavaLayout(),
	}
	if _, err := client.preparePlatformJavaClass("java/lang/Object"); err != nil {
		t.Fatal(err)
	}
	fixture := &javaThreadFixture{t: t, archive: archive, session: session, client: client, next: javaFixtureCode, stubs: map[string]uint32{}}
	for name, slot := range map[string][2]uint32{
		"sleep":      {svcCategoryJava, javaStaticMethodSlot(0)},
		"yield":      {svcCategoryJava, javaStaticMethodSlot(1)},
		"wait":       {svcCategoryJava, javaVirtualSlot("java/lang/Object", 9)},
		"enter":      {svcCategoryJava, javaSVCMonitorEnter},
		"exit":       {svcCategoryJava, javaSVCMonitorExit},
		"initialize": {svcCategoryJava, javaSVCInitializeClass},
		"resolve":    {svcCategoryJava, javaSVCResolveClass},
		"try":        {svcCategoryJava, javaSVCEnterTry},
		"throw":      {svcCategoryJava, javaSVCThrowNull},
		"setjmp":     {svcCategoryStdlib, stdlibSetJump},
		"function":   {svcCategoryStdlib, stdlibRunFunction},
	} {
		address, err := client.stub(slot[0], slot[1])
		if err != nil {
			t.Fatal(err)
		}
		fixture.stubs[name] = address
	}
	// A vtable stub is found by a tag folded from the class name, so the one
	// the fixture uses is checked to be the method it means.
	if dispatch, ok := client.javaPlatformVirtualDispatch(javaVirtualSlot("java/lang/Object", 9)); !ok || dispatch.Called != "wait()V" {
		t.Fatalf("the wait stub resolves to %+v", dispatch)
	}
	return fixture
}

func (fixture *javaThreadFixture) counter(index uint32) uint32 { return javaFixtureWords + index*4 }

// install assembles one routine at the next free address and answers it.
func (fixture *javaThreadFixture) install(build func(a *assembler)) uint32 {
	fixture.t.Helper()
	a := &assembler{base: fixture.next}
	build(a)
	code := a.finish()
	if uint64(fixture.next)+uint64(len(code)) > uint64(fixtureDataBase)+0x1000 {
		fixture.t.Fatal("the fixture's code page is full")
	}
	if err := fixture.client.core.Memory().Write(fixture.next, code); err != nil {
		fixture.t.Fatal(err)
	}
	address := fixture.next
	fixture.next += uint32(len(code))
	return address
}

func (fixture *javaThreadFixture) increment(a *assembler, index uint32) {
	a.literal(4, fixture.counter(index))
	a.emit(armLdr(0, 4, 0), armAddImm(0, 0, 1), armStr(0, 4, 0))
}

// call emits a call to a platform stub with literal arguments.
func (fixture *javaThreadFixture) call(a *assembler, stub string, arguments ...uint32) {
	for index, value := range arguments {
		a.literal(uint32(index), value)
	}
	a.literal(5, fixture.stubs[stub])
	a.call(5)
}

func (fixture *javaThreadFixture) loop(a *assembler, top uint32) {
	a.emit(armBranch(int32(int64(top)-int64(a.here()+8)) / 4))
}

// newClass registers an application class whose only method is `run`, and
// answers the class and one instance of it.
func (fixture *javaThreadFixture) newClass(name string, run uint32) (*javaRuntimeClass, uint32) {
	fixture.t.Helper()
	client := fixture.client
	runtime := client.javaRuntimeState()
	table, err := client.allocateWords([]uint32{0, 0, 0})
	if err != nil {
		fixture.t.Fatal(err)
	}
	class := &javaRuntimeClass{
		Name: name, Handle: table, VTable: table, Slots: 2, Instance: 2,
		Record: javaClass{Name: name, Handle: table, Methods: []javaMember{{Name: "run", Descriptor: "()V", Body: run}}},
	}
	if err := client.writeWord(table, table); err != nil {
		fixture.t.Fatal(err)
	}
	if class.Object, err = client.allocateJavaClassObject(class); err != nil {
		fixture.t.Fatal(err)
	}
	class.dataBlock, _ = client.readWord(class.Object + 8)
	runtime.byHandle[class.Handle], runtime.byObject[class.Object], runtime.byName[name] = class, class, class
	instance, err := client.allocateJavaObject(class)
	if err != nil {
		fixture.t.Fatal(err)
	}
	return class, instance
}

// start builds a Thread over a new Runnable whose `run` is the routine, and
// starts it the way a title does.
func (fixture *javaThreadFixture) start(name string, run uint32) uint32 {
	fixture.t.Helper()
	client := fixture.client
	_, runnable := fixture.newClass(name, run)
	thread, err := newTestObject(fixture.t, client, javaThreadClass)
	if err != nil {
		fixture.t.Fatal(err)
	}
	if _, err := javaThreadConstructor(client, context.Background(), nil, []uint32{thread, runnable}); err != nil {
		fixture.t.Fatal(err)
	}
	if _, err := javaThreadStart(client, context.Background(), nil, []uint32{thread}); err != nil {
		fixture.t.Fatal(err)
	}
	return thread
}

// settle ends the round the fixture built its objects in. They were allocated
// outside a platform call, so they are pinned until a round ends — which is
// the boundary a checkpoint is taken at.
func (fixture *javaThreadFixture) settle() {
	fixture.client.javaRun.pins = fixture.client.javaRun.pins[:0]
}

func (fixture *javaThreadFixture) words(session *Session) [8]uint32 {
	fixture.t.Helper()
	var words [8]uint32
	for index := range words {
		words[index] = guestWord(fixture.t, session, fixture.counter(uint32(index)))
	}
	return words
}

// restore takes the session through a checkpoint's bytes into a second one
// with saves of its own.
func (fixture *javaThreadFixture) restore(checkpoint backend.Checkpoint) *Session {
	fixture.t.Helper()
	store, _ := backend.NewMemorySaveStore(nil)
	prepared := prepareFixtureCheckpoint(fixture.t, fixture.archive, checkpoint, store)
	restored, err := prepared.Commit(context.Background(), nil)
	if err != nil {
		fixture.t.Fatal(err)
	}
	fixture.t.Cleanup(func() { _ = restored.Close(context.Background()) })
	return restored
}

// compare ticks the two sessions together and requires them to agree after
// every tick, in what a Host sees and in what the threads counted.
func (fixture *javaThreadFixture) compare(restored *Session, rounds int) {
	fixture.t.Helper()
	for round := 0; round < rounds; round++ {
		tickSession(fixture.t, fixture.session, 1)
		tickSession(fixture.t, restored, 1)
		if before, after := fingerprintOf(fixture.session), fingerprintOf(restored); before != after {
			fixture.t.Fatalf("round %d: restored %+v, want %+v", round, after, before)
		}
		if before, after := fixture.words(fixture.session), fixture.words(restored); before != after {
			fixture.t.Fatalf("round %d: the threads counted %v, want %v", round, after, before)
		}
	}
	if memoryDigest(fixture.t, restored.client) != memoryDigest(fixture.t, fixture.session.client) {
		fixture.t.Fatal("guest memory differs after the same ticks")
	}
}

// javaFixtureMoment is what the fixture's session looked like after one tick.
type javaFixtureMoment struct {
	fingerprint checkpointFingerprint
	words       [8]uint32
	memory      string
}

func (fixture *javaThreadFixture) moment(session *Session) javaFixtureMoment {
	return javaFixtureMoment{fingerprintOf(session), fixture.words(session), memoryDigest(fixture.t, session.client)}
}

// replay restores a checkpoint and requires the restored session to pass
// through the moments the source went on to, one per tick.
func (fixture *javaThreadFixture) replay(checkpoint backend.Checkpoint, moments []javaFixtureMoment) {
	fixture.t.Helper()
	restored := fixture.restore(checkpoint)
	defer restored.Close(context.Background())
	for index, want := range moments {
		tickSession(fixture.t, restored, 1)
		if got := fixture.moment(restored); got != want {
			fixture.t.Fatalf("tick %d after the load: restored %+v %v, want %+v %v",
				index+1, got.fingerprint, got.words, want.fingerprint, want.words)
		}
	}
}

func decodeRuntime(t *testing.T, checkpoint backend.Checkpoint) sessionCheckpointState {
	t.Helper()
	var saved sessionCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		t.Fatal(err)
	}
	return saved
}

// remainders lists what every recorded thread is parked in, outermost call
// first, so a test can say which boundary it was looking at.
func remainders(saved sessionCheckpointState) []string {
	names := map[javaRemainder]string{
		javaRemainderNone: "budget", javaRemainderResult: "result", javaRemainderWait: "wait",
		javaRemainderMonitor: "monitor", javaRemainderInitializer: "initializer", javaRemainderFunction: "function",
	}
	var chains []string
	for _, worker := range saved.Client.Java.Workers {
		parts := []string{}
		for _, call := range worker.Calls {
			parts = append(parts, names[javaRemainder(call.Remainder)])
		}
		if len(parts) == 0 {
			parts = append(parts, "unstarted")
		}
		chains = append(chains, strings.Join(parts, ">"))
	}
	return chains
}

// Every place a guest thread parks is one it can be restored from: inside a
// sleep, at the end of its budget, in a wait that gave a lock up two levels
// deep, behind a lock another thread is sleeping on, and before its first
// slice. The restored threads then do what the originals do, tick for tick.
func TestCheckpointRestoresParkedJavaThreads(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	shared, err := newTestObject(t, client, "java/lang/Object")
	if err != nil {
		t.Fatal(err)
	}
	waited, err := newTestObject(t, client, "java/lang/Object")
	if err != nil {
		t.Fatal(err)
	}
	// A frame loop: count, sleep, again.
	sleeper := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.increment(a, 0)
		fixture.call(a, "sleep", 30, 0)
		fixture.loop(a, top)
	})
	// synchronized (o) { synchronized (o) { o.wait(); count++; } }
	waiter := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.call(a, "enter", waited)
		fixture.call(a, "enter", waited)
		fixture.call(a, "wait", waited)
		fixture.increment(a, 2)
		fixture.call(a, "exit", waited)
		fixture.call(a, "exit", waited)
		fixture.loop(a, top)
	})
	// A thread that holds a lock across a long sleep, and one that wants it.
	holder := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.call(a, "enter", shared)
		fixture.increment(a, 3)
		fixture.call(a, "sleep", 180, 0)
		fixture.call(a, "exit", shared)
		fixture.call(a, "sleep", 30, 0)
		fixture.loop(a, top)
	})
	contender := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.call(a, "enter", shared)
		fixture.increment(a, 4)
		fixture.call(a, "exit", shared)
		fixture.call(a, "yield")
		fixture.call(a, "sleep", 20, 0)
		fixture.loop(a, top)
	})
	fixture.start("Sleeper", sleeper)
	fixture.start("Waiter", waiter)
	fixture.start("Holder", holder)
	fixture.start("Contender", contender)
	fixture.settle()

	seen := map[string]bool{}
	for boundary := 0; boundary < 6; boundary++ {
		checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
		if checkpoint.Variant != backend.CheckpointLGTJava {
			t.Fatalf("variant = %d, want the Java one", checkpoint.Variant)
		}
		saved := decodeRuntime(t, checkpoint)
		for _, chain := range remainders(saved) {
			seen[chain] = true
		}
		restored := fixture.restore(checkpoint)
		if fixture.words(restored) != fixture.words(fixture.session) {
			t.Fatalf("boundary %d: the restored threads start from other counts", boundary)
		}
		fixture.compare(restored, 9)
		// The restored session is a session: it can be saved again.
		if _, err := restored.CaptureCheckpoint(context.Background()); err != nil {
			t.Fatalf("boundary %d: recapture: %v", boundary, err)
		}
		_ = restored.Close(context.Background())
	}
	for _, chain := range []string{"unstarted", "result", "wait", "monitor"} {
		if !seen[chain] {
			t.Errorf("no checkpoint was taken with a thread parked in %q (saw %v)", chain, seen)
		}
	}
	words := fixture.words(fixture.session)
	for _, index := range []int{0, 2, 3, 4} {
		if words[index] == 0 {
			t.Errorf("the thread counting in word %d never ran", index)
		}
	}
}

// A thread that never sleeps is parked where its budget ran out, in the middle
// of its own code with nothing of the platform's above it. It is restored at
// that instruction with a fresh budget, which is what the park would have
// given it.
func TestCheckpointRestoresAThreadAtTheEndOfItsBudget(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	spinner := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.increment(a, 1)
		fixture.loop(a, top)
	})
	fixture.start("Spinner", spinner)
	fixture.settle()
	var checkpoints []backend.Checkpoint
	var trace []javaFixtureMoment
	for tick := 0; tick < 3; tick++ {
		tickSession(t, fixture.session, 1)
		trace = append(trace, fixture.moment(fixture.session))
		if tick < 2 {
			checkpoints = append(checkpoints, roundTripCheckpoint(t, fixture.archive, fixture.session))
		}
	}
	for boundary, checkpoint := range checkpoints {
		if chains := remainders(decodeRuntime(t, checkpoint)); !reflect.DeepEqual(chains, []string{"budget"}) {
			t.Fatalf("boundary %d: the thread is parked in %v", boundary, chains)
		}
		fixture.replay(checkpoint, trace[boundary+1:])
	}
	if trace[2].words[1] <= trace[0].words[1] {
		t.Fatal("the thread did not run on")
	}
}

// A slice can end on its budget anywhere, including inside a static
// initialiser the thread's own code set off, and inside a function the C
// library was asked to run. The platform call above each of them has only its
// answer left, so both are restored: the nested call finishes and the thread
// carries on in the method that started it.
func TestCheckpointRestoresAThreadInsideANestedCall(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	// A routine that burns more than one slice's budget and then counts once.
	burn := func(index uint32) uint32 {
		return fixture.install(func(a *assembler) {
			a.literal(2, 2_500_000)
			a.emit(0xe2522001) // subs r2, r2, #1
			a.emit(0x1afffffd) // bne  the line above
			fixture.increment(a, index)
			a.emit(armBX(14))
		})
	}
	initializer, function := burn(0), burn(1)
	target, _ := fixture.newClass("Initialised", 0)
	run := fixture.install(func(a *assembler) {
		fixture.call(a, "initialize", target.Object, initializer)
		fixture.increment(a, 2)
		fixture.call(a, "function", function)
		fixture.increment(a, 3)
		top := a.here()
		fixture.call(a, "sleep", 30, 0)
		fixture.increment(a, 4)
		fixture.loop(a, top)
	})
	fixture.start("Loader", run)
	fixture.settle()

	// One run of the source, with a checkpoint after each of its first ticks:
	// the thread is inside the initialiser at the first boundary and inside
	// the function at the second, and comparing a restored session must not
	// move the source past either.
	seen := map[string]bool{}
	var checkpoints []backend.Checkpoint
	var trace []javaFixtureMoment
	for tick := 0; tick < 9; tick++ {
		tickSession(t, fixture.session, 1)
		trace = append(trace, fixture.moment(fixture.session))
		if tick < 4 {
			checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
			for _, chain := range remainders(decodeRuntime(t, checkpoint)) {
				seen[chain] = true
			}
			checkpoints = append(checkpoints, checkpoint)
		}
	}
	for boundary, checkpoint := range checkpoints {
		fixture.replay(checkpoint, trace[boundary+1:])
	}
	for _, chain := range []string{"initializer>budget", "function>budget", "result"} {
		if !seen[chain] {
			t.Errorf("no checkpoint was taken with the thread in %q (saw %v)", chain, seen)
		}
	}
	if words := fixture.words(fixture.session); words[0] != 1 || words[1] != 1 || words[2] != 1 || words[3] != 1 || words[4] == 0 {
		t.Fatalf("the thread counted %v: each nested call runs once and the loop after them runs", words)
	}
	if !target.initialized {
		t.Fatal("the class was not marked initialised")
	}
}

// A try region is a saved point the platform keeps for the thread, not
// something in guest memory. A thread restored inside one still has it: the
// exception thrown after the load lands in the handler the title wrote.
func TestCheckpointKeepsAThreadsTryRegions(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	run := fixture.install(func(a *assembler) {
		fixture.call(a, "try")
		a.literal(5, fixture.stubs["setjmp"])
		a.call(5) // r0 is zero now and the exception later
		a.emit(armCmpImm(0, 0))
		branch := len(a.words)
		a.emit(0) // bne handler, patched below
		fixture.call(a, "sleep", 30, 0)
		fixture.call(a, "throw", 0)
		fixture.increment(a, 1) // not reached: the throw does not return
		handler := a.here()
		a.words[branch] = 0x1a000000 | uint32(int32(int64(handler)-int64(a.base+uint32(branch)*4+8))/4)&0xffffff
		fixture.increment(a, 0)
		top := a.here()
		fixture.call(a, "sleep", 30, 0)
		fixture.loop(a, top)
	})
	fixture.start("Catcher", run)
	fixture.settle()
	tickSession(t, fixture.session, 1)
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	saved := decodeRuntime(t, checkpoint)
	if worker := saved.Client.Java.Workers[0]; len(worker.Try) != 1 || !worker.Try[0].Armed || worker.Try[0].Depth != 1 {
		t.Fatalf("the thread's try regions are %+v, want one armed region at depth one", worker.Try)
	}
	restored := fixture.restore(checkpoint)
	fixture.compare(restored, 6)
	if words := fixture.words(restored); words[0] != 1 || words[1] != 0 {
		t.Fatalf("the restored thread counted %v: the handler runs once and the line after the throw never", words)
	}
	if count, _ := restored.UncaughtCallbacks(); count != 0 {
		t.Fatal("the restored thread's exception went uncaught")
	}
}

// A thread parked inside a platform call this platform cannot finish later is
// a checkpoint refused, by name, and nothing else: the session carries on, and
// the same request is answered once the thread is somewhere it can be
// restored from.
func TestCheckpointRefusesAThreadItCannotResume(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	// Preparing a class calls the module's own declaration thunk, and what is
	// left of that platform call afterwards — the vtable, the ready flag — is
	// in Go locals nothing here records.
	thunk := fixture.install(func(a *assembler) {
		a.literal(2, 2_500_000)
		a.emit(0xe2522001, 0x1afffffd)
		fixture.increment(a, 0)
		a.emit(armBX(14))
	})
	writeJavaClassFixture(t, client)
	if err := client.writeWord(fixtureClassHeader+0x2c, thunk); err != nil {
		t.Fatal(err)
	}
	run := fixture.install(func(a *assembler) {
		fixture.call(a, "resolve", fixtureClassHandle, 0)
		fixture.increment(a, 1)
		top := a.here()
		fixture.call(a, "sleep", 30, 0)
		fixture.loop(a, top)
	})
	fixture.start("Resolver", run)
	fixture.settle()
	tickSession(t, fixture.session, 1)
	_, err := fixture.session.CaptureCheckpoint(context.Background())
	if err == nil || errors.Is(err, ErrCheckpointBusy) || !strings.Contains(err.Error(), "resolve a class") {
		t.Fatalf("a thread inside a class declaration: %v", err)
	}
	// The refusal changed nothing: the thunk finishes, the class is prepared
	// and the thread reaches its loop.
	for tick := 0; tick < 4; tick++ {
		tickSession(t, fixture.session, 1)
	}
	if words := fixture.words(fixture.session); words[0] != 1 || words[1] != 1 {
		t.Fatalf("after the refusal the thread counted %v", words)
	}
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	fixture.compare(fixture.restore(checkpoint), 5)
}

// A restored thread that has not run yet still owes what it was restored
// with. A checkpoint taken before its first slice records that again, rather
// than a thread that never started — which would enter `run` from the top.
func TestCheckpointOfARestoredSessionBeforeItRuns(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	run := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.increment(a, 0)
		fixture.call(a, "sleep", 30, 0)
		fixture.loop(a, top)
	})
	fixture.start("Sleeper", run)
	fixture.settle()
	tickSession(t, fixture.session, 3)
	first := roundTripCheckpoint(t, fixture.archive, fixture.session)
	middle := fixture.restore(first)
	second := roundTripCheckpoint(t, fixture.archive, middle)
	if before, after := decodeRuntime(t, first), decodeRuntime(t, second); !reflect.DeepEqual(before, after) {
		t.Fatalf("a restored session writes a different record before it runs:\n%s", describeStateDifference(before, after))
	}
	fixture.compare(fixture.restore(second), 8)
}

// Loading over a running Java title ends its guest threads. They are parked
// goroutines, and nothing else would ever wake them.
func TestCheckpointLoadEndsTheDisplacedThreads(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	sleeper := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.increment(a, 0)
		fixture.call(a, "sleep", 30, 0)
		fixture.loop(a, top)
	})
	spinner := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.increment(a, 1)
		fixture.loop(a, top)
	})
	fixture.start("Sleeper", sleeper)
	fixture.start("Spinner", spinner)
	fixture.settle()
	tickSession(t, fixture.session, 1)
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	counted := fixture.words(fixture.session)
	tickSession(t, fixture.session, 1)
	// A third thread started after the checkpoint has never been granted a
	// slice when the load arrives.
	fixture.start("Late", sleeper)
	displaced := append([]*javaWorker(nil), fixture.client.javaRun.workers...)

	prepared := prepareFixtureCheckpoint(t, fixture.archive, checkpoint, store)
	restored, err := prepared.Commit(context.Background(), fixture.session)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(context.Background())
	if len(displaced) != 3 {
		t.Fatalf("the displaced session had %d threads", len(displaced))
	}
	for index, worker := range displaced {
		if !worker.done {
			t.Errorf("displaced thread %d was not ended", index)
		}
		select {
		case _, open := <-worker.grant:
			if open {
				t.Errorf("displaced thread %d was granted a slice", index)
			}
		case <-time.After(time.Second):
			t.Errorf("displaced thread %d is still waiting for a slice", index)
		}
	}
	if fixture.words(restored) != counted {
		t.Fatal("the restored session is not at the checkpoint")
	}
	before := fixture.words(restored)
	tickSession(t, restored, 2)
	if after := fixture.words(restored); after[0] <= before[0] || after[1] <= before[1] {
		t.Fatalf("the restored threads counted %v after %v", after, before)
	}
	// A prepared session that is thrown away ends its threads the same way.
	discarded := prepareFixtureCheckpoint(t, fixture.archive, checkpoint, store)
	workers := append([]*javaWorker(nil), discarded.session.client.javaRun.workers...)
	discarded.Discard()
	for index, worker := range workers {
		if !worker.done {
			t.Errorf("discarded thread %d was not ended", index)
		}
	}
}

// What a record says about a parked thread is checked against the restored
// runtime before any goroutine is started for it.
func TestCheckpointRefusesAThreadRecordThatDoesNotAddUp(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	lock, err := newTestObject(t, client, "java/lang/Object")
	if err != nil {
		t.Fatal(err)
	}
	waiter := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.call(a, "enter", lock)
		fixture.call(a, "wait", lock)
		fixture.increment(a, 0)
		fixture.call(a, "exit", lock)
		fixture.loop(a, top)
	})
	sleeper := fixture.install(func(a *assembler) {
		top := a.here()
		fixture.call(a, "sleep", 30, 0)
		fixture.loop(a, top)
	})
	fixture.start("Waiter", waiter)
	fixture.start("Sleeper", sleeper)
	fixture.settle()
	tickSession(t, fixture.session, 2)
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	if chains := remainders(decodeRuntime(t, checkpoint)); !reflect.DeepEqual(chains, []string{"wait", "result"}) {
		t.Fatalf("the threads are parked in %v", chains)
	}
	for _, test := range []struct {
		name   string
		mutate func(*javaState)
		// reason is part of the refusal a case has to be refused with, for the
		// ones a neighbouring check could otherwise be refusing instead.
		reason string
	}{
		{name: "a wait recorded as a sleep", mutate: func(java *javaState) { java.Workers[0].Calls[0].Remainder = uint8(javaRemainderResult) }},
		{name: "a sleep recorded as a wait", mutate: func(java *javaState) {
			java.Workers[1].Calls[0].Remainder, java.Workers[1].Calls[0].Object, java.Workers[1].Calls[0].Depth = uint8(javaRemainderWait), lock, 1
		}},
		{name: "a wait on another object", mutate: func(java *javaState) { java.Workers[0].Calls[0].Object++ }},
		{name: "a wait with no depth to take back", mutate: func(java *javaState) { java.Workers[0].Calls[0].Depth = 0 }},
		{name: "a platform call that parks nobody", mutate: func(java *javaState) {
			java.Workers[1].Calls[0].Frame.Context.Registers[12] = javaSVCAllocate
		}},
		{name: "a call that returns somewhere else", mutate: func(java *javaState) { java.Workers[1].Calls[0].Frame.End = 0x1000 }},
		{name: "a call on a larger budget", mutate: func(java *javaState) {
			java.Workers[1].Calls[0].Frame.Budget *= 2
			java.Workers[1].Root.StepBudget *= 2
		}},
		{name: "a call on another thread's stack", mutate: func(java *javaState) {
			java.Workers[1].Calls[0].Frame.Context.Registers[armcore.RegisterSP] = java.Workers[0].StackBase + 0x100
		}},
		{name: "a call that does not start where its thread does", mutate: func(java *javaState) { java.Workers[1].Calls[0].Frame.EntryStack -= 8 }},
		{name: "a depth with no calls under it", mutate: func(java *javaState) { java.Workers[1].CallDepth = 3 }},
		{name: "a second call nothing made", mutate: func(java *javaState) {
			java.Workers[1].Calls = append(java.Workers[1].Calls, java.Workers[1].Calls[0])
			java.Workers[1].CallDepth = 2
		}},
		{name: "a stack this session never mapped", mutate: func(java *javaState) { java.Workers[1].StackBase += 8 * uint32(javaThreadStackSize) }},
		{name: "two threads on one stack", mutate: func(java *javaState) { java.Workers[1].StackBase = java.Workers[0].StackBase }},
		{name: "a lock count with no monitor", mutate: func(java *javaState) { java.Workers[1].Monitors = 2 }},
		{name: "a monitor held by nobody", mutate: func(java *javaState) {
			java.Monitors = append(java.Monitors, javaMonitorState{Object: 0x7ffffff0, Owner: 5, Count: 1})
		}},
		{name: "a try region deeper than its calls", mutate: func(java *javaState) {
			java.Workers[1].TryBuffers = []uint32{0x30000100}
			java.Workers[1].Try = []javaTryState{{Buffer: 0x30000100, Depth: 4}}
		}},
		{name: "a thread naming a worker that is not there", mutate: func(java *javaState) { java.Threads[0].Worker = 9 }},
		{name: "a class that is its own superclass", mutate: func(java *javaState) { java.Classes[0].Super = 0 }},
		{name: "a stream read past its end", mutate: func(java *javaState) {
			java.Streams = append(java.Streams, javaStreamState{Object: 0x7ffffff0, Data: []byte{1}, Read: 5})
		}},
		{name: "a stream of a resource the archive has not", mutate: func(java *javaState) {
			java.Streams = append(java.Streams, javaStreamState{Object: 0x7ffffff0, Archive: 1, Key: []byte("absent")})
		}},
		{name: "a database with a record it does not list", mutate: func(java *javaState) {
			java.Databases = append(java.Databases, javaDatabaseState{Object: 0x7ffffff0, Records: [][]byte{{1}}})
		}},
		{name: "an object table torn in the middle", mutate: func(java *javaState) { java.Objects = java.Objects[:len(java.Objects)-5] }},
		{name: "more threads than stacks", mutate: func(java *javaState) { java.ThreadStacks = 1 }},
		{name: "a finished thread that still holds a lock", reason: "finished thread", mutate: func(java *javaState) {
			java.Workers[1].Done, java.Workers[1].Calls, java.Workers[1].CallDepth, java.Workers[1].Monitors = true, nil, 0, 1
			java.Monitors = []javaMonitorState{{Object: lock, Owner: 1, Count: 1}}
		}},
		{name: "a thread waiting for a lock it holds", reason: "waiting for a lock it holds", mutate: func(java *javaState) {
			java.Workers[0].Monitors = 1
			java.Monitors = []javaMonitorState{{Object: lock, Owner: 0, Count: 1}}
		}},
		{name: "an object the allocator never handed out", reason: "outside the blocks", mutate: func(java *javaState) {
			binary.LittleEndian.PutUint32(java.Objects[4:], platformDataBase+uint32(platformDataSize)-0x100)
		}},
		{name: "an object with more fields than its block", reason: "outside the blocks", mutate: func(java *javaState) {
			binary.LittleEndian.PutUint32(java.Objects[8:], 0xfffffff0)
		}},
		{name: "a class chain closed through a name", reason: "comes back to itself", mutate: func(java *javaState) {
			// A platform class with no superclass yet takes whichever class
			// its specified superclass's name is registered under, so
			// registering Object's name to it closes the chain on itself.
			java.Classes = append(java.Classes, javaClassState{Name: []byte("fixture/Looped"), Super: -1})
			for index, entry := range java.ByName {
				if string(entry.Name) == "java/lang/Object" {
					java.ByName[index].Class = int32(len(java.Classes) - 1)
					return
				}
			}
			panic("the fixture has no Object class")
		}},
		{name: "a layout class that is its own superclass", reason: "comes back to itself", mutate: func(java *javaState) {
			java.Link = &javaLinkState{HasLayout: true, Layout: []javaLayoutClassState{
				{Key: []byte("fixture/Looped"), Name: []byte("fixture/Looped"), Super: []byte("fixture/Looped")},
			}}
		}},
		{name: "a layout class with more statics than a class has", reason: "impossible size", mutate: func(java *javaState) {
			java.Link = &javaLinkState{HasLayout: true, Layout: []javaLayoutClassState{
				{Key: []byte("fixture/Vast"), Name: []byte("fixture/Vast"), StaticWords: 0xfffffff0},
			}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			saved := decodeRuntime(t, checkpoint)
			test.mutate(saved.Client.Java)
			record, err := backend.EncodeCheckpointRecord(saved)
			if err != nil {
				t.Fatal(err)
			}
			damaged := checkpoint
			damaged.Runtime = record
			other, _ := backend.NewMemorySaveStore(nil)
			prepared, err := PrepareSessionCheckpoint(fixture.archive, damaged, SessionOptions{SaveStore: other})
			if err == nil {
				prepared.Discard()
				t.Fatal("the record was accepted")
			}
			if !strings.Contains(err.Error(), test.reason) {
				t.Fatalf("the record was refused for another reason: %v", err)
			}
		})
	}
	// The record the cases were cut from is a good one.
	fixture.compare(fixture.restore(checkpoint), 4)
}

// A superclass chain that never ends is refused however it was closed: by the
// superclass a class records, or by the name the first type check to walk
// through a platform class would look its superclass up under.
func TestCheckpointRefusesAClassChainThatNeverEnds(t *testing.T) {
	class := func(name string, handle uint32, super int32) javaClassState {
		return javaClassState{Name: []byte(name), Handle: handle, Super: super}
	}
	named := func(pairs ...any) []javaClassNameState {
		var table []javaClassNameState
		for index := 0; index < len(pairs); index += 2 {
			table = append(table, javaClassNameState{[]byte(pairs[index].(string)), int32(pairs[index+1].(int))})
		}
		return table
	}
	platform := []javaClassState{
		class("java/lang/Object", 0, -1), class("java/io/InputStream", 0, -1),
		class("java/io/DataInputStream", 0, -1), class("Game", 0x1000, 2),
	}
	for _, test := range []struct {
		name    string
		classes []javaClassState
		byName  []javaClassNameState
		ends    bool
	}{
		{"platform classes under their own names", platform,
			named("java/io/DataInputStream", 2, "java/io/InputStream", 1, "java/lang/Object", 0), true},
		{"platform classes nothing has registered", platform, nil, true},
		{"a recorded superclass that is laid out later", []javaClassState{class("Game", 0x1000, 1), class("java/lang/Object", 0, -1)}, nil, true},
		{"a class that records itself", []javaClassState{class("Game", 0x1000, 0)}, nil, false},
		{"two classes that record each other", []javaClassState{class("Game", 0x1000, 1), class("Base", 0x2000, 0)}, nil, false},
		{"Object's name registered to a class beneath it", platform,
			named("java/io/InputStream", 1, "java/lang/Object", 2), false},
		{"a platform class registered as its own superclass", platform,
			named("java/io/InputStream", 2), false},
		{"an application class is not given a superclass by name", []javaClassState{class("Game", 0x1000, -1)},
			named("java/lang/Object", 0), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			java := javaState{Classes: test.classes, ByName: test.byName}
			if ends := java.classChainsEnd(); ends != test.ends {
				t.Fatalf("the chains end: %v, want %v", ends, test.ends)
			}
		})
	}
}

// The collector's table names blocks the allocator has outstanding, each of
// them once. That is what keeps a record from stating a size for the first
// collection to read.
func TestCheckpointRefusesAnObjectTheAllocatorDidNotHandOut(t *testing.T) {
	data := newArena(platformDataBase, platformDataSize)
	allocate := func(size uint64) uint32 {
		address, ok := data.allocate(size)
		if !ok {
			t.Fatal("the fixture arena is full")
		}
		return address
	}
	first, firstBlock := allocate(javaObjectBytes), allocate(40)
	second, secondBlock := allocate(javaObjectBytes), allocate(40)
	pack := func(records ...[3]uint32) []byte {
		var table []byte
		for _, record := range records {
			for _, word := range record {
				table = binary.LittleEndian.AppendUint32(table, word)
			}
			table = append(table, 0)
		}
		return table
	}
	for _, test := range []struct {
		name    string
		objects []byte
		valid   bool
	}{
		{"two objects and their blocks", pack([3]uint32{first, firstBlock, 40}, [3]uint32{second, secondBlock, 40}), true},
		{"an object with no fields", pack([3]uint32{first, 0, 0}), true},
		{"no objects", nil, true},
		{"an object at an address nothing was handed out at", pack([3]uint32{first + 8, firstBlock, 40}), false},
		{"a block at an address nothing was handed out at", pack([3]uint32{first, firstBlock + 8, 40}), false},
		{"a block larger than the one handed out", pack([3]uint32{first, firstBlock, 48}), false},
		{"a block of four gigabytes", pack([3]uint32{first, firstBlock, 0xfffffff0}), false},
		{"one block under two objects", pack([3]uint32{first, firstBlock, 40}, [3]uint32{second, firstBlock, 40}), false},
		{"an object that is another's block", pack([3]uint32{first, second, 8}, [3]uint32{second, secondBlock, 40}), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateJavaObjects(test.objects, data); (err == nil) != test.valid {
				t.Fatalf("validateJavaObjects answered %v", err)
			}
		})
	}
}

// A layout is walked upwards by name and allocated from by size, so a record
// may bring neither a chain that does not end nor a size no class has.
func TestCheckpointRefusesALayoutThatCannotBeWalked(t *testing.T) {
	class := func(key, super string) javaLayoutClassState {
		return javaLayoutClassState{Key: []byte(key), Name: []byte(key), Super: []byte(super)}
	}
	sized := func(change func(*javaLayoutClassState)) []javaLayoutClassState {
		laid := class("Game", "java/lang/Object")
		change(&laid)
		return []javaLayoutClassState{laid}
	}
	for _, test := range []struct {
		name   string
		link   javaLinkState
		reason string
	}{
		{"an application class over platform ones", javaLinkState{HasLayout: true, Layout: []javaLayoutClassState{
			class("Game", "java/util/Stack"), class("java/lang/Object", ""), class("java/util/Vector", "java/lang/Object"),
		}}, ""},
		{"an empty layout", javaLinkState{HasLayout: true}, ""},
		{"no layout", javaLinkState{}, ""},
		{"the largest sizes a class states", javaLinkState{HasLayout: true, Layout: sized(func(laid *javaLayoutClassState) {
			laid.InstanceSize, laid.VTableSize, laid.InstanceWords, laid.StaticWords = maxJavaLayoutWords, maxJavaLayoutWords, maxJavaLayoutWords, maxJavaLayoutWords
		})}, ""},
		{"a class that is its own superclass", javaLinkState{HasLayout: true, Layout: []javaLayoutClassState{class("Game", "Game")}}, "comes back to itself"},
		{"two classes over each other", javaLinkState{HasLayout: true, Layout: []javaLayoutClassState{
			class("Base", "Game"), class("Game", "Base"),
		}}, "comes back to itself"},
		// Stack is not in the layout, so the walk leaves it by the
		// specification's own link — which is back to Vector.
		{"a chain closed through the specification", javaLinkState{HasLayout: true, Layout: []javaLayoutClassState{
			class("java/util/Vector", "java/util/Stack"),
		}}, "comes back to itself"},
		{"Object given a superclass beneath it", javaLinkState{HasLayout: true, Layout: []javaLayoutClassState{
			class("Game", "java/lang/Object"), class("java/lang/Object", "Game"),
		}}, "comes back to itself"},
		{"more statics than a class has", javaLinkState{HasLayout: true, Layout: sized(func(laid *javaLayoutClassState) { laid.StaticWords = maxJavaLayoutWords + 1 })}, "impossible size"},
		{"a larger vtable than a class has", javaLinkState{HasLayout: true, Layout: sized(func(laid *javaLayoutClassState) { laid.VTableSize = maxJavaLayoutWords + 1 })}, "impossible size"},
		{"a larger instance than a class has", javaLinkState{HasLayout: true, Layout: sized(func(laid *javaLayoutClassState) { laid.InstanceSize = 0xffffffff })}, "impossible size"},
		{"more instance words than a class has", javaLinkState{HasLayout: true, Layout: sized(func(laid *javaLayoutClassState) { laid.InstanceWords = 0xffffffff })}, "impossible size"},
		{"classes out of order", javaLinkState{HasLayout: true, Layout: []javaLayoutClassState{class("b", ""), class("a", "")}}, "out of order"},
		{"classes with no layout to hold them", javaLinkState{Layout: []javaLayoutClassState{class("a", "")}}, "says it has not"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.link.validate()
			switch {
			case test.reason == "" && err != nil:
				t.Fatalf("the layout was refused: %v", err)
			case test.reason != "" && (err == nil || !strings.Contains(err.Error(), test.reason)):
				t.Fatalf("the layout was answered %v, want a refusal naming %q", err, test.reason)
			}
			if err != nil {
				return
			}
			if _, err := restoreJavaLink(&test.link); err != nil {
				t.Fatalf("a layout that passed was not restored: %v", err)
			}
		})
	}
}

// The tables behind a Java title's objects survive a round trip: the text of
// its strings, its lists, its record stores, its open streams and what it is
// drawing with. A restored session writes the record it was restored from.
func TestCheckpointRecordsTheJavaRuntimesTables(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	runtime := client.javaRun
	ctx := context.Background()

	text := newTestString(t, client, "한글 text")
	// Half of a surrogate pair is a string a title can hold and a record has
	// to carry as bytes.
	lone := newTestString(t, client, javaTextOfUnits([]uint16{0xd83d, 'x'}))
	vector := newFixtureVector(t, client)
	if _, err := javaVectorAdd(client, ctx, nil, []uint32{vector, text}); err != nil {
		t.Fatal(err)
	}
	database := openFixtureDatabase(t, client, "scores", 4)
	kept := insertFixtureRecord(t, client, database, []byte{1, 2, 3, 4})
	deleted := insertFixtureRecord(t, client, database, []byte{5, 6, 7, 8})
	if _, err := javaDeleteRecord(client, ctx, nil, []uint32{database, deleted}); err != nil {
		t.Fatal(err)
	}
	calendar, err := javaCalendarGetInstance(client, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaCalendarSet(client, ctx, client.thread, []uint32{calendar, javaCalendarMonth, 13}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaCalendarGetTime(client, ctx, nil, []uint32{calendar}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaCalendarSet(client, ctx, client.thread, []uint32{calendar, javaCalendarMonth, 2}); err != nil {
		t.Fatal(err)
	}
	random, err := newTestObject(t, client, "java/util/Random")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaRandomSetSeed(client, ctx, nil, []uint32{random, 77, 0}); err != nil {
		t.Fatal(err)
	}
	for draw := 0; draw < 9; draw++ {
		if _, err := javaRandomNext(client, ctx, nil, []uint32{random}); err != nil {
			t.Fatal(err)
		}
	}
	// A resource stream part way through, and one over bytes of its own.
	name := newTestString(t, client, "data/hello.txt")
	stream, err := javaGetResourceAsStream(client, ctx, nil, []uint32{0, name})
	if err != nil || stream == 0 {
		t.Fatalf("the resource stream: %d, %v", stream, err)
	}
	if _, err := javaStreamRead(client, ctx, client.thread, []uint32{stream}); err != nil {
		t.Fatal(err)
	}
	own, err := newTestObject(t, client, javaInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	runtime.streams[own] = &javaStream{Name: "own", Data: []byte{9, 8, 7, 6}, Read: 2, Markable: true, Mark: 1}
	sink, err := newTestObject(t, client, javaByteSinkClass)
	if err != nil {
		t.Fatal(err)
	}
	runtime.sinks[sink] = []byte("written")
	// A sink that was closed keeps no bytes and an open one that has been
	// written nothing keeps none either, and only the second is still a sink.
	closedSink, err := newTestObject(t, client, javaByteSinkClass)
	if err != nil {
		t.Fatal(err)
	}
	runtime.sinks[closedSink] = nil
	emptySink, err := newTestObject(t, client, javaByteSinkClass)
	if err != nil {
		t.Fatal(err)
	}
	runtime.sinks[emptySink] = []byte{}
	wrapper, err := newTestObject(t, client, javaByteSinkClass)
	if err != nil {
		t.Fatal(err)
	}
	runtime.wrapped[wrapper] = sink
	// A surface an Image stands for, shared with the decode cache, and a
	// Graphics over it with a clip and a translation.
	surface, err := client.newFramebuffer(5, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	surface.drawnHere, surface.pixels[3] = true, 0x7777
	image, err := newTestObject(t, client, javaImageClass)
	if err != nil {
		t.Fatal(err)
	}
	runtime.images[image] = surface.handle
	runtime.decodedImages = map[string]uint32{"data/picture.png": surface.handle, imageDigestKey([]byte{1, 2}): surface.handle}
	graphics, err := client.newJavaGraphics(surface.handle)
	if err != nil {
		t.Fatal(err)
	}
	*runtime.graphics[graphics] = javaGraphics{surface: surface.handle, color: 0x1f, translateX: 3, translateY: -2,
		clipX: 1, clipY: 1, clipWidth: 3, clipHeight: 2, clipSet: true, alpha: javaAlphaOpaque, packed: 0x112233, xor: true}
	runtime.widgets = map[uint32]*javaWidget{
		vector: {text: "entered", maxLength: 8, revision: 3, kind: javaWidgetTextField, mode: 1, children: []uint32{text}, shown: true, focused: true},
	}
	runtime.focusedWidget, runtime.widgetGeneration = vector, 5
	runtime.dates[calendar] = 1234567890123
	runtime.serial = []uint32{text}
	runtime.card, runtime.cardDirty, runtime.jlet = image, true, graphics
	// A lock the platform's own thread holds across the boundary.
	if err := client.javaMonitorEnter(ctx, lone); err != nil {
		t.Fatal(err)
	}
	fixture.settle()

	first := roundTripCheckpoint(t, fixture.archive, fixture.session)
	restored := fixture.restore(first)
	second := roundTripCheckpoint(t, fixture.archive, restored)
	if before, after := decodeRuntime(t, first), decodeRuntime(t, second); !reflect.DeepEqual(before, after) {
		t.Fatalf("a restored session writes a different record:\n%s", describeStateDifference(before, after))
	}
	copied := restored.client
	if got, _ := copied.javaText(text); got != "한글 text" {
		t.Fatalf("the string holds %q", got)
	}
	if got, _ := copied.javaText(lone); !reflect.DeepEqual(utf16Units(got), []uint16{0xd83d, 'x'}) {
		t.Fatalf("the lone surrogate came back as %v", utf16Units(got))
	}
	if !reflect.DeepEqual(copied.javaRun.vectors[vector], []uint32{text}) {
		t.Fatal("the vector lost its element")
	}
	if got := databaseRecord(t, copied, database, kept); !reflect.DeepEqual(got, []byte{1, 2, 3, 4}) {
		t.Fatalf("the database record is %v", got)
	}
	if held := copied.javaRun.databases[database]; !reflect.DeepEqual(held.deleted, runtime.databases[database].deleted) ||
		!reflect.DeepEqual(held.records, runtime.databases[database].records) {
		t.Fatal("the deleted record came back")
	}
	if next, _ := javaRandomNext(copied, ctx, nil, []uint32{random}); next != func() uint32 {
		value, _ := javaRandomNext(client, ctx, nil, []uint32{random})
		return value
	}() {
		t.Fatal("the generator did not continue its sequence")
	}
	// The resource stream reads on from where it was, out of the archive's
	// own bytes rather than a copy of them.
	if value, err := javaStreamRead(copied, ctx, copied.thread, []uint32{stream}); err != nil || value != 'a' {
		t.Fatalf("the resource stream reads %q, %v", value, err)
	}
	packaged, _ := copied.archive.Resource("data/hello.txt")
	if held := copied.javaRun.streams[stream].Data; &held[0] != &packaged[0] {
		t.Fatal("the resource stream holds a copy of the archive's bytes")
	}
	if held := copied.javaRun.streams[own]; held.Read != 2 || held.Mark != 1 || !held.Markable || !reflect.DeepEqual(held.Data, []byte{9, 8, 7, 6}) {
		t.Fatalf("the title's own stream is %+v", held)
	}
	if moment, _ := copied.javaCalendarOf(calendar); moment != func() time.Time {
		expected, _ := client.javaCalendarOf(calendar)
		return expected
	}() {
		t.Fatal("the calendar's pending fields did not survive")
	}
	if held, kept := copied.javaRun.sinks[sink]; !kept || string(held) != "written" {
		t.Fatalf("the sink holds %q", held)
	}
	if held, kept := copied.javaRun.sinks[closedSink]; !kept || held != nil {
		t.Fatalf("the closed sink came back as %v (kept %v)", held, kept)
	}
	if held, kept := copied.javaRun.sinks[emptySink]; !kept || held == nil || len(held) != 0 {
		t.Fatalf("the empty sink came back as %v (kept %v)", held, kept)
	}
	if monitor := copied.javaRun.monitors[lone]; monitor == nil || !monitor.platform || monitor.count != 1 {
		t.Fatalf("the platform's lock is %+v", monitor)
	}
	if state := copied.javaRun.graphics[graphics]; *state != *runtime.graphics[graphics] {
		t.Fatalf("the Graphics is %+v", *state)
	}
	if buffer := copied.framebuffers[surface.handle]; buffer == nil || !buffer.drawnHere || buffer.pixels[3] != 0x7777 {
		t.Fatal("the surface drawn into on this side lost its pixels")
	}
}

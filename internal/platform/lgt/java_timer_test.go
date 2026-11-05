package lgt

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// newTimerFixture is the guest-thread fixture with the stubs a timer's task
// calls: the timer's and the task's own cancel, and Thread's interrupt and
// join and Object's timed wait for the interrupt tests.
func newTimerFixture(t *testing.T) *javaThreadFixture {
	t.Helper()
	store, _ := backend.NewMemorySaveStore(nil)
	fixture := newJavaThreadFixture(t, store)
	client := fixture.client
	for _, class := range []string{javaTimerClass, javaTimerTaskClass, javaThreadClass, javaDateClass} {
		if _, err := client.preparePlatformJavaClass(class); err != nil {
			t.Fatal(err)
		}
	}
	for name, slot := range map[string]uint32{
		"timerCancel": javaVirtualSlot(javaTimerClass, 16),
		"taskCancel":  javaVirtualSlot(javaTimerTaskClass, 11),
		"interrupt":   javaVirtualSlot(javaThreadClass, 12),
		"join":        javaVirtualSlot(javaThreadClass, 17),
		"waitTimed":   javaVirtualSlot(javaObjectClass, 7),
		"leave":       javaSVCLeaveTry,
	} {
		address, err := client.stub(svcCategoryJava, slot)
		if err != nil {
			t.Fatal(err)
		}
		fixture.stubs[name] = address
	}
	return fixture
}

// newTimer builds a Timer the way a title does: an instance of the platform's
// class, and its constructor.
func (fixture *javaThreadFixture) newTimer() uint32 {
	fixture.t.Helper()
	object, err := newTestObject(fixture.t, fixture.client, javaTimerClass)
	if err != nil {
		fixture.t.Fatal(err)
	}
	if _, err := javaTimerConstructor(fixture.client, context.Background(), nil, []uint32{object}); err != nil {
		fixture.t.Fatal(err)
	}
	return object
}

// newTaskClass registers a title's own TimerTask subclass whose run is the
// routine.
func (fixture *javaThreadFixture) newTaskClass(name string, run uint32) *javaRuntimeClass {
	fixture.t.Helper()
	client := fixture.client
	base, err := client.preparePlatformJavaClass(javaTimerTaskClass)
	if err != nil {
		fixture.t.Fatal(err)
	}
	table, err := client.allocateWords([]uint32{0, 0, 0})
	if err != nil {
		fixture.t.Fatal(err)
	}
	class := &javaRuntimeClass{
		Name: name, Handle: table, VTable: table, Slots: 2, Instance: 2, Super: base,
		Record: javaClass{Name: name, Handle: table, Methods: []javaMember{{Name: "run", Descriptor: "()V", Body: run}}},
	}
	if err := client.writeWord(table, table); err != nil {
		fixture.t.Fatal(err)
	}
	if class.Object, err = client.allocateJavaClassObject(class); err != nil {
		fixture.t.Fatal(err)
	}
	class.dataBlock, _ = client.readWord(class.Object + 8)
	runtime := client.javaRuntimeState()
	runtime.byHandle[class.Handle], runtime.byObject[class.Object], runtime.byName[name] = class, class, class
	return class
}

// newTaskOf builds one task of a class, constructed.
func (fixture *javaThreadFixture) newTaskOf(class *javaRuntimeClass) uint32 {
	fixture.t.Helper()
	task, err := fixture.client.allocateJavaObject(class)
	if err != nil {
		fixture.t.Fatal(err)
	}
	if _, err := javaTimerTaskConstructor(fixture.client, context.Background(), nil, []uint32{task}); err != nil {
		fixture.t.Fatal(err)
	}
	return task
}

func (fixture *javaThreadFixture) newTask(name string, run uint32) uint32 {
	fixture.t.Helper()
	return fixture.newTaskOf(fixture.newTaskClass(name, run))
}

// countRun is a task's run that counts in one word and returns.
func (fixture *javaThreadFixture) countRun(index uint32) uint32 {
	return fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		fixture.increment(a, index)
		a.emit(armPopPC)
	})
}

// sleepyRun counts in one word as it starts, then sleeps, then returns.
func (fixture *javaThreadFixture) sleepyRun(index, milliseconds uint32) uint32 {
	return fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		fixture.increment(a, index)
		fixture.call(a, "sleep", milliseconds, 0)
		a.emit(armPopPC)
	})
}

// catching is a thread's run that does `try { body } catch { count in the
// handler's word }` and then sleeps in 30ms steps for ever.
func (fixture *javaThreadFixture) catching(handler uint32, body func(a *assembler)) uint32 {
	return fixture.install(func(a *assembler) {
		fixture.call(a, "try")
		a.literal(5, fixture.stubs["setjmp"])
		a.call(5) // r0 is zero now and the exception later
		a.emit(armCmpImm(0, 0))
		branch := len(a.words)
		a.emit(0) // bne handler, patched below
		body(a)
		fixture.call(a, "leave")
		skip := len(a.words)
		a.emit(0) // b past the handler, patched below
		catch := a.here()
		a.words[branch] = 0x1a000000 | uint32(int32(int64(catch)-int64(a.base+uint32(branch)*4+8))/4)&0xffffff
		fixture.increment(a, handler)
		top := a.here()
		a.words[skip] = armBranch(int32(int64(top)-int64(a.base+uint32(skip)*4+8)) / 4)
		fixture.call(a, "sleep", 30, 0)
		fixture.loop(a, top)
	})
}

// build makes a Thread over a new Runnable whose run is the routine, without
// starting it.
func (fixture *javaThreadFixture) build(name string, run uint32) uint32 {
	fixture.t.Helper()
	_, runnable := fixture.newClass(name, run)
	thread, err := newTestObject(fixture.t, fixture.client, javaThreadClass)
	if err != nil {
		fixture.t.Fatal(err)
	}
	if _, err := javaThreadConstructor(fixture.client, context.Background(), nil, []uint32{thread, runnable}); err != nil {
		fixture.t.Fatal(err)
	}
	return thread
}

func (fixture *javaThreadFixture) interrupt(thread uint32) {
	fixture.t.Helper()
	if _, err := javaThreadInterrupt(fixture.client, context.Background(), nil, []uint32{thread}); err != nil {
		fixture.t.Fatal(err)
	}
}

// schedule calls one of Timer's slots the way a call site does.
func (fixture *javaThreadFixture) schedule(slot uint32, arguments ...uint32) error {
	fixture.t.Helper()
	_, err := slotCall(fixture.t, fixture.client, javaTimerClass, slot, arguments...)
	return err
}

func (fixture *javaThreadFixture) mustSchedule(slot uint32, arguments ...uint32) {
	fixture.t.Helper()
	if err := fixture.schedule(slot, arguments...); err != nil {
		fixture.t.Fatalf("Timer slot %d: %v", slot, err)
	}
}

// scheduledAt is a task's scheduledExecutionTime, both words of it.
func scheduledAt(t *testing.T, client *Client, task uint32) int64 {
	t.Helper()
	low := mustSlot(t, client, javaTimerTaskClass, 12, task)
	high, err := client.thread.Register(1)
	if err != nil {
		t.Fatal(err)
	}
	return int64(uint64(high)<<32 | uint64(low))
}

// A task runs once, in the tick that reaches its deadline and not before —
// the tick span lands on it — on a Thread the timer built for it, which has
// ended once the run is over; its scheduled time is the deadline, and a
// single run that has run has none to come.
func TestJavaTimerRunsATaskAtItsDeadlineOnAThreadOfItsOwn(t *testing.T) {
	fixture := newTimerFixture(t)
	client := fixture.client
	timer := fixture.newTimer()
	task := fixture.newTask("game/Once", fixture.countRun(0))
	fixture.settle()
	if got := scheduledAt(t, client, task); got != 0 {
		t.Fatalf("an unscheduled task's scheduledExecutionTime = %d, want 0", got)
	}
	due := client.clock.now() + 120*time.Millisecond
	fixture.mustSchedule(10, timer, task, 120, 0)
	if wait, pending := client.nextJavaThreadDue(); !pending || wait != 120*time.Millisecond {
		t.Fatalf("the next deadline is %v (%v), want the task's 120ms", wait, pending)
	}
	ran := false
	for tick := 0; tick < 8; tick++ {
		before := client.clock.now()
		tickSession(t, fixture.session, 1)
		if fixture.words(fixture.session)[0] != 0 && !ran {
			ran = true
			if before >= due || client.clock.now() < due {
				t.Fatalf("the task ran in the tick from %v to %v; it was due at %v", before, client.clock.now(), due)
			}
		}
	}
	if count := fixture.words(fixture.session)[0]; count != 1 {
		t.Fatalf("the task ran %d times, want once", count)
	}
	thread := client.javaTimerThreadRecord(client.javaRun.timers[timer])
	if thread == nil || !client.javaObjectIs(thread.object, javaThreadClass) {
		t.Fatal("the timer has no Thread of its own")
	}
	if client.javaThreadAlive(thread.object) {
		t.Error("the timer's thread is still running after its one run")
	}
	if got, want := scheduledAt(t, client, task), client.javaGuestEpochMillis(due); got != want {
		t.Errorf("scheduledExecutionTime = %d, want the deadline %d", got, want)
	}
	if answer := mustSlot(t, client, javaTimerTaskClass, 11, task); answer != javaFalse {
		t.Error("cancelling a single run that has run answered true")
	}
}

// A repeating task's next run is due from the deadline it was due at for a
// fixed rate, so one that fell behind is due again at once and catches up, and
// from the time it was started for a fixed delay. Two runs that each take
// longer than the period show the difference in the deadlines their runs
// were due at; the two timers run side by side.
func TestJavaTimerFixedRateAndFixedDelayMeasureFromDifferentPlaces(t *testing.T) {
	fixture := newTimerFixture(t)
	client := fixture.client
	rate, delay := fixture.newTimer(), fixture.newTimer()
	rateTask := fixture.newTask("game/Rate", fixture.sleepyRun(0, 100))
	delayTask := fixture.newTask("game/Delay", fixture.sleepyRun(1, 100))
	fixture.settle()
	start := client.javaGuestEpochMillis(client.clock.now())
	fixture.mustSchedule(14, rate, rateTask, 0, 0, 40, 0)
	fixture.mustSchedule(12, delay, delayTask, 0, 0, 40, 0)
	rateDue, delayDue := []int64{}, []int64{}
	for tick := 0; tick < 40 && (len(rateDue) < 4 || len(delayDue) < 4); tick++ {
		tickSession(t, fixture.session, 1)
		words := fixture.words(fixture.session)
		if int(words[0]) > len(rateDue) {
			rateDue = append(rateDue, scheduledAt(t, client, rateTask)-start)
		}
		if int(words[1]) > len(delayDue) {
			delayDue = append(delayDue, scheduledAt(t, client, delayTask)-start)
		}
	}
	for index, want := range []int64{0, 40, 80, 120} {
		if index >= len(rateDue) || rateDue[index] != want {
			t.Fatalf("the fixed-rate runs were due at %v ms, want 0, 40, 80, 120", rateDue)
		}
	}
	for index, want := range []int64{0, 40, 140, 240} {
		if index >= len(delayDue) || delayDue[index] != want {
			t.Fatalf("the fixed-delay runs were due at %v ms, want 0, 40, 140, 240", delayDue)
		}
	}
}

// A timer has one thread and runs its tasks in turn: a task due while another
// of its timer's runs is going waits for it, and tasks on two timers run side
// by side.
func TestJavaTimerRunsItsTasksOneAfterAnother(t *testing.T) {
	for _, shared := range []bool{true, false} {
		fixture := newTimerFixture(t)
		first := fixture.newTimer()
		second := first
		if !shared {
			second = fixture.newTimer()
		}
		one := fixture.newTask("game/One", fixture.sleepyRun(0, 60))
		two := fixture.newTask("game/Two", fixture.sleepyRun(1, 60))
		fixture.settle()
		started := fixture.client.clock.now()
		fixture.mustSchedule(10, first, one, 0, 0)
		fixture.mustSchedule(10, second, two, 0, 0)
		// Tick by tick while the first run sleeps its 60ms: on one timer the
		// second task has not started, on two it started with the first.
		for fixture.client.clock.now()-started < 60*time.Millisecond {
			tickSession(t, fixture.session, 1)
			words := fixture.words(fixture.session)
			if shared && (words[0] != 1 || words[1] != 0) {
				t.Fatalf("at %v one timer has started %v of its two tasks, want the first alone",
					fixture.client.clock.now()-started, words[:2])
			}
			if !shared && (words[0] != 1 || words[1] != 1) {
				t.Fatalf("at %v two timers have started %v, want both tasks", fixture.client.clock.now()-started, words[:2])
			}
		}
		tickSession(t, fixture.session, 6)
		if words := fixture.words(fixture.session); words[0] != 1 || words[1] != 1 {
			t.Fatalf("after the first run the counts are %v, want both tasks run once", words[:2])
		}
	}
}

// Timer.cancel discards what was queued, refuses a schedule after it, and
// leaves its tasks scheduled in name, so cancelling one of them still answers
// true. TimerTask.cancel answers whether the task had a run to come, and a
// task cancelled before it was scheduled cannot be scheduled.
func TestJavaTimerCancelSemantics(t *testing.T) {
	fixture := newTimerFixture(t)
	client := fixture.client
	timer, other := fixture.newTimer(), fixture.newTimer()
	class := fixture.newTaskClass("game/Tick", fixture.countRun(0))
	queued, spare, never, once, repeating := fixture.newTaskOf(class), fixture.newTaskOf(class),
		fixture.newTaskOf(class), fixture.newTaskOf(class), fixture.newTaskOf(class)
	fixture.settle()

	fixture.mustSchedule(10, timer, queued, 100, 0)
	mustSlot(t, client, javaTimerClass, 16, timer)
	if err := fixture.schedule(10, timer, spare, 0, 0); thrownClass(err) != javaIllegalStateClass {
		t.Errorf("a schedule on a cancelled timer = %v, want IllegalStateException", err)
	}
	if answer := mustSlot(t, client, javaTimerTaskClass, 11, queued); answer != javaTrue {
		t.Error("cancelling a task its cancelled timer had queued answered false")
	}

	if answer := mustSlot(t, client, javaTimerTaskClass, 11, never); answer != javaFalse {
		t.Error("cancelling a task never scheduled answered true")
	}
	if err := fixture.schedule(10, other, never, 0, 0); thrownClass(err) != javaIllegalStateClass {
		t.Errorf("scheduling a cancelled task = %v, want IllegalStateException", err)
	}

	fixture.mustSchedule(10, other, once, 30, 0)
	if answer := mustSlot(t, client, javaTimerTaskClass, 11, once); answer != javaTrue {
		t.Error("cancelling a single run still to come answered false")
	}
	if answer := mustSlot(t, client, javaTimerTaskClass, 11, once); answer != javaFalse {
		t.Error("cancelling a cancelled task again answered true")
	}
	fixture.mustSchedule(12, other, repeating, 0, 0, 20, 0)
	tickSession(t, fixture.session, 4)
	runs := fixture.words(fixture.session)[0]
	if runs < 2 {
		t.Fatalf("the repeating task ran %d times in four ticks", runs)
	}
	if answer := mustSlot(t, client, javaTimerTaskClass, 11, repeating); answer != javaTrue {
		t.Error("cancelling a repeating task answered false")
	}
	tickSession(t, fixture.session, 6)
	if after := fixture.words(fixture.session)[0]; after != runs {
		t.Errorf("the tasks ran %d times after their cancels, want %d: the queued, cancelled and single tasks never, the repeating one no more", after, runs)
	}
}

// A task that cancels itself, or its timer, from inside its own run is not run
// again, and what else the timer had queued is not run at all.
func TestJavaTimerTaskCancelledFromItsOwnRun(t *testing.T) {
	fixture := newTimerFixture(t)
	selfTimer, timer := fixture.newTimer(), fixture.newTimer()
	// A run counts and cancels the task it is the run of; the task is passed
	// in word 6, which the test writes once the task is built.
	selfClass := fixture.newTaskClass("game/Self", fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		fixture.increment(a, 0)
		a.literal(4, fixture.counter(6))
		a.emit(armLdr(0, 4, 0))
		a.literal(5, fixture.stubs["taskCancel"])
		a.call(5)
		a.emit(armPopPC)
	}))
	self := fixture.newTaskOf(selfClass)
	if err := fixture.client.writeWord(fixture.counter(6), self); err != nil {
		t.Fatal(err)
	}
	ender := fixture.newTask("game/Ender", fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		fixture.increment(a, 1)
		fixture.call(a, "timerCancel", timer)
		a.emit(armPopPC)
	}))
	later := fixture.newTask("game/Later", fixture.countRun(2))
	fixture.settle()
	fixture.mustSchedule(12, selfTimer, self, 0, 0, 20, 0)
	fixture.mustSchedule(14, timer, ender, 0, 0, 20, 0)
	fixture.mustSchedule(10, timer, later, 40, 0)
	tickSession(t, fixture.session, 10)
	if words := fixture.words(fixture.session); words[0] != 1 || words[1] != 1 || words[2] != 0 {
		t.Fatalf("the runs counted %v, want each canceller once and the task queued behind the timer's cancel never", words[:3])
	}
}

// An exception a task does not catch ends its run and its timer: what the
// timer had queued never runs, a schedule on it is refused, and the
// exception is reported the way one that ends a thread is.
func TestJavaTimerEndsWithAnUncaughtException(t *testing.T) {
	fixture := newTimerFixture(t)
	timer := fixture.newTimer()
	thrower := fixture.newTask("game/Thrower", fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		fixture.increment(a, 0)
		fixture.call(a, "throw", 0)
		fixture.increment(a, 1) // not reached: the throw does not return
		a.emit(armPopPC)
	}))
	later := fixture.newTask("game/Later", fixture.countRun(2))
	spare := fixture.newTask("game/Spare", fixture.countRun(3))
	fixture.settle()
	fixture.mustSchedule(10, timer, thrower, 0, 0)
	fixture.mustSchedule(10, timer, later, 30, 0)
	tickSession(t, fixture.session, 6)
	if words := fixture.words(fixture.session); words[0] != 1 || words[1] != 0 || words[2] != 0 {
		t.Fatalf("the runs counted %v, want the thrower once and nothing after it", words[:3])
	}
	if count, first := fixture.session.UncaughtCallbacks(); count != 1 || !strings.Contains(first, javaThrowNullClass) {
		t.Errorf("uncaught callbacks = %d (%q), want the one NullPointerException", count, first)
	}
	if err := fixture.schedule(10, timer, spare, 0, 0); thrownClass(err) != javaIllegalStateClass {
		t.Errorf("a schedule on the timer the exception ended = %v, want IllegalStateException", err)
	}
}

// Every refusal is the exception the specification names, in the order SKT's
// checks run; a period of zero is a single run, as on KTF and SKT; and a task
// already scheduled cannot be scheduled again, on any timer.
func TestJavaTimerScheduleRefusals(t *testing.T) {
	fixture := newTimerFixture(t)
	client := fixture.client
	timer, other := fixture.newTimer(), fixture.newTimer()
	class := fixture.newTaskClass("game/Tick", fixture.countRun(0))
	task, zero := fixture.newTaskOf(class), fixture.newTaskOf(class)
	fixture.settle()
	negative := uint32(0xffffffff)
	for _, probe := range []struct {
		name      string
		slot      uint32
		arguments []uint32
		want      string
	}{
		{"a null task", 10, []uint32{timer, 0, 10, 0}, javaThrowNullClass},
		{"a null task with a period", 14, []uint32{timer, 0, 10, 0, 10, 0}, javaThrowNullClass},
		{"a negative delay", 10, []uint32{timer, task, negative, negative}, javaIllegalArgumentClass},
		{"a negative period", 12, []uint32{timer, task, 0, 0, negative, negative}, javaIllegalArgumentClass},
		{"a delay past the clock", 10, []uint32{timer, task, negative, 0x7fffffff}, javaIllegalArgumentClass},
		{"a null task with a date", 11, []uint32{timer, 0, 0}, javaThrowNullClass},
		{"a null date", 11, []uint32{timer, task, 0}, javaThrowNullClass},
		{"a null date with a period", 15, []uint32{timer, task, 0, 10, 0}, javaThrowNullClass},
	} {
		if err := fixture.schedule(probe.slot, probe.arguments...); thrownClass(err) != probe.want {
			t.Errorf("%s: %v, want %s", probe.name, err, probe.want)
		}
	}
	if len(client.javaRun.timerTasks) != 0 {
		t.Fatal("a refused schedule left a task behind")
	}

	fixture.mustSchedule(12, timer, zero, 0, 0, 0, 0)
	fixture.mustSchedule(10, timer, task, 0, 0)
	for _, target := range []uint32{timer, other} {
		if err := fixture.schedule(10, target, task, 0, 0); thrownClass(err) != javaIllegalStateClass {
			t.Errorf("scheduling a task again = %v, want IllegalStateException", err)
		}
	}
	tickSession(t, fixture.session, 6)
	if runs := fixture.words(fixture.session)[0]; runs != 2 {
		t.Errorf("the period-zero task and the single one ran %d times together, want once each", runs)
	}
}

// A first time named by a Date is a delay of how far it lies past the guest
// clock: one already gone is due at once, one ahead is due then. A title's own
// Date answering getTime with code of its own is refused by name.
func TestJavaTimerScheduleByDate(t *testing.T) {
	fixture := newTimerFixture(t)
	client := fixture.client
	timer := fixture.newTimer()
	class := fixture.newTaskClass("game/Tick", fixture.countRun(0))
	past, ahead, own := fixture.newTaskOf(class), fixture.newTaskOf(class), fixture.newTaskOf(class)
	date := func(at int64) uint32 {
		object, err := newTestObject(t, client, javaDateClass)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := javaDateAt(client, context.Background(), nil, []uint32{object, uint32(at), uint32(uint64(at) >> 32)}); err != nil {
			t.Fatal(err)
		}
		return object
	}
	now := client.clock.unixMillis()
	gone, soon := date(now-5000), date(now+200)
	mine := installGuestSubclass(t, client, "game/Moment", javaDateClass, 4, 0,
		guestRoutine{slot: javaDateSlotGetTime, code: []uint16{0x2000, 0x4770}}) // movs r0, #0; bx lr
	fixture.settle()
	fixture.mustSchedule(11, timer, past, gone)
	fixture.mustSchedule(13, timer, ahead, soon, 1000, 0)
	if err := fixture.schedule(11, timer, own, mine); err == nil || !strings.Contains(err.Error(), "getTime") {
		t.Errorf("a schedule by a title's own Date = %v, want it refused by name", err)
	}
	if got := client.javaRun.timerTasks[past].next; got != client.clock.now() {
		t.Errorf("a date already gone is due at %v, want now (%v)", got, client.clock.now())
	}
	if got, want := client.javaRun.timerTasks[ahead].next, client.clock.now()+200*time.Millisecond; got != want {
		t.Errorf("a date ahead is due at %v, want %v", got, want)
	}
	if task := client.javaRun.timerTasks[ahead]; task.period != time.Second || task.fixedRate {
		t.Errorf("the date form with a period scheduled %+v, want a fixed delay of a second", task)
	}
}

// The tasks waiting to run are bounded: past maxJavaTimerTasks a schedule
// throws OutOfMemoryError, as a thread started past the thread bound does.
func TestJavaTimerQueueIsBounded(t *testing.T) {
	fixture := newTimerFixture(t)
	timer := fixture.newTimer()
	class := fixture.newTaskClass("game/Tick", fixture.countRun(0))
	for index := 0; index < maxJavaTimerTasks; index++ {
		fixture.mustSchedule(10, timer, fixture.newTaskOf(class), 10000, 0)
	}
	if err := fixture.schedule(10, timer, fixture.newTaskOf(class), 10000, 0); thrownClass(err) != javaOutOfMemoryClass {
		t.Errorf("a schedule past the bound = %v, want OutOfMemoryError", err)
	}
}

// A scheduled task is often held by nothing but its timer. The collector
// keeps it, and the timer, alive through as many cycles as it likes, because
// the timers' own block names them; the task then runs.
func TestJavaTimerKeepsWhatItHoldsFromTheCollector(t *testing.T) {
	fixture := newTimerFixture(t)
	client := fixture.client
	timer := fixture.newTimer()
	task := fixture.newTask("game/Held", fixture.countRun(0))
	fixture.mustSchedule(10, timer, task, 200, 0)
	fixture.settle()
	for cycle := 0; cycle < 3; cycle++ {
		if _, err := client.collectJavaObjects(nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, object := range []uint32{timer, task} {
		if _, tracked := client.javaRun.objects[object]; !tracked {
			t.Fatalf("the collector freed %#x, which the timer holds", object)
		}
	}
	words, err := client.readJavaTimerRoots(client.javaRun.timerRoots)
	if err != nil {
		t.Fatal(err)
	}
	if len(words) != 2 || words[0] != timer || words[1] != task {
		t.Fatalf("the timers' block holds %#x, want the timer and its task", words)
	}
	tickSession(t, fixture.session, 8)
	if runs := fixture.words(fixture.session)[0]; runs != 1 {
		t.Fatalf("the held task ran %d times, want once", runs)
	}
}

// A task's run is found by name, as a thread's is, and otherwise at
// TimerTask's run slot in the task's own dispatch table, where the compiler
// puts the override; a task with neither stops with its class's name.
func TestJavaTimerFindsATasksRun(t *testing.T) {
	fixture := newTimerFixture(t)
	client := fixture.client
	timer := fixture.newTimer()
	byName := fixture.newTask("game/ByName", fixture.countRun(0))
	bySlot := installGuestSubclass(t, client, "game/BySlot", javaTimerTaskClass, 4, 0)
	class, _ := client.javaClassOfObject(bySlot)
	if err := client.writeWord(class.VTable+4+javaTimerTaskSlotRun*4, fixture.countRun(1)); err != nil {
		t.Fatal(err)
	}
	if body, _ := client.javaTimerTaskBody(class); body == 0 {
		t.Fatal("the run at TimerTask's slot was not found")
	}
	runless := installGuestSubclass(t, client, "game/Runless", javaTimerTaskClass, 4, 0)
	runlessClass, _ := client.javaClassOfObject(runless)
	if body, _ := client.javaTimerTaskBody(runlessClass); body != 0 {
		t.Fatal("a task whose slot still holds TimerTask's own stub found a run")
	}
	fixture.settle()
	fixture.mustSchedule(10, timer, byName, 0, 0)
	fixture.mustSchedule(10, fixture.newTimer(), bySlot, 0, 0)
	fixture.settle()
	tickSession(t, fixture.session, 3)
	if words := fixture.words(fixture.session); words[0] != 1 || words[1] != 1 {
		t.Fatalf("the tasks counted %v, want each run once", words[:2])
	}
	fixture.mustSchedule(10, fixture.newTimer(), runless, 0, 0)
	fixture.settle()
	err := fixture.session.Tick(context.Background())
	if err == nil || !strings.Contains(err.Error(), "game/Runless declares no run") {
		t.Fatalf("a task with no run = %v, want it named", err)
	}
}

// A run that finds every stack taken waits for one, without throwing, and is
// not reported as due while it waits — a deadline the clock could never move
// past — and runs once a thread has ended.
func TestJavaTimerWaitsForAStack(t *testing.T) {
	fixture := newTimerFixture(t)
	client := fixture.client
	sleeper := fixture.install(func(a *assembler) {
		a.emit(armPushLR)
		fixture.call(a, "sleep", 100, 0)
		a.emit(armPopPC)
	})
	for index := 0; index < maxJavaThreads; index++ {
		fixture.start("Holder", sleeper)
	}
	timer := fixture.newTimer()
	task := fixture.newTask("game/Late", fixture.countRun(0))
	fixture.settle()
	tickSession(t, fixture.session, 1)
	fixture.mustSchedule(10, timer, task, 0, 0)
	tickSession(t, fixture.session, 1)
	if runs := fixture.words(fixture.session)[0]; runs != 0 {
		t.Fatal("a run started with every stack taken")
	}
	if wait, pending := client.nextJavaThreadDue(); !pending || wait == 0 {
		t.Fatalf("the next deadline is %v (%v): the waiting run was reported as due", wait, pending)
	}
	tickSession(t, fixture.session, 6)
	if runs := fixture.words(fixture.session)[0]; runs != 1 {
		t.Fatalf("the waiting run ran %d times once the stacks were free, want once", runs)
	}
}

// What this platform keeps for a timer or a task the collector has taken is
// neither recorded by a checkpoint nor kept past the next constructor, which
// is the one place a new one can arrive at the old one's address — and one
// that does arrive there starts afresh.
func TestJavaTimerForgetsWhatTheCollectorTook(t *testing.T) {
	fixture := newTimerFixture(t)
	client := fixture.client
	idle := fixture.newTimer()
	cancelled := fixture.newTask("game/Gone", fixture.countRun(0))
	mustSlot(t, client, javaTimerClass, 16, idle)
	mustSlot(t, client, javaTimerTaskClass, 11, cancelled)
	fixture.settle()
	for cycle := 0; cycle < 2; cycle++ {
		if _, err := client.collectJavaObjects(nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, object := range []uint32{idle, cancelled} {
		if _, tracked := client.javaRun.objects[object]; tracked {
			t.Fatalf("%#x is still tracked; the test needs it collected", object)
		}
	}
	saved, err := client.captureJavaSchedulingState()
	if err != nil {
		t.Fatal(err)
	}
	if saved != nil && (len(saved.Timers) != 0 || len(saved.Tasks) != 0) {
		t.Errorf("a checkpoint records what the collector took: %+v", saved)
	}
	client.forgetStaleJavaTimers()
	client.forgetStaleJavaTimerTasks()
	if client.javaRun.timers[idle] != nil || client.javaRun.timerTasks[cancelled] != nil {
		t.Fatal("what was kept for a collected timer or task is still kept")
	}
	reused := fixture.newTimer()
	if timer := client.javaRun.timers[reused]; timer == nil || timer.cancelled {
		t.Errorf("a new timer starts as %+v, want a fresh one", timer)
	}
}

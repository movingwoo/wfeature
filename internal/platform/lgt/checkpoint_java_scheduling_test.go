package lgt

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// decodeScheduling reads a quick save's scheduling record, or nil for one
// written in an earlier shape.
func decodeScheduling(t *testing.T, checkpoint backend.Checkpoint) *javaSchedulingState {
	t.Helper()
	_, _, _, _, scheduling, err := decodeLGTSchedulingRecord(checkpoint.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	return scheduling
}

// inEarlierShape is a quick save as a build before the scheduling record
// would have written it: the same records around it, without it.
func inEarlierShape(t *testing.T, checkpoint backend.Checkpoint, media *javaMediaState, library *javaLibraryState,
	screen *javaScreenState) backend.Checkpoint {
	t.Helper()
	session, _, _, _, _, err := decodeLGTSchedulingRecord(checkpoint.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	earlier := checkpoint
	if earlier.Runtime, err = encodeLGTScreenRecord(session, media, library, screen); err != nil {
		t.Fatal(err)
	}
	return earlier
}

// Every shape an earlier build wrote still decodes, with no scheduling state —
// which is what such a build left — and a session holding none is written in
// exactly the shape it would have been written in without this record. With
// scheduling state beside them, the other records come back as well.
func TestJavaSchedulingRecordLeavesEarlierRecordsReadable(t *testing.T) {
	session := sessionCheckpointState{Version: sessionCheckpointVersion}
	media := &javaMediaState{}
	library := &javaLibraryState{Numbers: []javaNumberState{{Object: 0x1000, Value: 7}}}
	screen := &javaScreenState{Font: javaFontState{Size: 8}}
	held := &javaSchedulingState{
		Roots: 0x3000, Order: 2,
		Timers: []javaTimerState{{Object: 0x2000, Thread: 0x2100, Queue: []uint32{0x2200}}},
		Tasks: []javaTimerTaskState{{Object: 0x2200, Timer: 0x2000, State: uint8(javaTimerTaskScheduled),
			Next: 40 * time.Millisecond, Period: 20 * time.Millisecond, FixedRate: true, Last: 9, Order: 2}},
		Interrupted: []uint32{0x2100},
	}
	for _, shape := range []struct {
		name    string
		media   *javaMediaState
		library *javaLibraryState
		screen  *javaScreenState
	}{
		{"session alone", nil, nil, nil},
		{"media wrapper", media, nil, nil},
		{"class library", nil, library, nil},
		{"screen", nil, nil, screen},
		{"all three", media, library, screen},
	} {
		earlier, err := encodeLGTScreenRecord(session, shape.media, shape.library, shape.screen)
		if err != nil {
			t.Fatalf("%s: %v", shape.name, err)
		}
		_, decodedMedia, decodedLibrary, decodedScreen, scheduling, err := decodeLGTSchedulingRecord(earlier)
		if err != nil {
			t.Fatalf("%s: %v", shape.name, err)
		}
		if scheduling != nil || (decodedMedia != nil) != (shape.media != nil) ||
			!reflect.DeepEqual(decodedLibrary, shape.library) || !reflect.DeepEqual(decodedScreen, shape.screen) {
			t.Errorf("%s came back as media %v, library %+v, screen %+v, scheduling %+v",
				shape.name, decodedMedia, decodedLibrary, decodedScreen, scheduling)
		}
		written, err := encodeLGTSchedulingRecord(session, shape.media, shape.library, shape.screen, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(written, earlier) {
			t.Errorf("%s with no scheduling state is not written in the earlier shape", shape.name)
		}

		record, err := encodeLGTSchedulingRecord(session, shape.media, shape.library, shape.screen, held)
		if err != nil {
			t.Fatal(err)
		}
		if version, ok := leadingRecordVersion(record); !ok || version != sessionSchedulingCheckpointVersion {
			t.Fatalf("%s: the record opens with version %d (%v), want %d", shape.name, version, ok, sessionSchedulingCheckpointVersion)
		}
		_, decodedMedia, decodedLibrary, decodedScreen, scheduling, err = decodeLGTSchedulingRecord(record)
		if err != nil || !reflect.DeepEqual(scheduling, held) || (decodedMedia != nil) != (shape.media != nil) ||
			!reflect.DeepEqual(decodedLibrary, shape.library) || !reflect.DeepEqual(decodedScreen, shape.screen) {
			t.Errorf("%s with scheduling state came back as media %v, library %+v, screen %+v, scheduling %+v, %v",
				shape.name, decodedMedia, decodedLibrary, decodedScreen, scheduling, err)
		}
	}
}

// What a record claims is checked against the Java runtime it is restored
// onto before any of it is applied, and a record a strict reader refuses is
// refused whole.
func TestJavaSchedulingRecordRefusesWhatItCannotRestore(t *testing.T) {
	fixture := newTimerFixture(t)
	client := fixture.client
	timer, other := fixture.newTimer(), fixture.newTimer()
	task := fixture.newTask("game/Tick", fixture.sleepyRun(0, 200))
	spare := fixture.newTask("game/Spare", fixture.countRun(1))
	thread := fixture.build("Plain", fixture.countRun(2))
	fixture.settle()
	fixture.mustSchedule(12, timer, task, 0, 0, 500, 0)
	fixture.mustSchedule(10, other, spare, 1000, 0)
	tickSession(t, fixture.session, 1)
	main, err := javaCurrentThread(client, context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fixture.interrupt(thread)
	saved, err := client.captureJavaSchedulingState()
	if err != nil || saved == nil {
		t.Fatalf("captureJavaSchedulingState() = %+v, %v", saved, err)
	}
	if err := client.validateJavaSchedulingState(saved); err != nil {
		t.Fatalf("the captured record is refused: %v", err)
	}
	// The record lists the timers and tasks in the order of the objects.
	first, second := 0, 1
	if saved.Timers[0].Object != timer {
		first, second = 1, 0
	}
	running := client.javaRun.timers[timer].thread
	clone := func() *javaSchedulingState {
		copied := *saved
		copied.Timers = slices.Clone(saved.Timers)
		for index := range copied.Timers {
			copied.Timers[index].Queue = slices.Clone(saved.Timers[index].Queue)
		}
		copied.Tasks = slices.Clone(saved.Tasks)
		copied.Interrupted = slices.Clone(saved.Interrupted)
		return &copied
	}
	taskIndex := func(state *javaSchedulingState, object uint32) int {
		for index, record := range state.Tasks {
			if record.Object == object {
				return index
			}
		}
		t.Fatalf("the record has no task %#x", object)
		return -1
	}
	// A block the arena handed out, large enough, that is an object's own.
	array, err := client.newJavaByteArray(make([]byte, 5000))
	if err != nil {
		t.Fatal(err)
	}
	objectBlock := client.javaRun.objects[array].block
	type damage struct {
		reason string
		apply  func(*javaSchedulingState)
	}
	for name, probe := range map[string]damage{
		"tasks out of order": {"tasks out of order", func(s *javaSchedulingState) {
			s.Tasks[0], s.Tasks[1] = s.Tasks[1], s.Tasks[0]
		}},
		"timers out of order": {"timers out of order", func(s *javaSchedulingState) {
			s.Timers[0], s.Timers[1] = s.Timers[1], s.Timers[0]
		}},
		"a task in no state": {"an impossible task", func(s *javaSchedulingState) {
			s.Tasks[taskIndex(s, spare)].State = 9
		}},
		"a spent task with a period": {"an impossible task", func(s *javaSchedulingState) {
			s.Tasks[taskIndex(s, task)].State = uint8(javaTimerTaskExecuted)
		}},
		"a negative period": {"an impossible task", func(s *javaSchedulingState) {
			s.Tasks[taskIndex(s, task)].Period = -1
		}},
		"an order past the last": {"an impossible task", func(s *javaSchedulingState) {
			s.Tasks[taskIndex(s, spare)].Order = s.Order + 1
		}},
		"a scheduled task of nothing": {"an impossible task", func(s *javaSchedulingState) {
			s.Tasks[taskIndex(s, spare)].Timer = 0
		}},
		"a cancelled timer with work": {"a cancelled timer with tasks to run", func(s *javaSchedulingState) {
			s.Timers[second].Cancelled = true
		}},
		"a queued task of another timer": {"not that timer's to run", func(s *javaSchedulingState) {
			s.Timers[first].Queue = append(s.Timers[first].Queue, spare)
		}},
		"a task queued twice": {"not that timer's to run", func(s *javaSchedulingState) {
			s.Timers[second].Queue = []uint32{spare, spare}
		}},
		"a queued task spent": {"not that timer's to run", func(s *javaSchedulingState) {
			s.Tasks[taskIndex(s, spare)].State = uint8(javaTimerTaskExecuted)
		}},
		"a queued task with no record": {"not that timer's to run", func(s *javaSchedulingState) {
			s.Timers[second].Queue = []uint32{0x7000}
		}},
		"a timer on no restored thread": {"a thread the Java record did not restore", func(s *javaSchedulingState) {
			s.Timers[second].Thread = 0x7000
		}},
		"a timer on the platform's thread": {"a thread the Java record did not restore", func(s *javaSchedulingState) {
			s.Timers[second].Thread = main
		}},
		"two timers on one thread": {"a thread the Java record did not restore", func(s *javaSchedulingState) {
			s.Timers[second].Thread = running
		}},
		"a run of another timer's task": {"a task that is not its own", func(s *javaSchedulingState) {
			s.Tasks[taskIndex(s, task)].Timer = other
			s.Timers[first].Queue = nil
		}},
		"an interrupt of no restored thread": {"an interrupt for a thread", func(s *javaSchedulingState) {
			s.Interrupted = []uint32{0x7000}
		}},
		"interrupts out of order": {"an interrupt for a thread", func(s *javaSchedulingState) {
			s.Interrupted = []uint32{max(thread, main), min(thread, main)}
		}},
		"work and no block":         {"no block to hold it", func(s *javaSchedulingState) { s.Roots = 0 }},
		"a block not handed out":    {"not one the arena handed out", func(s *javaSchedulingState) { s.Roots = 0x7000 }},
		"a block too small":         {"not one the arena handed out", func(s *javaSchedulingState) { s.Roots = task }},
		"a block that is an object": {"is an object's", func(s *javaSchedulingState) { s.Roots = objectBlock }},
	} {
		damaged := clone()
		probe.apply(damaged)
		err := client.validateJavaSchedulingState(damaged)
		if err == nil {
			t.Errorf("a record with %s was accepted", name)
		} else if !strings.Contains(err.Error(), probe.reason) {
			t.Errorf("a record with %s was refused for %v, want %q", name, err, probe.reason)
		}
	}
	many := clone()
	many.Timers[second].Queue = make([]uint32, maxJavaTimerTasks+1)
	for index := range many.Timers[second].Queue {
		many.Timers[second].Queue[index] = 0x10000 + uint32(index)*16
		many.Tasks = append(many.Tasks, javaTimerTaskState{Object: 0x10000 + uint32(index)*16, Timer: other,
			State: uint8(javaTimerTaskScheduled), Order: 1})
	}
	slices.SortFunc(many.Tasks, func(a, b javaTimerTaskState) int { return int(a.Object) - int(b.Object) })
	if err := client.validateJavaSchedulingState(many); err == nil {
		t.Error("a record queuing more tasks than a session holds was accepted")
	}
	if err := client.restoreJavaSchedulingState(nil); err != nil {
		t.Errorf("a record with no scheduling state was refused: %v", err)
	}

	record, err := encodeLGTSchedulingRecord(sessionCheckpointState{Version: sessionCheckpointVersion}, nil, nil, nil, saved)
	if err != nil {
		t.Fatal(err)
	}
	for name, damaged := range map[string][]byte{
		"unknown field": bytes.Replace(record, []byte(`"Scheduling":`), []byte(`"Unknown":0,"Scheduling":`), 1),
		"missing field": bytes.Replace(record, []byte(`"Scheduling":`), []byte(`"Other":`), 1),
		"other version": bytes.Replace(record, []byte(`"Version":7,`), []byte(`"Version":8,`), 1),
	} {
		if bytes.Equal(damaged, record) {
			t.Fatalf("%s: the record was not changed", name)
		}
		if _, _, _, _, _, err := decodeLGTSchedulingRecord(damaged); err == nil {
			t.Errorf("a record with %s was decoded", name)
		}
	}

	client.javaRun = nil
	if err := client.restoreJavaSchedulingState(saved); err == nil {
		t.Error("scheduling state was restored onto a session with no Java runtime")
	}
}

// Through whole quick saves and loads, a timer due later fires at the same
// guest time after a load as it does in the session the save was taken from,
// and so does every run after it — a repeating one at a fixed rate on the same
// timer, and one on another timer whose run is parked in its own sleep when
// the save is taken. The restored session is the same session tick for tick:
// the counts, the frames, the clock and every page of guest memory.
func TestJavaTimerRoundTripsThroughAQuickSave(t *testing.T) {
	fixture := newTimerFixture(t)
	client := fixture.client
	timer, other := fixture.newTimer(), fixture.newTimer()
	later := fixture.newTask("game/Later", fixture.countRun(0))
	beat := fixture.newTask("game/Beat", fixture.countRun(1))
	slow := fixture.newTask("game/Slow", fixture.sleepyRun(2, 70))
	fixture.settle()
	fixture.mustSchedule(10, timer, later, 400, 0)
	fixture.mustSchedule(14, timer, beat, 0, 0, 40, 0)
	fixture.mustSchedule(12, other, slow, 10, 0, 100, 0)

	midRun := false
	for boundary := 0; boundary < 6; boundary++ {
		checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
		if version, _ := leadingRecordVersion(checkpoint.Runtime); version != sessionSchedulingCheckpointVersion {
			t.Fatalf("boundary %d: the quick save opens with version %d, want %d", boundary, version, sessionSchedulingCheckpointVersion)
		}
		if scheduling := decodeScheduling(t, checkpoint); scheduling == nil || len(scheduling.Timers) != 2 {
			t.Fatalf("boundary %d: the quick save holds %+v, want both timers", boundary, scheduling)
		}
		midRun = midRun || client.javaTimerRunning(client.javaRun.timers[other])
		restored := fixture.restore(checkpoint)
		fixture.compare(restored, 7)
		_ = restored.Close(context.Background())
	}
	if !midRun {
		t.Error("no quick save was taken with a run parked inside its task")
	}
	words := fixture.words(fixture.session)
	if words[0] != 1 || words[1] < 10 || words[2] < 3 {
		t.Errorf("the tasks counted %v, want the later one once and the others repeatedly", words[:3])
	}
}

// A quick save in every shape an earlier build wrote still loads into a
// session that holds no timer, which is what that build kept: nothing a timer
// had queued runs. The same save as this build writes it runs them.
func TestJavaTimerQuickSaveInAnEarlierShapeLoads(t *testing.T) {
	fixture := newTimerFixture(t)
	timer := fixture.newTimer()
	task := fixture.newTask("game/Tick", fixture.countRun(0))
	fixture.settle()
	fixture.mustSchedule(14, timer, task, 30, 0, 40, 0)
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	current := fixture.restore(checkpoint)
	tickSession(t, current, 6)
	if runs := fixture.words(current)[0]; runs == 0 {
		t.Fatal("the quick save as this build writes it ran nothing")
	}
	for name, shape := range map[string]struct {
		media   *javaMediaState
		library *javaLibraryState
		screen  *javaScreenState
	}{
		"session alone": {}, "media wrapper": {media: &javaMediaState{}},
		"class library": {library: &javaLibraryState{}}, "screen": {screen: &javaScreenState{Font: javaFontState{Size: 8}}},
	} {
		earlier := inEarlierShape(t, checkpoint, shape.media, shape.library, shape.screen)
		if version, _ := leadingRecordVersion(earlier.Runtime); version == sessionSchedulingCheckpointVersion {
			t.Fatalf("%s: the earlier shape opens with this record's version", name)
		}
		restored := fixture.restore(earlier)
		if timers := restored.client.javaRun.timers; len(timers) != 0 {
			t.Errorf("%s: the earlier shape restored %d timers", name, len(timers))
		}
		tickSession(t, restored, 6)
		if runs := fixture.words(restored)[0]; runs != 0 {
			t.Errorf("%s: the earlier shape ran a task %d times", name, runs)
		}
		_ = restored.Close(context.Background())
	}
}

// A pending interrupt is kept by a quick save: the restored thread's first
// sleep throws, as the source's does. A quick save in the earlier shape has no
// interrupt, and the restored thread sleeps.
func TestJavaInterruptRoundTripsThroughAQuickSave(t *testing.T) {
	fixture := newTimerFixture(t)
	thread := fixture.build("Early", fixture.catching(0, func(a *assembler) {
		fixture.call(a, "sleep", 10000, 0)
		fixture.increment(a, 1)
	}))
	fixture.interrupt(thread)
	if _, err := javaThreadStart(fixture.client, context.Background(), nil, []uint32{thread}); err != nil {
		t.Fatal(err)
	}
	fixture.settle()
	checkpoint := roundTripCheckpoint(t, fixture.archive, fixture.session)
	if scheduling := decodeScheduling(t, checkpoint); scheduling == nil || !slices.Equal(scheduling.Interrupted, []uint32{thread}) {
		t.Fatalf("the quick save holds %+v, want the one pending interrupt", scheduling)
	}
	restored := fixture.restore(checkpoint)
	fixture.compare(restored, 3)
	if words := fixture.words(restored); words[0] != 1 || words[1] != 0 {
		t.Fatalf("the restored thread counted %v, want its first sleep to throw", words[:2])
	}
	earlier := fixture.restore(inEarlierShape(t, checkpoint, nil, nil, nil))
	tickSession(t, earlier, 3)
	if words := fixture.words(earlier); words[0] != 0 {
		t.Errorf("the earlier shape restored an interrupt: the thread counted %v", words[:2])
	}
}

// **A thread restored inside a sleep finishes that sleep when an interrupt
// wakes it, and its next sleep throws**: the remainder a restored call
// completes has no exception to throw into (checkpoint_worker.go). The
// interrupt is not lost, and it is thrown one blocking call later than in the
// session the save came from, where the sleep it interrupts throws.
func TestJavaInterruptOfAThreadRestoredInItsSleep(t *testing.T) {
	fixture := newTimerFixture(t)
	thread := fixture.start("Sleeper", fixture.catching(0, func(a *assembler) {
		fixture.call(a, "sleep", 10000, 0)
		fixture.increment(a, 1)
		fixture.call(a, "sleep", 10000, 0)
		fixture.increment(a, 2)
	}))
	fixture.settle()
	tickSession(t, fixture.session, 1)
	restored := fixture.restore(roundTripCheckpoint(t, fixture.archive, fixture.session))
	for _, session := range []*Session{fixture.session, restored} {
		if _, err := javaThreadInterrupt(session.client, context.Background(), nil, []uint32{thread}); err != nil {
			t.Fatal(err)
		}
		tickSession(t, session, 1)
	}
	if words := fixture.words(fixture.session); words[0] != 1 || words[1] != 0 {
		t.Errorf("the source's interrupted sleep counted %v, want it to throw", words[:3])
	}
	if words := fixture.words(restored); words[0] != 1 || words[1] != 1 || words[2] != 0 {
		t.Errorf("the restored thread counted %v, want its restored sleep to end and the next to throw", words[:3])
	}
}

// The scheduling record (version 7) wraps the screen state whole, the cards'
// part (version 6's) included, so a session holding both a positioned card and
// a timer keeps both through a quick save; and a version 6 record, which holds
// cards and no scheduling, still reads as the screen record it is.
func TestJavaSchedulingRecordCarriesTheCardsBesideTheTimers(t *testing.T) {
	saved := sessionCheckpointState{}
	screen := &javaScreenState{Cards: javaCardsState{
		Bounds:  []javaCardBoundsState{{Card: 0x1000, X: 10, Y: 20, Width: 30, Height: 40}},
		Surface: 0x2000, Owner: 0x1000,
	}}
	scheduling := &javaSchedulingState{Roots: 0x3000, Order: 2, Interrupted: []uint32{0x4000}}
	record, err := encodeLGTSchedulingRecord(saved, nil, nil, screen, scheduling)
	if err != nil {
		t.Fatal(err)
	}
	if version, ok := leadingRecordVersion(record); !ok || version != sessionSchedulingCheckpointVersion {
		t.Fatalf("the record opens with version %d (%v), want %d", version, ok, sessionSchedulingCheckpointVersion)
	}
	_, _, _, gotScreen, gotScheduling, err := decodeLGTSchedulingRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	if gotScreen == nil || !reflect.DeepEqual(gotScreen.Cards, screen.Cards) {
		t.Fatalf("the cards came back as %+v, want %+v", gotScreen, screen.Cards)
	}
	if gotScheduling == nil || gotScheduling.Roots != scheduling.Roots || gotScheduling.Order != scheduling.Order ||
		!reflect.DeepEqual(gotScheduling.Interrupted, scheduling.Interrupted) {
		t.Fatalf("the scheduling came back as %+v, want %+v", gotScheduling, scheduling)
	}

	cardsOnly, err := encodeLGTSchedulingRecord(saved, nil, nil, screen, nil)
	if err != nil {
		t.Fatal(err)
	}
	if version, _ := leadingRecordVersion(cardsOnly); version != sessionCardScreenCheckpointVersion {
		t.Fatalf("a record with cards and no scheduling opens with version %d, want %d", version, sessionCardScreenCheckpointVersion)
	}
	_, _, _, gotScreen, gotScheduling, err = decodeLGTSchedulingRecord(cardsOnly)
	if err != nil || gotScheduling != nil || gotScreen == nil || !reflect.DeepEqual(gotScreen.Cards, screen.Cards) {
		t.Fatalf("a version 6 record read as screen %+v, scheduling %+v, %v", gotScreen, gotScheduling, err)
	}
}

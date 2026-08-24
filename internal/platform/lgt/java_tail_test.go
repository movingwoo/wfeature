package lgt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A byte-array stream holds the bytes it was built from, and a reader over it
// reads the characters those bytes decode to. The two together are how one
// title reads a text resource it has already loaded.
func TestJavaByteStreamAndReaderReadTheSameBytes(t *testing.T) {
	client := fixtureClient(t)
	// "가" in the handset's own encoding, then two ASCII bytes.
	data := []byte{0xb0, 0xa1, 'A', 'B'}
	array, err := client.newJavaByteArray(data)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.preparePlatformJavaClass(javaInputStreamClass)
	if err != nil {
		t.Fatal(err)
	}
	source, err := client.allocateJavaObject(stream)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaByteStreamConstructor(client, nil, nil, []uint32{source, array}); err != nil {
		t.Fatalf("ByteArrayInputStream([B) error = %v", err)
	}

	reader, err := client.preparePlatformJavaClass(javaInputStreamReaderClass)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := client.allocateJavaObject(reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaWrapStream(client, nil, nil, []uint32{wrapper, source}); err != nil {
		t.Fatalf("InputStreamReader(InputStream) error = %v", err)
	}

	chars, err := client.javaArrayType(1, "C", 2)
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := client.allocateJavaArray(chars.Object, 8)
	if err != nil {
		t.Fatal(err)
	}
	count, err := javaReaderRead(client, nil, nil, []uint32{wrapper, buffer})
	if err != nil {
		t.Fatalf("read([C) error = %v", err)
	}
	// Two bytes made one character and the other two made one each.
	if count != 3 {
		t.Fatalf("read([C) = %d, want 3 characters out of 4 bytes", count)
	}
	units, err := client.readJavaArrayChars(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if got := javaTextOfUnits(units[:count]); got != "가AB" {
		t.Errorf("the reader read %q, want the decoded text", got)
	}
	// The stream is spent, and a reader at the end says so with -1 rather than
	// a count of zero a caller would loop on for ever.
	if again, err := javaReaderRead(client, nil, nil, []uint32{wrapper, buffer}); err != nil ||
		again != ^uint32(0) {
		t.Errorf("read([C) at the end = %d (%v), want -1", int32(again), err)
	}
}

// `readBoolean` is one byte and `readUnsignedByte` is the same byte kept
// unsigned, which is what separates it from the signed `readByte` beside it.
func TestJavaDataStreamReadsBooleansAndUnsignedBytes(t *testing.T) {
	client := fixtureClient(t)
	client.archive = &Archive{Resources: map[string][]byte{"save.dat": {0, 1, 0xff, 0xff, 0x80, 0x01}}}
	name, err := client.newJavaString("/save.dat")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := javaGetResourceAsStream(client, nil, nil, []uint32{0, name})
	if err != nil || stream == 0 {
		t.Fatalf("getResourceAsStream() = %#x, %v", stream, err)
	}

	for _, want := range []uint32{0, 1} {
		value, err := javaStreamReadBoolean(client, nil, nil, []uint32{stream})
		if err != nil {
			t.Fatal(err)
		}
		if value != want {
			t.Errorf("readBoolean() = %d, want %d", value, want)
		}
	}
	value, err := javaStreamReadUnsignedByte(client, nil, nil, []uint32{stream})
	if err != nil {
		t.Fatal(err)
	}
	if value != 0xff {
		t.Errorf("readUnsignedByte() = %d, want 255", value)
	}
	// `readChar` is the halfword `readShort` reads without the sign, which is
	// the whole difference between the two slots.
	thread := armcore.NewThread(armcore.NewContext())
	character, err := javaStreamReadChar(client, nil, thread, []uint32{stream})
	if err != nil {
		t.Fatal(err)
	}
	if character != 0xff80 {
		t.Errorf("readChar() = %#x, want the two bytes unsigned", character)
	}
}

// A stack is the list underneath reached from its end, and `isEmpty` and
// `removeAllElements` are the two Vector slots the same title reaches.
func TestJavaStackPushesPopsAndReportsEmpty(t *testing.T) {
	client := fixtureClient(t)
	vector := newFixtureVector(t, client)

	if empty, err := javaVectorEmpty(client, nil, nil, []uint32{vector}); err != nil || empty != 1 {
		t.Fatalf("isEmpty() on a new list = %d (%v), want true", empty, err)
	}
	for _, element := range []uint32{0x1000, 0x2000} {
		if pushed, err := javaStackPush(client, nil, nil, []uint32{vector, element}); err != nil ||
			pushed != element {
			t.Fatalf("push(%#x) = %#x (%v), want the item back", element, pushed, err)
		}
	}
	if empty, err := javaVectorEmpty(client, nil, nil, []uint32{vector}); err != nil || empty != 0 {
		t.Fatalf("isEmpty() with two elements = %d (%v), want false", empty, err)
	}
	// Last in, first out.
	if top, err := javaStackPop(client, nil, nil, []uint32{vector}); err != nil || top != 0x2000 {
		t.Fatalf("pop() = %#x (%v), want the last pushed", top, err)
	}
	if _, err := javaVectorClear(client, nil, nil, []uint32{vector}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaStackPop(client, nil, nil, []uint32{vector}); err == nil {
		t.Error("pop() on an emptied stack answered instead of reporting")
	}
}

// `indexOf(int)` and `indexOf(int, int)` share an implementation, and the
// second argument is where the search starts rather than what it looks for.
func TestJavaStringIndexOfCharacter(t *testing.T) {
	client := fixtureClient(t)
	text, err := client.newJavaString("a&b&c")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		arguments []uint32
		index     int32
	}{
		{[]uint32{text, '&'}, 1},
		{[]uint32{text, '&', 2}, 3},
		{[]uint32{text, '&', 4}, -1},
		{[]uint32{text, 'z'}, -1},
		// A start below zero is the whole string, the way the language says.
		{[]uint32{text, '&', ^uint32(0)}, 1},
	} {
		index, err := javaStringIndexOfChar(client, nil, nil, want.arguments)
		if err != nil {
			t.Fatalf("indexOf%v error = %v", want.arguments[1:], err)
		}
		if int32(index) != want.index {
			t.Errorf("indexOf%v = %d, want %d", want.arguments[1:], int32(index), want.index)
		}
	}
}

// Closing a database twice is defined as doing nothing the second time, and one
// title closes at the end of its save and again on the way out of the routine
// that called it. An object that was never a database is still a stop.
func TestJavaDatabaseClosesTwiceWithoutFailing(t *testing.T) {
	client := fixtureClient(t)
	name, err := client.newJavaString("save")
	if err != nil {
		t.Fatal(err)
	}
	database, err := javaOpenDataBase(client, nil, nil, []uint32{name, 16, 1})
	if err != nil {
		t.Fatalf("openDataBase() error = %v", err)
	}
	if _, err := javaCloseDataBase(client, nil, nil, []uint32{database}); err != nil {
		t.Fatalf("the first close failed: %v", err)
	}
	if _, err := javaCloseDataBase(client, nil, nil, []uint32{database}); err != nil {
		t.Errorf("the second close failed: %v", err)
	}
	// A closed database is not a database to read records out of.
	if _, err := javaDataBaseRecordCount(client, nil, nil, []uint32{database}); err == nil {
		t.Error("a closed database answered a record count")
	}
	if _, err := javaCloseDataBase(client, nil, nil, []uint32{0xdeadbeef}); err == nil {
		t.Error("closing something this platform never opened was accepted")
	}
}

// A `long[]` is stored into and read back through the pair of interface
// functions that carry a long in two registers.
func TestJavaLongArrayStoreAndLoad(t *testing.T) {
	client := fixtureClient(t)
	longs, err := client.javaArrayType(1, "J", 8)
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.allocateJavaArray(longs.Object, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.storeJavaArrayWide(array, 2, 0x89abcdef, 0x01234567); err != nil {
		t.Fatalf("the store failed: %v", err)
	}
	low, high, err := client.loadJavaArrayWide(array, 2)
	if err != nil {
		t.Fatalf("the load failed: %v", err)
	}
	if low != 0x89abcdef || high != 0x01234567 {
		t.Errorf("read back %#x %#x, want the words that were written", high, low)
	}
	// An untouched element is a zero, and an index past the end is refused
	// rather than written over whatever follows the array.
	if low, high, err := client.loadJavaArrayWide(array, 0); err != nil || low != 0 || high != 0 {
		t.Errorf("an untouched element = %#x %#x (%v), want zero", high, low, err)
	}
	if err := client.storeJavaArrayWide(array, 3, 0, 0); err == nil {
		t.Error("a store past the end was accepted")
	}
	if _, _, err := client.loadJavaArrayWide(array, 3); err == nil {
		t.Error("a read past the end was accepted")
	}
}

// `clipRect` can only narrow: the clip becomes what it and the rectangle have
// in common, and an intersection with nothing in it stays empty.
func TestJavaClipRectOnlyNarrows(t *testing.T) {
	client := fixtureClient(t)
	screen, err := client.screenSurface()
	if err != nil {
		t.Fatal(err)
	}
	graphics, err := client.newJavaGraphics(screen.handle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaSetClip(client, nil, nil, []uint32{graphics, 10, 10, 100, 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaClipRect(client, nil, nil, []uint32{graphics, 50, 0, 100, 40}); err != nil {
		t.Fatalf("clipRect() error = %v", err)
	}
	state, err := client.javaGraphicsState(graphics)
	if err != nil {
		t.Fatal(err)
	}
	if state.clipX != 50 || state.clipY != 10 || state.clipWidth != 60 || state.clipHeight != 30 {
		t.Errorf("the clip is %d,%d %dx%d, want the intersection 50,10 60x30",
			state.clipX, state.clipY, state.clipWidth, state.clipHeight)
	}
	if _, err := javaClipRect(client, nil, nil, []uint32{graphics, 0, 0, 10, 10}); err != nil {
		t.Fatal(err)
	}
	if state.clipWidth != 0 || state.clipHeight != 0 {
		t.Errorf("an empty intersection came out %dx%d, want nothing",
			state.clipWidth, state.clipHeight)
	}
	// A reset puts back the state a fresh Graphics has, clip included.
	if _, err := javaGraphicsReset(client, nil, nil, []uint32{graphics}); err != nil {
		t.Fatal(err)
	}
	if state.clipSet || state.alpha != javaAlphaOpaque || state.surface != screen.handle {
		t.Errorf("reset() left clipSet=%t alpha=%d surface=%#x",
			state.clipSet, state.alpha, state.surface)
	}
}

// A calendar moved to a Date reads its components from that instant rather than
// from the clock it was made on.
func TestJavaCalendarSetTimeMovesTheCalendar(t *testing.T) {
	client := fixtureClient(t)
	calendar, err := javaCalendarGetInstance(client, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	class, err := client.preparePlatformJavaClass(javaDateClass)
	if err != nil {
		t.Fatal(err)
	}
	date, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	// 2001-09-09T01:46:40Z, which is a round number of seconds and a year
	// nothing here can answer by accident.
	var millis uint64 = 1000000000000
	low, high := uint32(millis), uint32(millis>>32)
	if _, err := javaDateAt(client, nil, nil, []uint32{date, low, high}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaCalendarSetTime(client, nil, nil, []uint32{calendar, date}); err != nil {
		t.Fatalf("setTime(Date) error = %v", err)
	}
	year, err := javaCalendarGet(client, nil, nil, []uint32{calendar, javaCalendarYear})
	if err != nil {
		t.Fatal(err)
	}
	if year != 2001 {
		t.Errorf("get(YEAR) after setTime = %d, want 2001", year)
	}
	if _, err := javaCalendarSetTime(client, nil, nil, []uint32{calendar, 0xdeadbeef}); err == nil {
		t.Error("setTime with something that is not a Date was accepted")
	}
}

// `wait` gives the object's lock back and takes it again, at the depth it was
// held. Without that the thread meant to change the condition can never enter
// the body that changes it.
func TestJavaObjectWaitReleasesAndRetakesTheLock(t *testing.T) {
	client := fixtureClient(t)
	const object = uint32(0x30001000)
	for depth := 0; depth < 2; depth++ {
		if err := client.javaMonitorEnter(nil, object); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.waitOnJavaObject(nil, object, 0); err != nil {
		t.Fatalf("wait() error = %v", err)
	}
	monitor := client.javaRuntimeState().monitors[object]
	if monitor == nil || monitor.count != 2 {
		t.Fatalf("the lock came back at depth %v, want 2", monitor)
	}
	for depth := 0; depth < 2; depth++ {
		if err := client.javaMonitorExit(object); err != nil {
			t.Fatal(err)
		}
	}
	// A wait on a lock the waiting thread does not hold is a stop, not a
	// silent handover.
	if _, err := client.waitOnJavaObject(nil, object, 0); err == nil {
		t.Error("wait() on a lock nobody holds was accepted")
	}
}

// The priority a title sets is kept and changes nothing else: a guest thread
// here is scheduled by the slice the session grants it.
func TestJavaThreadSetPriorityIsKept(t *testing.T) {
	client := fixtureClient(t)
	object, err := javaCurrentThread(client, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := javaThreadSetPriority(client, nil, nil, []uint32{object, 10}); err != nil {
		t.Fatalf("setPriority(10) error = %v", err)
	}
	if got := client.javaRuntimeState().threads[object].priority; got != 10 {
		t.Errorf("the thread's priority is %d, want 10", got)
	}
	if _, err := javaThreadSetPriority(client, nil, nil, []uint32{0xdeadbeef, 10}); err == nil {
		t.Error("setPriority on something that is not a thread was accepted")
	}
}

// The sixty-four bit pair answer in the register pair a long comes back in, and
// they compare as signed numbers.
func TestJavaMathLongPickTheRightEnd(t *testing.T) {
	client := fixtureClient(t)
	thread := armcore.NewThread(armcore.NewContext())
	// -1 and 1, as the module hands them over: low word first.
	arguments := []uint32{0xffffffff, 0xffffffff, 1, 0}
	low, err := javaMathLong(false)(client, nil, thread, arguments)
	if err != nil {
		t.Fatal(err)
	}
	high, err := thread.Register(1)
	if err != nil {
		t.Fatal(err)
	}
	if low != 1 || high != 0 {
		t.Errorf("max(-1, 1) = %#x %#x, want 1", high, low)
	}
	low, err = javaMathLong(true)(client, nil, thread, arguments)
	if err != nil {
		t.Fatal(err)
	}
	high, err = thread.Register(1)
	if err != nil {
		t.Fatal(err)
	}
	if low != 0xffffffff || high != 0xffffffff {
		t.Errorf("min(-1, 1) = %#x %#x, want -1", high, low)
	}
}

// `callSerially` queues rather than running from inside the call, and the queue
// is bounded: a title that has asked for thousands is looping into it.
func TestJavaCallSeriallyQueues(t *testing.T) {
	client := fixtureClient(t)
	if _, err := javaCallSerially(client, nil, nil, []uint32{0, 0x30001000}); err != nil {
		t.Fatalf("callSerially() error = %v", err)
	}
	if queued := client.javaRuntimeState().serial; len(queued) != 1 || queued[0] != 0x30001000 {
		t.Errorf("the queue holds %v, want the one runnable", queued)
	}
	if _, err := javaCallSerially(client, nil, nil, []uint32{0, 0}); err == nil {
		t.Error("callSerially with no runnable was accepted")
	}
	for len(client.javaRuntimeState().serial) < maxJavaSerialCalls {
		if _, err := javaCallSerially(client, nil, nil, []uint32{0, 0x30001000}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := javaCallSerially(client, nil, nil, []uint32{0, 0x30001000}); err == nil {
		t.Error("the queue took more than its bound")
	}
}

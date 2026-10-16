package jvm

import (
	_ "embed"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/korean"
)

//go:embed testdata/LibraryContracts.class
var libraryContractsClass []byte

//go:embed testdata/LibraryContracts$1.class
var libraryContracts1Class []byte

//go:embed testdata/LibraryContracts$2.class
var libraryContracts2Class []byte

//go:embed testdata/LibraryContracts$3.class
var libraryContracts3Class []byte

//go:embed testdata/LibraryContracts$Key.class
var libraryContractsKeyClass []byte

func libraryContractsSource() mapClassSource {
	return mapClassSource{
		"LibraryContracts":     libraryContractsClass,
		"LibraryContracts$1":   libraryContracts1Class,
		"LibraryContracts$2":   libraryContracts2Class,
		"LibraryContracts$3":   libraryContracts3Class,
		"LibraryContracts$Key": libraryContractsKeyClass,
	}
}

func contractInt(t *testing.T, vm *VM, method string) int32 {
	t.Helper()
	result, err := vm.InvokeStatic("LibraryContracts", method, "()I")
	if err != nil {
		t.Fatalf("%s() error = %v", method, err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func contractString(t *testing.T, vm *VM, method string) string {
	t.Helper()
	result, err := vm.InvokeStatic("LibraryContracts", method, "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("%s() error = %v", method, err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	text, _ := StringText(object)
	return text
}

// waitForContract polls a static int until it leaves its starting value.
func waitForContract(t *testing.T, vm *VM, method string, unset int32) int32 {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if value := contractInt(t, vm, method); value != unset {
			return value
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s() still %d after the deadline", method, unset)
		}
		time.Sleep(time.Millisecond)
	}
}

// A record is read until the stream runs out, and the end is told apart from
// a broken stream by the exception's class: DataInput raises EOFException,
// and a handler written for it must see one.
func TestDataInputRaisesEOFExceptionAtTheEnd(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	if got := contractInt(t, vm, "readIntPastEnd"); got != 1 {
		t.Fatalf("readInt past the end took branch %d, want 1 (EOFException)", got)
	}
	if got := contractInt(t, vm, "readRecordsUntilEnd"); got != 7 {
		t.Fatalf("readShort until the end = %d, want 7", got)
	}
	if got := contractInt(t, vm, "readUTFPastEnd"); got != 1 {
		t.Fatalf("readUTF past the end took branch %d, want 1 (EOFException)", got)
	}
}

// One class is one Class object, whichever way it was reached: getClass,
// a class literal, or Class.forName. A key class whose equals compares classes
// is otherwise never equal to anything.
func TestClassObjectsAreCanonical(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	for _, method := range []string{"classIdentity", "equalByClass"} {
		result, err := vm.InvokeStatic("LibraryContracts", method, "()Z")
		if err != nil {
			t.Fatalf("%s() error = %v", method, err)
		}
		if got, _ := result.Int32(); got != 1 {
			t.Fatalf("%s() = %d, want true", method, got)
		}
	}
}

// wait(0, nanos) with nanos above zero is a timed wait of a millisecond, not
// an untimed one.
func TestWaitWithNanosecondsIsTimed(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	defer vm.Close()
	if _, err := invokeWithin(t, 5*time.Second, func() (Value, error) {
		return vm.InvokeStatic("LibraryContracts", "waitWithNanos", "()V")
	}); err != nil {
		t.Fatalf("waitWithNanos() error = %v", err)
	}
	lock := &Object{ClassName: ObjectClass}
	state := vm.newExecution()
	if err := vm.enterMonitor(state, &lock.monitor); err != nil {
		t.Fatal(err)
	}
	for _, nanos := range []int32{-1, 1_000_000} {
		_, err := vm.invokeInstance(state, ObjectClass, lock, "wait", "(JI)V", []Value{LongValue(0), IntValue(nanos)})
		if !vm.IsGuestException(err, "java/lang/IllegalArgumentException") {
			t.Fatalf("wait(0, %d) error = %v, want IllegalArgumentException", nanos, err)
		}
	}
}

// A sleep or wait far longer than a session lasts still waits: it does not
// wrap around to a negative duration that returns at once.
func TestLongSleepDoesNotOverflow(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	defer vm.Close()
	result, err := vm.InvokeStatic("LibraryContracts", "sleepForever", "()Ljava/lang/Thread;")
	if err != nil {
		t.Fatalf("sleepForever() error = %v", err)
	}
	thread, err := result.Reference()
	if err != nil || thread == nil {
		t.Fatalf("sleepForever() = %v/%v", thread, err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := contractInt(t, vm, "sleeper"); got != 0 {
		t.Fatalf("sleep(Long.MAX_VALUE) ended on its own with %d", got)
	}
	if _, err := vm.InvokeVirtual(thread, "interrupt", "()V"); err != nil {
		t.Fatal(err)
	}
	if got := waitForContract(t, vm, "sleeper", 0); got != 2 {
		t.Fatalf("interrupted sleep ended with %d, want 2", got)
	}

	// The same on the Host's own pass, which is not a guest thread: it waits
	// until the machine closes rather than returning at once.
	done := make(chan error, 1)
	go func() {
		_, err := vm.InvokeStatic(ThreadClass, "sleep", "(J)V", LongValue(math.MaxInt64))
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("Host sleep(Long.MAX_VALUE) returned at once: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	vm.Close()
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("Host sleep after Close error = %v, want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Host sleep was not released by Close")
	}

	slow := New(nil, Options{Speed: func() float64 { return 0.5 }})
	if got := slow.guestDelay(time.Duration(math.MaxInt64)); got <= 0 {
		t.Fatalf("guestDelay of the longest wait at half speed = %v, want positive", got)
	}
}

// A buffer length past what any array may hold is refused before anything is
// allocated for it.
func TestStringBufferSetLengthIsBounded(t *testing.T) {
	small := New(nil, Options{MaxArrayLength: 1024})
	buffer, err := small.NewObject(StringBufferClass, "()V")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := small.InvokeVirtual(buffer, "setLength", "(I)V", IntValue(4096)); err == nil {
		t.Fatal("setLength past MaxArrayLength was accepted")
	}
	if _, err := small.InvokeVirtual(buffer, "setLength", "(I)V", IntValue(1024)); err != nil {
		t.Fatalf("setLength at MaxArrayLength error = %v", err)
	}

	vm := New(libraryContractsSource(), Options{})
	_, err = vm.InvokeStatic("LibraryContracts", "setHugeLength", "()I")
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("setLength(Integer.MAX_VALUE) error = %v, want the array limit", err)
	}
}

// A null where a String method wants a String is a NullPointerException the
// title can catch, and equalsIgnoreCase(null) is simply false.
func TestNullStringArgumentsRaiseNullPointerException(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	if got := contractInt(t, vm, "nullArguments"); got != 127 {
		t.Fatalf("nullArguments() = %#b, want %#b", got, 127)
	}
}

// A StringBuffer shared between two threads is read and written under its own
// lock by every method. Run with -race: the read side used to skip the lock.
func TestStringBufferReadsTakeTheBufferLock(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	defer vm.Close()
	if got := contractInt(t, vm, "sharedBuffer"); got != 2000 {
		t.Fatalf("sharedBuffer() = %d, want 2000", got)
	}
}

func readerUnits(t *testing.T, vm *VM, data []byte) []uint16 {
	t.Helper()
	result, err := vm.InvokeStatic("LibraryContracts", "readerUnits", "([B)[C", ReferenceValue(NewByteArray(data)))
	if err != nil {
		t.Fatalf("readerUnits(% x) error = %v", data, err)
	}
	array, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	_, values, err := ArraySnapshot(array)
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(values))
	for index, value := range values {
		unit, _ := value.Int32()
		units[index] = uint16(unit)
	}
	return units
}

// A byte the charset cannot read is one replacement character, and the bytes
// after it are read as what they are. A character outside the basic plane is
// the two units of its surrogate pair.
func TestInputStreamReaderKeepsTheBytesAfterABadOne(t *testing.T) {
	utf8 := New(libraryContractsSource(), Options{})
	for _, probe := range []struct {
		data []byte
		want []uint16
	}{
		{[]byte{0xff, 'A', 'B', 'C'}, []uint16{0xfffd, 'A', 'B', 'C'}},
		{[]byte{0xe0, 'A'}, []uint16{0xfffd, 'A'}},
		{[]byte("가😀!"), []uint16{0xac00, 0xd83d, 0xde00, '!'}},
		{[]byte{'A', 0xe0}, []uint16{'A', 0xfffd}},
	} {
		if got := readerUnits(t, utf8, probe.data); !slices.Equal(got, probe.want) {
			t.Errorf("UTF-8 reader over % x = %x, want %x", probe.data, got, probe.want)
		}
	}

	euckr := New(libraryContractsSource(), Options{ByteDecoder: func(data []byte) string {
		decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
		if err != nil {
			return strings.ToValidUTF8(string(data), "�")
		}
		return string(decoded)
	}})
	data := []byte{0xb0, 0xa1, 0xff, 'A', 'B', 0xb0, 0xa1}
	want := []uint16{0xac00, 0xfffd, 'A', 'B', 0xac00}
	if got := readerUnits(t, euckr, data); !slices.Equal(got, want) {
		t.Errorf("EUC-KR reader over % x = %x, want %x", data, got, want)
	}
}

// An empty message is a message: getMessage answers "" and toString keeps
// the separator, where a missing one answers null.
func TestThrowableKeepsAnEmptyMessage(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	if got := contractInt(t, vm, "emptyMessageLength"); got != 0 {
		t.Fatalf("new Exception(\"\").getMessage().length() = %d, want 0", got)
	}
	if got := contractString(t, vm, "emptyMessageText"); got != "java.lang.Exception: " {
		t.Fatalf("new Exception(\"\").toString() = %q", got)
	}
	empty, err := vm.NewObject("java/lang/Exception", "()V")
	if err != nil {
		t.Fatal(err)
	}
	message, err := vm.InvokeVirtual(empty, "getMessage", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	if object, _ := message.Reference(); object != nil {
		t.Fatalf("new Exception().getMessage() = %v, want null", object)
	}
}

// An interrupt is consumed by the sleep that throws for it, so the next sleep
// sleeps.
func TestInterruptedSleepConsumesTheInterrupt(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	defer vm.Close()
	if _, err := vm.InvokeStatic("LibraryContracts", "interruptSelfThenSleepTwice", "()V"); err != nil {
		t.Fatal(err)
	}
	if got := waitForContract(t, vm, "interrupts", -1); got != 1 {
		t.Fatalf("InterruptedExceptions from two sleeps after one interrupt = %d, want 1", got)
	}
}

// The parses refuse what CLDC's refuse: a leading plus sign and surrounding
// spaces are not digits.
func TestNumberParsesAreStrict(t *testing.T) {
	vm := New(libraryContractsSource(), Options{})
	if got := contractInt(t, vm, "parseStrictly"); got != 63 {
		t.Fatalf("parseStrictly() = %#b, want %#b", got, 63)
	}
	for _, text := range []string{"+5", " 5", "5 "} {
		_, err := vm.InvokeStatic(IntegerClass, "parseInt", "(Ljava/lang/String;I)I", ReferenceValue(vm.NewString(text)), IntValue(10))
		if !vm.IsGuestException(err, "java/lang/NumberFormatException") {
			t.Errorf("Integer.parseInt(%q, 10) error = %v, want NumberFormatException", text, err)
		}
		_, err = vm.InvokeStatic(LongClass, "parseLong", "(Ljava/lang/String;I)J", ReferenceValue(vm.NewString(text)), IntValue(16))
		if !vm.IsGuestException(err, "java/lang/NumberFormatException") {
			t.Errorf("Long.parseLong(%q, 16) error = %v, want NumberFormatException", text, err)
		}
	}
}

// One multianewarray is held to what one array of the maximum length may
// cost, counting the arrays it creates as well as their elements, and a
// negative dimension is refused even behind a zero one.
func TestMultiArrayAllocationIsBounded(t *testing.T) {
	grid := func(vm *VM, rows, columns int32) (*Object, error) {
		result, err := vm.InvokeStatic("LibraryContracts", "grid", "(II)[[I", IntValue(rows), IntValue(columns))
		if err != nil {
			return nil, err
		}
		return result.Reference()
	}
	small := New(libraryContractsSource(), Options{MaxArrayLength: 1000})
	if _, err := grid(small, 100, 100); err == nil {
		t.Fatal("100x100 grid under a 1000-element limit was allocated")
	}
	if _, err := grid(small, 1000, 0); err == nil {
		t.Fatal("1000 empty rows under a 1000-element limit were allocated")
	}
	vm := New(libraryContractsSource(), Options{})
	if _, err := grid(vm, 16384, 16384); err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("16384x16384 grid error = %v, want the array limit", err)
	}
	if _, err := grid(vm, 0, -1); !vm.IsGuestException(err, "java/lang/NegativeArraySizeException") {
		t.Fatalf("grid(0, -1) error = %v, want NegativeArraySizeException", err)
	}
	array, err := grid(vm, 300, 200)
	if err != nil {
		t.Fatalf("300x200 grid error = %v", err)
	}
	_, rows, err := ArraySnapshot(array)
	if err != nil || len(rows) != 300 {
		t.Fatalf("grid rows = %d/%v, want 300", len(rows), err)
	}
	row, _ := rows[299].Reference()
	if _, columns, err := ArraySnapshot(row); err != nil || len(columns) != 200 {
		t.Fatalf("grid columns = %d/%v, want 200", len(columns), err)
	}
}

package jvm

import "testing"

// java/util/Date is the instant CLDC keeps behind Calendar, and a title that
// wants a number rather than a set of fields calls it directly. The class
// resolved before this existed — an unknown class still answers as a record —
// so the gap surfaced as a failed method lookup deep inside a running game
// rather than at load.
//
// These call the bodies the way a platform's class record does, by the name
// and descriptor a guest looks them up under: there is no class file behind a
// builtin class, so the loader is not the path here.
func TestDateReadsAndCarriesTheGuestClock(t *testing.T) {
	const guestNow int64 = 1_700_000_000_123
	vm := New(mapClassSource{}, Options{Clock: func() int64 { return guestNow }})

	now := &Object{ClassName: "java/util/Date"}
	call(t, vm, "java/util/Date", "<init>", "()V", ReferenceValue(now))
	milliseconds := callLong(t, vm, "java/util/Date", "getTime", "()J", ReferenceValue(now))
	if milliseconds != guestNow {
		t.Fatalf("new Date().getTime() = %d, want the guest clock %d", milliseconds, guestNow)
	}

	// The clock Date reads has to be the clock currentTimeMillis reads: a
	// title measuring an interval across the two would otherwise be able to
	// measure a negative one.
	stamped := &Object{ClassName: "java/util/Date"}
	call(t, vm, "java/util/Date", "<init>", "(J)V", ReferenceValue(stamped), LongValue(guestNow-5_000))
	if elapsed := milliseconds - callLong(t, vm, "java/util/Date", "getTime", "()J", ReferenceValue(stamped)); elapsed != 5_000 {
		t.Fatalf("interval between the two dates = %d, want 5000", elapsed)
	}

	call(t, vm, "java/util/Date", "setTime", "(J)V", ReferenceValue(stamped), LongValue(guestNow))
	if again := callLong(t, vm, "java/util/Date", "getTime", "()J", ReferenceValue(stamped)); again != guestNow {
		t.Fatalf("getTime() after setTime() = %d, want %d", again, guestNow)
	}
	equal := call(t, vm, "java/util/Date", "equals", "(Ljava/lang/Object;)Z", ReferenceValue(stamped), ReferenceValue(now))
	if value, _ := equal.Int32(); value != 1 {
		t.Fatalf("two dates at the same instant compared unequal")
	}
}

// Calendar's own Date methods are the way between the fields and the instant.
func TestCalendarHandsOutAndTakesBackADate(t *testing.T) {
	const guestNow int64 = 1_700_000_000_000
	vm := New(mapClassSource{}, Options{Clock: func() int64 { return guestNow }})

	instance, err := call(t, vm, "java/util/Calendar", "getInstance", "()Ljava/util/Calendar;").Reference()
	if err != nil {
		t.Fatal(err)
	}
	date, err := call(t, vm, "java/util/Calendar", "getTime", "()Ljava/util/Date;", ReferenceValue(instance)).Reference()
	if err != nil {
		t.Fatal(err)
	}
	if milliseconds := callLong(t, vm, "java/util/Date", "getTime", "()J", ReferenceValue(date)); milliseconds != guestNow {
		t.Fatalf("Calendar.getTime().getTime() = %d, want %d", milliseconds, guestNow)
	}

	// A calendar set from a date reports that date back.
	moved := &Object{ClassName: "java/util/Date"}
	call(t, vm, "java/util/Date", "<init>", "(J)V", ReferenceValue(moved), LongValue(guestNow+86_400_000))
	call(t, vm, "java/util/Calendar", "setTime", "(Ljava/util/Date;)V", ReferenceValue(instance), ReferenceValue(moved))
	returned, err := call(t, vm, "java/util/Calendar", "getTime", "()Ljava/util/Date;", ReferenceValue(instance)).Reference()
	if err != nil {
		t.Fatal(err)
	}
	if milliseconds := callLong(t, vm, "java/util/Date", "getTime", "()J", ReferenceValue(returned)); milliseconds != guestNow+86_400_000 {
		t.Fatalf("the calendar did not keep the date it was set to: %d", milliseconds)
	}
}

func call(t *testing.T, vm *VM, class, name, descriptor string, arguments ...Value) Value {
	t.Helper()
	if !vm.HasMethodBody(class, name, descriptor) {
		t.Fatalf("%s.%s%s has no body", class, name, descriptor)
	}
	entry, ok := vm.natives[methodKey{class: class, name: name, descriptor: descriptor}]
	if !ok || entry.plain == nil {
		t.Fatalf("%s.%s%s is not a builtin", class, name, descriptor)
	}
	result, err := entry.plain(vm, arguments)
	if err != nil {
		t.Fatalf("%s.%s%s error = %v", class, name, descriptor, err)
	}
	return result
}

func callLong(t *testing.T, vm *VM, class, name, descriptor string, arguments ...Value) int64 {
	t.Helper()
	value, err := call(t, vm, class, name, descriptor, arguments...).Int64()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// A title that builds a `GregorianCalendar` itself, rather than asking the
// factory for one, gets the same instant and the same fields: the calendar
// methods key on what the object holds, not on its class name.
func TestAGregorianCalendarIsACalendar(t *testing.T) {
	const guestNow int64 = 1_700_000_000_000
	vm := New(mapClassSource{}, Options{Clock: func() int64 { return guestNow }})

	instance := &Object{ClassName: "java/util/GregorianCalendar"}
	call(t, vm, "java/util/GregorianCalendar", "<init>", "()V", ReferenceValue(instance))

	date, err := call(t, vm, "java/util/Calendar", "getTime", "()Ljava/util/Date;",
		ReferenceValue(instance)).Reference()
	if err != nil {
		t.Fatal(err)
	}
	if milliseconds := callLong(t, vm, "java/util/Date", "getTime", "()J", ReferenceValue(date)); milliseconds != guestNow {
		t.Fatalf("new GregorianCalendar().getTime().getTime() = %d, want %d", milliseconds, guestNow)
	}
	year := call(t, vm, "java/util/Calendar", "get", "(I)I", ReferenceValue(instance), IntValue(1))
	value, err := year.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != 2023 {
		t.Errorf("get(YEAR) = %d, want the year the guest clock stands at", value)
	}
}

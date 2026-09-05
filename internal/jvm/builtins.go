package jvm

import (
	"fmt"
	"math"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

func (vm *VM) registerBuiltins() {
	vm.builtin("java/lang/Object", "<init>", "()V", func(_ *VM, _ []Value) (Value, error) {
		return VoidValue(), nil
	})
	vm.builtin("java/lang/Object", "hashCode", "()I", func(vm *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(int32(vm.objectIdentity(object))), nil
	})
	// The root toString. A title reaches it through String.valueOf or a
	// StringBuffer append on an object whose class does not override it, and
	// without a body of its own that resolution ended the session on the class
	// every other class inherits from.
	vm.builtin("java/lang/Object", "toString", "()Ljava/lang/String;", func(vm *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		if object == nil {
			return ReferenceValue(nativeStringValue("null")), nil
		}
		return ReferenceValue(nativeStringValue(fmt.Sprintf("%s@%x", object.ClassName, vm.objectIdentity(object)))), nil
	})
	vm.builtin("java/lang/Object", "equals", "(Ljava/lang/Object;)Z", func(_ *VM, arguments []Value) (Value, error) {
		left, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		right, err := nativeReference(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		if left == right {
			return IntValue(1), nil
		}
		return IntValue(0), nil
	})
	vm.builtin("java/lang/Object", "getClass", "()Ljava/lang/Class;", func(vm *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		classObject := &Object{ClassName: "java/lang/Class", Native: object.ClassName}
		vm.objectIdentity(classObject)
		return ReferenceValue(classObject), nil
	})
	vm.builtin(ClassClass, "getName", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		name, err := classObjectName(arguments)
		if err != nil {
			return VoidValue(), err
		}
		// Class.getName answers in source form; internal names use slashes.
		return ReferenceValue(nativeStringValue(strings.ReplaceAll(name, "/", "."))), nil
	})
	vm.builtin(ClassClass, "toString", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		name, err := classObjectName(arguments)
		if err != nil {
			return VoidValue(), err
		}
		return ReferenceValue(nativeStringValue("class " + strings.ReplaceAll(name, "/", "."))), nil
	})

	vm.builtin("java/lang/System", "currentTimeMillis", "()J", func(vm *VM, _ []Value) (Value, error) {
		if vm.config.Clock != nil {
			return LongValue(vm.config.Clock()), nil
		}
		return LongValue(time.Now().UnixMilli()), nil
	})
	vm.builtin("java/lang/System", "identityHashCode", "(Ljava/lang/Object;)I", func(vm *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(int32(vm.objectIdentity(object))), nil
	})
	vm.builtin("java/lang/System", "arraycopy", "(Ljava/lang/Object;ILjava/lang/Object;II)V", systemArraycopy)
	vm.builtin(PrintStreamClass, "emit", "(ILjava/lang/String;)V", printStreamEmit)

	vm.registerMathBuiltins()
	vm.registerStringBuiltins()
	vm.registerStringBuilderBuiltins(StringBufferClass)
	vm.registerStringBuilderBuiltins(StringBuilderClass)
	vm.registerMonitorBuiltins()
	vm.registerThreadBuiltins()
	vm.registerDataInputBuiltins()
	vm.registerTimeZoneBuiltins()
	vm.registerUtilityBuiltins()
	vm.registerLanguageBuiltins()
}

func (vm *VM) registerMonitorBuiltins() {
	for _, descriptor := range []string{"()V", "(J)V", "(JI)V"} {
		descriptor := descriptor
		vm.contextBuiltin("java/lang/Object", "wait", descriptor, func(_ *VM, state *execution, arguments []Value) (Value, error) {
			object, err := nativeReference(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			var timeout time.Duration
			if descriptor != "()V" {
				milliseconds, err := arguments[1].Int64()
				if err != nil {
					return VoidValue(), err
				}
				if milliseconds < 0 {
					return VoidValue(), guestException("java/lang/IllegalArgumentException", "negative wait timeout")
				}
				timeout = time.Duration(milliseconds) * time.Millisecond
			}
			if err := object.monitor.wait(state.id, vm.guestDelay(timeout)); err != nil {
				return VoidValue(), guestException("java/lang/IllegalMonitorStateException", err.Error())
			}
			return VoidValue(), nil
		})
	}
	vm.contextBuiltin("java/lang/Object", "notify", "()V", func(_ *VM, state *execution, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		if err := object.monitor.notify(state.id, false); err != nil {
			return VoidValue(), guestException("java/lang/IllegalMonitorStateException", err.Error())
		}
		return VoidValue(), nil
	})
	vm.contextBuiltin("java/lang/Object", "notifyAll", "()V", func(_ *VM, state *execution, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		if err := object.monitor.notify(state.id, true); err != nil {
			return VoidValue(), guestException("java/lang/IllegalMonitorStateException", err.Error())
		}
		return VoidValue(), nil
	})
}

type guestThread struct {
	mu          sync.Mutex
	started     bool
	alive       bool
	interrupted bool
	wake        chan struct{}
}

func (vm *VM) registerThreadBuiltins() {
	vm.contextBuiltin(ThreadClass, "start", "()V", func(vm *VM, _ *execution, arguments []Value) (Value, error) {
		thread, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		state := vm.threadState(thread)
		state.mu.Lock()
		if state.started {
			state.mu.Unlock()
			return VoidValue(), guestException("java/lang/IllegalThreadStateException", "thread already started")
		}
		state.started = true
		state.alive = true
		state.mu.Unlock()
		if vm.config.GuestThreadStarter != nil {
			return VoidValue(), vm.config.GuestThreadStarter(thread)
		}
		go vm.runGuestThread(thread, state)
		return VoidValue(), nil
	})
	vm.contextBuiltin(ThreadClass, "interrupt", "()V", func(vm *VM, _ *execution, arguments []Value) (Value, error) {
		thread, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		state := vm.threadState(thread)
		state.mu.Lock()
		state.interrupted = true
		select {
		case state.wake <- struct{}{}:
		default:
		}
		state.mu.Unlock()
		return VoidValue(), nil
	})
	vm.contextBuiltin(ThreadClass, "isAlive", "()Z", func(vm *VM, _ *execution, arguments []Value) (Value, error) {
		thread, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		state := vm.threadState(thread)
		state.mu.Lock()
		alive := state.alive
		state.mu.Unlock()
		if alive {
			return IntValue(1), nil
		}
		return IntValue(0), nil
	})
	vm.contextBuiltin(ThreadClass, "sleep", "(J)V", func(vm *VM, execution *execution, arguments []Value) (Value, error) {
		milliseconds, err := arguments[0].Int64()
		if err != nil {
			return VoidValue(), err
		}
		if milliseconds < 0 {
			return VoidValue(), guestException("java/lang/IllegalArgumentException", "negative sleep timeout")
		}
		requested := vm.guestDelay(time.Duration(milliseconds) * time.Millisecond)
		if execution.thread == nil {
			time.Sleep(requested)
		} else if err := vm.sleepGuestThread(execution.thread, requested); err != nil {
			return VoidValue(), err
		}
		return VoidValue(), vm.threadYield()
	})
	// activeCount is a census a title takes of itself: it counts its own
	// workers before starting another. What it counts here is every thread
	// object this machine holds that is still running, plus the main one,
	// because the specification counts the current thread group and this
	// runtime has exactly one.
	vm.contextBuiltin(ThreadClass, "activeCount", "()I", func(vm *VM, _ *execution, _ []Value) (Value, error) {
		count := int32(0)
		for _, thread := range vm.ThreadObjects() {
			state := vm.threadState(thread)
			state.mu.Lock()
			alive := state.alive
			state.mu.Unlock()
			if alive || thread == vm.mainThread {
				count++
			}
		}
		if count == 0 {
			count = 1
		}
		return IntValue(count), nil
	})
	// yield is a hint, and on this runtime it is one the scheduler underneath
	// does not need. It used to be `runtime.Gosched()`, which is a full
	// scheduler round trip and, on one host, a futex wake per call.
	//
	// **The titles that call it do not call it once.** One local SKT archive
	// has thirty-eight `Thread.yield()` call sites, which is the idiom of the
	// handsets these were written for: their scheduler was cooperative, so a
	// thread that did not yield did not let anything else run, and a game
	// sprinkled the call through everything it did. Here a guest thread is a
	// goroutine and Go preempts it whether it asks or not, so every one of
	// those calls bought nothing and cost a wake — the profile of one such
	// title spent 63% of its CPU in `runtime.wakep` under this line.
	//
	// The platform hook stays: a runtime that really does need to hand a token
	// over installs `Options.ThreadYield` and gets it. Nothing here does.
	vm.contextBuiltin(ThreadClass, "yield", "()V", func(vm *VM, _ *execution, _ []Value) (Value, error) {
		return VoidValue(), vm.threadYield()
	})
}

func (vm *VM) registerDataInputBuiltins() {
	vm.builtin(DataInputStreamClass, "readUTF", "()Ljava/lang/String;", func(vm *VM, arguments []Value) (Value, error) {
		stream, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		lengthValue, err := vm.InvokeVirtual(stream, "readUnsignedShort", "()I")
		if err != nil {
			return VoidValue(), err
		}
		length, err := lengthValue.Int32()
		if err != nil || length < 0 {
			return VoidValue(), guestException(IOExceptionClass, "invalid UTF length")
		}
		bytes := NewByteArray(make([]byte, int(length)))
		for index := int32(0); index < length; index++ {
			value, readErr := vm.InvokeVirtual(stream, "read", "()I")
			if readErr != nil {
				return VoidValue(), readErr
			}
			integer, valueErr := value.Int32()
			if valueErr != nil || integer < 0 {
				return VoidValue(), guestException(IOExceptionClass, "unexpected end of modified UTF-8 string")
			}
			if setErr := SetArrayRange(bytes, int(index), []Value{IntValue(integer)}); setErr != nil {
				return VoidValue(), setErr
			}
		}
		data, err := ByteArraySnapshot(bytes)
		if err != nil {
			return VoidValue(), err
		}
		text, err := decodeModifiedUTF8(data)
		if err != nil {
			return VoidValue(), guestException(IOExceptionClass, err.Error())
		}
		return ReferenceValue(&Object{ClassName: "java/lang/String", Native: text}), nil
	})
}

// encodeModifiedUTF8 is the inverse of the decoder below: one to three bytes
// per UTF-16 unit, with the null character encoded as two bytes so that no zero
// byte appears inside the text.
func encodeModifiedUTF8(text string) []byte {
	encoded := make([]byte, 0, len(text))
	for _, unit := range utf16.Encode([]rune(text)) {
		switch {
		case unit >= 0x0001 && unit <= 0x007f:
			encoded = append(encoded, byte(unit))
		case unit <= 0x07ff:
			encoded = append(encoded, byte(0xc0|unit>>6), byte(0x80|unit&0x3f))
		default:
			encoded = append(encoded, byte(0xe0|unit>>12), byte(0x80|unit>>6&0x3f), byte(0x80|unit&0x3f))
		}
	}
	return encoded
}

func decodeModifiedUTF8(data []byte) (string, error) {
	units := make([]uint16, 0, len(data))
	for index := 0; index < len(data); {
		first := data[index]
		switch {
		case first&0x80 == 0:
			if first == 0 {
				return "", fmt.Errorf("modified UTF-8 contains a zero byte")
			}
			units = append(units, uint16(first))
			index++
		case first&0xe0 == 0xc0:
			if index+1 >= len(data) || data[index+1]&0xc0 != 0x80 {
				return "", fmt.Errorf("invalid modified UTF-8 sequence")
			}
			unit := uint16(first&0x1f)<<6 | uint16(data[index+1]&0x3f)
			if unit < 0x80 && unit != 0 {
				return "", fmt.Errorf("overlong modified UTF-8 sequence")
			}
			units = append(units, unit)
			index += 2
		case first&0xf0 == 0xe0:
			if index+2 >= len(data) || data[index+1]&0xc0 != 0x80 || data[index+2]&0xc0 != 0x80 {
				return "", fmt.Errorf("invalid modified UTF-8 sequence")
			}
			unit := uint16(first&0x0f)<<12 | uint16(data[index+1]&0x3f)<<6 | uint16(data[index+2]&0x3f)
			if unit < 0x800 {
				return "", fmt.Errorf("overlong modified UTF-8 sequence")
			}
			units = append(units, unit)
			index += 3
		default:
			return "", fmt.Errorf("invalid modified UTF-8 leading byte")
		}
	}
	return string(utf16.Decode(units)), nil
}

type calendarData struct {
	time time.Time
}

// guestDelay is what a wait the guest asked for costs on the wall. A game runs
// faster by having its waits cost less and its clock move more, which are the
// two halves of one setting: see Options.Speed.
func (vm *VM) guestDelay(requested time.Duration) time.Duration {
	if vm == nil || vm.config.Speed == nil || requested <= 0 {
		return requested
	}
	speed := vm.config.Speed()
	if speed <= 0 {
		return requested
	}
	return time.Duration(float64(requested) / speed)
}

// nowMilliseconds is the clock every date-shaped builtin reads: the guest's
// own when a Host injected one, the wall clock otherwise. Two builtins reading
// two clocks would let a title measure a negative interval between them.
func (vm *VM) nowMilliseconds() int64 {
	if vm != nil && vm.config.Clock != nil {
		return vm.config.Clock()
	}
	return time.Now().UnixMilli()
}

// dateMilliseconds reads the instant a Date receiver carries.
func dateMilliseconds(arguments []Value) (int64, error) {
	object, err := nativeReference(arguments, 0)
	if err != nil {
		return 0, err
	}
	milliseconds, ok := object.Native.(int64)
	if !ok {
		return 0, fmt.Errorf("receiver is not a Date")
	}
	return milliseconds, nil
}

func (vm *VM) registerUtilityBuiltins() {
	// An Integer is its int: the object carries the value natively, and the
	// constructor is what a title uses when it boxes one into a Vector.
	vm.builtin("java/lang/Integer", "<init>", "(I)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		value, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		object.Native = value
		return VoidValue(), nil
	})
	vm.builtin("java/lang/Integer", "parseInt", "(Ljava/lang/String;)I", parseInteger(10))
	vm.builtin("java/lang/Integer", "parseInt", "(Ljava/lang/String;I)I", func(_ *VM, arguments []Value) (Value, error) {
		text, err := parsedText(arguments)
		if err != nil {
			return VoidValue(), err
		}
		radix, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		value, parseErr := strconv.ParseInt(text, int(radix), 32)
		if parseErr != nil {
			return VoidValue(), guestException("java/lang/NumberFormatException", parseErr.Error())
		}
		return IntValue(int32(value)), nil
	})
	vm.builtin("java/lang/Integer", "toString", "(I)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeInt(arguments, 0)
		return ReferenceValue(nativeStringValue(strconv.FormatInt(int64(value), 10))), err
	})
	vm.builtin("java/lang/Integer", "valueOf", "(Ljava/lang/String;)Ljava/lang/Integer;", func(_ *VM, arguments []Value) (Value, error) {
		text, err := parsedText(arguments)
		if err != nil {
			return VoidValue(), err
		}
		value, parseErr := strconv.ParseInt(text, 10, 32)
		if parseErr != nil {
			return VoidValue(), guestException("java/lang/NumberFormatException", parseErr.Error())
		}
		return ReferenceValue(&Object{ClassName: "java/lang/Integer", Native: int32(value)}), nil
	})
	// The narrowing accessors are the same value read at another width, which
	// is what a title asks for when it keeps small numbers boxed.
	for _, accessor := range []struct {
		name       string
		descriptor string
		narrow     func(int32) Value
	}{
		{"intValue", "()I", func(value int32) Value { return IntValue(value) }},
		{"byteValue", "()B", func(value int32) Value { return IntValue(int32(int8(value))) }},
		{"shortValue", "()S", func(value int32) Value { return IntValue(int32(int16(value))) }},
		{"longValue", "()J", func(value int32) Value { return LongValue(int64(value)) }},
	} {
		narrow := accessor.narrow
		vm.builtin("java/lang/Integer", accessor.name, accessor.descriptor, func(_ *VM, arguments []Value) (Value, error) {
			object, err := nativeReference(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			value, ok := object.Native.(int32)
			if !ok {
				return VoidValue(), fmt.Errorf("receiver is not an Integer")
			}
			return narrow(value), nil
		})
	}
	vm.builtin("java/lang/Integer", "toString", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		value, ok := object.Native.(int32)
		if !ok {
			return VoidValue(), fmt.Errorf("receiver is not an Integer")
		}
		return ReferenceValue(nativeStringValue(strconv.FormatInt(int64(value), 10))), nil
	})
	vm.builtin("java/util/Calendar", "getInstance", "()Ljava/util/Calendar;", func(vm *VM, _ []Value) (Value, error) {
		return ReferenceValue(&Object{ClassName: "java/util/Calendar", Native: &calendarData{time: time.UnixMilli(vm.nowMilliseconds())}}), nil
	})
	// The zone form is how a title that stores a stamp reads it back somewhere
	// other than where the handset stands — a daily reset at GMT midnight, a
	// scheduled event. The instant is the same one the no-argument form
	// answers; what the zone decides is which fields get(I) breaks it into,
	// and Calendar.set already carries a calendar's location forward, so
	// choosing it here is the whole of it.
	vm.builtin("java/util/Calendar", "getInstance", "(Ljava/util/TimeZone;)Ljava/util/Calendar;", func(vm *VM, arguments []Value) (Value, error) {
		zone, err := arguments[0].Reference()
		if err != nil {
			return VoidValue(), err
		}
		if zone == nil {
			return VoidValue(), guestException("java/lang/NullPointerException", "Calendar.getInstance zone")
		}
		data, ok := zone.Native.(*timeZoneData)
		if !ok {
			return VoidValue(), fmt.Errorf("argument is not a TimeZone")
		}
		instant := time.UnixMilli(vm.nowMilliseconds())
		instant = instant.In(timeZoneLocation(data, instant))
		return ReferenceValue(&Object{ClassName: "java/util/Calendar", Native: &calendarData{time: instant}}), nil
	})
	// setTimeZone moves a calendar that already exists into a zone, which is
	// the other half of the pair above: a title takes the default calendar and
	// then names the zone it wants to read the fields in. The instant does not
	// move — only which fields get(I) breaks it into — so this is exactly what
	// the zone form of getInstance does, applied to a calendar in hand.
	vm.builtin("java/util/Calendar", "setTimeZone", "(Ljava/util/TimeZone;)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		calendar, ok := object.Native.(*calendarData)
		if !ok {
			return VoidValue(), fmt.Errorf("receiver is not a Calendar")
		}
		zone, err := nativeReference(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		if zone == nil {
			return VoidValue(), guestException("java/lang/NullPointerException", "Calendar.setTimeZone zone")
		}
		data, ok := zone.Native.(*timeZoneData)
		if !ok {
			return VoidValue(), fmt.Errorf("argument is not a TimeZone")
		}
		calendar.time = calendar.time.In(timeZoneLocation(data, calendar.time))
		return VoidValue(), nil
	})
	// `java/util/GregorianCalendar` is the concrete calendar a title reaches
	// for when it wants one without going through the factory. CLDC does not
	// declare it and the handset library does, and a title that constructs one
	// then calls `Calendar.get` on it is calling the methods above: they key on
	// what the object holds rather than on its class name, so one instant
	// serves both. The constructor is the only method of its own — everything
	// else it answers, it inherits.
	vm.builtin("java/util/GregorianCalendar", "<init>", "()V", func(vm *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		object.Native = &calendarData{time: time.UnixMilli(vm.nowMilliseconds())}
		return VoidValue(), nil
	})
	vm.builtin("java/util/Calendar", "get", "(I)I", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		calendar, ok := object.Native.(*calendarData)
		if !ok {
			return VoidValue(), fmt.Errorf("receiver is not a Calendar")
		}
		field, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		var value int
		switch field {
		case 1:
			value = calendar.time.Year()
		case 2:
			value = int(calendar.time.Month()) - 1
		case 5:
			value = calendar.time.Day()
		case 7:
			value = int(calendar.time.Weekday()) + 1
		case 9:
			value = calendar.time.Hour() / 12
		case 10:
			value = calendar.time.Hour() % 12
		case 11:
			value = calendar.time.Hour()
		case 12:
			value = calendar.time.Minute()
		case 13:
			value = calendar.time.Second()
		case 14:
			value = calendar.time.Nanosecond() / int(time.Millisecond)
		default:
			return VoidValue(), guestException("java/lang/IllegalArgumentException", fmt.Sprintf("unsupported Calendar field %d", field))
		}
		return IntValue(int32(value)), nil
	})
	// Calendar.set moves the instant a Calendar stands for. A title uses it to
	// build a date rather than to read one — a stamp it writes into a save, or
	// the day an event opens on — so the field it names has to move the same
	// component get(I) above reads back.
	vm.builtin("java/util/Calendar", "set", "(II)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		calendar, ok := object.Native.(*calendarData)
		if !ok {
			return VoidValue(), fmt.Errorf("receiver is not a Calendar")
		}
		field, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		value, err := nativeInt(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		current := calendar.time
		year, month, day := current.Year(), int(current.Month()), current.Day()
		hour, minute, second := current.Hour(), current.Minute(), current.Second()
		nanosecond := current.Nanosecond()
		switch field {
		case 1:
			year = int(value)
		case 2:
			month = int(value) + 1
		case 5:
			day = int(value)
		case 7:
			// A day of the week names a day inside the week the calendar is
			// already in, which is what moving by the difference does; the
			// normalization below carries it into the next or previous month.
			day += int(value) - 1 - int(current.Weekday())
		case 9:
			hour = hour%12 + int(value)*12
		case 10:
			hour = hour/12*12 + int(value)
		case 11:
			hour = int(value)
		case 12:
			minute = int(value)
		case 13:
			second = int(value)
		case 14:
			nanosecond = int(value) * int(time.Millisecond)
		default:
			return VoidValue(), guestException("java/lang/IllegalArgumentException", fmt.Sprintf("unsupported Calendar field %d", field))
		}
		// time.Date normalizes out-of-range components the way Calendar's own
		// lenient mode does, which is what a title relies on when it adds a
		// day to the end of a month.
		calendar.time = time.Date(year, time.Month(month), day, hour, minute, second, nanosecond, current.Location())
		return VoidValue(), nil
	})
	// A Date is the millisecond instant it holds, and nothing else: CLDC keeps
	// four methods on it and one time zone under it, so the object needs no
	// calendar of its own. It reads the same clock Calendar and
	// System.currentTimeMillis do, which is what lets a title compare a
	// timestamp it stored against one it takes now.
	vm.builtin("java/util/Date", "<init>", "()V", func(vm *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		object.Native = vm.nowMilliseconds()
		return VoidValue(), nil
	})
	vm.builtin("java/util/Date", "<init>", "(J)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		milliseconds, err := nativeLong(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		object.Native = milliseconds
		return VoidValue(), nil
	})
	vm.builtin("java/util/Date", "getTime", "()J", func(_ *VM, arguments []Value) (Value, error) {
		milliseconds, err := dateMilliseconds(arguments)
		if err != nil {
			return VoidValue(), err
		}
		return LongValue(milliseconds), nil
	})
	vm.builtin("java/util/Date", "setTime", "(J)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		milliseconds, err := nativeLong(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		object.Native = milliseconds
		return VoidValue(), nil
	})
	vm.builtin("java/util/Date", "equals", "(Ljava/lang/Object;)Z", func(_ *VM, arguments []Value) (Value, error) {
		milliseconds, err := dateMilliseconds(arguments)
		if err != nil {
			return VoidValue(), err
		}
		if len(arguments) < 2 {
			return IntValue(0), nil
		}
		other, err := arguments[1].Reference()
		if err != nil || other == nil {
			return IntValue(0), nil
		}
		instant, ok := other.Native.(int64)
		if !ok || instant != milliseconds {
			return IntValue(0), nil
		}
		return IntValue(1), nil
	})
	vm.builtin("java/util/Date", "hashCode", "()I", func(_ *VM, arguments []Value) (Value, error) {
		milliseconds, err := dateMilliseconds(arguments)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(int32(milliseconds) ^ int32(milliseconds>>32)), nil
	})
	// Calendar's own two Date methods are what a title uses to move between
	// the fields and the instant; without them a Date it can build is one it
	// cannot get out of the calendar it already asked for.
	vm.builtin("java/util/Calendar", "getTime", "()Ljava/util/Date;", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		calendar, ok := object.Native.(*calendarData)
		if !ok {
			return VoidValue(), fmt.Errorf("receiver is not a Calendar")
		}
		return ReferenceValue(&Object{ClassName: "java/util/Date", Native: calendar.time.UnixMilli()}), nil
	})
	vm.builtin("java/util/Calendar", "setTime", "(Ljava/util/Date;)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		calendar, ok := object.Native.(*calendarData)
		if !ok {
			return VoidValue(), fmt.Errorf("receiver is not a Calendar")
		}
		if len(arguments) < 2 {
			return VoidValue(), fmt.Errorf("Calendar.setTime expected a date")
		}
		date, err := arguments[1].Reference()
		if err != nil {
			return VoidValue(), err
		}
		if date == nil {
			return VoidValue(), guestException("java/lang/NullPointerException", "Calendar.setTime date")
		}
		milliseconds, ok := date.Native.(int64)
		if !ok {
			return VoidValue(), fmt.Errorf("argument is not a Date")
		}
		// The zone the calendar was made in outlives the instant in it: a
		// title that asked for GMT and then set a stored date still means
		// GMT when it reads the fields back.
		calendar.time = time.UnixMilli(milliseconds).In(calendar.time.Location())
		return VoidValue(), nil
	})
	vm.builtin("java/lang/Runtime", "getRuntime", "()Ljava/lang/Runtime;", func(_ *VM, _ []Value) (Value, error) {
		return ReferenceValue(&Object{ClassName: "java/lang/Runtime"}), nil
	})
	vm.builtin("java/lang/Runtime", "gc", "()V", func(_ *VM, _ []Value) (Value, error) {
		runtime.GC()
		return VoidValue(), nil
	})
	vm.builtin("java/lang/System", "gc", "()V", func(_ *VM, _ []Value) (Value, error) {
		runtime.GC()
		return VoidValue(), nil
	})
}

func parseInteger(radix int) NativeMethod {
	return func(_ *VM, arguments []Value) (Value, error) {
		text, err := parsedText(arguments)
		if err != nil {
			return VoidValue(), err
		}
		value, parseErr := strconv.ParseInt(text, radix, 32)
		if parseErr != nil {
			return VoidValue(), guestException("java/lang/NumberFormatException", parseErr.Error())
		}
		return IntValue(int32(value)), nil
	}
}

// parsedText reads the string a parse method was handed. A null is the
// standard's NumberFormatException rather than a failed native, because a
// title parsing a property the platform does not answer is asking a question
// with a Java answer: it catches the exception and takes its other path.
func parsedText(arguments []Value) (string, error) {
	object, err := nativeReference(arguments, 0)
	if err != nil {
		return "", err
	}
	if object == nil {
		return "", guestException("java/lang/NumberFormatException", "null")
	}
	value, ok := nativeStringObject(object)
	if !ok {
		return "", fmt.Errorf("native argument 0 is not a string")
	}
	return value, nil
}

// contextBuiltin installs one of the runtime's own bodies that needs the
// execution it was called on. It replaces whatever was registered for the
// method before, including a class definition's body, so the last registration
// is the one that runs.
func (vm *VM) contextBuiltin(class, name, descriptor string, method contextNativeMethod) {
	key := methodKey{class: class, name: name, descriptor: descriptor}
	vm.natives[key] = nativeEntry{context: method}
	vm.builtinNatives[key] = true
}

func (vm *VM) threadState(thread *Object) *guestThread {
	vm.threadMu.Lock()
	defer vm.threadMu.Unlock()
	state := vm.threads[thread]
	if state == nil {
		state = &guestThread{wake: make(chan struct{}, 1)}
		vm.threads[thread] = state
	}
	return state
}

// EndGuestThread records that a thread's run() has returned.
//
// A platform that installs GuestThreadStarter takes over Thread.start, and
// with it the end of the thread: nothing else can know when a run that the
// platform's own scheduler is driving has finished. Not reporting it leaves
// isAlive answering yes forever, and a title that loads on a worker and waits
// for it with `while (loader.isAlive())` waits for the whole session.
func (vm *VM) EndGuestThread(thread *Object) {
	if vm == nil || thread == nil {
		return
	}
	state := vm.threadState(thread)
	state.mu.Lock()
	state.alive = false
	state.mu.Unlock()
}

// guestPanicStack bounds the stack a panicking guest thread reports. A guest
// call chain is deep, and the frames near the panic are the ones worth having.
const guestPanicStack = 16 << 10

func (vm *VM) runGuestThread(thread *Object, state *guestThread) {
	execution := vm.newExecution()
	execution.thread = thread
	// The run is wrapped rather than deferred over the function because what
	// follows has to happen either way: a thread that panicked is still a
	// thread that is no longer alive, and a title that waits on isAlive would
	// wait for ever if the panic skipped that. The panic becomes this thread's
	// error, which the Host already has somewhere to put — it is one unsupported
	// title's thread, and taking the process down with it would destroy the
	// report that says which. This says in eight lines what backend.GuestPanic
	// says for the platforms; the Execution layer does not import the Runtime.
	var err error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				stack := debug.Stack()
				if len(stack) > guestPanicStack {
					stack = stack[:guestPanicStack]
				}
				if vm.config.Logger != nil {
					vm.config.Logger.Error("emulator panicked",
						"where", "JVM guest thread "+thread.ClassName,
						"panic", fmt.Sprint(recovered), "stack", string(stack))
				}
				err = fmt.Errorf("JVM guest thread %s panicked: %v", thread.ClassName, recovered)
			}
		}()
		_, err = vm.invokeInstance(execution, thread.ClassName, thread, "run", "()V", nil)
	}()
	state.mu.Lock()
	state.alive = false
	state.mu.Unlock()
	if err != nil {
		wrapped := fmt.Errorf("run guest thread %s: %w", thread.ClassName, err)
		if vm.config.AsyncError != nil {
			vm.config.AsyncError(wrapped)
		} else if vm.config.Logger != nil {
			vm.config.Logger.Error("JVM guest thread failed", "error", wrapped)
		}
	}
}

func (vm *VM) sleepGuestThread(thread *Object, duration time.Duration) error {
	state := vm.threadState(thread)
	state.mu.Lock()
	if state.interrupted {
		state.interrupted = false
		state.mu.Unlock()
		return guestException("java/lang/InterruptedException", "thread interrupted")
	}
	state.mu.Unlock()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-state.wake:
		state.mu.Lock()
		state.interrupted = false
		state.mu.Unlock()
		return guestException("java/lang/InterruptedException", "thread interrupted")
	}
}

func (vm *VM) threadYield() error {
	if vm.config.ThreadYield != nil {
		return vm.config.ThreadYield()
	}
	return nil
}

func (vm *VM) registerMathBuiltins() {
	vm.builtin("java/lang/Math", "abs", "(I)I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		if value < 0 {
			value = -value
		}
		return IntValue(value), nil
	})
	vm.builtin("java/lang/Math", "abs", "(J)J", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeLong(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		if value < 0 {
			value = -value
		}
		return LongValue(value), nil
	})
	vm.builtin("java/lang/Math", "abs", "(F)F", func(_ *VM, arguments []Value) (Value, error) {
		value, err := arguments[0].Float32()
		if err != nil {
			return VoidValue(), err
		}
		return FloatValue(math.Float32frombits(math.Float32bits(value) &^ (1 << 31))), nil
	})
	vm.builtin("java/lang/Math", "abs", "(D)D", func(_ *VM, arguments []Value) (Value, error) {
		value, err := arguments[0].Float64()
		if err != nil {
			return VoidValue(), err
		}
		return DoubleValue(math.Abs(value)), nil
	})
	vm.builtin("java/lang/Math", "min", "(II)I", func(_ *VM, arguments []Value) (Value, error) {
		left, err := nativeInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		right, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		if right < left {
			left = right
		}
		return IntValue(left), nil
	})
	vm.builtin("java/lang/Math", "max", "(II)I", func(_ *VM, arguments []Value) (Value, error) {
		left, err := nativeInt(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		right, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		if right > left {
			left = right
		}
		return IntValue(left), nil
	})
}

func (vm *VM) registerStringBuiltins() {
	vm.builtin(StringClass, "<init>", "()V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err == nil {
			object.Native = ""
		}
		return VoidValue(), err
	})
	vm.builtin(StringClass, "<init>", "(Ljava/lang/String;)V", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		value, err := nativeString(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		object.Native = value
		return VoidValue(), nil
	})
	for _, descriptor := range []string{"([B)V", "([BII)V", "([BLjava/lang/String;)V"} {
		descriptor := descriptor
		vm.builtin(StringClass, "<init>", descriptor, func(vm *VM, arguments []Value) (Value, error) {
			object, err := nativeReference(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			array, err := nativeReference(arguments, 1)
			if err != nil {
				return VoidValue(), err
			}
			data, err := ByteArraySnapshot(array)
			if err != nil {
				return VoidValue(), err
			}
			if descriptor == "([BII)V" {
				offset, offsetErr := nativeInt(arguments, 2)
				length, lengthErr := nativeInt(arguments, 3)
				if offsetErr != nil || lengthErr != nil || offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(data)) {
					return VoidValue(), guestException("java/lang/IndexOutOfBoundsException", "String byte range")
				}
				data = data[int(offset):int(offset+length)]
			}
			if descriptor == "([BLjava/lang/String;)V" {
				encoding, encodingErr := nativeString(arguments, 2)
				if encodingErr != nil {
					return VoidValue(), encodingErr
				}
				// The charset a title names is answered the same way here as
				// in the ranged constructor and in getBytes: those two go
				// through charsetOf, and this one had its own test that
				// accepted UTF-8 and nothing else. A title naming the
				// handset's own charset — `EUC_KR`, which charsetOf already
				// knows — was handed an IOException it caught, printed and
				// then drew an empty screen from for as long as it ran.
				switch charsetOf(encoding) {
				case charsetUTF8:
					object.Native = strings.ToValidUTF8(string(data), "\ufffd")
					return VoidValue(), nil
				case charsetPlatform:
					object.Native = vm.decodePlatformBytes(data)
					return VoidValue(), nil
				}
				return VoidValue(), guestException("java/io/IOException", "unsupported character encoding: "+encoding)
			}
			object.Native = vm.decodePlatformBytes(data)
			return VoidValue(), nil
		})
	}
	// The char-array constructors are how CLDC code turns a StringBuffer's or
	// a TextBox's characters back into a String; without them a game that
	// edits text has no way back.
	for _, descriptor := range []string{"([C)V", "([CII)V"} {
		descriptor := descriptor
		vm.builtin(StringClass, "<init>", descriptor, func(_ *VM, arguments []Value) (Value, error) {
			object, err := nativeReference(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			array, err := nativeReference(arguments, 1)
			if err != nil {
				return VoidValue(), err
			}
			component, values, err := ArraySnapshot(array)
			if err != nil {
				return VoidValue(), err
			}
			if component.Kind != TypeChar {
				return VoidValue(), fmt.Errorf("String constructor argument is not a char array")
			}
			offset, length := int32(0), int32(len(values))
			if descriptor == "([CII)V" {
				offsetValue, offsetErr := nativeInt(arguments, 2)
				lengthValue, lengthErr := nativeInt(arguments, 3)
				if offsetErr != nil || lengthErr != nil || offsetValue < 0 || lengthValue < 0 ||
					int64(offsetValue)+int64(lengthValue) > int64(len(values)) {
					return VoidValue(), guestException("java/lang/IndexOutOfBoundsException", "String char range")
				}
				offset, length = offsetValue, lengthValue
			}
			units := make([]uint16, length)
			for index := range units {
				unit, unitErr := values[int(offset)+index].Int32()
				if unitErr != nil {
					return VoidValue(), unitErr
				}
				units[index] = uint16(unit)
			}
			object.Native = string(utf16.Decode(units))
			return VoidValue(), nil
		})
	}
	vm.builtin("java/lang/String", "length", "()I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(int32(len(utf16.Encode([]rune(value))))), nil
	})
	vm.builtin("java/lang/String", "charAt", "(I)C", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		index, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		units := utf16.Encode([]rune(value))
		if index < 0 || int64(index) >= int64(len(units)) {
			return VoidValue(), guestException("java/lang/StringIndexOutOfBoundsException", fmt.Sprintf("index %d", index))
		}
		return IntValue(int32(units[index])), nil
	})
	vm.builtin("java/lang/String", "equals", "(Ljava/lang/Object;)Z", func(_ *VM, arguments []Value) (Value, error) {
		left, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		right, err := nativeReference(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		if rightValue, ok := nativeStringObject(right); ok && left == rightValue {
			return IntValue(1), nil
		}
		return IntValue(0), nil
	})
	vm.builtin("java/lang/String", "hashCode", "()I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		var hash int32
		for _, unit := range utf16.Encode([]rune(value)) {
			hash = 31*hash + int32(unit)
		}
		return IntValue(hash), nil
	})
	vm.builtin(StringClass, "concat", "(Ljava/lang/String;)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		left, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		right, err := nativeString(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		return ReferenceValue(nativeStringValue(left + right)), nil
	})
	vm.builtin(StringClass, "getBytes", "()[B", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return ReferenceValue(NewByteArray(vm.encodePlatformString(value))), nil
	})
	vm.builtin(StringClass, "toCharArray", "()[C", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		units := utf16.Encode([]rune(value))
		values := make([]Value, len(units))
		for index, unit := range units {
			values[index] = IntValue(int32(unit))
		}
		return ReferenceValue(&Object{
			ClassName: "[C",
			Native: &Array{
				Component: Type{Kind: TypeChar},
				storage:   valueStorage(values),
			},
		}), nil
	})
	vm.builtin(StringClass, "getChars", "(II[CI)V", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		begin, beginErr := nativeInt(arguments, 1)
		end, endErr := nativeInt(arguments, 2)
		destination, destinationErr := nativeReference(arguments, 3)
		offset, offsetErr := nativeInt(arguments, 4)
		if beginErr != nil || endErr != nil || destinationErr != nil || offsetErr != nil {
			return VoidValue(), fmt.Errorf("String.getChars arguments are invalid")
		}
		units := utf16.Encode([]rune(value))
		if begin < 0 || end < begin || int64(end) > int64(len(units)) {
			return VoidValue(), guestException("java/lang/StringIndexOutOfBoundsException", fmt.Sprintf("range %d..%d", begin, end))
		}
		values := make([]Value, 0, end-begin)
		for _, unit := range units[begin:end] {
			values = append(values, IntValue(int32(unit)))
		}
		if err := SetArrayRange(destination, int(offset), values); err != nil {
			return VoidValue(), err
		}
		return VoidValue(), nil
	})
	vm.builtin(StringClass, "indexOf", "(I)I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		character, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		for index, unit := range utf16.Encode([]rune(value)) {
			if int32(unit) == character {
				return IntValue(int32(index)), nil
			}
		}
		return IntValue(-1), nil
	})
	for _, descriptor := range []string{"(Ljava/lang/String;)I", "(Ljava/lang/String;I)I"} {
		descriptor := descriptor
		vm.builtin(StringClass, "indexOf", descriptor, func(_ *VM, arguments []Value) (Value, error) {
			value, err := nativeString(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			needle, err := nativeString(arguments, 1)
			if err != nil {
				return VoidValue(), err
			}
			from := int32(0)
			if descriptor == "(Ljava/lang/String;I)I" {
				from, err = nativeInt(arguments, 2)
				if err != nil {
					return VoidValue(), err
				}
			}
			units := utf16.Encode([]rune(value))
			needleUnits := utf16.Encode([]rune(needle))
			index := indexUTF16(units, needleUnits, int(from))
			return IntValue(int32(index)), nil
		})
	}
	vm.builtin(StringClass, "startsWith", "(Ljava/lang/String;)Z", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		prefix, err := nativeString(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		return booleanValue(strings.HasPrefix(value, prefix)), nil
	})
	for _, descriptor := range []string{"(I)Ljava/lang/String;", "(II)Ljava/lang/String;"} {
		descriptor := descriptor
		vm.builtin(StringClass, "substring", descriptor, func(_ *VM, arguments []Value) (Value, error) {
			value, err := nativeString(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			begin, err := nativeInt(arguments, 1)
			if err != nil {
				return VoidValue(), err
			}
			units := utf16.Encode([]rune(value))
			end := int32(len(units))
			if descriptor == "(II)Ljava/lang/String;" {
				end, err = nativeInt(arguments, 2)
				if err != nil {
					return VoidValue(), err
				}
			}
			if begin < 0 || end < begin || int64(end) > int64(len(units)) {
				// The range and the length are in the message because the two
				// numbers are the whole of what went wrong, and a title that
				// slices a string it built itself gives no other clue.
				return VoidValue(), guestException("java/lang/StringIndexOutOfBoundsException",
					fmt.Sprintf("substring range %d..%d of a %d character string", begin, end, len(units)))
			}
			return ReferenceValue(nativeStringValue(string(utf16.Decode(units[begin:end])))), nil
		})
	}
	// compareTo orders by UTF-16 unit, which is what the language defines and
	// what a title's own sort depends on. Comparing the Go strings instead
	// would order the astral planes ahead of the private use area rather than
	// after it.
	vm.builtin(StringClass, "compareTo", "(Ljava/lang/String;)I", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		other, err := nativeString(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		left, right := utf16.Encode([]rune(value)), utf16.Encode([]rune(other))
		for index := 0; index < len(left) && index < len(right); index++ {
			if left[index] != right[index] {
				return IntValue(int32(left[index]) - int32(right[index])), nil
			}
		}
		return IntValue(int32(len(left) - len(right))), nil
	})
	vm.builtin(StringClass, "equalsIgnoreCase", "(Ljava/lang/String;)Z", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		other, err := nativeString(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		return booleanValue(strings.EqualFold(value, other)), nil
	})
	vm.builtin(StringClass, "endsWith", "(Ljava/lang/String;)Z", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		suffix, err := nativeString(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		return booleanValue(strings.HasSuffix(value, suffix)), nil
	})
	vm.builtin(StringClass, "toUpperCase", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		return ReferenceValue(nativeStringValue(strings.ToUpper(value))), err
	})
	vm.builtin(StringClass, "toLowerCase", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		return ReferenceValue(nativeStringValue(strings.ToLower(value))), err
	})
	vm.builtin(StringClass, "trim", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		value = strings.TrimFunc(value, func(r rune) bool { return r <= 0x20 })
		return ReferenceValue(nativeStringValue(value)), nil
	})
	vm.builtin(StringClass, "toString", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		return ReferenceValue(object), err
	})
	vm.builtin(StringClass, "valueOf", "(C)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeInt(arguments, 0)
		return ReferenceValue(nativeStringValue(string(utf16.Decode([]uint16{uint16(value)})))), err
	})
	vm.builtin(StringClass, "valueOf", "(I)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeInt(arguments, 0)
		return ReferenceValue(nativeStringValue(strconv.FormatInt(int64(value), 10))), err
	})
	vm.builtin(StringClass, "valueOf", "(J)Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		value, err := nativeLong(arguments, 0)
		return ReferenceValue(nativeStringValue(strconv.FormatInt(value, 10))), err
	})
	vm.builtin(StringClass, "valueOf", "(Ljava/lang/Object;)Ljava/lang/String;", func(vm *VM, arguments []Value) (Value, error) {
		object, err := nativeReference(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		text, err := vm.objectText(object)
		if err != nil {
			return VoidValue(), err
		}
		return ReferenceValue(nativeStringValue(text)), nil
	})
}

type stringBufferData struct {
	mu    sync.Mutex
	units []uint16
}

// registerStringBuilderBuiltins implements one mutable character buffer for
// both java/lang/StringBuffer and java/lang/StringBuilder. The two differ
// only in the return type of the chaining methods and in StringBuffer's
// synchronization, which this implementation gives to both.
func (vm *VM) registerStringBuilderBuiltins(class string) {
	self := "L" + class + ";"
	for _, descriptor := range []string{"()V", "(I)V", "(Ljava/lang/String;)V"} {
		descriptor := descriptor
		vm.builtin(class, "<init>", descriptor, func(_ *VM, arguments []Value) (Value, error) {
			object, err := nativeReference(arguments, 0)
			if err != nil {
				return VoidValue(), err
			}
			data := &stringBufferData{}
			// The (I)V form is a capacity hint; the backing slice grows on
			// demand, so the value only needs validation.
			if descriptor == "(I)V" {
				capacity, capacityErr := nativeInt(arguments, 1)
				if capacityErr != nil {
					return VoidValue(), capacityErr
				}
				if capacity < 0 {
					return VoidValue(), guestException("java/lang/NegativeArraySizeException", "StringBuffer capacity")
				}
			} else if descriptor != "()V" {
				value, valueErr := nativeString(arguments, 1)
				if valueErr != nil {
					return VoidValue(), valueErr
				}
				data.units = utf16.Encode([]rune(value))
			}
			object.Native = data
			return VoidValue(), nil
		})
	}
	vm.builtin(class, "append", "(C)"+self, appendStringBuffer(func(arguments []Value) (string, error) {
		value, err := nativeInt(arguments, 1)
		return string(utf16.Decode([]uint16{uint16(value)})), err
	}))
	vm.builtin(class, "append", "(I)"+self, appendStringBuffer(func(arguments []Value) (string, error) {
		value, err := nativeInt(arguments, 1)
		return strconv.FormatInt(int64(value), 10), err
	}))
	vm.builtin(class, "append", "(Ljava/lang/String;)"+self, appendStringBuffer(func(arguments []Value) (string, error) {
		object, err := nativeReference(arguments, 1)
		if err != nil {
			return "", err
		}
		if object == nil {
			return "null", nil
		}
		return nativeString(arguments, 1)
	}))
	vm.builtin(class, "append", "(Ljava/lang/Object;)"+self, appendStringBuffer(func(arguments []Value) (string, error) {
		object, err := nativeReference(arguments, 1)
		if err != nil {
			return "", err
		}
		return vm.objectText(object)
	}))
	vm.builtin(class, "append", "(J)"+self, appendStringBuffer(func(arguments []Value) (string, error) {
		value, err := nativeLong(arguments, 1)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d", value), nil
	}))
	vm.builtin(class, "append", "(Z)"+self, appendStringBuffer(func(arguments []Value) (string, error) {
		value, err := nativeInt(arguments, 1)
		if err != nil {
			return "", err
		}
		if value != 0 {
			return "true", nil
		}
		return "false", nil
	}))
	// The char-array appends. The whole array and a window on it are the same
	// read, and both are what a title that formats into a char array reaches
	// for when it puts the result into a line.
	vm.builtin(class, "append", "([C)"+self, appendStringBuffer(func(arguments []Value) (string, error) {
		array, err := nativeReference(arguments, 1)
		if err != nil {
			return "", err
		}
		if array == nil {
			return "", guestException("java/lang/NullPointerException", "StringBuffer.append char array")
		}
		_, values, err := ArraySnapshot(array)
		if err != nil {
			return "", err
		}
		return charArrayString(array, 0, int32(len(values)))
	}))
	vm.builtin(class, "append", "([CII)"+self, appendStringBuffer(func(arguments []Value) (string, error) {
		array, err := nativeReference(arguments, 1)
		if err != nil {
			return "", err
		}
		if array == nil {
			return "", guestException("java/lang/NullPointerException", "StringBuffer.append char array")
		}
		offset, err := nativeInt(arguments, 2)
		if err != nil {
			return "", err
		}
		length, err := nativeInt(arguments, 3)
		if err != nil {
			return "", err
		}
		return charArrayString(array, offset, length)
	}))
	vm.builtin(class, "delete", "(II)"+self, func(_ *VM, arguments []Value) (Value, error) {
		object, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		start, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		end, err := nativeInt(arguments, 2)
		if err != nil {
			return VoidValue(), err
		}
		data.mu.Lock()
		if start < 0 || int(start) > len(data.units) || end < start {
			data.mu.Unlock()
			return VoidValue(), guestException("java/lang/StringIndexOutOfBoundsException", "StringBuffer.delete range")
		}
		if int(end) > len(data.units) {
			end = int32(len(data.units))
		}
		data.units = append(data.units[:start], data.units[end:]...)
		data.mu.Unlock()
		return ReferenceValue(object), nil
	})
	// deleteCharAt is delete's one-character form, and it is the one a title
	// backspacing a field reaches for. It is not delete(i, i+1) at the end of
	// the buffer: the specification refuses an index equal to the length,
	// where the ranged form clamps.
	vm.builtin(class, "deleteCharAt", "(I)"+self, func(_ *VM, arguments []Value) (Value, error) {
		object, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		index, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		data.mu.Lock()
		if index < 0 || int(index) >= len(data.units) {
			data.mu.Unlock()
			return VoidValue(), guestException("java/lang/StringIndexOutOfBoundsException", fmt.Sprintf("StringBuffer.deleteCharAt index %d", index))
		}
		data.units = append(data.units[:index], data.units[index+1:]...)
		data.mu.Unlock()
		return ReferenceValue(object), nil
	})
	// getChars copies a window of the buffer into a char array the caller
	// owns, which is how a title lays out text it is still building without
	// making a String of it first. It is String.getChars against a buffer, so
	// the range check is the same one.
	vm.builtin(class, "getChars", "(II[CI)V", func(_ *VM, arguments []Value) (Value, error) {
		_, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		begin, beginErr := nativeInt(arguments, 1)
		end, endErr := nativeInt(arguments, 2)
		destination, destinationErr := nativeReference(arguments, 3)
		offset, offsetErr := nativeInt(arguments, 4)
		if beginErr != nil || endErr != nil || destinationErr != nil || offsetErr != nil {
			return VoidValue(), fmt.Errorf("StringBuffer.getChars arguments are invalid")
		}
		data.mu.Lock()
		if begin < 0 || end < begin || int(end) > len(data.units) {
			data.mu.Unlock()
			return VoidValue(), guestException("java/lang/StringIndexOutOfBoundsException", fmt.Sprintf("StringBuffer.getChars range %d..%d", begin, end))
		}
		values := make([]Value, 0, end-begin)
		for _, unit := range data.units[begin:end] {
			values = append(values, IntValue(int32(unit)))
		}
		data.mu.Unlock()
		return VoidValue(), SetArrayRange(destination, int(offset), values)
	})
	// insert is delete's opposite and the only other way a title of this era
	// edits a buffer in place: it builds a line and then puts a prefix in front
	// of it rather than rebuilding the whole string.
	for _, form := range []struct {
		descriptor string
		text       func([]Value) (string, error)
	}{
		{"Ljava/lang/String;", func(arguments []Value) (string, error) {
			object, err := nativeReference(arguments, 2)
			if err != nil {
				return "", err
			}
			if object == nil {
				return "null", nil
			}
			return nativeString(arguments, 2)
		}},
		{"C", func(arguments []Value) (string, error) {
			value, err := nativeInt(arguments, 2)
			return string(utf16.Decode([]uint16{uint16(value)})), err
		}},
		{"I", func(arguments []Value) (string, error) {
			value, err := nativeInt(arguments, 2)
			return strconv.FormatInt(int64(value), 10), err
		}},
	} {
		text := form.text
		vm.builtin(class, "insert", "(I"+form.descriptor+")"+self, func(_ *VM, arguments []Value) (Value, error) {
			object, data, err := stringBufferArgument(arguments)
			if err != nil {
				return VoidValue(), err
			}
			offset, err := nativeInt(arguments, 1)
			if err != nil {
				return VoidValue(), err
			}
			value, err := text(arguments)
			if err != nil {
				return VoidValue(), err
			}
			data.mu.Lock()
			defer data.mu.Unlock()
			if offset < 0 || int(offset) > len(data.units) {
				return VoidValue(), guestException("java/lang/StringIndexOutOfBoundsException", "StringBuffer.insert offset")
			}
			data.units = slices.Insert(data.units, int(offset), utf16.Encode([]rune(value))...)
			return ReferenceValue(object), nil
		})
	}
	vm.builtin(class, "setLength", "(I)V", func(_ *VM, arguments []Value) (Value, error) {
		_, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		length, err := nativeInt(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		if length < 0 {
			return VoidValue(), guestException("java/lang/IndexOutOfBoundsException", "StringBuffer.setLength")
		}
		data.mu.Lock()
		if int(length) <= len(data.units) {
			data.units = data.units[:length]
		} else {
			grown := make([]uint16, length)
			copy(grown, data.units)
			data.units = grown
		}
		data.mu.Unlock()
		return VoidValue(), nil
	})
	vm.builtin(class, "length", "()I", func(_ *VM, arguments []Value) (Value, error) {
		_, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		data.mu.Lock()
		length := len(data.units)
		data.mu.Unlock()
		return IntValue(int32(length)), nil
	})
	vm.builtin(class, "toString", "()Ljava/lang/String;", func(_ *VM, arguments []Value) (Value, error) {
		_, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		data.mu.Lock()
		value := string(utf16.Decode(append([]uint16(nil), data.units...)))
		data.mu.Unlock()
		return ReferenceValue(nativeStringValue(value)), nil
	})
}

func appendStringBuffer(value func([]Value) (string, error)) NativeMethod {
	return func(_ *VM, arguments []Value) (Value, error) {
		object, data, err := stringBufferArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		text, err := value(arguments)
		if err != nil {
			return VoidValue(), err
		}
		data.mu.Lock()
		data.units = append(data.units, utf16.Encode([]rune(text))...)
		data.mu.Unlock()
		return ReferenceValue(object), nil
	}
}

func stringBufferArgument(arguments []Value) (*Object, *stringBufferData, error) {
	object, err := nativeReference(arguments, 0)
	if err != nil {
		return nil, nil, err
	}
	data, ok := object.Native.(*stringBufferData)
	if !ok || data == nil {
		return nil, nil, fmt.Errorf("receiver is not a StringBuffer")
	}
	return object, data, nil
}

func nativeStringValue(value string) *Object {
	return &Object{ClassName: StringClass, Native: value}
}

func booleanValue(value bool) Value {
	if value {
		return IntValue(1)
	}
	return IntValue(0)
}

func indexUTF16(value, needle []uint16, from int) int {
	if from < 0 {
		from = 0
	}
	if len(needle) == 0 {
		return min(from, len(value))
	}
	for index := from; index+len(needle) <= len(value); index++ {
		match := true
		for offset := range needle {
			if value[index+offset] != needle[offset] {
				match = false
				break
			}
		}
		if match {
			return index
		}
	}
	return -1
}

// builtin installs one of the runtime's own bodies. See contextBuiltin: an
// entry holds one kind or the other, so writing it replaces whichever was
// there.
func (vm *VM) builtin(class, name, descriptor string, method NativeMethod) {
	key := methodKey{class: class, name: name, descriptor: descriptor}
	vm.natives[key] = nativeEntry{plain: method}
	vm.builtinNatives[key] = true
}

// printStreamEmit takes what a game printed to the logging boundary. It is
// debug output from a shipped title, so it goes out at debug level and a build
// that logs nothing drops it; what matters is that the call returns rather than
// failing somewhere unrelated to printing.
func printStreamEmit(vm *VM, arguments []Value) (Value, error) {
	stream, err := nativeInt(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	text, err := nativeString(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if vm.config.Logger != nil {
		name := "out"
		if stream != 0 {
			name = "err"
		}
		vm.config.Logger.Debug("guest print", "stream", name, "text", text)
	}
	return VoidValue(), nil
}

func systemArraycopy(vm *VM, arguments []Value) (Value, error) {
	sourceObject, err := nativeReference(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	sourceIndex, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	targetObject, err := nativeReference(arguments, 2)
	if err != nil {
		return VoidValue(), err
	}
	targetIndex, err := nativeInt(arguments, 3)
	if err != nil {
		return VoidValue(), err
	}
	length, err := nativeInt(arguments, 4)
	if err != nil {
		return VoidValue(), err
	}
	source, err := objectArray(sourceObject)
	if err != nil {
		return VoidValue(), err
	}
	target, err := objectArray(targetObject)
	if err != nil {
		return VoidValue(), err
	}
	if source.Component.Descriptor() != target.Component.Descriptor() &&
		(!source.Component.IsReference() || !target.Component.IsReference()) {
		return VoidValue(), guestException("java/lang/ArrayStoreException", "incompatible array types")
	}
	if sourceIndex < 0 || targetIndex < 0 || length < 0 ||
		int64(sourceIndex)+int64(length) > int64(source.Length()) ||
		int64(targetIndex)+int64(length) > int64(target.Length()) {
		return VoidValue(), guestException("java/lang/ArrayIndexOutOfBoundsException", "arraycopy range")
	}
	vm.arraycopyMu.Lock()
	defer vm.arraycopyMu.Unlock()
	// Reading the whole source range before writing any of it is what makes an
	// overlapping copy within one array behave, so the two halves stay separate
	// operations even when source and target are the same array.
	values, err := source.LoadRange(int(sourceIndex), int(length))
	if err != nil {
		return VoidValue(), err
	}
	if err := target.StoreRange(int(targetIndex), values); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), nil
}

func nativeReference(arguments []Value, index int) (*Object, error) {
	if index < 0 || index >= len(arguments) {
		return nil, fmt.Errorf("native argument %d is missing", index)
	}
	return arguments[index].Reference()
}

func nativeInt(arguments []Value, index int) (int32, error) {
	if index < 0 || index >= len(arguments) {
		return 0, fmt.Errorf("native argument %d is missing", index)
	}
	return arguments[index].Int32()
}

func nativeLong(arguments []Value, index int) (int64, error) {
	if index < 0 || index >= len(arguments) {
		return 0, fmt.Errorf("native argument %d is missing", index)
	}
	return arguments[index].Int64()
}

func nativeString(arguments []Value, index int) (string, error) {
	object, err := nativeReference(arguments, index)
	if err != nil {
		return "", err
	}
	if value, ok := nativeStringObject(object); ok {
		return value, nil
	}
	return "", fmt.Errorf("native argument %d is not a string", index)
}

func nativeStringObject(object *Object) (string, bool) {
	if object == nil || object.ClassName != "java/lang/String" {
		return "", false
	}
	value, ok := object.Native.(string)
	return value, ok
}

// classObjectName reads the internal class name a java/lang/Class instance
// stands for.
func classObjectName(arguments []Value) (string, error) {
	object, err := nativeReference(arguments, 0)
	if err != nil {
		return "", err
	}
	name, ok := object.Native.(string)
	if object.ClassName != ClassClass || !ok {
		return "", fmt.Errorf("receiver is not a Class")
	}
	return name, nil
}

// timeZoneData is a zone's identifier and its offset from GMT in milliseconds.
// Nothing here observes daylight saving: the two zones this runtime has are GMT
// and whatever the guest clock's own offset is right now.
type timeZoneData struct {
	id     string
	offset int32
}

// gmtTimeZoneID is the identifier the specification gives the zero-offset zone
// and the answer an unrecognized identifier gets.
const gmtTimeZoneID = "GMT"

// guestTimeZone is the zone the guest clock runs in, named by its offset the
// way the specification's own custom identifiers are.
func (vm *VM) guestTimeZone() *timeZoneData {
	_, seconds := time.UnixMilli(vm.nowMilliseconds()).Zone()
	if seconds == 0 {
		return &timeZoneData{id: gmtTimeZoneID}
	}
	sign, minutes := "+", seconds/60
	if minutes < 0 {
		sign, minutes = "-", -minutes
	}
	return &timeZoneData{
		id:     fmt.Sprintf("GMT%s%02d:%02d", sign, minutes/60, minutes%60),
		offset: int32(seconds) * 1000,
	}
}

// timeZoneLocation is the zone a calendar breaks an instant into fields with.
// The two zones this runtime has are GMT and the guest clock's own, and the
// second is time.Local at the instant being read — guestTimeZone reads its
// offset off that clock — so naming it rather than rebuilding it keeps a
// calendar made from the default zone identical to one made without a zone at
// all, daylight saving included.
func timeZoneLocation(data *timeZoneData, instant time.Time) *time.Location {
	if data == nil || data.offset == 0 {
		return time.UTC
	}
	if _, seconds := instant.Zone(); int32(seconds)*1000 == data.offset {
		return time.Local
	}
	return time.FixedZone(data.id, int(data.offset/1000))
}

func newTimeZoneObject(data *timeZoneData) Value {
	return ReferenceValue(&Object{ClassName: TimeZoneClass, Native: data})
}

func (vm *VM) registerTimeZoneBuiltins() {
	vm.builtin(TimeZoneClass, "getDefault", "()Ljava/util/TimeZone;", func(vm *VM, _ []Value) (Value, error) {
		return newTimeZoneObject(vm.guestTimeZone()), nil
	})
	vm.builtin(TimeZoneClass, "getTimeZone", "(Ljava/lang/String;)Ljava/util/TimeZone;", func(vm *VM, arguments []Value) (Value, error) {
		name, err := nativeString(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		if local := vm.guestTimeZone(); name == local.id {
			return newTimeZoneObject(local), nil
		}
		return newTimeZoneObject(&timeZoneData{id: gmtTimeZoneID}), nil
	})
	vm.builtin(TimeZoneClass, "getAvailableIDs", "()[Ljava/lang/String;", func(vm *VM, _ []Value) (Value, error) {
		identifiers := []string{vm.guestTimeZone().id}
		if identifiers[0] != gmtTimeZoneID {
			identifiers = append(identifiers, gmtTimeZoneID)
		}
		array, err := vm.NewArray(Type{Kind: TypeReference, ClassName: StringClass}, int32(len(identifiers)))
		if err != nil {
			return VoidValue(), err
		}
		values := make([]Value, len(identifiers))
		for index, identifier := range identifiers {
			values[index] = ReferenceValue(vm.NewString(identifier))
		}
		if err := SetArrayRange(array, 0, values); err != nil {
			return VoidValue(), err
		}
		return ReferenceValue(array), nil
	})
	vm.builtin(TimeZoneClass, "getID", "()Ljava/lang/String;", func(vm *VM, arguments []Value) (Value, error) {
		data, err := timeZoneArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		return ReferenceValue(vm.NewString(data.id)), nil
	})
	vm.builtin(TimeZoneClass, "getRawOffset", "()I", func(_ *VM, arguments []Value) (Value, error) {
		data, err := timeZoneArgument(arguments)
		if err != nil {
			return VoidValue(), err
		}
		return IntValue(data.offset), nil
	})
	vm.builtin(TimeZoneClass, "useDaylightTime", "()Z", func(_ *VM, _ []Value) (Value, error) {
		return IntValue(0), nil
	})
}

func timeZoneArgument(arguments []Value) (*timeZoneData, error) {
	object, err := nativeReference(arguments, 0)
	if err != nil {
		return nil, err
	}
	data, ok := object.Native.(*timeZoneData)
	if !ok || data == nil {
		return nil, fmt.Errorf("receiver is not a TimeZone")
	}
	return data, nil
}

// objectText is what `String.valueOf(Object)` and a `StringBuffer` append
// answer for a reference, and it is the specification's answer rather than a
// name: an object whose class overrides `toString` is asked, and only a class
// that does not override it is printed as `class@identity`.
//
// Asking costs a virtual dispatch out of a native — guest code re-entered from
// inside a call the guest is already in — so the resolution decides first and
// nothing is invoked for the common case. A census over one platform's whole
// corpus found the object form asked for by seven titles and handed a string
// every time, which is why this stayed a name for so long; the caller that
// finally needed it builds a resource path out of a `StringBuffer` and gets a
// file name nothing in its archive answers, so it catches its own exception,
// keeps the null image it was left with, and paints it several calls later.
func (vm *VM) objectText(object *Object) (string, error) {
	if object == nil {
		return "null", nil
	}
	if value, ok := nativeStringObject(object); ok {
		return value, nil
	}
	identity := fmt.Sprintf("%s@%x", object.ClassName, vm.objectIdentity(object))
	// The root's own toString is this same identity, so a class that does not
	// override it is answered without entering the interpreter at all. A class
	// that cannot be resolved is answered the same way, because a name is a
	// better answer here than ending the session.
	declaring, _, err := vm.resolveInstanceMethod(object.ClassName, "toString", "()Ljava/lang/String;")
	if err != nil || declaring == nil || declaring.Name == ObjectClass {
		return identity, nil
	}
	if depth := vm.toStringDepth.Add(1); depth > maxToStringDepth {
		vm.toStringDepth.Add(-1)
		return identity, nil
	}
	result, err := vm.InvokeVirtual(object, "toString", "()Ljava/lang/String;")
	vm.toStringDepth.Add(-1)
	if err != nil {
		return "", err
	}
	text, err := result.Reference()
	if err != nil {
		return "", err
	}
	if text == nil {
		return "null", nil
	}
	value, ok := nativeStringObject(text)
	if !ok {
		return identity, nil
	}
	return value, nil
}

// maxToStringDepth bounds objectText's nesting across the whole machine. One
// object naming another is ordinary; a chain this long is a guest archive
// recursing, and the answer it gets back is the identity it would have had
// before any of this.
const maxToStringDepth = 16

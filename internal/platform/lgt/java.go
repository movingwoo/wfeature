package lgt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The Java interface table. A handful of LGT titles are not Clets: their
// application code is Java, compiled ahead of time to the same native ARM the
// module is otherwise made of, and the class metadata that would be in a class
// file is handed to the platform through this table instead.
//
// Nothing here runs such a title yet. What is here takes delivery of that
// metadata and answers it — where a field sits, which vtable slot a method
// takes, what address a static method is entered at (java_metadata.go,
// java_link.go) — and then reports, in one place, exactly what the title asked
// for next. The table is undocumented, the original runtime does not name it,
// and the only way to learn its shape is to watch a real title fill it in. The
// alternative, refusing at the first resolution, stops the title before it has
// said anything.
//
// The indices are the ones the modules here resolve. Names are descriptions of
// what the call was seen doing, not names from any specification.
const (
	javaSVCRegisterUnknown uint32 = 0x03
	javaSVCPrepare         uint32 = 0x06
	javaSVCClassList       uint32 = 0x07
	javaSVCStringConstant  uint32 = 0x09
	javaSVCPrepareClass    uint32 = 0x0b
	javaSVCResolveClass    uint32 = 0x0c
	javaSVCInitializeClass uint32 = 0x0d
	javaSVCResolveArray    uint32 = 0x0e
	javaSVCAllocate        uint32 = 0x0f
	javaSVCAllocateArray   uint32 = 0x10
	javaSVCAllocateArrayN  uint32 = 0x11
	javaSVCTypeCheck       uint32 = 0x12
	javaSVCDefineClass     uint32 = 0x13
	javaSVCLoadClasses     uint32 = 0x14
	javaSVCThrowNull       uint32 = 0x22
	javaSVCThrowArrayIndex uint32 = 0x23
	// javaSVCInterfaceTable is `invokeinterface`'s half of a dispatch: it is
	// handed a receiver and the name of an interface, and answers where in the
	// receiver's own dispatch table that interface's methods begin. The call
	// site reads `+4` out of the answer and calls it with the receiver — and
	// `+8`, `+0xc` for the interface's later methods — so the answer is the
	// vtable biased by the slot the class record's interface table gives.
	javaSVCInterfaceTable uint32 = 0x64
	javaSVCStoreReference uint32 = 0xfa
	// The second reference store. **Nothing at any call site tells it from
	// `0xfa`**: both take an array, an index and a reference in the same three
	// registers, both drop the answer, and one module uses each of them dozens
	// of times. What the first argument is, is what settled it — asked at the
	// call that stopped a title, it answered `[Lr;`, an array of the class the
	// third argument is an instance of. A store check is the likeliest thing
	// the pair differs by, and that check is the one this platform cannot make
	// (see java_array.go), so the two do the same thing here.
	javaSVCStoreReferenceAlso uint32 = 0x61
	// The store a sixty-four bit element takes. **It is four registers rather
	// than three**, which is what tells it from the reference store above: the
	// array, the index, and the value's low and high words in the pair the
	// rest of this platform's ABI carries a long in. The one call site reached
	// locally clears a `long[]` in a loop — the array out of a field, the
	// index a counter incremented on the line after the call, both value
	// registers set to zero, and the answer dropped — and there is no reading
	// of that under which the two zeroes are anything but the value.
	javaSVCStoreWide uint32 = 0xfd
	// The load that goes with it: an array and an index, and the element back
	// in the register pair a long is returned in. The site that settled it is
	// the whole body of a one-line accessor — the array out of a field, the
	// index straight from the method's own argument, and the frame popped on
	// the instruction after the call — so what it answers is what the method
	// answers, and the only thing an eight-byte element can be read into.
	javaSVCLoadWide uint32 = 0x5b
	// `synchronized`: the lock a protected body is entered and left through.
	// See java_thread.go for what says which is which.
	javaSVCMonitorEnter     uint32 = 0x56
	javaSVCMonitorExit      uint32 = 0x57
	javaSVCEnterMethod      uint32 = 0x54
	javaSVCLeaveMethod      uint32 = 0x55
	javaSVCUnknownEighty    uint32 = 0x82
	javaSVCInvokeStaticMain uint32 = 0x83
	// The two types every module asks for by themselves. **Neither takes an
	// argument**, at any of the 254 call sites across the local Java titles,
	// and each answer goes straight to one call: `0xe1`'s to the allocator,
	// followed by one of `java/lang/String`'s own constructors, and `0xe2`'s to
	// the array allocation, whose elements are then stored through the
	// reference store `0xfa` and read back out of fields declared
	// `[Ljava/lang/String;`. They are `String` and `String[]`, which is what a
	// compiler would hard-wire: a Jlet is entered with one and the platform
	// builds every string constant out of the other.
	javaSVCStringClass     uint32 = 0xe1
	javaSVCStringArrayType uint32 = 0xe2
)

// javaSVCArguments is how many arguments each of these takes. Only the load
// call passes more than four, and it passes eleven — four in registers and
// seven on the stack.
//
// The slots past the startup sequence are here because a module's import
// stubs resolve lazily — the first call is what resolves one — so a slot that
// no local title has reached yet still has to resolve, or the title stops at
// the resolution rather than at the call that would have said what it wanted.
// The counts for those are what their call sites pass; see docs/lgt.md.
var javaSVCArguments = map[uint32]int{
	javaSVCRegisterUnknown:    3,
	javaSVCPrepare:            1,
	javaSVCClassList:          2,
	javaSVCPrepareClass:       1,
	javaSVCResolveClass:       2,
	javaSVCInitializeClass:    2,
	javaSVCResolveArray:       3,
	javaSVCAllocate:           1,
	javaSVCAllocateArray:      2,
	javaSVCAllocateArrayN:     3,
	javaSVCStoreReference:     3,
	javaSVCStoreReferenceAlso: 3,
	javaSVCStoreWide:          4,
	javaSVCLoadWide:           2,
	javaSVCMonitorEnter:       1,
	javaSVCMonitorExit:        1,
	javaSVCDefineClass:        12,
	javaSVCLoadClasses:        11,
	javaSVCThrowNull:          1,
	javaSVCThrowArrayIndex:    2,
	javaSVCInterfaceTable:     2,
	javaSVCEnterTry:           1,
	javaSVCLeaveTry:           1,
	javaSVCThrow:              1,
	javaSVCThrowArithmetic:    1,
	javaSVCTypeCheck:          2,
	javaSVCStringClass:        0,
	javaSVCStringArrayType:    0,
	javaSVCStringConstant:     4,
	javaSVCEnterMethod:        1,
	javaSVCLeaveMethod:        1,
	javaSVCUnknownEighty:      1,
	javaSVCInvokeStaticMain:   4,
}

// javaSVCNames is what a slot is called when it is reported. A slot with no
// name here is one this platform has only seen in a stub table.
var javaSVCNames = map[uint32]string{
	javaSVCRegisterUnknown:    "register application",
	javaSVCPrepare:            "acknowledge the class list",
	javaSVCClassList:          "hand over the application's classes",
	javaSVCPrepareClass:       "prepare a class",
	javaSVCResolveClass:       "resolve a class",
	javaSVCInitializeClass:    "run a class initializer",
	javaSVCResolveArray:       "resolve an array type",
	javaSVCAllocate:           "allocate an instance",
	javaSVCAllocateArray:      "allocate an array",
	javaSVCAllocateArrayN:     "allocate an array of arrays",
	javaSVCStoreReference:     "store into a reference array",
	javaSVCStoreReferenceAlso: "store into a reference array",
	javaSVCStoreWide:          "store into a long array",
	javaSVCLoadWide:           "read from a long array",
	javaSVCMonitorEnter:       "take an object's lock",
	javaSVCMonitorExit:        "give an object's lock back",
	javaSVCDefineClass:        "lay one application class out",
	javaSVCLoadClasses:        "load the platform classes",
	javaSVCThrowNull:          "throw NullPointerException",
	javaSVCThrowArrayIndex:    "throw ArrayIndexOutOfBoundsException",
	javaSVCInterfaceTable:     "where an interface's methods start",
	javaSVCEnterTry:           "enter a try region",
	javaSVCLeaveTry:           "leave a try region",
	javaSVCThrow:              "throw",
	javaSVCThrowArithmetic:    "throw ArithmeticException",
	javaSVCTypeCheck:          "is this class the named one",
	javaSVCStringClass:        "the String class",
	javaSVCStringArrayType:    "the String[] array type",
	javaSVCStringConstant:     "make a string constant",
	javaSVCEnterMethod:        "enter a method",
	javaSVCLeaveMethod:        "leave a method",
	javaSVCInvokeStaticMain:   "enter the application",
}

// A Java title also resolves single functions out of three tables of their
// own, each at index 3. They are carried on this category too, keyed by table
// and index together, because there is nothing else in those tables to make
// them worth a category each.
var javaAuxiliaryTables = map[uint32]bool{
	0x1fc: true,
	0x1ff: true,
	0x201: true,
}

// oemJavaFunction is the one entry of the OEM table that only a Java title
// resolves. It sits in that table but has nothing to do with the OEM calls a
// Clet makes, so it is serviced here rather than there.
const oemJavaFunction uint32 = 0x17

// javaAuxiliarySlot packs a table and index into one slot number. It is above
// every real index, so the two kinds never collide.
func javaAuxiliarySlot(table, index uint32) uint32 { return 1<<24 | table<<8 | index }

func javaAuxiliaryParts(slot uint32) (uint32, uint32, bool) {
	if slot&(1<<24) == 0 {
		return 0, 0, false
	}
	return slot >> 8 & 0xffff, slot & 0xff, true
}

// javaInterfaceFunction resolves one entry of the Java interface table.
// Resolving anything from it is what identifies the title as a Java app,
// which is the fact every later failure has to be reported against: the calls
// this platform cannot serve are not gaps a Clet would ever reach.
func (client *Client) javaInterfaceFunction(index uint32) (uint32, error) {
	client.javaApplication = true
	if _, known := javaSVCArguments[index]; !known && client.logger != nil {
		client.logger.Debug("LGT java interface function is not known", "function", index)
	}
	// Every index resolves, known or not. Refusing an unknown one here names
	// the slot and nothing else; letting it resolve and reporting at the call
	// names it *with the arguments the module passed*, which is what any
	// reading of a new slot starts from. The call itself still stops the title.
	return client.stub(svcCategoryJava, index)
}

// handleJavaSVC services one call on the Java interface table. Every one of
// them records what it was handed and then stops the title, because answering
// would be claiming the class metadata was accepted.
func (client *Client) handleJavaSVC(ctx context.Context, thread *armcore.Thread, slot uint32) error {
	argument := func(index int) (uint32, error) {
		if index < 4 {
			return thread.Register(index)
		}
		stack, err := thread.Register(armcore.RegisterSP)
		if err != nil {
			return 0, err
		}
		return client.readWord(stack + uint32(index-4)*4)
	}
	if index, static := javaStaticMethodParts(slot); static {
		if owner, _, unnamed := client.javaLink.unnamedStaticEntry(index); unnamed {
			// The two entries every platform class's run opens with. **Both
			// answer with the class**, and they are the platform-class half of
			// the two thunks an application class carries in its own record:
			// the first is reached before a class's first static call and its
			// answer dropped, the second where the class itself is wanted —
			// handed to the allocator, or read for the name at word 2 of its
			// data, which is what the array-type call is given. Answering the
			// second with an instance is what a first reading did, and it
			// surfaces as an array of a class named at address zero.
			class, err := client.preparePlatformJavaClass(owner)
			if err != nil {
				return fmt.Errorf("%w (the platform class %s: %w)", ErrJavaAppUnsupported, owner, err)
			}
			return thread.SetRegister(0, class.Object)
		}
		// A platform static or special method the module called through the
		// table of addresses this platform filled in. What is implemented is in
		// java_api.go; naming what is not is the whole point of having answered
		// the table before implementing any of it.
		served, err := client.callJavaPlatformStatic(ctx, thread, index)
		if served {
			if err != nil {
				return fmt.Errorf("%w (%w)", ErrJavaAppUnsupported, err)
			}
			return nil
		}
		return fmt.Errorf("%w (it called %s%s)",
			ErrJavaAppUnsupported, client.javaLink.describeJavaStaticMethod(index),
			client.describeJavaCallSite(thread))
	}
	if slot&javaSlotVirtual != 0 {
		// A dispatch through a vtable slot of a platform class. What is
		// implemented is in java_api.go; the rest names the slot rather than
		// branching into a zero.
		served, err := client.callJavaPlatformVirtual(ctx, thread, slot)
		if served {
			if err != nil {
				return fmt.Errorf("%w (%w)", ErrJavaAppUnsupported, err)
			}
			return nil
		}
		return fmt.Errorf("%w (it dispatched through %s%s, with %s; %s)",
			ErrJavaAppUnsupported, client.describeJavaVirtualSlot(slot),
			client.describeJavaCallSite(thread),
			formatWords(registerWords(thread, 4)), client.describeJavaWords(registerWords(thread, 4)))
	}
	count, known := javaSVCArguments[slot]
	if !known {
		// An auxiliary table's one function. How many arguments it takes is
		// not known, so the four a call can pass in registers are recorded
		// and the rest, if there are any, are not invented.
		count = 4
	}
	values := make([]uint32, count)
	for index := range values {
		value, err := argument(index)
		if err != nil {
			return err
		}
		values[index] = value
	}
	name := fmt.Sprintf("%#x", slot)
	if described, known := javaSVCNames[slot]; known {
		name = fmt.Sprintf("%#x (%s)", slot, described)
	}
	if table, index, auxiliary := javaAuxiliaryParts(slot); auxiliary {
		name = fmt.Sprintf("table %#x function %#x", table, index)
	}
	if client.logger != nil {
		client.logger.Debug("LGT java interface call",
			"function", name,
			"arguments", formatWords(values))
	}
	switch slot {
	case javaSVCClassList:
		// The call that hands the application's own classes over, and its
		// string constants with them. What is read here is what a later
		// attempt has to build objects from; nothing runs on it yet.
		if err := client.takeJavaClassList(values[0], values[1]); err != nil {
			return fmt.Errorf("%w (the class list: %w)", ErrJavaAppUnsupported, err)
		}
		// The answer is the handle the module keeps and hands back to the
		// resolve call. Its value is this platform's to choose; the class
		// list's own address is the one thing that is certainly unique.
		return thread.SetRegister(0, values[0])
	case javaSVCLoadClasses:
		surface, err := client.readJavaSurface(values)
		if err != nil {
			return fmt.Errorf("%w (the platform class table: %w)", ErrJavaAppUnsupported, err)
		}
		if client.logger != nil {
			for _, line := range describeJavaSurface(surface) {
				client.logger.Debug("LGT java platform class", "record", line)
			}
		}
		layout := newJavaLayout()
		if err := client.linkJavaSurface(surface, layout); err != nil {
			return fmt.Errorf("%w (answering the platform class table: %w)",
				ErrJavaAppUnsupported, err)
		}
		client.javaLink = &javaLink{surface: surface, layout: layout}
		return thread.SetRegister(0, 0)
	case javaSVCDefineClass:
		if err := client.defineJavaClass(values); err != nil {
			return fmt.Errorf("%w (laying a class out: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, 0)
	case javaSVCPrepareClass:
		class, err := client.prepareJavaClass(ctx, thread, values[0])
		if err != nil {
			return fmt.Errorf("%w (preparing a class: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, class.Object)
	case javaSVCResolveClass:
		class, err := client.prepareJavaClass(ctx, thread, values[0])
		if err != nil {
			return fmt.Errorf("%w (resolving a class: %w)", ErrJavaAppUnsupported, err)
		}
		if err := client.checkJavaClassObject(class); err != nil {
			return fmt.Errorf("%w (resolving a class: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, class.Object)
	case javaSVCInitializeClass:
		if err := client.initializeJavaClass(ctx, thread, values[0], values[1]); err != nil {
			return fmt.Errorf("%w (initialising a class: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, 0)
	case javaSVCResolveArray:
		class, err := client.resolveJavaArrayType(values[0], values[1], values[2])
		if err != nil {
			return fmt.Errorf("%w (an array type: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, class.Object)
	case javaSVCAllocateArray:
		array, err := client.allocateJavaArray(values[0], values[1])
		if err != nil {
			return fmt.Errorf("%w (allocating an array: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, array)
	case javaSVCAllocateArrayN:
		array, err := client.allocateJavaMultiArray(values[0], values[1], values[2])
		if err != nil {
			return fmt.Errorf("%w (allocating an array of arrays: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, array)
	case javaSVCMonitorEnter:
		if err := client.javaMonitorEnter(ctx, values[0]); err != nil {
			return fmt.Errorf("%w (taking a lock: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, 0)
	case javaSVCMonitorExit:
		if err := client.javaMonitorExit(values[0]); err != nil {
			return fmt.Errorf("%w (giving a lock back: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, 0)
	case javaSVCStoreReference, javaSVCStoreReferenceAlso:
		if err := client.storeJavaArrayReference(values[0], values[1], values[2]); err != nil {
			return fmt.Errorf("%w (storing into a reference array: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, 0)
	case javaSVCStoreWide:
		if err := client.storeJavaArrayWide(values[0], values[1], values[2], values[3]); err != nil {
			return fmt.Errorf("%w (storing into a long array: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, 0)
	case javaSVCLoadWide:
		low, high, err := client.loadJavaArrayWide(values[0], values[1])
		if err != nil {
			return fmt.Errorf("%w (reading from a long array: %w)", ErrJavaAppUnsupported, err)
		}
		if err := thread.SetRegister(1, high); err != nil {
			return err
		}
		return thread.SetRegister(0, low)
	case javaSVCAllocate:
		object, err := client.allocateJavaInstance(values[0])
		if err != nil {
			return fmt.Errorf("%w (allocating an instance: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, object)
	case javaSVCStringConstant:
		object, err := client.takeJavaStringConstant(values[1], values[2], values[3])
		if err != nil {
			return fmt.Errorf("%w (a string constant: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, object)
	case javaSVCEnterMethod, javaSVCLeaveMethod:
		// A method enters through one and leaves through the other, carrying a
		// small constant in. Nothing reads the answer, and what the pair is for
		// — a frame the collector can walk is the guess — does not have to be
		// known to let a method run.
		return thread.SetRegister(0, 0)
	case javaSVCEnterTry:
		buffer, err := client.enterJavaTry()
		if err != nil {
			return fmt.Errorf("%w (entering a try region: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, buffer)
	case javaSVCLeaveTry:
		if err := client.leaveJavaTry(); err != nil {
			return fmt.Errorf("%w (leaving a try region: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, 0)
	case javaSVCThrow:
		return javaThrowDelivered(client.throwJavaObject(thread, values[0]))
	case javaSVCTypeCheck:
		matches, err := client.javaTypeCheck(values[0], values[1])
		if err != nil {
			return fmt.Errorf("%w (a type check: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, matches)
	case javaSVCStringClass:
		class, err := client.preparePlatformJavaClass(javaStringClass)
		if err != nil {
			return fmt.Errorf("%w (the String class: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, class.Object)
	case javaSVCStringArrayType:
		class, err := client.javaArrayType(1, "L"+javaStringClass+";", 4)
		if err != nil {
			return fmt.Errorf("%w (the String[] array type: %w)", ErrJavaAppUnsupported, err)
		}
		return thread.SetRegister(0, class.Object)
	case javaSVCThrowNull:
		return javaThrowDelivered(client.throwJavaPlatform(thread, javaThrowNullClass, ""))
	case javaSVCThrowArrayIndex:
		return javaThrowDelivered(client.throwJavaPlatform(thread, javaThrowArrayClass,
			fmt.Sprintf(": index %d of %d", int32(values[0]), int32(values[1]))))
	case javaSVCThrowArithmetic:
		return javaThrowDelivered(
			client.throwJavaPlatform(thread, javaThrowArithmeticClass, ": division by zero"))
	case javaSVCInterfaceTable:
		answer, err := client.javaInterfaceTable(values[0], values[1])
		if err != nil {
			return fmt.Errorf("%w (%w%s)", ErrJavaAppUnsupported, err, client.describeJavaCallSite(thread))
		}
		return thread.SetRegister(0, answer)
	case javaSVCInvokeStaticMain:
		// The module asks for the platform's Jlet launcher and hands it the
		// name of the application's own Jlet. What that takes is in
		// java_launcher.go; what it reports when it cannot get there is which
		// class it found and how far into it the title ran.
		entry, _ := client.readCString(values[0])
		if err := client.runJavaLauncher(ctx, thread, values[2], values[3]); err != nil {
			return fmt.Errorf("%w (the %s launcher: %w — %s)",
				ErrJavaAppUnsupported, entry, err, client.describeJavaLauncher(values[2], values[3]))
		}
		return thread.SetRegister(0, 0)
	}
	if _, known := javaSVCArguments[slot]; !known {
		if _, _, auxiliary := javaAuxiliaryParts(slot); !auxiliary {
			// A slot no reading of this table covers. It is reported with the
			// four words a call can pass in registers, because that is what
			// tells one candidate meaning from another — and with the address
			// it was called from, because the stub returns through `lr`, so
			// that word is the call site in the module itself. Reading the code
			// there is what settles what the arguments mean.
			return fmt.Errorf("%w (java interface function %#x with %s%s; %s)",
				ErrJavaAppUnsupported, slot, formatWords(values),
				client.describeJavaCallSite(thread), client.describeJavaWords(values))
		}
	}
	// The rest are accepted. They are registrations, and refusing one stops
	// the title before it has said what it would have registered — which is
	// the whole of what is known about this table so far.
	return thread.SetRegister(0, 0)
}

// asJavaFailure reports a Java title's failure as one. Startup carries on past
// the metadata calls so the table layouts can be read out of a real title, and
// what stops it after that is whatever the Java runtime asks for next — a
// stdlib slot no Clet uses, so far. Reporting that slot on its own would name
// a symptom; the cause is that this is a Java app.
func (client *Client) asJavaFailure(err error) error {
	if err == nil || !client.javaApplication || errors.Is(err, ErrJavaAppUnsupported) {
		return err
	}
	return fmt.Errorf("%w (it stopped at: %w)", ErrJavaAppUnsupported, err)
}

// describeJavaCallSite answers where in the module a Java call came from.
// **Every one of these stubs returns through `lr`**, so that register holds the
// instruction after the call, and reading the module there is what says how
// many arguments a call set and what it does with the answer — the only
// evidence there is for a slot this platform has no name for.
func (client *Client) describeJavaCallSite(thread *armcore.Thread) string {
	site, err := thread.Register(armcore.RegisterLR)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(", from %#x", site)
}

// registerWords reads the first registers a call passes its arguments in, for
// a report that has no descriptor to say how many there are.
func registerWords(thread *armcore.Thread, count int) []uint32 {
	values := make([]uint32, 0, count)
	for index := 0; index < count; index++ {
		value, err := thread.Register(index)
		if err != nil {
			break
		}
		values = append(values, value)
	}
	return values
}

// describeCallWords is describeJavaWords over the registers a call arrived
// with. **A slot with no name is settled by what its arguments are**, and that
// question is asked the same way on both sides of the platform: the C library
// slots reach this platform through the same stubs and are as unnamed, so a
// title that stops on one leaves the same evidence and gets the same report.
func (client *Client) describeCallWords(thread *armcore.Thread, count int) string {
	return client.describeJavaWords(registerWords(thread, count))
}

// describeJavaWords names the class of every argument that turns out to be an
// object this platform issued. **What a word is, is what settles a slot**: a
// call that takes an array and an index reads the same as one that takes an
// object and a field number until the first argument is asked what it is.
func (client *Client) describeJavaWords(values []uint32) string {
	parts := make([]string, 0, len(values))
	for index, value := range values {
		if described, ok := client.describeJavaWord(value); ok {
			parts = append(parts, fmt.Sprintf("argument %d is %s", index, described))
		}
	}
	if len(parts) == 0 {
		return "none of the arguments is an object issued here"
	}
	return strings.Join(parts, ", ")
}

// describeJavaWord says what one argument word is. **An argument with no name
// is what leaves an unimplemented slot unreadable**, and the words a reference
// points at are usually enough to name it: a slot handed a class record is a
// different call from one handed a string, and a report that says only "not an
// object issued here" cannot tell them apart. So a word that is not a bound
// object is followed — as text, and otherwise as the first few words it points
// at, with those named the same way one level down.
func (client *Client) describeJavaWord(value uint32) (string, bool) {
	if class, known := client.javaClassOfObject(value); known {
		return "a " + class.Name, true
	}
	if text, ok := client.readPrintableString(value); ok {
		return fmt.Sprintf("the text %q", text), true
	}
	words := make([]string, 0, 4)
	for offset := uint32(0); offset < 4; offset++ {
		word, err := client.readWord(value + offset*4)
		if err != nil {
			break
		}
		described := fmt.Sprintf("%#x", word)
		if text, ok := client.readPrintableString(word); ok {
			described = fmt.Sprintf("%#x->%q", word, text)
		} else if class, known := client.javaClassOfObject(word); known {
			described = fmt.Sprintf("%#x->a %s", word, class.Name)
		}
		words = append(words, described)
	}
	if len(words) == 0 {
		return "", false
	}
	return fmt.Sprintf("a reference to [%s]", strings.Join(words, " ")), true
}

func formatWords(values []uint32) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = fmt.Sprintf("%#x", value)
	}
	return strings.Join(parts, " ")
}

// readPrintableString answers the printable ASCII string at an address, when
// that is what is there. A class table is mostly names, so this is what tells
// a name column from a count column.
func (client *Client) readPrintableString(pointer uint32) (string, bool) {
	if pointer < 0x1000 {
		return "", false
	}
	text, err := client.readCString(pointer)
	if err != nil || text == "" || len(text) > 128 {
		return "", false
	}
	for _, symbol := range []byte(text) {
		if symbol < 0x20 || symbol > 0x7e {
			return "", false
		}
	}
	return text, true
}

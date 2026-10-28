package lgt

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/lang/Object`'s own methods, and the three questions every class answers
// through them: hashCode, equals and toString.
//
// **The three are virtual all the way down.** A title's own class overrides
// them in its dispatch table, a library class overrides them in the class
// library, and a class that overrides nothing answers with Object's: its
// identity, and its class name with that identity's hash. So when the platform
// needs one of the answers itself — a Hashtable hashing a key, a Vector looking
// for an element, a StringBuffer appending an object — it asks the way a call
// site in the title would, through the receiver's own slot (javaCallVirtual).
//
// Object's own forms answer only for a class that overrides nothing. A library
// class that declares its own form this platform does not implement — a
// Date's toString, a Thread's — is refused by name instead: "java.util.Date@3f0c"
// where a title expected a date is a plausible wrong answer, and a stop that
// names the gap is worth more (java_api.go). The one exception is the label
// an append or a valueOf builds, which keeps the name it had (javaToStringOf).

const javaObjectClass = "java/lang/Object"

// The slots Object gives the three, which every class inherits. They are
// Object's declaration order — getClass, hashCode, equals, toString — from the
// getClass at 1 and the toString at 4 that call sites settled; see
// javaBakedVirtualSlots.
const (
	javaHashCodeSlot uint32 = 2
	javaEqualsSlot   uint32 = 3
	javaToStringSlot uint32 = 4
)

// javaLibraryOverrides is which of the three the class library declares on
// its own classes, as the specification's class pages list them. A class
// below one of these inherits the declaration, so the walk in
// javaObjectDefaultFor goes up the chain. No `org/kwis` class declares any of
// the three.
var javaLibraryOverrides = map[string]map[string]bool{
	"java/io/ByteArrayOutputStream": {"toString": true},
	"java/lang/Boolean":             {"equals": true, "hashCode": true, "toString": true},
	"java/lang/Byte":                {"equals": true, "hashCode": true, "toString": true},
	"java/lang/Character":           {"equals": true, "hashCode": true, "toString": true},
	"java/lang/Class":               {"toString": true},
	"java/lang/Double":              {"equals": true, "hashCode": true, "toString": true},
	"java/lang/Float":               {"equals": true, "hashCode": true, "toString": true},
	"java/lang/Integer":             {"equals": true, "hashCode": true, "toString": true},
	"java/lang/Long":                {"equals": true, "hashCode": true, "toString": true},
	"java/lang/Short":               {"equals": true, "hashCode": true, "toString": true},
	"java/lang/String":              {"equals": true, "hashCode": true, "toString": true},
	"java/lang/StringBuffer":        {"toString": true},
	"java/lang/Thread":              {"toString": true},
	"java/lang/Throwable":           {"toString": true},
	"java/util/Calendar":            {"equals": true},
	"java/util/Date":                {"equals": true, "hashCode": true, "toString": true},
	"java/util/GregorianCalendar":   {"equals": true, "hashCode": true},
	"java/util/Hashtable":           {"toString": true},
	"java/util/SimpleTimeZone":      {"equals": true, "hashCode": true},
	"java/util/TimeZone":            {"toString": true},
	"java/util/Vector":              {"toString": true},
}

// javaIdentityHash is an object's identity hash: its address. Nothing here
// moves an object, so the address is the object's for as long as it lives,
// which is all the language asks of an identity hash; a checkpoint restores
// every object where it was.
func javaIdentityHash(object uint32) uint32 { return object }

// javaObjectDefaultFor refuses one of Object's forms for an object whose class
// is below a library class that declares its own. A class the title wrote is
// stepped over: a form it overrode would have been its own code in the slot,
// and this is only reached when the slot still holds the stub.
func (client *Client) javaObjectDefaultFor(object uint32, member string) error {
	class, known := client.javaClassOfObject(object)
	if !known {
		return nil
	}
	for step := class; step != nil; step = client.javaSuperOf(step) {
		if step.Handle != 0 {
			continue
		}
		if javaLibraryOverrides[step.Name][member] {
			return fmt.Errorf("%s declares its own %s, which this platform does not implement",
				step.Name, member)
		}
	}
	return nil
}

// javaObjectHashCode is `Object.hashCode()`, slot 2: the identity hash.
func javaObjectHashCode(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if err := client.javaObjectDefaultFor(arguments[0], "hashCode"); err != nil {
		return 0, err
	}
	return javaIdentityHash(arguments[0]), nil
}

// javaObjectEquals is `Object.equals(Object)`, slot 3: the same object, and
// nothing else.
func javaObjectEquals(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if err := client.javaObjectDefaultFor(arguments[0], "equals"); err != nil {
		return 0, err
	}
	if arguments[0] == arguments[1] {
		return javaTrue, nil
	}
	return javaFalse, nil
}

// javaObjectToString is `Object.toString()`, slot 4, which the language defines
// as `getClass().getName() + "@" + Integer.toHexString(hashCode())` — the
// receiver's own hashCode, so a class that overrides only that one shows its
// own number.
func javaObjectToString(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if err := client.javaObjectDefaultFor(arguments[0], "toString"); err != nil {
		return 0, err
	}
	class, known := client.javaClassOfObject(arguments[0])
	if !known {
		return 0, fmt.Errorf("the object at %#x was not issued here", arguments[0])
	}
	hash, err := client.javaCallVirtual(ctx, thread, arguments[0], javaHashCodeSlot)
	if err != nil {
		return 0, err
	}
	return client.newJavaString(fmt.Sprintf("%s@%x", javaDottedName(class.Name), hash))
}

// javaDottedName is a class name the way `Class.getName` gives it: with the
// dots the source wrote rather than the slashes a record carries.
func javaDottedName(name string) string { return strings.ReplaceAll(name, "/", ".") }

// javaObjectWaitNanos is `Object.wait(long, int)`, slot 8, which the language
// defines on `wait(long)`: the nanoseconds round the milliseconds up by one
// when they are half a millisecond or more, or when they are all there is.
func javaObjectWaitNanos(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	milliseconds := int64(uint64(arguments[1]) | uint64(arguments[2])<<32)
	nanoseconds := int32(arguments[3])
	if milliseconds < 0 || nanoseconds < 0 || nanoseconds > 999999 {
		return 0, client.throwJavaPlatform(thread, javaIllegalArgumentClass,
			fmt.Sprintf(": wait(%d, %d)", milliseconds, nanoseconds))
	}
	if nanoseconds >= 500000 || nanoseconds != 0 && milliseconds == 0 {
		milliseconds++
	}
	if milliseconds > int64(time.Hour/time.Millisecond) {
		return 0, fmt.Errorf("a wait of %d milliseconds", milliseconds)
	}
	return client.waitOnJavaObject(ctx, arguments[0], time.Duration(milliseconds)*time.Millisecond)
}

// javaCallVirtual calls one of an object's virtual methods the way a call site
// in the title does: through the slot in the receiver's own dispatch table.
//
// A slot the title filled is its own code, run below this call. A slot that
// still holds one of this platform's stubs is answered here, by the method the
// stub would have reached, without a round trip through the guest — the
// answer is the same and the platform's own call is not entered twice.
//
// **An exception the title's code throws and does not catch is the title's**,
// thrown on from the platform call this one is made inside
// (rethrowFromCallback), so a caller stops on any error and hands it back.
//
// **The answers made here nest without bound otherwise.** A vector that holds
// itself, or two tables that hold each other, ask their own toString for
// ever, and every step of that is a Go frame rather than guest stack: the
// class library would run out of stack and throw StackOverflowError, and here
// the depth is counted instead (maxJavaVirtualNesting). A step through the
// title's own code spends guest stack, which ends a recursion of its own.
func (client *Client) javaCallVirtual(
	ctx context.Context, thread *armcore.Thread, object, slot uint32, arguments ...uint32,
) (uint32, error) {
	class, known := client.javaClassOfObject(object)
	if !known {
		return 0, fmt.Errorf("the object at %#x was not issued here", object)
	}
	if class.VTable == 0 || slot >= class.Slots {
		return 0, fmt.Errorf("%s has no vtable slot %d", class.Name, slot)
	}
	entry, err := client.readWord(class.VTable + 4 + slot*4)
	if err != nil {
		return 0, err
	}
	words := append([]uint32{object}, arguments...)
	if stub, ok := client.javaStubSlot(entry); ok {
		dispatch, implemented := client.javaPlatformVirtualDispatch(stub)
		if !implemented {
			return 0, fmt.Errorf("%s is not implemented", client.describeJavaVirtualSlot(stub))
		}
		if dispatch.Method.Words != len(words) {
			return 0, fmt.Errorf("%s.%s takes %d words, called with %d",
				dispatch.Owner, dispatch.Called, dispatch.Method.Words, len(words))
		}
		if client.javaVirtualNesting >= maxJavaVirtualNesting {
			return 0, fmt.Errorf("%s.%s nested %d calls deep, past the stack the class library would have",
				dispatch.Owner, dispatch.Called, client.javaVirtualNesting)
		}
		client.javaVirtualNesting++
		defer func() { client.javaVirtualNesting-- }()
		return dispatch.Method.Implementat(client, ctx, thread, words)
	}
	if entry == 0 {
		return 0, fmt.Errorf("%s vtable slot %d is empty", class.Name, slot)
	}
	answer, err := client.callOn(ctx, thread, entry, words)
	if err != nil {
		return 0, client.rethrowFromCallback(thread,
			fmt.Errorf("run %s vtable slot %d at %#x: %w", class.Name, slot, entry, err))
	}
	return answer, nil
}

// maxJavaVirtualNesting bounds how deep javaCallVirtual's own answers nest. It
// is far past a structure a title builds — a table of vectors of tables — and
// far short of what the Go stack holds.
const maxJavaVirtualNesting = 256

// javaLacksLibraryForm reports whether an object's slot reaches Object's own
// form for a member its library class declares for itself and this platform
// does not implement — the answer javaObjectDefaultFor refuses.
func (client *Client) javaLacksLibraryForm(object, slot uint32, member string) bool {
	class, known := client.javaClassOfObject(object)
	if !known || class.VTable == 0 || slot >= class.Slots {
		return false
	}
	entry, err := client.readWord(class.VTable + 4 + slot*4)
	if err != nil {
		return false
	}
	stub, ok := client.javaStubSlot(entry)
	if !ok {
		return false
	}
	dispatch, implemented := client.javaPlatformVirtualDispatch(stub)
	return implemented && dispatch.Owner == javaObjectClass && client.javaObjectDefaultFor(object, member) != nil
}

// javaStubSlot answers the slot a dispatch-table entry stands for when the
// entry is one of this platform's own stubs, which is what a slot nothing
// overrode still holds. The stub's shape is the one stub writes: the category
// in the SVC, and the slot in the literal after it.
func (client *Client) javaStubSlot(entry uint32) (uint32, bool) {
	address := entry &^ 1
	if address < platformCodeBase || uint64(address)+16 > uint64(platformCodeBase)+platformCodeSize {
		return 0, false
	}
	code := make([]byte, 16)
	if err := client.core.Memory().Read(address, code); err != nil {
		return 0, false
	}
	if code[8] != byte(svcCategoryJava) || code[9] != 0xdf {
		return 0, false
	}
	slot := binary.LittleEndian.Uint32(code[12:])
	if slot&javaSlotVirtual == 0 {
		return 0, false
	}
	return slot, true
}

// javaHashCodeOf is an object's own hashCode, which is what a hash table files
// it under.
func (client *Client) javaHashCodeOf(
	ctx context.Context, thread *armcore.Thread, object uint32,
) (int32, error) {
	if _, known := client.javaClassOfObject(object); !known {
		// A word that is no object issued here has nothing but its identity.
		return int32(javaIdentityHash(object)), nil
	}
	hash, err := client.javaCallVirtual(ctx, thread, object, javaHashCodeSlot)
	return int32(hash), err
}

// javaEqualsOf is `object.equals(other)`, asked of the first object as the
// class library asks it: a Vector asks the object being looked for, a
// Hashtable the key it already holds.
func (client *Client) javaEqualsOf(
	ctx context.Context, thread *armcore.Thread, object, other uint32,
) (bool, error) {
	if object == other {
		return true, nil
	}
	if _, known := client.javaClassOfObject(object); !known {
		return false, nil
	}
	answer, err := client.javaCallVirtual(ctx, thread, object, javaEqualsSlot, other)
	return answer != 0, err
}

// javaToStringOf is `String.valueOf(object)`: "null" for null, and otherwise
// the object's own toString.
func (client *Client) javaToStringOf(
	ctx context.Context, thread *armcore.Thread, object uint32,
) (string, error) {
	if object == 0 {
		return "null", nil
	}
	// A String is its own text and a StringBuffer's toString is a copy of
	// its, and nothing else keeps text here, so this is both their answers.
	if text, ok := client.javaText(object); ok {
		return text, nil
	}
	if _, known := client.javaClassOfObject(object); !known {
		return client.javaObjectText(object), nil
	}
	// **A library class whose own toString this platform lacks is named the
	// way every object was before toString was asked at all** (javaObjectText),
	// rather than stopping the title: what an append or a valueOf builds is a
	// label, mostly on its way to a log line, and a Thread subclass printing
	// itself is a title that ran before. A call the title makes to the method
	// itself is still refused by name.
	if client.javaLacksLibraryForm(object, javaToStringSlot, "toString") {
		return client.javaObjectText(object), nil
	}
	answer, err := client.javaCallVirtual(ctx, thread, object, javaToStringSlot)
	if err != nil {
		return "", err
	}
	if answer == 0 {
		// A title's own toString may answer null, and the language appends
		// what String.valueOf makes of that.
		return "null", nil
	}
	text, ok := client.javaText(answer)
	if !ok {
		return "", fmt.Errorf("toString answered %#x, which holds no text", answer)
	}
	return text, nil
}

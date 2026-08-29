package lgt

import (
	"errors"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// errJavaThrowHandled says a throw was caught: the guest's whole context is
// already the handler's, and **nothing may touch it after that** — least of all
// the answer register, which is where the jump left the exception object. It
// travels as an error because a throw is not a return: the platform method it
// came out of has no result to give.
var errJavaThrowHandled = errors.New("the throw reached a handler")

// javaThrowDelivered turns that into "the call is done" for a caller that only
// has to stop, and leaves every other failure alone.
func javaThrowDelivered(err error) error {
	if errors.Is(err, errJavaThrowHandled) {
		return nil
	}
	return err
}

// Exceptions, which on this platform are `setjmp` and `longjmp`.
//
// A try region is two calls, and **the second one is not on the Java table at
// all**: `0x1f` answers a buffer and the module hands that buffer straight to
// the C library's `0x32`. The pairing is what says so — 58, 57 and 63 call
// sites of `0x1f` in the three local Java titles, and exactly 58, 57 and 63 of
// `0x32`, with the answer of the first passed to the second at every one of the
// 178 sites. A site reads:
//
//	bl    0x64/0x1f            ; r0 = the buffer for this region
//	bl    0x1/0x32             ; setjmp: 0 now, the exception later
//	cmp   r0, #0
//	bne   <the handler>
//	...                        ; the try body
//	bl    0x64/0x20            ; leave the region
//
// and the handler reads the thrown object's class out of `[r0]` and tests it
// against a catch type, which is what makes the answer of the second return an
// object rather than a code.
//
// The rest of the family throws:
//
//	0x21(object)         throw what the application built
//	0x22(object)         the null check in front of every dereference
//	0x23(index, length)  the bounds check in front of every array access
//	0x25(value)          the divisor check in front of every division
//
// `0x25` is read off its call site: it is called only when a divisor compares
// equal to zero, and the code after it is the `0x80000000 / -1` special case.
const (
	javaSVCEnterTry          uint32 = 0x1f
	javaSVCLeaveTry          uint32 = 0x20
	javaSVCThrow             uint32 = 0x21
	javaSVCThrowArithmetic   uint32 = 0x25
	stdlibSetJump            uint32 = 0x32
	javaClassClass                  = "java/lang/Class"
	javaStringClass                 = "java/lang/String"
	javaThrowNullClass              = "java/lang/NullPointerException"
	javaThrowArrayClass             = "java/lang/ArrayIndexOutOfBoundsException"
	javaThrowArithmeticClass        = "java/lang/ArithmeticException"
	// maxJavaTryDepth bounds how deep try regions nest. A title that opens more
	// than this without leaving one is not nesting, it is leaking.
	maxJavaTryDepth = 256
)

// javaTryFrame is one open try region.
type javaTryFrame struct {
	// Buffer is what the enter call answered and the jump call was given. It is
	// a real address in the platform's own data, because the module keeps it in
	// a local and the platform is the only thing that reads it.
	Buffer uint32
	// Depth is how many nested guest calls deep the region was opened. A jump
	// restores registers, which can only reach a frame the *same* call is still
	// inside; a frame opened outside this one would need the Go stack unwound
	// with it. See throwJava.
	Depth int
	// Saved is where the jump call was made, and Armed says it has been made.
	// The enter call opens the frame; the frame is only a target once the jump
	// call has saved a point to come back to.
	Saved armcore.Context
	Armed bool
}

// enterJavaTry opens a try region and answers the buffer its jump call saves
// into. The buffers are one per depth and reused: a title runs through the same
// try region hundreds of thousands of times, and a buffer allocated per entry
// would be the arena's whole life.
func (client *Client) enterJavaTry() (uint32, error) {
	if len(client.javaTry) >= maxJavaTryDepth {
		return 0, fmt.Errorf("%d try regions are open at once", len(client.javaTry))
	}
	for len(client.javaTryBuffers) <= len(client.javaTry) {
		buffer, err := client.allocateWords(make([]uint32, javaTryBufferWords))
		if err != nil {
			return 0, err
		}
		client.javaTryBuffers = append(client.javaTryBuffers, buffer)
	}
	buffer := client.javaTryBuffers[len(client.javaTry)]
	client.javaTry = append(client.javaTry, javaTryFrame{Buffer: buffer, Depth: client.javaCallDepth})
	return buffer, nil
}

// javaTryBufferWords is how large the buffer is. Nothing in the module reads
// it — the saved point is this platform's, not the guest's — so it is only as
// large as a `jmp_buf` has to look.
const javaTryBufferWords = 16

// leaveJavaTry closes the innermost region. The module emits the call on every
// path out of a try body, so a missing frame is this platform having lost one
// rather than the module having made a mistake.
func (client *Client) leaveJavaTry() error {
	if len(client.javaTry) == 0 {
		return fmt.Errorf("a try region was left that was never entered")
	}
	client.javaTry = client.javaTry[:len(client.javaTry)-1]
	return nil
}

// armJavaTry is the jump call: it saves where the guest is so a throw can come
// back to it, and answers zero the way `setjmp` does on its first return.
func (client *Client) armJavaTry(thread *armcore.Thread, buffer uint32) error {
	for index := len(client.javaTry) - 1; index >= 0; index-- {
		if client.javaTry[index].Buffer != buffer {
			continue
		}
		saved := thread.Context()
		// The point to come back to is where the call would have returned to,
		// which is the link register rather than the stub this is running in.
		if err := saved.SetPC(saved.Registers[armcore.RegisterLR]); err != nil {
			return err
		}
		client.javaTry[index].Saved, client.javaTry[index].Armed = saved, true
		return thread.SetRegister(0, 0)
	}
	return fmt.Errorf("the buffer %#x is not one this platform handed out", buffer)
}

// throwJava unwinds to the innermost armed region, which is a long jump: the
// registers of the saved point come back and the answer is the object, so the
// module's own `cmp r0, #0` after the jump call takes the handler.
//
// **A frame from an outer guest call cannot be jumped to.** Restoring registers
// moves the guest, and this platform's own Go stack stays where it is — the
// call that is waiting for the guest would return into a frame that has already
// been left. Such a throw is reported with its class instead, which names both
// the exception and the fact that it crossed a platform call.
func (client *Client) throwJava(thread *armcore.Thread, class string, object uint32, what string) error {
	for len(client.javaTry) > 0 {
		frame := client.javaTry[len(client.javaTry)-1]
		if !frame.Armed || frame.Depth != client.javaCallDepth {
			break
		}
		client.javaTry = client.javaTry[:len(client.javaTry)-1]
		if client.logger != nil {
			client.logger.Debug("LGT java exception caught",
				"class", class, "object", object, "resume", frame.Saved.PC())
		}
		if err := thread.SetContext(frame.Saved); err != nil {
			return err
		}
		if err := thread.SetRegister(0, object); err != nil {
			return err
		}
		return errJavaThrowHandled
	}
	if client.logger != nil {
		client.logger.Debug("LGT java throw is uncaught", "class", class,
			"open", len(client.javaTry), "depth", client.javaCallDepth)
	}
	return &javaUncaughtThrow{Class: class, What: what}
}

// javaUncaughtThrow is an exception with nothing of the application's own left
// to catch it. It is told apart from every other way a Java title can stop
// because it is the one that is not a defect in this platform: the guest did
// what it was written to do and the frame that would have caught it is not on
// this stack. A callback ends on it; see absorbUncaughtCallback.
//
// It still unwraps to ErrJavaAppUnsupported, because everything that reports
// on an LGT Java title stopping reads that.
type javaUncaughtThrow struct {
	Class string
	What  string
}

// The sentinel's own words are not repeated here: every path that reports one
// of these wraps it in ErrJavaAppUnsupported on the way out, and a message that
// said "LGT Java apps are not supported" twice was what the first one did.
func (throw *javaUncaughtThrow) Error() string {
	return fmt.Sprintf("the application threw %s%s", throw.Class, throw.What)
}

func (throw *javaUncaughtThrow) Unwrap() error { return ErrJavaAppUnsupported }

// throwJavaPlatform throws one of the exceptions the platform itself raises:
// the object is this platform's to build, and it is an instance of the class
// the check names so the catch test has something to compare.
func (client *Client) throwJavaPlatform(thread *armcore.Thread, name, what string) error {
	class, err := client.preparePlatformJavaClass(name)
	if err != nil {
		return fmt.Errorf("%w (%s: %w)", ErrJavaAppUnsupported, name, err)
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return fmt.Errorf("%w (%s: %w)", ErrJavaAppUnsupported, name, err)
	}
	return client.throwJava(thread, name, object, what)
}

// throwJavaObject throws what the application built. Its class is whatever it
// is; what is reported if nothing catches it is the class of the object, which
// this platform can only name when it issued it.
func (client *Client) throwJavaObject(thread *armcore.Thread, object uint32) error {
	name := fmt.Sprintf("an object at %#x", object)
	if class, ok := client.javaClassOfObject(object); ok {
		name = class.Name
	}
	return client.throwJava(thread, name, object, "")
}

// javaClassOfObject answers the class of an object this platform allocated, by
// the vtable its first word points at.
func (client *Client) javaClassOfObject(object uint32) (*javaRuntimeClass, bool) {
	if object == 0 || client.javaRun == nil {
		return nil, false
	}
	table, err := client.readWord(object)
	if err != nil {
		return nil, false
	}
	for _, class := range client.javaRun.byHandle {
		if class.VTable == table {
			return class, true
		}
	}
	for _, class := range client.javaRun.byName {
		if class.VTable == table {
			return class, true
		}
	}
	return nil, false
}

// dropJavaTryFrames closes the regions a returning guest call left open. A
// frame is only a target for as long as the call that opened it is running, so
// one that outlives its call is one this platform must not jump to later.
func (client *Client) dropJavaTryFrames(depth int) {
	for len(client.javaTry) > 0 && client.javaTry[len(client.javaTry)-1].Depth > depth {
		client.javaTry = client.javaTry[:len(client.javaTry)-1]
	}
}

// javaTypeCheck answers `instanceof` and the catch-clause test, which are the
// same question: is this class the named one, or below it?
//
// **The two arguments are not the same kind of thing.** The first is the word
// in front of an object's vtable — a class record's handle for a class the
// module declares, and the class object for one of the platform's — and the
// second says which class to test against. So the call is "the class of the
// object I have, against the class the source wrote", and it is answered by
// walking the first one's chain.
//
// **What the second argument is, is not fixed either.** Usually it is a name,
// taken out of word 2 of another class object's data, which is where the
// array-type call reads one too. But a title testing an element against an
// array type resolves that type first — `new char[]`'s own type object — and
// passes *the class object itself*, which reads as no name at all: one site
// dispatches `firstElement()`, takes the element's class out of its vtable,
// resolves `[C` and asks this call about the two, with the same word in both
// registers. So a second argument this platform issued as a class is taken as
// one, and only a word that is no class of its own is read as a name.
func (client *Client) javaTypeCheck(identity, name uint32) (uint32, error) {
	class, ok := client.javaClassOfIdentity(identity)
	if !ok {
		return 0, fmt.Errorf("the class %#x was not issued here", identity)
	}
	wanted, ok := "", false
	if against, isClass := client.javaClassOfIdentity(name); isClass {
		wanted, ok = against.Name, true
	} else {
		wanted, ok = client.readPrintableString(name)
	}
	if !ok {
		return 0, fmt.Errorf("the name at %#x is not one", name)
	}
	if client.logger != nil {
		client.logger.Debug("LGT java type check", "class", class.Name, "against", wanted)
	}
	for step := class; step != nil; step = client.javaSuperOf(step) {
		if step.Name == wanted {
			return 1, nil
		}
	}
	return 0, nil
}

// javaClassOfIdentity resolves the word a vtable carries in front of slot 0.
func (client *Client) javaClassOfIdentity(identity uint32) (*javaRuntimeClass, bool) {
	if identity == 0 || client.javaRun == nil {
		return nil, false
	}
	if class, ok := client.javaRun.byHandle[identity]; ok {
		return class, true
	}
	class, ok := client.javaRun.byObject[identity]
	return class, ok
}

// javaSuperOf walks one step up. A class the module declares carries its
// superclass; a platform class's chain is the specification's, which is what
// javaPlatformSupers holds, and a class not in it is rooted at Object.
func (client *Client) javaSuperOf(class *javaRuntimeClass) *javaRuntimeClass {
	if class.Super != nil {
		return class.Super
	}
	if class.Handle != 0 || class.Name == "java/lang/Object" {
		return nil
	}
	super, err := client.preparePlatformJavaClass(javaPlatformSuper(class.Name))
	if err != nil {
		return nil
	}
	class.Super = super
	return super
}

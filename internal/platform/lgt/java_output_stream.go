package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/io/OutputStream`'s own bodies: what a title's own OutputStream
// subclass inherits.
//
// A title that writes a stream of its own overrides `write(int)`, the one
// method the class declares abstract, and calls the rest on itself. The class
// library defines `write(byte[])` over the receiver's `write(byte[], int,
// int)`, and that over the receiver's `write(int)`, one byte at a time, so each
// is asked through the receiver's slot (javaCallVirtual) and the title's own
// code answers; `flush` and `close` do nothing. The slots are the ones
// ByteArrayOutputStream and DataOutputStream serve and inherit from this class:
// `write(int)` 10, `write(byte[])` 11, `write(byte[], int, int)` 12, `flush` 13
// and `close` 14 — the class page's order from 10.
//
// **These answer only for a class that has no form of its own.** Both of this
// platform's sinks serve every OutputStream method they declare, but
// PrintStream declares its own `write(int)`, `write(byte[], int, int)`, `flush`
// and `close` and this platform serves none of them (java_print.go), so a
// dispatch on System.out reaching one of these is refused by name rather than
// answered with the empty body the base class gives — the same rule Object's
// own forms keep (javaObjectDefaultFor).

const javaOutputStreamClass = "java/io/OutputStream"

// The OutputStream slots the bodies here, and a writer handing its bytes to a
// stream (java_writer.go), dispatch through.
const (
	javaOutputStreamSlotWrite      = 10 // write(I)V, which the range write writes each byte through
	javaOutputStreamSlotWriteRange = 12 // write([BII)V
	javaOutputStreamSlotFlush      = 13 // flush()V
	javaOutputStreamSlotClose      = 14 // close()V
)

// javaUnservedOverrides is, for a platform class below OutputStream, which of
// OutputStream's methods its class page declares for itself and this platform
// does not serve on it, by slot.
var javaUnservedOverrides = map[string]map[uint32]string{
	javaPrintStreamClass: {10: "write(I)V", 12: "write([BII)V", 13: "flush()V", 14: "close()V"},
}

// javaOutputStreamBaseFor refuses one of these bodies for an object whose
// class, or a platform class between it and OutputStream, declares its own
// form of the method. A class the title wrote is stepped over: a form it
// declared would have been its own code in the slot, and this is only reached
// when the slot still holds a stub.
func (client *Client) javaOutputStreamBaseFor(object, slot uint32) error {
	class, known := client.javaClassOfObject(object)
	if !known {
		return fmt.Errorf("the object at %#x was not issued here", object)
	}
	for step := class; step != nil && step.Name != javaOutputStreamClass; step = client.javaSuperOf(step) {
		if step.Handle != 0 {
			continue
		}
		if called, own := javaUnservedOverrides[step.Name][slot]; own {
			return fmt.Errorf("%s declares its own %s, which this platform does not implement", step.Name, called)
		}
	}
	return nil
}

// javaOutputStreamBaseWriteAll is `write(byte[])`, slot 11: the whole array,
// through the receiver's own `write(byte[], int, int)`.
func javaOutputStreamBaseWriteAll(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if err := client.javaOutputStreamBaseFor(arguments[0], 11); err != nil {
		return 0, err
	}
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": write of a null array")
	}
	_, length, err := client.javaArrayBlock(arguments[1])
	if err != nil {
		return 0, err
	}
	_, err = client.javaCallVirtual(ctx, thread, arguments[0], javaOutputStreamSlotWriteRange, arguments[1], 0, length)
	return 0, err
}

// javaOutputStreamBaseWriteRange is `write(byte[], int, int)`, slot 12: each
// byte of the run through the receiver's own `write(int)`, in order. A run
// outside the array is the IndexOutOfBoundsException the class library throws,
// and each byte is read from the array as its call comes up, the way the class
// library reads `b[off + i]`, because the title's `write` may change the array
// it is being handed.
func javaOutputStreamBaseWriteRange(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if err := client.javaOutputStreamBaseFor(arguments[0], 12); err != nil {
		return 0, err
	}
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": write of a null array")
	}
	view, err := client.javaArrayElements(arguments[1])
	if err != nil {
		return 0, err
	}
	if view.Width != 1 || view.Reference {
		return 0, fmt.Errorf("bytes written from an array of %d-byte elements", view.Width)
	}
	from, count := int64(int32(arguments[2])), int64(int32(arguments[3]))
	if from < 0 || count < 0 || from+count > int64(view.Length) {
		return 0, client.throwJavaPlatform(thread, javaIndexClass,
			fmt.Sprintf(": %d bytes from %d of an array of %d", count, from, view.Length))
	}
	element := make([]byte, 1)
	for index := from; index < from+count; index++ {
		if err := client.core.Memory().Read(view.Block+javaArrayLengthWords*4+uint32(index), element); err != nil {
			return 0, err
		}
		// A byte is signed, and the int `write(int)` is handed is that byte
		// widened, sign and all.
		if _, err := client.javaCallVirtual(ctx, thread, arguments[0], javaOutputStreamSlotWrite,
			uint32(int32(int8(element[0])))); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

// javaOutputStreamBaseFlush and javaOutputStreamBaseClose are `flush()` and
// `close()`, slots 13 and 14, which the class library gives no body: a stream
// that holds nothing back has nothing to flush or let go of.
func javaOutputStreamBaseFlush(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return 0, client.javaOutputStreamBaseFor(arguments[0], 13)
}

func javaOutputStreamBaseClose(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return 0, client.javaOutputStreamBaseFor(arguments[0], 14)
}

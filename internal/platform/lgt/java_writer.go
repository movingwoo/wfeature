package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/io/Writer` and `java/io/OutputStreamWriter`: characters written as
// bytes into a stream. Neither the KTF nor the SKT runtime has them, so both
// are CLDC 1.1's classes, whole.
//
// **The slots are the declaration-order rule's alone**; nothing has been
// watched calling either class. Writer declares `write(int)`, `write(char[])`,
// `write(char[], int, int)`, `write(String)`, `write(String, int, int)`,
// `flush` and `close`, which take 10 to 16, and OutputStreamWriter overrides
// five of them in place and declares nothing new, so it takes the same seven
// and inherits `write(char[])` and `write(String)` from Writer
// (javaPlatformSupers). TestJavaBakedSlotsFollowTheDeclarationOrder holds both
// to the pages' order.
//
// Writer's bodies are the class library's: every write is defined over the
// receiver's own `write(char[], int, int)` — one character, a whole array, a
// run of a String copied out first — and `write(String)` over the receiver's
// `write(String, int, int)`, each asked through the receiver's slot so a
// title's own Writer subclass answers itself. Writer declares the range write,
// `flush` and `close` abstract, and they have no body here either.
//
// An OutputStreamWriter stands for the stream it was built on, the way a
// DataOutputStream stands for its sink: the stream is held against the writer
// as the object it writes through (the wrapper table, which the collector
// follows and a checkpoint carries), and closing the writer closes the stream
// and lets go of it, after which every call but another `close` is the
// IOException the class library throws for a closed writer. The bytes are
// handed to the stream through its own `write(byte[], int, int)`, so a
// ByteArrayOutputStream, a File's stream and a title's own OutputStream all
// take them the way a write of theirs would. The encoding is a name the
// InputStreamReader beside it accepts (javaCharsetOf) — the handset's own,
// EUC-KR, unless the writer was named UTF-8 — and it is kept against the writer
// in the numbers' table as the charset's number, which a checkpoint carries the
// same way. A character is encoded the way `String.getBytes` encodes it in that
// encoding.
//
// **The two directions do not agree on UTF-8.** The InputStreamReader decodes
// the handset's own encoding whatever it is named, as the KTF and SKT readers
// do (java_reader.go), so bytes a writer named UTF-8 wrote do not read back
// through a reader named UTF-8 on any of the three; `new String(bytes,
// "UTF-8")` reads them.

const (
	javaWriterClass       = "java/io/Writer"
	javaStreamWriterClass = "java/io/OutputStreamWriter"
)

// The Writer slots its own bodies dispatch through, by the rule.
const (
	javaWriterSlotWriteRange     = 12 // write([CII)V
	javaWriterSlotWriteTextRange = 14 // write(Ljava/lang/String;II)V
)

// javaWriterBaseConstructor is `Writer()` and `Writer(Object lock)`, which
// take the lock the way Reader's two constructors do (java_reader.go).
func javaWriterBaseConstructor(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	lock := arguments[0]
	if len(arguments) > 1 {
		if arguments[1] == 0 {
			return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": a Writer locked on null")
		}
		lock = arguments[1]
	}
	return 0, client.setJavaLock(arguments[0], javaWriterClass, lock)
}

// javaWriterBaseWriteChar is Writer's `write(int)`, slot 10: the character the
// low sixteen bits carry, written through the receiver's own range write as an
// array of one.
func javaWriterBaseWriteChar(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	array, err := client.newJavaCharArray([]uint16{uint16(arguments[1])})
	if err != nil {
		return 0, err
	}
	_, err = client.javaCallVirtual(ctx, thread, arguments[0], javaWriterSlotWriteRange, array, 0, 1)
	return 0, err
}

// javaWriterBaseWriteChars is Writer's `write(char[])`, slot 11: the whole
// array, through the receiver's own range write.
func javaWriterBaseWriteChars(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": write of a null array")
	}
	_, length, err := client.javaArrayBlock(arguments[1])
	if err != nil {
		return 0, err
	}
	_, err = client.javaCallVirtual(ctx, thread, arguments[0], javaWriterSlotWriteRange, arguments[1], 0, length)
	return 0, err
}

// javaWriterBaseWriteText is Writer's `write(String)`, slot 13: the whole of
// it, through the receiver's own `write(String, int, int)`.
func javaWriterBaseWriteText(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	text, err := client.javaTextArgument(thread, arguments[1], "write")
	if err != nil {
		return 0, err
	}
	_, err = client.javaCallVirtual(ctx, thread, arguments[0], javaWriterSlotWriteTextRange,
		arguments[1], 0, uint32(javaUnitCount(text)))
	return 0, err
}

// javaWriterBaseWriteTextRange is Writer's `write(String, int, int)`, slot 14:
// the run copied out of the String into an array, the way `getChars` copies it
// — a run outside the String is the StringIndexOutOfBoundsException that throws
// — and the array written through the receiver's own range write.
func javaWriterBaseWriteTextRange(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	units, err := client.javaTextRun(thread, arguments[1], arguments[2], arguments[3], javaStringIndexClass)
	if err != nil {
		return 0, err
	}
	array, err := client.newJavaCharArray(units)
	if err != nil {
		return 0, err
	}
	_, err = client.javaCallVirtual(ctx, thread, arguments[0], javaWriterSlotWriteRange,
		array, 0, uint32(len(units)))
	return 0, err
}

// javaTextRun is the run of a String argument from an offset, refused with the
// named exception when it does not fit inside the String. Null is the
// NullPointerException a call on it throws.
func (client *Client) javaTextRun(
	thread *armcore.Thread, object, offset, count uint32, refusal string,
) ([]uint16, error) {
	text, err := client.javaTextArgument(thread, object, "write")
	if err != nil {
		return nil, err
	}
	units := utf16Units(text)
	from, length := int64(int32(offset)), int64(int32(count))
	if from < 0 || length < 0 || from+length > int64(len(units)) {
		return nil, client.throwJavaPlatform(thread, refusal,
			fmt.Sprintf(": %d characters from %d of a string of %d", length, from, len(units)))
	}
	return units[from : from+length], nil
}

// javaStreamWriterConstructor is `OutputStreamWriter(OutputStream)` and
// `OutputStreamWriter(OutputStream, String enc)`. A null stream is the
// NullPointerException the class library throws before it looks at the name,
// and the name is checked the way InputStreamReader's is.
func javaStreamWriterConstructor(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": OutputStreamWriter over a null stream")
	}
	charset := javaCharsetPlatform
	if len(arguments) > 2 {
		named, err := client.javaCharsetNamed(thread, arguments[2])
		if err != nil {
			return 0, err
		}
		charset = named
	}
	runtime := client.javaRuntimeState()
	runtime.wrapped[arguments[0]] = arguments[1]
	runtime.numbers[arguments[0]] = int64(charset)
	return 0, nil
}

// javaStreamWriterOf answers the stream an open writer writes through and the
// encoding it writes in. A writer that has been closed holds no stream, and
// asking it for one is the IOException the class library throws.
func (client *Client) javaStreamWriterOf(thread *armcore.Thread, writer uint32) (uint32, javaCharset, error) {
	runtime := client.javaRuntimeState()
	stream, open := runtime.wrapped[writer]
	if !open || stream == 0 {
		return 0, 0, client.throwJavaPlatform(thread, javaIOExceptionClass, ": Stream closed")
	}
	charset := javaCharset(runtime.numbers[writer])
	if charset != javaCharsetUTF8 {
		charset = javaCharsetPlatform
	}
	return stream, charset, nil
}

// writeJavaStreamBytes hands encoded bytes to a stream through its own
// `write(byte[], int, int)`, in an array this platform builds for the call.
func (client *Client) writeJavaStreamBytes(
	ctx context.Context, thread *armcore.Thread, stream uint32, data []byte,
) error {
	if len(data) == 0 {
		return nil
	}
	array, err := client.newJavaByteArray(data)
	if err != nil {
		return err
	}
	_, err = client.javaCallVirtual(ctx, thread, stream, javaOutputStreamSlotWriteRange, array, 0, uint32(len(data)))
	return err
}

// writeJavaStreamUnits encodes code units in a writer's encoding and hands the
// bytes to its stream.
func (client *Client) writeJavaStreamUnits(
	ctx context.Context, thread *armcore.Thread, writer uint32, units []uint16,
) error {
	stream, charset, err := client.javaStreamWriterOf(thread, writer)
	if err != nil {
		return err
	}
	return client.writeJavaStreamBytes(ctx, thread, stream, charset.encode(javaTextOfUnits(units)))
}

// javaStreamWriterWriteChar is OutputStreamWriter's `write(int)`, slot 10: the
// character the low sixteen bits carry.
func javaStreamWriterWriteChar(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return 0, client.writeJavaStreamUnits(ctx, thread, arguments[0], []uint16{uint16(arguments[1])})
}

// javaStreamWriterWriteChars is OutputStreamWriter's `write(char[], int,
// int)`, slot 12: a run of the array. The writer is checked open first, then
// the run against the array, which is the IndexOutOfBoundsException the class
// library throws for one outside it.
func javaStreamWriterWriteChars(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, _, err := client.javaStreamWriterOf(thread, arguments[0]); err != nil {
		return 0, err
	}
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": write of a null array")
	}
	units, err := client.readJavaArrayChars(arguments[1])
	if err != nil {
		return 0, err
	}
	from, count := int64(int32(arguments[2])), int64(int32(arguments[3]))
	if from < 0 || count < 0 || from+count > int64(len(units)) {
		return 0, client.throwJavaPlatform(thread, javaIndexClass,
			fmt.Sprintf(": %d characters from %d of an array of %d", count, from, len(units)))
	}
	return 0, client.writeJavaStreamUnits(ctx, thread, arguments[0], units[from:from+count])
}

// javaStreamWriterWriteText is OutputStreamWriter's `write(String, int, int)`,
// slot 14: a run of the String, refused the way the array form refuses one.
func javaStreamWriterWriteText(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, _, err := client.javaStreamWriterOf(thread, arguments[0]); err != nil {
		return 0, err
	}
	units, err := client.javaTextRun(thread, arguments[1], arguments[2], arguments[3], javaIndexClass)
	if err != nil {
		return 0, err
	}
	return 0, client.writeJavaStreamUnits(ctx, thread, arguments[0], units)
}

// javaStreamWriterFlush is `flush()`, slot 15: the stream's own flush.
func javaStreamWriterFlush(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, _, err := client.javaStreamWriterOf(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	_, err = client.javaCallVirtual(ctx, thread, stream, javaOutputStreamSlotFlush)
	return 0, err
}

// javaStreamWriterClose is `close()`, slot 16: the stream's own close, and the
// writer lets go of it. A writer already closed closes nothing, and a close the
// stream refuses leaves the writer open, as the class library's does.
func javaStreamWriterClose(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	stream, open := runtime.wrapped[arguments[0]]
	if !open || stream == 0 {
		return 0, nil
	}
	if _, err := client.javaCallVirtual(ctx, thread, stream, javaOutputStreamSlotClose); err != nil {
		return 0, err
	}
	delete(runtime.wrapped, arguments[0])
	return 0, nil
}

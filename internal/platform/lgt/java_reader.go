package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/io/Reader`'s own bodies: what a title's own Reader subclass inherits,
// and the named-encoding InputStreamReader.
//
// A title that writes a Reader of its own overrides the two methods the class
// declares abstract, `read(char[], int, int)` and `close`, and calls the rest
// on itself: `read()`, `read(char[])`, `skip`, and the mark contract. **Every
// one of those is defined over the receiver's own `read(char[], int, int)`**,
// so each asks it through the receiver's slot (javaCallVirtual) and the title's
// own code answers, the way the class library's bodies call it.
//
// The slots are InputStreamReader's served positions, which it inherits:
// `read()` 10, `read(char[])` 11, `read(char[], int, int)` 12, `skip` 13,
// `ready` 14, `markSupported` 15, `mark` 16, `reset` 17 and `close` 18, the
// class page's own order from 10 (TestJavaBakedSlotsFollowTheDeclarationOrder).
// InputStreamReader serves every one of them itself, so these answer only for
// a class the title wrote.

const javaReaderClass = "java/io/Reader"

const (
	// javaReaderSlotReadRange is `read(char[], int, int)`, the one every body
	// here reads through.
	javaReaderSlotReadRange = 12
	// javaReaderSkipBuffer is the most one round of `skip` asks for at a time:
	// the class library's skip buffer, which it allocates once a reader skips.
	javaReaderSkipBuffer = 8192
	// maxJavaReaderSkipRounds bounds how many rounds of reading one `skip` may
	// take. The class library has no bound, and a reader that never ends skips
	// for ever there; here a round is a guest call made from inside a platform
	// call, and a session that never comes back out of one is a hang. Stopping
	// early answers a count short of the one asked for, which the contract
	// allows — `skip` answers how many it actually skipped.
	maxJavaReaderSkipRounds = 1 << 20
)

// javaReaderBaseConstructor is `Reader()` and `Reader(Object lock)`: the object
// a subclass synchronizes on is itself or the one it was given, and null is the
// NullPointerException the class library throws for it.
func javaReaderBaseConstructor(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	lock := arguments[0]
	if len(arguments) > 1 {
		if arguments[1] == 0 {
			return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": a Reader locked on null")
		}
		lock = arguments[1]
	}
	return 0, client.setJavaLock(arguments[0], javaReaderClass, lock)
}

// setJavaLock publishes the object a Reader or a Writer synchronizes on into
// the word the module was told the class's protected `lock` field sits in. The
// specification declares it protected so a subclass can take it, and one that
// writes `synchronized (lock)` would otherwise lock on null. A module that never
// asked for the field never reads it, and nothing is written (see
// layoutPlatformFields).
func (client *Client) setJavaLock(object uint32, class string, lock uint32) error {
	if client.javaLink == nil || client.javaLink.layout == nil {
		return nil
	}
	word, ok := client.javaLink.layout.platformFieldWord(class, "lock", "Ljava/lang/Object;")
	if !ok {
		return nil
	}
	record, tracked := client.javaRuntimeState().objects[object]
	if !tracked || uint64(word)*4+4 > uint64(record.blockSize) {
		return fmt.Errorf("%s.lock is word %d, past the object at %#x", class, word, object)
	}
	return client.writeWord(record.block+word*4, lock)
}

// javaReaderBaseRead is `read()`, slot 10: one character, read through the
// receiver's own `read(char[], int, int)` into an array of one, or -1 when that
// answered -1.
func javaReaderBaseRead(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	array, err := client.newJavaCharArray(make([]uint16, 1))
	if err != nil {
		return 0, err
	}
	count, err := client.javaCallVirtual(ctx, thread, arguments[0], javaReaderSlotReadRange, array, 0, 1)
	if err != nil {
		return 0, err
	}
	if int32(count) == -1 {
		return ^uint32(0), nil
	}
	units, err := client.readJavaArrayChars(array)
	if err != nil || len(units) != 1 {
		return 0, fmt.Errorf("read(): the array of one the reader filled reads as %d characters: %v", len(units), err)
	}
	return uint32(units[0]), nil
}

// javaReaderBaseReadArray is `read(char[])`, slot 11: the whole array, through
// the receiver's own `read(char[], int, int)`.
func javaReaderBaseReadArray(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": read into a null array")
	}
	_, length, err := client.javaArrayBlock(arguments[1])
	if err != nil {
		return 0, err
	}
	return client.javaCallVirtual(ctx, thread, arguments[0], javaReaderSlotReadRange, arguments[1], 0, length)
}

// javaReaderBaseSkip is `skip(long)`, slot 13: characters read and dropped,
// through the receiver's own `read(char[], int, int)` into a buffer of at most
// javaReaderSkipBuffer, until the count is reached or the reader ends. It
// answers how many it skipped, as a long, and a negative count is the
// IllegalArgumentException the class library throws.
//
// A read that answers nothing, or less than -1, is taken as the end: the
// language has such a read block until it has a character, and a reader that
// keeps answering none would otherwise be asked again for ever.
func javaReaderBaseSkip(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	wanted := int64(uint64(arguments[1]) | uint64(arguments[2])<<32)
	if wanted < 0 {
		return 0, client.throwJavaPlatform(thread, javaIllegalArgumentClass, ": skip value is negative")
	}
	left := wanted
	if wanted > 0 {
		buffer, err := client.newJavaCharArray(make([]uint16, min(wanted, javaReaderSkipBuffer)))
		if err != nil {
			return 0, err
		}
		for round := 0; left > 0 && round < maxJavaReaderSkipRounds; round++ {
			asked := min(left, javaReaderSkipBuffer)
			answer, err := client.javaCallVirtual(ctx, thread, arguments[0], javaReaderSlotReadRange,
				buffer, 0, uint32(asked))
			if err != nil {
				return 0, err
			}
			count := int64(int32(answer))
			if count <= 0 {
				break
			}
			if count > asked {
				return 0, fmt.Errorf("skip: read([CII) answered %d characters when asked for %d", count, asked)
			}
			left -= count
		}
	}
	skipped := uint64(wanted - left)
	if err := thread.SetRegister(1, uint32(skipped>>32)); err != nil {
		return 0, err
	}
	return uint32(skipped), nil
}

// javaReaderBaseReady and javaReaderBaseMarkSupported are `ready()` and
// `markSupported()`, slots 14 and 15: false, which is the class library's
// answer for a reader that says nothing of its own.
func javaReaderBaseReady(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return javaFalse, nil
}

func javaReaderBaseMarkSupported(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return javaFalse, nil
}

// javaReaderBaseMark and javaReaderBaseReset are `mark(int)` and `reset()`,
// slots 16 and 17: the IOException the class library throws from a reader that
// does not support either.
func javaReaderBaseMark(
	client *Client, _ context.Context, thread *armcore.Thread, _ []uint32,
) (uint32, error) {
	return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, ": mark() not supported")
}

func javaReaderBaseReset(
	client *Client, _ context.Context, thread *armcore.Thread, _ []uint32,
) (uint32, error) {
	return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, ": reset() not supported")
}

// javaReaderConstructor is `InputStreamReader(InputStream)`: the reader stands
// for the stream it was built on (javaWrapStream). A null stream is the
// NullPointerException the class library throws for one, and the KTF and SKT
// runtimes too, where the wrapping alone would stop the title on an object it
// cannot name. The check is the reader's own rather than the wrapping's,
// because the class library's DataInputStream constructor, which wraps the
// same way, takes a null and leaves the failure to its first read.
func javaReaderConstructor(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": InputStreamReader over a null stream")
	}
	return javaWrapStream(client, ctx, thread, arguments[:2])
}

// javaReaderConstructorIn is `InputStreamReader(InputStream, String enc)`. The
// encoding is checked the way the KTF and SKT runtimes check it — the names
// they accept, NullPointerException for a null one and
// UnsupportedEncodingException for one they do not know — and then the reader
// is the one-argument form's: it stands for the stream it was built on and
// decodes with the handset's own encoding, which is also what those runtimes'
// readers decode with whatever name they were given.
func javaReaderConstructorIn(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, err := client.javaCharsetNamed(thread, arguments[2]); err != nil {
		return 0, err
	}
	return javaReaderConstructor(client, ctx, thread, arguments[:2])
}

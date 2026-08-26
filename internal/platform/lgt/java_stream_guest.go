package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A stream the title wrote itself, read through the title's own code.
//
// `DataInputStream` and `InputStreamReader` are defined over the abstract
// `java/io/InputStream`, not over a resource this platform opened, and a title
// is entitled to hand either of them one of its own — a decompressor, a
// decrypting filter, a stream over a block it holds. There are no bytes here
// for such an object: they are behind its `read`, which is a virtual call away
// on the class the title compiled.
//
// So the wrapper reads it the way the guest would. Bytes are pulled a block at
// a time into the same buffer a resource stream fills, and the cursor is the
// same field, which is what lets every reader above — `readInt`, `readUTF`,
// `readFully`, the reader's character decode — work unchanged: each one asks
// for the bytes it needs before it looks at them, and asking is a no-op for a
// stream this platform holds whole.
//
// **The methods are found in the object's vtable, not in its class record.** A
// class the compiler laid out itself declares no member records at all — its
// dispatch table is in the image — so a lookup by name and descriptor finds
// nothing on exactly the classes this is for. The slots are `InputStream`'s
// own, which this platform numbers (java_api.go), and a subclass's override
// sits at the number it inherits.

// The `java/io/InputStream` vtable slots a title's own stream may override.
// They are the numbers this platform assigned the class, so a subclass's
// override of one is at the same slot.
const (
	javaStreamSlotRead      = 10 // read()I
	javaStreamSlotReadBlock = 12 // read([BII)I
	javaStreamSlotAvailable = 14 // available()I
)

// javaStreamSource is the title's own stream a wrapper stands for.
type javaStreamSource struct {
	// Object is the guest `InputStream` subclass instance.
	Object uint32
	// ReadBlock and ReadByte are the entry points of whichever of the two
	// `read` methods the title overrode. `read([BII)` is preferred and
	// `read()` is the fallback, which is the pair the class library itself
	// defines the one in terms of the other: a subclass must override at least
	// the second, and a subclass that overrode neither is not a stream.
	ReadBlock uint32
	ReadByte  uint32
	// Available is the title's own `available`, when it overrode one. A stream
	// that did not is asked nothing: what has been pulled is the answer.
	Available uint32
	// Buffer is the byte array `read([BII)` is handed, allocated once and
	// reused, because a pull per block would otherwise allocate a block per
	// pull.
	Buffer uint32
	// Ended records that its `read` has answered -1. It is not asked again
	// afterwards: the specification says the end of a stream stays the end.
	Ended bool
	// Pulling is set while the title's own `read` is running. A stream whose
	// `read` reads through the same wrapper would otherwise recurse until the
	// host stack ended, and the report for that is a fault with no name in it.
	Pulling bool
}

const (
	// javaStreamPull is how many bytes one call of the title's `read([BII)` is
	// asked for. It is a block rather than the whole stream because a title's
	// own stream has no length to ask for — `available` is a lower bound, not
	// a size — and a block is what its own callers would have used.
	javaStreamPull = 4096
	// javaStreamWindow caps how far ahead of the cursor bytes are held. A
	// reader that asks for more than this at once is asking for a single
	// element bigger than any this platform's readers have, and the cap is
	// what keeps a title's own stream from being told to produce a heap.
	javaStreamWindow = 8 << 20
)

// openJavaGuestStream builds the stream a wrapper over a title's own
// `InputStream` stands for, or says why that object is not one.
func (client *Client) openJavaGuestStream(object uint32) (*javaStream, error) {
	class, known := client.javaClassOfObject(object)
	if !known {
		return nil, fmt.Errorf("the object at %#x is not a stream this platform opened", object)
	}
	source := &javaStreamSource{Object: object}
	var err error
	if source.ReadBlock, err = client.javaOverriddenSlot(class, javaStreamSlotReadBlock); err != nil {
		return nil, err
	}
	if source.ReadByte, err = client.javaOverriddenSlot(class, javaStreamSlotRead); err != nil {
		return nil, err
	}
	if source.Available, err = client.javaOverriddenSlot(class, javaStreamSlotAvailable); err != nil {
		return nil, err
	}
	if source.ReadBlock == 0 && source.ReadByte == 0 {
		return nil, fmt.Errorf(
			"%s overrides neither read() nor read([BII), so it is not a stream this platform can read",
			class.Name)
	}
	if client.logger != nil {
		client.logger.Debug("LGT java stream is the title's own", "class", class.Name,
			"read([BII)I", fmt.Sprintf("%#x", source.ReadBlock),
			"read()I", fmt.Sprintf("%#x", source.ReadByte),
			"available()I", fmt.Sprintf("%#x", source.Available),
			"object", object)
	}
	return &javaStream{Name: class.Name, Source: source}, nil
}

// javaOverriddenSlot answers the entry point a class has in one of
// `java/io/InputStream`'s slots, or zero when the slot still holds what the
// platform's own class put there — which is what "the title did not override
// this one" looks like from here.
func (client *Client) javaOverriddenSlot(class *javaRuntimeClass, slot uint32) (uint32, error) {
	if slot >= class.Slots {
		return 0, nil
	}
	entry, err := client.readWord(class.VTable + 4 + slot*4)
	if err != nil {
		return 0, err
	}
	if entry == 0 {
		return 0, nil
	}
	platform, ok := client.javaRuntimeState().byName[javaInputStreamClass]
	if !ok {
		return 0, fmt.Errorf("%s is not prepared", javaInputStreamClass)
	}
	if slot < platform.Slots {
		inherited, err := client.readWord(platform.VTable + 4 + slot*4)
		if err != nil {
			return 0, err
		}
		if entry == inherited {
			return 0, nil
		}
	}
	return entry, nil
}

// needJavaStream makes sure a stream holds at least that many unread bytes, or
// has ended before it could. It answers how many are actually there, which is
// short of what was asked for only at the end of the data.
//
// **A stream this platform opened is already whole**, so this costs nothing for
// the resource streams every other local title reads; only a title's own stream
// runs the guest.
func (client *Client) needJavaStream(
	ctx context.Context, thread *armcore.Thread, stream *javaStream, want int,
) (int, error) {
	if stream.Source == nil {
		return len(stream.Data) - stream.Read, nil
	}
	if want > javaStreamWindow {
		return 0, fmt.Errorf("%d bytes at once is more than a stream is read in", want)
	}
	if stream.Source.Pulling {
		return 0, fmt.Errorf("%s reads through the wrapper built on it", stream.Name)
	}
	stream.Source.Pulling = true
	defer func() { stream.Source.Pulling = false }()
	for len(stream.Data)-stream.Read < want && !stream.Source.Ended {
		if err := client.pullJavaStream(ctx, thread, stream, want); err != nil {
			return 0, err
		}
	}
	return len(stream.Data) - stream.Read, nil
}

// pullJavaStream runs the title's own `read` once and appends what it answered.
// The bytes already behind the cursor are dropped first: nothing here seeks
// backwards, so the window is exactly what has not been read yet and a long
// stream costs a block rather than its length.
func (client *Client) pullJavaStream(
	ctx context.Context, thread *armcore.Thread, stream *javaStream, want int,
) error {
	source := stream.Source
	if stream.Read > 0 {
		stream.Data = stream.Data[:copy(stream.Data, stream.Data[stream.Read:])]
		stream.Read = 0
	}
	data, err := client.readJavaGuestStream(ctx, thread, stream, want)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		source.Ended = true
		return nil
	}
	stream.Data = append(stream.Data, data...)
	if len(stream.Data)-stream.Read > javaStreamWindow {
		return fmt.Errorf("%s held %d bytes without being read", stream.Name, len(stream.Data)-stream.Read)
	}
	return nil
}

// readJavaGuestStream is the one call into the title. It answers no bytes at
// the end of the stream, which is what the caller turns into "ended".
func (client *Client) readJavaGuestStream(
	ctx context.Context, thread *armcore.Thread, stream *javaStream, want int,
) ([]byte, error) {
	source := stream.Source
	if source.ReadBlock != 0 {
		if source.Buffer == 0 {
			buffer, err := client.newJavaByteArray(make([]byte, javaStreamPull))
			if err != nil {
				return nil, fmt.Errorf("open a buffer for %s: %w", stream.Name, err)
			}
			source.Buffer = buffer
		}
		answer, err := client.callOn(ctx, thread, source.ReadBlock,
			[]uint32{source.Object, source.Buffer, 0, javaStreamPull})
		if err != nil {
			return nil, fmt.Errorf("run %s read([BII)I at %#x: %w", stream.Name, source.ReadBlock, err)
		}
		count := int(int32(answer))
		if count <= 0 {
			// -1 is the end of the stream. A read of nothing when a block was
			// asked for is not a case the language has — a stream with bytes
			// answers at least one, one without blocks or ends — and treating
			// it as anything but the end is a loop that never finishes.
			return nil, nil
		}
		if count > javaStreamPull {
			return nil, fmt.Errorf("%s read([BII) filled %d bytes of a %d-byte array",
				stream.Name, count, javaStreamPull)
		}
		block, _, err := client.javaArrayBlock(source.Buffer)
		if err != nil {
			return nil, err
		}
		data := make([]byte, count)
		if err := client.core.Memory().Read(block+javaArrayLengthWords*4, data); err != nil {
			return nil, err
		}
		return data, nil
	}
	// Only `read()` was overridden, which is the one the class library declares
	// abstract and the least a subclass can get away with. It is asked for
	// exactly what is wanted rather than for a block: a byte is a guest call
	// here, and a block of them for a title that wanted four is the difference
	// between a load and a stall.
	if want > javaStreamPull {
		want = javaStreamPull
	}
	data := make([]byte, 0, want)
	for len(data) < want {
		answer, err := client.callOn(ctx, thread, source.ReadByte, []uint32{source.Object})
		if err != nil {
			return nil, fmt.Errorf("run %s read()I at %#x: %w", stream.Name, source.ReadByte, err)
		}
		if int32(answer) < 0 {
			break
		}
		data = append(data, byte(answer))
	}
	return data, nil
}

// javaGuestStreamAvailable asks the title's own stream how much is left, which
// is the one question the window here cannot answer: what has been pulled says
// nothing about what the title's stream still holds. A stream that overrode no
// `available` answers with the window, which is the specification's own floor —
// `available` is what can be read without blocking, and these bytes can.
func (client *Client) javaGuestStreamAvailable(
	ctx context.Context, thread *armcore.Thread, stream *javaStream,
) (uint32, error) {
	held := uint32(len(stream.Data) - stream.Read)
	if stream.Source.Available == 0 {
		return held, nil
	}
	answer, err := client.callOn(ctx, thread, stream.Source.Available, []uint32{stream.Source.Object})
	if err != nil {
		return 0, fmt.Errorf("run %s available()I at %#x: %w",
			stream.Name, stream.Source.Available, err)
	}
	if int32(answer) < 0 {
		return held, nil
	}
	// The title's own estimate plus what has already been pulled, saturated:
	// `available` is a count a caller sizes an array from, and one that wrapped
	// would be a smaller array than the read that follows it needs.
	if total := uint64(held) + uint64(answer); total <= uint64(maxJavaArrayLength) {
		return uint32(total), nil
	}
	return maxJavaArrayLength, nil
}

// skipJavaGuestStream is `skip` over a title's own stream: the bytes are pulled
// and dropped, because nothing here can move a cursor the title owns. It
// answers how far it actually moved, which is short of what was asked for at
// the end of the data.
func (client *Client) skipJavaGuestStream(
	ctx context.Context, thread *armcore.Thread, stream *javaStream, wanted uint64,
) (uint32, error) {
	moved := uint64(0)
	for moved < wanted {
		step := wanted - moved
		if step > javaStreamPull {
			step = javaStreamPull
		}
		held, err := client.needJavaStream(ctx, thread, stream, int(step))
		if err != nil {
			return 0, err
		}
		if held == 0 {
			break
		}
		if uint64(held) < step {
			step = uint64(held)
		}
		stream.Read += int(step)
		moved += step
	}
	if err := thread.SetRegister(1, uint32(moved>>32)); err != nil {
		return 0, err
	}
	return uint32(moved), nil
}

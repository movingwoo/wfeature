package lgt

import (
	"context"
	"crypto/sha256"
	"fmt"
	"unicode/utf16"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Resource streams: `Class.getResourceAsStream` and what a title reads out of
// one.
//
// This is how an AOT title reads its own data. Its call sites read:
//
//	bl    <Object.getClass>          ; the class of the Jlet
//	bl    <Class slot 16>(name)      ; the stream, null-checked
//	bl    <InputStream slot 10>      ; a byte, twice, shifted into a halfword
//
// which is `getResourceAsStream` and `read()`. The bytes come from the same
// place a Clet's resource read takes them from — the archive's own files — so a
// Java title and a Clet see one filesystem.

// javaStream is one open resource stream: what it holds and how far a title has
// read into it. When Source is set the bytes are not the platform's — Data is
// a window pulled through the title's own stream, and Read is a cursor into
// that window rather than into a whole file.
type javaStream struct {
	Name   string
	Data   []byte
	Read   int
	Closed bool
	Source *javaStreamSource
}

const (
	javaInputStreamClass       = "java/io/InputStream"
	javaDataInputStreamClass   = "java/io/DataInputStream"
	javaInputStreamReaderClass = "java/io/InputStreamReader"
	javaImageClass             = "org/kwis/msp/lcdui/Image"
)

// openJavaResourceStream is `Class.getResourceAsStream(String)`. The receiver
// is a class object and is not read: a resource name is absolute in the
// archive, and the class is only there because the method is not static.
//
// **A missing resource answers null**, which is what the specification says and
// what the call sites expect — every one of them null-checks the answer before
// touching it.
func javaGetResourceAsStream(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, ok := client.javaText(arguments[1])
	if !ok {
		return 0, fmt.Errorf("the name at %#x is not a string this platform built", arguments[1])
	}
	data, found := client.archive.Resource(name)
	if !found {
		if client.logger != nil {
			client.logger.Debug("LGT java resource is not in the archive", "name", name)
		}
		return 0, nil
	}
	class, err := client.preparePlatformJavaClass(javaInputStreamClass)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().streams[object] = &javaStream{Name: name, Data: data}
	if client.logger != nil {
		client.logger.Debug("LGT java resource opened",
			"name", name, "bytes", len(data), "stream", object)
	}
	return object, nil
}

// javaStreamOf answers the stream an object stands for.
func (client *Client) javaStreamOf(object uint32) (*javaStream, error) {
	if client.javaRun == nil {
		return nil, fmt.Errorf("no stream has been opened")
	}
	stream, ok := client.javaRun.streams[object]
	if !ok {
		return nil, fmt.Errorf("the object at %#x is not a stream this platform opened", object)
	}
	if stream.Closed {
		return nil, fmt.Errorf("the stream of %s is closed", stream.Name)
	}
	return stream, nil
}

// javaStreamNeeding answers the stream an object stands for with at least that
// many unread bytes in it. For a resource this platform opened that is the
// lookup alone; for a title's own stream it is where the title's `read` runs.
// Every reader below goes through it, so a wrapper over either kind reads the
// same.
func (client *Client) javaStreamNeeding(
	ctx context.Context, thread *armcore.Thread, object uint32, want int,
) (*javaStream, error) {
	stream, err := client.javaStreamOf(object)
	if err != nil {
		return nil, err
	}
	if _, err := client.needJavaStream(ctx, thread, stream, want); err != nil {
		return nil, err
	}
	return stream, nil
}

// javaStreamRead is `InputStream.read()`: one byte as an unsigned value, and
// -1 at the end, which is why the answer is an int rather than a byte.
func javaStreamRead(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamNeeding(ctx, thread, arguments[0], 1)
	if err != nil {
		return 0, err
	}
	if stream.Read >= len(stream.Data) {
		return ^uint32(0), nil
	}
	value := uint32(stream.Data[stream.Read])
	stream.Read++
	return value, nil
}

// javaStreamReadArray is `InputStream.read([B)` and `read([BII)`: as many bytes
// as are left, into the array the caller passes, answering how many were read
// and -1 when there were none because the stream had ended.
func javaStreamReadArray(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamOf(arguments[0])
	if err != nil {
		return 0, err
	}
	array := arguments[1]
	block, err := client.readWord(array + 8)
	if err != nil {
		return 0, err
	}
	length, err := client.readWord(block)
	if err != nil {
		return 0, err
	}
	offset, count := uint32(0), length
	if len(arguments) > 3 {
		offset, count = arguments[2], arguments[3]
	}
	if uint64(offset)+uint64(count) > uint64(length) {
		return 0, fmt.Errorf("%d bytes at %d is past the end of an array of %d", count, offset, length)
	}
	left, err := client.needJavaStream(ctx, thread, stream, int(count))
	if err != nil {
		return 0, err
	}
	if left <= 0 {
		// The end of the stream is -1, and it is not the same answer as a read
		// of nothing: a caller loops until one of them.
		if count == 0 {
			return 0, nil
		}
		return ^uint32(0), nil
	}
	if uint64(count) > uint64(left) {
		count = uint32(left)
	}
	data := stream.Data[stream.Read : stream.Read+int(count)]
	if err := client.core.Memory().Write(block+javaArrayLengthWords*4+offset, data); err != nil {
		return 0, err
	}
	stream.Read += int(count)
	return count, nil
}

// javaCreateImage is `Image.createImage([BII)`: the bytes of an encoded image
// out of the array a title just read its resource into. The picture itself is
// the same surface a Clet's `MC_grpCreateImage` answers — one decoder, one
// framebuffer table — and the object is what a Java title holds it by.
func javaCreateImage(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	array, offset, length := arguments[0], arguments[1], arguments[2]
	block, count, err := client.javaArrayBlock(array)
	if err != nil {
		return 0, err
	}
	if uint64(offset)+uint64(length) > uint64(count) || length > maxImageBytes {
		return 0, fmt.Errorf("%d bytes at %d of an array of %d", length, offset, count)
	}
	encoded := make([]byte, length)
	if err := client.core.Memory().Read(block+javaArrayLengthWords*4+offset, encoded); err != nil {
		return 0, err
	}
	// The same rule the named form gets: the bytes are the name here, so a
	// title that decodes one picture twice pays for one surface. See
	// newSharedJavaImage.
	return client.newSharedJavaImage(imageDigestKey(encoded), encoded)
}

// javaCreateImageNamed is `Image.createImage(String)`: the same picture, named
// rather than handed over as bytes. The name is a resource in the title's own
// archive, which is where its Clet path reads one from too.
//
// **A missing resource is a failure and not a null**: the specification gives
// this form an `IOException`, and a title that catches one is told what it
// asked for, while a null would fault at the first draw.
func javaCreateImageNamed(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the name at %#x is not a string this platform built", arguments[0])
	}
	encoded, found := client.archive.Resource(name)
	if !found {
		return 0, client.throwJavaPlatform(thread, javaIOExceptionClass, ": "+name)
	}
	// **One surface per picture** — see newSharedJavaImage, which gives the
	// byte form the same rule under a different key.
	return client.newSharedJavaImage("name:"+name, encoded)
}

// newSharedJavaImage answers an Image over the surface this exact picture was
// already decoded into, decoding it only the first time.
//
// A picture is immutable — the specification says only a copy can be drawn
// into — so two Images decoded from the same resource, or from the same bytes,
// are the same pixels; and decoding a second set of them costs a surface
// **nothing here ever reclaims**, because the Java path has no collector and
// an Image is released by the language on a handset. One title reloads its
// sprite sheets from inside `paint`: 879 loads of a handful of names in two
// thousand ticks, which filled the surface region and ended the run.
//
// The object is still a new one. Sharing the pixels is unobservable; sharing
// the object would make `==` answer true where the language says nothing, and
// a title that keeps two of them apart is entitled to. A title that then draws
// into one gets its own copy — see unshareDecodedSurface.
func (client *Client) newSharedJavaImage(key string, encoded []byte) (uint32, error) {
	runtime := client.javaRuntimeState()
	if handle, cached := runtime.decodedImages[key]; cached {
		return client.newJavaImageOn(handle)
	}
	object, err := client.newJavaImage(encoded)
	if err != nil {
		return 0, err
	}
	if runtime.decodedImages == nil {
		runtime.decodedImages = map[string]uint32{}
	}
	runtime.decodedImages[key] = runtime.images[object]
	return object, nil
}

// imageDigestKey names a picture by its bytes, for the form that is handed an
// array rather than a name. It is a digest rather than the bytes themselves so
// that a title assembling large pictures does not also pay for a second copy
// of each one in the key.
func imageDigestKey(encoded []byte) string {
	digest := sha256.Sum256(encoded)
	return "bytes:" + string(digest[:])
}

// unshareDecodedSurface gives an image its own pixels when it is about to be
// drawn into and its surface is one the decode cache is handing out. An image
// nothing else holds is left alone: the copy is only for the sharing.
func (client *Client) unshareDecodedSurface(object uint32, surface *framebuffer) (*framebuffer, error) {
	runtime := client.javaRuntimeState()
	shared := false
	for _, handle := range runtime.decodedImages {
		if handle == surface.handle {
			shared = true
			break
		}
	}
	if !shared {
		return surface, nil
	}
	private, err := client.newFramebuffer(surface.width, surface.height, false)
	if err != nil {
		return nil, err
	}
	copy(private.pixels, surface.pixels)
	if surface.opaque != nil {
		private.opaque = append([]bool(nil), surface.opaque...)
	}
	runtime.images[object] = private.handle
	// The cache keeps the picture it decoded, for whoever asks for it next.
	return private, nil
}

// newJavaImageOn answers a fresh Image object over a surface that is already
// decoded.
func (client *Client) newJavaImageOn(handle uint32) (uint32, error) {
	class, err := client.preparePlatformJavaClass(javaImageClass)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().images[object] = handle
	return object, nil
}

// newJavaImage decodes an encoded picture and answers the Image object a title
// holds it by. The surface is the same kind a Clet's `MC_grpCreateImage`
// answers — one decoder, one framebuffer table.
func (client *Client) newJavaImage(encoded []byte) (uint32, error) {
	if len(encoded) > maxImageBytes {
		return 0, fmt.Errorf("an image of %d bytes", len(encoded))
	}
	decoded, err := decodeImage(encoded)
	if err != nil {
		return 0, fmt.Errorf("decode %d bytes as an image: %w", len(encoded), err)
	}
	surface, err := client.framebufferFromImage(decoded)
	if err != nil {
		return 0, err
	}
	class, err := client.preparePlatformJavaClass(javaImageClass)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().images[object] = surface.handle
	return object, nil
}

// javaImageSurface answers the surface a title's Image object stands for.
func (client *Client) javaImageSurface(object uint32) (*framebuffer, error) {
	if client.javaRun == nil {
		return nil, fmt.Errorf("no image has been created")
	}
	handle, ok := client.javaRun.images[object]
	if !ok {
		return nil, fmt.Errorf("the object at %#x is not an image this platform created", object)
	}
	surface := client.framebuffer(handle)
	if surface == nil {
		return nil, fmt.Errorf("the image at %#x has no surface", object)
	}
	return surface, nil
}

// javaImageArgument answers the surface an image argument names. A null is not
// a wrong object but the missing one, and the specification says what a call
// handed it does: it throws. Keeping the two apart is the whole difference
// between a title whose scene change drew one frame too early and a title that
// stopped; see uncaught.go.
func (client *Client) javaImageArgument(
	thread *armcore.Thread, object uint32, where string,
) (*framebuffer, error) {
	if object == 0 {
		return nil, client.javaNullImage(thread, where)
	}
	return client.javaImageSurface(object)
}

// javaImageWidth and javaImageHeight answer what the decoded picture measures.
func javaImageWidth(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	surface, err := client.javaImageArgument(thread, arguments[0], "getWidth")
	if err != nil {
		return 0, err
	}
	return uint32(surface.width), nil
}

func javaImageHeight(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	surface, err := client.javaImageArgument(thread, arguments[0], "getHeight")
	if err != nil {
		return 0, err
	}
	return uint32(surface.height), nil
}

// javaStreamClose is `InputStream.close()`: the title is done with the
// resource. The stream is kept rather than forgotten, so a read after a close
// says that is what it was rather than that the object was never a stream.
//
// **Closing twice is not an error**, which the language says in as many words
// and the titles rely on: a wrapper and the stream under it are both closed,
// in that order, and they are the same open stream here.
func javaStreamClose(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if client.javaRun == nil {
		return 0, fmt.Errorf("no stream has been opened")
	}
	stream, ok := client.javaRun.streams[arguments[0]]
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a stream this platform opened", arguments[0])
	}
	stream.Closed = true
	// A stream opened on a File leaves that file where the reading got to; a
	// plain one is bound to nothing and this is nothing. See java_file.go.
	return 0, client.syncJavaStreamToFile(arguments[0])
}

// javaStreamAvailable is `InputStream.available()`: how much is left, which for
// a resource this platform holds whole is exact rather than an estimate.
func javaStreamAvailable(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if stream.Source != nil {
		return client.javaGuestStreamAvailable(ctx, thread, stream)
	}
	return uint32(len(stream.Data) - stream.Read), nil
}

// javaStreamSkip is `skip(long)`: move the cursor forward without reading, and
// answer how far it actually moved — which is less than asked for at the end of
// the data. The answer comes back as the two words a long takes.
func javaStreamSkip(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamOf(arguments[0])
	if err != nil {
		return 0, err
	}
	wanted := uint64(arguments[1]) | uint64(arguments[2])<<32
	if stream.Source != nil {
		return client.skipJavaGuestStream(ctx, thread, stream, wanted)
	}
	left := uint64(len(stream.Data) - stream.Read)
	if wanted > left {
		wanted = left
	}
	stream.Read += int(wanted)
	if err := thread.SetRegister(1, uint32(wanted>>32)); err != nil {
		return 0, err
	}
	return uint32(wanted), nil
}

// javaWrapStream is the constructor of a stream built on another one — a
// `DataInputStream` over the resource stream a title just opened. **Both
// objects stand for the same open stream**, so a read through either moves the
// one cursor, which is what wrapping means.
//
// **The stream underneath does not have to be one this platform opened.** A
// title that wrote its own `InputStream` subclass and wraps that is wrapping
// the abstract class the constructor is declared over, which is legal Java and
// what one local title does; the wrapper then reads through the title's own
// `read`. See java_stream_guest.go.
func javaWrapStream(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamOf(arguments[1])
	if err != nil {
		guest, guestErr := client.openJavaGuestStream(arguments[1])
		if guestErr != nil {
			return 0, guestErr
		}
		stream = guest
		client.javaRuntimeState().streams[arguments[1]] = stream
	}
	client.javaRuntimeState().streams[arguments[0]] = stream
	return 0, nil
}

// javaStreamReadShort reads the two bytes a data stream's halfword takes, most
// significant first, which is the order every Java stream reads numbers in.
func javaStreamReadShort(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamNeeding(ctx, thread, arguments[0], 2)
	if err != nil {
		return 0, err
	}
	if stream.Read+2 > len(stream.Data) {
		return 0, fmt.Errorf("%s ends after %d bytes", stream.Name, len(stream.Data))
	}
	value := int16(uint16(stream.Data[stream.Read])<<8 | uint16(stream.Data[stream.Read+1]))
	stream.Read += 2
	return uint32(int32(value)), nil
}

// javaStreamReadInt is `DataInputStream.readInt()`, slot 28: four bytes,
// big-endian, which is the order every DataInput method reads in. A stream
// with fewer than four bytes left is the end of the file rather than a short
// read, and the language says so by throwing; this reports it, which stops the
// title where the truncated data is rather than somewhere downstream of a
// number made out of nothing.
func javaStreamReadInt(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamNeeding(ctx, thread, arguments[0], 4)
	if err != nil {
		return 0, err
	}
	if stream.Read+4 > len(stream.Data) {
		return 0, fmt.Errorf("readInt past the end of %q, at %d of %d", stream.Name, stream.Read, len(stream.Data))
	}
	value := uint32(stream.Data[stream.Read])<<24 |
		uint32(stream.Data[stream.Read+1])<<16 |
		uint32(stream.Data[stream.Read+2])<<8 |
		uint32(stream.Data[stream.Read+3])
	stream.Read += 4
	return value, nil
}

// javaStreamReadByte is `DataInputStream.readByte()`, slot 23: one byte read
// as a signed number, which is what separates it from `read()` on the same
// stream — that one answers 0 to 255 and -1 at the end, this one answers -128
// to 127 and has no room left for an end marker, so the end is reported.
func javaStreamReadByte(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamNeeding(ctx, thread, arguments[0], 1)
	if err != nil {
		return 0, err
	}
	if stream.Read >= len(stream.Data) {
		return 0, fmt.Errorf("readByte past the end of %q, at %d of %d", stream.Name, stream.Read, len(stream.Data))
	}
	value := int8(stream.Data[stream.Read])
	stream.Read++
	return uint32(int32(value)), nil
}

// javaStreamReadFully is `DataInputStream.readFully([BII)`, slot 20: exactly
// the bytes asked for or none at all. That is the whole difference from
// `read([BII)` beside it, which answers a short count and leaves the caller to
// loop; a title that calls this one has sized its array from a count it read
// first and treats a short read as a corrupt file.
func javaStreamReadFully(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamOf(arguments[0])
	if err != nil {
		return 0, err
	}
	block, length, err := client.javaArrayBlock(arguments[1])
	if err != nil {
		return 0, err
	}
	offset, count := uint32(0), length
	if len(arguments) > 3 {
		offset, count = arguments[2], arguments[3]
	}
	if uint64(offset)+uint64(count) > uint64(length) {
		return 0, fmt.Errorf("%d bytes at %d is past the end of an array of %d", count, offset, length)
	}
	if _, err := client.needJavaStream(ctx, thread, stream, int(count)); err != nil {
		return 0, err
	}
	if uint64(count) > uint64(len(stream.Data)-stream.Read) {
		return 0, fmt.Errorf("readFully of %d bytes past the end of %q, at %d of %d",
			count, stream.Name, stream.Read, len(stream.Data))
	}
	data := stream.Data[stream.Read : stream.Read+int(count)]
	if err := client.core.Memory().Write(block+javaArrayLengthWords*4+offset, data); err != nil {
		return 0, err
	}
	stream.Read += int(count)
	return 0, nil
}

// `java/io/ByteArrayOutputStream`: the sink a title builds a block of bytes in
// before it hands them somewhere — a record it is about to write, a message it
// is about to send. What it holds is this platform's, keyed by the object, the
// same arrangement a String's characters and a Vector's contents have.

const (
	javaByteSinkClass         = "java/io/ByteArrayOutputStream"
	javaDataOutputStreamClass = "java/io/DataOutputStream"
)

// javaByteSinkConstructor is `ByteArrayOutputStream()` and the form that takes
// a starting capacity. The capacity is a hint about an allocation a caller
// cannot observe, so both open the same empty sink.
func javaByteSinkConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaRuntimeState().sinks[arguments[0]] = []byte{}
	return 0, nil
}

// javaByteSinkOf answers what a sink holds and which object holds it, which
// are not the same handle when the caller is writing through a wrapper.
func (client *Client) javaByteSinkOf(object uint32) ([]byte, uint32, error) {
	runtime := client.javaRuntimeState()
	if inner, wrapped := runtime.wrapped[object]; wrapped {
		object = inner
	}
	held, ok := runtime.sinks[object]
	if !ok {
		return nil, 0, fmt.Errorf("the object at %#x is not a byte sink this platform built", object)
	}
	return held, object, nil
}

// javaByteSinkWrite is `write(int)`, slot 10: one byte, the low eight bits of
// what it is given, which is what the specification says a caller that hands
// over a whole int gets.
func javaByteSinkWrite(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, sink, err := client.javaByteSinkOf(arguments[0])
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().sinks[sink] = append(held, byte(arguments[1]))
	return 0, nil
}

// javaByteSinkWriteRange is `write([BII)`, slot 12: a run of an array.
func javaByteSinkWriteRange(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, sink, err := client.javaByteSinkOf(arguments[0])
	if err != nil {
		return 0, err
	}
	data, err := client.readJavaArrayBytes(arguments[1])
	if err != nil {
		return 0, err
	}
	offset, count := arguments[2], arguments[3]
	if uint64(offset)+uint64(count) > uint64(len(data)) {
		return 0, fmt.Errorf("%d bytes at %d is past the end of an array of %d", count, offset, len(data))
	}
	client.javaRuntimeState().sinks[sink] = append(held, data[offset:offset+count]...)
	return 0, nil
}

// javaByteSinkWriteAll is `write([B)`, slot 11: the whole of an array.
func javaByteSinkWriteAll(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, sink, err := client.javaByteSinkOf(arguments[0])
	if err != nil {
		return 0, err
	}
	data, err := client.readJavaArrayBytes(arguments[1])
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().sinks[sink] = append(held, data...)
	return 0, nil
}

// javaByteSinkFlush is `flush()` and `close()`, slots 13 and 14. A sink is
// bytes in memory: there is nothing held back for a flush to release, and
// closing one leaves what it holds readable, which the specification says in
// so many words.
func javaByteSinkFlush(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	_, sink, err := client.javaByteSinkOf(arguments[0])
	if err != nil {
		return 0, err
	}
	// A plain sink has nowhere to drain to and a flush is nothing; one opened
	// on a File is where a title expects its bytes to have landed by now.
	return 0, client.drainJavaSinkToFile(sink)
}

// javaByteSinkReset is `reset()`, slot 15: the sink emptied and used again,
// which is how a title that writes one record after another avoids building a
// sink per record.
func javaByteSinkReset(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	_, sink, err := client.javaByteSinkOf(arguments[0])
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().sinks[sink] = []byte{}
	return 0, nil
}

// javaByteSinkBytes is `toByteArray()`, slot 16: a copy of what has been
// written, as an array the guest owns. It is a copy the way the specification
// says: a title that keeps the array and writes again must not see its copy
// change.
func javaByteSinkBytes(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, _, err := client.javaByteSinkOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return client.newJavaByteArray(append([]byte{}, held...))
}

// javaByteSinkSize is `size()`, slot 17: how many bytes have been written.
func javaByteSinkSize(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, _, err := client.javaByteSinkOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return uint32(len(held)), nil
}

// `java/io/DataOutputStream`: numbers written into the sink underneath it,
// most significant byte first, which is the order every Java stream writes in
// and the order the read side beside it reads in. The pair is one round trip:
// a title stores a record with these and reads it back with the
// DataInputStream methods above.

// javaWrapSink is `DataOutputStream(OutputStream)`. A wrapper is not a second
// sink; it stands for the one it was built on, so writing through it and
// reading the sink's own bytes back agree.
func javaWrapSink(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, _, err := client.javaByteSinkOf(arguments[1]); err != nil {
		return 0, err
	}
	client.javaRuntimeState().wrapped[arguments[0]] = arguments[1]
	return 0, nil
}

// javaSinkAppend is every fixed-width write on a data stream: the low `width`
// bytes of the value, most significant first.
func javaSinkAppend(width int) func(*Client, context.Context, *armcore.Thread, []uint32) (uint32, error) {
	return func(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
		held, sink, err := client.javaByteSinkOf(arguments[0])
		if err != nil {
			return 0, err
		}
		value := arguments[1]
		for shift := (width - 1) * 8; shift >= 0; shift -= 8 {
			held = append(held, byte(value>>uint(shift)))
		}
		client.javaRuntimeState().sinks[sink] = held
		return 0, nil
	}
}

// javaSinkWriteLong is `writeLong(long)`: eight bytes, and the one write whose
// value arrives in two registers.
func javaSinkWriteLong(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, sink, err := client.javaByteSinkOf(arguments[0])
	if err != nil {
		return 0, err
	}
	value := uint64(arguments[2])<<32 | uint64(arguments[1])
	for shift := 56; shift >= 0; shift -= 8 {
		held = append(held, byte(value>>uint(shift)))
	}
	client.javaRuntimeState().sinks[sink] = held
	return 0, nil
}

// javaSinkWriteUTF is `writeUTF(String)`: a two-byte length and then the text
// in modified UTF-8. It is written against the read side's own encoding rather
// than against Go's, because the pair is one round trip — a title stores a
// name with this and reads it back with readUTF — and modified UTF-8 differs
// from Go's in exactly the two places a name can reach: a null character is
// two bytes rather than one, and a character outside the basic plane is the
// two surrogates that stand for it rather than four bytes.
func javaSinkWriteUTF(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	held, sink, err := client.javaByteSinkOf(arguments[0])
	if err != nil {
		return 0, err
	}
	text, ok := client.javaText(arguments[1])
	if !ok {
		return 0, fmt.Errorf("DataOutputStream.writeUTF was given no text at %#x", arguments[1])
	}
	encoded := modifiedUTF8(text)
	if len(encoded) > 0xffff {
		return 0, fmt.Errorf("writeUTF of %d bytes does not fit its two-byte length", len(encoded))
	}
	held = append(held, byte(len(encoded)>>8), byte(len(encoded)))
	client.javaRuntimeState().sinks[sink] = append(held, encoded...)
	return 0, nil
}

// javaStreamReadUTF is `DataInputStream.readUTF()`, slot 32: a two-byte length
// and then that many bytes of modified UTF-8. It is the read side of the
// `writeUTF` beside it and decodes with the same encoding, so a name a title
// stored comes back the way it went in.
//
// **The length is in bytes, not in characters**, which is what makes a short
// stream tell-able from a long name: the count is checked against what is left
// before any of it is decoded.
func javaStreamReadUTF(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamNeeding(ctx, thread, arguments[0], 2)
	if err != nil {
		return 0, err
	}
	if stream.Read+2 > len(stream.Data) {
		return 0, fmt.Errorf("readUTF past the end of %q, at %d of %d",
			stream.Name, stream.Read, len(stream.Data))
	}
	count := int(stream.Data[stream.Read])<<8 | int(stream.Data[stream.Read+1])
	if _, err := client.needJavaStream(ctx, thread, stream, 2+count); err != nil {
		return 0, err
	}
	if stream.Read+2+count > len(stream.Data) {
		return 0, fmt.Errorf("readUTF of %d bytes past the end of %q, at %d of %d",
			count, stream.Name, stream.Read, len(stream.Data))
	}
	text, err := decodeModifiedUTF8(stream.Data[stream.Read+2 : stream.Read+2+count])
	if err != nil {
		return 0, fmt.Errorf("readUTF from %q: %w", stream.Name, err)
	}
	stream.Read += 2 + count
	return client.newJavaString(text)
}

// decodeModifiedUTF8 reads what modifiedUTF8 writes. A sequence that is not one
// of the three forms is reported rather than replaced: the bytes came out of a
// title's own file, and text that does not decode means the cursor is not where
// the title thinks it is.
func decodeModifiedUTF8(data []byte) (string, error) {
	units := make([]uint16, 0, len(data))
	for index := 0; index < len(data); {
		first := data[index]
		switch {
		case first&0x80 == 0:
			units = append(units, uint16(first))
			index++
		case first&0xe0 == 0xc0:
			if index+1 >= len(data) || data[index+1]&0xc0 != 0x80 {
				return "", fmt.Errorf("a two-byte sequence at %d is cut short", index)
			}
			units = append(units, uint16(first&0x1f)<<6|uint16(data[index+1]&0x3f))
			index += 2
		case first&0xf0 == 0xe0:
			if index+2 >= len(data) || data[index+1]&0xc0 != 0x80 || data[index+2]&0xc0 != 0x80 {
				return "", fmt.Errorf("a three-byte sequence at %d is cut short", index)
			}
			units = append(units, uint16(first&0x0f)<<12|
				uint16(data[index+1]&0x3f)<<6|uint16(data[index+2]&0x3f))
			index += 3
		default:
			return "", fmt.Errorf("byte %#02x at %d begins no sequence", first, index)
		}
	}
	return string(utf16.Decode(units)), nil
}

// modifiedUTF8 is the encoding a Java stream's UTF methods use.
func modifiedUTF8(text string) []byte {
	encoded := make([]byte, 0, len(text))
	for _, unit := range utf16Units(text) {
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

// javaByteStreamConstructor is `ByteArrayInputStream([B)`: a stream whose
// content is the array it was handed. The bytes are copied rather than read
// back out of the array on demand, because the specification's stream reads
// the array as it was when the stream was built and a title is free to reuse
// the buffer.
func javaByteStreamConstructor(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	// **A null array is the language's own NullPointerException, not a platform
	// failure.** The constructor reads the array's length, so the specification
	// has it throw; reporting instead stops the title where a title that
	// catches its own nulls would have carried on. One local title logs exactly
	// that catch a few lines earlier in its own run.
	if arguments[1] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass,
			"a null array handed to ByteArrayInputStream")
	}
	data, err := client.readJavaArrayBytes(arguments[1])
	if err != nil {
		return 0, err
	}
	held := make([]byte, len(data))
	copy(held, data)
	client.javaRuntimeState().streams[arguments[0]] = &javaStream{
		Name: fmt.Sprintf("a byte array of %d", len(held)), Data: held}
	return 0, nil
}

// javaStreamReadBoolean is `DataInputStream.readBoolean()`, slot 22: one byte,
// false when it is zero and true otherwise, which is what the specification
// defines and the byte `writeBoolean` wrote.
func javaStreamReadBoolean(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamNeeding(ctx, thread, arguments[0], 1)
	if err != nil {
		return 0, err
	}
	if stream.Read >= len(stream.Data) {
		return 0, fmt.Errorf("the stream of %s ended before a boolean", stream.Name)
	}
	value := stream.Data[stream.Read]
	stream.Read++
	if value == 0 {
		return 0, nil
	}
	return 1, nil
}

// javaReaderRead is `InputStreamReader.read(char[])`: as many characters as
// the array holds, decoded from the stream the reader was built on, answering
// how many were read and -1 when the stream had already ended.
//
// **The cursor stays in bytes**, because that is what the stream underneath
// counts, so the loop takes one character's worth of bytes at a time rather
// than decoding the rest of the stream and guessing how far that got. A
// character is two bytes when its first one is a lead byte and there is a
// second to go with it, and one otherwise — the shape of the handset's own
// encoding.
func javaReaderRead(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamNeeding(ctx, thread, arguments[0], 1)
	if err != nil {
		return 0, err
	}
	width, err := client.javaArrayElementBytes(arguments[1])
	if err != nil {
		return 0, err
	}
	if width != 2 {
		return 0, fmt.Errorf("a reader fills a char array, not one of %d-byte elements", width)
	}
	block, length, err := client.javaArrayBlock(arguments[1])
	if err != nil {
		return 0, err
	}
	// One character is at most two bytes here, so a full array is at most
	// twice its length: a reader over a title's own stream pulls that much
	// before it decodes, rather than a block per character.
	if _, err := client.needJavaStream(ctx, thread, stream, 2*int(length)); err != nil {
		return 0, err
	}
	if stream.Read >= len(stream.Data) {
		return ^uint32(0), nil
	}
	written := uint32(0)
	for written < length && stream.Read < len(stream.Data) {
		step := 1
		if stream.Data[stream.Read] >= 0x81 && stream.Read+1 < len(stream.Data) {
			step = 2
		}
		symbols := []rune(decodeEUCKR(stream.Data[stream.Read : stream.Read+step]))
		if written+uint32(len(symbols)) > length {
			break
		}
		for _, symbol := range symbols {
			at := block + javaArrayLengthWords*4 + written*2
			if err := client.writeHalfword(at, uint16(symbol)); err != nil {
				return 0, err
			}
			written++
		}
		stream.Read += step
	}
	return written, nil
}

// javaStreamReadChar is `DataInputStream.readChar()`, slot 27: the same two
// big-endian bytes `readShort` takes, kept unsigned.
func javaStreamReadChar(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	value, err := javaStreamReadShort(client, ctx, thread, arguments)
	if err != nil {
		return 0, err
	}
	return value & 0xffff, nil
}

// javaStreamReadUnsignedByte is `DataInputStream.readUnsignedByte()`, slot 24:
// the byte `readByte` reads, as 0 to 255. The end of the stream is reported
// rather than answered with -1, because 255 values are already spoken for.
func javaStreamReadUnsignedByte(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamNeeding(ctx, thread, arguments[0], 1)
	if err != nil {
		return 0, err
	}
	if stream.Read >= len(stream.Data) {
		return 0, fmt.Errorf("readUnsignedByte past the end of %q, at %d of %d",
			stream.Name, stream.Read, len(stream.Data))
	}
	value := uint32(stream.Data[stream.Read])
	stream.Read++
	return value, nil
}

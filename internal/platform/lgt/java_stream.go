package lgt

import (
	"context"
	"fmt"

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
// read into it.
type javaStream struct {
	Name   string
	Data   []byte
	Read   int
	Closed bool
}

const (
	javaInputStreamClass     = "java/io/InputStream"
	javaDataInputStreamClass = "java/io/DataInputStream"
	javaImageClass           = "org/kwis/msp/lcdui/Image"
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

// javaStreamRead is `InputStream.read()`: one byte as an unsigned value, and
// -1 at the end, which is why the answer is an int rather than a byte.
func javaStreamRead(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamOf(arguments[0])
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
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
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
	left := len(stream.Data) - stream.Read
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
	return client.newJavaImage(encoded)
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
	return client.newJavaImage(encoded)
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

// javaImageWidth and javaImageHeight answer what the decoded picture measures.
func javaImageWidth(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	surface, err := client.javaImageSurface(arguments[0])
	if err != nil {
		return 0, err
	}
	return uint32(surface.width), nil
}

func javaImageHeight(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	surface, err := client.javaImageSurface(arguments[0])
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
	return 0, nil
}

// javaStreamAvailable is `InputStream.available()`: how much is left, which for
// a resource this platform holds whole is exact rather than an estimate.
func javaStreamAvailable(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return uint32(len(stream.Data) - stream.Read), nil
}

// javaStreamSkip is `skip(long)`: move the cursor forward without reading, and
// answer how far it actually moved — which is less than asked for at the end of
// the data. The answer comes back as the two words a long takes.
func javaStreamSkip(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamOf(arguments[0])
	if err != nil {
		return 0, err
	}
	wanted := uint64(arguments[1]) | uint64(arguments[2])<<32
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
func javaWrapStream(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamOf(arguments[1])
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().streams[arguments[0]] = stream
	return 0, nil
}

// javaStreamReadShort reads the two bytes a data stream's halfword takes, most
// significant first, which is the order every Java stream reads numbers in.
func javaStreamReadShort(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	stream, err := client.javaStreamOf(arguments[0])
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

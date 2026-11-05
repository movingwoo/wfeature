package jvm

import (
	"fmt"
	"math"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// The java/io half of the core library. The streams are the surface a game
// reads its own archive entries and save records through, so what matters here
// is that an overridden read or write is still the one that runs: every call
// these bodies make on their own receiver goes back through the VM.

func ioExceptionDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      IOExceptionClass,
		SuperName: "java/lang/Exception",
		Access:    AccessPublic,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: exceptionSuperInit("()V")},
			{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic, Body: exceptionSuperInit("(Ljava/lang/String;)V")},
		},
	}
}

// exceptionSuperInit is the body of an exception constructor that does nothing
// but hand its arguments to java/lang/Exception, which is where the message a
// catch block reads is kept.
func exceptionSuperInit(descriptor string) ContextMethod {
	return func(call *Invocation, arguments []Value) (Value, error) {
		receiver, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		_, err = call.InvokeSpecial(receiver, "java/lang/Exception", "<init>", descriptor, arguments[1:]...)
		return VoidValue(), err
	}
}

func inputStreamDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      InputStreamClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessAbstract,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessProtected, Body: emptyConstructor},
			{Name: "read", Descriptor: "()I", Access: AccessPublic | AccessAbstract, Throws: []string{"java/io/IOException"}},
			{Name: "read", Descriptor: "([B)I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: inputStreamReadArray},
			{Name: "read", Descriptor: "([BII)I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: inputStreamReadRange},
			{Name: "skip", Descriptor: "(J)J", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: inputStreamSkip},
			{Name: "available", Descriptor: "()I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: constantInt(0)},
			{Name: "close", Descriptor: "()V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: doNothing},
			{Name: "mark", Descriptor: "(I)V", Access: AccessPublic, Body: doNothing},
			{Name: "markSupported", Descriptor: "()Z", Access: AccessPublic, Body: constantInt(0)},
			{Name: "reset", Descriptor: "()V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: inputStreamReset},
		},
	}
}

func inputStreamReadArray(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	data, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	array, err := guestArray(data)
	if err != nil {
		return VoidValue(), err
	}
	return call.InvokeVirtual(stream, "read", "([BII)I", ReferenceValue(data), IntValue(0), IntValue(int32(array.Length())))
}

func inputStreamReadRange(call *Invocation, arguments []Value) (Value, error) {
	stream, data, offset, length, err := streamRangeArguments(arguments)
	if err != nil {
		return VoidValue(), err
	}
	if length == 0 {
		return IntValue(0), nil
	}
	count := int32(0)
	for count < length {
		value, err := call.InvokeVirtual(stream, "read", "()I")
		if err != nil {
			return VoidValue(), err
		}
		next, err := value.Int32()
		if err != nil {
			return VoidValue(), err
		}
		if next < 0 {
			break
		}
		// read answers an unsigned byte and the array holds a signed one, the
		// narrowing bastore would have done. Storing 255 where the guest reads
		// -1 is a byte a game decodes as a different number.
		if err := data.Store(int(offset+count), IntValue(int32(int8(next)))); err != nil {
			return VoidValue(), err
		}
		count++
	}
	if count == 0 {
		return IntValue(-1), nil
	}
	return IntValue(count), nil
}

func inputStreamSkip(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	count, err := nativeLong(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	remaining := count
	for remaining > 0 {
		value, err := call.InvokeVirtual(stream, "read", "()I")
		if err != nil {
			return VoidValue(), err
		}
		next, err := value.Int32()
		if err != nil {
			return VoidValue(), err
		}
		if next < 0 {
			break
		}
		remaining--
	}
	return LongValue(count - remaining), nil
}

func inputStreamReset(_ *Invocation, arguments []Value) (Value, error) {
	if _, err := requireObject(arguments, 0); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), guestException(IOExceptionClass, "mark/reset is not supported")
}

func outputStreamDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      OutputStreamClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessAbstract,
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: emptyConstructor},
			{Name: "write", Descriptor: "(I)V", Access: AccessPublic | AccessAbstract, Throws: []string{"java/io/IOException"}},
			{Name: "write", Descriptor: "([B)V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: outputStreamWriteArray},
			{Name: "write", Descriptor: "([BII)V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: outputStreamWriteRange},
			{Name: "flush", Descriptor: "()V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: doNothing},
			{Name: "close", Descriptor: "()V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: doNothing},
		},
	}
}

func outputStreamWriteArray(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	buffer, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	array, err := guestArray(buffer)
	if err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeVirtual(stream, "write", "([BII)V", ReferenceValue(buffer), IntValue(0), IntValue(int32(array.Length())))
	return VoidValue(), err
}

func outputStreamWriteRange(call *Invocation, arguments []Value) (Value, error) {
	stream, buffer, offset, length, err := writeRangeArguments(arguments)
	if err != nil {
		return VoidValue(), err
	}
	for index := int32(0); index < length; index++ {
		element, err := buffer.Load(int(offset + index))
		if err != nil {
			return VoidValue(), err
		}
		value, err := element.Int32()
		if err != nil {
			return VoidValue(), err
		}
		if _, err := call.InvokeVirtual(stream, "write", "(I)V", IntValue(value)); err != nil {
			return VoidValue(), err
		}
	}
	return VoidValue(), nil
}

func byteArrayInputStreamDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      ByteArrayInputStreamClass,
		SuperName: InputStreamClass,
		Access:    AccessPublic,
		// The four names are the specification's own, and they are protected
		// rather than private because it declares them that way: a title
		// subclasses this stream and reads `buf` where it wants the bytes
		// without the copy, exactly as it does on the sink. A private field
		// named something else answers no such read, and the miss surfaces
		// wherever the subclass first touches it rather than at the class.
		Fields: []FieldDefinition{
			{Name: "buf", Descriptor: "[B", Access: AccessProtected},
			{Name: "pos", Descriptor: "I", Access: AccessProtected},
			// Where the stream ends, which is the array's end only for the
			// constructor that takes the whole array. A title that keeps
			// several records in one buffer reads each of them by handing the
			// same array a different window, so the end has to be per stream.
			{Name: "count", Descriptor: "I", Access: AccessProtected},
			// Where reset goes back to. A stream over an array is always
			// markable, and both constructors set this to where reading
			// starts, so a title that resets without ever marking gets the
			// beginning of its window rather than an exception.
			{Name: "mark", Descriptor: "I", Access: AccessProtected},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "([B)V", Access: AccessPublic, Body: byteArrayInputStreamInit},
			{Name: "<init>", Descriptor: "([BII)V", Access: AccessPublic, Body: byteArrayInputStreamInitRange},
			{Name: "read", Descriptor: "()I", Access: AccessPublic, Body: byteArrayInputStreamRead},
			{Name: "read", Descriptor: "([BII)I", Access: AccessPublic, Body: byteArrayInputStreamReadRange},
			{Name: "skip", Descriptor: "(J)J", Access: AccessPublic, Body: byteArrayInputStreamSkip},
			{Name: "available", Descriptor: "()I", Access: AccessPublic, Body: byteArrayInputStreamAvailable},
			{Name: "mark", Descriptor: "(I)V", Access: AccessPublic, Body: byteArrayInputStreamMark},
			{Name: "markSupported", Descriptor: "()Z", Access: AccessPublic, Body: constantInt(1)},
			{Name: "reset", Descriptor: "()V", Access: AccessPublic, Body: byteArrayInputStreamReset},
		},
	}
}

func byteArrayInputStreamInit(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	data, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if err := call.vm.SetField(stream, ByteArrayInputStreamClass, "buf", "[B", ReferenceValue(data)); err != nil {
		return VoidValue(), err
	}
	array, err := guestArray(data)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), setIntField(call.vm, stream, ByteArrayInputStreamClass, "count", int32(array.Length()))
}

// byteArrayInputStreamMark and byteArrayInputStreamReset are the pair a title
// uses to read the same bytes twice. A stream over an array can always go
// back, so markSupported is true here and reset never fails — the read-ahead
// limit the caller passes has nothing to bound, because nothing is buffered.
//
// Leaving these to the superclass is what a resource stream used to do, and
// there reset throws: one title marks nothing, reads a header, resets to take
// the same bytes again, catches the IOException its own decoder never
// expected, and paints the image it did not build.
func byteArrayInputStreamMark(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	position, err := intField(call.vm, stream, ByteArrayInputStreamClass, "pos")
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), setIntField(call.vm, stream, ByteArrayInputStreamClass, "mark", position)
}

func byteArrayInputStreamReset(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	mark, err := intField(call.vm, stream, ByteArrayInputStreamClass, "mark")
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), setIntField(call.vm, stream, ByteArrayInputStreamClass, "pos", mark)
}

// byteArrayInputStreamInitRange opens a window on the array rather than the
// whole of it. The specification clamps the window to what the array holds
// rather than refusing an over-long one, and a negative offset or length is
// the caller's own error; both are what a title that reads a record out of a
// larger buffer depends on.
func byteArrayInputStreamInitRange(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	data, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	offset, err := nativeInt(arguments, 2)
	if err != nil {
		return VoidValue(), err
	}
	length, err := nativeInt(arguments, 3)
	if err != nil {
		return VoidValue(), err
	}
	array, err := guestArray(data)
	if err != nil {
		return VoidValue(), err
	}
	size := int32(array.Length())
	if offset < 0 || length < 0 || offset > size {
		return VoidValue(), guestException("java/lang/IndexOutOfBoundsException", "ByteArrayInputStream range")
	}
	limit := offset + length
	if limit > size || limit < 0 {
		limit = size
	}
	if err := call.vm.SetField(stream, ByteArrayInputStreamClass, "buf", "[B", ReferenceValue(data)); err != nil {
		return VoidValue(), err
	}
	if err := setIntField(call.vm, stream, ByteArrayInputStreamClass, "pos", offset); err != nil {
		return VoidValue(), err
	}
	// The mark starts where reading does. The class documentation describes a
	// reset on a windowed stream as reaching the whole array, which is the
	// text every edition of it carries and not what any implementation of it
	// does; a window that reset widened would let a record reader walk into
	// the record before it.
	if err := setIntField(call.vm, stream, ByteArrayInputStreamClass, "mark", offset); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), setIntField(call.vm, stream, ByteArrayInputStreamClass, "count", limit)
}

// byteArrayInputStreamState reads the fields every method here starts from:
// the array, where reading is, and where it stops.
func byteArrayInputStreamState(vm *VM, stream *Object) (*Array, int32, int32, error) {
	value, err := vm.Field(stream, ByteArrayInputStreamClass, "buf", "[B")
	if err != nil {
		return nil, 0, 0, err
	}
	object, err := value.Reference()
	if err != nil {
		return nil, 0, 0, err
	}
	array, err := guestArray(object)
	if err != nil {
		return nil, 0, 0, err
	}
	position, err := intField(vm, stream, ByteArrayInputStreamClass, "pos")
	if err != nil {
		return nil, 0, 0, err
	}
	limit, err := intField(vm, stream, ByteArrayInputStreamClass, "count")
	if err != nil {
		return nil, 0, 0, err
	}
	if limit > int32(array.Length()) {
		limit = int32(array.Length())
	}
	return array, position, limit, nil
}

func byteArrayInputStreamRead(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	data, position, limit, err := byteArrayInputStreamState(call.vm, stream)
	if err != nil {
		return VoidValue(), err
	}
	if position >= limit {
		return IntValue(-1), nil
	}
	element, err := data.Load(int(position))
	if err != nil {
		return VoidValue(), err
	}
	value, err := element.Int32()
	if err != nil {
		return VoidValue(), err
	}
	if err := setIntField(call.vm, stream, ByteArrayInputStreamClass, "pos", position+1); err != nil {
		return VoidValue(), err
	}
	return IntValue(value & 0xff), nil
}

func byteArrayInputStreamReadRange(call *Invocation, arguments []Value) (Value, error) {
	stream, output, offset, length, err := streamRangeArguments(arguments)
	if err != nil {
		return VoidValue(), err
	}
	data, position, limit, err := byteArrayInputStreamState(call.vm, stream)
	if err != nil {
		return VoidValue(), err
	}
	remaining := limit - position
	if remaining <= 0 {
		if length == 0 {
			return IntValue(0), nil
		}
		return IntValue(-1), nil
	}
	count := length
	if remaining < count {
		count = remaining
	}
	values, err := data.LoadRange(int(position), int(count))
	if err != nil {
		return VoidValue(), err
	}
	if err := output.StoreRange(int(offset), values); err != nil {
		return VoidValue(), err
	}
	if err := setIntField(call.vm, stream, ByteArrayInputStreamClass, "pos", position+count); err != nil {
		return VoidValue(), err
	}
	return IntValue(count), nil
}

// byteArrayInputStreamSkip steps over bytes without reading them one at a
// time. The superclass skips by calling read for every byte, which a game
// stepping over a multi-kilobyte chunk of its own archive pays a call for; the
// whole stream is already in memory here.
func byteArrayInputStreamSkip(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	count, err := nativeLong(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	_, position, limit, err := byteArrayInputStreamState(call.vm, stream)
	if err != nil {
		return VoidValue(), err
	}
	available := int64(limit) - int64(position)
	skipped := count
	if skipped > available {
		skipped = available
	}
	if skipped < 0 {
		return LongValue(0), nil
	}
	if err := setIntField(call.vm, stream, ByteArrayInputStreamClass, "pos", position+int32(skipped)); err != nil {
		return VoidValue(), err
	}
	return LongValue(skipped), nil
}

func byteArrayInputStreamAvailable(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	_, position, limit, err := byteArrayInputStreamState(call.vm, stream)
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(limit - position), nil
}

func byteArrayOutputStreamDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      ByteArrayOutputStreamClass,
		SuperName: OutputStreamClass,
		Access:    AccessPublic,
		Fields: []FieldDefinition{
			{Name: "buf", Descriptor: "[B", Access: AccessProtected},
			{Name: "count", Descriptor: "I", Access: AccessProtected},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessPublic, Body: byteArrayOutputStreamInitDefault},
			{Name: "<init>", Descriptor: "(I)V", Access: AccessPublic, Body: byteArrayOutputStreamInit},
			{Name: "write", Descriptor: "(I)V", Access: AccessPublic, Body: byteArrayOutputStreamWrite},
			{Name: "write", Descriptor: "([BII)V", Access: AccessPublic, Body: byteArrayOutputStreamWriteRange},
			{Name: "toByteArray", Descriptor: "()[B", Access: AccessPublic, Body: byteArrayOutputStreamToByteArray},
			{Name: "size", Descriptor: "()I", Access: AccessPublic, Body: byteArrayOutputStreamSize},
			{Name: "reset", Descriptor: "()V", Access: AccessPublic, Body: byteArrayOutputStreamReset},
			{Name: "toString", Descriptor: "()Ljava/lang/String;", Access: AccessPublic, Body: byteArrayOutputStreamToString},
		},
	}
}

// byteArrayOutputStreamToString is the bytes written so far as text, decoded
// with the platform's charset: `new String(buf, 0, count)`, which is what the
// class library answers and exactly what that constructor answers here. It
// reads the two protected fields, so a subclass that wrote them is read as it
// left them, with the String constructor's refusals for a count that does not
// fit the buffer and a buffer that is null.
func byteArrayOutputStreamToString(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	buffer, _, count, err := byteArrayOutputStreamBuffer(call.vm, stream)
	if err != nil {
		return VoidValue(), err
	}
	data, err := ByteArraySnapshot(buffer)
	if err != nil {
		return VoidValue(), err
	}
	if count < 0 || int(count) > len(data) {
		return VoidValue(), guestException("java/lang/IndexOutOfBoundsException", "String byte range")
	}
	return call.vm.stringResult(call.vm.decodePlatformBytes(data[:count]))
}

// byteArrayOutputStreamDefaultCapacity is what a stream constructed without a
// size starts at, the same 32 bytes the class file had.
const byteArrayOutputStreamDefaultCapacity = 32

func byteArrayOutputStreamInitDefault(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeSpecial(stream, ByteArrayOutputStreamClass, "<init>", "(I)V", IntValue(byteArrayOutputStreamDefaultCapacity))
	return VoidValue(), err
}

func byteArrayOutputStreamInit(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	size, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if size < 0 {
		return VoidValue(), guestException("java/lang/IllegalArgumentException", "negative buffer size")
	}
	buffer, err := call.vm.newArray(Type{Kind: TypeByte}, size)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), call.vm.SetField(stream, ByteArrayOutputStreamClass, "buf", "[B", ReferenceValue(buffer))
}

// byteArrayOutputStreamBuffer reads the buffer and the count together, since
// nothing here can be done with one without the other.
func byteArrayOutputStreamBuffer(vm *VM, stream *Object) (*Object, *Array, int32, error) {
	value, err := vm.Field(stream, ByteArrayOutputStreamClass, "buf", "[B")
	if err != nil {
		return nil, nil, 0, err
	}
	object, err := value.Reference()
	if err != nil {
		return nil, nil, 0, err
	}
	array, err := guestArray(object)
	if err != nil {
		return nil, nil, 0, err
	}
	count, err := intField(vm, stream, ByteArrayOutputStreamClass, "count")
	if err != nil {
		return nil, nil, 0, err
	}
	return object, array, count, nil
}

// growByteArrayOutputStream makes room for capacity bytes, doubling rather
// than growing to exactly what was asked for so that a game writing one byte
// at a time does not copy the whole buffer on every call.
func growByteArrayOutputStream(vm *VM, stream *Object, capacity int32) (*Array, error) {
	_, array, count, err := byteArrayOutputStreamBuffer(vm, stream)
	if err != nil {
		return nil, err
	}
	if capacity <= int32(array.Length()) {
		return array, nil
	}
	next := int32(array.Length()) * 2
	if next < capacity {
		next = capacity
	}
	grown, err := vm.newArray(Type{Kind: TypeByte}, next)
	if err != nil {
		return nil, err
	}
	values, err := array.LoadRange(0, int(count))
	if err != nil {
		return nil, err
	}
	grownArray, err := guestArray(grown)
	if err != nil {
		return nil, err
	}
	if err := grownArray.StoreRange(0, values); err != nil {
		return nil, err
	}
	if err := vm.SetField(stream, ByteArrayOutputStreamClass, "buf", "[B", ReferenceValue(grown)); err != nil {
		return nil, err
	}
	return grownArray, nil
}

func byteArrayOutputStreamWrite(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	count, err := intField(call.vm, stream, ByteArrayOutputStreamClass, "count")
	if err != nil {
		return VoidValue(), err
	}
	buffer, err := growByteArrayOutputStream(call.vm, stream, count+1)
	if err != nil {
		return VoidValue(), err
	}
	if err := buffer.Store(int(count), IntValue(int32(int8(value)))); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), setIntField(call.vm, stream, ByteArrayOutputStreamClass, "count", count+1)
}

func byteArrayOutputStreamWriteRange(call *Invocation, arguments []Value) (Value, error) {
	stream, source, offset, length, err := writeRangeArguments(arguments)
	if err != nil {
		return VoidValue(), err
	}
	count, err := intField(call.vm, stream, ByteArrayOutputStreamClass, "count")
	if err != nil {
		return VoidValue(), err
	}
	buffer, err := growByteArrayOutputStream(call.vm, stream, count+length)
	if err != nil {
		return VoidValue(), err
	}
	values, err := source.LoadRange(int(offset), int(length))
	if err != nil {
		return VoidValue(), err
	}
	if err := buffer.StoreRange(int(count), values); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), setIntField(call.vm, stream, ByteArrayOutputStreamClass, "count", count+length)
}

func byteArrayOutputStreamToByteArray(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	_, buffer, count, err := byteArrayOutputStreamBuffer(call.vm, stream)
	if err != nil {
		return VoidValue(), err
	}
	values, err := buffer.LoadRange(0, int(count))
	if err != nil {
		return VoidValue(), err
	}
	result, err := call.vm.newArray(Type{Kind: TypeByte}, count)
	if err != nil {
		return VoidValue(), err
	}
	if err := SetArrayRange(result, 0, values); err != nil {
		return VoidValue(), err
	}
	return ReferenceValue(result), nil
}

func byteArrayOutputStreamSize(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	count, err := intField(call.vm, stream, ByteArrayOutputStreamClass, "count")
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(count), nil
}

func byteArrayOutputStreamReset(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), setIntField(call.vm, stream, ByteArrayOutputStreamClass, "count", 0)
}

func dataInputDefinition() ClassDefinition {
	abstract := AccessPublic | AccessAbstract
	throws := []string{IOExceptionClass}
	return ClassDefinition{
		Name:      DataInputClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessInterface | AccessAbstract,
		Methods: []MethodDefinition{
			{Name: "readFully", Descriptor: "([B)V", Access: abstract, Throws: throws},
			{Name: "readFully", Descriptor: "([BII)V", Access: abstract, Throws: throws},
			{Name: "skipBytes", Descriptor: "(I)I", Access: abstract, Throws: throws},
			{Name: "readBoolean", Descriptor: "()Z", Access: abstract, Throws: throws},
			{Name: "readByte", Descriptor: "()B", Access: abstract, Throws: throws},
			{Name: "readUnsignedByte", Descriptor: "()I", Access: abstract, Throws: throws},
			{Name: "readShort", Descriptor: "()S", Access: abstract, Throws: throws},
			{Name: "readUnsignedShort", Descriptor: "()I", Access: abstract, Throws: throws},
			{Name: "readChar", Descriptor: "()C", Access: abstract, Throws: throws},
			{Name: "readInt", Descriptor: "()I", Access: abstract, Throws: throws},
			{Name: "readLong", Descriptor: "()J", Access: abstract, Throws: throws},
			{Name: "readFloat", Descriptor: "()F", Access: abstract, Throws: throws},
			{Name: "readDouble", Descriptor: "()D", Access: abstract, Throws: throws},
			{Name: "readUTF", Descriptor: "()Ljava/lang/String;", Access: abstract, Throws: throws},
		},
	}
}

func dataOutputDefinition() ClassDefinition {
	abstract := AccessPublic | AccessAbstract
	throws := []string{IOExceptionClass}
	return ClassDefinition{
		Name:      DataOutputClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessInterface | AccessAbstract,
		Methods: []MethodDefinition{
			{Name: "write", Descriptor: "(I)V", Access: abstract, Throws: throws},
			{Name: "write", Descriptor: "([B)V", Access: abstract, Throws: throws},
			{Name: "write", Descriptor: "([BII)V", Access: abstract, Throws: throws},
			{Name: "writeBoolean", Descriptor: "(Z)V", Access: abstract, Throws: throws},
			{Name: "writeByte", Descriptor: "(I)V", Access: abstract, Throws: throws},
			{Name: "writeShort", Descriptor: "(I)V", Access: abstract, Throws: throws},
			{Name: "writeChar", Descriptor: "(I)V", Access: abstract, Throws: throws},
			{Name: "writeInt", Descriptor: "(I)V", Access: abstract, Throws: throws},
			{Name: "writeLong", Descriptor: "(J)V", Access: abstract, Throws: throws},
			{Name: "writeFloat", Descriptor: "(F)V", Access: abstract, Throws: throws},
			{Name: "writeDouble", Descriptor: "(D)V", Access: abstract, Throws: throws},
			{Name: "writeChars", Descriptor: "(Ljava/lang/String;)V", Access: abstract, Throws: throws},
			{Name: "writeUTF", Descriptor: "(Ljava/lang/String;)V", Access: abstract, Throws: throws},
		},
	}
}

func dataInputStreamDefinition() ClassDefinition {
	return ClassDefinition{
		Name:       DataInputStreamClass,
		SuperName:  InputStreamClass,
		Interfaces: []string{DataInputClass},
		Access:     AccessPublic,
		Fields: []FieldDefinition{
			{Name: "input", Descriptor: "Ljava/io/InputStream;", Access: AccessPrivate},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "(Ljava/io/InputStream;)V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputStreamInit},
			{Name: "read", Descriptor: "()I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputDelegate("read", "()I")},
			{Name: "read", Descriptor: "([BII)I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputDelegate("read", "([BII)I")},
			{Name: "available", Descriptor: "()I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputDelegate("available", "()I")},
			{Name: "skip", Descriptor: "(J)J", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputDelegate("skip", "(J)J")},
			{Name: "close", Descriptor: "()V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputDelegate("close", "()V")},
			// The wrapper marks and resets whatever it wraps. Nothing is
			// buffered here — every read goes straight through — so handing
			// the three calls on is the whole of the contract, and a title
			// that resets the stream it is decoding from gets the answer of
			// the stream underneath rather than the refusal of the abstract
			// superclass.
			{Name: "mark", Descriptor: "(I)V", Access: AccessPublic, Body: dataInputDelegate("mark", "(I)V")},
			{Name: "markSupported", Descriptor: "()Z", Access: AccessPublic, Body: dataInputDelegate("markSupported", "()Z")},
			{Name: "reset", Descriptor: "()V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputDelegate("reset", "()V")},
			{Name: "skipBytes", Descriptor: "(I)I", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataInputSkipBytes},
			{Name: "readBoolean", Descriptor: "()Z", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataInputReadBoolean},
			{Name: "readByte", Descriptor: "()B", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadByte},
			{Name: "readShort", Descriptor: "()S", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadShort},
			{Name: "readUnsignedByte", Descriptor: "()I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadUnsignedByte},
			{Name: "readUnsignedShort", Descriptor: "()I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadUnsignedShort},
			{Name: "readChar", Descriptor: "()C", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadChar},
			{Name: "readInt", Descriptor: "()I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadInt},
			{Name: "readLong", Descriptor: "()J", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadLong},
			{Name: "readFloat", Descriptor: "()F", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadFloat},
			{Name: "readDouble", Descriptor: "()D", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadDouble},
			{Name: "readUTF", Descriptor: "()Ljava/lang/String;", Access: AccessPublic | AccessNative, Throws: []string{"java/io/IOException"}},
			// The static form reads a string out of any DataInput, a title's
			// own included; see dataInputReadUTFFrom.
			{Name: "readUTF", Descriptor: "(Ljava/io/DataInput;)Ljava/lang/String;", Access: AccessPublic | AccessStatic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataInputReadUTFFrom},
			{Name: "readFully", Descriptor: "([B)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataInputReadFullyArray},
			{Name: "readFully", Descriptor: "([BII)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataInputReadFullyRange},
		},
	}
}

// dataInputReadFullyArray and dataInputReadFullyRange fill the caller's buffer
// or raise. A title reads a fixed-size record with these and then decodes it
// by offset, so a short read that answered with a count would leave it
// decoding whatever the buffer held before.
func dataInputReadFullyArray(call *Invocation, arguments []Value) (Value, error) {
	array, err := requireObject(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	data, err := ByteArraySnapshot(array)
	if err != nil {
		return VoidValue(), err
	}
	return dataInputReadFully(call, arguments, 0, int32(len(data)))
}

func dataInputReadFullyRange(call *Invocation, arguments []Value) (Value, error) {
	offset, err := nativeInt(arguments, 2)
	if err != nil {
		return VoidValue(), err
	}
	length, err := nativeInt(arguments, 3)
	if err != nil {
		return VoidValue(), err
	}
	return dataInputReadFully(call, arguments, offset, length)
}

// dataInputReadFully reads through the stream's own read([BII), because a
// subclass that replaced it is what a game wrapped this around.
func dataInputReadFully(call *Invocation, arguments []Value, offset, length int32) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	array, err := requireObject(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	data, err := ByteArraySnapshot(array)
	if err != nil {
		return VoidValue(), err
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(data)) {
		return VoidValue(), guestException("java/lang/IndexOutOfBoundsException", "readFully range")
	}
	for filled := int32(0); filled < length; {
		result, readErr := call.InvokeVirtual(stream, "read", "([BII)I",
			ReferenceValue(array), IntValue(offset+filled), IntValue(length-filled))
		if readErr != nil {
			return VoidValue(), readErr
		}
		count, countErr := result.Int32()
		if countErr != nil {
			return VoidValue(), countErr
		}
		if count <= 0 {
			return VoidValue(), guestException(eofExceptionClass,
				fmt.Sprintf("readFully wanted %d bytes and reached the end after %d", length, filled))
		}
		filled += count
	}
	return VoidValue(), nil
}

// dataInputReadUTFFrom is DataInputStream.readUTF(DataInput): a two-byte
// length from the input's own readUnsignedShort, then that many bytes from its
// own readFully, both asked through the interface, so a title's DataInput
// reads its record its own way and the end of it is that input's
// EOFException. The bytes are decoded as the class library decodes them; see
// decodeDataUTF. A null input is the NullPointerException the call dereferences
// it into.
func dataInputReadUTFFrom(call *Invocation, arguments []Value) (Value, error) {
	input, err := nativeReference(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	if input == nil {
		return VoidValue(), guestException("java/lang/NullPointerException", "readUTF from a null DataInput")
	}
	lengthValue, err := call.InvokeVirtual(input, "readUnsignedShort", "()I")
	if err != nil {
		return VoidValue(), err
	}
	length, err := lengthValue.Int32()
	if err != nil {
		return VoidValue(), err
	}
	// A title's own input answers whatever it answers. The array is made as
	// the class library makes it, so a negative length is that array's
	// NegativeArraySizeException and a long one is bounded as every array is.
	data, err := call.vm.newArray(Type{Kind: TypeByte}, length)
	if err != nil {
		return VoidValue(), err
	}
	if _, err := call.InvokeVirtual(input, "readFully", "([BII)V", ReferenceValue(data), IntValue(0), IntValue(length)); err != nil {
		return VoidValue(), err
	}
	bytes, err := ByteArraySnapshot(data)
	if err != nil {
		return VoidValue(), err
	}
	units, err := decodeDataUTF(bytes)
	if err != nil {
		return VoidValue(), guestException("java/io/UTFDataFormatException", err.Error())
	}
	return call.vm.stringResult(string(utf16.Decode(units)))
}

// decodeDataUTF decodes the bytes readUTF(DataInput) read, as the class
// library decodes them: a byte below 0x80 is a character, 110xxxxx 10xxxxxx
// and 1110xxxx 10xxxxxx 10xxxxxx are one each, and anything else — a
// continuation byte where a character starts, a lead byte of 0xf0 or more, a
// continuation byte that is not one, a sequence the length cuts short — is
// malformed. Nothing is refused for being a zero byte or a longer form than it
// needed, which the class library does not refuse either.
func decodeDataUTF(data []byte) ([]uint16, error) {
	units := make([]uint16, 0, len(data))
	for index := 0; index < len(data); {
		first := data[index]
		switch first >> 4 {
		case 0, 1, 2, 3, 4, 5, 6, 7:
			units = append(units, uint16(first))
			index++
		case 12, 13:
			if index+2 > len(data) || data[index+1]&0xc0 != 0x80 {
				return nil, fmt.Errorf("malformed two-byte sequence at %d", index)
			}
			units = append(units, uint16(first&0x1f)<<6|uint16(data[index+1]&0x3f))
			index += 2
		case 14:
			if index+3 > len(data) || data[index+1]&0xc0 != 0x80 || data[index+2]&0xc0 != 0x80 {
				return nil, fmt.Errorf("malformed three-byte sequence at %d", index)
			}
			units = append(units, uint16(first&0x0f)<<12|uint16(data[index+1]&0x3f)<<6|uint16(data[index+2]&0x3f))
			index += 3
		default:
			return nil, fmt.Errorf("malformed input around byte %d", index)
		}
	}
	return units, nil
}

func dataInputStreamInit(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	input, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), call.vm.SetField(stream, DataInputStreamClass, "input", "Ljava/io/InputStream;", ReferenceValue(input))
}

// wrappedInput answers the stream a DataInputStream reads through. The class
// calls it directly rather than through its own read, so a subclass that
// overrides read does not change what readInt decodes — which is what the
// class file did, and what a game that wraps its archive entry expects.
func wrappedInput(vm *VM, stream *Object) (*Object, error) {
	value, err := vm.Field(stream, DataInputStreamClass, "input", "Ljava/io/InputStream;")
	if err != nil {
		return nil, err
	}
	input, err := value.Reference()
	if err != nil {
		return nil, err
	}
	if input == nil {
		return nil, guestException("java/lang/NullPointerException", "data input stream has no source")
	}
	return input, nil
}

func dataInputDelegate(name, descriptor string) ContextMethod {
	return func(call *Invocation, arguments []Value) (Value, error) {
		stream, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		input, err := wrappedInput(call.vm, stream)
		if err != nil {
			return VoidValue(), err
		}
		return call.InvokeVirtual(input, name, descriptor, arguments[1:]...)
	}
}

func dataInputSkipBytes(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	count, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	input, err := wrappedInput(call.vm, stream)
	if err != nil {
		return VoidValue(), err
	}
	skipped, err := call.InvokeVirtual(input, "skip", "(J)J", LongValue(int64(count)))
	if err != nil {
		return VoidValue(), err
	}
	value, err := skipped.Int64()
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(int32(value)), nil
}

// eofExceptionClass is what every DataInput read raises at the end of its
// input. A title that reads records until the stream runs out catches it by
// name, and a plain IOException would go past that handler.
const eofExceptionClass = "java/io/EOFException"

// dataInputRequired reads one byte and refuses to answer a short stream with a
// value, because every fixed-width read below is decoding a record whose
// length the caller already committed to.
func dataInputRequired(call *Invocation, stream *Object) (int32, error) {
	input, err := wrappedInput(call.vm, stream)
	if err != nil {
		return 0, err
	}
	result, err := call.InvokeVirtual(input, "read", "()I")
	if err != nil {
		return 0, err
	}
	value, err := result.Int32()
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, guestException(eofExceptionClass, "unexpected end of stream")
	}
	return value & 0xff, nil
}

func dataInputReadBoolean(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	result, err := call.InvokeVirtual(stream, "readByte", "()B")
	if err != nil {
		return VoidValue(), err
	}
	value, err := result.Int32()
	if err != nil {
		return VoidValue(), err
	}
	if value != 0 {
		return IntValue(1), nil
	}
	return IntValue(0), nil
}

func dataInputReadByte(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := dataInputRequired(call, stream)
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(int32(int8(value))), nil
}

// dataInputReadUnsignedByte and dataInputReadChar complete the DataInput
// surface a title of this era reads a record with. A char is two bytes, most
// significant first, exactly like the short beside it — the difference is only
// that it is not sign-extended.
func dataInputReadUnsignedByte(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := dataInputRequired(call, stream)
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(value), nil
}

func dataInputReadChar(call *Invocation, arguments []Value) (Value, error) {
	return dataInputReadUnsignedShort(call, arguments)
}

func dataInputReadShort(call *Invocation, arguments []Value) (Value, error) {
	value, err := dataInputReadUnsignedShort(call, arguments)
	if err != nil {
		return VoidValue(), err
	}
	unsigned, err := value.Int32()
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(int32(int16(unsigned))), nil
}

func dataInputReadUnsignedShort(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	high, err := dataInputRequired(call, stream)
	if err != nil {
		return VoidValue(), err
	}
	low, err := dataInputRequired(call, stream)
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(high<<8 | low), nil
}

func dataInputReadInt(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	result := int32(0)
	for index := 0; index < 4; index++ {
		value, err := dataInputRequired(call, stream)
		if err != nil {
			return VoidValue(), err
		}
		result = result<<8 | value
	}
	return IntValue(result), nil
}

func dataInputReadLong(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	high, err := call.InvokeVirtual(stream, "readInt", "()I")
	if err != nil {
		return VoidValue(), err
	}
	low, err := call.InvokeVirtual(stream, "readInt", "()I")
	if err != nil {
		return VoidValue(), err
	}
	highValue, err := high.Int32()
	if err != nil {
		return VoidValue(), err
	}
	lowValue, err := low.Int32()
	if err != nil {
		return VoidValue(), err
	}
	return LongValue(int64(highValue)<<32 | int64(uint32(lowValue))), nil
}

func dataInputReadFloat(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	bits, err := call.InvokeVirtual(stream, "readInt", "()I")
	if err != nil {
		return VoidValue(), err
	}
	value, err := bits.Int32()
	if err != nil {
		return VoidValue(), err
	}
	return FloatValue(math.Float32frombits(uint32(value))), nil
}

func dataInputReadDouble(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	bits, err := call.InvokeVirtual(stream, "readLong", "()J")
	if err != nil {
		return VoidValue(), err
	}
	value, err := bits.Int64()
	if err != nil {
		return VoidValue(), err
	}
	return DoubleValue(math.Float64frombits(uint64(value))), nil
}

func dataOutputStreamDefinition() ClassDefinition {
	return ClassDefinition{
		Name:       DataOutputStreamClass,
		SuperName:  OutputStreamClass,
		Interfaces: []string{DataOutputClass},
		Access:     AccessPublic,
		Fields: []FieldDefinition{
			{Name: "out", Descriptor: "Ljava/io/OutputStream;", Access: AccessProtected},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "(Ljava/io/OutputStream;)V", Access: AccessPublic, Body: dataOutputStreamInit},
			{Name: "write", Descriptor: "(I)V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataOutputDelegate("write", "(I)V")},
			{Name: "write", Descriptor: "([BII)V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataOutputDelegate("write", "([BII)V")},
			{Name: "flush", Descriptor: "()V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataOutputDelegate("flush", "()V")},
			{Name: "close", Descriptor: "()V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataOutputDelegate("close", "()V")},
			{Name: "writeBoolean", Descriptor: "(Z)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataOutputWriteBoolean},
			{Name: "writeByte", Descriptor: "(I)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataOutputWriteByte},
			{Name: "writeShort", Descriptor: "(I)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataOutputWriteShort},
			{Name: "writeChar", Descriptor: "(I)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataOutputWriteChar},
			{Name: "writeInt", Descriptor: "(I)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataOutputWriteInt},
			{Name: "writeLong", Descriptor: "(J)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataOutputWriteLong},
			{Name: "writeFloat", Descriptor: "(F)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataOutputWriteFloat},
			{Name: "writeDouble", Descriptor: "(D)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataOutputWriteDouble},
			{Name: "writeUTF", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataOutputWriteUTF},
			{Name: "writeChars", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataOutputWriteChars},
		},
	}
}

func dataOutputStreamInit(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	out, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), call.vm.SetField(stream, DataOutputStreamClass, "out", "Ljava/io/OutputStream;", ReferenceValue(out))
}

func wrappedOutput(vm *VM, stream *Object) (*Object, error) {
	value, err := vm.Field(stream, DataOutputStreamClass, "out", "Ljava/io/OutputStream;")
	if err != nil {
		return nil, err
	}
	out, err := value.Reference()
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, guestException("java/lang/NullPointerException", "data output stream has no sink")
	}
	return out, nil
}

func dataOutputDelegate(name, descriptor string) ContextMethod {
	return func(call *Invocation, arguments []Value) (Value, error) {
		stream, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		out, err := wrappedOutput(call.vm, stream)
		if err != nil {
			return VoidValue(), err
		}
		return call.InvokeVirtual(out, name, descriptor, arguments[1:]...)
	}
}

// dataOutputByte writes one byte to the wrapped stream. Every fixed-width
// write below is this, a byte at a time, most significant first.
func dataOutputByte(call *Invocation, stream *Object, value int32) error {
	out, err := wrappedOutput(call.vm, stream)
	if err != nil {
		return err
	}
	_, err = call.InvokeVirtual(out, "write", "(I)V", IntValue(value))
	return err
}

func dataOutputWriteBoolean(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	written := int32(0)
	if value != 0 {
		written = 1
	}
	return VoidValue(), dataOutputByte(call, stream, written)
}

func dataOutputWriteByte(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), dataOutputByte(call, stream, value)
}

func dataOutputWriteShort(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if err := dataOutputByte(call, stream, int32(uint32(value)>>8)); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), dataOutputByte(call, stream, value)
}

func dataOutputWriteChar(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeVirtual(stream, "writeShort", "(I)V", IntValue(value))
	return VoidValue(), err
}

func dataOutputWriteInt(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	for shift := 24; shift >= 0; shift -= 8 {
		if err := dataOutputByte(call, stream, int32(uint32(value)>>uint(shift))); err != nil {
			return VoidValue(), err
		}
	}
	return VoidValue(), nil
}

func dataOutputWriteLong(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := nativeLong(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if _, err := call.InvokeVirtual(stream, "writeInt", "(I)V", IntValue(int32(uint64(value)>>32))); err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeVirtual(stream, "writeInt", "(I)V", IntValue(int32(value)))
	return VoidValue(), err
}

func dataOutputWriteFloat(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := arguments[1].Float32()
	if err != nil {
		return VoidValue(), err
	}
	bits := math.Float32bits(value)
	// DataOutput uses Float.floatToIntBits rather than the raw-bits form, so
	// every NaN payload has the one canonical serialized representation.
	if math.IsNaN(float64(value)) {
		bits = 0x7fc00000
	}
	_, err = call.InvokeVirtual(stream, "writeInt", "(I)V", IntValue(int32(bits)))
	return VoidValue(), err
}

func dataOutputWriteDouble(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := arguments[1].Float64()
	if err != nil {
		return VoidValue(), err
	}
	bits := math.Float64bits(value)
	// DataOutput uses Double.doubleToLongBits, which likewise collapses all
	// NaN encodings before writing the eight bytes.
	if math.IsNaN(value) {
		bits = 0x7ff8000000000000
	}
	_, err = call.InvokeVirtual(stream, "writeLong", "(J)V", LongValue(int64(bits)))
	return VoidValue(), err
}

// printStreamDefinition is the stream behind System.out and System.err.
// Shipped titles are full of leftover debug printing, and a game that cannot
// resolve the field dies in whatever method happened to contain the call, so
// this exists to keep that printing harmless rather than to be a console.
//
// Each call is one line at the logging boundary, including print, which a real
// stream would have held until a newline arrived. Nothing in a game reads back
// what it printed, and a partial line that never arrives is worse to debug than
// one that arrives early.
func printStreamDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      PrintStreamClass,
		SuperName: OutputStreamClass,
		Access:    AccessPublic,
		Fields: []FieldDefinition{
			{Name: "stream", Descriptor: "I", Access: AccessPrivate | AccessFinal},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "(I)V", Access: AccessPublic, Body: printStreamInit},
			{Name: "write", Descriptor: "(I)V", Access: AccessPublic, Body: printStreamWrite},
			{Name: "print", Descriptor: "(Z)V", Access: AccessPublic, Body: printStreamPrint("(Z)V")},
			{Name: "print", Descriptor: "(C)V", Access: AccessPublic, Body: printStreamPrint("(C)V")},
			{Name: "print", Descriptor: "(I)V", Access: AccessPublic, Body: printStreamPrint("(I)V")},
			{Name: "print", Descriptor: "(J)V", Access: AccessPublic, Body: printStreamPrint("(J)V")},
			{Name: "print", Descriptor: "([C)V", Access: AccessPublic, Body: printStreamPrint("([C)V")},
			{Name: "print", Descriptor: "([B)V", Access: AccessPublic, Body: printStreamPrint("([B)V")},
			{Name: "print", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic, Body: printStreamPrint("(Ljava/lang/String;)V")},
			{Name: "print", Descriptor: "(Ljava/lang/Object;)V", Access: AccessPublic, Body: printStreamPrint("(Ljava/lang/Object;)V")},
			{Name: "println", Descriptor: "()V", Access: AccessPublic, Body: printStreamPrintln("")},
			{Name: "println", Descriptor: "(Z)V", Access: AccessPublic, Body: printStreamPrintln("(Z)V")},
			{Name: "println", Descriptor: "(C)V", Access: AccessPublic, Body: printStreamPrintln("(C)V")},
			{Name: "println", Descriptor: "(I)V", Access: AccessPublic, Body: printStreamPrintln("(I)V")},
			{Name: "println", Descriptor: "(J)V", Access: AccessPublic, Body: printStreamPrintln("(J)V")},
			{Name: "println", Descriptor: "([C)V", Access: AccessPublic, Body: printStreamPrintln("([C)V")},
			// The byte-array form is not the standard library's. This vendor's
			// titles log text they already hold as handset bytes, and printing
			// the array's identity instead of what it says would make the log
			// useless to the person reading it.
			{Name: "println", Descriptor: "([B)V", Access: AccessPublic, Body: printStreamPrintln("([B)V")},
			{Name: "println", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic, Body: printStreamPrintln("(Ljava/lang/String;)V")},
			{Name: "println", Descriptor: "(Ljava/lang/Object;)V", Access: AccessPublic, Body: printStreamPrintln("(Ljava/lang/Object;)V")},
			{Name: "flush", Descriptor: "()V", Access: AccessPublic, Body: doNothing},
			{Name: "close", Descriptor: "()V", Access: AccessPublic, Body: doNothing},
			{Name: "emit", Descriptor: "(ILjava/lang/String;)V", Access: AccessPrivate | AccessStatic | AccessNative},
		},
	}
}

func printStreamInit(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	which, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), setIntField(call.vm, stream, PrintStreamClass, "stream", which)
}

// printStreamEmitText hands one line to the emit native, which is where the
// logging boundary is. A platform may replace that native; going through it
// rather than logging from here is what keeps that possible.
func printStreamEmitText(call *Invocation, stream *Object, text string) error {
	which, err := intField(call.vm, stream, PrintStreamClass, "stream")
	if err != nil {
		return err
	}
	_, err = call.InvokeStatic(PrintStreamClass, "emit", "(ILjava/lang/String;)V",
		IntValue(which), ReferenceValue(call.vm.NewString(text)))
	return err
}

func printStreamWrite(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), printStreamEmitText(call, stream, string(rune(uint16(value))))
}

// printStreamPrint is the body of every print overload. The descriptor decides
// how the argument reads, because the stack does not distinguish a boolean or
// a char from the int both are carried as, and printing 1 where a game printed
// true is a worse log than no log.
func printStreamPrint(descriptor string) ContextMethod {
	return func(call *Invocation, arguments []Value) (Value, error) {
		stream, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		text, err := printArgumentText(call, descriptor, arguments)
		if err != nil {
			return VoidValue(), err
		}
		return VoidValue(), printStreamEmitText(call, stream, text)
	}
}

// printStreamPrintln prints a value and ends the line. Since every call is
// already one line, the only thing left to do is what print does — except for
// the no-argument overload, which prints the empty line itself.
func printStreamPrintln(descriptor string) ContextMethod {
	return func(call *Invocation, arguments []Value) (Value, error) {
		stream, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		if descriptor == "" {
			return VoidValue(), printStreamEmitText(call, stream, "")
		}
		_, err = call.InvokeVirtual(stream, "print", descriptor, arguments[1:]...)
		return VoidValue(), err
	}
}

// printArgumentText renders the one argument a print overload was given, as
// the type its descriptor names.
func printArgumentText(call *Invocation, descriptor string, arguments []Value) (string, error) {
	if len(arguments) < 2 {
		return "", fmt.Errorf("print takes one argument")
	}
	value := arguments[1]
	switch descriptor {
	case "(Z)V":
		integer, err := value.Int32()
		if err != nil {
			return "", err
		}
		return strconv.FormatBool(integer != 0), nil
	case "(C)V":
		integer, err := value.Int32()
		if err != nil {
			return "", err
		}
		return string(rune(uint16(integer))), nil
	case "(I)V":
		integer, err := value.Int32()
		if err != nil {
			return "", err
		}
		return strconv.FormatInt(int64(integer), 10), nil
	case "(J)V":
		long, err := value.Int64()
		if err != nil {
			return "", err
		}
		return strconv.FormatInt(long, 10), nil
	default:
		object, err := value.Reference()
		if err != nil {
			return "", err
		}
		return printReferenceText(call, object)
	}
}

// printReferenceText renders a reference argument. A char array prints its
// characters and anything else prints what its own toString answers, which for
// a game's object is the game's method.
func printReferenceText(call *Invocation, object *Object) (string, error) {
	if object == nil {
		return "null", nil
	}
	if text, ok := StringText(object); ok {
		return text, nil
	}
	// A byte array is the handset's own text rather than an object: this
	// vendor's println takes one, and the bytes are in the charset the rest of
	// this runtime decodes a String's bytes with.
	if component, _, ok := ArrayComponent(object); ok && component.Kind == TypeByte {
		data, err := ByteArraySnapshot(object)
		if err != nil {
			return "", err
		}
		return call.VM().decodePlatformBytes(data), nil
	}
	if component, length, ok := ArrayComponent(object); ok && component.Kind == TypeChar {
		array, err := guestArray(object)
		if err != nil {
			return "", err
		}
		runes := make([]rune, 0, length)
		for index := 0; index < length; index++ {
			element, err := array.Load(index)
			if err != nil {
				return "", err
			}
			character, err := element.Int32()
			if err != nil {
				return "", err
			}
			runes = append(runes, rune(uint16(character)))
		}
		return string(runes), nil
	}
	result, err := call.InvokeVirtual(object, "toString", "()Ljava/lang/String;")
	if err != nil {
		return "", err
	}
	text, err := result.Reference()
	if err != nil {
		return "", err
	}
	if value, ok := StringText(text); ok {
		return value, nil
	}
	return "null", nil
}

// streamRangeArguments reads the (byte[], offset, length) triple every ranged
// stream call takes, with the bounds check the library made before touching
// the array. It raises java/lang/IndexOutOfBoundsException, which is what the
// reading side raises; see writeRangeArguments for the other one.
func streamRangeArguments(arguments []Value) (receiver *Object, data *Array, offset, length int32, err error) {
	return rangeArguments(arguments, "java/lang/IndexOutOfBoundsException")
}

// writeRangeArguments is streamRangeArguments for the writing side, which
// raises the array exception rather than the general one. A game catching
// ArrayIndexOutOfBoundsException around a write catches only that one.
func writeRangeArguments(arguments []Value) (receiver *Object, data *Array, offset, length int32, err error) {
	return rangeArguments(arguments, "java/lang/ArrayIndexOutOfBoundsException")
}

func rangeArguments(arguments []Value, boundsClass string) (receiver *Object, data *Array, offset, length int32, err error) {
	receiver, err = requireObject(arguments, 0)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	buffer, err := nativeReference(arguments, 1)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	data, err = guestArray(buffer)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	if offset, err = nativeInt(arguments, 2); err != nil {
		return nil, nil, 0, 0, err
	}
	if length, err = nativeInt(arguments, 3); err != nil {
		return nil, nil, 0, 0, err
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(data.Length()) {
		return nil, nil, 0, 0, guestException(boundsClass,
			fmt.Sprintf("offset %d length %d of %d", offset, length, data.Length()))
	}
	return receiver, data, offset, length, nil
}

// guestArray answers the array behind a reference, rejecting null the way the
// array opcodes do.
func guestArray(object *Object) (*Array, error) {
	if object == nil {
		return nil, guestException("java/lang/NullPointerException", "null array")
	}
	array, ok := object.Native.(*Array)
	if !ok {
		return nil, fmt.Errorf("%s is not an array", object.ClassName)
	}
	return array, nil
}

func intField(vm *VM, object *Object, className, name string) (int32, error) {
	value, err := vm.Field(object, className, name, "I")
	if err != nil {
		return 0, err
	}
	return value.Int32()
}

// typedIntField is intField for a field declared as one of the other int-sized
// types: a field is found by its descriptor as well as its name.
func typedIntField(vm *VM, object *Object, className, name, descriptor string) (int32, error) {
	value, err := vm.Field(object, className, name, descriptor)
	if err != nil {
		return 0, err
	}
	return value.Int32()
}

func setIntField(vm *VM, object *Object, className, name string, value int32) error {
	return vm.SetField(object, className, name, "I", IntValue(value))
}

// doNothing is the body of a method that exists so a call resolves.
func doNothing(_ *Invocation, arguments []Value) (Value, error) {
	if _, err := nativeReference(arguments, 0); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), nil
}

// constantInt is the body of a method that always answers the same number.
func constantInt(result int32) ContextMethod {
	return func(_ *Invocation, arguments []Value) (Value, error) {
		if _, err := nativeReference(arguments, 0); err != nil {
			return VoidValue(), err
		}
		return IntValue(result), nil
	}
}

// dataOutputWriteUTF writes a string the way readUTF above reads one: a
// two-byte length in bytes, then modified UTF-8. A save written with this and
// read back with readUTF is the round trip a title of this era stores a name
// with, so the two have to agree on the encoding rather than on Go's.
func dataOutputWriteUTF(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	text, err := nativeString(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	encoded := encodeModifiedUTF8(text)
	if len(encoded) > 0xffff {
		return VoidValue(), guestException("java/io/UTFDataFormatException", "encoded string is longer than 65535 bytes")
	}
	if err := dataOutputByte(call, stream, int32(len(encoded)>>8)); err != nil {
		return VoidValue(), err
	}
	if err := dataOutputByte(call, stream, int32(len(encoded))); err != nil {
		return VoidValue(), err
	}
	for _, value := range encoded {
		if err := dataOutputByte(call, stream, int32(value)); err != nil {
			return VoidValue(), err
		}
	}
	return VoidValue(), nil
}

// dataOutputWriteChars writes a string as its UTF-16 units with no length in
// front, which is what DataOutput declares beside writeUTF.
func dataOutputWriteChars(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	text, err := nativeString(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	for _, unit := range utf16.Encode([]rune(text)) {
		if err := dataOutputByte(call, stream, int32(unit>>8)); err != nil {
			return VoidValue(), err
		}
		if err := dataOutputByte(call, stream, int32(unit&0xff)); err != nil {
			return VoidValue(), err
		}
	}
	return VoidValue(), nil
}

// java/io/Reader and java/io/InputStreamReader are how a title reads a text
// resource — a dialogue script, a word list — as characters rather than bytes.
//
// The decoding is incremental and knows nothing about the charset: it feeds the
// installed platform decoder one more byte at a time until the decoder answers
// with a character rather than a replacement, which is what makes the same code
// right for the single-byte and the two-byte cases without a table of lead
// bytes here. Reading a whole stream up front would have been simpler and would
// have made a reader over a connection wait for the far end to close.
func readerDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      ReaderClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessAbstract,
		Fields: []FieldDefinition{
			// What a subclass synchronizes on: the reader itself, or the
			// object it was constructed with.
			{Name: "lock", Descriptor: "Ljava/lang/Object;", Access: AccessProtected},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessProtected, Body: lockInit(ReaderClass)},
			{Name: "<init>", Descriptor: "(Ljava/lang/Object;)V", Access: AccessProtected, Body: lockInitWith(ReaderClass)},
			{Name: "read", Descriptor: "()I", Access: AccessPublic | AccessAbstract, Throws: []string{"java/io/IOException"}},
			{Name: "read", Descriptor: "([C)I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: readerReadArray},
			{Name: "read", Descriptor: "([CII)I", Access: AccessPublic | AccessAbstract, Throws: []string{"java/io/IOException"}},
			{Name: "close", Descriptor: "()V", Access: AccessPublic | AccessAbstract, Throws: []string{"java/io/IOException"}},
			{Name: "ready", Descriptor: "()Z", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: returnFalse},
			{Name: "markSupported", Descriptor: "()Z", Access: AccessPublic, Body: returnFalse},
			{Name: "skip", Descriptor: "(J)J", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: readerSkip},
			// A reader that does not override these supports no mark, which
			// is what markSupported above answers for it.
			{Name: "mark", Descriptor: "(I)V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: refuseIO("mark() not supported")},
			{Name: "reset", Descriptor: "()V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: refuseIO("reset() not supported")},
		},
	}
}

func inputStreamReaderDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      InputStreamReaderClass,
		SuperName: ReaderClass,
		Access:    AccessPublic,
		Fields: []FieldDefinition{
			{Name: "in", Descriptor: "Ljava/io/InputStream;", Access: AccessPrivate},
			// What one read took from the stream past the character it
			// answered, for the next read to start from: a byte plus one, and
			// the low half of a surrogate pair. Zero is nothing held.
			{Name: "pendingByte", Descriptor: "I", Access: AccessPrivate},
			{Name: "pendingChar", Descriptor: "C", Access: AccessPrivate},
			// The two as they were when mark was called, which is what reset
			// brings back with the stream's position. A reader no mark was
			// ever made on holds zero in both, which is what it held at the
			// start of the stream; see inputStreamReaderMark.
			{Name: "markedByte", Descriptor: "I", Access: AccessPrivate},
			{Name: "markedChar", Descriptor: "C", Access: AccessPrivate},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "(Ljava/io/InputStream;)V", Access: AccessPublic, Body: inputStreamReaderInit},
			// The named-encoding form takes the same path: the name is
			// validated so an encoding this runtime does not have is refused
			// rather than silently read with the platform's own.
			{Name: "<init>", Descriptor: "(Ljava/io/InputStream;Ljava/lang/String;)V", Access: AccessPublic, Throws: []string{"java/io/UnsupportedEncodingException"}, Body: inputStreamReaderInitEncoding},
			{Name: "read", Descriptor: "()I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: inputStreamReaderRead},
			{Name: "read", Descriptor: "([CII)I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: inputStreamReaderReadRange},
			{Name: "close", Descriptor: "()V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: inputStreamReaderClose},
			{Name: "ready", Descriptor: "()Z", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: inputStreamReaderReady},
			// A reader over a stream marks and resets that stream, so it
			// supports a mark exactly when the stream does.
			{Name: "markSupported", Descriptor: "()Z", Access: AccessPublic, Body: inputStreamReaderMarkSupported},
			{Name: "mark", Descriptor: "(I)V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: inputStreamReaderMark},
			{Name: "reset", Descriptor: "()V", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: inputStreamReaderReset},
		},
	}
}

// lockInit is the no-argument constructor of Reader and Writer: the object a
// subclass synchronizes on is the reader or writer itself.
func lockInit(class string) ContextMethod {
	return func(call *Invocation, arguments []Value) (Value, error) {
		receiver, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		return VoidValue(), call.vm.SetField(receiver, class, "lock", "Ljava/lang/Object;", ReferenceValue(receiver))
	}
}

// lockInitWith is the constructor that names that object. A null one is the
// NullPointerException the class library throws, since there would be nothing
// to synchronize on.
func lockInitWith(class string) ContextMethod {
	return func(call *Invocation, arguments []Value) (Value, error) {
		receiver, err := requireObject(arguments, 0)
		if err != nil {
			return VoidValue(), err
		}
		lock, err := nativeReference(arguments, 1)
		if err != nil {
			return VoidValue(), err
		}
		if lock == nil {
			return VoidValue(), guestException("java/lang/NullPointerException", javaClassName(class)+" lock")
		}
		return VoidValue(), call.vm.SetField(receiver, class, "lock", "Ljava/lang/Object;", ReferenceValue(lock))
	}
}

// refuseIO is the body of a method the class declares and does not support,
// which says so with IOException.
func refuseIO(message string) ContextMethod {
	return func(_ *Invocation, arguments []Value) (Value, error) {
		if _, err := requireObject(arguments, 0); err != nil {
			return VoidValue(), err
		}
		return VoidValue(), guestException(IOExceptionClass, message)
	}
}

func readerReadArray(call *Invocation, arguments []Value) (Value, error) {
	reader, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	array, err := requireObject(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	_, length, ok := ArrayComponent(array)
	if !ok {
		return VoidValue(), fmt.Errorf("Reader.read expected a character array")
	}
	return call.InvokeVirtual(reader, "read", "([CII)I", arguments[1], IntValue(0), IntValue(int32(length)))
}

func readerSkip(call *Invocation, arguments []Value) (Value, error) {
	reader, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	count, err := nativeLong(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if count < 0 {
		return VoidValue(), guestException("java/lang/IllegalArgumentException", "Reader.skip count")
	}
	skipped := int64(0)
	for ; skipped < count; skipped++ {
		result, readErr := call.InvokeVirtual(reader, "read", "()I")
		if readErr != nil {
			return VoidValue(), readErr
		}
		value, valueErr := result.Int32()
		if valueErr != nil {
			return VoidValue(), valueErr
		}
		if value < 0 {
			break
		}
	}
	return LongValue(skipped), nil
}

func returnFalse(_ *Invocation, _ []Value) (Value, error) {
	return IntValue(0), nil
}

// maxDecodedSequence is how many bytes one character may be built from before
// the decoder is taken at its word and a replacement is emitted. Four covers
// every sequence the charsets this runtime installs produce, the longest being
// UTF-8's for a character outside the basic plane.
const maxDecodedSequence = 4

func inputStreamReaderInit(call *Invocation, arguments []Value) (Value, error) {
	reader, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	source, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	// Reader's own constructor runs first, as super() does, and makes the
	// reader the object its subclasses synchronize on.
	if _, err := call.InvokeSpecial(reader, ReaderClass, "<init>", "()V"); err != nil {
		return VoidValue(), err
	}
	if source == nil {
		return VoidValue(), guestException("java/lang/NullPointerException", "InputStreamReader source")
	}
	return VoidValue(), call.vm.SetField(reader, InputStreamReaderClass, "in", "Ljava/io/InputStream;", ReferenceValue(source))
}

// inputStreamReaderMarkSupported answers what the stream underneath answers.
// A closed reader has no stream, and supports no mark.
func inputStreamReaderMarkSupported(call *Invocation, arguments []Value) (Value, error) {
	reader, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := call.vm.Field(reader, InputStreamReaderClass, "in", "Ljava/io/InputStream;")
	if err != nil {
		return VoidValue(), err
	}
	source, err := value.Reference()
	if err != nil {
		return VoidValue(), err
	}
	if source == nil {
		return IntValue(0), nil
	}
	return call.InvokeVirtual(source, "markSupported", "()Z")
}

// inputStreamReaderMark marks the stream underneath, and keeps what the reader
// holds past the stream's position — a byte read ahead of the character it
// answered, or the second half of a pair — for reset to bring back with it,
// so a read after reset answers the character a read after mark did. A stream
// that supports no mark is the IOException the class library throws, and so is
// a closed reader.
//
// The limit goes to the stream as it was given, as the CLDC reader passes it
// and as LGT's reader does: it counts characters where the stream counts
// bytes, so a multi-byte text can outrun it, which is the library's answer too.
func inputStreamReaderMark(call *Invocation, arguments []Value) (Value, error) {
	reader, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	limit, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	source, err := inputStreamReaderSource(call.vm, reader)
	if err != nil {
		return VoidValue(), err
	}
	supported, err := call.InvokeVirtual(source, "markSupported", "()Z")
	if err != nil {
		return VoidValue(), err
	}
	if flag, err := supported.Int32(); err != nil || flag == 0 {
		if err != nil {
			return VoidValue(), err
		}
		return VoidValue(), guestException(IOExceptionClass, "mark() not supported")
	}
	if _, err := call.InvokeVirtual(source, "mark", "(I)V", IntValue(limit)); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), copyReaderHeld(call.vm, reader, "pending", "marked")
}

// inputStreamReaderReset returns the stream underneath to its mark, and the
// reader to what it held there. A stream that cannot reset says so with its
// own IOException; a closed reader is one too.
func inputStreamReaderReset(call *Invocation, arguments []Value) (Value, error) {
	reader, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	source, err := inputStreamReaderSource(call.vm, reader)
	if err != nil {
		return VoidValue(), err
	}
	if _, err := call.InvokeVirtual(source, "reset", "()V"); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), copyReaderHeld(call.vm, reader, "marked", "pending")
}

// copyReaderHeld copies a reader's held byte and character from one pair of
// its fields to the other.
func copyReaderHeld(vm *VM, reader *Object, from, to string) error {
	for _, field := range []struct{ suffix, descriptor string }{{"Byte", "I"}, {"Char", "C"}} {
		value, err := vm.Field(reader, InputStreamReaderClass, from+field.suffix, field.descriptor)
		if err != nil {
			return err
		}
		if err := vm.SetField(reader, InputStreamReaderClass, to+field.suffix, field.descriptor, value); err != nil {
			return err
		}
	}
	return nil
}

func inputStreamReaderInitEncoding(call *Invocation, arguments []Value) (Value, error) {
	name, err := nativeString(arguments, 2)
	if err != nil {
		return VoidValue(), err
	}
	if charsetOf(name) == charsetUnknown {
		return VoidValue(), guestException("java/io/UnsupportedEncodingException", name)
	}
	return inputStreamReaderInit(call, arguments[:2])
}

func inputStreamReaderSource(vm *VM, reader *Object) (*Object, error) {
	value, err := vm.Field(reader, InputStreamReaderClass, "in", "Ljava/io/InputStream;")
	if err != nil {
		return nil, err
	}
	source, err := value.Reference()
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, guestException(IOExceptionClass, "reader is closed")
	}
	return source, nil
}

// inputStreamReaderRead answers one character, or -1 at the end of the stream.
//
// A byte that does not finish a character leaves the decoder answering one of
// two ways: a lone replacement while it still waits for more, or a replacement
// followed by more text when the bytes before were a sequence it cannot read
// and this byte begins the next character. The second answers the replacement
// and keeps the byte for the next read, instead of reading on and swallowing
// the text after a stray byte. A character outside the basic plane is two
// units, and the second one waits for the next read the same way.
func inputStreamReaderRead(call *Invocation, arguments []Value) (Value, error) {
	reader, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	source, err := inputStreamReaderSource(call.vm, reader)
	if err != nil {
		return VoidValue(), err
	}
	if unit, err := takeReaderPending(call.vm, reader, "pendingChar", "C"); err != nil || unit != 0 {
		return IntValue(unit), err
	}
	pending := make([]byte, 0, maxDecodedSequence)
	held, err := takeReaderPending(call.vm, reader, "pendingByte", "I")
	if err != nil {
		return VoidValue(), err
	}
	if held != 0 {
		pending = append(pending, byte(held-1))
	}
	for {
		if len(pending) > 0 {
			decoded := []rune(call.vm.decodePlatformBytes(pending))
			if len(decoded) == 1 && decoded[0] != utf8.RuneError {
				units := utf16.Encode(decoded)
				if len(units) == 2 {
					if err := call.vm.SetField(reader, InputStreamReaderClass, "pendingChar", "C", IntValue(int32(units[1]))); err != nil {
						return VoidValue(), err
					}
				}
				return IntValue(int32(units[0])), nil
			}
			if len(decoded) > 1 {
				next := int32(pending[len(pending)-1]) + 1
				if err := call.vm.SetField(reader, InputStreamReaderClass, "pendingByte", "I", IntValue(next)); err != nil {
					return VoidValue(), err
				}
				return IntValue(0xfffd), nil
			}
			if len(pending) >= maxDecodedSequence {
				return IntValue(0xfffd), nil
			}
		}
		result, readErr := call.InvokeVirtual(source, "read", "()I")
		if readErr != nil {
			return VoidValue(), readErr
		}
		value, valueErr := result.Int32()
		if valueErr != nil {
			return VoidValue(), valueErr
		}
		if value < 0 {
			if len(pending) == 0 {
				return IntValue(-1), nil
			}
			// A sequence the stream ended in the middle of is one character's
			// worth of unreadable bytes, which is what a replacement is for.
			return IntValue(0xfffd), nil
		}
		pending = append(pending, byte(value))
	}
}

// takeReaderPending answers what a reader held over from its last read in one
// of its pending fields, and empties the field.
func takeReaderPending(vm *VM, reader *Object, name, descriptor string) (int32, error) {
	value, err := vm.Field(reader, InputStreamReaderClass, name, descriptor)
	if err != nil {
		return 0, err
	}
	held, err := value.Int32()
	if err != nil || held == 0 {
		return 0, err
	}
	return held, vm.SetField(reader, InputStreamReaderClass, name, descriptor, IntValue(0))
}

func inputStreamReaderReadRange(call *Invocation, arguments []Value) (Value, error) {
	reader, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	array, err := requireObject(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	offset, err := nativeInt(arguments, 2)
	if err != nil {
		return VoidValue(), err
	}
	length, err := nativeInt(arguments, 3)
	if err != nil {
		return VoidValue(), err
	}
	_, count, ok := ArrayComponent(array)
	if !ok {
		return VoidValue(), fmt.Errorf("InputStreamReader.read expected a character array")
	}
	size := int32(count)
	if offset < 0 || length < 0 || offset > size || length > size-offset {
		return VoidValue(), guestException("java/lang/IndexOutOfBoundsException", "InputStreamReader.read range")
	}
	filled := int32(0)
	for ; filled < length; filled++ {
		result, readErr := call.InvokeVirtual(reader, "read", "()I")
		if readErr != nil {
			return VoidValue(), readErr
		}
		value, valueErr := result.Int32()
		if valueErr != nil {
			return VoidValue(), valueErr
		}
		if value < 0 {
			break
		}
		if setErr := SetArrayRange(array, int(offset+filled), []Value{IntValue(value)}); setErr != nil {
			return VoidValue(), setErr
		}
	}
	if filled == 0 && length > 0 {
		return IntValue(-1), nil
	}
	return IntValue(filled), nil
}

func inputStreamReaderReady(call *Invocation, arguments []Value) (Value, error) {
	reader, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	source, err := inputStreamReaderSource(call.vm, reader)
	if err != nil {
		return VoidValue(), err
	}
	result, err := call.InvokeVirtual(source, "available", "()I")
	if err != nil {
		return VoidValue(), err
	}
	value, err := result.Int32()
	if err != nil {
		return VoidValue(), err
	}
	return booleanValue(value > 0), nil
}

func inputStreamReaderClose(call *Invocation, arguments []Value) (Value, error) {
	reader, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := call.vm.Field(reader, InputStreamReaderClass, "in", "Ljava/io/InputStream;")
	if err != nil {
		return VoidValue(), err
	}
	source, err := value.Reference()
	if err != nil {
		return VoidValue(), err
	}
	if source == nil {
		return VoidValue(), nil
	}
	if err := call.vm.SetField(reader, InputStreamReaderClass, "in", "Ljava/io/InputStream;", ReferenceValue(nil)); err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeVirtual(source, "close", "()V")
	return VoidValue(), err
}

// java/io/Writer and java/io/OutputStreamWriter are the character side of the
// output streams, the counterpart of Reader and InputStreamReader: a title
// writes text through one into a byte stream — a record it stores, a buffer
// it sends — in the handset's charset or in UTF-8.
//
// Writer's own write forms all come down to write(char[], int, int), which a
// subclass implements, and they reach it through the VM, so a title's own
// Writer receives every character its callers write the way the class library
// hands them over.
func writerDefinition() ClassDefinition {
	throws := []string{IOExceptionClass}
	return ClassDefinition{
		Name:      WriterClass,
		SuperName: ObjectClass,
		Access:    AccessPublic | AccessAbstract,
		Fields: []FieldDefinition{
			// What a subclass synchronizes on: the writer itself, or the
			// object it was constructed with.
			{Name: "lock", Descriptor: "Ljava/lang/Object;", Access: AccessProtected},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessProtected, Body: lockInit(WriterClass)},
			{Name: "<init>", Descriptor: "(Ljava/lang/Object;)V", Access: AccessProtected, Body: lockInitWith(WriterClass)},
			{Name: "write", Descriptor: "(I)V", Access: AccessPublic, Throws: throws, Body: writerWriteChar},
			{Name: "write", Descriptor: "([C)V", Access: AccessPublic, Throws: throws, Body: writerWriteArray},
			{Name: "write", Descriptor: "([CII)V", Access: AccessPublic | AccessAbstract, Throws: throws},
			{Name: "write", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic, Throws: throws, Body: writerWriteString},
			{Name: "write", Descriptor: "(Ljava/lang/String;II)V", Access: AccessPublic, Throws: throws, Body: writerWriteStringRange},
			{Name: "flush", Descriptor: "()V", Access: AccessPublic | AccessAbstract, Throws: throws},
			{Name: "close", Descriptor: "()V", Access: AccessPublic | AccessAbstract, Throws: throws},
		},
	}
}

// writerWriteChar is write(int): the low sixteen bits as one character.
func writerWriteChar(call *Invocation, arguments []Value) (Value, error) {
	writer, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), writeUnitsThrough(call, writer, []uint16{uint16(value)})
}

// writerWriteArray is write(char[]): the whole array, a null one being the
// NullPointerException its length is read from.
func writerWriteArray(call *Invocation, arguments []Value) (Value, error) {
	writer, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	array, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	characters, err := guestArray(array)
	if err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeVirtual(writer, "write", "([CII)V", ReferenceValue(array), IntValue(0), IntValue(int32(characters.Length())))
	return VoidValue(), err
}

// writerWriteString is write(String): write(str, 0, str.length()), through the
// writer's own three-argument form.
func writerWriteString(call *Invocation, arguments []Value) (Value, error) {
	writer, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	units, err := stringUnits(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeVirtual(writer, "write", "(Ljava/lang/String;II)V", arguments[1], IntValue(0), IntValue(int32(len(units))))
	return VoidValue(), err
}

// writerWriteStringRange is write(String, int, int): the characters
// String.getChars would copy out of that range, refused as getChars refuses
// one, handed to write(char[], int, int).
func writerWriteStringRange(call *Invocation, arguments []Value) (Value, error) {
	writer, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	units, err := stringUnits(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	window, err := stringWindow(units, arguments)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), writeUnitsThrough(call, writer, window)
}

// stringWindow reads the offset and length after a String argument and answers
// that window of its units, or the StringIndexOutOfBoundsException getChars
// raises for a window outside it.
func stringWindow(units []uint16, arguments []Value) ([]uint16, error) {
	offset, err := nativeInt(arguments, 2)
	if err != nil {
		return nil, err
	}
	length, err := nativeInt(arguments, 3)
	if err != nil {
		return nil, err
	}
	end := int64(offset) + int64(length)
	if offset < 0 || length < 0 || end > int64(len(units)) {
		return nil, guestException("java/lang/StringIndexOutOfBoundsException",
			fmt.Sprintf("characters %d to %d of %d", offset, end, len(units)))
	}
	return units[offset:end], nil
}

// writeUnitsThrough hands units to a writer's own write(char[], int, int), in
// an array made for the call.
func writeUnitsThrough(call *Invocation, writer *Object, units []uint16) error {
	array, err := call.vm.newArray(Type{Kind: TypeChar}, int32(len(units)))
	if err != nil {
		return err
	}
	values := make([]Value, len(units))
	for index, unit := range units {
		values[index] = IntValue(int32(unit))
	}
	if err := SetArrayRange(array, 0, values); err != nil {
		return err
	}
	_, err = call.InvokeVirtual(writer, "write", "([CII)V", ReferenceValue(array), IntValue(0), IntValue(int32(len(units))))
	return err
}

// outputStreamWriterDefinition is the Writer over an OutputStream. Nothing is
// buffered: every write sends its bytes to the stream before it returns, as the
// class library's does, so flush only passes on to the stream and a title that
// reads its byte stream after a write finds what it wrote there.
//
// The bytes are what String.getBytes answers here for the same characters and
// the same encoding, with one difference a stream of separate writes needs: a
// write that ends on the first half of a surrogate pair holds that half until
// the next write brings the second, so a character written one unit at a time
// comes out as it does written whole. A half nothing completes goes out as an
// unmappable character when the writer closes.
func outputStreamWriterDefinition() ClassDefinition {
	throws := []string{IOExceptionClass}
	return ClassDefinition{
		Name:      OutputStreamWriterClass,
		SuperName: WriterClass,
		Access:    AccessPublic,
		Fields: []FieldDefinition{
			{Name: "out", Descriptor: "Ljava/io/OutputStream;", Access: AccessPrivate},
			// Whether the text goes out in UTF-8 rather than in the
			// platform's charset.
			{Name: "utf8", Descriptor: "Z", Access: AccessPrivate},
			// The first half of a surrogate pair the last write ended on.
			// Zero is nothing held.
			{Name: "pendingChar", Descriptor: "C", Access: AccessPrivate},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "(Ljava/io/OutputStream;)V", Access: AccessPublic, Body: outputStreamWriterInit},
			// The encoding names accepted are InputStreamReader's, and a name
			// that is not one of them is refused rather than written in
			// another charset.
			{Name: "<init>", Descriptor: "(Ljava/io/OutputStream;Ljava/lang/String;)V", Access: AccessPublic, Throws: []string{"java/io/UnsupportedEncodingException"}, Body: outputStreamWriterInitEncoding},
			{Name: "write", Descriptor: "(I)V", Access: AccessPublic, Throws: throws, Body: outputStreamWriterWriteChar},
			{Name: "write", Descriptor: "([CII)V", Access: AccessPublic, Throws: throws, Body: outputStreamWriterWriteChars},
			{Name: "write", Descriptor: "(Ljava/lang/String;II)V", Access: AccessPublic, Throws: throws, Body: outputStreamWriterWriteString},
			{Name: "flush", Descriptor: "()V", Access: AccessPublic, Throws: throws, Body: outputStreamWriterFlush},
			{Name: "close", Descriptor: "()V", Access: AccessPublic, Throws: throws, Body: outputStreamWriterClose},
		},
	}
}

func outputStreamWriterInit(call *Invocation, arguments []Value) (Value, error) {
	return VoidValue(), openOutputStreamWriter(call, arguments, nil)
}

func outputStreamWriterInitEncoding(call *Invocation, arguments []Value) (Value, error) {
	if len(arguments) < 3 {
		return VoidValue(), fmt.Errorf("OutputStreamWriter expected a stream and an encoding")
	}
	name := arguments[2]
	return VoidValue(), openOutputStreamWriter(call, arguments[:2], &name)
}

// openOutputStreamWriter is both constructors: Writer's own first, as super()
// is, then a null stream or a null encoding name refused with
// NullPointerException, and a name this runtime has no charset for with
// UnsupportedEncodingException — the order the class library checks them in.
func openOutputStreamWriter(call *Invocation, arguments []Value, encoding *Value) error {
	writer, err := requireObject(arguments, 0)
	if err != nil {
		return err
	}
	if _, err := call.InvokeSpecial(writer, WriterClass, "<init>", "()V"); err != nil {
		return err
	}
	stream, err := nativeReference(arguments, 1)
	if err != nil {
		return err
	}
	utf8 := false
	if encoding != nil {
		name, err := encoding.Reference()
		if err != nil {
			return err
		}
		if stream == nil || name == nil {
			return guestException("java/lang/NullPointerException", "OutputStreamWriter stream or encoding")
		}
		text, ok := nativeStringObject(name)
		if !ok {
			return fmt.Errorf("OutputStreamWriter encoding is not a String")
		}
		switch charsetOf(text) {
		case charsetUnknown:
			return guestException("java/io/UnsupportedEncodingException", text)
		case charsetUTF8:
			utf8 = true
		}
	}
	if stream == nil {
		return guestException("java/lang/NullPointerException", "OutputStreamWriter stream")
	}
	if err := call.vm.SetField(writer, OutputStreamWriterClass, "utf8", "Z", booleanValue(utf8)); err != nil {
		return err
	}
	return call.vm.SetField(writer, OutputStreamWriterClass, "out", "Ljava/io/OutputStream;", ReferenceValue(stream))
}

// outputStreamWriterStream answers the stream a writer sends to, or the
// IOException the class library throws for a writer that was closed.
func outputStreamWriterStream(vm *VM, writer *Object) (*Object, error) {
	value, err := vm.Field(writer, OutputStreamWriterClass, "out", "Ljava/io/OutputStream;")
	if err != nil {
		return nil, err
	}
	stream, err := value.Reference()
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, guestException(IOExceptionClass, "writer is closed")
	}
	return stream, nil
}

// outputStreamWriterSend encodes units and writes the bytes to the stream with
// one write(byte[], int, int), which a title's own stream receives as such.
// The first half of a pair the units end on is held for the next write unless
// this is the last one.
func outputStreamWriterSend(call *Invocation, writer, stream *Object, units []uint16, last bool) error {
	held, err := typedIntField(call.vm, writer, OutputStreamWriterClass, "pendingChar", "C")
	if err != nil {
		return err
	}
	if held != 0 {
		units = append([]uint16{uint16(held)}, units...)
	}
	next := int32(0)
	if count := len(units); !last && count > 0 && units[count-1] >= 0xd800 && units[count-1] < 0xdc00 {
		next = int32(units[count-1])
		units = units[:count-1]
	}
	if err := call.vm.SetField(writer, OutputStreamWriterClass, "pendingChar", "C", IntValue(next)); err != nil {
		return err
	}
	if len(units) == 0 {
		return nil
	}
	text := string(utf16.Decode(units))
	utf8, err := typedIntField(call.vm, writer, OutputStreamWriterClass, "utf8", "Z")
	if err != nil {
		return err
	}
	data := []byte(text)
	if utf8 == 0 {
		data = call.vm.encodePlatformString(text)
	}
	if len(data) == 0 {
		return nil
	}
	array, err := call.vm.newByteArrayObject(data)
	if err != nil {
		return err
	}
	_, err = call.InvokeVirtual(stream, "write", "([BII)V", ReferenceValue(array), IntValue(0), IntValue(int32(len(data))))
	return err
}

func outputStreamWriterWriteChar(call *Invocation, arguments []Value) (Value, error) {
	writer, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := nativeInt(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	stream, err := outputStreamWriterStream(call.vm, writer)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), outputStreamWriterSend(call, writer, stream, []uint16{uint16(value)}, false)
}

// outputStreamWriterWriteChars is write(char[], int, int): a closed writer is
// IOException first, then a null array NullPointerException, then a window
// outside the array IndexOutOfBoundsException.
func outputStreamWriterWriteChars(call *Invocation, arguments []Value) (Value, error) {
	writer, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	stream, err := outputStreamWriterStream(call.vm, writer)
	if err != nil {
		return VoidValue(), err
	}
	array, err := nativeReference(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	if array == nil {
		return VoidValue(), guestException("java/lang/NullPointerException", "write of a null char array")
	}
	units, err := charArrayUnits(array)
	if err != nil {
		return VoidValue(), err
	}
	offset, err := nativeInt(arguments, 2)
	if err != nil {
		return VoidValue(), err
	}
	length, err := nativeInt(arguments, 3)
	if err != nil {
		return VoidValue(), err
	}
	end := int64(offset) + int64(length)
	if offset < 0 || length < 0 || end > int64(len(units)) {
		return VoidValue(), guestException("java/lang/IndexOutOfBoundsException",
			fmt.Sprintf("characters %d to %d of %d", offset, end, len(units)))
	}
	return VoidValue(), outputStreamWriterSend(call, writer, stream, units[offset:end], false)
}

// outputStreamWriterWriteString is write(String, int, int): a closed writer is
// IOException first, then a null string NullPointerException, then a window
// outside it the StringIndexOutOfBoundsException getChars raises.
func outputStreamWriterWriteString(call *Invocation, arguments []Value) (Value, error) {
	writer, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	stream, err := outputStreamWriterStream(call.vm, writer)
	if err != nil {
		return VoidValue(), err
	}
	units, err := stringUnits(arguments, 1)
	if err != nil {
		return VoidValue(), err
	}
	window, err := stringWindow(units, arguments)
	if err != nil {
		return VoidValue(), err
	}
	return VoidValue(), outputStreamWriterSend(call, writer, stream, window, false)
}

// outputStreamWriterFlush passes the flush on to the stream; nothing waits
// here to be sent except a half pair, which a flush does not end.
func outputStreamWriterFlush(call *Invocation, arguments []Value) (Value, error) {
	writer, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	stream, err := outputStreamWriterStream(call.vm, writer)
	if err != nil {
		return VoidValue(), err
	}
	_, err = call.InvokeVirtual(stream, "flush", "()V")
	return VoidValue(), err
}

// outputStreamWriterClose sends a half pair nothing completed, closes the
// stream and lets it go; the writer is closed after that, and closing it again
// does nothing.
func outputStreamWriterClose(call *Invocation, arguments []Value) (Value, error) {
	writer, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	value, err := call.vm.Field(writer, OutputStreamWriterClass, "out", "Ljava/io/OutputStream;")
	if err != nil {
		return VoidValue(), err
	}
	stream, err := value.Reference()
	if err != nil || stream == nil {
		return VoidValue(), err
	}
	if err := outputStreamWriterSend(call, writer, stream, nil, true); err != nil {
		return VoidValue(), err
	}
	if _, err := call.InvokeVirtual(stream, "close", "()V"); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), call.vm.SetField(writer, OutputStreamWriterClass, "out", "Ljava/io/OutputStream;", ReferenceValue(nil))
}

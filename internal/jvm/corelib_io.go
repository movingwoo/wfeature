package jvm

import (
	"fmt"
	"strconv"
	"unicode/utf16"
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
		Fields: []FieldDefinition{
			{Name: "data", Descriptor: "[B", Access: AccessPrivate},
			{Name: "position", Descriptor: "I", Access: AccessPrivate},
		},
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "([B)V", Access: AccessPublic, Body: byteArrayInputStreamInit},
			{Name: "read", Descriptor: "()I", Access: AccessPublic, Body: byteArrayInputStreamRead},
			{Name: "read", Descriptor: "([BII)I", Access: AccessPublic, Body: byteArrayInputStreamReadRange},
			{Name: "skip", Descriptor: "(J)J", Access: AccessPublic, Body: byteArrayInputStreamSkip},
			{Name: "available", Descriptor: "()I", Access: AccessPublic, Body: byteArrayInputStreamAvailable},
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
	return VoidValue(), call.vm.SetField(stream, ByteArrayInputStreamClass, "data", "[B", ReferenceValue(data))
}

// byteArrayInputStreamState reads the two fields every method here starts from.
func byteArrayInputStreamState(vm *VM, stream *Object) (*Array, int32, error) {
	value, err := vm.Field(stream, ByteArrayInputStreamClass, "data", "[B")
	if err != nil {
		return nil, 0, err
	}
	object, err := value.Reference()
	if err != nil {
		return nil, 0, err
	}
	array, err := guestArray(object)
	if err != nil {
		return nil, 0, err
	}
	position, err := intField(vm, stream, ByteArrayInputStreamClass, "position")
	if err != nil {
		return nil, 0, err
	}
	return array, position, nil
}

func byteArrayInputStreamRead(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	data, position, err := byteArrayInputStreamState(call.vm, stream)
	if err != nil {
		return VoidValue(), err
	}
	if position >= int32(data.Length()) {
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
	if err := setIntField(call.vm, stream, ByteArrayInputStreamClass, "position", position+1); err != nil {
		return VoidValue(), err
	}
	return IntValue(value & 0xff), nil
}

func byteArrayInputStreamReadRange(call *Invocation, arguments []Value) (Value, error) {
	stream, output, offset, length, err := streamRangeArguments(arguments)
	if err != nil {
		return VoidValue(), err
	}
	data, position, err := byteArrayInputStreamState(call.vm, stream)
	if err != nil {
		return VoidValue(), err
	}
	remaining := int32(data.Length()) - position
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
	if err := setIntField(call.vm, stream, ByteArrayInputStreamClass, "position", position+count); err != nil {
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
	data, position, err := byteArrayInputStreamState(call.vm, stream)
	if err != nil {
		return VoidValue(), err
	}
	available := int64(data.Length()) - int64(position)
	skipped := count
	if skipped > available {
		skipped = available
	}
	if skipped < 0 {
		return LongValue(0), nil
	}
	if err := setIntField(call.vm, stream, ByteArrayInputStreamClass, "position", position+int32(skipped)); err != nil {
		return VoidValue(), err
	}
	return LongValue(skipped), nil
}

func byteArrayInputStreamAvailable(call *Invocation, arguments []Value) (Value, error) {
	stream, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	data, position, err := byteArrayInputStreamState(call.vm, stream)
	if err != nil {
		return VoidValue(), err
	}
	return IntValue(int32(data.Length()) - position), nil
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
		},
	}
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

func dataInputStreamDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      DataInputStreamClass,
		SuperName: InputStreamClass,
		Access:    AccessPublic,
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
			{Name: "skipBytes", Descriptor: "(I)I", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataInputSkipBytes},
			{Name: "readBoolean", Descriptor: "()Z", Access: AccessPublic | AccessFinal, Throws: []string{"java/io/IOException"}, Body: dataInputReadBoolean},
			{Name: "readByte", Descriptor: "()B", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadByte},
			{Name: "readShort", Descriptor: "()S", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadShort},
			{Name: "readUnsignedByte", Descriptor: "()I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadUnsignedByte},
			{Name: "readUnsignedShort", Descriptor: "()I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadUnsignedShort},
			{Name: "readChar", Descriptor: "()C", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadChar},
			{Name: "readInt", Descriptor: "()I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadInt},
			{Name: "readLong", Descriptor: "()J", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: dataInputReadLong},
			{Name: "readUTF", Descriptor: "()Ljava/lang/String;", Access: AccessPublic | AccessNative, Throws: []string{"java/io/IOException"}},
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
			return VoidValue(), guestException("java/io/EOFException",
				fmt.Sprintf("readFully wanted %d bytes and reached the end after %d", length, filled))
		}
		filled += count
	}
	return VoidValue(), nil
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
		return 0, guestException(IOExceptionClass, "unexpected end of stream")
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

func dataOutputStreamDefinition() ClassDefinition {
	return ClassDefinition{
		Name:      DataOutputStreamClass,
		SuperName: OutputStreamClass,
		Access:    AccessPublic,
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
			{Name: "print", Descriptor: "(Ljava/lang/String;)V", Access: AccessPublic, Body: printStreamPrint("(Ljava/lang/String;)V")},
			{Name: "print", Descriptor: "(Ljava/lang/Object;)V", Access: AccessPublic, Body: printStreamPrint("(Ljava/lang/Object;)V")},
			{Name: "println", Descriptor: "()V", Access: AccessPublic, Body: printStreamPrintln("")},
			{Name: "println", Descriptor: "(Z)V", Access: AccessPublic, Body: printStreamPrintln("(Z)V")},
			{Name: "println", Descriptor: "(C)V", Access: AccessPublic, Body: printStreamPrintln("(C)V")},
			{Name: "println", Descriptor: "(I)V", Access: AccessPublic, Body: printStreamPrintln("(I)V")},
			{Name: "println", Descriptor: "(J)V", Access: AccessPublic, Body: printStreamPrintln("(J)V")},
			{Name: "println", Descriptor: "([C)V", Access: AccessPublic, Body: printStreamPrintln("([C)V")},
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
		Methods: []MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: AccessProtected, Body: doNothing},
			{Name: "read", Descriptor: "()I", Access: AccessPublic | AccessAbstract, Throws: []string{"java/io/IOException"}},
			{Name: "read", Descriptor: "([C)I", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: readerReadArray},
			{Name: "read", Descriptor: "([CII)I", Access: AccessPublic | AccessAbstract, Throws: []string{"java/io/IOException"}},
			{Name: "close", Descriptor: "()V", Access: AccessPublic | AccessAbstract, Throws: []string{"java/io/IOException"}},
			{Name: "ready", Descriptor: "()Z", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: returnFalse},
			{Name: "markSupported", Descriptor: "()Z", Access: AccessPublic, Body: returnFalse},
			{Name: "skip", Descriptor: "(J)J", Access: AccessPublic, Throws: []string{"java/io/IOException"}, Body: readerSkip},
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
		},
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
// the decoder is taken at its word and a replacement is emitted. Three covers
// every sequence the charsets this runtime installs produce.
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
	if source == nil {
		return VoidValue(), guestException("java/lang/NullPointerException", "InputStreamReader source")
	}
	return VoidValue(), call.vm.SetField(reader, InputStreamReaderClass, "in", "Ljava/io/InputStream;", ReferenceValue(source))
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
func inputStreamReaderRead(call *Invocation, arguments []Value) (Value, error) {
	reader, err := requireObject(arguments, 0)
	if err != nil {
		return VoidValue(), err
	}
	source, err := inputStreamReaderSource(call.vm, reader)
	if err != nil {
		return VoidValue(), err
	}
	pending := make([]byte, 0, maxDecodedSequence)
	for len(pending) < maxDecodedSequence {
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
		units := utf16.Encode([]rune(call.vm.decodePlatformBytes(pending)))
		if len(units) == 1 && units[0] != 0xfffd {
			return IntValue(int32(units[0])), nil
		}
	}
	return IntValue(0xfffd), nil
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

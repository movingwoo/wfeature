package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `DataInputStream.readUTF(DataInput)`: a name read through any DataInput,
// which the KTF and SKT runtimes do not have, so it is CLDC 1.1's.
//
// The class library reads it in two calls on the DataInput it is handed —
// `readUnsignedShort()` for the length in bytes, then `readFully(byte[], int,
// int)` for that many bytes — and decodes the bytes as modified UTF-8, throwing
// UTFDataFormatException for a sequence that is not one; a stream that ends
// first is the EOFException those two throw. Both calls are made the way a
// title's own would reach them:
//
//   - On a DataInputStream, through its slots — `readFully(byte[], int, int)`
//     at 20 and `readUnsignedShort` at 26, which this platform serves — so a
//     title's own subclass that overrides one answers itself. A slot that still
//     holds this platform's stub is read here from the stream the object stands
//     for, with the ending the class library gives those two: what is left is
//     taken and EOFException is thrown, where the slots' own bodies stop the
//     title instead (java_stream.go).
//   - On a title's own class that implements DataInput, through the interface
//     table its record carries, at the method's place in the interface:
//     `readFully(byte[], int, int)` second and `readUnsignedShort` eighth, the
//     order the interface's page declares them in. **That order is the rule's
//     alone**: no title has been watched calling DataInput through its
//     interface, and the position of an interface's methods is the same
//     declaration-order reading the class slots are.
//
// The text keeps every code unit the bytes decode to, an unpaired surrogate
// included, as `new String(char[])` does.

const (
	javaDataInputInterface = "java/io/DataInput"
	javaEOFClass           = "java/io/EOFException"
	javaUTFDataFormatClass = "java/io/UTFDataFormatException"
)

// Where the two methods readUTF calls sit: in a DataInputStream's own slots,
// and among the DataInput interface's methods.
const (
	javaDataStreamSlotReadFully         = 20 // readFully([BII)V
	javaDataStreamSlotReadUnsignedShort = 26 // readUnsignedShort()I
	javaDataInputReadFully              = 1
	javaDataInputReadUnsignedShort      = 7
)

// javaDataInputReadUTF is the static `readUTF(DataInput)`.
func javaDataInputReadUTF(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	in := arguments[0]
	if in == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": readUTF from a null DataInput")
	}
	length, err := client.javaDataInputUnsignedShort(ctx, thread, in)
	if err != nil {
		return 0, err
	}
	data, err := client.javaDataInputFully(ctx, thread, in, length)
	if err != nil {
		return 0, err
	}
	units, err := modifiedUTF8Units(data)
	if err != nil {
		return 0, client.throwJavaPlatform(thread, javaUTFDataFormatClass, ": "+err.Error())
	}
	return client.newJavaString(javaTextOfUnits(units))
}

// javaDataInputEntry answers how one of the two methods is reached on an
// object: zero for a DataInputStream slot that still holds this platform's
// stub, which is read here, and otherwise the entry point of the title's own
// method.
func (client *Client) javaDataInputEntry(object, slot, method uint32) (uint32, error) {
	class, known := client.javaClassOfObject(object)
	if !known {
		return 0, fmt.Errorf("the DataInput at %#x is not an object this platform issued", object)
	}
	if client.javaClassReaches(class, javaDataInputStreamClass) {
		if class.VTable == 0 || slot >= class.Slots {
			return 0, fmt.Errorf("%s has no vtable slot %d", class.Name, slot)
		}
		entry, err := client.readWord(class.VTable + 4 + slot*4)
		if err != nil {
			return 0, err
		}
		if _, stub := client.javaStubSlot(entry); stub {
			return 0, nil
		}
		if entry == 0 {
			return 0, fmt.Errorf("%s vtable slot %d is empty", class.Name, slot)
		}
		return entry, nil
	}
	base, err := client.javaInterfaceBase(class, javaDataInputInterface)
	if err != nil {
		return 0, err
	}
	entry, err := client.readWord(base + 4 + method*4)
	if err != nil {
		return 0, err
	}
	if entry == 0 {
		return 0, fmt.Errorf("%s gives %s method %d no entry", class.Name, javaDataInputInterface, method)
	}
	return entry, nil
}

// javaDataInputUnsignedShort is the length readUTF reads first. A title's own
// method may answer any int; a negative one is the NegativeArraySizeException
// the class library's `new byte[utflen]` throws for it.
func (client *Client) javaDataInputUnsignedShort(
	ctx context.Context, thread *armcore.Thread, in uint32,
) (int, error) {
	entry, err := client.javaDataInputEntry(in, javaDataStreamSlotReadUnsignedShort, javaDataInputReadUnsignedShort)
	if err != nil {
		return 0, err
	}
	if entry == 0 {
		stream, err := client.javaStreamNeeding(ctx, thread, in, 2)
		if err != nil {
			return 0, err
		}
		if len(stream.Data)-stream.Read < 2 {
			stream.Read = len(stream.Data)
			return 0, client.throwJavaPlatform(thread, javaEOFClass, ": readUnsignedShort at the end of "+stream.Name)
		}
		value := int(stream.Data[stream.Read])<<8 | int(stream.Data[stream.Read+1])
		stream.Read += 2
		return value, nil
	}
	answer, err := client.callOn(ctx, thread, entry, []uint32{in})
	if err != nil {
		return 0, client.rethrowFromCallback(thread,
			fmt.Errorf("run readUnsignedShort()I at %#x: %w", entry, err))
	}
	length := int64(int32(answer))
	switch {
	case length < 0:
		return 0, client.throwJavaPlatform(thread, javaNegativeArraySizeClass, fmt.Sprintf(": readUTF of %d bytes", length))
	case length > maxJavaArrayLength:
		return 0, fmt.Errorf("readUTF of %d bytes is more than an array holds", length)
	}
	return int(length), nil
}

// javaDataInputFully is the bytes readUTF reads second: exactly that many, or
// the EOFException a stream that ends first is.
func (client *Client) javaDataInputFully(
	ctx context.Context, thread *armcore.Thread, in uint32, length int,
) ([]byte, error) {
	entry, err := client.javaDataInputEntry(in, javaDataStreamSlotReadFully, javaDataInputReadFully)
	if err != nil {
		return nil, err
	}
	if entry == 0 {
		stream, err := client.javaStreamNeeding(ctx, thread, in, length)
		if err != nil {
			return nil, err
		}
		if len(stream.Data)-stream.Read < length {
			stream.Read = len(stream.Data)
			return nil, client.throwJavaPlatform(thread, javaEOFClass,
				fmt.Sprintf(": readFully of %d bytes past the end of %s", length, stream.Name))
		}
		data := append([]byte(nil), stream.Data[stream.Read:stream.Read+length]...)
		stream.Read += length
		return data, nil
	}
	array, err := client.newJavaByteArray(make([]byte, length))
	if err != nil {
		return nil, err
	}
	if _, err := client.callOn(ctx, thread, entry, []uint32{in, array, 0, uint32(length)}); err != nil {
		return nil, client.rethrowFromCallback(thread,
			fmt.Errorf("run readFully([BII)V at %#x: %w", entry, err))
	}
	return client.readJavaArrayBytes(array)
}

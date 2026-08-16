package skvm

import "github.com/movingwoo/wfeature/internal/jvm"

// The com.xce.io file surface. A title's saves reach the Host through XFile's
// natives; the two stream classes here are the java.io view of one, and they
// are ordinary calls on the file object rather than a second path into the
// platform.

const (
	xfileDescriptor = "Lcom/xce/io/XFile;"

	// The open modes and seek origins XFile publishes.
	xfileRead    int32 = 1
	xfileWrite   int32 = 2
	xfileSeekSet int32 = 0
	xfileSeekCur int32 = 1
	xfileSeekEnd int32 = 2
)

// xfileInitHandle wraps a handle the platform already opened.
func xfileInitHandle(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	file, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(file, XFileClass, "initHandle", "(I)V", arguments[1])
	return jvm.VoidValue(), err
}

func xfileInitName(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	file, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(file, XFileClass, "initName", "(Ljava/lang/String;I)V", arguments[1], arguments[2])
	return jvm.VoidValue(), err
}

// xfileInitMode opens by the textual mode a C-style caller passes. An
// unrecognized mode reads: refusing it would fail a title that opens its save
// with a spelling this table does not have, and reading is the harmless half.
func xfileInitMode(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	file, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	mode, err := arguments[2].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	bits := xfileRead
	if text, ok := jvm.StringText(mode); ok {
		bits = 0
		for _, character := range text {
			switch character {
			case 'r':
				bits |= xfileRead
			case 'w', 'a':
				bits |= xfileWrite
			}
		}
		if bits == 0 {
			bits = xfileRead
		}
	}
	_, err = call.InvokeSpecial(file, XFileClass, "initName", "(Ljava/lang/String;I)V", arguments[1], jvm.IntValue(bits))
	return jvm.VoidValue(), err
}

// streamFile reads the file a stream was built around.
func streamFile(machine *jvm.VM, stream *jvm.Object, className string) (*jvm.Object, error) {
	value, err := machine.Field(stream, className, "file", xfileDescriptor)
	if err != nil {
		return nil, err
	}
	file, err := value.Reference()
	if err != nil {
		return nil, err
	}
	if file == nil {
		return nil, jvm.Throw("java/lang/NullPointerException", "stream has no file")
	}
	return file, nil
}

// openStreamFile makes the file a stream constructor was asked for and stores
// it. The three forms differ only in what they hand XFile.
func openStreamFile(className, descriptor string, mode int32) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		stream, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		var file *jvm.Object
		switch descriptor {
		case "(I)V":
			file, err = call.NewObject(XFileClass, "(I)V", arguments[1])
		case xfileDescriptor:
			file, err = arguments[1].Reference()
		default:
			file, err = call.NewObject(XFileClass, "(Ljava/lang/String;I)V", arguments[1], jvm.IntValue(mode))
		}
		if err != nil {
			return jvm.VoidValue(), err
		}
		return jvm.VoidValue(), call.VM().SetField(stream, className, "file", xfileDescriptor, jvm.ReferenceValue(file))
	}
}

// fileOutputStreamAppendInit opens for writing and steps to the end, which is
// what append means for a handset file: the existing bytes stay.
func fileOutputStreamAppendInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	stream, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file, err := call.NewObject(XFileClass, "(Ljava/lang/String;I)V", arguments[1], jvm.IntValue(xfileWrite))
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := call.VM().SetField(stream, FileOutputStreamClass, "file", xfileDescriptor, jvm.ReferenceValue(file)); err != nil {
		return jvm.VoidValue(), err
	}
	append, err := arguments[2].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if append == 0 {
		return jvm.VoidValue(), nil
	}
	_, err = call.InvokeVirtual(file, "seek", "(II)I", jvm.IntValue(0), jvm.IntValue(xfileSeekEnd))
	return jvm.VoidValue(), err
}

// fileDelegate forwards a stream method to the file behind it.
func fileDelegate(className, name, descriptor string) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		stream, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		file, err := streamFile(call.VM(), stream, className)
		if err != nil {
			return jvm.VoidValue(), err
		}
		return call.InvokeVirtual(file, name, descriptor, arguments[1:]...)
	}
}

// markIsSupported answers the mark question a stream over a real file can
// say yes to: the position is a seek away, so mark and reset are exact.
func markIsSupported(_ *jvm.Invocation, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(1), nil
}

func fileInputStreamMark(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	stream, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file, err := streamFile(call.VM(), stream, FileInputStreamClass)
	if err != nil {
		return jvm.VoidValue(), err
	}
	position, err := call.InvokeVirtual(file, "seek", "(II)I", jvm.IntValue(0), jvm.IntValue(xfileSeekCur))
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), call.VM().SetField(stream, FileInputStreamClass, "mark", "I", position)
}

func fileInputStreamReset(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	stream, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file, err := streamFile(call.VM(), stream, FileInputStreamClass)
	if err != nil {
		return jvm.VoidValue(), err
	}
	mark, err := call.VM().Field(stream, FileInputStreamClass, "mark", "I")
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeVirtual(file, "seek", "(II)I", mark, jvm.IntValue(xfileSeekSet))
	return jvm.VoidValue(), err
}

// fileInputStreamRead reads one byte through the ranged read, because that is
// the only read the file itself has.
func fileInputStreamRead(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	stream, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file, err := streamFile(call.VM(), stream, FileInputStreamClass)
	if err != nil {
		return jvm.VoidValue(), err
	}
	buffer := jvm.NewByteArray(make([]byte, 1))
	result, err := call.InvokeVirtual(file, "read", "([BII)I", jvm.ReferenceValue(buffer), jvm.IntValue(0), jvm.IntValue(1))
	if err != nil {
		return jvm.VoidValue(), err
	}
	count, err := result.Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if count <= 0 {
		return jvm.IntValue(-1), nil
	}
	data, err := jvm.ByteArraySnapshot(buffer)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(data[0]) & 0xff), nil
}

func fileInputStreamReadArray(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	stream, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if data == nil {
		return jvm.VoidValue(), jvm.Throw("java/lang/NullPointerException", "null buffer")
	}
	_, length, ok := jvm.ArrayComponent(data)
	if !ok {
		return jvm.VoidValue(), jvm.Throw("java/lang/IllegalArgumentException", "buffer is not an array")
	}
	return call.InvokeVirtual(stream, "read", "([BII)I", arguments[1], jvm.IntValue(0), jvm.IntValue(int32(length)))
}

// fileInputStreamReadRange answers the end of a file the way java.io does,
// with -1 rather than the zero the file layer returns.
func fileInputStreamReadRange(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	stream, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file, err := streamFile(call.VM(), stream, FileInputStreamClass)
	if err != nil {
		return jvm.VoidValue(), err
	}
	result, err := call.InvokeVirtual(file, "read", "([BII)I", arguments[1], arguments[2], arguments[3])
	if err != nil {
		return jvm.VoidValue(), err
	}
	count, err := result.Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if count <= 0 {
		return jvm.IntValue(-1), nil
	}
	return jvm.IntValue(count), nil
}

// fileInputStreamSkip steps the file position and answers with how far it
// actually moved, which is short of the request at the end of the file.
func fileInputStreamSkip(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	stream, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file, err := streamFile(call.VM(), stream, FileInputStreamClass)
	if err != nil {
		return jvm.VoidValue(), err
	}
	count, err := arguments[1].Int64()
	if err != nil {
		return jvm.VoidValue(), err
	}
	start, err := call.InvokeVirtual(file, "seek", "(II)I", jvm.IntValue(0), jvm.IntValue(xfileSeekCur))
	if err != nil {
		return jvm.VoidValue(), err
	}
	end, err := call.InvokeVirtual(file, "seek", "(II)I", jvm.IntValue(int32(count)), jvm.IntValue(xfileSeekCur))
	if err != nil {
		return jvm.VoidValue(), err
	}
	before, err := start.Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	after, err := end.Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.LongValue(int64(after - before)), nil
}

// fileOutputStreamWrite writes one byte through the ranged write, for the
// reason fileInputStreamRead reads one through the ranged read.
func fileOutputStreamWrite(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	stream, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	value, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	buffer := jvm.NewByteArray([]byte{byte(value)})
	_, err = call.InvokeVirtual(stream, "write", "([BII)V", jvm.ReferenceValue(buffer), jvm.IntValue(0), jvm.IntValue(1))
	return jvm.VoidValue(), err
}

func fileOutputStreamWriteRange(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	stream, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	file, err := streamFile(call.VM(), stream, FileOutputStreamClass)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeVirtual(file, "write", "([BII)I", arguments[1], arguments[2], arguments[3])
	return jvm.VoidValue(), err
}

// smsMessageInit builds an empty message. The platform fills one in when a
// message arrives; a game constructs one to hand to the send call.
func smsMessageInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	message, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(message, SMSMessageClass, "init", "([BLjava/lang/String;)V",
		jvm.ReferenceValue(nil), jvm.ReferenceValue(nil))
	return jvm.VoidValue(), err
}

// runtimeAudioClipInit remembers the content type the clip was asked for. The
// data arrives later, through open.
func runtimeAudioClipInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	clip, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), call.VM().SetField(clip, RuntimeAudioClipClass, "type", "Ljava/lang/String;", arguments[1])
}

package ktf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/movingwoo/wfeature/internal/jvm"
)

const (
	relayDependency         = "01039AD6"
	relayDependencyVersion  = "01.00.08"
	relayAddress            = "socket://wipiwicgsfg.magicn.com:17096"
	runtimeRelaySocketClass = "org/kwis/msf/io/Socket"
	runtimeRelayInputClass  = "wfeature/compat/RelayInputStream"
	runtimeRelayOutputClass = "wfeature/compat/RelayOutputStream"
)

// A descriptor alone does not install its dependencies. This provider is
// available only with the observed relay middleware and endpoint contract.
func (runtime *initializationRuntime) hasSlotRelay() bool {
	if runtime == nil || runtime.client == nil {
		return false
	}
	client := runtime.client
	found := false
	for _, entry := range strings.Split(client.appProperties["REQLIB"], "`") {
		if entry == relayDependency+"|"+relayDependencyVersion {
			found = true
		}
	}
	return found && bytes.Contains(client.image.Data, []byte("com/vdigm/billcom/relay/a")) && bytes.Contains(client.image.Data, relayHostConstant())
}

// Java string constants in this image format are UTF-16LE. The endpoint is
// assembled from a host constant and an integer port by the middleware.
func relayHostConstant() []byte {
	var result []byte
	for _, b := range []byte("wipiwicgsfg.magicn.com") {
		result = append(result, b, 0)
	}
	return result
}

type relaySocket struct {
	service                                 slotRelay
	pending, incoming                       []byte
	input, output                           *jvm.Object
	closed                                  bool
	socketClosed, inputClosed, outputClosed bool
}

func (socket *relaySocket) close() {
	socket.closed = true
	socket.socketClosed, socket.inputClosed, socket.outputClosed = true, true, true
	socket.pending = nil
	socket.incoming = nil
}

func (socket *relaySocket) write(data []byte) error {
	if socket.closed || socket.outputClosed {
		return fmt.Errorf("local relay socket is closed")
	}
	if len(data) > maxRelayFrameSize-len(socket.pending) {
		socket.close()
		return fmt.Errorf("local relay input exceeds limit")
	}
	socket.pending = append(socket.pending, data...)
	for len(socket.pending) > 0 {
		frame, n, err := decodeRelayFrame(socket.pending)
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		if err != nil {
			socket.close()
			return err
		}
		response, err := socket.service.respond(frame.payload)
		if err != nil {
			socket.close()
			return err
		}
		wire, err := encodeRelayFrame(relayFrame{payload: response})
		if err != nil {
			socket.close()
			return err
		}
		if len(wire) > maxRelayFrameSize-len(socket.incoming) {
			socket.close()
			return fmt.Errorf("local relay output exceeds limit")
		}
		socket.incoming = append(socket.incoming, wire...)
		socket.pending = socket.pending[n:]
	}
	socket.pending = nil
	return nil
}

func runtimeRelayClasses() []runtimeJavaClass {
	method := func(class, name, descriptor string, body runtimeJavaImplementation) runtimeJavaMethod {
		return runtimeJavaMethod{class: class, name: name, descriptor: descriptor, accessFlags: 1, implementation: body}
	}
	return []runtimeJavaClass{
		{name: runtimeRelaySocketClass, superName: "java/lang/Object", accessFlags: 0x21, methods: []runtimeJavaMethod{
			method(runtimeRelaySocketClass, "getInputStream", "()Ljava/io/InputStream;", runtimeRelayInput),
			method(runtimeRelaySocketClass, "getOutputStream", "()Ljava/io/OutputStream;", runtimeRelayOutput),
			method(runtimeRelaySocketClass, "isStream", "()Z", runtimeRelayIsStream),
			method(runtimeRelaySocketClass, "close", "()V", runtimeRelayClose)}},
		{name: runtimeRelayInputClass, superName: "java/io/InputStream", accessFlags: 0x21, methods: []runtimeJavaMethod{
			method(runtimeRelayInputClass, "read", "()I", runtimeRelayRead),
			method(runtimeRelayInputClass, "read", "([B)I", runtimeRelayRead),
			method(runtimeRelayInputClass, "read", "([BII)I", runtimeRelayRead),
			method(runtimeRelayInputClass, "available", "()I", runtimeRelayAvailable),
			method(runtimeRelayInputClass, "close", "()V", runtimeRelayClose)}},
		{name: runtimeRelayOutputClass, superName: "java/io/OutputStream", accessFlags: 0x21, methods: []runtimeJavaMethod{
			method(runtimeRelayOutputClass, "write", "(I)V", runtimeRelayWrite),
			method(runtimeRelayOutputClass, "write", "([B)V", runtimeRelayWrite),
			method(runtimeRelayOutputClass, "write", "([BII)V", runtimeRelayWrite),
			method(runtimeRelayOutputClass, "flush", "()V", runtimeRelayFlush),
			method(runtimeRelayOutputClass, "close", "()V", runtimeRelayClose)}},
	}
}

func relayIOException(err error) error {
	return &jvm.GuestException{Object: &jvm.Object{ClassName: "java/io/IOException"}, Message: err.Error()}
}
func relayState(args []jvm.Value) (*relaySocket, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("relay receiver missing")
	}
	receiver, err := args[0].Reference()
	if err != nil {
		return nil, err
	}
	if receiver == nil {
		return nil, fmt.Errorf("relay receiver is null")
	}
	socket, ok := receiver.Native.(*relaySocket)
	if !ok {
		return nil, fmt.Errorf("invalid relay receiver")
	}
	return socket, nil
}
func runtimeRelayInput(_ *initializationRuntime, _ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
	socket, err := relayState(args)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if socket.closed || socket.socketClosed {
		return jvm.VoidValue(), relayIOException(fmt.Errorf("local relay socket is closed"))
	}
	if socket.input == nil {
		socket.input = &jvm.Object{ClassName: runtimeRelayInputClass, Native: socket}
	}
	return jvm.ReferenceValue(socket.input), nil
}
func runtimeRelayOutput(_ *initializationRuntime, _ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
	socket, err := relayState(args)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if socket.closed || socket.socketClosed {
		return jvm.VoidValue(), relayIOException(fmt.Errorf("local relay socket is closed"))
	}
	if socket.output == nil {
		socket.output = &jvm.Object{ClassName: runtimeRelayOutputClass, Native: socket}
	}
	return jvm.ReferenceValue(socket.output), nil
}
func runtimeRelayIsStream(_ *initializationRuntime, _ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
	if _, err := relayState(args); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(1), nil
}
func runtimeRelayClose(_ *initializationRuntime, _ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
	socket, err := relayState(args)
	if err != nil {
		return jvm.VoidValue(), err
	}
	receiver, _ := args[0].Reference()
	switch receiver.ClassName {
	case runtimeRelayInputClass:
		socket.inputClosed = true
	case runtimeRelayOutputClass:
		socket.outputClosed = true
	default:
		socket.socketClosed = true
	}
	// WIPI keeps returned streams alive until their own close calls, even
	// after Socket.close. Network.disconnect instead closes all handles.
	if socket.socketClosed && (socket.input == nil || socket.inputClosed) && (socket.output == nil || socket.outputClosed) {
		socket.close()
	}
	return jvm.VoidValue(), nil
}
func runtimeRelayFlush(_ *initializationRuntime, _ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
	socket, err := relayState(args)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if socket.closed || socket.outputClosed {
		return jvm.VoidValue(), relayIOException(fmt.Errorf("local relay socket is closed"))
	}
	return jvm.VoidValue(), nil
}
func runtimeRelayAvailable(_ *initializationRuntime, _ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
	socket, err := relayState(args)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if socket.closed || socket.inputClosed {
		return jvm.VoidValue(), relayIOException(fmt.Errorf("local relay socket is closed"))
	}
	return jvm.IntValue(int32(len(socket.incoming))), nil
}

// Validate a byte-array range before waiting or copying. A bad guest length
// must not turn into a host slice panic or a parked worker.
func relayArrayRange(args []jvm.Value) (*jvm.Object, int, int, error) {
	array, err := args[1].Reference()
	if err != nil {
		return nil, 0, 0, err
	}
	if array == nil {
		return nil, 0, 0, &jvm.GuestException{Object: &jvm.Object{ClassName: "java/lang/NullPointerException"}, Message: "relay byte array is null"}
	}
	component, size, ok := jvm.ArrayComponent(array)
	if !ok {
		return nil, 0, 0, fmt.Errorf("relay requires byte array")
	}
	if component.Kind != jvm.TypeByte {
		return nil, 0, 0, fmt.Errorf("relay requires byte array")
	}
	offset, length := int32(0), int32(size)
	if len(args) == 4 {
		offset, err = args[2].Int32()
		if err != nil {
			return nil, 0, 0, err
		}
		length, err = args[3].Int32()
		if err != nil {
			return nil, 0, 0, err
		}
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(size) {
		return nil, 0, 0, &jvm.GuestException{Object: &jvm.Object{ClassName: "java/lang/IndexOutOfBoundsException"}, Message: "relay byte array range"}
	}
	return array, int(offset), int(length), nil
}
func runtimeRelayRead(runtime *initializationRuntime, _ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
	socket, err := relayState(args)
	if err != nil {
		return jvm.VoidValue(), err
	}
	var array *jvm.Object
	offset, length := 0, 1
	if len(args) != 1 {
		array, offset, length, err = relayArrayRange(args)
		if err != nil {
			return jvm.VoidValue(), err
		}
	}
	if socket.closed || socket.inputClosed {
		return jvm.VoidValue(), relayIOException(fmt.Errorf("local relay socket is closed"))
	}
	if length == 0 {
		return jvm.IntValue(0), nil
	}
	for len(socket.incoming) == 0 {
		if socket.closed || socket.inputClosed {
			return jvm.VoidValue(), relayIOException(fmt.Errorf("local relay socket is closed"))
		}
		if runtime.client.activeWorker == nil {
			return jvm.VoidValue(), relayIOException(fmt.Errorf("local relay read requires a guest worker"))
		}
		if err := runtime.yieldCurrentWorker(); err != nil {
			return jvm.VoidValue(), err
		}
	}
	n := min(length, len(socket.incoming))
	value := int32(socket.incoming[0])
	if array != nil {
		values := make([]jvm.Value, n)
		for i := range values {
			values[i] = jvm.IntValue(int32(int8(socket.incoming[i])))
		}
		if err := jvm.SetArrayRange(array, offset, values); err != nil {
			return jvm.VoidValue(), err
		}
		value = int32(n)
	}
	socket.incoming = socket.incoming[n:]
	return jvm.IntValue(value), nil
}
func runtimeRelayWrite(runtime *initializationRuntime, _ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
	socket, err := relayState(args)
	if err != nil {
		return jvm.VoidValue(), err
	}
	var data []byte
	if len(args) == 2 && args[1].Kind() != jvm.ValueReference {
		value, err := args[1].Int32()
		if err != nil {
			return jvm.VoidValue(), err
		}
		data = []byte{byte(value)}
	} else {
		array, offset, length, err := relayArrayRange(args)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if length > maxRelayFrameSize {
			return jvm.VoidValue(), relayIOException(fmt.Errorf("local relay write exceeds limit"))
		}
		values, err := array.Native.(*jvm.Array).LoadRange(offset, length)
		if err != nil {
			return jvm.VoidValue(), err
		}
		data = make([]byte, length)
		for i, v := range values {
			n, err := v.Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
			data[i] = byte(n)
		}
	}
	phase := socket.service.phase
	if err := socket.write(data); err != nil {
		runtime.countDiagnostic("local relay refused: " + err.Error())
		return jvm.VoidValue(), relayIOException(err)
	}
	if socket.service.phase != phase {
		runtime.countDiagnostic(fmt.Sprintf("local slot relay phase %d", socket.service.phase))
	}
	return jvm.VoidValue(), nil
}

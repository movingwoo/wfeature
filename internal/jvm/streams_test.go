package jvm

import (
	"bytes"
	_ "embed"
	"math"
	"testing"
)

//go:embed testdata/Streams.class
var streamsClass []byte

// A title may write its save through java.io.DataOutput and read it back
// through java.io.DataInput, naming the interfaces and passing the streams.
// The interface calls have always dispatched on the class of the receiver, but
// `instanceof` asks the stream's declaration whether it implements the named
// interface. Both questions have to agree.
func TestInterfaceCallsLandOnTheStreamThatWasPassed(t *testing.T) {
	vm := New(mapClassSource{"Streams": streamsClass}, Options{})
	result, err := vm.InvokeStatic("Streams", "roundTrip", "()I")
	if err != nil {
		t.Fatalf("roundTrip() error = %v", err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	// 4660 + 5 + 1 + 1000, plus one and two for the exact float and double
	// round trips, written and read back in that order.
	if value != 5669 {
		t.Fatalf("roundTrip() = %d, want 5669", value)
	}
}

func TestDataStreamsImplementTheirCLDCInterfaces(t *testing.T) {
	vm := New(nil, Options{})
	for _, interfaceName := range []string{DataInputClass, DataOutputClass} {
		class, err := vm.loader.Load(interfaceName)
		if err != nil {
			t.Fatalf("Load(%s) error = %v", interfaceName, err)
		}
		if class.AccessFlags&AccessInterface == 0 {
			t.Errorf("%s is not declared as an interface", interfaceName)
		}
	}

	sink, err := vm.NewObject(ByteArrayOutputStreamClass, "()V")
	if err != nil {
		t.Fatalf("NewObject(ByteArrayOutputStream) error = %v", err)
	}
	output, err := vm.NewObject(DataOutputStreamClass, "(Ljava/io/OutputStream;)V", ReferenceValue(sink))
	if err != nil {
		t.Fatalf("NewObject(DataOutputStream) error = %v", err)
	}
	if !vm.IsInstance(output, DataOutputClass) {
		t.Error("DataOutputStream is not assignable to DataOutput")
	}

	source, err := vm.NewObject(ByteArrayInputStreamClass, "([B)V", ReferenceValue(NewByteArray(nil)))
	if err != nil {
		t.Fatalf("NewObject(ByteArrayInputStream) error = %v", err)
	}
	input, err := vm.NewObject(DataInputStreamClass, "(Ljava/io/InputStream;)V", ReferenceValue(source))
	if err != nil {
		t.Fatalf("NewObject(DataInputStream) error = %v", err)
	}
	if !vm.IsInstance(input, DataInputClass) {
		t.Error("DataInputStream is not assignable to DataInput")
	}
}

func TestDataOutputStreamCanonicalizesNaNThroughTheInterface(t *testing.T) {
	vm := New(mapClassSource{"Streams": streamsClass}, Options{})
	for _, probe := range []struct {
		name       string
		method     string
		descriptor string
		value      Value
		want       []byte
	}{
		{
			name:       "float",
			method:     "encodeFloat",
			descriptor: "(F)[B",
			value:      FloatValue(math.Float32frombits(0x7fa12345)),
			want:       []byte{0x7f, 0xc0, 0x00, 0x00},
		},
		{
			name:       "double",
			method:     "encodeDouble",
			descriptor: "(D)[B",
			value:      DoubleValue(math.Float64frombits(0x7ff0123456789abc)),
			want:       []byte{0x7f, 0xf8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			result, err := vm.InvokeStatic("Streams", probe.method, probe.descriptor, probe.value)
			if err != nil {
				t.Fatalf("%s() error = %v", probe.method, err)
			}
			array, err := result.Reference()
			if err != nil {
				t.Fatal(err)
			}
			got, err := ByteArraySnapshot(array)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, probe.want) {
				t.Fatalf("%s(noncanonical NaN) = % x, want canonical % x", probe.method, got, probe.want)
			}
		})
	}
}

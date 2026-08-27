package ktf

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestClientInvokesSyntheticAOTConstructorAndMethods(t *testing.T) {
	client := syntheticLifecycleClient(t)

	object, constructor, err := client.NewObject(context.Background(), "game/Lifecycle", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}
	if object.ClassName != "game/Lifecycle" || len(constructor.Runs) != 1 || constructor.Runs[0].Steps != 1 {
		t.Fatalf("NewObject() = %+v, summary = %+v", object, constructor)
	}
	address, ok := client.JVM().AOTAddress(object)
	if !ok || address == 0 {
		t.Fatalf("constructed object address = %#x/%t", address, ok)
	}

	instance, err := client.InvokeVirtual(context.Background(), object, "add", "(I)I", jvm.IntValue(9))
	if err != nil {
		t.Fatalf("InvokeVirtual(add) error = %v", err)
	}
	instanceResult, err := instance.Result.Int32()
	if err != nil || instanceResult != 14 {
		t.Fatalf("InvokeVirtual(add) = %d/%v, want 14", instanceResult, err)
	}

	static, err := client.InvokeStatic(context.Background(), "game/Lifecycle", "staticAdd", "(I)I", jvm.IntValue(4))
	if err != nil {
		t.Fatalf("InvokeStatic(staticAdd) error = %v", err)
	}
	staticResult, err := static.Result.Int32()
	if err != nil || staticResult != 11 {
		t.Fatalf("InvokeStatic(staticAdd) = %d/%v, want 11", staticResult, err)
	}

	reference, err := client.InvokeVirtual(context.Background(), object, "self", "()Ljava/lang/Object;")
	if err != nil {
		t.Fatalf("InvokeVirtual(self) error = %v", err)
	}
	returned, err := reference.Result.Reference()
	if err != nil || returned != object {
		t.Fatalf("InvokeVirtual(self) = %p/%v, want %p", returned, err, object)
	}
}

func TestClientAOTCategoryTwoABI(t *testing.T) {
	client := syntheticLifecycleClient(t)
	object, _, err := client.NewObject(context.Background(), "game/Lifecycle", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}

	for _, want := range []int64{0x0123456789abcdef, -2, 0} {
		echoed, invokeErr := client.InvokeVirtual(context.Background(), object, "echoLong", "(J)J", jvm.LongValue(want))
		if invokeErr != nil {
			t.Fatalf("InvokeVirtual(echoLong, %#x) error = %v", want, invokeErr)
		}
		got, valueErr := echoed.Result.Int64()
		if valueErr != nil || got != want {
			t.Fatalf("InvokeVirtual(echoLong) = %#x/%v, want %#x", got, valueErr, want)
		}
	}

	for _, want := range []float64{-2.5e-3, math.Inf(1), 1 << 62} {
		echoed, invokeErr := client.InvokeStatic(context.Background(), "game/Lifecycle", "echoDouble", "(D)D", jvm.DoubleValue(want))
		if invokeErr != nil {
			t.Fatalf("InvokeStatic(echoDouble, %g) error = %v", want, invokeErr)
		}
		got, valueErr := echoed.Result.Float64()
		if valueErr != nil || math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("InvokeStatic(echoDouble) = %g/%v, want bit-exact %g", got, valueErr, want)
		}
	}

	// Two leading ints push the long's high word past r3 onto the stack, so
	// this pins the register/stack straddle of a category-2 argument.
	const spilled = int64(0x7fffeeee_ddddcccc)
	straddled, err := client.InvokeStatic(context.Background(), "game/Lifecycle", "spillLong", "(IIJ)J", jvm.IntValue(1), jvm.IntValue(2), jvm.LongValue(spilled))
	if err != nil {
		t.Fatalf("InvokeStatic(spillLong) error = %v", err)
	}
	if got, valueErr := straddled.Result.Int64(); valueErr != nil || got != spilled {
		t.Fatalf("InvokeStatic(spillLong) = %#x/%v, want %#x", got, valueErr, spilled)
	}
}

func TestClientOuterCallHandsOffGuestExceptionTyped(t *testing.T) {
	client := syntheticLifecycleClient(t)
	object, _, err := client.NewObject(context.Background(), "game/Lifecycle", "()V")
	if err != nil {
		t.Fatalf("NewObject() error = %v", err)
	}

	_, err = client.InvokeVirtual(context.Background(), object, "boom", "()V")
	if err == nil {
		t.Fatal("InvokeVirtual(boom) succeeded, want an uncaught guest exception")
	}
	var uncaught *UncaughtAOTException
	if !errors.As(err, &uncaught) || uncaught.Exception == nil || uncaught.Exception.Object == nil {
		t.Fatalf("InvokeVirtual(boom) error = %v, want UncaughtAOTException with a pinned object", err)
	}
	if uncaught.Exception.Object.ClassName != "java/lang/RuntimeException" {
		t.Fatalf("uncaught class = %q", uncaught.Exception.Object.ClassName)
	}
	if !client.JVM().IsGuestException(err, "java/lang/RuntimeException") {
		t.Fatalf("outer-call error %v is not visible as a typed JVM exception", err)
	}

	// The wrapper must restore the entry exception-handler head so the same
	// client keeps working after an uncaught throw.
	head, err := client.Core().ThreadLocalWord(client.thread, client.runtime.exceptionContext+javaExceptionHead)
	if err != nil || head != 0 {
		t.Fatalf("exception handler head after uncaught throw = %#x/%v, want 0", head, err)
	}
	next, err := client.InvokeVirtual(context.Background(), object, "add", "(I)I", jvm.IntValue(1))
	if err != nil {
		t.Fatalf("InvokeVirtual(add) after uncaught throw error = %v", err)
	}
	if result, valueErr := next.Result.Int32(); valueErr != nil || result != 6 {
		t.Fatalf("InvokeVirtual(add) after uncaught throw = %d/%v, want 6", result, valueErr)
	}
}

func TestClientRejectsUnboundAOTReferenceArgument(t *testing.T) {
	client := syntheticLifecycleClient(t)
	_, err := client.InvokeStatic(
		context.Background(),
		"game/Lifecycle",
		"take",
		"(Ljava/lang/Object;)V",
		jvm.ReferenceValue(client.JVM().NewString("host-only")),
	)
	if err == nil || !strings.Contains(err.Error(), "has no KTF guest address") {
		t.Fatalf("InvokeStatic(take) error = %v, want unbound-reference failure", err)
	}
}

func syntheticLifecycleClient(t *testing.T) *Client {
	t.Helper()
	data := syntheticInitializableClient()
	data = append(data, make([]byte, 0x200-len(data))...)
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: data}, armcore.CoreOptions{MaxSteps: 1_000})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := client.ExecuteEntry(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Initialize(context.Background(), entry.Context.Registers[0]); err != nil {
		t.Fatal(err)
	}

	runtime := client.runtime
	classAddress, err := runtime.allocate(javaClassSize)
	if err != nil {
		t.Fatal(err)
	}
	className, err := runtime.allocateBytes([]byte("game/Lifecycle\x00"))
	if err != nil {
		t.Fatal(err)
	}
	methodSpecs := []struct {
		fullName string
		body     uint32
		flags    uint16
	}{
		{fullName: "()V+<init>", body: ImageBase + 0x161, flags: 0x0001},
		{fullName: "(I)I+add", body: ImageBase + 0x169, flags: 0x0001},
		{fullName: "(I)I+staticAdd", body: ImageBase + 0x175, flags: 0x0009},
		{fullName: "()Ljava/lang/Object;+self", body: ImageBase + 0x181, flags: 0x0001},
		{fullName: "(Ljava/lang/Object;)V+take", body: ImageBase + 0x189, flags: 0x0009},
		{fullName: "(J)J+echoLong", body: ImageBase + 0x191, flags: 0x0001},
		{fullName: "(D)D+echoDouble", body: ImageBase + 0x199, flags: 0x0009},
		{fullName: "(IIJ)J+spillLong", body: ImageBase + 0x1a1, flags: 0x0009},
		{fullName: "()V+boom", body: ImageBase + 0x1b1, flags: 0x0001},
		// The Jlet lifecycle. Each writes what it was told into the receiver's
		// own first word, which is all a test needs to see: the platform
		// either ran the body in the class's method table or it did not.
		{fullName: "()V+pauseApp", body: ImageBase + 0x1c1, flags: 0x0001},
		{fullName: "()V+resumeApp", body: ImageBase + 0x1c9, flags: 0x0001},
	}
	methodPointers := make([]uint32, 0, len(methodSpecs)+1)
	for _, spec := range methodSpecs {
		fullName, allocationErr := runtime.allocateBytes(append([]byte{0}, append([]byte(spec.fullName), 0)...))
		if allocationErr != nil {
			t.Fatal(allocationErr)
		}
		methodAddress, allocationErr := runtime.allocate(javaMethodSize)
		if allocationErr != nil {
			t.Fatal(allocationErr)
		}
		writeTestWords(t, client, methodAddress, []uint32{
			spec.body,
			classAddress,
			0,
			fullName,
			0,
			uint32(spec.flags) << 16,
			0,
		})
		methodPointers = append(methodPointers, methodAddress)
	}
	methodPointers = append(methodPointers, 0)
	methodTable, err := runtime.allocateWords(methodPointers)
	if err != nil {
		t.Fatal(err)
	}
	fieldTable, err := runtime.allocateWords([]uint32{0})
	if err != nil {
		t.Fatal(err)
	}
	vtable, err := runtime.allocateWords(methodPointers[:2])
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := runtime.allocate(javaDescriptorSize)
	if err != nil {
		t.Fatal(err)
	}
	descriptorData := make([]byte, javaDescriptorSize)
	binary.LittleEndian.PutUint32(descriptorData[0:], className)
	binary.LittleEndian.PutUint32(descriptorData[12:], methodTable)
	binary.LittleEndian.PutUint32(descriptorData[20:], fieldTable)
	binary.LittleEndian.PutUint16(descriptorData[26:], 4)
	binary.LittleEndian.PutUint16(descriptorData[28:], 0x0021)
	if err := client.Core().Memory().Write(descriptor, descriptorData); err != nil {
		t.Fatal(err)
	}
	classData := make([]byte, javaClassSize)
	binary.LittleEndian.PutUint32(classData[0:], classAddress+4)
	binary.LittleEndian.PutUint32(classData[8:], descriptor)
	binary.LittleEndian.PutUint32(classData[12:], vtable)
	binary.LittleEndian.PutUint16(classData[16:], 1)
	if err := client.Core().Memory().Write(classAddress, classData); err != nil {
		t.Fatal(err)
	}

	writeThumb := func(offset int, instructions ...uint16) {
		t.Helper()
		bytes := make([]byte, len(instructions)*2)
		for index, instruction := range instructions {
			binary.LittleEndian.PutUint16(bytes[index*2:], instruction)
		}
		if err := client.Core().Memory().Write(ImageBase+uint32(offset), bytes); err != nil {
			t.Fatal(err)
		}
	}
	writeThumb(0x150, 0x4800, 0x4770) // GetClass: ldr r0, literal; bx lr.
	writeTestWords(t, client, ImageBase+0x154, []uint32{classAddress})
	writeThumb(0x160, 0x4770)                 // <init>: bx lr.
	writeThumb(0x168, 0x4610, 0x3005, 0x4770) // add: r0 = r2 + 5.
	writeThumb(0x174, 0x4608, 0x3007, 0x4770) // staticAdd: r0 = r1 + 7.
	writeThumb(0x180, 0x4608, 0x4770)         // self: r0 = r1.
	writeThumb(0x188, 0x4770)                 // take: bx lr.
	writeThumb(0x190, 0x4610, 0x4619, 0x4770) // echoLong: r0:r1 = r2:r3 (receiver in r1).
	writeThumb(0x198, 0x4608, 0x4611, 0x4770) // echoDouble: r0:r1 = r1:r2.
	writeThumb(0x1a0, 0x4618, 0x9900, 0x4770) // spillLong: r0 = r3, r1 = [sp] (high word spilled).
	exceptionName, err := runtime.allocateBytes([]byte("java/lang/RuntimeException\x00"))
	if err != nil {
		t.Fatal(err)
	}
	throwStub, err := runtime.stub(svcCategoryInit, initSVCJavaThrow)
	if err != nil {
		t.Fatal(err)
	}
	// boom: r0 = exception class name, then tail-branch into the platform's
	// JavaThrow callback stub — the same route a real client's athrow takes.
	writeThumb(0x1b0, 0x4801, 0x4b02, 0x4718, 0x0000)
	writeTestWords(t, client, ImageBase+0x1b8, []uint32{exceptionName, throwStub})
	// pauseApp and resumeApp: store a marker in the receiver's first field.
	// An instance method takes its receiver in r1, and an object's fields
	// start after its two header words and its vtable header — see
	// allocateAOTObject — so the class's one field is at +12.
	writeThumb(0x1c0, 0x2002, 0x60c8, 0x4770) // movs r0,#2; str r0,[r1,#12]; bx lr
	writeThumb(0x1c8, 0x2003, 0x60c8, 0x4770) // movs r0,#3; str r0,[r1,#12]; bx lr
	client.executable.Interface.Functions.GetClass = ImageBase + 0x151
	return client
}

// lifecycleMarker reads the word the fixture's callbacks write into their
// receiver.
func lifecycleMarker(t *testing.T, client *Client, object *jvm.Object) uint32 {
	t.Helper()
	address, ok := client.JVM().AOTAddress(object)
	if !ok || address == 0 {
		t.Fatalf("the object is not bound to guest memory (%#x/%t)", address, ok)
	}
	var word [4]byte
	if err := client.Core().Memory().Read(address+javaInstanceSize+javaInstanceHeader, word[:]); err != nil {
		t.Fatalf("read the lifecycle marker: %v", err)
	}
	return binary.LittleEndian.Uint32(word[:])
}

// A touch is offered to a title that wrote somewhere for it to land, and to no
// other. The rule reads class metadata, so it is checked against metadata: the
// platform's own registration of `Card.pointerNotify` is a native body and is
// exactly what a title that wrote nothing inherits, which is why it may not
// count as the title having a pointer.
func TestOnlyATitleThatOverridesPointerNotifyIsOfferedATouch(t *testing.T) {
	cases := []struct {
		name    string
		classes []jvm.AOTClassMetadata
		want    bool
	}{
		{name: "a title with no pointerNotify at all", classes: []jvm.AOTClassMetadata{
			{Name: "Clet", Methods: []jvm.AOTMethodMetadata{
				{Name: "startApp", Descriptor: "([Ljava/lang/String;)V", Body: ImageBase + 0x100},
				{Name: "keyNotify", Descriptor: "(II)Z", Body: ImageBase + 0x200},
			}},
		}, want: false},
		{name: "the platform's own body standing in for the title's", classes: []jvm.AOTClassMetadata{
			{Name: "org/kwis/msp/lcdui/Card", Methods: []jvm.AOTMethodMetadata{
				{Name: "pointerNotify", Descriptor: "(III)Z", NativeBody: ImageBase + 0x300},
			}},
		}, want: false},
		{name: "a body of the title's own", classes: []jvm.AOTClassMetadata{
			{Name: "Clet$CletCard", Methods: []jvm.AOTMethodMetadata{
				{Name: "pointerNotify", Descriptor: "(III)Z", Body: ImageBase + 0x400},
			}},
		}, want: true},
		{name: "the name without the descriptor is another method", classes: []jvm.AOTClassMetadata{
			{Name: "Clet$CletCard", Methods: []jvm.AOTMethodMetadata{
				{Name: "pointerNotify", Descriptor: "(II)Z", Body: ImageBase + 0x500},
			}},
		}, want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := aotDeclaresPointerNotify(test.classes); got != test.want {
				t.Errorf("a touch offered: %v, want %v", got, test.want)
			}
		})
	}
}

// A client with no title loaded answers no rather than crashing: the Host asks
// this on the way into a session and on the way out of a failed one.
func TestAClientWithNothingLoadedIsNotOfferedATouch(t *testing.T) {
	if (*Client)(nil).HasPointer() {
		t.Error("a client that is not there says it takes a touch")
	}
	if (*Session)(nil).HasPointer() {
		t.Error("a session that is not there says it takes a touch")
	}
}

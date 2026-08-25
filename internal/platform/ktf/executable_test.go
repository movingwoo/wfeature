package ktf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestSyntheticClientReturnsValidatedExecutable(t *testing.T) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticExecutableClient()}, armcore.CoreOptions{MaxSteps: 10})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := client.ExecuteEntry(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Steps != 3 || summary.Context.Registers[0] != ImageBase+0x40 {
		t.Fatalf("entry steps=%d result=%#x, want 3/%#x", summary.Steps, summary.Context.Registers[0], ImageBase+0x40)
	}

	executable, err := client.ReadExecutable(summary.Context.Registers[0])
	if err != nil {
		t.Fatal(err)
	}
	if executable.Address != ImageBase+0x40 || executable.Name != "WIPI_exe" || executable.Init != ImageBase+0x29 {
		t.Fatalf("executable = %+v", executable)
	}
	if executable.Interface.Address != ImageBase+0x80 || executable.Interface.Name != "ExeInterface" {
		t.Fatalf("interface = %+v", executable.Interface)
	}
	functions := executable.Interface.Functions
	if functions.Address != ImageBase+0xa0 || functions.Init != ImageBase+0x21 || functions.GetDefaultDLL != 0 || functions.GetClass != ImageBase+0x25 {
		t.Fatalf("functions = %+v", functions)
	}
}

func TestSyntheticClientRunsInterfaceAndWIPIInitialization(t *testing.T) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{MaxSteps: 100})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := client.ExecuteEntry(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := client.Initialize(context.Background(), entry.Context.Registers[0])
	if err != nil {
		t.Fatal(err)
	}
	if summary.Interface.Context.Registers[0] != 0 || summary.WIPI.Context.Registers[0] != 0 {
		t.Fatalf("initialization results = interface %#x, WIPI %#x", summary.Interface.Context.Registers[0], summary.WIPI.Context.Registers[0])
	}
	if summary.Callbacks.Allocations != 1 {
		t.Fatalf("allocation callbacks = %d, want 1", summary.Callbacks.Allocations)
	}
	if _, err := client.Initialize(context.Background(), entry.Context.Registers[0]); err == nil || !strings.Contains(err.Error(), "already started") {
		t.Fatalf("second Initialize() error = %v, want already-started failure", err)
	}
}

func TestInitializationRejectsOversizedGuestAllocation(t *testing.T) {
	data := syntheticInitializableClient()
	for index, instruction := range []uint16{
		0x4803, // ldr r0, [pc, #12]
		0x2406, // movs r4, #alloc-id
		0x46a4, // mov r12, r4
		0xdf01, // svc #init
		0x2000,
		0x4770,
		0x46c0,
		0x46c0,
	} {
		binary.LittleEndian.PutUint16(data[0x20+index*2:], instruction)
	}
	binary.LittleEndian.PutUint32(data[0x30:], uint32(maxPlatformAllocation+1))
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: data}, armcore.CoreOptions{MaxSteps: 100})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := client.ExecuteEntry(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Initialize(context.Background(), entry.Context.Registers[0])
	if err == nil || !strings.Contains(err.Error(), "allocation") || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Initialize() error = %v, want bounded allocation failure", err)
	}
}

func TestInitializationBindsJavaObjectsToJVM(t *testing.T) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}

	stringData, err := runtime.allocateBytes([]byte{'K', 0, 'T', 0, 'F', 0})
	if err != nil {
		t.Fatal(err)
	}
	stringContext := armcore.NewContext()
	stringContext.Registers[0] = stringData
	stringContext.Registers[1] = 3
	stringAddress, err := runtime.registerJavaString(armcore.NewThread(stringContext))
	if err != nil {
		t.Fatalf("registerJavaString() error = %v", err)
	}
	stringObject, ok := client.JVM().AOTObject(stringAddress)
	if !ok {
		t.Fatalf("JVM has no object for guest string %#x", stringAddress)
	}
	result, err := client.JVM().InvokeVirtual(stringObject, "length", "()I")
	if err != nil {
		t.Fatalf("InvokeVirtual(String.length) error = %v", err)
	}
	length, err := result.Int32()
	if err != nil || length != 3 {
		t.Fatalf("registered String.length = %d/%v, want 3", length, err)
	}

	classTarget, err := runtime.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	className, err := runtime.allocateBytes([]byte("java/lang/String\x00"))
	if err != nil {
		t.Fatal(err)
	}
	classContext := armcore.NewContext()
	classContext.Registers[0] = classTarget
	classContext.Registers[1] = className
	if _, err := runtime.loadJavaClass(armcore.NewThread(classContext)); err != nil {
		t.Fatalf("loadJavaClass() error = %v", err)
	}
	var classResult [4]byte
	if err := client.Core().Memory().Read(classTarget, classResult[:]); err != nil {
		t.Fatal(err)
	}
	classAddress := binary.LittleEndian.Uint32(classResult[:])
	classObject, ok := client.JVM().AOTObject(classAddress)
	if !ok || classObject.ClassName != "java/lang/Class" || classObject.Native != "java/lang/String" {
		t.Fatalf("bound class object = %+v/%t", classObject, ok)
	}
}

func TestInitializationRegistersAOTClassMetadataWithJVM(t *testing.T) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}

	classAddress, err := runtime.allocate(20)
	if err != nil {
		t.Fatal(err)
	}
	className, _ := runtime.allocateBytes([]byte("game/Example\x00"))
	methodName, _ := runtime.allocateBytes(append([]byte{0}, []byte("(I)I+run\x00")...))
	fieldName, _ := runtime.allocateBytes(append([]byte{0}, []byte("I+value\x00")...))
	methodAddress, _ := runtime.allocate(28)
	fieldAddress, _ := runtime.allocate(16)
	methodTable, _ := runtime.allocateWords([]uint32{methodAddress, 0})
	fieldTable, _ := runtime.allocateWords([]uint32{fieldAddress, 0})
	vtable, _ := runtime.allocateWords([]uint32{methodAddress})
	descriptor, _ := runtime.allocate(36)

	writeTestWords(t, client, methodAddress, []uint32{0x101001, classAddress, 0, methodName, 0, 0x00010000, 0})
	writeTestWords(t, client, fieldAddress, []uint32{0x0001, classAddress, fieldName, 4})
	descriptorData := make([]byte, 36)
	binary.LittleEndian.PutUint32(descriptorData[0:], className)
	binary.LittleEndian.PutUint32(descriptorData[12:], methodTable)
	binary.LittleEndian.PutUint32(descriptorData[20:], fieldTable)
	binary.LittleEndian.PutUint16(descriptorData[24:], 1)
	binary.LittleEndian.PutUint16(descriptorData[26:], 8)
	binary.LittleEndian.PutUint16(descriptorData[28:], 0x21)
	if err := client.Core().Memory().Write(descriptor, descriptorData); err != nil {
		t.Fatal(err)
	}
	classData := make([]byte, 20)
	binary.LittleEndian.PutUint32(classData[0:], classAddress+4)
	binary.LittleEndian.PutUint32(classData[8:], descriptor)
	binary.LittleEndian.PutUint32(classData[12:], vtable)
	binary.LittleEndian.PutUint16(classData[16:], 1)
	if err := client.Core().Memory().Write(classAddress, classData); err != nil {
		t.Fatal(err)
	}

	registerContext := armcore.NewContext()
	registerContext.Registers[0] = classAddress
	if _, err := runtime.registerAOTClass(context.Background(), armcore.NewThread(registerContext)); err != nil {
		t.Fatalf("registerAOTClass() error = %v", err)
	}
	metadata, ok := client.JVM().AOTClass("game/Example")
	if !ok {
		t.Fatal("registered AOT class is missing from the JVM")
	}
	if metadata.Address != classAddress || metadata.InstanceSize != 8 || metadata.VTableAddress != vtable || len(metadata.VTable) != 1 || len(metadata.Methods) != 1 || len(metadata.Fields) != 1 {
		t.Fatalf("AOT metadata = %+v", metadata)
	}
	if metadata.Methods[0].Name != "run" || metadata.Methods[0].Descriptor != "(I)I" || metadata.Fields[0].Name != "value" {
		t.Fatalf("AOT members = %+v / %+v", metadata.Methods, metadata.Fields)
	}
	methodContext := armcore.NewContext()
	methodContext.Registers[0] = classAddress
	methodContext.Registers[1] = methodName
	resolvedMethod, err := runtime.getAOTMethod(armcore.NewThread(methodContext))
	if err != nil || resolvedMethod != methodAddress {
		t.Fatalf("getAOTMethod() = %#x/%v, want %#x", resolvedMethod, err, methodAddress)
	}
	fieldContext := armcore.NewContext()
	fieldContext.Registers[0] = classAddress
	fieldContext.Registers[1] = fieldName
	resolvedField, err := runtime.getAOTField(armcore.NewThread(fieldContext))
	if err != nil || resolvedField != fieldAddress {
		t.Fatalf("getAOTField() = %#x/%v, want %#x", resolvedField, err, fieldAddress)
	}
	if err := client.Core().Memory().Write(methodName+1, []byte("I+run\x00")); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.registerAOTClass(context.Background(), armcore.NewThread(registerContext)); err == nil || !strings.Contains(err.Error(), "method descriptor") {
		t.Fatalf("registerAOTClass() error = %v, want invalid method descriptor", err)
	}
}

func TestInitializationCreatesBoundAOTObjectsAndArrays(t *testing.T) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}
	firstVTable, err := runtime.allocateWords([]uint32{ImageBase + 0x101})
	if err != nil {
		t.Fatal(err)
	}
	secondVTable, err := runtime.allocateWords([]uint32{ImageBase + 0x105})
	if err != nil {
		t.Fatal(err)
	}
	makeClassRecord := func(vtable uint32) uint32 {
		t.Helper()
		record := make([]byte, javaClassSize)
		binary.LittleEndian.PutUint32(record[12:], vtable)
		binary.LittleEndian.PutUint16(record[16:], 1)
		address, err := runtime.allocateBytes(record)
		if err != nil {
			t.Fatal(err)
		}
		next := make([]byte, 4)
		binary.LittleEndian.PutUint32(next, address+4)
		if err := client.Core().Memory().Write(address, next); err != nil {
			t.Fatal(err)
		}
		return address
	}
	classes := []jvm.AOTClassMetadata{
		{Address: makeClassRecord(firstVTable), Name: "game/First", InstanceSize: 8, VTableAddress: firstVTable, VTable: []uint32{ImageBase + 0x101}},
		{Address: makeClassRecord(secondVTable), Name: "game/Second", InstanceSize: 4, VTableAddress: secondVTable, VTable: []uint32{ImageBase + 0x105}},
	}
	for _, metadata := range classes {
		if err := client.JVM().RegisterAOTClass(metadata); err != nil {
			t.Fatal(err)
		}
	}

	newInstance := func(classAddress uint32) uint32 {
		t.Helper()
		callContext := armcore.NewContext()
		callContext.Registers[0] = classAddress
		address, err := runtime.handleInitCall(armcore.NewThread(callContext), initSVCJavaNew)
		if err != nil {
			t.Fatalf("JavaNew(%#x) error = %v", classAddress, err)
		}
		return address
	}
	first := newInstance(classes[0].Address)
	second := newInstance(classes[1].Address)

	firstData := readTestBytes(t, client, first, javaInstanceSize+javaInstanceHeader+int(classes[0].InstanceSize))
	if fields := binary.LittleEndian.Uint32(firstData[0:]); fields != first+javaInstanceSize {
		t.Fatalf("first fields pointer = %#x, want %#x", fields, first+javaInstanceSize)
	}
	if class := binary.LittleEndian.Uint32(firstData[4:]); class != classes[0].Address {
		t.Fatalf("first class pointer = %#x, want %#x", class, classes[0].Address)
	}
	// The header encodes a dispatch-alias class record relative to the JVM
	// context: guest dispatch reads its vtable at +12 and guest type checks
	// read its descriptor at +8.
	assertDispatchHeader := func(label string, header, classAddress uint32) {
		t.Helper()
		if header == 0 || header&0x1f != 0 {
			t.Fatalf("%s vtable header = %#x, want shifted alias offset", label, header)
		}
		alias := runtime.jvmContext + header>>5
		aliasData := readTestBytes(t, client, alias, javaClassSize)
		record := readTestBytes(t, client, classAddress, javaClassSize)
		if next := binary.LittleEndian.Uint32(aliasData[0:]); next != alias+4 {
			t.Fatalf("%s alias next pointer = %#x, want %#x", label, next, alias+4)
		}
		if !bytes.Equal(aliasData[4:], record[4:]) {
			t.Fatalf("%s alias record = %x, want copy of %x", label, aliasData[4:], record[4:])
		}
	}
	assertDispatchHeader("first", binary.LittleEndian.Uint32(firstData[8:]), classes[0].Address)
	for index, value := range firstData[12:] {
		if value != 0 {
			t.Fatalf("first instance field byte %d = %#x, want zero", index, value)
		}
	}
	if bound, ok := client.JVM().AOTObject(first); !ok || bound.ClassName != classes[0].Name {
		t.Fatalf("first bound object = %+v/%t", bound, ok)
	}

	secondData := readTestBytes(t, client, second, javaInstanceSize+javaInstanceHeader+int(classes[1].InstanceSize))
	assertDispatchHeader("second", binary.LittleEndian.Uint32(secondData[8:]), classes[1].Address)
	if firstHeader, secondHeader := binary.LittleEndian.Uint32(firstData[8:]), binary.LittleEndian.Uint32(secondData[8:]); firstHeader == secondHeader {
		t.Fatalf("distinct classes share dispatch header %#x", firstHeader)
	}
	third := newInstance(classes[0].Address)
	thirdData := readTestBytes(t, client, third, javaInstanceSize+javaInstanceHeader+int(classes[0].InstanceSize))
	if firstHeader, thirdHeader := binary.LittleEndian.Uint32(firstData[8:]), binary.LittleEndian.Uint32(thirdData[8:]); firstHeader != thirdHeader {
		t.Fatalf("same class produced dispatch headers %#x and %#x", firstHeader, thirdHeader)
	}

	arrayContext := armcore.NewContext()
	arrayContext.Registers[0] = 'I'
	arrayContext.Registers[1] = 3
	arrayAddress, err := runtime.handleInitCall(armcore.NewThread(arrayContext), initSVCJavaArrayNew)
	if err != nil {
		t.Fatalf("JavaArrayNew() error = %v", err)
	}
	arrayClass, ok := client.JVM().AOTClass("[I")
	if !ok {
		t.Fatal("runtime-created [I class is not registered")
	}
	arrayData := readTestBytes(t, client, arrayAddress, javaInstanceSize+javaInstanceHeader+javaArrayLengthSize+3*4)
	if fields := binary.LittleEndian.Uint32(arrayData[0:]); fields != arrayAddress+javaInstanceSize {
		t.Fatalf("array fields pointer = %#x, want %#x", fields, arrayAddress+javaInstanceSize)
	}
	if class := binary.LittleEndian.Uint32(arrayData[4:]); class != arrayClass.Address {
		t.Fatalf("array class pointer = %#x, want %#x", class, arrayClass.Address)
	}
	if length := binary.LittleEndian.Uint32(arrayData[12:]); length != 3 {
		t.Fatalf("raw array length = %d, want 3", length)
	}
	for index, value := range arrayData[16:] {
		if value != 0 {
			t.Fatalf("raw array byte %d = %#x, want zero", index, value)
		}
	}
	arrayObject, ok := client.JVM().AOTObject(arrayAddress)
	if !ok {
		t.Fatalf("JVM has no object for guest array %#x", arrayAddress)
	}
	component, values, err := jvm.ArraySnapshot(arrayObject)
	if err != nil || component.Kind != jvm.TypeInt || len(values) != 3 {
		t.Fatalf("bound array = component %s length %d error %v", component.Descriptor(), len(values), err)
	}

	referenceArrayClass := jvm.AOTClassMetadata{Address: makeClassRecord(0), Name: "[Lgame/First;"}
	if err := client.JVM().RegisterAOTClass(referenceArrayClass); err != nil {
		t.Fatal(err)
	}
	referenceContext := armcore.NewContext()
	referenceContext.Registers[0] = referenceArrayClass.Address
	referenceContext.Registers[1] = 2
	referenceAddress, err := runtime.handleInitCall(armcore.NewThread(referenceContext), initSVCJavaArrayNew)
	if err != nil {
		t.Fatalf("reference JavaArrayNew() error = %v", err)
	}
	referenceData := readTestBytes(t, client, referenceAddress, javaInstanceSize+javaInstanceHeader+javaArrayLengthSize+2*4)
	if class := binary.LittleEndian.Uint32(referenceData[4:]); class != referenceArrayClass.Address {
		t.Fatalf("reference array class pointer = %#x, want %#x", class, referenceArrayClass.Address)
	}
	if length := binary.LittleEndian.Uint32(referenceData[12:]); length != 2 {
		t.Fatalf("reference raw array length = %d, want 2", length)
	}
	referenceObject, ok := client.JVM().AOTObject(referenceAddress)
	if !ok {
		t.Fatalf("JVM has no object for guest reference array %#x", referenceAddress)
	}
	component, values, err = jvm.ArraySnapshot(referenceObject)
	if err != nil || component.Kind != jvm.TypeReference || component.ClassName != "game/First" || len(values) != 2 {
		t.Fatalf("bound reference array = component %s length %d error %v", component.Descriptor(), len(values), err)
	}
	if runtime.callbacks.Allocations != 5 {
		t.Fatalf("allocation callbacks = %d, want 5", runtime.callbacks.Allocations)
	}
}

func TestInitializationRejectsInvalidAOTArrayRequests(t *testing.T) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		elementType uint32
		count       uint32
		want        string
	}{
		{name: "unknown primitive", elementType: 'V', count: 1, want: "element type"},
		{name: "too many elements", elementType: 'I', count: maxJavaArrayElements + 1, want: "array length"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			callContext := armcore.NewContext()
			callContext.Registers[0] = test.elementType
			callContext.Registers[1] = test.count
			_, err := runtime.handleInitCall(armcore.NewThread(callContext), initSVCJavaArrayNew)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("JavaArrayNew() error = %v, want %q", err, test.want)
			}
		})
	}
	if _, err := runtime.aotVTableHeader(jvm.AOTClassMetadata{Name: "game/Unreadable", Address: 0x40000000}); err == nil || !strings.Contains(err.Error(), "dispatch alias") {
		t.Fatalf("aotVTableHeader() error = %v, want unreadable dispatch alias", err)
	}
}

func TestInitializationJavaJumpCallsAOTMethodBodies(t *testing.T) {
	data := syntheticInitializableClient()
	for index, instruction := range []uint16{
		0x3005, // adds r0, #5
		0x4770, // bx lr
		0x1840, // adds r0, r0, r1
		0x4770, // bx lr
		0x1840, // adds r0, r0, r1
		0x1880, // adds r0, r0, r2
		0x4770, // bx lr
	} {
		binary.LittleEndian.PutUint16(data[0x10+index*2:], instruction)
	}
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: data}, armcore.CoreOptions{MaxSteps: 100})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		id   uint32
		regs [4]uint32
		want uint32
	}{
		{name: "jump1", id: javaSVCJump1, regs: [4]uint32{37, ImageBase + 0x11}, want: 42},
		{name: "jump2", id: javaSVCJump2, regs: [4]uint32{20, 22, ImageBase + 0x15}, want: 42},
		{name: "jump3", id: javaSVCJump3, regs: [4]uint32{10, 12, 20, ImageBase + 0x19}, want: 42},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			callContext := armcore.NewContext()
			callContext.Registers[0] = test.regs[0]
			callContext.Registers[1] = test.regs[1]
			callContext.Registers[2] = test.regs[2]
			callContext.Registers[3] = test.regs[3]
			callContext.Registers[armcore.RegisterSP] = ThreadStackBase + uint32(ThreadStackSize)
			result, err := runtime.handleJavaCall(context.Background(), armcore.NewThread(callContext), test.id)
			if err != nil || result != test.want {
				t.Fatalf("handleJavaCall() = %#x/%v, want %#x", result, err, test.want)
			}
		})
	}
}

func TestInitializationCallNativeRunsGuestBodyAndWritesResult(t *testing.T) {
	data := syntheticInitializableClient()
	for index, instruction := range []uint16{
		0x6802, // ldr r2, [r0]
		0x684b, // ldr r3, [r1, #4]
		0x18d0, // adds r0, r2, r3
		0x4770, // bx lr
	} {
		binary.LittleEndian.PutUint16(data[0x10+index*2:], instruction)
	}
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: data}, armcore.CoreOptions{MaxSteps: 100})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	container, err := runtime.allocateWords([]uint32{20, 22})
	if err != nil {
		t.Fatal(err)
	}
	callContext := armcore.NewContext()
	callContext.Registers[0] = ImageBase + 0x11
	callContext.Registers[1] = container
	callContext.Registers[armcore.RegisterSP] = ThreadStackBase + uint32(ThreadStackSize)
	thread := armcore.NewThread(callContext)

	result, err := runtime.handleJavaCall(context.Background(), thread, javaSVCCallNative)
	if err != nil || result != container {
		t.Fatalf("CallNative() = %#x/%v, want %#x", result, err, container)
	}
	words := readTestBytes(t, client, container, 8)
	if value, exception := binary.LittleEndian.Uint32(words[:4]), binary.LittleEndian.Uint32(words[4:]); value != 42 || exception != 0 {
		t.Fatalf("CallNative result = {%d, %#x}, want {42, 0}", value, exception)
	}
	if got := thread.Context(); got != callContext {
		t.Fatalf("CallNative changed parent context: got %+v, want %+v", got, callContext)
	}
}

func TestInitializationCallNativeRejectsInvalidPointers(t *testing.T) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{MaxSteps: 100})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	container, err := runtime.allocateWords([]uint32{20, 22})
	if err != nil {
		t.Fatal(err)
	}
	const readOnlyData = uint32(0x40001000)
	if err := client.Core().Memory().Map(readOnlyData, 8, armcore.PermissionRead); err != nil {
		t.Fatal(err)
	}
	if err := client.Core().Memory().Load(readOnlyData, make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		target  uint32
		data    uint32
		wantErr string
	}{
		{name: "null target", target: 0, data: container, wantErr: "target is null"},
		{name: "unaligned data", target: ImageBase + 0x11, data: container + 1, wantErr: "not word-aligned"},
		{name: "unmapped data", target: ImageBase + 0x11, data: 0x40000000, wantErr: "native call data"},
		{name: "read-only data", target: ImageBase + 0x11, data: readOnlyData, wantErr: "writable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			callContext := armcore.NewContext()
			callContext.Registers[0] = test.target
			callContext.Registers[1] = test.data
			callContext.Registers[armcore.RegisterSP] = ThreadStackBase + uint32(ThreadStackSize)
			_, err := runtime.handleJavaCall(context.Background(), armcore.NewThread(callContext), javaSVCCallNative)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("CallNative() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestInitializationAOTCallsRejectExcessiveNesting(t *testing.T) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{MaxSteps: 100})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	container, err := runtime.allocateWords([]uint32{20, 22})
	if err != nil {
		t.Fatal(err)
	}
	runtime.aotCallDepth = map[*armcore.Thread]uint32{runtime.currentThread: maxAOTCallDepth}

	callContext := armcore.NewContext()
	callContext.Registers[0] = ImageBase + 0x11
	callContext.Registers[1] = container
	callContext.Registers[armcore.RegisterSP] = ThreadStackBase + uint32(ThreadStackSize)
	if _, err := runtime.handleJavaCall(context.Background(), armcore.NewThread(callContext), javaSVCCallNative); err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("CallNative() error = %v, want nesting limit", err)
	}

	jumpContext := armcore.NewContext()
	jumpContext.Registers[0] = 1
	jumpContext.Registers[1] = ImageBase + 0x11
	jumpContext.Registers[armcore.RegisterSP] = ThreadStackBase + uint32(ThreadStackSize)
	if _, err := runtime.handleJavaCall(context.Background(), armcore.NewThread(jumpContext), javaSVCJump1); err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("JavaJump1() error = %v, want nesting limit", err)
	}
}

func TestInitializationJavaThrowResumesMatchingAOTHandler(t *testing.T) {
	data := syntheticExecutableClient()
	for index, instruction := range []uint16{
		0x2407, // movs r4, #JavaJump1
		0x46a4, // mov r12, r4
		0xdf02, // svc #JavaInterface
		0x2063, // movs r0, #99 (must be skipped)
		0x4770, // bx lr
	} {
		binary.LittleEndian.PutUint16(data[0x10+index*2:], instruction)
	}
	for index, instruction := range []uint16{
		0x6001, // str r1, [r0]
		0x4608, // mov r0, r1
		0x4770, // bx lr
	} {
		binary.LittleEndian.PutUint16(data[0x1a+index*2:], instruction)
	}
	for index, instruction := range []uint16{
		0x2401, // movs r4, #JavaThrow
		0x46a4, // mov r12, r4
		0xdf01, // svc #Init
		0x2063, // movs r0, #99 (must be skipped)
		0x4770, // bx lr
	} {
		binary.LittleEndian.PutUint16(data[0x20+index*2:], instruction)
	}

	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: data}, armcore.CoreOptions{MaxSteps: 100})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}
	exceptionName, err := runtime.allocateBytes([]byte("java/lang/Error\x00"))
	if err != nil {
		t.Fatal(err)
	}
	matchingClass, err := runtime.ensureJavaClass("java/lang/Error")
	if err != nil {
		t.Fatal(err)
	}
	nonmatchingClass, err := runtime.ensureJavaClass("java/lang/RuntimeException")
	if err != nil {
		t.Fatal(err)
	}
	const (
		currentPC = uint32(0x105)
		fromPC    = uint32(0x100)
		toPC      = uint32(0x110)
		target    = ImageBase + 0x31
		restorePC = ImageBase + 0x1b
	)
	makeHandler := func(oldHandler, catchClass uint32) uint32 {
		t.Helper()
		entry, err := runtime.allocateWords([]uint32{fromPC, toPC, target, catchClass})
		if err != nil {
			t.Fatal(err)
		}
		table, err := runtime.allocateWords([]uint32{entry})
		if err != nil {
			t.Fatal(err)
		}
		methodData := make([]byte, javaMethodSize)
		binary.LittleEndian.PutUint32(methodData[8:], table)
		binary.LittleEndian.PutUint16(methodData[16:], 1)
		method, err := runtime.allocateBytes(methodData)
		if err != nil {
			t.Fatal(err)
		}
		functions, err := runtime.allocateWords([]uint32{0, restorePC})
		if err != nil {
			t.Fatal(err)
		}
		handler := make([]uint32, javaExceptionHandler/4)
		handler[0] = method
		handler[2] = oldHandler
		handler[3] = currentPC
		handler[5] = functions
		address, err := runtime.allocateWords(handler)
		if err != nil {
			t.Fatal(err)
		}
		return address
	}
	outerHandler := makeHandler(0, matchingClass)
	innerHandler := makeHandler(outerHandler, nonmatchingClass)
	// Materialize the thrown class's dispatch alias first so the next data
	// allocation is the exception object itself.
	errorMetadata, ok := client.JVM().AOTClassAt(matchingClass)
	if !ok {
		t.Fatal("java/lang/Error metadata is not registered")
	}
	if _, err := runtime.aotVTableHeader(errorMetadata); err != nil {
		t.Fatal(err)
	}
	// Nothing has been released yet, so the next allocation comes off the
	// arena's bump cursor.
	exceptionAddress := uint32(runtime.arena.cursor)

	parentContext := armcore.NewContext()
	parentContext.Registers[armcore.RegisterSP] = ThreadStackBase + uint32(ThreadStackSize)
	parent := armcore.NewThread(parentContext)
	if err := client.Core().SetThreadLocalWord(parent, runtime.exceptionContext+javaExceptionHead, innerHandler); err != nil {
		t.Fatal(err)
	}
	summary, err := client.Core().Call(
		context.Background(),
		parent,
		ImageBase+0x11,
		ReturnAddress,
		[]uint32{exceptionName, ImageBase + 0x21},
		runtime.handleSupervisorCall,
	)
	if err != nil {
		t.Fatalf("AOT exception call error = %v", err)
	}
	if summary.Context.Registers[0] != target {
		t.Fatalf("AOT exception result = %#x, want target %#x", summary.Context.Registers[0], target)
	}
	// Entering the catch block leaves the region that was protected, and the
	// record's label has to say so: a catch block that throws on its own and
	// finds the region it just left matches the same entry again and jumps
	// back to its own top for ever.
	label := readTestBytes(t, client, outerHandler+javaExceptionCurrentPC, 4)
	if resumed := binary.LittleEndian.Uint32(label); resumed != target {
		t.Fatalf("handler label = %#x, want the target it resumed at %#x", resumed, target)
	}
	if target >= fromPC && target < toPC {
		t.Fatal("the target is inside the range it leaves, so the label proves nothing")
	}
	context := readTestBytes(t, client, outerHandler+javaExceptionContext, 4)
	if restoredTarget := binary.LittleEndian.Uint32(context); restoredTarget != target {
		t.Fatalf("restored handler target = %#x, want %#x", restoredTarget, target)
	}
	current, err := client.Core().ThreadLocalWord(parent, runtime.exceptionContext+javaExceptionHead)
	if err != nil {
		t.Fatal(err)
	}
	if current != outerHandler {
		t.Fatalf("exception handler head = %#x, want matched handler %#x", current, outerHandler)
	}
	exception, ok := client.JVM().AOTObject(exceptionAddress)
	if !ok || exception.ClassName != "java/lang/Error" {
		t.Fatalf("pinned caught exception at %#x = %+v/%t", exceptionAddress, exception, ok)
	}
	// The catch block reads what it caught out of the handler record, and the
	// record is on the guest stack: a word left unwritten is whatever the
	// frame underneath put there. One title re-threw such a word and the
	// platform could only say it was not an object.
	caught := readTestBytes(t, client, outerHandler+javaExceptionObject, 4)
	if handed := binary.LittleEndian.Uint32(caught); handed != exceptionAddress {
		t.Fatalf("the matched handler was handed %#x, want the exception %#x", handed, exceptionAddress)
	}
	if got := parent.Context(); got != parentContext {
		t.Fatalf("AOT exception changed parent context: got %+v, want %+v", got, parentContext)
	}
}

func TestInitializationJavaThrowRejectsCyclicHandlerChain(t *testing.T) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}
	exceptionName, err := runtime.allocateBytes([]byte("java/lang/Error\x00"))
	if err != nil {
		t.Fatal(err)
	}
	method, err := runtime.allocateBytes(make([]byte, javaMethodSize))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := runtime.allocate(javaExceptionHandler)
	if err != nil {
		t.Fatal(err)
	}
	handlerWords := make([]uint32, javaExceptionHandler/4)
	handlerWords[0] = method
	handlerWords[2] = handler
	writeTestWords(t, client, handler, handlerWords)

	throwContext := armcore.NewContext()
	throwContext.Registers[0] = exceptionName
	thread := armcore.NewThread(throwContext)
	if err := client.Core().SetThreadLocalWord(thread, runtime.exceptionContext+javaExceptionHead, handler); err != nil {
		t.Fatal(err)
	}
	_, err = runtime.handleInitCall(thread, initSVCJavaThrow)
	if err == nil || !strings.Contains(err.Error(), "cycles") {
		t.Fatalf("JavaThrow() error = %v, want cyclic handler failure", err)
	}
}

func TestInitializationJavaThrowPinsAndPropagatesUncaughtException(t *testing.T) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}
	exceptionName, err := runtime.allocateBytes([]byte("java/lang/RuntimeException\x00"))
	if err != nil {
		t.Fatal(err)
	}
	throwContext := armcore.NewContext()
	throwContext.Registers[0] = exceptionName
	_, err = runtime.handleInitCall(armcore.NewThread(throwContext), initSVCJavaThrow)
	if err == nil {
		t.Fatal("JavaThrow() succeeded without an exception handler")
	}
	var uncaught *UncaughtAOTException
	if !errors.As(err, &uncaught) {
		t.Fatalf("JavaThrow() error = %v, want UncaughtAOTException", err)
	}
	if uncaught.Address == 0 || uncaught.Exception == nil || uncaught.Exception.Object == nil {
		t.Fatalf("uncaught exception = %+v", uncaught)
	}
	if uncaught.Exception.Object.ClassName != "java/lang/RuntimeException" {
		t.Fatalf("uncaught class = %q", uncaught.Exception.Object.ClassName)
	}
	pinned, ok := client.JVM().AOTObject(uncaught.Address)
	if !ok || pinned != uncaught.Exception.Object {
		t.Fatalf("pinned uncaught exception at %#x = %+v/%t", uncaught.Address, pinned, ok)
	}
	if !client.JVM().IsGuestException(err, "java/lang/Throwable") {
		t.Fatalf("uncaught error %v is not visible as a JVM Throwable", err)
	}
}

func TestReadExecutableRejectsInvalidGuestPointers(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]byte)
		want   string
	}{
		{
			name: "interface outside image",
			mutate: func(data []byte) {
				binary.LittleEndian.PutUint32(data[0x40:], 0xfffffff0)
			},
			want: "ExeInterface range",
		},
		{
			name: "unterminated name",
			mutate: func(data []byte) {
				for index := 0xc0; index < len(data); index++ {
					data[index] = 'A'
				}
			},
			want: "not null-terminated",
		},
		{
			name: "unexpected interface name",
			mutate: func(data []byte) {
				copy(data[0xd0:], "OtherInterface\x00")
			},
			want: "ExeInterface name",
		},
		{
			name: "null init function",
			mutate: func(data []byte) {
				binary.LittleEndian.PutUint32(data[0xa0+2*4:], 0)
			},
			want: "ExeInterface init pointer is null",
		},
		{
			name: "function outside image",
			mutate: func(data []byte) {
				binary.LittleEndian.PutUint32(data[0xa0+4*4:], 0xfffffff1)
			},
			want: "get-class pointer",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := syntheticExecutableClient()
			test.mutate(data)
			client, err := LoadClient(ClientImage{Name: "client.bin0", Data: data}, armcore.CoreOptions{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ReadExecutable(ImageBase + 0x40)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ReadExecutable() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReadExecutableRejectsUnalignedDescriptor(t *testing.T) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticExecutableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ReadExecutable(ImageBase + 0x41)
	if err == nil || !strings.Contains(err.Error(), "not word-aligned") {
		t.Fatalf("ReadExecutable() error = %v, want alignment failure", err)
	}
}

func syntheticExecutableClient() []byte {
	data := make([]byte, 0xe0)
	// push {lr}; ldr r0,[pc,#4]; pop {pc}; nop; descriptor address.
	binary.LittleEndian.PutUint16(data[0x00:], 0xb500)
	binary.LittleEndian.PutUint16(data[0x02:], 0x4801)
	binary.LittleEndian.PutUint16(data[0x04:], 0xbd00)
	binary.LittleEndian.PutUint16(data[0x06:], 0x46c0)
	binary.LittleEndian.PutUint32(data[0x08:], ImageBase+0x40)

	putExecutableWords(data[0x40:], []uint32{
		ImageBase + 0x80,
		ImageBase + 0xc0,
		0, 0, 0,
		ImageBase + 0x29,
		0, 0, 0, 0,
	})
	putExecutableWords(data[0x80:], []uint32{
		ImageBase + 0xa0,
		ImageBase + 0xd0,
		0, 0, 0, 0, 0, 0,
	})
	putExecutableWords(data[0xa0:], []uint32{
		0, 0,
		ImageBase + 0x21,
		0,
		ImageBase + 0x25,
		0, 0,
	})
	copy(data[0xc0:], "WIPI_exe\x00")
	copy(data[0xd0:], "ExeInterface\x00")
	return data
}

func syntheticInitializableClient() []byte {
	data := syntheticExecutableClient()
	// Interface init loads InitParam4 from the fifth stack argument, calls its
	// fn_alloc pointer through the generated ARMv4T SVC stub, and returns zero
	// only when it receives a non-null guest pointer.
	for index, instruction := range []uint16{
		0xb510,
		0x9c02,
		0x6ae4,
		0x2010,
		0x467b,
		0x3305,
		0x469e,
		0x4720,
		0x2800,
		0xd001,
		0x2000,
		0xbd10,
		0x2001,
		0xbd10,
	} {
		binary.LittleEndian.PutUint16(data[0x20+index*2:], instruction)
	}
	// WIPI init and the unused get-class function both return zero.
	binary.LittleEndian.PutUint16(data[0x3c:], 0x2000)
	binary.LittleEndian.PutUint16(data[0x3e:], 0x4770)
	binary.LittleEndian.PutUint32(data[0x40+5*4:], ImageBase+0x3d)
	binary.LittleEndian.PutUint32(data[0xa0+2*4:], ImageBase+0x21)
	binary.LittleEndian.PutUint32(data[0xa0+4*4:], ImageBase+0x3d)
	return data
}

func putExecutableWords(destination []byte, words []uint32) {
	for index, word := range words {
		binary.LittleEndian.PutUint32(destination[index*4:], word)
	}
}

func writeTestWords(t *testing.T, client *Client, address uint32, words []uint32) {
	t.Helper()
	data := make([]byte, len(words)*4)
	putExecutableWords(data, words)
	if err := client.Core().Memory().Write(address, data); err != nil {
		t.Fatal(err)
	}
}

func readTestBytes(t *testing.T, client *Client, address uint32, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	if err := client.Core().Memory().Read(address, data); err != nil {
		t.Fatal(err)
	}
	return data
}

// A long jump can only be resumed where the frame the catch block runs on
// still belongs to the Host call being resumed. The guest stack is shared by
// every nested call, so a handler saved above this call's entry belongs to a
// caller the Host has not returned to yet: resuming it here would run that
// caller's code inside this call, and the run would end — leaving the Go
// frames that made it waiting for a guest stack that no longer exists. One
// title reached that state on its first painted card and executed at 0x1a.
func TestInitializationJavaThrowLeavesACallWhoseHandlerIsOutsideIt(t *testing.T) {
	data := syntheticExecutableClient()
	for index, instruction := range []uint16{
		0x2401, // movs r4, #JavaThrow
		0x46a4, // mov r12, r4
		0xdf01, // svc #Init
		0x4770, // bx lr
	} {
		binary.LittleEndian.PutUint16(data[0x20+index*2:], instruction)
	}

	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: data}, armcore.CoreOptions{MaxSteps: 100})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}
	exceptionName, err := runtime.allocateBytes([]byte("java/lang/Error\x00"))
	if err != nil {
		t.Fatal(err)
	}
	matchingClass, err := runtime.ensureJavaClass("java/lang/Error")
	if err != nil {
		t.Fatal(err)
	}
	const (
		currentPC = uint32(0x105)
		fromPC    = uint32(0x100)
		toPC      = uint32(0x110)
		target    = ImageBase + 0x31
		restorePC = ImageBase + 0x1b
	)
	entry, err := runtime.allocateWords([]uint32{fromPC, toPC, target, matchingClass})
	if err != nil {
		t.Fatal(err)
	}
	table, err := runtime.allocateWords([]uint32{entry})
	if err != nil {
		t.Fatal(err)
	}
	methodData := make([]byte, javaMethodSize)
	binary.LittleEndian.PutUint32(methodData[8:], table)
	binary.LittleEndian.PutUint16(methodData[16:], 1)
	method, err := runtime.allocateBytes(methodData)
	if err != nil {
		t.Fatal(err)
	}
	functions, err := runtime.allocateWords([]uint32{0, restorePC})
	if err != nil {
		t.Fatal(err)
	}
	// The call below starts one word below the top of the stack, and the
	// handler saved the frame above it: the catch block belongs to the caller.
	callEntry := ThreadStackBase + uint32(ThreadStackSize) - 4
	handlerWords := make([]uint32, javaExceptionHandler/4)
	handlerWords[0] = method
	handlerWords[3] = currentPC
	handlerWords[5] = functions
	handlerWords[javaExceptionFrameStack/4] = callEntry + 4
	handler, err := runtime.allocateWords(handlerWords)
	if err != nil {
		t.Fatal(err)
	}

	parentContext := armcore.NewContext()
	parentContext.Registers[armcore.RegisterSP] = callEntry
	parent := armcore.NewThread(parentContext)
	if err := client.Core().SetThreadLocalWord(parent, runtime.exceptionContext+javaExceptionHead, handler); err != nil {
		t.Fatal(err)
	}
	_, err = client.Core().Call(
		context.Background(),
		parent,
		ImageBase+0x21,
		ReturnAddress,
		[]uint32{exceptionName},
		runtime.handleSupervisorCall,
	)
	var unwind *aotExceptionUnwind
	if !errors.As(err, &unwind) {
		t.Fatalf("call error = %v, want the unwind to leave the call", err)
	}
	if unwind.target != target || unwind.framePointer != callEntry+4 {
		t.Fatalf("unwind = %+v, want target %#x on frame %#x", unwind, target, callEntry+4)
	}
	// The call one out entered at or above that frame, and there the same
	// unwind is the one to resume.
	if unwind.resumableFrom(callEntry) {
		t.Fatal("a call entered below the frame claimed it")
	}
	if !unwind.resumableFrom(callEntry + 4) {
		t.Fatal("the call the frame belongs to did not claim it")
	}
}

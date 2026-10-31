package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// TestNativeFormCallReleasesItsArgumentContainer covers the Host's call into
// a native-form method body. Every runtime class method is native-form, so the
// container its arguments travel in was allocated on every such call and
// never given back: the arena only grew.
func TestNativeFormCallReleasesItsArgumentContainer(t *testing.T) {
	client, runtime := newTestRuntime(t)
	body := returningStub(t, runtime)
	method := jvm.AOTMethodMetadata{Name: "call", Descriptor: "(II)V", AccessFlags: 0x109, NativeBody: body}
	typeInfo, err := jvm.ParseMethodDescriptor(method.Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	call := func() {
		if _, _, err := runtime.runAOTMethod(t.Context(), client.Thread, "", method, typeInfo, []uint32{0x11, 0x22}); err != nil {
			t.Fatal(err)
		}
	}
	call()
	before := runtime.Arena.Used()
	for range 64 {
		call()
	}
	if after := runtime.Arena.Used(); after != before {
		t.Fatalf("64 native-form calls grew the arena from %d to %d bytes", before, after)
	}
}

// TestModuleNativeCallReleasesItsArgumentContainer is the same rule for an
// older module's invoke helper reaching a native body through its jump slot,
// which is how every platform call such a module makes arrives.
func TestModuleNativeCallReleasesItsArgumentContainer(t *testing.T) {
	client, runtime := newTestRuntime(t)
	body := returningStub(t, runtime)
	name, err := runtime.AllocateBytes([]byte("\x00(I)V+call\x00"))
	if err != nil {
		t.Fatal(err)
	}
	record := make([]byte, javaMethodSize)
	binary.LittleEndian.PutUint32(record[8:], body)
	binary.LittleEndian.PutUint32(record[12:], name)
	binary.LittleEndian.PutUint16(record[22:], 0x109)
	methodAddress, err := runtime.AllocateBytes(record)
	if err != nil {
		t.Fatal(err)
	}
	stackTop := wipicrt.ThreadStackBase + uint32(wipicrt.ThreadStackSize) - 64
	call := func() {
		thread := armcore.NewThread(armcore.NewContext())
		for register, value := range map[int]uint32{0: methodAddress, 1: 0x33, armcore.RegisterSP: stackTop} {
			if err := thread.SetRegister(register, value); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := runtime.handleModuleJumpCall(t.Context(), thread, moduleJumpInvokeNative); err != nil {
			t.Fatal(err)
		}
	}
	call()
	before := runtime.Arena.Used()
	for range 64 {
		call()
	}
	if after := runtime.Arena.Used(); after != before {
		t.Fatalf("64 module native calls grew the arena from %d to %d bytes", before, after)
	}
	_ = client
}

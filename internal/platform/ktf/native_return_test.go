package ktf

import (
	"encoding/binary"
	"github.com/movingwoo/wfeature/internal/armcore"
	"testing"
)

func TestNativeEnvironmentWordReturnOverridesScratchRegister(t *testing.T) {
	for _, test := range []struct {
		name             string
		tag, value, want uint32
	}{
		{"word", 2, 75, 75}, {"zero", 2, 0, 0}, {"signed", 2, 0xfffffff0, 0xfffffff0}, {"unknown tag", 3, 75, 99},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			address := uint32((runtime.codeCursor + 3) &^ 3)
			body := make([]byte, 28)
			// ldr r2,env; ldr r3,value; str r3,[r2,#40]; movs r3,tag;
			// str r3,[r2,#36]; movs r0,99; bx lr; padding; env; value.
			for i, v := range []uint16{0x4a03, 0x4b04, 0x6293, uint16(0x2300 | test.tag), 0x6253, 0x2063, 0x4770, 0x46c0} {
				binary.LittleEndian.PutUint16(body[i*2:], v)
			}
			binary.LittleEndian.PutUint32(body[16:], runtime.exceptionContext)
			binary.LittleEndian.PutUint32(body[20:], test.value)
			if err := client.core.Memory().Load(address, body); err != nil {
				t.Fatal(err)
			}
			container, err := runtime.allocateWords([]uint32{123, 456})
			if err != nil {
				t.Fatal(err)
			}
			call := armcore.NewContext()
			call.Registers[0] = address | 1
			call.Registers[1] = container
			call.Registers[13] = ThreadStackBase + uint32(ThreadStackSize)
			thread := armcore.NewThread(call)
			if _, err := runtime.callAOTNative(t.Context(), thread); err != nil {
				t.Fatal(err)
			}
			result := readTestBytes(t, client, container, 8)
			if got := binary.LittleEndian.Uint32(result); got != test.want {
				t.Fatalf("result=%#x want=%#x", got, test.want)
			}
			if high := binary.LittleEndian.Uint32(result[4:]); high != 0 {
				t.Fatalf("high word=%#x", high)
			}
			// A later register-returning native must not inherit the previous tag.
			binary.LittleEndian.PutUint16(body[0:], 0x202a)
			binary.LittleEndian.PutUint16(body[2:], 0x4770)
			if err := client.core.Memory().Load(address, body); err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.callAOTNative(t.Context(), thread); err != nil {
				t.Fatal(err)
			}
			if got := binary.LittleEndian.Uint32(readTestBytes(t, client, container, 4)); got != 42 {
				t.Fatalf("stale native result=%d", got)
			}
		})
	}
}

func TestNativeReturnScopesPreserveNestedAndOtherThreadValues(t *testing.T) {
	client, runtime := newTestRuntime(t)
	first, second := armcore.NewThread(armcore.NewContext()), armcore.NewThread(armcore.NewContext())
	write := func(thread *armcore.Thread, tag, value uint32) {
		t.Helper()
		for offset, v := range map[uint32]uint32{nativeReturnTagOffset: tag, nativeReturnValueOffset: value} {
			if err := client.core.SetThreadLocalWord(thread, runtime.exceptionContext+offset, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	outer, err := runtime.beginNativeReturn(first)
	if err != nil {
		t.Fatal(err)
	}
	defer outer.restore()
	write(first, 2, 75)
	nested, err := runtime.beginNativeReturn(first)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := nested.result(99); err != nil || got != 99 {
		t.Fatalf("nested inherited result=%d error=%v", got, err)
	}
	write(first, 2, 17)
	sibling, err := runtime.beginNativeReturn(second)
	if err != nil {
		t.Fatal(err)
	}
	write(second, 2, 23)
	if got, err := nested.result(99); err != nil || got != 17 {
		t.Fatalf("sibling changed nested result=%d error=%v", got, err)
	}
	sibling.restore()
	nested.restore()
	if got, err := outer.result(99); err != nil || got != 75 {
		t.Fatalf("nested changed outer result=%d error=%v", got, err)
	}
}

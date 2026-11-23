package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// A routine handed a null pointer reads a count through it, stores a code
// through it and reads that back. Without the page the first read stops the
// guest; with it the count is zero and the store holds.
func TestNullPageMemory(t *testing.T) {
	routine := []byte{
		0x83, 0x89, // ldrh r3,[r0,#0xc]
		0x01, 0x61, // str r1,[r0,#0x10]
		0x02, 0x69, // ldr r2,[r0,#0x10]
		0xc0, 0x46, // nop
	}
	run := func(mapped bool) (armcore.Context, error) {
		t.Helper()
		memory := armcore.NewMemory()
		if err := memory.Map(0x200000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
		if err := memory.Load(0x200000, routine); err != nil {
			t.Fatal(err)
		}
		if mapped {
			if err := mapNullPage(memory); err != nil {
				t.Fatal(err)
			}
		}
		state := armcore.NewContext()
		state.Registers[1] = 0x100
		state.Registers[3] = 0xffff
		if err := state.SetPC(0x200001); err != nil {
			t.Fatal(err)
		}
		_, err := (armcore.Engine{}).Run(&state, memory, 0x200006, 16)
		return state, err
	}
	if _, err := run(false); err == nil {
		t.Fatal("a read through a null pointer was answered without the page")
	}
	state, err := run(true)
	if err != nil {
		t.Fatal(err)
	}
	if state.Registers[3] != 0 || state.Registers[2] != 0x100 {
		t.Fatalf("count = %#x, stored code = %#x; want 0 and 0x100", state.Registers[3], state.Registers[2])
	}
	if compatibility.IdentifyNative("ktf", []byte("unrecognized client")).Has(compatibility.KTFNullPageMemory) {
		t.Fatal("unknown revision matched")
	}
}

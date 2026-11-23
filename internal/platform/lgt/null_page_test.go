package lgt

import (
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// A read through a null pointer answers zero on the page, and a store through
// one still stops the guest: the page is read only.
func TestNullPageAnswersReadsAndStopsStores(t *testing.T) {
	run := func(mapped bool, code []byte) (armcore.Context, error) {
		t.Helper()
		memory := armcore.NewMemory()
		if err := memory.Map(0x200000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
		if err := memory.Load(0x200000, code); err != nil {
			t.Fatal(err)
		}
		if mapped {
			if err := mapNullPage(memory); err != nil {
				t.Fatal(err)
			}
		}
		state := armcore.NewContext()
		state.Registers[1] = 0xffff
		state.Registers[2] = 0xff
		if err := state.SetPC(0x200001); err != nil {
			t.Fatal(err)
		}
		_, err := (armcore.Engine{}).Run(&state, memory, 0x200000+uint32(len(code)), 16)
		return state, err
	}
	read := []byte{
		0x99, 0x5e, // ldrsh r1,[r3,r2] with r3 = 0 and r2 = 0
		0x98, 0x78, // ldrb r0,[r3,#2]
	}
	if _, err := run(false, append([]byte{0x00, 0x23, 0x00, 0x22}, read...)); err == nil {
		t.Fatal("a read through a null pointer was answered without the page")
	}
	state, err := run(true, append([]byte{0x00, 0x23, 0x00, 0x22}, read...))
	if err != nil {
		t.Fatal(err)
	}
	if state.Registers[1] != 0 || state.Registers[0] != 0 {
		t.Fatalf("read %#x and %#x through null, want zeros", state.Registers[1], state.Registers[0])
	}
	store := []byte{
		0x00, 0x23, // movs r3,#0
		0x19, 0x60, // str r1,[r3]
	}
	if _, err := run(true, store); err == nil {
		t.Fatal("a store through a null pointer was accepted")
	}
	if compatibility.IdentifyNative("lgt", []byte("unrecognized module")).Has(compatibility.LGTNullPageMemory) {
		t.Fatal("an unknown revision was selected")
	}
}

// A record taken before the page was mapped gains it, once, at the front.
func TestNullPageJoinsAnEarlierRecord(t *testing.T) {
	module := armcore.MappingState{Address: 0x1000, Size: 0x9000, Permission: armcore.PermissionReadWriteExecute}
	earlier := []armcore.MappingState{module}
	joined := withNullPage(earlier)
	if !slices.Equal(joined, []armcore.MappingState{nullPageMapping, module}) {
		t.Fatalf("joined = %v", joined)
	}
	if again := withNullPage(joined); !slices.Equal(again, joined) {
		t.Fatalf("a record with the page gained another: %v", again)
	}
	if len(earlier) != 1 {
		t.Fatal("the earlier record was changed in place")
	}
}

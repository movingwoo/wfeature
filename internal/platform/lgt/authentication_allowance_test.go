package lgt

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

const allowanceModule = "f159d0d09bb8ae3347e427dea4d0be452d913b64e7d0a6949a955af0a539601b"

// allowanceFixture loads the recognized dismissal where the fix expects it.
// The routine it completes with is the test's own and marks r7; the record
// r5 points at holds the failure count at 0x28.
func allowanceFixture(t *testing.T) *armcore.Memory {
	t.Helper()
	memory := armcore.NewMemory()
	for _, page := range []uint32{0x19000, 0x200000} {
		if err := memory.Map(page, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
	}
	site := allowanceSites[allowanceModule]
	if err := memory.Load(site.at, site.expect); err != nil {
		t.Fatal(err)
	}
	if err := memory.Load(0x19bec, []byte{0x01, 0x27, 0x70, 0x47}); err != nil { // movs r7,#1; bx lr
		t.Fatal(err)
	}
	return memory
}

// dismissals presses OK on the failure notice until the scene completes and
// answers how many presses that took.
func dismissals(t *testing.T, memory *armcore.Memory) int {
	t.Helper()
	for press := 1; press <= 8; press++ {
		state := armcore.NewContext()
		state.Registers[0] = 0xfffffffb // OK
		state.Registers[5] = 0x200000
		state.Registers[13] = 0x200800
		if err := state.SetPC(0x19cb5); err != nil {
			t.Fatal(err)
		}
		if _, err := (armcore.Engine{}).Run(&state, memory, 0x19cd4, 32); err != nil {
			t.Fatal(err)
		}
		if state.Registers[7] == 1 {
			return press
		}
	}
	return 0
}

func TestAuthenticationAllowanceLetsTheFirstFailureThrough(t *testing.T) {
	if got := dismissals(t, allowanceFixture(t)); got != 5 {
		t.Fatalf("as packaged the scene completed after %d failures, want 5", got)
	}
	memory := allowanceFixture(t)
	if err := applyAuthenticationAllowance(memory, allowanceModule); err != nil {
		t.Fatal(err)
	}
	if got := dismissals(t, memory); got != 1 {
		t.Fatalf("with the fix the scene completed after %d failures, want 1", got)
	}
	var count [4]byte
	if err := memory.Read(0x200028, count[:]); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(count[:]) != 1 {
		t.Fatalf("failure count = %d", binary.LittleEndian.Uint32(count[:]))
	}
}

func TestAuthenticationAllowanceRejectsUnexpectedCode(t *testing.T) {
	if err := applyAuthenticationAllowance(armcore.NewMemory(), allowanceModule); err == nil {
		t.Fatal("unmapped code accepted")
	}
	memory := allowanceFixture(t)
	site := allowanceSites[allowanceModule]
	if err := memory.Write(site.at+uint32(site.offset), []byte{0x05, 0x2b}); err != nil {
		t.Fatal(err)
	}
	if err := applyAuthenticationAllowance(memory, allowanceModule); err == nil {
		t.Fatal("a changed allowance was accepted")
	}
	got := make([]byte, 2)
	if err := memory.Read(site.at+uint32(site.offset), got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0x05, 0x2b}) {
		t.Fatal("a refused module was written")
	}
	if err := applyAuthenticationAllowance(allowanceFixture(t), "unknown"); err == nil {
		t.Fatal("a module without a recorded site was accepted")
	}
	if compatibility.IdentifyNative("lgt", []byte("unrecognized module")).Has(compatibility.LGTAuthenticationAllowance) {
		t.Fatal("an unknown revision was selected")
	}
}

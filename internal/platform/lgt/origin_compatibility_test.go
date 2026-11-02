package lgt

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Authored Thumb initialization: advance to the origin field, read it,
// add the handset strip height and write it back. No archive is embedded.
func TestFramebufferOriginCorrection(t *testing.T) {
	for _, corrected := range []bool{false, true} {
		memory := armcore.NewMemory()
		if err := memory.Map(0x1e000, 0x2000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
		code := []byte{0x90, 0x32, 0x13, 0x68, 0x18, 0x33, 0x13, 0x60}
		if err := memory.Load(framebufferOriginAddress, code); err != nil {
			t.Fatal(err)
		}
		if corrected {
			if err := correctFramebufferOrigin(memory); err != nil {
				t.Fatal(err)
			}
		}
		state := armcore.NewContext()
		state.Registers[2] = 0x1f000
		if err := state.SetPC(framebufferOriginAddress | 1); err != nil {
			t.Fatal(err)
		}
		if _, err := (armcore.Engine{}).Run(&state, memory, 0xffffffff, 4); err != nil {
			t.Fatal(err)
		}
		var result [4]byte
		if err := memory.Read(0x1f090, result[:]); err != nil {
			t.Fatal(err)
		}
		want := byte(24)
		if corrected {
			want = 0
		}
		if result != [4]byte{want} {
			t.Fatalf("corrected=%v origin=%v, want %d", corrected, result, want)
		}
		var loaded [8]byte
		if err := memory.Read(framebufferOriginAddress, loaded[:]); err != nil {
			t.Fatal(err)
		}
		if corrected {
			code[4] = 0
		}
		if !bytes.Equal(loaded[:], code) {
			t.Fatal("surrounding initialization changed")
		}
	}
}

func TestFramebufferOriginCorrectionRejectsUnexpectedCode(t *testing.T) {
	memory := armcore.NewMemory()
	if err := memory.Map(0x1e000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	if err := correctFramebufferOrigin(memory); err == nil {
		t.Fatal("unexpected initialization accepted")
	}
}

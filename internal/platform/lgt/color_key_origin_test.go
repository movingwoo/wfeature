package lgt

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func TestColorKeyCalibrationSamplesDrawnPixel(t *testing.T) {
	for _, corrected := range []bool{false, true} {
		memory := armcore.NewMemory()
		if err := memory.Map(0x1000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
		// Authored Thumb argument setup for PutPixel(dst, 0, 0, context).
		code := []byte{0x30, 0x1c, 0, 0x21, 0, 0x22}
		if err := memory.Load(0x1742, code); err != nil {
			t.Fatal(err)
		}
		if corrected {
			if err := correctColorKeySampleOrigin(memory); err != nil {
				t.Fatal(err)
			}
		}
		client := fixtureClient(t)
		target, err := client.newFramebuffer(4, 32, false)
		if err != nil {
			t.Fatal(err)
		}
		state := armcore.NewContext()
		state.Registers[6] = target.handle
		if err := state.SetPC(0x1743); err != nil {
			t.Fatal(err)
		}
		if _, err := (armcore.Engine{}).Run(&state, memory, 0xffffffff, 3); err != nil {
			t.Fatal(err)
		}
		pointer := writeGuest(t, client, make([]byte, grpContextSize))
		if code := client.initContext(pointer); code != wipiSuccess {
			t.Fatalf("initContext = %d", code)
		}
		if err := client.writeWord(pointer+grpContextClip+4, 32<<16|4); err != nil {
			t.Fatal(err)
		}
		callSlot(t, client, slotSetContext, pointer, grpFieldForeground, uint32(maskPixel))
		callDrawSlot(t, client, slotPutPixel, state.Registers[0], state.Registers[1], state.Registers[2], pointer)
		sample := make([]uint16, 1)
		if err := client.core.Memory().ReadHalfwords(target.address+24*4*2, sample); err != nil {
			t.Fatal(err)
		}
		want := uint16(0)
		if corrected {
			want = maskPixel
		}
		if sample[0] != want {
			t.Fatalf("corrected=%v sampled key=%#x want=%#x", corrected, sample[0], want)
		}
		var loaded [6]byte
		if err := memory.Read(0x1742, loaded[:]); err != nil {
			t.Fatal(err)
		}
		if corrected {
			code[4] = 24
		}
		if !bytes.Equal(loaded[:], code) {
			t.Fatal("surrounding calibration code changed")
		}
	}
}

func TestColorKeyCalibrationRejectsUnexpectedCode(t *testing.T) {
	memory := armcore.NewMemory()
	if err := correctColorKeySampleOrigin(memory); err == nil {
		t.Fatal("unmapped code accepted")
	}
	if err := memory.Map(0x1000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	if err := correctColorKeySampleOrigin(memory); err == nil {
		t.Fatal("unexpected code accepted")
	}
}

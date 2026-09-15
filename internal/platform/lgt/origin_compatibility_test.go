package lgt

import (
	"bytes"
	"os"
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

// WFEATURE_LGT_ORIGIN_ARCHIVE names an ignored local archive. Verify matching
// through the real loader, without starting gameplay or touching saved data.
func TestLocalFramebufferOriginArchive(t *testing.T) {
	path := os.Getenv("WFEATURE_LGT_ORIGIN_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_LGT_ORIGIN_ARCHIVE")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(archive.Module)
	if !hasFramebufferOriginCorrection(original) {
		t.Fatal("original module did not select correction")
	}
	check := func(want byte) {
		t.Helper()
		client, err := Load(archive, Options{})
		if err != nil {
			t.Fatal(err)
		}
		var instruction [2]byte
		if err := client.core.Memory().Read(framebufferOriginAddress+4, instruction[:]); err != nil {
			t.Fatal(err)
		}
		if instruction != [2]byte{want, 0x33} {
			t.Fatalf("loaded origin instruction = %x, want %02x33", instruction, want)
		}
	}
	check(0)
	if !bytes.Equal(original, archive.Module) {
		t.Fatal("loader changed original module")
	}
	archive.Descriptor.AID = "other"
	archive.Descriptor.PID = "other"
	archive.Resources["unrelated"] = []byte("other")
	check(0)
	// Even a change outside loadable code must reject this exact-module fix.
	archive.Module = bytes.Clone(original)
	archive.Module[15] ^= 1 // ELF identification padding, ignored by the loader.
	if hasFramebufferOriginCorrection(archive.Module) {
		t.Fatal("module with changed padding selected correction")
	}
	check(24)
	// One changed instruction is a different revision, even with identical
	// descriptor and resources. It must retain its own initialization.
	archive.Module = bytes.Clone(original)
	archive.Module[0x1dfc4] = 25
	if hasFramebufferOriginCorrection(archive.Module) {
		t.Fatal("changed module selected correction")
	}
	check(25)
}

package lgt

import (
	"bytes"
	"os"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func TestDirectFramebufferOrigin(t *testing.T) {
	for _, corrected := range []bool{false, true} {
		memory := armcore.NewMemory()
		if err := memory.Map(0x1000, 0x2000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
		// Authored Thumb code: set the strip field and preserve a screen handle.
		code := []byte{0x18, 0x23, 0x3b, 0x60, 0xe0, 0x63}
		if err := memory.Load(0x12bc, code); err != nil {
			t.Fatal(err)
		}
		if corrected {
			if err := correctDirectFramebufferOrigin(memory); err != nil {
				t.Fatal(err)
			}
		}
		state := armcore.NewContext()
		state.Registers[0], state.Registers[4], state.Registers[7] = 73, 0x2000, 0x2100
		if err := state.SetPC(0x12bd); err != nil {
			t.Fatal(err)
		}
		if _, err := (armcore.Engine{}).Run(&state, memory, 0xffffffff, 3); err != nil {
			t.Fatal(err)
		}
		var strip, handle [4]byte
		if err := memory.Read(0x2100, strip[:]); err != nil {
			t.Fatal(err)
		}
		if err := memory.Read(0x203c, handle[:]); err != nil {
			t.Fatal(err)
		}
		want := byte(24)
		if corrected {
			want = 0
		}
		if strip != [4]byte{want} || handle != [4]byte{73} {
			t.Fatalf("corrected=%v strip=%v handle=%v", corrected, strip, handle)
		}
		var loaded [6]byte
		if err := memory.Read(0x12bc, loaded[:]); err != nil {
			t.Fatal(err)
		}
		code[0] = want
		if !bytes.Equal(code, loaded[:]) {
			t.Fatal("surrounding code changed")
		}
	}
}

func TestDirectFramebufferOriginRejectsUnexpectedCode(t *testing.T) {
	memory := armcore.NewMemory()
	if err := correctDirectFramebufferOrigin(memory); err == nil {
		t.Fatal("unmapped initialization accepted")
	}
	if err := memory.Map(0x1000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	if err := correctDirectFramebufferOrigin(memory); err == nil {
		t.Fatal("unexpected initialization accepted")
	}
}

func TestLocalDirectFramebufferOriginArchive(t *testing.T) {
	testLocalStripOrigin(t, "WFEATURE_LGT_DIRECT_ORIGIN_ARCHIVE", 0x12bc, 0x23)
}

func TestLocalImageFramebufferOriginArchive(t *testing.T) {
	testLocalStripOrigin(t, "WFEATURE_LGT_IMAGE_ORIGIN_ARCHIVE", 0x28a10, 0x22)
}

func testLocalStripOrigin(t *testing.T, variable string, address uint32, opcode byte) {
	t.Helper()
	path := os.Getenv(variable)
	if path == "" {
		t.Skip("set " + variable)
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
	check := func(want byte) {
		t.Helper()
		client, err := Load(archive, Options{})
		if err != nil {
			t.Fatal(err)
		}
		var instruction [2]byte
		if err := client.core.Memory().Read(address, instruction[:]); err != nil {
			t.Fatal(err)
		}
		if instruction != [2]byte{want, opcode} {
			t.Fatalf("origin instruction = %x, want %02x%02x", instruction, want, opcode)
		}
	}
	check(0)
	if !bytes.Equal(original, archive.Module) {
		t.Fatal("archive module changed")
	}
	archive.Descriptor.AID, archive.Descriptor.PID = "other", "other"
	check(0)
	archive.Module = bytes.Clone(original)
	archive.Module[15] ^= 1 // Loader-ignored ELF padding still changes the identity.
	check(24)
}

func TestImageFramebufferOrigin(t *testing.T) {
	for _, corrected := range []bool{false, true} {
		memory := armcore.NewMemory()
		if err := memory.Map(0x28000, 0x2000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
		// Authored Thumb branches store 24 for wide screens or 20 for 176 pixels.
		code := []byte{0x18, 0x22, 0x02, 0xe0, 0x14, 0x22, 0x00, 0xe0, 0xc0, 0x46, 0x02, 0x60}
		if err := memory.Load(0x28a10, code); err != nil {
			t.Fatal(err)
		}
		if corrected {
			if err := correctImageFramebufferOrigin(memory); err != nil {
				t.Fatal(err)
			}
		}
		for _, entry := range []uint32{0x28a11, 0x28a15} {
			state := armcore.NewContext()
			state.Registers[0] = 0x29000
			if err := state.SetPC(entry); err != nil {
				t.Fatal(err)
			}
			if _, err := (armcore.Engine{}).Run(&state, memory, 0xffffffff, 3); err != nil {
				t.Fatal(err)
			}
			var result [4]byte
			if err := memory.Read(0x29000, result[:]); err != nil {
				t.Fatal(err)
			}
			want := byte(24)
			if corrected {
				want = 0
			}
			if entry == 0x28a15 {
				want = 20
			}
			if result != [4]byte{want} {
				t.Fatalf("corrected=%v entry=%#x strip=%v, want %d", corrected, entry, result, want)
			}
		}
		var loaded [12]byte
		if err := memory.Read(0x28a10, loaded[:]); err != nil {
			t.Fatal(err)
		}
		if corrected {
			code[0] = 0
		}
		if !bytes.Equal(code, loaded[:]) {
			t.Fatal("surrounding code changed")
		}
	}
}

func TestImageFramebufferOriginRejectsUnexpectedCode(t *testing.T) {
	memory := armcore.NewMemory()
	if err := correctImageFramebufferOrigin(memory); err == nil {
		t.Fatal("unmapped initialization accepted")
	}
	if err := memory.Map(0x28000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	if err := correctImageFramebufferOrigin(memory); err == nil {
		t.Fatal("unexpected initialization accepted")
	}
}

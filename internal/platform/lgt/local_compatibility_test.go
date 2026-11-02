package lgt

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/testlocal"
)

// The compatibility registry's fixes are tested on authored fixtures beside
// each fix. The tests in this file check an entry against the original archive
// it recognizes, which stays local: each reads an absolute archive path from
// its own environment variable.

func TestLocalCertificateMessageSelection(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_CERTIFICATE_MESSAGE_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	for _, disabled := range []bool{false, true} {
		s, err := StartSession(context.Background(), data, SessionOptions{DisableAuthentication: disabled, SaveRoot: t.TempDir(), Width: 240, Height: 320})
		if err != nil {
			t.Fatal(err)
		}
		status := backend.AuthenticationLGTCertificateMessage
		if disabled {
			status = backend.AuthenticationOff
		}
		if s.Authentication() != status || (s.client.notificationNetwork == nil) != disabled {
			t.Fatal("incorrect session selection")
		}
		if err := s.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(data, original) {
		t.Fatal("original archive changed")
	}
}

func TestLocalSaveSubscriberArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_SAVE_SUBSCRIBER_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	for _, disabled := range []bool{false, true} {
		session, err := StartSession(context.Background(), data, SessionOptions{DisableAuthentication: disabled, Width: 240, Height: 320})
		if err != nil {
			t.Fatal(err)
		}
		var code [2]byte
		if err := session.client.core.Memory().Read(0x69d9a, code[:]); err != nil {
			t.Fatal(err)
		}
		want := byte(0xe0)
		status := backend.AuthenticationLGTSaveSubscriber
		if disabled {
			want = 0xd0
			status = backend.AuthenticationOff
		}
		if code != [2]byte{2, want} || session.Authentication() != status {
			t.Fatalf("disabled=%v code=%x status=%s", disabled, code, session.Authentication())
		}
		if err := session.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(data, original) {
		t.Fatal("original archive changed")
	}
}

func TestLocalColorKeyCalibrationArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_COLOR_KEY_ARCHIVE", "a local archive")
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
		var code [2]byte
		if err := client.core.Memory().Read(0x1746, code[:]); err != nil {
			t.Fatal(err)
		}
		if code != [2]byte{want, 0x22} {
			t.Fatalf("sample coordinate instruction=%x", code)
		}
	}
	check(24)
	if !bytes.Equal(original, archive.Module) {
		t.Fatal("archive module changed")
	}
	archive.Descriptor.AID, archive.Descriptor.PID = "other", "other"
	check(24)
	archive.Module = bytes.Clone(original)
	archive.Module[15] ^= 1
	check(0)
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

// WFEATURE_LGT_ORIGIN_ARCHIVE names an ignored local archive. Verify matching
// through the real loader, without starting gameplay or touching saved data.
func TestLocalFramebufferOriginArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_ORIGIN_ARCHIVE", "a local archive")
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

// TestLocalRecordTableArchive checks the recognized revision itself, from the
// absolute path in WFEATURE_LGT_RECORD_TABLE_ARCHIVE: its module is selected,
// the search for each of the two maps without a record runs past the table as
// packaged, and leaves by the exit for a missing record once corrected.
func TestLocalRecordTableArchive(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_LGT_RECORD_TABLE_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRecordTableTerminator(archive.Module) {
		t.Fatal("the archive's module is not selected")
	}
	packaged, ok := archive.Resource(recordTableName)
	if !ok {
		t.Fatalf("the archive has no %s", recordTableName)
	}
	packaged = bytes.Clone(packaged)
	client, err := Load(archive, Options{Width: 240, Height: 320})
	if err != nil {
		t.Fatal(err)
	}
	corrected, ok := client.titleFile(recordTableName)
	if !ok || !bytes.Equal(corrected, append(bytes.Clone(packaged), recordTableEnd...)) {
		t.Fatalf("the title read %d bytes for a %d-byte table", len(corrected), len(packaged))
	}
	for _, missing := range []byte{124, 126} {
		if got := searchRecord(packaged, 0, missing); got != "past" {
			t.Fatalf("map %d over the packaged table answered %s", missing, got)
		}
		if got := searchRecord(corrected, 0, missing); got != "end" {
			t.Fatalf("map %d over the corrected table answered %s", missing, got)
		}
	}
	if current, _ := archive.Resource(recordTableName); !bytes.Equal(current, packaged) {
		t.Fatal("the archive's own copy changed")
	}
}

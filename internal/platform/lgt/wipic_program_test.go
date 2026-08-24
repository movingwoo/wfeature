package lgt

import (
	"strings"
	"testing"
)

// MC_knlGetProgramName answers the descriptor's own name into the caller's
// buffer. A title reaches it with sixteen bytes of its own stack, which is not
// enough for every name a descriptor carries — the specification answers that
// with M_E_SHORTBUF and says nothing about the buffer, so a short buffer is
// left as the caller prepared it.
func TestGetProgramNameAnswersTheDescriptorName(t *testing.T) {
	client := fixtureClient(t)
	name := client.archive.Descriptor.Fields["Name"]
	if name == "" {
		t.Fatal("the fixture descriptor carries no Name to answer with")
	}

	buffer, err := client.allocate(64)
	if err != nil {
		t.Fatal(err)
	}
	if result := int32(callSlot(t, client, slotGetProgramName, buffer, 64)); result != wipiSuccess {
		t.Fatalf("MC_knlGetProgramName = %d, want %d", result, wipiSuccess)
	}
	answered, err := client.readCString(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if answered != name {
		t.Fatalf("MC_knlGetProgramName answered %q, want %q", answered, name)
	}

	// One byte short of the name and its terminator is a refusal, not a
	// truncation: a caller that reads a truncated name has no way to know.
	short, err := client.allocate(64)
	if err != nil {
		t.Fatal(err)
	}
	size := uint32(len(name))
	if result := int32(callSlot(t, client, slotGetProgramName, short, size)); result != wipiShortBuffer {
		t.Fatalf("a buffer of %d for a %d-byte name = %d, want %d", size, len(name), result, wipiShortBuffer)
	}
	if left, err := client.readCString(short); err != nil || left != "" {
		t.Fatalf("a refused call wrote %q into the buffer", left)
	}
}

// MC_dbListDataBases answers the names of the program's databases, separated
// by one NUL and terminated by two. No C database can exist until
// MC_dbOpenDataBase is served, so the list is empty and the count is zero —
// which the specification calls a success, and which the one caller here walks
// straight past.
func TestListDataBasesAnswersAnEmptyList(t *testing.T) {
	client := fixtureClient(t)

	buffer, err := client.allocate(64)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.core.Memory().Write(buffer, []byte(strings.Repeat("x", 8))); err != nil {
		t.Fatal(err)
	}
	if result := int32(callSlot(t, client, slotDbListDataBases, buffer, 64)); result != 0 {
		t.Fatalf("MC_dbListDataBases = %d, want 0 databases", result)
	}
	var terminator [2]byte
	if err := client.core.Memory().Read(buffer, terminator[:]); err != nil {
		t.Fatal(err)
	}
	if terminator != [2]byte{0, 0} {
		t.Fatalf("the buffer starts %v, want the empty list's two NULs", terminator)
	}

	// The arguments the specification calls invalid are refused rather than
	// answered, because a caller that passes them has a bug this platform
	// should not hide.
	if result := int32(callSlot(t, client, slotDbListDataBases, 0, 64)); result != wipiInvalid {
		t.Fatalf("a null buffer = %d, want %d", result, wipiInvalid)
	}
	if result := int32(callSlot(t, client, slotDbListDataBases, buffer, 0)); result != wipiInvalid {
		t.Fatalf("a zero length = %d, want %d", result, wipiInvalid)
	}
}

// MC_fsTell answers where the cursor is, which is how a program with no
// size call asks for a file's size: seek to the end, then ask. That is exactly
// the pair a title stops on, one instruction apart.
func TestFsTellAnswersTheCursorAfterASeekToTheEnd(t *testing.T) {
	client := fixtureClient(t)

	name, err := client.allocateBytes(append([]byte("data/hello.txt"), 0))
	if err != nil {
		t.Fatal(err)
	}
	handle := callSlot(t, client, slotFsOpen, name, fileOpenReadOnly)
	if int32(handle) < 0 {
		t.Fatalf("open answered %d", int32(handle))
	}
	if result := int32(callSlot(t, client, slotFsTell, handle)); result != 0 {
		t.Fatalf("a freshly opened file tells %d, want 0", result)
	}

	const seekFromEnd = 2
	if result := int32(callSlot(t, client, slotFsSeek, handle, 0, seekFromEnd)); result < 0 {
		t.Fatalf("seek to the end answered %d", result)
	}
	size := int32(len(client.files[handle].data))
	if result := int32(callSlot(t, client, slotFsTell, handle)); result != size {
		t.Fatalf("tell after a seek to the end is %d, want the %d-byte length", result, size)
	}

	// A handle this platform never issued is a failure rather than a zero,
	// because zero is where a caller would then start reading.
	if result := int32(callSlot(t, client, slotFsTell, handle+0x1000)); result != wipiError {
		t.Fatalf("tell on an unknown handle is %d, want %d", result, wipiError)
	}
}

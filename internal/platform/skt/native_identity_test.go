package skt

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// ServiceAllowance raises an executable's Host service step allowance only for
// a digest the registry names, and leaves every other title on the default.
func TestServiceAllowanceFollowsTheRegistry(t *testing.T) {
	var unregistered NativeExecutable
	if got := unregistered.ServiceAllowance(); got != 0 {
		t.Errorf("unregistered allowance = %d, want 0 (the default)", got)
	}
	registered := NativeExecutable{}
	digest, err := hex.DecodeString("a3d70df6a56b18053c1734462373f95150102b536a3fcf01c5ed81e815aefc34")
	if err != nil {
		t.Fatal(err)
	}
	copy(registered.Digest[:], digest)
	if got := registered.ServiceAllowance(); got != NativeHeavyLoadAllowance {
		t.Errorf("registered allowance = %d, want %d", got, NativeHeavyLoadAllowance)
	}
}

// An executable the registry does not name gets no fix and is not touched,
// which is what keeps identity compatibility to the exact recognized
// revisions.
func TestIdentityCompatibilityLeavesUnknownAlone(t *testing.T) {
	executable, err := ParseNativeExecutable(validParts().bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, matched := executable.IdentityCompatibility(); matched {
		t.Fatal("IdentityCompatibility matched an unregistered executable")
	}
	code, _, err := executable.Load(0x00100000)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), code...)
	archive := &NativeArchive{Executable: executable}
	status, subscriber, err := archive.ApplyIdentityCompatibility(code, "01000000000")
	if err != nil {
		t.Fatalf("ApplyIdentityCompatibility: %v", err)
	}
	if status != "unsupported" || subscriber != "" {
		t.Errorf("status = %q, want unsupported", status)
	}
	for index := range before {
		if before[index] != code[index] {
			t.Fatalf("code changed at %d for an unregistered executable", index)
		}
	}
}

func TestExplicitHandsetKeepsTheOriginalCertificateDataComparison(t *testing.T) {
	const digest = "a3d70df6a56b18053c1734462373f95150102b536a3fcf01c5ed81e815aefc34"
	executable := &NativeExecutable{}
	copy(executable.Digest[:], mustHex(digest))
	archive := NativeArchive{Executable: executable}
	fixes := executable.registeredFixes()
	if len(fixes) != 1 {
		t.Fatalf("registered identity fixes = %d", len(fixes))
	}
	fix := fixes[0]
	window := mustHex(fix.window)
	code := make([]byte, int(fix.offset)+len(window))
	copy(code[fix.offset:], window)
	const recorded = "01000000000" // Authored certificate field.
	for _, tt := range []struct {
		number string
		nextPC uint32
	}{
		{recorded, 20},      // Fall through to the guest's success result.
		{"01012345678", 36}, // Branch to the guest's mismatch handler.
	} {
		number := tt.number
		status, subscriber, err := archive.ApplyIdentityCompatibility(code, number)
		if err != nil || subscriber != number || status != backend.AuthenticationGuestManaged {
			t.Fatalf("original certificate check was not bound to the explicit handset: status=%s err=%v", status, err)
		}
		if !bytes.Equal(code[fix.offset:], window) {
			t.Fatal("explicit handset changed the original comparison")
		}
		// Execute the guest's decision after the string comparison, with the
		// same two independent inputs the guest receives in a session.
		checkComparison(t, code[fix.offset:], 12, 2, strings.Compare(recorded, subscriber), tt.nextPC)
	}
	status, subscriber, err := archive.ApplyIdentityCompatibility(code, "")
	if err != nil || subscriber != "" || status != backend.AuthenticationSKTNativeCertificate {
		t.Fatalf("implicit handset lost its registered behavior: status=%s err=%v", status, err)
	}
	if bytes.Equal(code[fix.offset:], window) {
		t.Fatal("the default session lost the compatibility correction")
	}
}

func TestExplicitHandsetKeepsBothThumbSubscriberChecks(t *testing.T) {
	const digest = "1966e7ecfea09d9da90cd9e3e41c00a20452bfe2fca1a8bf299d072cd1ae80ad"
	executable := &NativeExecutable{}
	copy(executable.Digest[:], mustHex(digest))
	archive := NativeArchive{Executable: executable}
	fixes := executable.registeredFixes()
	if len(fixes) != 2 {
		t.Fatalf("registered identity fixes = %d", len(fixes))
	}
	var code []byte
	for _, fix := range fixes {
		window := mustHex(fix.window)
		end := int(fix.offset) + len(window)
		if end > len(code) {
			code = append(code, make([]byte, end-len(code))...)
		}
		copy(code[fix.offset:], window)
	}
	original := bytes.Clone(code)
	const number = "01012345678"
	status, selected, err := archive.ApplyIdentityCompatibility(code, number)
	if err != nil || !bytes.Equal(code, original) || status != backend.AuthenticationGuestManaged || selected != number {
		t.Fatalf("explicit handset changed a save or certificate check: status=%s err=%v", status, err)
	}
	// The save reader branches over its success flag on mismatch. The
	// certificate reader branches to the next field only on a match.
	for _, gate := range []struct {
		fix                nativeIdentityFix
		steps              uint32
		equalPC, unequalPC uint32
	}{{fixes[0], 3, 6, 8}, {fixes[1], 2, 18, 4}} {
		length := uint32(len(mustHex(gate.fix.window)))
		window := code[gate.fix.offset : gate.fix.offset+length]
		for compared, next := range []uint32{gate.equalPC, gate.unequalPC} {
			checkComparison(t, window, 1, gate.steps, compared, next)
		}
	}
	status, selected, err = archive.ApplyIdentityCompatibility(code, "")
	if err != nil || status != backend.AuthenticationSKTNativeCertificate || selected != "" {
		t.Fatalf("implicit handset lost its default comparison: status=%s err=%v", status, err)
	}
	for _, fix := range fixes {
		window := mustHex(fix.window)
		if bytes.Equal(code[fix.offset:fix.offset+uint32(len(window))], window) {
			t.Fatal("a default comparison was lost")
		}
	}
}

// entry is relative to the start of the compiled window, with bit zero
// indicating Thumb mode just as for the program's entry points.
func checkComparison(t *testing.T, window []byte, entry, steps uint32, compared int, nextPC uint32) {
	t.Helper()
	const base = 0x1000
	memory := armcore.NewMemory()
	if err := memory.Map(base, 0x1000, armcore.PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Load(base, window); err != nil {
		t.Fatal(err)
	}
	var state armcore.Context
	if err := state.SetPC(base + entry); err != nil {
		t.Fatal(err)
	}
	state.Registers[0] = uint32(compared)
	if _, err := (armcore.Engine{}).Run(&state, memory, 0, steps); err != nil || state.PC() != base+nextPC {
		t.Fatalf("comparison result %d: reached %#x, want %#x: %v", compared, state.PC(), base+nextPC, err)
	}
}

// Every identity fix's window and replacement are well formed: hex that
// decodes, a replacement that fits inside the window at its relax offset, and
// an authentication status to report. A replacement that runs off its window
// is a build defect rather than a run-time refusal.
func TestIdentityFixesAreWellFormed(t *testing.T) {
	for _, fix := range nativeIdentityFixes {
		window := mustHex(fix.window)
		replacement := mustHex(fix.replacement)
		if int(fix.relax)+len(replacement) > len(window) {
			t.Errorf("%s: replacement of %d at relax %d runs past the %d-byte window", fix.fix, len(replacement), fix.relax, len(window))
		}
		if len(window) == 0 || len(replacement) == 0 {
			t.Errorf("%s: empty window or replacement", fix.fix)
		}
		if fix.status == "" {
			t.Errorf("%s has no authentication status", fix.fix)
		}
	}
}

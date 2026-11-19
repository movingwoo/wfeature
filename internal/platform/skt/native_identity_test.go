package skt

import (
	"encoding/hex"
	"testing"
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
	status, err := executable.ApplyIdentityCompatibility(code)
	if err != nil {
		t.Fatalf("ApplyIdentityCompatibility: %v", err)
	}
	if status != "unsupported" {
		t.Errorf("status = %q, want unsupported", status)
	}
	for index := range before {
		if before[index] != code[index] {
			t.Fatalf("code changed at %d for an unregistered executable", index)
		}
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

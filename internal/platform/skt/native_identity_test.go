package skt

import "testing"

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

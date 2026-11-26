package skt

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

const installIdentityScript = "eab7d4de11fba9a131ec0aef8a7b3947aef41317fc3d23903d0d1a3812cbe9c2"

// installIdentityArchive is a version-four image holding the recognized
// comparison where the adapter expects it. The derivation it calls and the
// refused answer after it are the test's own: a routine that returns at once,
// and `push 1; ret`.
func installIdentityArchive(t *testing.T) *Archive {
	t.Helper()
	const derivation, end = 0xb70a, 0xb70c
	image := make([]byte, end)
	site := scriptIdentitySites[installIdentityScript][0]
	copy(image[site.at:], site.expect)
	copy(image[site.at+len(site.expect):], []byte{0x05, 0x00, 0x01, 0x00, 0x86, 0x00})
	copy(image[derivation:], []byte{0x86, 0x00})
	variables := make([][]int32, 27)
	for i := range variables {
		variables[i] = []int32{int32(i)}
	}
	program := &sgsvm.Program{Wide: &sgsvm.WideProgram{Variables: variables}, Data: image, CodeStart: 128, CodeEnd: end}
	return &Archive{Script: program, scriptDigest: installIdentityScript}
}

// answer runs the identity routine and reports what it left for its caller:
// 0 for an installation the handset owns, 1 for one it does not.
func installIdentityAnswer(t *testing.T, program *sgsvm.Program, owned bool) int32 {
	t.Helper()
	vm := sgsvm.NewWide(program, func(id uint16, vm *sgsvm.WideVM) error {
		switch id {
		case 1:
			_, err := vm.Args(1)
			return err
		case 2:
			if _, err := vm.Args(1); err != nil {
				return err
			}
			return vm.Push(0)
		case 0xa4:
			if _, err := vm.Args(2); err != nil {
				return err
			}
			if owned {
				return vm.Push(0)
			}
			return vm.Push(1)
		}
		t.Fatalf("unexpected service %#x", id)
		return nil
	})
	if err := vm.Run(context.Background(), 0x1dc); err != nil {
		t.Fatal(err)
	}
	result, err := vm.Args(1)
	if err != nil {
		t.Fatal(err)
	}
	return result[0]
}

func TestScriptInstallIdentity(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		disabled, configured bool
		status               backend.AuthenticationStatus
		foreign              int32
	}{
		{"implicit", false, false, backend.AuthenticationSKTScriptInstall, 0},
		{"configured", false, true, backend.AuthenticationGuestManaged, 1},
		{"disabled", true, false, backend.AuthenticationOff, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := installIdentityArchive(t)
			packaged := bytes.Clone(archive.Script.Data)
			program, status, err := archive.scriptIdentity(tc.disabled, tc.configured)
			if err != nil {
				t.Fatal(err)
			}
			if status != tc.status {
				t.Fatalf("status = %q, want %q", status, tc.status)
			}
			if !bytes.Equal(archive.Script.Data, packaged) {
				t.Fatal("the archive's own script changed")
			}
			if accepted := tc.status == backend.AuthenticationSKTScriptInstall; (program != archive.Script) != accepted {
				t.Fatalf("session-owned copy = %v, want %v", program != archive.Script, accepted)
			}
			// Only the low word of the one branch target differs.
			want := bytes.Clone(packaged)
			if program != archive.Script {
				want[0x21c], want[0x21d] = 0x1e, 0x02
			}
			if !bytes.Equal(program.Data, want) {
				t.Fatal("the session's script differs from the packaged one outside the branch target")
			}
			if got := installIdentityAnswer(t, program, true); got != 0 {
				t.Fatalf("an owned installation answered %d", got)
			}
			if got := installIdentityAnswer(t, program, false); got != tc.foreign {
				t.Fatalf("another handset's installation answered %d, want %d", got, tc.foreign)
			}
		})
	}
}

func TestScriptInstallIdentityRecognition(t *testing.T) {
	archive := installIdentityArchive(t)
	archive.scriptDigest = "0000000000000000000000000000000000000000000000000000000000000000"
	if program, status, err := archive.scriptIdentity(false, false); err != nil || program != archive.Script || status != backend.AuthenticationUnsupported {
		t.Fatalf("unregistered script: status %q, error %v", status, err)
	}
	// A registered digest whose comparison is not the examined one is an error
	// for either handset, and nothing is copied.
	for _, configured := range []bool{false, true} {
		archive = installIdentityArchive(t)
		archive.Script.Data[0x212] ^= 1
		if _, _, err := archive.scriptIdentity(false, configured); err == nil {
			t.Fatalf("configured=%v: a changed comparison was accepted", configured)
		}
	}
	archive = installIdentityArchive(t)
	archive.Script.Data = archive.Script.Data[:0x200]
	if _, _, err := archive.scriptIdentity(false, false); err == nil {
		t.Fatal("a truncated script was accepted")
	}
	if (*ScriptSession)(nil).Authentication() != backend.AuthenticationUnsupported {
		t.Fatal("a session that never started reports a policy")
	}
}

func TestScriptIdentitySitesMatchRegistry(t *testing.T) {
	registered := compatibility.DigestsForFix(compatibility.SKTScriptInstallIdentity)
	sites := slices.Sorted(maps.Keys(scriptIdentitySites))
	if !slices.Equal(registered, sites) {
		t.Errorf("script identity digests: registry %v, Go sites %v", registered, sites)
	}
}

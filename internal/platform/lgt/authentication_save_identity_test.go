package lgt

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/wipic"
)

func TestSaveSubscriberBranchPreservesChecksum(t *testing.T) {
	for _, configured := range []bool{false, true} {
		for _, identityMatches := range []bool{false, true} {
			for _, checksumMatches := range []bool{false, true} {
				memory := armcore.NewMemory()
				if err := memory.Map(0x69000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
					t.Fatal(err)
				}
				// Authored comparison/failure window and checksum continuation.
				code := []byte{0x00, 0x28, 0x02, 0xd0, 0x1b, 0x48, 0x1c, 0x49, 0x13, 0xe0,
					0x00, 0x29, 0x01, 0xd0, 0x00, 0x22, 0x00, 0xe0, 0x01, 0x22, 0x00, 0xe0, 0xc0, 0x46}
				if err := memory.Load(0x69d98, code); err != nil {
					t.Fatal(err)
				}
				status, err := applySaveSubscriberIdentity(memory, configured)
				if err != nil {
					t.Fatal(err)
				}
				wantStatus := backend.AuthenticationLGTSaveSubscriber
				if configured {
					wantStatus = backend.AuthenticationGuestManaged
				}
				if status != wantStatus {
					t.Fatalf("configured=%t status=%q, want %q", configured, status, wantStatus)
				}
				state := armcore.NewContext()
				if !identityMatches {
					state.Registers[0] = 1
				}
				if !checksumMatches {
					state.Registers[1] = 1
				}
				if err := state.SetPC(0x69d99); err != nil {
					t.Fatal(err)
				}
				if _, err := (armcore.Engine{}).Run(&state, memory, 0xffffffff, 2); err != nil {
					t.Fatal(err)
				}
				want := uint32(0x69da2)
				if configured && !identityMatches {
					want = 0x69d9c
				}
				if state.Registers[15]&^1 != want {
					t.Fatalf("validation target=%#x want=%#x", state.Registers[15], want)
				}
				if want == 0x69da2 {
					if _, err := (armcore.Engine{}).Run(&state, memory, 0x69db0, 20); err != nil {
						t.Fatal(err)
					}
					expected := uint32(0)
					if checksumMatches {
						expected = 1
					}
					if state.Registers[2] != expected {
						t.Fatal("checksum validation changed")
					}
				}
				got := make([]byte, len(code))
				if err := memory.Read(0x69d98, got); err != nil {
					t.Fatal(err)
				}
				if !configured {
					code[3] = 0xe0
				}
				if !bytes.Equal(got, code) {
					t.Fatal("surrounding code changed")
				}
			}
		}
	}
}

func TestSaveSubscriberBranchRejectsUnexpectedCode(t *testing.T) {
	memory := armcore.NewMemory()
	if _, err := applySaveSubscriberIdentity(memory, false); err == nil {
		t.Fatal("unmapped code accepted")
	}
	if err := memory.Map(0x69000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	if _, err := applySaveSubscriberIdentity(memory, false); err == nil {
		t.Fatal("unknown code accepted")
	}
	if hasSaveIdentityCompatibility([]byte("unrecognized module")) {
		t.Fatal("unknown revision matched")
	}
}

func setTestHandset(t *testing.T, number string) {
	t.Helper()
	if number == "" {
		wipic.ResetSubscriberNumber()
	} else if err := wipic.SetSubscriberNumber(number); err != nil {
		t.Fatal(err)
	}
}

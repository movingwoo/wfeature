package lgt

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

func TestSaveSubscriberBranchPreservesChecksum(t *testing.T) {
	for _, corrected := range []bool{false, true} {
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
				if corrected {
					if err := correctSaveSubscriberIdentity(memory); err != nil {
						t.Fatal(err)
					}
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
				if !corrected && !identityMatches {
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
				if corrected {
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
	if err := correctSaveSubscriberIdentity(memory); err == nil {
		t.Fatal("unmapped code accepted")
	}
	if err := memory.Map(0x69000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	if err := correctSaveSubscriberIdentity(memory); err == nil {
		t.Fatal("unknown code accepted")
	}
	if hasSaveIdentityCompatibility([]byte("unrecognized module")) {
		t.Fatal("unknown revision matched")
	}
}

func TestLocalSaveSubscriberArchive(t *testing.T) {
	path := os.Getenv("WFEATURE_LGT_SAVE_SUBSCRIBER_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_LGT_SAVE_SUBSCRIBER_ARCHIVE")
	}
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

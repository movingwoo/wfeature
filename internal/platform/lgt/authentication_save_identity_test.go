package lgt

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
	"github.com/movingwoo/wfeature/internal/wipic"
)

const (
	saveReaderModule  = "b2437411838c04e13f62fd6b9a206c348d87b3eac4119971226c30169286168a"
	ownerRecordModule = "38baf4aa1f650faaa099eb34071b4938ecf16b422ded84d7717552ded71af31f"
)

// subscriberFixture maps the pages a module's recognized comparisons live in
// and loads each authored window where the adapter expects it. The code after
// a window, the routines it calls and the words it reads are the test's own.
func subscriberFixture(t *testing.T, digest string) *armcore.Memory {
	t.Helper()
	memory := armcore.NewMemory()
	for _, page := range []uint32{0x68000, 0x69000, 0x70000, 0xca000, 0x10e000, 0x200000} {
		if err := memory.Map(page, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
	}
	for _, site := range subscriberSites[digest] {
		if err := memory.Load(site.at, site.expect); err != nil {
			t.Fatal(err)
		}
	}
	for address, code := range map[uint32][]byte{
		// The save reader's checksum continuation: r2 answers whether r1 is 0.
		0x69da2: {0x00, 0x29, 0x01, 0xd0, 0x00, 0x22, 0x00, 0xe0, 0x01, 0x22, 0x00, 0xe0, 0xc0, 0x46},
		// The registration check's three answers after its window:
		// movs r0,#0; b end; movs r0,#1; rsbs r0,r0,#0; end.
		0x682e6: {0x00, 0x20, 0x01, 0xe0, 0x01, 0x20, 0x40, 0x42, 0xc0, 0x46},
		// Both modules call through a register: bx r5, and bx r3.
		0xcaa00:  {0x28, 0x47},
		0x10e224: {0x18, 0x47},
		// The comparison each calls answers with r7: adds r0,r7,#0; bx lr.
		0x200100: {0x38, 0x1c, 0x70, 0x47},
		// The owner check's two answers after its window: movs r0,#1; b end
		// on the matching path, and movs r0,#0 where a refusal lands.
		0x70eb8: {0x01, 0x20, 0x09, 0xe0},
		0x70ecc: {0x00, 0x20, 0xc0, 0x46},
		// The word the owner check loads its comparison from.
		0x70f08: {0x01, 0x01, 0x20, 0x00},
	} {
		if err := memory.Load(address, code); err != nil {
			t.Fatal(err)
		}
	}
	return memory
}

func runThumb(t *testing.T, memory *armcore.Memory, state *armcore.Context, start, end uint32) {
	t.Helper()
	if err := state.SetPC(start | 1); err != nil {
		t.Fatal(err)
	}
	result, err := (armcore.Engine{}).Run(state, memory, end, 64)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != armcore.StopEnd {
		t.Fatalf("run from %#x stopped at %#x before %#x", start, state.PC(), end)
	}
}

func TestSaveSubscriberBranchPreservesChecksum(t *testing.T) {
	for _, configured := range []bool{false, true} {
		for _, identityMatches := range []bool{false, true} {
			for _, checksumMatches := range []bool{false, true} {
				memory := subscriberFixture(t, saveReaderModule)
				status, err := applySaveSubscriberIdentity(memory, saveReaderModule, configured)
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
			}
		}
	}
}

// The registration record names PHONENUMBER, the constant a pending
// registration is written with, or another handset. Only the last answer
// changes, and only for the implicit handset.
func TestRegistrationRecordAcceptsArchivedSubscriber(t *testing.T) {
	const registered, pending, refused = 1, 0, 0xffffffff
	for _, tc := range []struct {
		name                   string
		configured             bool
		handset, pendingRecord bool
		attempts               uint32
		want                   uint32
	}{
		{"handset", false, true, false, 0, registered},
		{"handset, configured", true, true, false, 0, registered},
		{"pending", false, false, true, 9, pending},
		{"pending, configured", true, false, true, 9, pending},
		{"pending past its attempts", false, false, true, 10, registered},
		{"pending past its attempts, configured", true, false, true, 10, registered},
		{"archived", false, false, false, 0, registered},
		{"archived, configured", true, false, false, 0, refused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			memory := subscriberFixture(t, saveReaderModule)
			if _, err := applySaveSubscriberIdentity(memory, saveReaderModule, tc.configured); err != nil {
				t.Fatal(err)
			}
			if err := memory.Load(0x200200, []byte{byte(tc.attempts), 0, 0, 0}); err != nil {
				t.Fatal(err)
			}
			state := armcore.NewContext()
			if !tc.handset {
				state.Registers[0] = 1
			}
			if !tc.pendingRecord {
				state.Registers[7] = 1
			}
			state.Registers[5] = 0x200101
			state.Registers[6] = 0x200200
			runThumb(t, memory, &state, 0x682ca, 0x682ee)
			if state.Registers[0] != tc.want {
				t.Fatalf("answer = %#x, want %#x", state.Registers[0], tc.want)
			}
		})
	}
}

// The owner record either names PHONENUMBER or it does not.
func TestOwnerRecordAcceptsArchivedSubscriber(t *testing.T) {
	for _, tc := range []struct {
		name                string
		configured, handset bool
		accepted            uint32
		status              backend.AuthenticationStatus
	}{
		{"handset", false, true, 1, backend.AuthenticationLGTSaveSubscriber},
		{"handset, configured", true, true, 1, backend.AuthenticationGuestManaged},
		{"archived", false, false, 1, backend.AuthenticationLGTSaveSubscriber},
		{"archived, configured", true, false, 0, backend.AuthenticationGuestManaged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			memory := subscriberFixture(t, ownerRecordModule)
			status, err := applySaveSubscriberIdentity(memory, ownerRecordModule, tc.configured)
			if err != nil {
				t.Fatal(err)
			}
			if status != tc.status {
				t.Fatalf("status = %q, want %q", status, tc.status)
			}
			state := armcore.NewContext()
			state.Registers[13] = 0x200800
			if !tc.handset {
				state.Registers[7] = 1
			}
			runThumb(t, memory, &state, 0x70ea0, 0x70ed0)
			if state.Registers[0] != tc.accepted {
				t.Fatalf("answer = %d, want %d", state.Registers[0], tc.accepted)
			}
		})
	}
}

// Nothing but the one instruction of each comparison is written, and a
// configured handset writes nothing.
func TestSaveSubscriberSitesChangeOneInstruction(t *testing.T) {
	for digest, sites := range subscriberSites {
		for _, configured := range []bool{false, true} {
			memory := subscriberFixture(t, digest)
			if _, err := applySaveSubscriberIdentity(memory, digest, configured); err != nil {
				t.Fatal(err)
			}
			for _, site := range sites {
				want := bytes.Clone(site.expect)
				if !configured {
					copy(want[site.branch:], site.accept[:])
				}
				got := make([]byte, len(want))
				if err := memory.Read(site.at, got); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("module %.8s configured=%t: code at %#x = %x, want %x", digest, configured, site.at, got, want)
				}
			}
		}
	}
}

func TestSaveSubscriberBranchRejectsUnexpectedCode(t *testing.T) {
	memory := armcore.NewMemory()
	if _, err := applySaveSubscriberIdentity(memory, saveReaderModule, false); err == nil {
		t.Fatal("unmapped code accepted")
	}
	if err := memory.Map(0x69000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	if _, err := applySaveSubscriberIdentity(memory, saveReaderModule, false); err == nil {
		t.Fatal("unknown code accepted")
	}
	// One comparison that is not the examined one leaves the others alone.
	memory = subscriberFixture(t, saveReaderModule)
	if err := memory.Write(0x682da, []byte{0x05, 0xd1}); err != nil {
		t.Fatal(err)
	}
	if _, err := applySaveSubscriberIdentity(memory, saveReaderModule, false); err == nil {
		t.Fatal("changed comparison accepted")
	}
	var reader [2]byte
	if err := memory.Read(0x69d9a, reader[:]); err != nil {
		t.Fatal(err)
	}
	if reader != [2]byte{0x02, 0xd0} {
		t.Fatalf("save reader comparison = %x after a refused module", reader)
	}
	if _, err := applySaveSubscriberIdentity(subscriberFixture(t, saveReaderModule), "unknown", false); err == nil {
		t.Fatal("module without recorded comparisons accepted")
	}
	if compatibility.IdentifyNative("lgt", []byte("unrecognized module")).Has(compatibility.LGTSaveSubscriberIdentity) {
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

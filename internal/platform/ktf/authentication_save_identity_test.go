package ktf

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

const ownerRecordClient = "1d34e05b90527ef933fc96e1ca4ddf10857844490ed441bca578767e6a6a289a"

// ownerRecordFixture maps the pages the recognized comparison lives in and
// loads its authored window where the adapter expects it. The routine it
// calls, the words it reads and the code after it are the test's own.
func ownerRecordFixture(t *testing.T) *armcore.Memory {
	t.Helper()
	memory := armcore.NewMemory()
	for _, page := range []uint32{0x13a000, 0x200000, 0x250000} {
		if err := memory.Map(page, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
	}
	for _, site := range subscriberSites[ownerRecordClient] {
		if err := memory.Load(site.at, site.expect); err != nil {
			t.Fatal(err)
		}
	}
	for address, code := range map[uint32][]byte{
		// A refusal answers 0 and a missing record 2, and both rejoin the
		// window's own epilogue: movs r0,#N; b 0x13adda.
		0x13ade6: {0x00, 0x20, 0xf7, 0xe7},
		0x13adea: {0x02, 0x20, 0xf5, 0xe7},
		// The offset of the comparison in the table r10 addresses, the table
		// word, and the call through r3.
		0x13ae10: {0x10, 0x00, 0x00, 0x00},
		0x200010: {0x01, 0x01, 0x20, 0x00},
		0x2507b4: {0x18, 0x47},
		// The comparison answers with r7: adds r0,r7,#0; bx lr.
		0x200100: {0x38, 0x1c, 0x70, 0x47},
	} {
		if err := memory.Load(address, code); err != nil {
			t.Fatal(err)
		}
	}
	return memory
}

// The owner record is missing, names PHONENUMBER, or names another handset.
// Only the last answer changes, and only for the implicit handset.
func TestOwnerRecordAcceptsArchivedSubscriber(t *testing.T) {
	const missing, refused, accepted = 2, 0, 1
	for _, tc := range []struct {
		name                         string
		configured, present, handset bool
		want                         uint32
		status                       backend.AuthenticationStatus
	}{
		{"missing", false, false, false, missing, backend.AuthenticationKTFSaveSubscriber},
		{"missing, configured", true, false, false, missing, backend.AuthenticationGuestManaged},
		{"handset", false, true, true, accepted, backend.AuthenticationKTFSaveSubscriber},
		{"handset, configured", true, true, true, accepted, backend.AuthenticationGuestManaged},
		{"archived", false, true, false, accepted, backend.AuthenticationKTFSaveSubscriber},
		{"archived, configured", true, true, false, refused, backend.AuthenticationGuestManaged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			memory := ownerRecordFixture(t)
			status, err := applySaveSubscriberIdentity(memory, ownerRecordClient, tc.configured)
			if err != nil {
				t.Fatal(err)
			}
			if status != tc.status {
				t.Fatalf("status = %q, want %q", status, tc.status)
			}
			state := armcore.NewContext()
			state.Registers[13] = 0x200800
			state.Registers[10] = 0x200000
			if tc.present {
				state.Registers[0] = 12
			}
			if !tc.handset {
				state.Registers[7] = 1
			}
			if err := state.SetPC(0x13adc3); err != nil {
				t.Fatal(err)
			}
			result, err := (armcore.Engine{}).Run(&state, memory, 0x13addc, 64)
			if err != nil {
				t.Fatal(err)
			}
			if result.Reason != armcore.StopEnd {
				t.Fatalf("stopped at %#x before the epilogue", state.PC())
			}
			if state.Registers[0] != tc.want {
				t.Fatalf("answer = %d, want %d", state.Registers[0], tc.want)
			}
			site := subscriberSites[ownerRecordClient][0]
			want := bytes.Clone(site.expect)
			if !tc.configured {
				copy(want[site.branch:], site.accept[:])
			}
			got := make([]byte, len(want))
			if err := memory.Read(site.at, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("code = %x, want %x", got, want)
			}
		})
	}
}

func TestOwnerRecordRejectsUnexpectedCode(t *testing.T) {
	memory := armcore.NewMemory()
	if _, err := applySaveSubscriberIdentity(memory, ownerRecordClient, false); err == nil {
		t.Fatal("unmapped code accepted")
	}
	memory = ownerRecordFixture(t)
	if err := memory.Write(0x13add6, []byte{0x05, 0xd1}); err != nil {
		t.Fatal(err)
	}
	if _, err := applySaveSubscriberIdentity(memory, ownerRecordClient, false); err == nil {
		t.Fatal("changed comparison accepted")
	}
	if _, err := applySaveSubscriberIdentity(ownerRecordFixture(t), "unknown", false); err == nil {
		t.Fatal("client without recorded comparisons accepted")
	}
	if compatibility.IdentifyNative("ktf", []byte("unrecognized client")).Has(compatibility.KTFSaveSubscriberIdentity) {
		t.Fatal("unknown revision matched")
	}
}

const saveOwnerClient = "ef321ab32b16f9acf4ad365cef2b1fce89eb6caf6e434a76d5bb29fc623bebb3"

// saveOwnerFixture loads the recognized save reader's comparison where the
// adapter expects it. The two routines it calls and both of its ways out are
// the test's own: the handset read does nothing, the comparison answers with
// r5, and the refused and accepted paths answer 0 and 1 at 0x10fda8.
func saveOwnerFixture(t *testing.T) *armcore.Memory {
	t.Helper()
	memory := armcore.NewMemory()
	for _, page := range []uint32{0x10f000, 0x119000, 0x21e000, 0x200000} {
		if err := memory.Map(page, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
	}
	for _, site := range subscriberSites[saveOwnerClient] {
		if err := memory.Load(site.at, site.expect); err != nil {
			t.Fatal(err)
		}
	}
	for address, code := range map[uint32][]byte{
		// Refused: movs r0,#0; b 0x10fda8. Accepted: movs r0,#1; nop.
		0x10fce4: {0x00, 0x20, 0x5f, 0xe0},
		0x10fda4: {0x01, 0x20, 0xc0, 0x46},
		// The handset read: bx lr. The comparison: adds r0,r5,#0; bx lr.
		0x119398: {0x70, 0x47},
		0x21eb44: {0x28, 0x1c, 0x70, 0x47},
	} {
		if err := memory.Load(address, code); err != nil {
			t.Fatal(err)
		}
	}
	return memory
}

// A save names PHONENUMBER or another handset, and its caller asks for the
// comparison or does not. Only a different handset's save changes, and only
// for the implicit handset.
func TestSaveOwnerAcceptsArchivedSubscriber(t *testing.T) {
	const refused, accepted = 0, 1
	for _, tc := range []struct {
		name                       string
		configured, asked, handset bool
		want                       uint32
		status                     backend.AuthenticationStatus
	}{
		{"not asked", false, false, false, accepted, backend.AuthenticationKTFSaveSubscriber},
		{"not asked, configured", true, false, false, accepted, backend.AuthenticationKTFSaveSubscriber},
		{"handset", false, true, true, accepted, backend.AuthenticationKTFSaveSubscriber},
		{"handset, configured", true, true, true, accepted, backend.AuthenticationKTFSaveSubscriber},
		{"archived", false, true, false, accepted, backend.AuthenticationKTFSaveSubscriber},
		// This client deletes a save it refuses, so a configured handset is
		// accepted too.
		{"archived, configured", true, true, false, accepted, backend.AuthenticationKTFSaveSubscriber},
	} {
		t.Run(tc.name, func(t *testing.T) {
			memory := saveOwnerFixture(t)
			status, err := applySaveSubscriberIdentity(memory, saveOwnerClient, tc.configured)
			if err != nil {
				t.Fatal(err)
			}
			if status != tc.status {
				t.Fatalf("status = %q, want %q", status, tc.status)
			}
			state := armcore.NewContext()
			state.Registers[13] = 0x200800
			state.Registers[8] = 0x200900
			if tc.asked {
				state.Registers[7] = 1
			}
			if !tc.handset {
				state.Registers[5] = 1
			}
			if err := state.SetPC(0x10fd85); err != nil {
				t.Fatal(err)
			}
			result, err := (armcore.Engine{}).Run(&state, memory, 0x10fda8, 64)
			if err != nil {
				t.Fatal(err)
			}
			if result.Reason != armcore.StopEnd {
				t.Fatalf("stopped at %#x before either way out", state.PC())
			}
			if state.Registers[0] != tc.want {
				t.Fatalf("answer = %d, want %d", state.Registers[0], tc.want)
			}
			site := subscriberSites[saveOwnerClient][0]
			want := bytes.Clone(site.expect)
			copy(want[site.branch:], site.accept[:])
			got := make([]byte, len(want))
			if err := memory.Read(site.at, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("code = %x, want %x", got, want)
			}
		})
	}
}

func TestSaveOwnerRejectsUnexpectedCode(t *testing.T) {
	memory := saveOwnerFixture(t)
	if err := memory.Write(0x10fda2, []byte{0x9e, 0xd1}); err != nil {
		t.Fatal(err)
	}
	if _, err := applySaveSubscriberIdentity(memory, saveOwnerClient, false); err == nil {
		t.Fatal("changed comparison accepted")
	}
}

package ktf

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func TestSaveOwnerRecordChecksItsContent(t *testing.T) {
	// An authored 29-byte record with a nonzero seed, table and payload.
	data, err := hex.DecodeString("1d000000a87125319708a1492c7948a010923a5ab35173676829306e0c")
	if err != nil {
		t.Fatal(err)
	}
	var table [16]byte
	for i := range table {
		table[i] = byte(11*i + 13)
	}
	before := bytes.Clone(data)
	if got := saveRecordOwner(data, &table); got != "01024681357" {
		t.Fatalf("owner = %q", got)
	}
	if !bytes.Equal(data, before) {
		t.Fatal("reading the owner changed the record")
	}
	for _, index := range []int{0, 4, 8, 20, len(data) - 1} {
		damaged := bytes.Clone(data)
		damaged[index] ^= 1
		if owner := saveRecordOwner(damaged, &table); owner != "" {
			t.Fatalf("accepted a record damaged at %d", index)
		}
	}
	for n := 0; n < 24; n++ {
		if owner := saveRecordOwner(data[:n], &table); owner != "" {
			t.Fatalf("accepted a %d-byte record", n)
		}
	}
	for _, record := range [][]byte{
		handsetRecord(112, "", 0x70000026),
		handsetRecord(112, "0102468135x", 0x47327025),
		handsetRecord(112, "012345678901", 0x4c3d3413),
	} {
		if owner := saveRecordOwner(record, &[16]byte{}); owner != "" {
			t.Fatal("accepted a malformed owner with an intact checksum")
		}
	}
}

// The fixture has a zero encoding table, a zero seed and a zero-filled
// payload. The stored checksums were calculated independently.
func handsetRecord(size int, owner string, checksum uint32) []byte {
	data := make([]byte, size)
	binary.LittleEndian.PutUint32(data, uint32(size))
	copy(data[8:20], owner)
	binary.LittleEndian.PutUint32(data[size-4:], checksum)
	return data
}

const saveOwnerHandsetClient = "0324122a4015ed83c274c01ae15198d69240f971825ee583b15100ea31d4077b"

func TestSaveOwnerHandsetUsesMatchingLiveRecords(t *testing.T) {
	const (
		capture = "01024681357"
		player  = "01987654321"
		handset = "01000000000"
	)
	captured := map[string][]byte{
		"vd.dat": handsetRecord(112, capture, 0x47323f25),
		"pd.dat": handsetRecord(1268, capture, 0xc3363f25),
	}
	vd := handsetRecord(112, player, 0x44353d2a)
	pd := handsetRecord(1268, player, 0xc0313d2a)
	if !compatibility.HasFix("ktf", compatibility.NativeModule, saveOwnerHandsetClient, compatibility.KTFSaveOwnerHandset) {
		t.Fatal("missing save owner handset revision")
	}
	for _, tc := range []struct {
		name       string
		saved      map[string][]byte
		configured bool
		want       string
		status     backend.AuthenticationStatus
	}{
		{"captured pair", nil, false, capture, backend.AuthenticationKTFSaveSubscriber},
		{"persisted pair", map[string][]byte{"db/vd.dat": vd, "db/pd.dat": pd}, false, player, backend.AuthenticationKTFSaveSubscriber},
		{"file overlay", map[string][]byte{"fs/vd.dat": vd, "db/pd.dat": pd}, false, player, backend.AuthenticationKTFSaveSubscriber},
		{"conflicting owners", map[string][]byte{"db/pd.dat": pd}, false, handset, backend.AuthenticationGuestManaged},
		{"invalid checksum", map[string][]byte{"db/vd.dat": handsetRecord(112, capture, 0)}, false, handset, backend.AuthenticationGuestManaged},
		{"oversized shadow", map[string][]byte{"db/vd.dat": append(bytes.Clone(captured["vd.dat"]), 0)}, false, handset, backend.AuthenticationGuestManaged},
		{"deleted record", map[string][]byte{wipicrt.DatabaseRemovedKey: []byte("vd.dat")}, false, handset, backend.AuthenticationGuestManaged},
		{"configured", nil, true, handset, backend.AuthenticationGuestManaged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newTestRuntime(t)
			client.image.Data = make([]byte, 0xe4394+16)
			client.SubscriberNumber = handset
			client.authentication = backend.AuthenticationUnsupported
			client.AttachFilesystem(captured)
			client.AttachSaveStore(testSaveStore(t, tc.saved))
			if err := client.selectSaveOwnerHandset(tc.configured); err != nil {
				t.Fatal(err)
			}
			if client.SubscriberNumber != tc.want || client.authentication != tc.status {
				t.Fatalf("selected (%q, %q), want (%q, %q)", client.SubscriberNumber, client.authentication, tc.want, tc.status)
			}
		})
	}
}

func TestSaveOwnerHandsetReportsUnreadableRecords(t *testing.T) {
	client, _ := newTestRuntime(t)
	client.image.Data = make([]byte, 0xe4394+16)
	client.AttachSaveStore(&wipicrttest.UnreadableSaves{
		DirectorySaveStore: wipicrt.NewDirectorySaveStore(t.TempDir()),
		Keys:               map[string]bool{"db/vd.dat": true},
	})
	if err := client.selectSaveOwnerHandset(false); err == nil {
		t.Fatal("an unreadable save was treated as absent")
	}
}

func TestUnregisteredClientSkipsSaveOwnerHandset(t *testing.T) {
	data, err := testfixture.KTFSaveArchive()
	if err != nil {
		t.Fatal(err)
	}
	session, err := StartSession(t.Context(), data, SessionOptions{SaveStore: testSaveStore(t, nil)})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.Client.authentication != backend.AuthenticationUnsupported {
		t.Fatalf("unregistered client selected %v", session.Client.authentication)
	}
}

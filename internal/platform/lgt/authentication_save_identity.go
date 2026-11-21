package lgt

import (
	"bytes"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// A subscriberSite is one conditional branch a recognized module takes when a
// record in its private storage names a subscriber other than PHONENUMBER.
// expect is the code that has to be at `at` for the branch to be the one that
// was examined, branch the offset in it of the two-byte Thumb instruction, and
// accept the instruction that takes the matching subscriber's path instead.
type subscriberSite struct {
	at     uint32
	expect []byte
	branch int
	accept [2]byte
}

// subscriberSites are, by module digest, the comparisons
// lgt.save_subscriber_identity accepts. A capture of a handset carries the
// files its title wrote there, and each of these records names that handset:
// read on any other, the title refuses to start or to load. Every site is the
// comparison alone. The lengths, checksums and digests the same readers check
// run as they were, and a record that does not name a subscriber at all — one
// the title writes itself while its own registration is pending — keeps its
// own path. A digest the fix selects without sites here fails loading rather
// than run with the comparison in place.
// See docs/authentication.md#archived-subscriber-records.
var subscriberSites = map[string][]subscriberSite{
	"b2437411838c04e13f62fd6b9a206c348d87b3eac4119971226c30169286168a": {
		// The save reader validates the length, decodes the payload, compares
		// the 12-byte saved subscriber and then validates the checksum:
		// cmp r0,#0; beq checksum becomes cmp r0,#0; b checksum.
		{at: 0x69d98, expect: []byte{0x00, 0x28, 0x02, 0xd0, 0x1b, 0x48, 0x1c, 0x49, 0x13, 0xe0}, branch: 2, accept: [2]byte{0x02, 0xe0}},
		// The registration record is compared with PHONENUMBER, then with the
		// constant the title writes while registration is pending. Naming
		// neither answers -1, which ends the title at its failure notice:
		// bne refused becomes bne registered, the answer for PHONENUMBER. The
		// pending record still reaches its attempt count.
		{at: 0x682ca, expect: []byte{0x00, 0x28, 0x09, 0xd0, 0x0f, 0x48, 0x21, 0x1c, 0x0c, 0x22, 0x62, 0xf0, 0x94, 0xfb,
			0x00, 0x28, 0x06, 0xd1, 0x33, 0x68, 0x09, 0x2b, 0x01, 0xdd, 0x01, 0x20, 0x03, 0xe0}, branch: 16, accept: [2]byte{0x02, 0xd1}},
	},
	"38baf4aa1f650faaa099eb34071b4938ecf16b422ded84d7717552ded71af31f": {
		// The owner record holds the subscriber that first started the title.
		// A missing record is written for PHONENUMBER; a different one answers
		// 0, which ends the title at its failure notice:
		// strcmp; cmp r3,#0; bne refused becomes a nop, the matching path.
		{at: 0x70ea0, expect: []byte{0x07, 0x9b, 0x09, 0xaa, 0x18, 0x1c, 0x11, 0x1c, 0x17, 0x4b, 0x9d, 0xf0, 0xbb, 0xf9,
			0x03, 0x1c, 0x00, 0x2b, 0x0b, 0xd1, 0x07, 0x9b, 0x01, 0x93}, branch: 18, accept: [2]byte{0xc0, 0x46}},
	},
}

func hasSaveIdentityCompatibility(module []byte) bool {
	return compatibility.HasFix("lgt", compatibility.NativeModule, moduleDigest(module), compatibility.LGTSaveSubscriberIdentity)
}

// applySaveSubscriberIdentity accepts the archived subscriber at each of the
// module's recognized comparisons. A configured handset keeps the original
// comparisons on every record the title opens; the code is checked either way.
func applySaveSubscriberIdentity(memory *armcore.Memory, digest string, configured bool) (backend.AuthenticationStatus, error) {
	sites := subscriberSites[digest]
	if len(sites) == 0 {
		return "", fmt.Errorf("LGT save subscriber compatibility: no comparison is known for module %s", digest)
	}
	// Every site is checked before any is written, so a module that is not
	// the one examined is left exactly as it was loaded.
	for _, site := range sites {
		code := make([]byte, len(site.expect))
		if err := memory.Read(site.at, code); err != nil {
			return "", fmt.Errorf("LGT save subscriber compatibility: %w", err)
		}
		if !bytes.Equal(code, site.expect) {
			return "", fmt.Errorf("LGT save subscriber compatibility: unexpected validation branch at %#x", site.at)
		}
	}
	if configured {
		return backend.AuthenticationGuestManaged, nil
	}
	for _, site := range sites {
		if err := memory.Write(site.at+uint32(site.branch), site.accept[:]); err != nil {
			return "", err
		}
	}
	return backend.AuthenticationLGTSaveSubscriber, nil
}

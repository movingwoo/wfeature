package ktf

import (
	"bytes"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// A subscriberSite is one conditional branch a recognized client takes when a
// record in its private storage names a subscriber other than PHONENUMBER.
// expect is the code that has to be at `at` once the client has relocated
// itself, branch the offset in it of the two-byte Thumb instruction, and
// accept the instruction that takes the matching subscriber's path instead.
type subscriberSite struct {
	at     uint32
	expect []byte
	branch int
	accept [2]byte
}

// subscriberSites are, by the digest of the client image as the JAR packages
// it, the comparisons ktf.save_subscriber_identity accepts. A capture of a
// handset carries the files its title wrote there, and the record at each of
// these sites names that handset: read on any other, the title stops at its
// failure notice or discards the save. The record's own length and digest
// checks run as they were.
// A digest the fix selects without sites here fails starting rather than run
// with the comparison in place.
// See docs/authentication.md#archived-subscriber-records.
var subscriberSites = map[string][]subscriberSite{
	"1d34e05b90527ef933fc96e1ca4ddf10857844490ed441bca578767e6a6a289a": {
		// The owner record holds the subscriber that first started the title.
		// A missing record is written for PHONENUMBER; a different one answers
		// 0: strcmp; cmp r0,#0; bne refused becomes a nop, the matching path.
		{at: 0x13adc2, expect: []byte{0x00, 0x28, 0x11, 0xdd, 0x12, 0x4b, 0x53, 0x44, 0x1b, 0x68, 0x68, 0x46, 0x29, 0x1c,
			0x15, 0xf1, 0xf0, 0xfc, 0x00, 0x28, 0x06, 0xd1, 0x01, 0x20, 0x06, 0xb0}, branch: 20, accept: [2]byte{0xc0, 0x46}},
	},
	"ef321ab32b16f9acf4ad365cef2b1fce89eb6caf6e434a76d5bb29fc623bebb3": {
		// Every save begins with the subscriber that wrote it, and the reader
		// compares it with PHONENUMBER when its caller asks. A different one
		// answers "no such save": the title deletes the file and writes its
		// defaults over it. cmp r0,#0; bne refused becomes a nop, the matching
		// path, between the length check before it and the checksum after.
		{at: 0x10fd84, expect: []byte{0x00, 0x2f, 0x0d, 0xd0, 0x03, 0xac, 0x00, 0x22, 0x20, 0x1c, 0x03, 0x92, 0x04, 0x92, 0x05, 0x92,
			0x09, 0xf0, 0x00, 0xfb, 0x40, 0x46, 0x21, 0x1c, 0x0e, 0xf1, 0xd2, 0xfe, 0x00, 0x28, 0x9f, 0xd1}, branch: 30, accept: [2]byte{0xc0, 0x46}},
	},
}

// deletingClients answer a save another handset wrote by deleting it, without
// a notice. Their comparison is accepted for a configured handset too: the
// handset setting is the Host's, shared by every title, and changing it for
// another title would otherwise delete every save of these, the ones the
// player made on the implicit handset among them.
var deletingClients = map[string]bool{
	"ef321ab32b16f9acf4ad365cef2b1fce89eb6caf6e434a76d5bb29fc623bebb3": true,
}

// applySaveSubscriberIdentity accepts the archived subscriber at each of the
// client's recognized comparisons. A configured handset keeps the original
// comparisons, except in a client that deletes what it does not accept; the
// code is checked either way, and nothing is written unless every site holds
// the code that was examined.
func applySaveSubscriberIdentity(memory *armcore.Memory, digest string, configured bool) (backend.AuthenticationStatus, error) {
	sites := subscriberSites[digest]
	if len(sites) == 0 {
		return "", fmt.Errorf("KTF save subscriber compatibility: no comparison is known for client %s", digest)
	}
	for _, site := range sites {
		code := make([]byte, len(site.expect))
		if err := memory.Read(site.at, code); err != nil {
			return "", fmt.Errorf("KTF save subscriber compatibility: %w", err)
		}
		if !bytes.Equal(code, site.expect) {
			return "", fmt.Errorf("KTF save subscriber compatibility: unexpected validation branch at %#x", site.at)
		}
	}
	if configured && !deletingClients[digest] {
		return backend.AuthenticationGuestManaged, nil
	}
	for _, site := range sites {
		if err := memory.Write(site.at+uint32(site.branch), site.accept[:]); err != nil {
			return "", err
		}
	}
	return backend.AuthenticationKTFSaveSubscriber, nil
}

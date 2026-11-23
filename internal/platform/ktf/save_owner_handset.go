package ktf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/bits"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// This client loads both private records on startup. The same handset must
// own both. Its record reader checks the length, owner and checksum before it
// uses any saved progress. Select the handset before the guest reads it.
func (client *Client) selectSaveOwnerHandset(digest string, configured bool) error {
	if !compatibility.HasFix("ktf", compatibility.NativeModule, digest, compatibility.KTFSaveOwnerHandset) {
		return nil
	}
	client.authentication = backend.AuthenticationGuestManaged
	if configured {
		return nil
	}
	// The save reader uses this 16-byte table in the registered client image
	// for both the byte stream and the checksum.
	const tableOffset = 0xe4394
	if len(client.image.Data) < tableOffset+16 {
		return fmt.Errorf("KTF save owner table is outside the client image")
	}
	table := (*[16]byte)(client.image.Data[tableOffset:])
	var owner string
	for _, record := range [...]struct {
		name string
		size int
	}{{"vd.dat", 112}, {"pd.dat", 1268}} {
		data, err := client.runtime.SizedCFile(record.name, record.size)
		if err != nil {
			return fmt.Errorf("read KTF save owner in %s: %w", record.name, err)
		}
		number := saveRecordOwner(data, table)
		if number == "" || owner != "" && number != owner {
			return nil
		}
		owner = number
	}
	client.SubscriberNumber = owner
	client.authentication = backend.AuthenticationKTFSaveSubscriber
	return nil
}

// The record starts with its length and a seed. The remaining bytes hold a
// zero-terminated owner field, the payload and a checksum of the plaintext.
func saveRecordOwner(record []byte, table *[16]byte) string {
	if len(record) < 24 || len(record) > 1268 || uint32(len(record)) != binary.LittleEndian.Uint32(record) {
		return ""
	}
	plain := bytes.Clone(record)
	seed := binary.LittleEndian.Uint32(plain[4:])
	state := seed
	next := func() uint32 {
		state = state*214013 + 2531011
		return state >> 16 & 0x7fff
	}
	checksum := next()
	state = seed
	for i := 8; i < len(plain); i++ {
		plain[i] ^= table[next()&15]
	}
	body := plain[:len(plain)-4]
	for _, b := range body {
		checksum = bits.RotateLeft32(checksum, 8) ^ uint32(b^table[b&15])
	}
	if checksum != binary.LittleEndian.Uint32(plain[len(body):]) {
		return ""
	}
	field, _, terminated := bytes.Cut(plain[8:20], []byte{0})
	number := string(field)
	if !terminated || wipic.ValidateSubscriberNumber(number) != nil {
		return ""
	}
	return number
}

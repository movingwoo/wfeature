package lgt

import (
	"bytes"
	"fmt"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// dataServiceLockProperty is the handset setting a title reads before it
// dials: "1" says the subscriber has locked wireless internet.
const dataServiceLockProperty = "DS_LOCK"

// A dataServiceLockReader is the code of a recognized module that asks for the
// setting: expect is what has to be at `at` once the module is loaded.
type dataServiceLockReader struct {
	at     uint32
	expect []byte
}

// dataServiceLockReaders are, by module digest, the titles
// lgt.data_service_lock answers "1". Each asks for the setting where its
// network scene starts. With "0" it dials, and its dial callback only prints
// when the dial fails: the title stays on its connecting screen with every key
// ignored, whatever failure the dial reports. With "1" it makes no dial, shows
// the notice it has for a locked handset and goes on at OK. A digest the fix
// selects without a reader here fails loading rather than run with an answer
// nothing was examined for.
// See docs/network.md#lgt-data-service-lock.
var dataServiceLockReaders = map[string]dataServiceLockReader{
	"1b3e570e1fd0d93b5cd160acb9f21c63ee1ec97ce48b392fba033a353b4615c4": {
		// The scene's initializer: DS_LOCK, then ROAMING_AREA, then
		// AIRPLANE_MODE through one helper, and state 9 for a nonzero answer.
		at: 0x3461e, expect: []byte{0x37, 0x48, 0x1e, 0x60, 0xff, 0xf7, 0x2b, 0xfe, 0x20, 0x60, 0x00, 0x28, 0x03, 0xd1, 0x34, 0x48,
			0xff, 0xf7, 0x25, 0xfe, 0x20, 0x60, 0x23, 0x68, 0x00, 0x2b, 0x03, 0xd1, 0x32, 0x48, 0xff, 0xf7, 0x1e, 0xfe, 0x20, 0x60,
			0x23, 0x68, 0x00, 0x2b, 0x01, 0xd0, 0x09, 0x22, 0x25, 0xe0},
	},
}

// selectDataServiceLock records whether this client's module is answered a
// locked data service, after checking the code that reads the answer. Nothing
// in the guest is changed.
func (client *Client) selectDataServiceLock() error {
	if !client.revision.Has(compatibility.LGTDataServiceLock) {
		return nil
	}
	digest := client.revision.Digest()
	reader, ok := dataServiceLockReaders[digest]
	if !ok {
		return fmt.Errorf("LGT data service lock compatibility: no reader is known for module %s", digest)
	}
	code := make([]byte, len(reader.expect))
	if err := client.core.Memory().Read(reader.at, code); err != nil {
		return fmt.Errorf("LGT data service lock compatibility: %w", err)
	}
	if !bytes.Equal(code, reader.expect) {
		return fmt.Errorf("LGT data service lock compatibility: unexpected reader at %#x", reader.at)
	}
	client.dataServiceLock = true
	return nil
}

package lgt

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// certificateMessageRevision is one recognized native revision of the
// exchange: the fields its client writes that the session does not supply, and
// the callbacks and endpoint its socket code passes. The two local revisions
// share the frame layout and differ only in these.
type certificateMessageRevision struct {
	// version fills the 20-byte client field of the first request.
	version string
	// code is bytes 6 and 7 of the second request, and product the field that
	// follows the model and its one-byte flag there. One revision sends no
	// product.
	code    [2]byte
	product string
	dial    uint32
	socket  uint32
	address uint32
	port    uint16
}

// certificateMessageRevisions are keyed by the SHA-256 of the native module
// whose client they describe. The digest names the code exactly, so the
// descriptor's identifier, and how an archive spells it, select nothing; the
// compatibility registry must still name the same digest before a session uses
// a revision.
var certificateMessageRevisions = map[string]certificateMessageRevision{
	"e73ff5c0e4ea82d53291faec14bc9a33ac9d85bc95124f6058d523ee5621fea1": {
		version: "V.1.0.1", code: [2]byte{4, 0},
		dial: 0x2d5c1, socket: 0x2d471, address: 0xfa4273d3, port: 0x1d3b,
	},
	"c3abb052c4eda11bba233e1bf21641fe04c5fb276b8fda9af4f581a1d7b1be8c": {
		version: "LGT_WIPIC_1.0.0", code: [2]byte{4, 0x29}, product: "0003060B",
		dial: 0x1ddf5, socket: 0x1e1c5, address: 0xfc4273d3, port: 0x223b,
	},
}

func newCertificateMessageNetwork(archive *Archive, identity, model string) *notificationNetwork {
	if archive == nil {
		return nil
	}
	digest := sha256.Sum256(archive.Module)
	module := hex.EncodeToString(digest[:])
	revision, known := certificateMessageRevisions[module]
	if !known || !compatibility.HasFix("lgt", compatibility.NativeModule, module, compatibility.LGTCertificateMessage) {
		return nil
	}
	return certificateMessageNetwork(revision, identity, model)
}

// The recognized private protocol first identifies the application/subscriber,
// then submits handset information. A nonempty informational reply completes
// its certificate gate. No remote account or carrier certificate is created.
func certificateMessageNetwork(revision certificateMessageRevision, identity, model string) *notificationNetwork {
	if wipic.ValidateSubscriberNumber(identity) != nil || model == "" || len(model) >= 50 || bytes.IndexByte([]byte(model), 0) >= 0 {
		return nil
	}
	if revision.version == "" || len(revision.version) >= 20 || len(revision.product) >= 20 {
		return nil
	}
	n := &notificationNetwork{contract: notificationContract{
		dials: [2]uint32{revision.dial, revision.dial}, socketCallback: revision.socket,
		address: revision.address, port: revision.port, protocol: localCertificateMessageProtocol,
	}}
	n.certificateRequests[0] = make([]byte, 73)
	copy(n.certificateRequests[0], []byte{0, 73, 0, 0, 0, 3, 0xf9, 0, 0, 2, 5})
	copy(n.certificateRequests[0][11:31], revision.version)
	copy(n.certificateRequests[0][31:43], identity)
	n.certificateRequests[1] = make([]byte, 79)
	copy(n.certificateRequests[1], []byte{0, 79, 0, 20, 0, 12, revision.code[0], revision.code[1]})
	copy(n.certificateRequests[1][8:58], model)
	copy(n.certificateRequests[1][59:79], revision.product)
	return n
}

func (n *notificationNetwork) writeCertificateMessageRequest(s *notificationSocketState, data []byte) bool {
	if s.failed || s.stage >= 2 || len(s.response) != 0 || len(s.pendingResponse) != 0 {
		return false
	}
	expected := n.certificateRequests[s.stage]
	if len(expected) == 0 || len(data) > len(expected)-len(s.request) {
		return false
	}
	s.request = append(s.request, data...)
	if !bytes.HasPrefix(expected, s.request) {
		return false
	}
	if len(s.request) != len(expected) {
		return true
	}
	s.request = nil
	if s.stage == 0 {
		s.pendingResponse = []byte{0, 5, 0, 0, 0}
	} else {
		message := []byte(localNotificationMessage)
		response := make([]byte, 8+len(message))
		binary.BigEndian.PutUint16(response, uint16(len(response)))
		response[3] = 20
		binary.BigEndian.PutUint16(response[6:], uint16(len(message)))
		copy(response[8:], message)
		s.pendingResponse = response
	}
	// Delivery at the next service boundary avoids reentering the guest's
	// response parser before its send routine finishes changing UI state.
	s.stage++
	return true
}

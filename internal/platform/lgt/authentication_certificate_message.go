package lgt

import (
	"bytes"
	"encoding/binary"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
	"github.com/movingwoo/wfeature/internal/wipic"
)

type certificateMessageLayout uint8

const (
	certificateStandard certificateMessageLayout = iota
	certificateModelOnly
	certificateExtendedHello
)

const certificateExtendedHelloSize = 108

// certificateMessageRevision is one recognized native revision of the
// exchange: the fields its client writes that the session does not supply, and
// the callbacks and endpoint its socket code passes.
type certificateMessageRevision struct {
	// version fills the 20-byte client field of the first request.
	version               string
	layout                certificateMessageLayout
	helloCode             [2]byte
	unassignedCertificate string
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
	"12d776585da56713c33860ffd9404a71a3d0d8a660984d129e8e8f438b037055": {
		version: "1.0.2", layout: certificateModelOnly, code: [2]byte{3, 0xf7},
		dial: 0x2d791, socket: 0x2d641, address: 0xfa4273d3, port: 0x1d3b,
	},
	"ca3f14e043fcd1659b894144d31053042d7f8f0f8d2a4c07b3aff8ab8879f8e4": {
		version: "1.0.2", layout: certificateExtendedHello, code: [2]byte{4, 0x12}, product: "0002CB6A",
		dial: 0x2dc81, socket: 0x2db21, address: 0xfc4273d3, port: 0x1d3b,
	},
	"f11086c249b305152625674c4accba4fd705d59ade956824b4c45ea3cea7576f": {
		version: "1.0.1", helloCode: [2]byte{4, 0x2c}, code: [2]byte{4, 0x2c}, product: "01042875",
		unassignedCertificate: "a08305330a2cfe2aa7a74782ff4cecdfe0030d9388791bf27f6244dc3ea91d02",
		dial:                  0x1d11d, socket: 0x1d4f5, address: 0xfc4273d3, port: 0x223b,
	},
}

func newCertificateMessageNetwork(module compatibility.NativeRevision, identity, model string) *notificationNetwork {
	revision, known := certificateMessageRevisions[module.Digest()]
	if !known || !module.Has(compatibility.LGTCertificateMessage) {
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
	helloSize, modelSize := 73, 79
	if revision.layout == certificateExtendedHello {
		helloSize = certificateExtendedHelloSize
	}
	if revision.layout == certificateModelOnly {
		modelSize = 58
	}
	hello, handset := make([]byte, helloSize), make([]byte, modelSize)
	copy(hello, []byte{0, byte(helloSize), 0, 0, 0, 3, 0xf9, revision.helloCode[0], revision.helloCode[1], 2, 5})
	copy(hello[11:31], revision.version)
	copy(hello[31:43], identity)
	if revision.layout == certificateModelOnly || revision.layout == certificateExtendedHello {
		for i := 47; i < 73; i++ {
			hello[i] = '0'
		}
	}
	copy(handset, []byte{0, byte(modelSize), 0, 20, 0, 12, revision.code[0], revision.code[1]})
	copy(handset[8:58], model)
	if revision.layout == certificateModelOnly {
		hello[4], handset[4], handset[5] = '0', '0', 10
	} else {
		copy(handset[59:79], revision.product)
	}
	n.unassignedCertificate = revision.unassignedCertificate
	n.certificateRequests = [2][]byte{hello, handset}
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

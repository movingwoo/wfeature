package lgt

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
	"github.com/movingwoo/wfeature/internal/wipic"
)

func newCertificateMessageNetwork(archive *Archive, identity, model string) *notificationNetwork {
	if archive == nil || archive.Descriptor.AID != "00028E76" {
		return nil
	}
	digest := sha256.Sum256(archive.Module)
	if !compatibility.HasFix("lgt", compatibility.NativeModule, hex.EncodeToString(digest[:]), compatibility.LGTCertificateMessage) {
		return nil
	}
	return certificateMessageNetwork(identity, model)
}

// The recognized private protocol first identifies the application/subscriber,
// then submits handset information. A nonempty informational reply completes
// its certificate gate. No remote account or carrier certificate is created.
func certificateMessageNetwork(identity, model string) *notificationNetwork {
	if wipic.ValidateSubscriberNumber(identity) != nil || model == "" || len(model) >= 50 || bytes.IndexByte([]byte(model), 0) >= 0 {
		return nil
	}
	n := &notificationNetwork{contract: notificationContract{
		dials: [2]uint32{0x2d5c1, 0x2d5c1}, socketCallback: 0x2d471,
		address: 0xfa4273d3, port: 0x1d3b, protocol: localCertificateMessageProtocol,
	}}
	n.certificateRequests[0] = make([]byte, 73)
	copy(n.certificateRequests[0], []byte{0, 73, 0, 0, 0, 3, 0xf9, 0, 0, 2, 5})
	copy(n.certificateRequests[0][11:31], "V.1.0.1")
	copy(n.certificateRequests[0][31:43], identity)
	n.certificateRequests[1] = make([]byte, 79)
	copy(n.certificateRequests[1], []byte{0, 79, 0, 20, 0, 12, 4, 0})
	copy(n.certificateRequests[1][8:58], model)
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

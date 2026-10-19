package lgt

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// The native modules of the two local revisions, as the registry names them.
const (
	firstCertificateMessageModule  = "e73ff5c0e4ea82d53291faec14bc9a33ac9d85bc95124f6058d523ee5621fea1"
	secondCertificateMessageModule = "c3abb052c4eda11bba233e1bf21641fe04c5fb276b8fda9af4f581a1d7b1be8c"
)

func TestCertificateMessageSequenceAndFragments(t *testing.T) {
	for _, module := range []string{firstCertificateMessageModule, secondCertificateMessageModule} {
		t.Run(module[:8], func(t *testing.T) {
			testCertificateMessageSequenceAndFragments(t, certificateMessageNetwork(certificateMessageRevisions[module], "01012345678", "Fixture"))
		})
	}
	revision := certificateMessageRevisions[firstCertificateMessageModule]
	if certificateMessageNetwork(revision, "invalid", "Fixture") != nil || certificateMessageNetwork(revision, "01012345678", "") != nil || certificateMessageNetwork(revision, "01012345678", strings.Repeat("x", 50)) != nil || certificateMessageNetwork(revision, "01012345678", "a\x00b") != nil {
		t.Fatal("invalid session identity accepted")
	}
	if newCertificateMessageNetwork(&Archive{Descriptor: Descriptor{AID: "00028E76"}, Module: []byte("other revision")}, "01012345678", "Fixture") != nil {
		t.Fatal("unknown module matched")
	}
	if newCertificateMessageNetwork(nil, "01012345678", "Fixture") != nil {
		t.Fatal("unknown archive matched")
	}
	for module := range certificateMessageRevisions {
		if !compatibility.HasFix("lgt", compatibility.NativeModule, module, compatibility.LGTCertificateMessage) {
			t.Fatalf("revision %s is not registered", module)
		}
	}
	for _, changed := range []certificateMessageRevision{
		{code: revision.code, dial: revision.dial},
		{version: strings.Repeat("v", 20), dial: revision.dial},
		{version: revision.version, product: strings.Repeat("p", 20)},
	} {
		if certificateMessageNetwork(changed, "01012345678", "Fixture") != nil {
			t.Fatalf("revision %+v with an unbounded field accepted", changed)
		}
	}
}

// Each stage's request is accepted in any two fragments and answered once, and
// any changed, extended or reordered request is refused.
func testCertificateMessageSequenceAndFragments(t *testing.T, n *notificationNetwork) {
	t.Helper()
	if n == nil {
		t.Fatal("revision rejected")
	}
	for stage := 0; stage < 2; stage++ {
		expected := n.certificateRequests[stage]
		for split := 1; split < len(expected); split++ {
			s := &notificationSocketState{stage: uint8(stage)}
			if !n.writeRequest(s, expected[:split]) || len(s.pendingResponse) != 0 || !n.writeRequest(s, expected[split:]) {
				t.Fatalf("stage %d split %d rejected", stage, split)
			}
			if len(s.response) != 0 || len(s.pendingResponse) == 0 {
				t.Fatal("response delivered inline or missing")
			}
			if n.writeRequest(s, expected) {
				t.Fatal("pending reply overwritten")
			}
			if stage == 0 {
				if !bytes.Equal(s.pendingResponse, []byte{0, 5, 0, 0, 0}) {
					t.Fatal("handshake reply changed")
				}
			} else {
				b := s.pendingResponse
				if int(binary.BigEndian.Uint16(b)) != len(b) || b[3] != 20 || b[5] != 0 || int(binary.BigEndian.Uint16(b[6:])) != len(b)-8 || string(b[8:]) != localNotificationMessage {
					t.Fatal("invalid certificate response")
				}
				s.pendingResponse = nil
				if n.writeRequest(s, n.certificateRequests[0]) {
					t.Fatal("completed socket restarted protocol")
				}
			}
		}
		for i := range expected {
			changed := bytes.Clone(expected)
			changed[i] ^= 0x80
			s := &notificationSocketState{stage: uint8(stage)}
			if n.writeRequest(s, changed) || len(s.pendingResponse) != 0 {
				t.Fatalf("stage %d changed byte %d accepted", stage, i)
			}
		}
		for _, request := range [][]byte{append(bytes.Clone(expected), 0), bytes.Repeat([]byte{'x'}, 101), n.certificateRequests[1-stage]} {
			if n.writeRequest(&notificationSocketState{stage: uint8(stage)}, request) {
				t.Fatal("trailing, oversized or reordered request accepted")
			}
		}
	}
}

// Each revision's two requests, as its client wrote them for this session
// identity and model, and the socket contract its code passes. The bytes are
// fixed here rather than derived from the revision table: the client field,
// the code word and the product field after the model are what separate the
// two revisions.
func TestCertificateMessageRevisionRequests(t *testing.T) {
	for _, revision := range []struct {
		module   string
		observed [2]string
		contract notificationContract
	}{
		{
			module: firstCertificateMessageModule,
			observed: [2]string{
				"004900000003f900000205562e312e302e3100000000000000000000000000303130303030303030303000" +
					"000000000000000000000000000000000000000000000000000000000000",
				"004f0014000c0400456d756c61746f72000000000000000000000000000000000000000000000000000000" +
					"000000000000000000000000000000000000000000000000000000000000000000000000",
			},
			contract: notificationContract{dials: [2]uint32{0x2d5c1, 0x2d5c1}, socketCallback: 0x2d471, address: 0xfa4273d3, port: 0x1d3b},
		},
		{
			module: secondCertificateMessageModule,
			observed: [2]string{
				"004900000003f9000002054c47545f57495049435f312e302e300000000000303130303030303030303000" +
					"000000000000000000000000000000000000000000000000000000000000",
				"004f0014000c0429456d756c61746f72000000000000000000000000000000000000000000000000000000" +
					"000000000000000000000000000000003030303330363042000000000000000000000000",
			},
			contract: notificationContract{dials: [2]uint32{0x1ddf5, 0x1ddf5}, socketCallback: 0x1e1c5, address: 0xfc4273d3, port: 0x223b},
		},
	} {
		n := certificateMessageNetwork(certificateMessageRevisions[revision.module], "01000000000", "Emulator")
		if n == nil {
			t.Fatalf("revision %s rejected", revision.module)
		}
		for stage, want := range revision.observed {
			if got := hex.EncodeToString(n.certificateRequests[stage]); got != want {
				t.Fatalf("revision %s stage %d request\n got %s\nwant %s", revision.module, stage, got, want)
			}
		}
		revision.contract.protocol = localCertificateMessageProtocol
		if n.contract != revision.contract {
			t.Fatalf("revision %s socket contract %+v", revision.module, n.contract)
		}
	}
}

func TestCertificateMessageSocketDeferredDeliveryAndClose(t *testing.T) {
	c := fixtureClient(t)
	n := certificateMessageNetwork(certificateMessageRevisions[firstCertificateMessageModule], "01012345678", "Fixture")
	n.active = true
	c.notificationNetwork = n
	cb := notificationReportingCallback(t, c)
	n.contract.socketCallback = cb
	output, err := c.allocate(16)
	if err != nil {
		t.Fatal(err)
	}
	fd := callSlot(t, c, slotNetSocketStandard, 2, 1)
	if notificationConnectSlot(t, c, fd, cb, output) != 0 {
		t.Fatal("connect rejected")
	}
	notificationService(t, c)
	buffer, err := c.allocate(100)
	if err != nil {
		t.Fatal(err)
	}
	for stage, request := range n.certificateRequests {
		if err := c.writeWord(output+8, 0); err != nil {
			t.Fatal(err)
		}
		address, err := c.allocateBytes(request)
		if err != nil {
			t.Fatal(err)
		}
		callSlot(t, c, slotNetSetReadCB, fd, cb, output)
		if got := callSlot(t, c, slotNetSocketWrite, fd, address, uint32(len(request))); got != uint32(len(request)) {
			t.Fatal("write rejected")
		}
		if got := int32(callSlot(t, c, slotNetSocketRead, fd, buffer, 2)); got != -19 {
			t.Fatalf("inline read=%d", got)
		}
		notificationService(t, c)
		if v, _ := c.readWord(output + 8); v != 1 {
			t.Fatal("read callback missing")
		}
		if got := callSlot(t, c, slotNetSocketRead, fd, buffer, 2); got != 2 {
			t.Fatal("header read failed")
		}
		var header [2]byte
		c.core.Memory().Read(buffer, header[:])
		size := uint32(binary.BigEndian.Uint16(header[:]))
		if size < 5 || size > 100 || callSlot(t, c, slotNetSocketRead, fd, buffer+2, size-2) != size-2 {
			t.Fatal("body read failed")
		}
		if stage == 1 {
			var reply [100]byte
			c.core.Memory().Read(buffer, reply[:size])
			if string(reply[8:size]) != localNotificationMessage {
				t.Fatal("message lost")
			}
		}
	}
	// A queued response cannot revive a closed socket or call its old callback.
	callSlot(t, c, slotNetSocketClose, fd)
	fd = callSlot(t, c, slotNetSocketStandard, 2, 1)
	if notificationConnectSlot(t, c, fd, cb, output) != 0 {
		t.Fatal("second connect rejected")
	}
	notificationService(t, c)
	c.writeWord(output+8, 0)
	request, _ := c.allocateBytes(n.certificateRequests[0])
	callSlot(t, c, slotNetSetReadCB, fd, cb, output)
	callSlot(t, c, slotNetSocketWrite, fd, request, uint32(len(n.certificateRequests[0])))
	callSlot(t, c, slotNetSocketClose, fd)
	notificationService(t, c)
	if v, _ := c.readWord(output + 8); v != 0 {
		t.Fatal("closed socket callback fired")
	}
}

func TestLocalCertificateMessageSelection(t *testing.T) {
	path := os.Getenv("WFEATURE_LGT_CERTIFICATE_MESSAGE_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_LGT_CERTIFICATE_MESSAGE_ARCHIVE")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	for _, disabled := range []bool{false, true} {
		s, err := StartSession(context.Background(), data, SessionOptions{DisableAuthentication: disabled, SaveRoot: t.TempDir(), Width: 240, Height: 320})
		if err != nil {
			t.Fatal(err)
		}
		status := backend.AuthenticationLGTCertificateMessage
		if disabled {
			status = backend.AuthenticationOff
		}
		if s.Authentication() != status || (s.client.notificationNetwork == nil) != disabled {
			t.Fatal("incorrect session selection")
		}
		if err := s.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(data, original) {
		t.Fatal("original archive changed")
	}
}

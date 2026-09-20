package lgt

import (
	"bytes"
	"strings"
	"testing"
)

func TestTerminatedNotificationChoiceAndFragments(t *testing.T) {
	n := notificationTestNetwork()
	n.contract.protocol = localTerminatedNotificationProtocol
	for i, choice := range []string{"N", "Y"} {
		request := []byte("SMSAGREE " + n.identity + " " + n.contract.application + " " + choice + "\x00")
		for split := 1; split < len(request); split++ {
			s := &notificationSocketState{}
			if !n.writeRequest(s, request[:split]) || len(s.response) != 0 {
				t.Fatalf("choice %s split %d completed before terminator", choice, split)
			}
			if !n.writeRequest(s, request[split:]) || !bytes.Equal(s.response, append([]byte{byte(i + 2), byte(len(localNotificationMessage))}, localNotificationMessage...)) {
				t.Fatalf("choice %s split %d reply = %x", choice, split, s.response)
			}
			s.response = nil
			if n.writeRequest(s, request) {
				t.Fatal("completed connection accepted another notification")
			}
		}
	}
}

func TestTerminatedNotificationRejectsMismatchedFramingAndIdentity(t *testing.T) {
	n := notificationTestNetwork()
	n.contract.protocol = localTerminatedNotificationProtocol
	valid := "SMSAGREE " + n.identity + " " + n.contract.application + " N\x00"
	for _, request := range []string{
		valid + "\x00", valid + "extra", strings.TrimSuffix(valid, "\x00") + "X\x00",
		strings.Replace(valid, n.identity, "01000000000", 1),
		strings.Replace(valid, n.contract.application, "OTHER_APP", 1),
		"IS_SAVEDATA_EXIST " + n.identity + " " + n.contract.application + "\x00",
		strings.Repeat("x", 65),
	} {
		s := &notificationSocketState{}
		if n.writeRequest(s, []byte(request)) || len(s.response) != 0 {
			t.Fatalf("unsupported request accepted: %q", request)
		}
	}
	for _, s := range []*notificationSocketState{{failed: true}, {stage: 1}, {response: []byte{2, 0}}} {
		if n.writeRequest(s, []byte(valid)) {
			t.Fatal("unavailable socket accepted request")
		}
	}
	n.identity = strings.Repeat("1", 64)
	if n.writeRequest(&notificationSocketState{}, []byte("SMSAGREE ")) {
		t.Fatal("oversized expected command accepted")
	}
}

func TestTerminatedNotificationSocketShortReads(t *testing.T) {
	c := fixtureClient(t)
	n := notificationTestNetwork()
	n.contract.protocol = localTerminatedNotificationProtocol
	c.notificationNetwork = n
	cb := notificationReportingCallback(t, c)
	n.contract.socketCallback = cb
	out, err := c.allocate(16)
	if err != nil {
		t.Fatal(err)
	}
	fd := callSlot(t, c, slotNetSocketStandard, 2, 1)
	if notificationConnectSlot(t, c, fd, cb, out) != 0 {
		t.Fatal("connect rejected")
	}
	notificationService(t, c)
	request := []byte("SMSAGREE " + n.identity + " " + n.contract.application + " N\x00")
	address, err := c.allocateBytes(request)
	if err != nil {
		t.Fatal(err)
	}
	if got := callSlot(t, c, slotNetSocketWrite, fd, address, uint32(len(request)-1)); got != uint32(len(request)-1) {
		t.Fatalf("partial write = %d", got)
	}
	if got := int32(callSlot(t, c, slotNetSocketRead, fd, out, 2)); got != -19 {
		t.Fatalf("unterminated read = %d", got)
	}
	if got := callSlot(t, c, slotNetSocketWrite, fd, address+uint32(len(request)-1), 1); got != 1 {
		t.Fatalf("terminator write = %d", got)
	}
	if callSlot(t, c, slotNetSocketRead, fd, out, 1) != 1 || callSlot(t, c, slotNetSocketRead, fd, out+1, 1) != 1 {
		t.Fatal("short response reads lost bytes")
	}
	var response [2]byte
	if err := c.core.Memory().Read(out, response[:]); err != nil {
		t.Fatal(err)
	}
	if response != [2]byte{2, byte(len(localNotificationMessage))} {
		t.Fatalf("guest response = %x", response)
	}
	body, err := c.allocate(uint64(len(localNotificationMessage)))
	if err != nil {
		t.Fatal(err)
	}
	if got := callSlot(t, c, slotNetSocketRead, fd, body, uint32(len(localNotificationMessage)+8)); got != uint32(len(localNotificationMessage)) {
		t.Fatalf("body read = %d", got)
	}
	text := make([]byte, len(localNotificationMessage))
	if err := c.core.Memory().Read(body, text); err != nil {
		t.Fatal(err)
	}
	if string(text) != localNotificationMessage {
		t.Fatalf("body = %q", text)
	}
	if got := int32(callSlot(t, c, slotNetSocketWrite, fd, address, uint32(len(request)))); got != wipiError {
		t.Fatalf("repeated request = %d", got)
	}
	if got := int32(callSlot(t, c, slotNetSocketRead, fd, out, 2)); got != wipiError {
		t.Fatalf("failed connection read = %d", got)
	}
}

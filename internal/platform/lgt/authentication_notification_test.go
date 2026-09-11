package lgt

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func notificationTestNetwork() *notificationNetwork {
	return &notificationNetwork{identity: "01012345678", contract: notificationContract{application: "APP_TEST", address: 0x0100007f, port: 0x3412}, active: true}
}

func TestNotificationProtocolPreservesChoiceAndRejectsOtherOperations(t *testing.T) {
	n := notificationTestNetwork()
	for _, choice := range []byte{'N', 'Y'} {
		request := make([]byte, 100)
		copy(request, "SMSAGREE 01012345678 APP_TEST "+string(choice))
		for split := 1; split < 100; split++ {
			s := &notificationSocketState{}
			if !n.writeRequest(s, request[:split]) || len(s.response) != 0 || !n.writeRequest(s, request[split:]) {
				t.Fatalf("fragmented choice %c at %d failed", choice, split)
			}
			want := byte(2)
			if choice == 'Y' {
				want = 3
			}
			if len(s.response) != 100 || s.response[0] != want || !bytes.Equal(s.response[1:], make([]byte, 99)) {
				t.Fatalf("choice %c response = %x", choice, s.response)
			}
		}
	}
	for _, request := range []string{
		"SMSAGREE 01012345678 APP_TEST X", "SMSAGREE 01012345679 APP_TEST N", "SMSAGREE 01012345678 APP_OTHER N",
		"IS_SAVEDATA_EXIST 01012345678 APP_TEST extra", "FINISH_SAVEDATA 01012345678 APP_TEST",
		"START_SAVEDATA_UP 01012345678 APP_TEST", "DELETE_SAVEDATA 01012345678 APP_TEST", strings.Repeat("x", 101),
	} {
		s := &notificationSocketState{}
		if n.writeRequest(s, []byte(request)) || len(s.response) != 0 {
			t.Fatalf("unsupported request accepted: %q", request)
		}
	}
}

func TestNotificationEmptySaveSequence(t *testing.T) {
	n := notificationTestNetwork()
	s := &notificationSocketState{}
	if !n.writeRequest(s, []byte("IS_SAVEDATA_EXIST 01012345678 APP_TEST")) || !bytes.Equal(s.response, []byte{2, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("empty lookup = %x", s.response)
	}
	if n.writeRequest(s, []byte("FINISH_SAVEDATA 01012345678 APP_TEST")) {
		t.Fatal("unread reply overwritten")
	}
	s.response = nil
	if !n.writeRequest(s, []byte("FINISH_SAVEDATA 01012345678 APP_TEST")) || !bytes.Equal(s.response, []byte{7, 0, 0}) {
		t.Fatalf("finish = %x", s.response)
	}
	s.response = nil
	if n.writeRequest(s, []byte("IS_SAVEDATA_EXIST 01012345678 APP_TEST")) {
		t.Fatal("finished connection reused")
	}
}

// This callback records all three WIPI arguments, proving the actual guest ABI.
func notificationReportingCallback(t *testing.T, c *Client) uint32 {
	return guestThumbStub(t, c, 0x60516010, 0x60932301, 0x00004770) // str r0,[r2]; str r1,[r2,#4]; mov r3,#1; str r3,[r2,#8]; bx lr
}
func notificationConnectSlot(t *testing.T, c *Client, fd, cb, param uint32) int32 {
	t.Helper()
	stack, err := c.allocateWords([]uint32{param})
	if err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.NewContext())
	for i, v := range []uint32{fd, c.notificationNetwork.contract.address, uint32(c.notificationNetwork.contract.port), cb} {
		if err := thread.SetRegister(i, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
		t.Fatal(err)
	}
	if err := c.handleWIPICSVC(context.Background(), thread, slotNetSocketConnect); err != nil {
		t.Fatal(err)
	}
	result, _ := thread.Register(0)
	return int32(result)
}
func notificationService(t *testing.T, c *Client) {
	t.Helper()
	if err := c.serviceNetConnects(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationSocketGuestCallbacksReadBoundsAndCancellation(t *testing.T) {
	c := fixtureClient(t)
	n := notificationTestNetwork()
	c.notificationNetwork = n
	cb := notificationReportingCallback(t, c)
	n.contract.socketCallback = cb
	out, err := c.allocate(16)
	if err != nil {
		t.Fatal(err)
	}
	fd := callSlot(t, c, slotNetSocketStandard, 2, 1)
	if notificationConnectSlot(t, c, fd, cb+2, out) != wipiError {
		t.Fatal("unrecognized connect callback accepted")
	}
	if notificationConnectSlot(t, c, fd, cb, out) != 0 {
		t.Fatal("recognized connect rejected")
	}
	if v, _ := c.readWord(out + 8); v != 0 {
		t.Fatal("inline connect callback")
	}
	notificationService(t, c)
	if v, _ := c.readWord(out); v != fd {
		t.Fatalf("callback fd=%d, want %d", v, fd)
	}
	if v, _ := c.readWord(out + 4); v != 0 {
		t.Fatalf("callback error=%d", v)
	}
	if v, _ := c.readWord(out + 8); v != 1 {
		t.Fatal("callback parameter missing")
	}
	c.writeWord(out+8, 0)
	notificationService(t, c)
	if v, _ := c.readWord(out + 8); v != 0 {
		t.Fatal("connect callback repeated")
	}
	request, _ := c.allocateBytes([]byte("IS_SAVEDATA_EXIST 01012345678 APP_TEST"))
	buffer, _ := c.allocate(8)
	if int32(callSlot(t, c, slotNetSocketRead, fd, buffer, 8)) != -19 {
		t.Fatal("empty stream did not report would-block")
	}
	callSlot(t, c, slotNetSetReadCB, fd, cb, out)
	if result := callSlot(t, c, slotNetSocketWrite, fd, request, uint32(len("IS_SAVEDATA_EXIST 01012345678 APP_TEST"))); result != uint32(len("IS_SAVEDATA_EXIST 01012345678 APP_TEST")) {
		t.Fatalf("write=%d", result)
	}
	notificationService(t, c)
	if v, _ := c.readWord(out + 8); v != 1 {
		t.Fatal("read-ready callback missing")
	}
	c.writeWord(out+8, 0)
	notificationService(t, c)
	if v, _ := c.readWord(out + 8); v != 0 {
		t.Fatal("read-ready callback repeated")
	}
	if int32(callSlot(t, c, slotNetSocketRead, fd, math.MaxUint32-2, 4)) != wipiError {
		t.Fatal("invalid memory accepted")
	}
	if callSlot(t, c, slotNetSocketRead, fd, buffer, 3) != 3 || callSlot(t, c, slotNetSocketRead, fd, buffer+3, 5) != 5 {
		t.Fatal("short reads lost data")
	}
	b := make([]byte, 8)
	c.core.Memory().Read(buffer, b)
	if !bytes.Equal(b, []byte{2, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("guest read=%x", b)
	}
	callSlot(t, c, slotNetSetWriteCB, fd, cb, out)
	callSlot(t, c, slotNetSocketClose, fd)
	notificationService(t, c)
	if v, _ := c.readWord(out + 8); v != 0 {
		t.Fatal("closed socket callback fired")
	}
	fd = callSlot(t, c, slotNetSocketStandard, 2, 1)
	notificationConnectSlot(t, c, fd, cb, out)
	callSlot(t, c, slotNetClose)
	notificationService(t, c)
	if v, _ := c.readWord(out + 8); v != 0 {
		t.Fatal("network close retained callback")
	}
	if int32(callSlot(t, c, slotNetSocketStandard, 2, 1)) != wipiError {
		t.Fatal("closed network still active")
	}
}

func TestNotificationDialIsScopedAndBounded(t *testing.T) {
	c := fixtureClient(t)
	n := notificationTestNetwork()
	n.active = false
	c.notificationNetwork = n
	out, _ := c.allocate(4)
	allowed := guestThumbStub(t, c, reportingCallback)
	other := guestThumbStub(t, c, reportingCallback)
	n.contract.dials = [2]uint32{allowed, allowed}
	if int32(callSlot(t, c, slotNetSocketStandard, 2, 1)) != wipiError {
		t.Fatal("socket created before local dial")
	}
	callSlot(t, c, slotNetConnect, other, out)
	c.clock.advance(time.Second)
	notificationService(t, c)
	if v, _ := c.readWord(out); int32(v) != wipiError || n.active {
		t.Fatal("unrecognized dial succeeded")
	}
	callSlot(t, c, slotNetConnect, allowed, out)
	c.clock.advance(time.Second)
	notificationService(t, c)
	if v, _ := c.readWord(out); v != 0 || !n.active {
		t.Fatal("recognized dial failed")
	}
	for i := 0; i < 64; i++ {
		if callSlot(t, c, slotNetConnect, allowed, out) != 0 {
			t.Fatalf("dial %d rejected", i)
		}
	}
	if int32(callSlot(t, c, slotNetConnect, allowed, out)) != wipiError || len(c.netConnects) != 64 {
		t.Fatal("dial queue unbounded")
	}
	for i := 0; i < 4; i++ {
		if int32(callSlot(t, c, slotNetSocketStandard, 2, 1)) < 0 {
			t.Fatal("bounded socket rejected")
		}
	}
	if int32(callSlot(t, c, slotNetSocketStandard, 2, 1)) != wipiError {
		t.Fatal("socket limit ignored")
	}
	callSlot(t, c, slotNetClose)
	c.clock.advance(time.Second)
	notificationService(t, c)
	if n.active || len(n.sockets) != 0 || len(c.netConnects) != 0 {
		t.Fatal("network close did not cancel pending work")
	}
}

// Assemble independent literal pools, addresses and function links. The fixture
// contains no archive bytes, original resource or service identity.
func notificationContractFixture(t *testing.T, delta uint32) (*Module, []byte, []byte) {
	t.Helper()
	code, data := make([]byte, 0x4000), make([]byte, 0x400)
	textBase, dataBase := uint32(0x100000)+delta, uint32(0x200000)+delta
	module := &Module{Sections: []Section{{Address: textBase, Size: uint32(len(code)), Data: code, Executable: true}, {Address: dataBase, Size: uint32(len(data)), Data: data}}}
	call := func(offset, target uint32) {
		d := target - (textBase + offset) - 4
		binary.LittleEndian.PutUint16(code[offset:], 0xf000|uint16(d>>12)&0x7ff)
		binary.LittleEndian.PutUint16(code[offset+2:], 0xf800|uint16(d>>1)&0x7ff)
	}
	lit := func(offset, value uint32) {
		word := binary.LittleEndian.Uint16(code[offset:])
		pool := ((textBase + offset + 4) &^ 3) + uint32(word&255)*4
		binary.LittleEndian.PutUint32(code[pool-textBase:], value)
	}
	for _, part := range []struct {
		offset  uint32
		pattern []uint16
	}{{0x400, notificationWriter}, {0x800, notificationReply}, {0xc00, notificationDial}, {0x1000, notificationQueryDial}, {0x1400, notificationQuery}, {0x1800, notificationFinish}, {0x1c00, notificationQueryReply}, {0x2000, notificationSocket}} {
		for i := 0; i < len(part.pattern); i++ {
			w := part.pattern[i]
			off := part.offset + uint32(i*2)
			if w == 0 {
				call(off, textBase+0x3000)
				i++
				continue
			}
			binary.LittleEndian.PutUint16(code[off:], w)
			if w&0xf800 == 0x4800 {
				lit(off, dataBase+0x300)
			}
		}
	}
	for _, entry := range []struct {
		offset uint32
		text   string
	}{{0, "SMSAGREE"}, {0x20, "%s %s %s %s"}, {0x40, "Y"}, {0x50, "N"}, {0x60, "IS_SAVEDATA_EXIST"}, {0x80, "FINISH_SAVEDATA"}, {0xa0, "APP_TEST"}, {0xc0, "127.0.0.1"}} {
		copy(data[entry.offset:], entry.text+"\x00")
	}
	for _, entry := range [][2]uint32{{0x438, 0}, {0x436, 0x20}, {0x42a, 0x40}, {0x430, 0x50}, {0x1402, 0x60}, {0x1802, 0x80}, {0x43c, 0xa0}, {0xc64, 0xc0}, {0x100e, 0xc0}} {
		lit(entry[0], dataBase+entry[1])
	}
	lit(0x456, textBase+0x3001)
	lit(0xc18, textBase+0x7a1)
	lit(0xc1e, textBase+0x3001)
	lit(0xc6c, textBase+0x3001)
	lit(0xc66, 0x1234)
	lit(0x1010, 0x1234)
	lit(0x2036, textBase+0x3101)
	// The lookup receiver joins its parse branch and opcode dispatch table.
	receiver := uint32(0x2200)
	lit(0x1008, textBase+receiver+1)
	put := func(off uint32, words ...uint16) {
		for i, w := range words {
			binary.LittleEndian.PutUint16(code[off+uint32(i*2):], w)
		}
	}
	put(receiver+0x84, 0x2b02, 0xd100)
	branch := func(off, target uint32) { d := int32(target) - int32(off) - 4; put(off, 0xe000|uint16(d>>1)&0x7ff) }
	branch(receiver+0x88, 0x1c00)
	put(receiver+0xb2, 0x4a20)
	lit(receiver+0xb2, dataBase+0x200)
	put(receiver+0x170, 0x9b03, 0x2b00, 0xd068, 0x2100, 0x2002, 0xe76c)
	// The fixed dispatch tail is relocated with its receiver, preserving its branch.
	put(receiver+0xea, 0x9905, 0x2007, 0xe7b2)
	binary.LittleEndian.PutUint32(data[0x208:], textBase+receiver+0x170)
	binary.LittleEndian.PutUint32(data[0x21c:], textBase+receiver+0xea)
	put(0x3100, 0x4770)
	return module, code, data
}

func TestNotificationRecognitionRequiresConnectedContracts(t *testing.T) {
	for _, delta := range []uint32{0, 0x11000, 0x710000} {
		module, code, data := notificationContractFixture(t, delta)
		contract := authenticationNotification(module)
		if contract == nil || contract.application != "APP_TEST" || contract.address != 0x0100007f || contract.port != 0x3412 {
			t.Fatalf("relocated contract rejected at %#x: %+v", delta, contract)
		}
		for _, offset := range []int{0x400, 0x438, 0x800, 0xc00, 0xc18, 0x1000, 0x1408, 0x1812, 0x1c00, 0x2000, 0x2288, 0x22ea, 0x2378} {
			original := code[offset]
			code[offset] ^= 0x80
			if authenticationNotification(module) != nil {
				t.Fatalf("changed contract at %#x accepted", offset)
			}
			code[offset] = original
		}
		for _, offset := range []int{0, 0x40, 0x50, 0x60, 0x80, 0xc0, 0x208, 0x21c} {
			original := data[offset]
			data[offset] = 0xff
			if authenticationNotification(module) != nil {
				t.Fatalf("invalid data at %#x accepted", offset)
			}
			data[offset] = original
		}
	}
	if authenticationNotification(nil) != nil {
		t.Fatal("nil module recognized")
	}
}

func TestNotificationCloseInsideGuestCallbackCancelsRemainingBatch(t *testing.T) {
	c := fixtureClient(t)
	n := notificationTestNetwork()
	c.notificationNetwork = n
	closeCB, err := c.stub(svcCategoryWIPIC, slotNetClose)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := c.allocate(12)
	report := notificationReportingCallback(t, c)
	n.sockets = map[uint32]*notificationSocketState{
		101: {connected: true, response: []byte{2}, read: notificationCallback{address: closeCB}},
		102: {connected: true, response: []byte{2}, read: notificationCallback{address: report, param: out}},
	}
	notificationService(t, c)
	if v, _ := c.readWord(out + 8); v != 0 {
		t.Fatal("second callback fired after guest closed network")
	}
	dialReport := guestThumbStub(t, c, reportingCallback)
	n.contract.dials = [2]uint32{closeCB, dialReport}
	callSlot(t, c, slotNetConnect, closeCB, 0)
	callSlot(t, c, slotNetConnect, dialReport, out)
	c.clock.advance(time.Second)
	notificationService(t, c)
	if v, _ := c.readWord(out + 8); v != 0 || n.active {
		t.Fatal("canceled dial reactivated local network")
	}
}

func TestNotificationInvalidRequestFailsPendingReaderOnce(t *testing.T) {
	c := fixtureClient(t)
	n := notificationTestNetwork()
	c.notificationNetwork = n
	out, _ := c.allocate(12)
	cb := notificationReportingCallback(t, c)
	n.sockets = map[uint32]*notificationSocketState{101: {connected: true, read: notificationCallback{address: cb, param: out}}}
	request, _ := c.allocateBytes([]byte("START_SAVEDATA_UP 01012345678 APP_TEST"))
	if int32(callSlot(t, c, slotNetSocketWrite, 101, request, 35)) != wipiError {
		t.Fatal("upload accepted")
	}
	notificationService(t, c)
	if v, _ := c.readWord(out + 4); int32(v) != wipiError {
		t.Fatalf("failed request callback=%d", int32(v))
	}
	c.writeWord(out+8, 0)
	notificationService(t, c)
	if v, _ := c.readWord(out + 8); v != 0 {
		t.Fatal("failed request callback repeated")
	}
	if int32(callSlot(t, c, slotNetSocketRead, 101, out, 1)) != wipiError {
		t.Fatal("failed connection returned data")
	}
}

func TestNotificationOpposingClientsKeepChoicesAndPolicySeparate(t *testing.T) {
	a, b, off := fixtureClient(t), fixtureClient(t), fixtureClient(t)
	a.notificationNetwork = notificationTestNetwork()
	b.notificationNetwork = notificationTestNetwork()
	b.notificationNetwork.identity = "01087654321"
	for _, c := range []*Client{a, b} {
		c.notificationNetwork.sockets = map[uint32]*notificationSocketState{101: {connected: true}}
	}
	done := make(chan uint32, 2)
	for i, c := range []*Client{a, b} {
		choice := "Y"
		if i == 1 {
			choice = "N"
		}
		payload := make([]byte, 100)
		copy(payload, "SMSAGREE "+c.notificationNetwork.identity+" APP_TEST "+choice)
		address, err := c.allocateBytes(payload)
		if err != nil {
			t.Fatal(err)
		}
		go func(c *Client, address uint32) {
			result := uint32(math.MaxUint32)
			defer func() { done <- result }()
			result = callSlot(t, c, slotNetSocketWrite, 101, address, 100)
		}(c, address)
	}
	for i := 0; i < 2; i++ {
		if <-done != 100 {
			t.Fatal("independent notification rejected")
		}
	}
	if a.notificationNetwork.sockets[101].response[0] != 3 || b.notificationNetwork.sockets[101].response[0] != 2 {
		t.Fatal("clients shared a choice or identity")
	}
	if int32(callSlot(t, off, slotNetSocketStandard, 2, 1)) != wipiError {
		t.Fatal("enabled client affected disabled client")
	}
	callSlot(t, a, slotNetClose)
	if !b.notificationNetwork.active || len(b.notificationNetwork.sockets[101].response) != 100 {
		t.Fatal("closing one client affected another")
	}
}

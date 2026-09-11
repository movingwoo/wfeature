package lgt

import (
	"bytes"
	"encoding/binary"
	"math/bits"
	"net/netip"
)

// Instruction relationships identify the notification and empty remote-save protocol.
// Literal pools and Thumb calls may relocate. These are recognition constraints,
// never guest instructions to execute or patch.
var notificationWriter = []uint16{
	0x9b03, 0x2b02, 0xd12d, 0xab09, 0x1c18, 0x2100, 0x2264, 0x4b29, 0x0, 0x0, 0x4669, 0x7eca,
	0x9907, 0x75ca, 0x0, 0x0, 0x1c03, 0x61b, 0xe1b, 0x2b01, 0xd102, 0x4b23, 0x9302, 0xe001,
	0x4922, 0x9102, 0xad09, 0x4a22, 0x4c22, 0xae28, 0x4b22, 0x9300, 0x9b02, 0x9301, 0x1c28, 0x1c11,
	0x1c22, 0x1c33, 0x4c15, 0x0, 0x0, 0x1c28, 0x2164, 0x4b1d, 0x0, 0x0, 0x1c03, 0x9308,
	0xe005,
}

var notificationReply = []uint16{
	0x4b82, 0x9307, 0x9a07, 0x7812, 0x9206, 0x9b06, 0x2b02, 0xd11d, 0xac0c, 0x1c20, 0x2100, 0x2264,
	0x4b7d, 0x0, 0x0, 0x9b26, 0x9a27, 0x1c20, 0x1c19, 0x4b7b, 0x0, 0x0, 0x7824, 0x9405,
	0x9b05, 0x3b02, 0x2b01, 0xd900, 0xe0dd, 0x4b77, 0x33aa, 0x466a, 0x7d12, 0x701a, 0x2001, 0x0,
	0x0, 0xe0d4,
}

var notificationDial = []uint16{
	0xb500, 0xb085, 0x9004, 0x9103, 0x9b04, 0x2b00, 0xd004, 0x4b2a, 0x1c18, 0x0, 0x0, 0xe04b,
	0x4b28, 0x1c18, 0x2100, 0x4b28, 0x0, 0x0, 0x4b27, 0x781b, 0x9301, 0x9b01, 0x2b00, 0xd109,
	0x4b25, 0x4a26, 0x1c18, 0x1c11, 0x4b25, 0x0, 0x0, 0x1c03, 0x9302, 0xe025, 0x9b01, 0x2b01,
	0xd109, 0x4b22, 0x4a22, 0x1c18, 0x1c11, 0x4b1f, 0x0, 0x0, 0x1c03, 0x9302, 0xe018, 0x9b01,
	0x2b02, 0xd109, 0x4b1b, 0x4a1d, 0x1c18, 0x1c11, 0x4b18, 0x0, 0x0, 0x1c03, 0x9302, 0xe00b,
	0x9b01, 0x2b03, 0xd108, 0x4b15, 0x4a16, 0x1c18, 0x1c11, 0x4b12, 0x0, 0x0, 0x1c03, 0x9302,
	0x9b02, 0x9300, 0x9b00, 0x2b00, 0xda0a, 0x9b00, 0x3307, 0xd100, 0xe006, 0x9b02, 0x3313, 0xd003,
	0x4b0d, 0x1c18, 0x0, 0x0, 0xb005, 0xbc01, 0x4700,
}

var notificationQueryDial = []uint16{
	0xb500, 0x2800, 0xd109, 0x2100, 0x4807, 0x0, 0x0, 0x4807, 0x4907, 0x0, 0x0, 0xbc01,
	0x4700, 0x4b06, 0x681a, 0x4b06, 0x6819, 0x0, 0x0, 0xe7f6,
}

var notificationQuery = []uint16{
	0xb500, 0x4806, 0x2102, 0x2200, 0x0, 0x0, 0x4b04, 0x4805, 0x6819, 0x0, 0x0, 0xbc01,
	0x4700,
}

var notificationFinish = []uint16{
	0xb500, 0x4806, 0x2107, 0x2200, 0x0, 0x0, 0x4b04, 0x4805, 0x6819, 0x0, 0x0, 0xbc01,
	0x4700,
}

var notificationQueryReply = []uint16{
	0x1c79, 0x2201, 0xa805, 0x0, 0x0, 0x1cb9, 0x2201, 0xa804, 0x0, 0x0, 0x9c04, 0x2201,
	0x1ce5, 0x1979, 0xa803, 0x0, 0x0, 0x1d25, 0x4c3a, 0x1979, 0x2204, 0x1c20, 0x0, 0x0,
	0x6820, 0x4b2d, 0x0, 0x0, 0x3504, 0x6020, 0xe755,
}

var notificationSocket = []uint16{
	0xb5f0, 0x4a1e, 0x409, 0x7813, 0x2501, 0xb091, 0x1c07, 0xc0e, 0x426d, 0x2b00, 0xd129, 0x1c7b,
	0xd020, 0x4b19, 0x2002, 0x2101, 0x0, 0x0, 0x4b17, 0x6018, 0x2800, 0xdb0c, 0x2300, 0x432,
	0x9300, 0x1c39, 0x1412, 0x4b14, 0x4c14, 0x0, 0x0, 0x1e05, 0xd008, 0x3513, 0xd006, 0x2101,
	0x2001, 0x4249, 0x220e, 0x4252, 0x0, 0x0, 0xb011, 0xbcf0, 0xbc01, 0x4700,
}

// notificationContract keeps routing local to the recognized callers and endpoint.
// The endpoint is compared as data; it is never dialed or resolved.
type notificationContract struct {
	dials          [2]uint32
	socketCallback uint32
	address        uint32
	port           uint16
	application    string
}

func authenticationNotification(module *Module) *notificationContract {
	writers := findOptionCode(module, notificationWriter)
	if len(writers) != 1 {
		return nil
	}
	replies := findOptionCode(module, notificationReply)
	dials := findOptionCode(module, notificationDial)
	queries := findOptionCode(module, notificationQuery)
	finishes := findOptionCode(module, notificationFinish)
	sockets := findOptionCode(module, notificationSocket)
	queryReplies := findOptionCode(module, notificationQueryReply)
	if len(replies) != 1 || len(dials) != 1 || len(queries) != 1 || len(finishes) != 1 || len(sockets) != 1 || len(queryReplies) != 1 {
		return nil
	}
	lookupDials := findOptionCode(module, notificationQueryDial)
	if len(lookupDials) != 1 {
		return nil
	}
	w, r, d, q, f, s, ld := writers[0], replies[0], dials[0], queries[0], finishes[0], sockets[0], lookupDials[0]
	lit := func(a uint32) uint32 { v, _ := optionLiteral(module, a); return v }
	call := func(a uint32) uint32 { v, _ := optionCall(module, a); return v }
	str := func(a uint32, want string) bool {
		return bytes.Equal(optionBytes(module, lit(a), len(want)+1, false), []byte(want+"\x00"))
	}
	if !str(w+0x38, "SMSAGREE") || !str(w+0x36, "%s %s %s %s") || !str(w+0x2a, "Y") || !str(w+0x30, "N") ||
		!str(q+2, "IS_SAVEDATA_EXIST") || !str(f+2, "FINISH_SAVEDATA") {
		return nil
	}
	// Both writers use the same command builder and byte-stream writer.
	if call(q+8) == 0 || call(q+8) != call(f+8) || call(q+0x12) != call(f+0x12) || lit(w+0x56) != call(q+0x12)|1 ||
		lit(q+0xc) != lit(f+0xc) || lit(q+0xe) != lit(f+0xe) {
		return nil
	}
	// The notification's kind field and response buffer join its dial and reader.
	if lit(d+0x24) != lit(r) || lit(d+0x18) != r-0x60|1 ||
		lit(d+0x1e) != call(ld+0xa)|1 || lit(d+0x6c) != call(ld+0x12)|1 ||
		lit(d+0x64) != lit(ld+0xe) || lit(d+0x66) != lit(ld+0x10) {
		return nil
	}

	// The lookup dial installs the receiver that parses this query response and
	// dispatches query/finish completion through its opcode table.
	receiver := lit(ld+8) &^ 1
	if receiver > 0xfffffbff {
		return nil
	}
	if !bytes.Equal(optionBytes(module, receiver+0x84, 4, true), []byte{2, 0x2b, 0, 0xd1}) || notificationBranch(module, receiver+0x88) != queryReplies[0] {
		return nil
	}
	table := lit(receiver + 0xb2)
	if table > 0xffffffdf {
		return nil
	}
	entry := func(index uint32) uint32 {
		b := optionBytes(module, table+index*4, 4, false)
		if len(b) != 4 {
			return 0
		}
		return binary.LittleEndian.Uint32(b)
	}
	if !matchOptionCode(module, entry(2), []uint16{0x9b03, 0x2b00, 0xd068, 0x2100, 0x2002, 0xe76c}) ||
		!matchOptionCode(module, entry(7), []uint16{0x9905, 0x2007, 0xe7b2}) || notificationBranch(module, entry(7)+4) != receiver+0x56 || notificationBranch(module, entry(2)+10) != receiver+0x56 {
		return nil
	}
	// The socket wrapper supplies the only accepted connect callback.
	cb := lit(s + 0x36)
	if cb&1 == 0 || len(optionBytes(module, cb&^1, 2, true)) != 2 {
		return nil
	}
	// Application labels are opaque bounded tokens, not selection keys.
	app := notificationToken(module, lit(w+0x3c), 32)
	if app == "" {
		return nil
	}
	host := notificationToken(module, lit(ld+0xe), 15)
	addr, err := netip.ParseAddr(host)
	if err != nil || !addr.Is4() {
		return nil
	}
	raw := addr.As4()
	port := lit(ld + 0x10)
	if port == 0 || port > 65535 {
		return nil
	}
	return &notificationContract{dials: [2]uint32{d | 1, ld | 1}, socketCallback: cb, address: binary.LittleEndian.Uint32(raw[:]), port: bits.ReverseBytes16(uint16(port)), application: app}
}

func notificationToken(module *Module, address uint32, limit int) string {
	if limit < 0 || uint64(address)+uint64(limit) > 0xffffffff {
		return ""
	}
	var token []byte
	for i := 0; i <= limit; i++ {
		b := optionBytes(module, address+uint32(i), 1, false)
		if len(b) != 1 {
			return ""
		}
		if b[0] == 0 {
			return string(token)
		}
		if b[0] <= 32 || b[0] >= 127 {
			return ""
		}
		token = append(token, b[0])
	}
	return ""
}

func notificationBranch(module *Module, address uint32) uint32 {
	b := optionBytes(module, address, 2, true)
	if len(b) != 2 {
		return 0
	}
	word := binary.LittleEndian.Uint16(b)
	if word&0xf800 != 0xe000 {
		return 0
	}
	displacement := int64(int16(word<<5) >> 4)
	target := int64(address) + 4 + displacement
	if target < 0 || target > 0xffffffff {
		return 0
	}
	return uint32(target)
}

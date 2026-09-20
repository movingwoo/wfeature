package lgt

import (
	"encoding/binary"
	"testing"
)

// Authored instruction windows and independent literal pools; no archive data.
func armNotificationFixture(t *testing.T, delta uint32) (*Module, []byte, []byte) {
	t.Helper()
	code, data := make([]byte, 0x4000), make([]byte, 0x400)
	base, db := uint32(0x100000)+delta, uint32(0x200000)+delta
	m := &Module{Sections: []Section{{Address: base, Size: uint32(len(code)), Data: code, Executable: true}, {Address: db, Size: uint32(len(data)), Data: data}}}
	put := func(off uint32, words ...uint32) {
		for i, w := range words {
			binary.LittleEndian.PutUint32(code[off+uint32(i)*4:], w)
		}
	}
	lit := func(off, value uint32) {
		instruction := binary.LittleEndian.Uint32(code[off:])
		if instruction&0xffff0000 != 0xe59f0000 {
			t.Fatalf("fixture literal at %#x is not LDR", off)
		}
		put(off+8+(instruction&0xfff), value)
	}
	for _, p := range []struct {
		off   uint32
		words []uint32
	}{
		{0x100, armNotificationWriter}, {0x194, armNotificationChoice}, {0x500, armNotificationInit},
		{0x900, armNotificationReply}, {0x1100, armNotificationDecoder}, {0x1900, armNotificationDial}, {0x1d00, armNotificationSocket},
	} {
		put(p.off, p.words...)
	}
	put(0x148, base+0x194)
	put(0x168, 0xe58dc000, 0xe59fc074, 0xe1a0e00f, 0xe12fff1c, 0xe5940008, 0xe24b1054, 0xe59f3064, 0xe1a0e00f, 0xe12fff13)
	put(0x1500, 0xe1a0c00d, 0xe92dd830, 0xe24cb004, 0xe1a04001, 0xe5903000, 0xe3a0100c, 0xe1a05000, 0xe593c028)
	put(0x1564, 0xe1a00004, 0xe59f302c, 0xe1a0e00f, 0xe12fff13, 0xe2800001, 0xe5850510)
	put(0x1548, 0xe59f1044)
	put(0x1558, 0xe59f3038)
	put(0xd00, 0xe1a0c00d, 0xe92dd810, 0xe59f3040, 0xe24cb004, 0xe1a04000, 0xe1a0e00f, 0xe12fff13, 0xe59f3030, 0xe2840e52, 0xe5843000)
	put(0x93c, base+0x2900)
	put(0x2900, 0xe59c3024, 0xe2433002, 0xe3530001, 0x859f1044, 0x8affffe6, 0xeafffff5)
	put(0x2100, 0xe1a0c00d, 0xe92dd830, 0xe59f507c, 0xe59f307c, 0xe24cb004, 0xe1a04000)
	put(0x2140, 0xe2541000, 0x03a01002, 0x0a00000b)
	put(0x217c, 0xe59f3024)
	put(0x2508, 0xe59f5064)
	put(0x2564, 0xe59f301c)
	put(0x252c, 0xe5903004, 0xe3530004, 0x0a000001)
	put(0x2540, 0xe2541000, 0x03a01005, 0x0a000005)
	for _, v := range []struct {
		off  uint32
		text string
	}{{0, "SMSAGREE %s %s %c"}, {0x40, "%s"}, {0x60, "APP_TEST"}, {0x80, "127.0.0.1"}} {
		copy(data[v.off:], v.text+"\x00")
	}
	for _, v := range [][2]uint32{
		{0x198, db}, {0x180, base + 0x1500}, {0x16c, base + 0x2d00}, {0x1548, db + 0x40}, {0x1558, base + 0x2d00},
		{0x52c, base + 0xd00}, {0x544, db + 0x80}, {0x554, 0x1234}, {0x560, db + 0x60}, {0xd1c, db + 0x280},
		{0x1920, base + 0x2100}, {0x1d5c, base + 0x2500}, {0x2108, db + 0x380}, {0x2508, db + 0x380},
		{0x1910, base + 0x2e00}, {0x217c, base + 0x2e00}, {0x2564, base + 0x2e00},
	} {
		lit(v[0], v[1])
	}
	for _, v := range [][2]uint32{{0x208, base + 0x500}, {0x214, base + 0x100}, {0x218, base + 0x900}, {0x290, base + 0x1100}, {0x318, base + 0x1900}, {0x31c, base + 0x1d00}} {
		binary.LittleEndian.PutUint32(data[v[0]:], v[1])
	}
	return m, code, data
}

func TestARMNotificationRelocationAndConnectedContract(t *testing.T) {
	for _, delta := range []uint32{0, 0x71000, 0x4000000} {
		m, _, _ := armNotificationFixture(t, delta)
		c := authenticationARMNotification(m)
		if c == nil {
			t.Fatalf("relocated contract rejected at %#x", delta)
		}
		if c.application != "APP_TEST" || c.address != 0x0100007f || c.port != 0x3412 || c.dials != [2]uint32{0x102100 + delta, 0x102100 + delta} || c.socketCallback != 0x102500+delta || c.protocol != localTerminatedNotificationProtocol {
			t.Fatalf("wrong contract: %+v", c)
		}
	}
}

func TestARMNotificationRejectsDisconnectedOrChangedContracts(t *testing.T) {
	for _, off := range []int{0x120, 0x148, 0x16c, 0x198, 0x194, 0x1564, 0x1574, 0x2900, 0x112c, 0x114c, 0x1920, 0x2108, 0x217c, 0x2508, 0x2540, 0x1d5c, 0x544, 0x554, 0x560} {
		m, code, _ := armNotificationFixture(t, 0)
		code[off] ^= 1
		if authenticationARMNotification(m) != nil {
			t.Fatalf("changed code/pool %#x accepted", off)
		}
	}
	for _, off := range []int{0, 0x40, 0x80, 0x208, 0x218, 0x290, 0x31c} {
		m, _, data := armNotificationFixture(t, 0)
		data[off] ^= 1
		if authenticationARMNotification(m) != nil {
			t.Fatalf("changed data/link %#x accepted", off)
		}
	}
	m, code, _ := armNotificationFixture(t, 0)
	copy(code[0x3000:], code[0x100:0x140])
	if authenticationARMNotification(m) != nil {
		t.Fatal("ambiguous writer accepted")
	}
	if authenticationARMNotification(nil) != nil {
		t.Fatal("nil module accepted")
	}
}

func TestARMNotificationRejectsTruncationAndAddressWrap(t *testing.T) {
	m, code, _ := armNotificationFixture(t, 0)
	for _, size := range []int{0, 1, 0x120, 0x550, 0x1904, 0x250a} {
		copyModule := *m
		copyModule.Sections = append([]Section(nil), m.Sections...)
		copyModule.Sections[0].Data = code[:size]
		if authenticationARMNotification(&copyModule) != nil {
			t.Fatalf("truncated code %#x accepted", size)
		}
	}
	m.Sections[0].Address = 0xfffffff0
	if armNotificationWord(m, 0xfffffff0, 0x20) != 0 || armNotificationMatch(m, 0xfffffff0, 0x20, []uint32{0}) {
		t.Fatal("address wrap accepted")
	}
}

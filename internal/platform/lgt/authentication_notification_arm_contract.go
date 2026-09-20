package lgt

import (
	"bytes"
	"encoding/binary"
	"math/bits"
	"net/netip"
)

// These instruction constraints describe the ARM, NUL-terminated agreement
// exchange. Literal values, image addresses and application names are not keys.
// A different compiler layout remains unsupported until its contract is proven.
var armNotificationWriter = []uint32{
	0xe1a0c00d, 0xe92dd830, 0xe24cb004, 0xe24b5054, 0xe24dd044, 0xe1a04000,
	0xe59f30c0, 0xe3a01000, 0xe3a02040, 0xe1a00005, 0xe1a0e00f, 0xe12fff13,
	0xe5943010, 0xe2433003, 0xe3530003, 0x979ff103,
}
var armNotificationChoice = []uint32{
	0xe1d4ccdc, 0xe59f1050, 0xe28420c0, 0xe28430d4, 0xe1a00005, 0xeaffffee,
}
var armNotificationInit = []uint32{
	0xe1a0c00d, 0xe92dd830, 0xe59f3064, 0xe24cb004, 0xe1a04000,
	0xe1a0e00f, 0xe12fff13, 0xe59f3054, 0xe3a00e55, 0xe1a0e00f,
	0xe12fff13, 0xe59f3048, 0xe1a05000, 0xe1a0e00f, 0xe12fff13,
	0xe5845008, 0xe284002c, 0xe59f1034, 0xe59f5034, 0xe1a0e00f,
	0xe12fff15, 0xe59f302c, 0xe28400d4, 0xe1c43abc, 0xe59f1024,
}
var armNotificationReply = []uint32{
	0xe1a0c00d, 0xe92dd800, 0xe5903008, 0xe5902010, 0xe593300c,
	0xe3a01000, 0xe2422003, 0xe24cb004, 0xe5803024, 0xe1a0c000,
	0xe1a00001, 0xe3520003, 0x979ff102,
}
var armNotificationDecoder = []uint32{
	0xe1a0c00d, 0xe92dd870, 0xe5903008, 0xe3530002, 0xe24cb004,
	0xe1a06000, 0xe1a05001, 0x0a000002, 0xe596000c, 0xe91b6870,
	0xe12fff1e, 0xe0d530d1, 0xe3530009, 0x13530007, 0xe580300c,
	0xe580352c, 0x0a000013, 0xe3730011, 0x0a000011, 0xe0d530d1,
	0xe3530000, 0xe5863530, 0xe2864010, 0xc3a03003, 0xd3a03005, 0xe5863008,
}
var armNotificationDial = []uint32{
	0xe1a0c00d, 0xe92dd870, 0xe3a01001, 0xe24cb004, 0xe59f507c,
	0xe1a06000, 0xe1a0e00f, 0xe12fff15, 0xe59f0070, 0xe3a01000,
	0xe59f306c, 0xe1a0e00f, 0xe12fff13,
}
var armNotificationSocket = []uint32{
	0xe1a0c00d, 0xe92dd810, 0xe24cb004, 0xe24dd004, 0xe1a04000,
	0xe5d0102c, 0xe3510001, 0x13a01001, 0x03a00002, 0x059f3084,
	0x13a00002, 0x159f3080, 0xe1a0e00f, 0xe12fff13, 0xe5840010,
	0xe5940010, 0xe3500000, 0xb1a01000, 0xb59f0068, 0xba00000f,
	0xe3a0c000, 0xe1d421f4, 0xe5941008, 0xe59f3058, 0xe58dc000,
	0xe59fc054, 0xe1a0e00f, 0xe12fff1c,
}

func authenticationARMNotification(module *Module) *notificationContract {
	writers := armNotificationFind(module, armNotificationWriter)
	dials := armNotificationFind(module, armNotificationDial)
	sockets := armNotificationFind(module, armNotificationSocket)
	if len(writers) != 1 || len(dials) != 1 || len(sockets) != 1 {
		return nil
	}
	w, dial, socket := writers[0], dials[0], sockets[0]
	match := func(base, offset uint32, words ...uint32) bool {
		return armNotificationMatch(module, base, offset, words)
	}
	word := func(base, offset uint32) uint32 { return armNotificationWord(module, base, offset) }
	literal := func(base, offset uint32) uint32 { return armNotificationLiteral(module, base, offset) }
	if !match(w, 0x94, armNotificationChoice...) || word(w, 0x48) != uint32(uint64(w)+0x94) ||
		!match(w, 0x68, 0xe58dc000, 0xe59fc074, 0xe1a0e00f, 0xe12fff1c, 0xe5940008, 0xe24b1054, 0xe59f3064, 0xe1a0e00f, 0xe12fff13) ||
		!armNotificationString(module, literal(w, 0x98), "SMSAGREE %s %s %c") {
		return nil
	}
	sender := literal(w, 0x80)
	if !match(sender, 0, 0xe1a0c00d, 0xe92dd830, 0xe24cb004, 0xe1a04001, 0xe5903000, 0xe3a0100c, 0xe1a05000, 0xe593c028) ||
		!match(sender, 0x64, 0xe1a00004, 0xe59f302c, 0xe1a0e00f, 0xe12fff13, 0xe2800001, 0xe5850510) ||
		!armNotificationString(module, literal(sender, 0x48), "%s") || literal(sender, 0x58) != literal(w, 0x6c) {
		return nil
	}
	// The agreement's virtual table connects its initializer, writer and reply.
	var table uint32
	for _, ref := range armNotificationFind(module, []uint32{w}) {
		if ref < 0x14 {
			continue
		}
		candidate := ref - 0x14
		if match(word(candidate, 8), 0, armNotificationInit...) && match(word(candidate, 0x18), 0, armNotificationReply...) {
			if table != 0 {
				return nil
			}
			table = candidate
		}
	}
	if table == 0 {
		return nil
	}
	init, reply := word(table, 8), word(table, 0x18)
	if !match(word(reply, 0x3c), 0, 0xe59c3024, 0xe2433002, 0xe3530001, 0x859f1044, 0x8affffe6, 0xeafffff5) {
		return nil
	}
	// The initializer installs the decoder through the network object's vtable.
	ctor := literal(init, 0x2c)
	if !match(ctor, 0, 0xe1a0c00d, 0xe92dd810, 0xe59f3040, 0xe24cb004, 0xe1a04000, 0xe1a0e00f, 0xe12fff13, 0xe59f3030, 0xe2840e52, 0xe5843000) ||
		!match(word(literal(ctor, 0x1c), 0x10), 0, armNotificationDecoder...) {
		return nil
	}
	// Both callbacks use the same transport singleton and state setter. The
	// transport vtable must link these exact dial/socket methods.
	dc, sc := literal(dial, 0x20), literal(socket, 0x5c)
	if !match(dc, 0, 0xe1a0c00d, 0xe92dd830, 0xe59f507c, 0xe59f307c, 0xe24cb004, 0xe1a04000) ||
		!match(dc, 0x40, 0xe2541000, 0x03a01002, 0x0a00000b) ||
		!match(sc, 0x2c, 0xe5903004, 0xe3530004, 0x0a000001) ||
		!match(sc, 0x40, 0xe2541000, 0x03a01005, 0x0a000005) ||
		literal(dial, 0x10) == 0 || literal(w, 0x6c) == 0 || literal(dc, 8) == 0 || literal(dc, 8) != literal(sc, 8) || literal(dial, 0x10) != literal(dc, 0x7c) || literal(dc, 0x7c) != literal(sc, 0x64) {
		return nil
	}
	linked := 0
	for _, ref := range armNotificationFind(module, []uint32{dial, socket}) {
		if ref >= 0x18 {
			linked++
		}
	}
	if linked != 1 {
		return nil
	}
	app := notificationToken(module, literal(init, 0x60), 32)
	host := notificationToken(module, literal(init, 0x44), 15)
	port := literal(init, 0x54)
	addr, err := netip.ParseAddr(host)
	if err != nil || !addr.Is4() || app == "" || port == 0 || port > 65535 {
		return nil
	}
	raw := addr.As4()
	return &notificationContract{dials: [2]uint32{dc, dc}, socketCallback: sc, address: binary.LittleEndian.Uint32(raw[:]), port: bits.ReverseBytes16(uint16(port)), application: app, protocol: localTerminatedNotificationProtocol}
}

func armNotificationString(module *Module, address uint32, want string) bool {
	return address != 0 && bytes.Equal(optionBytes(module, address, len(want)+1, false), []byte(want+"\x00"))
}
func armNotificationWord(module *Module, base, offset uint32) uint32 {
	address := uint64(base) + uint64(offset)
	if address+4 > 1<<32 {
		return 0
	}
	b := optionBytes(module, uint32(address), 4, false)
	if len(b) != 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}
func armNotificationLiteral(module *Module, base, offset uint32) uint32 {
	instruction := armNotificationWord(module, base, offset)
	if instruction&0xffff0000 != 0xe59f0000 {
		return 0
	}
	address := uint64(base) + uint64(offset) + 8 + uint64(instruction&0xfff)
	if address+4 > 1<<32 {
		return 0
	}
	return armNotificationWord(module, uint32(address), 0)
}
func armNotificationMatch(module *Module, base, offset uint32, words []uint32) bool {
	address := uint64(base) + uint64(offset)
	if address+uint64(len(words))*4 > 1<<32 {
		return false
	}
	b := optionBytes(module, uint32(address), len(words)*4, true)
	if len(b) != len(words)*4 {
		return false
	}
	for i, w := range words {
		if binary.LittleEndian.Uint32(b[i*4:]) != w {
			return false
		}
	}
	return true
}
func armNotificationFind(module *Module, words []uint32) []uint32 {
	if module == nil || len(words) == 0 {
		return nil
	}
	pattern := make([]byte, len(words)*4)
	for i, w := range words {
		binary.LittleEndian.PutUint32(pattern[i*4:], w)
	}
	var found []uint32
	for _, section := range module.Sections {
		for offset := 0; offset+len(pattern) <= len(section.Data); {
			at := bytes.Index(section.Data[offset:], pattern)
			if at < 0 {
				break
			}
			offset += at
			address := uint64(section.Address) + uint64(offset)
			if address%4 == 0 && address+uint64(len(pattern)) <= 1<<32 {
				if len(found) == 64 {
					return nil
				}
				found = append(found, uint32(address))
			}
			offset++
		}
	}
	return found
}

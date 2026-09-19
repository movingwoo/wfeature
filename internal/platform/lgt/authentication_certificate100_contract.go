package lgt

import "encoding/binary"

// Connected Thumb contracts for a 100-byte certificate embedded after an
// encrypted options header. Calls and literal pools may relocate.
var certificate100Reader = []uint16{
	0xb570, 0xb0bb, 0x4822, 0x2101, 0x2201, 0x4b22, 0, 0, 0x1e06, 0xdb38, 0xac01, 0x2100,
	0x221e, 0x1c20, 0x4b1e, 0, 0, 0x1c20, 0x4b1d, 0, 0, 0x4b1d, 0xad09, 0x1c22,
	0x9300, 0x491c, 0x4b1c, 0x4c1d, 0x1c28, 0, 0, 0xac22, 0x2164, 0x2200, 0x4b1a, 0x1c30,
	0, 0, 0x1c21, 0x2264, 0x4b18, 0x1c30, 0, 0, 0x4b17, 0x1c30, 0, 0,
	0x4b16, 0x1c28, 0, 0, 0x4b15, 0x1c01, 0x4a15, 0x1c20, 0, 0, 0x1c28, 0x1c21,
	0x4b13, 0, 0, 0x2800, 0xd101, 0x2001, 0xe000, 0x2000, 0xb03b, 0xbc70, 0xbc02, 0x4708,
}

var certificate100Writer = []uint16{
	0xb530, 0xb0a2, 0xac01, 0x2100, 0x221e, 0x1c20, 0x4b1d, 0, 0, 0x1c20, 0x4b1c, 0,
	0, 0x4b1c, 0xad09, 0x1c22, 0x491b, 0x9300, 0x4c1b, 0x4b1c, 0x1c28, 0, 0, 0x4b1b,
	0x1c28, 0, 0, 0x4a1a, 0x1c01, 0x4b1a, 0x1c28, 0, 0, 0x4819, 0x2108, 0x2201,
	0x4b18, 0, 0, 0x1e04, 0xdb14, 0x2164, 0x2200, 0x4b16, 0x1c20, 0, 0, 0x2264,
	0x1c29, 0x4b14, 0x1c20, 0, 0, 0x4b13, 0x1c20, 0, 0, 0x4b12, 0x2201, 0x76da,
	0, 0, 0xb022, 0xbc30, 0xbc01, 0x4700,
}

var certificate100Gate = []uint16{
	0xb500, 0x4b0b, 0x46bc, 0x4f0b, 0x7edb, 0x44bd, 0x4667, 0x2b00, 0xd101, 0x2000, 0xe007, 0,
	0, 0x2800, 0xd102, 0x2001, 0x4240, 0xe000, 0x2001, 0x2385, 0x9b, 0x449d, 0xbc02, 0x4708,
}

var certificate100Decoder = []uint16{
	0xb5f0, 0x1c07, 0x4b15, 0x1c08, 0x1c0d, 0x1c14, 0, 0, 0x2100, 0x1c06, 0x2d00, 0xdd13,
	0x4b11, 0x4812, 0x469c, 0x5c7a, 0x1223, 0x4053, 0x5473, 0x5c7b, 0x3101, 0x18e3, 0x4664, 0x6822,
	0x61b, 0xe1b, 0x435a, 0x6803, 0x1c1c, 0x4354, 0x42a9, 0xdbee, 0x1c31, 0x1c2a, 0x1c38, 0x4b08,
	0, 0, 0x1c30, 0x4b07, 0, 0, 0x1c38, 0xbcf0, 0xbc02, 0x4708,
}

var certificate100Encoder = []uint16{
	0xb5f0, 0x1c07, 0x4b15, 0x1c08, 0x1c0d, 0x1c14, 0, 0, 0x2100, 0x1c06, 0x2d00, 0xdd12,
	0x4b11, 0x4812, 0x469c, 0x5c7a, 0x1223, 0x4053, 0x5473, 0x191b, 0x4664, 0x6822, 0x61b, 0xe1b,
	0x435a, 0x6803, 0x3101, 0x1c1c, 0x4354, 0x42a9, 0xdbef, 0x1c31, 0x1c2a, 0x1c38, 0x4b08, 0,
	0, 0x1c30, 0x4b07, 0, 0, 0x1c38, 0xbcf0, 0xbc02, 0x4708,
}

var certificate100HeaderReader = []uint16{
	0xb5f0, 0xb09a, 0x4849, 0x2101, 0x2201, 0x4b49, 0, 0, 0x1e07, 0xda00, 0xe084, 0x2400,
	0x2100, 0x2200, 0x4b45, 0xae01, 0x9400, 0x1c38, 0, 0, 0x1c31, 0x2264, 0x4b42, 0x1c38,
	0, 0, 0x4a41, 0x1c30, 0x2164, 0x4b41, 0, 0, 0x2200, 0x5cb3, 0x3201, 0x18e4,
	0x2a2f, 0xddfa, 0x4668, 0xa90d, 0x2204, 0x4d3c, 0, 0, 0x9b00, 0x429c, 0xd002, 0x2001,
	0x4240, 0xe05e, 0x4c38, 0x1c31, 0x2204, 0x1c20, 0, 0, 0xa902, 0x2204, 0x1d20, 0,
	0, 0x1c20, 0xa903, 0x2204, 0x3008, 0, 0, 0x1c20, 0xa904, 0x220c, 0x300c, 0,
	0, 0x1c20, 0xa907, 0x2201, 0x3018, 0, 0, 0x1c20, 0x4669, 0x311d, 0x2201, 0x3019,
	0, 0, 0x1c20, 0x4669, 0x311e, 0x2201, 0x301a, 0, 0, 0x1c20, 0x4669, 0x311f,
	0x2201, 0x301b, 0, 0, 0x1c20, 0xa908, 0x2201, 0x301c, 0, 0, 0x1c20, 0x4669,
	0x3121, 0x2201, 0x301d, 0, 0, 0x1c20, 0x4669, 0x3122, 0x2202, 0x301e, 0, 0,
	0x1c20, 0xa909, 0x2208, 0x3020, 0, 0, 0x1c20, 0xa90b, 0x2204, 0x3028, 0, 0,
	0x1c20, 0xa90c, 0x2204, 0x302c, 0, 0, 0x1c38, 0x4b0d, 0, 0, 0x2001, 0xe000,
	0x2000, 0xb01a, 0xbcf0, 0xbc02, 0x4708,
}

var certificate100Subscriber = []uint16{0xb500, 0x1c01, 0x220d, 0x4803, 0x4b03, 0, 0, 0xbc01, 0x4700}

type certificate100Contract struct {
	name, application, token            string
	state, seed, headerSeed, multiplier uint32
}

func authenticationCertificate100(module *Module) *certificate100Contract {
	patterns := [][]uint16{certificate100Reader, certificate100Writer, certificate100Gate, certificate100Decoder, certificate100Encoder, certificate100HeaderReader}
	addresses := make([]uint32, len(patterns))
	for i, pattern := range patterns {
		found := findOptionCode(module, pattern)
		if len(found) != 1 {
			return nil
		}
		addresses[i] = found[0]
	}
	reader, writer, gate, decoder, encoder, header := addresses[0], addresses[1], addresses[2], addresses[3], addresses[4], addresses[5]
	literal := func(a uint32) uint32 { v, _ := optionLiteral(module, a); return v }
	call := func(a uint32) uint32 { v, _ := optionCall(module, a); return v }
	if call(gate+0x16) != reader || literal(reader+0x68) != decoder|1 || literal(writer+0x3a) != encoder|1 || literal(header+0x3a) != decoder|1 {
		return nil
	}
	state := literal(gate + 2)
	if state != literal(writer+0x72) || state != literal(header+0x64) || !optionDataRange(module, state, 48) {
		return nil
	}
	// Reader and writer must construct exactly the same subscriber-bound string.
	for _, pair := range [][2]uint32{{reader + 4, writer + 0x42}, {reader + 0x24, writer + 0x14}, {reader + 0x2a, writer + 0x1a}, {reader + 0x32, writer + 0x20}, {reader + 0x34, writer + 0x26}, {reader + 0x36, writer + 0x24}, {reader + 0x6c, writer + 0x36}, {reader + 4, header + 4}, {decoder + 0x18, encoder + 0x18}, {decoder + 0x1a, encoder + 0x1a}} {
		if literal(pair[0]) == 0 || literal(pair[0]) != literal(pair[1]) {
			return nil
		}
	}
	if string(optionBytes(module, literal(reader+0x32), 7, false)) != "%s%s%s\x00" {
		return nil
	}
	trampoline := call(reader + 0xc)
	if string(optionBytes(module, trampoline, 2, true)) != "\x18\x47" {
		return nil
	}
	stringTrampoline := call(reader + 0x3a)
	if string(optionBytes(module, stringTrampoline, 2, true)) != "\x20\x47" {
		return nil
	}
	if !certificate58Calls(module, reader, certificate100Reader, trampoline, map[uint32]uint32{0x3a: stringTrampoline}) ||
		!certificate58Calls(module, writer, certificate100Writer, trampoline, map[uint32]uint32{0x2a: stringTrampoline, 0x78: call(writer + 0x78)}) ||
		!certificate58Calls(module, decoder, certificate100Decoder, trampoline, nil) || !certificate58Calls(module, encoder, certificate100Encoder, trampoline, nil) {
		return nil
	}
	property := literal(reader + 0x24)
	if property&1 == 0 || !matchOptionCode(module, property&^1, certificate100Subscriber) ||
		string(optionBytes(module, literal((property&^1)+6), 12, false)) != "PHONENUMBER\x00" || call((property&^1)+10) != trampoline {
		return nil
	}
	copyTrampoline := call(header + 0x54)
	if string(optionBytes(module, copyTrampoline, 2, true)) != "\x28\x47" {
		return nil
	}
	copies := map[uint32]uint32{}
	for _, offset := range []uint32{0x54, 0x6c, 0x76, 0x82, 0x8e, 0x9a, 0xa8, 0xb6, 0xc4, 0xd0, 0xde, 0xec, 0xf8, 0x104, 0x110} {
		copies[offset] = copyTrampoline
	}
	if !certificate58Calls(module, header, certificate100HeaderReader, trampoline, copies) {
		return nil
	}
	// The certificate writer persists the same options state through a real
	// function. Its flag byte is also the startup gate's prerequisite.
	if len(optionBytes(module, call(writer+0x78), 2, true)) != 2 {
		return nil
	}
	name := certificate100String(module, literal(reader+4), 128)
	application := certificate100String(module, literal(reader+0x34), 32)
	token := certificate100String(module, literal(reader+0x2a), 64)
	if name == "" || application == "" || token == "" {
		return nil
	}
	if _, err := fileSaveKey(name); err != nil {
		return nil
	}
	a := optionBytes(module, literal(decoder+0x18), 4, false)
	b := optionBytes(module, literal(decoder+0x1a), 4, false)
	if len(a) != 4 || len(b) != 4 {
		return nil
	}
	return &certificate100Contract{name: name, application: application, token: token, state: state, seed: literal(reader + 0x6c), headerSeed: literal(header + 0x34), multiplier: binary.LittleEndian.Uint32(a) * binary.LittleEndian.Uint32(b)}
}

func certificate100String(module *Module, address uint32, limit int) string {
	for n := 1; n <= limit; n++ {
		data := optionBytes(module, address, n, false)
		if len(data) != n {
			return ""
		}
		if data[n-1] == 0 {
			return string(data[:n-1])
		}
	}
	return ""
}

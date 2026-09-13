package lgt

import (
	"encoding/binary"
	"math/bits"
	"net/netip"
)

// These connected instruction windows describe one small binary
// authentication exchange. Zero pairs are relocation-aware Thumb calls. The
// ARM-library BLX instructions remain fixed relative to the module image.
var authenticationHandshakeBuilder = []uint16{
	0xb570, 0x1c0d, 0x4cec, 0x3508, 0x1c06, 0x1c20, 0x1c29, 0x3024, 0, 0, 0x6a60, 0, 0,
	0x1c04, 0x1c29, 0xf001, 0xe842, 0x48e6, 0x8020, 0x8065, 0x2007, 0x80a0, 0x1c20, 0x3008,
	0x71a6, 0xbd70,
}

var authenticationHandshakeRequest = []uint16{
	0xb570, 0x496d, 0x2004, 0x6108, 0x486d, 0, 0, 0, 0, 0x2434, 0x210c, 0x486a, 0, 0,
	0x1c21, 0x200c, 0, 0, 0x1c05, 0x4868, 0x4e67, 0x1831, 0x1c28, 0xf001, 0xea4a,
	0x4865, 0x350c, 0x3010, 0x1831, 0x1c28, 0xf001, 0xea44, 0x350c, 0x2209, 0x1c28,
	0xa166, 0xf000, 0xed7a, 0x350c, 0x2206, 0x1c28, 0xa166, 0xf000, 0xed74, 0x2001,
	0x7328, 0x1c20, 0, 0, 0xbd70,
}

var authenticationHandshakeSend = []uint16{
	0xb510, 0x4c1e, 0x2100, 0x80a1, 0x3008, 0x62a0, 0x85a1, 0x6121, 0x2001, 0x222e, 0x5510,
	0x2030, 0x5501, 0x6a60, 0, 0, 0x7980, 0x61a0, 0, 0, 0xbd10,
}

var authenticationHandshakeDial = []uint16{
	0xb580, 0x2100, 0x4829, 0xf03d, 0xee96, 0xbd80,
}

var authenticationHandshakeSocket = []uint16{
	0xb538, 0xa02e, 0xf03d, 0xee9e, 0x4c29, 0x60a0, 0x482f, 0xf03d, 0xee9e, 0x80e0, 0x2200,
	0x9200, 0x1c05, 0x6820, 0x68a1, 0x1c2a, 0x4b2b, 0xf03d, 0xee98, 0xbd38,
}

var authenticationHandshakeSocketCallback = []uint16{
	0x2900, 0xd107, 0x480f, 0x2101, 0x222f, 0x5411, 0x2100, 0x6101, 0x6141, 0xe78a, 0x1c4b,
	0xd100, 0xe6a5, 0x4770,
}

var authenticationHandshakeDialCallback = []uint16{
	0xb580, 0x2800, 0xd10a, 0x2101, 0x2002, 0xf03d, 0xee66, 0x4905, 0x2800, 0x6008, 0xdb04,
	0, 0, 0xbd80, 0x1c43, 0xd1fc, 0, 0, 0xbd80,
}

var authenticationHandshakeReceiver = []uint16{
	0x79c0, 0x2800, 0xd007, 0x68e0, 0, 0, 0x1c32, 0x2108, 0, 0, 0xe00e, 0x68e0, 0, 0,
	0x7985, 0x68e0, 0, 0, 0x6961, 0x3008, 0x3908, 0x1c2a, 0, 0, 0x2800,
}

var authenticationHandshakeReply = []uint16{
	0x2c0c, 0xd10a, 0x9994, 0x2001, 0x6288, 0, 0, 0, 0, 0x20d9, 0, 0, 0xe011,
}

// authenticationHandshake connects packet construction, send/dial/socket
// setup and response dispatch. Names, hashes, fixed addresses and endpoint
// values do not select it.
func authenticationHandshake(module *Module) *notificationContract {
	builders := findOptionCode(module, authenticationHandshakeBuilder)
	requests := findOptionCode(module, authenticationHandshakeRequest)
	sends := findOptionCode(module, authenticationHandshakeSend)
	dials := findOptionCode(module, authenticationHandshakeDial)
	sockets := findOptionCode(module, authenticationHandshakeSocket)
	receivers := findOptionCode(module, authenticationHandshakeReceiver)
	if len(builders) != 1 || len(requests) != 1 || len(sends) != 1 || len(dials) != 1 || len(sockets) != 1 || len(receivers) != 1 {
		return nil
	}
	builder, request, send := builders[0], requests[0], sends[0]
	dial, socket, receiver := dials[0], sockets[0], receivers[0]
	call := func(address uint32) uint32 {
		target, _ := optionCall(module, address)
		return target
	}
	literal := func(address uint32) uint32 {
		value, _ := optionLiteral(module, address)
		return value
	}
	if literal(builder+0x22) != 0x504b || call(request+0x20) != builder || call(request+0x5e) != send || call(send+0x24) != dial {
		return nil
	}
	applicationAddress, applicationOK := authenticationHandshakeADR(module, request+0x46)
	versionAddress, versionOK := authenticationHandshakeADR(module, request+0x52)
	application := notificationToken(module, applicationAddress, 11)
	version := notificationToken(module, versionAddress, 11)
	if !applicationOK || !versionOK || application == "" || version == "" {
		return nil
	}

	dialCallback := literal(dial + 4)
	socketCallback := literal(socket + 0x20)
	if dialCallback&1 == 0 || socketCallback&1 == 0 ||
		!matchOptionCode(module, dialCallback&^1, authenticationHandshakeDialCallback) ||
		!matchOptionCode(module, socketCallback&^1, authenticationHandshakeSocketCallback) ||
		call((dialCallback&^1)+0x16) != socket {
		return nil
	}

	dispatcher := call(receiver + 0x10)
	if dispatcher == 0 || call(receiver+0x2c) != dispatcher || dispatcher > 0xfffff8ff ||
		!matchOptionCode(module, dispatcher+0x700, authenticationHandshakeReply) {
		return nil
	}

	hostAddress, ok := authenticationHandshakeADR(module, socket+2)
	if !ok {
		return nil
	}
	host := notificationToken(module, hostAddress, 15)
	address, err := netip.ParseAddr(host)
	if err != nil || !address.Is4() {
		return nil
	}
	portWord := literal(socket + 0xc)
	if high := portWord >> 16; high != 0 && high != 0xffff || uint16(portWord) == 0 {
		return nil
	}
	raw := address.As4()
	return &notificationContract{
		dials:          [2]uint32{dialCallback, dialCallback},
		socketCallback: socketCallback,
		address:        binary.LittleEndian.Uint32(raw[:]),
		port:           bits.ReverseBytes16(uint16(portWord)),
		application:    application,
		version:        version,
		protocol:       localAuthenticationProtocol,
	}
}

func authenticationHandshakeADR(module *Module, address uint32) (uint32, bool) {
	code := optionBytes(module, address, 2, true)
	if len(code) != 2 {
		return 0, false
	}
	word := binary.LittleEndian.Uint16(code)
	if word&0xf800 != 0xa000 {
		return 0, false
	}
	target := ((uint64(address) + 4) &^ uint64(3)) + uint64(word&0xff)*4
	if target > 0xffffffff || len(optionBytes(module, uint32(target), 1, false)) != 1 {
		return 0, false
	}
	return uint32(target), true
}

func newAuthenticationHandshakeNetwork(contract *notificationContract, archive *Archive, identity, model string) *notificationNetwork {
	if contract == nil || archive == nil || contract.protocol != localAuthenticationProtocol {
		return nil
	}
	if contract.application != archive.Descriptor.AID {
		return nil
	}
	request := make([]byte, 60)
	copy(request, []byte{'K', 'P', 60, 0, 7, 0, 12, 0})
	if !putAuthenticationHandshakeField(request[8:20], identity) ||
		!putAuthenticationHandshakeField(request[20:32], model) ||
		!putAuthenticationHandshakeField(request[32:44], contract.application) ||
		!putAuthenticationHandshakeField(request[44:56], contract.version) {
		return nil
	}
	binary.LittleEndian.PutUint32(request[56:], 1)
	return &notificationNetwork{
		contract:              *contract,
		identity:              identity,
		authenticationRequest: request,
	}
}

func putAuthenticationHandshakeField(field []byte, value string) bool {
	if value == "" || len(value) >= len(field) {
		return false
	}
	for index := range len(value) {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	copy(field, value)
	return true
}

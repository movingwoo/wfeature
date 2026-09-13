package lgt

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

const (
	handshakeFixtureBuilder        = uint32(0x400)
	handshakeFixtureRequest        = uint32(0x900)
	handshakeFixtureSend           = uint32(0xd00)
	handshakeFixtureDial           = uint32(0xe00)
	handshakeFixtureSocket         = uint32(0xf00)
	handshakeFixtureSocketCallback = uint32(0x1100)
	handshakeFixtureDialCallback   = uint32(0x1200)
	handshakeFixtureReceiver       = uint32(0x1300)
	handshakeFixtureDispatcher     = uint32(0x1400)
	handshakeFixtureReply          = uint32(0x1b00)
	handshakeFixtureDummy          = uint32(0x1d00)
)

func authenticationHandshakeFixture(t *testing.T, delta uint32) (*Module, []byte) {
	t.Helper()
	const textSize = 0x2000
	textBase := fixtureTextBase + delta
	code := make([]byte, textSize)
	module := &Module{Sections: []Section{{Address: textBase, Size: textSize, Data: code, Executable: true}}}
	put := func(offset uint32, words ...uint16) {
		for index, word := range words {
			binary.LittleEndian.PutUint16(code[offset+uint32(index*2):], word)
		}
	}
	call := func(offset, target uint32) {
		address := textBase + offset
		displacement := target - address - 4
		put(offset, 0xf000|uint16(displacement>>12)&0x7ff, 0xf800|uint16(displacement>>1)&0x7ff)
	}
	writePattern := func(offset uint32, pattern []uint16) {
		for index := 0; index < len(pattern); index++ {
			if pattern[index] == 0 && index+1 < len(pattern) && pattern[index+1] == 0 {
				call(offset+uint32(index*2), textBase+handshakeFixtureDummy)
				index++
				continue
			}
			put(offset+uint32(index*2), pattern[index])
		}
	}
	for _, part := range []struct {
		offset  uint32
		pattern []uint16
	}{
		{handshakeFixtureBuilder, authenticationHandshakeBuilder},
		{handshakeFixtureRequest, authenticationHandshakeRequest},
		{handshakeFixtureSend, authenticationHandshakeSend},
		{handshakeFixtureDial, authenticationHandshakeDial},
		{handshakeFixtureSocket, authenticationHandshakeSocket},
		{handshakeFixtureSocketCallback, authenticationHandshakeSocketCallback},
		{handshakeFixtureDialCallback, authenticationHandshakeDialCallback},
		{handshakeFixtureReceiver, authenticationHandshakeReceiver},
		{handshakeFixtureReply, authenticationHandshakeReply},
	} {
		writePattern(part.offset, part.pattern)
	}
	put(handshakeFixtureDummy, 0x4770)
	call(handshakeFixtureRequest+0x20, textBase+handshakeFixtureBuilder)
	call(handshakeFixtureRequest+0x5e, textBase+handshakeFixtureSend)
	call(handshakeFixtureSend+0x24, textBase+handshakeFixtureDial)
	call(handshakeFixtureDialCallback+0x16, textBase+handshakeFixtureSocket)
	call(handshakeFixtureReceiver+0x10, textBase+handshakeFixtureDispatcher)
	call(handshakeFixtureReceiver+0x2c, textBase+handshakeFixtureDispatcher)

	literal := func(offset, value uint32) {
		address := textBase + offset
		word := binary.LittleEndian.Uint16(code[offset:])
		pool := ((address + 4) &^ 3) + uint32(word&0xff)*4
		binary.LittleEndian.PutUint32(code[pool-textBase:], value)
	}
	literal(handshakeFixtureBuilder+0x22, 0x504b)
	literal(handshakeFixtureDial+4, textBase+handshakeFixtureDialCallback|1)
	literal(handshakeFixtureSocket+0xc, 0x1234)
	literal(handshakeFixtureSocket+0x20, textBase+handshakeFixtureSocketCallback|1)

	host, ok := authenticationHandshakeADR(module, textBase+handshakeFixtureSocket+2)
	if !ok {
		t.Fatal("fixture socket has no host address")
	}
	copy(code[host-textBase:], "127.0.0.1\x00")
	application, ok := authenticationHandshakeADR(module, textBase+handshakeFixtureRequest+0x46)
	if !ok {
		t.Fatal("fixture request has no application address")
	}
	version, ok := authenticationHandshakeADR(module, textBase+handshakeFixtureRequest+0x52)
	if !ok {
		t.Fatal("fixture request has no version address")
	}
	copy(code[application-textBase:], "APP00001\x00")
	copy(code[version-textBase:], "1.0.0\x00")
	return module, code
}

func TestAuthenticationHandshakeRecognitionRequiresConnectedContracts(t *testing.T) {
	for _, delta := range []uint32{0, 0x11000, 0x710000} {
		module, code := authenticationHandshakeFixture(t, delta)
		contract := authenticationHandshake(module)
		if contract == nil || contract.application != "APP00001" || contract.version != "1.0.0" ||
			contract.address != 0x0100007f || contract.port != 0x3412 || contract.protocol != localAuthenticationProtocol {
			t.Fatalf("relocated contract at %#x = %+v", delta, contract)
		}
		for _, offset := range []uint32{
			handshakeFixtureBuilder, handshakeFixtureRequest, handshakeFixtureSend, handshakeFixtureDial,
			handshakeFixtureSocket, handshakeFixtureSocketCallback, handshakeFixtureDialCallback,
			handshakeFixtureReceiver, handshakeFixtureReply,
		} {
			original := code[offset]
			code[offset] ^= 0x80
			if authenticationHandshake(module) != nil {
				t.Fatalf("changed contract at %#x accepted", offset)
			}
			code[offset] = original
		}
		for _, address := range []uint32{
			fixtureTextBase + delta + handshakeFixtureBuilder + 0x22,
			fixtureTextBase + delta + handshakeFixtureDial + 4,
			fixtureTextBase + delta + handshakeFixtureSocket + 0xc,
			fixtureTextBase + delta + handshakeFixtureSocket + 0x20,
		} {
			word := binary.LittleEndian.Uint16(optionBytes(module, address, 2, true))
			pool := ((address + 4) &^ 3) + uint32(word&0xff)*4
			data := optionBytes(module, pool, 4, false)
			original := bytes.Clone(data)
			for index := range data {
				data[index] = 0
			}
			if authenticationHandshake(module) != nil {
				t.Fatalf("cleared literal for %#x accepted", address)
			}
			copy(data, original)
		}
		for _, offset := range []uint32{
			handshakeFixtureRequest + 0x20,
			handshakeFixtureRequest + 0x5e,
			handshakeFixtureSend + 0x24,
			handshakeFixtureDialCallback + 0x16,
			handshakeFixtureReceiver + 0x2c,
		} {
			original := code[offset+2]
			code[offset+2] ^= 2
			if authenticationHandshake(module) != nil {
				t.Fatalf("disconnected call at %#x accepted", offset)
			}
			code[offset+2] = original
		}
		for _, address := range []uint32{
			fixtureTextBase + delta + handshakeFixtureSocket + 2,
			fixtureTextBase + delta + handshakeFixtureRequest + 0x46,
			fixtureTextBase + delta + handshakeFixtureRequest + 0x52,
		} {
			target, ok := authenticationHandshakeADR(module, address)
			if !ok {
				t.Fatalf("fixture ADR at %#x did not resolve", address)
			}
			data := optionBytes(module, target, 1, false)
			original := data[0]
			data[0] = 0xff
			if authenticationHandshake(module) != nil {
				t.Fatalf("invalid token at %#x accepted", target)
			}
			data[0] = original
		}
	}
	if authenticationHandshake(nil) != nil {
		t.Fatal("nil module recognized")
	}
}

func TestAuthenticationHandshakeAnswersOnlyTheExactRequest(t *testing.T) {
	contract := &notificationContract{
		dials: [2]uint32{1, 1}, socketCallback: 3, address: 0x0100007f, port: 0x3412,
		application: "APP00001", version: "1.0.0", protocol: localAuthenticationProtocol,
	}
	archive := &Archive{Descriptor: Descriptor{AID: "APP00001"}}
	network := newAuthenticationHandshakeNetwork(contract, archive, "01012345678", "Emulator")
	if network == nil || len(network.authenticationRequest) != 60 {
		t.Fatal("valid handshake did not produce a request contract")
	}
	for split := 1; split < len(network.authenticationRequest); split++ {
		socket := &notificationSocketState{}
		if !network.writeRequest(socket, network.authenticationRequest[:split]) || len(socket.response) != 0 ||
			!network.writeRequest(socket, network.authenticationRequest[split:]) {
			t.Fatalf("request split at %d failed", split)
		}
		if !bytes.Equal(socket.response, []byte{'K', 'P', 8, 0, 7, 0, 12, 0}) {
			t.Fatalf("response at split %d = %x", split, socket.response)
		}
	}
	for index := range network.authenticationRequest {
		changed := bytes.Clone(network.authenticationRequest)
		changed[index] ^= 0x80
		socket := &notificationSocketState{}
		if network.writeRequest(socket, changed) || len(socket.response) != 0 {
			t.Fatalf("changed request byte %d accepted", index)
		}
	}
	socket := &notificationSocketState{}
	trailing := append(bytes.Clone(network.authenticationRequest), 0)
	if network.writeRequest(socket, trailing) || network.writeRequest(socket, bytes.Repeat([]byte{'x'}, 61)) {
		t.Fatal("invalid request length accepted")
	}
	if newAuthenticationHandshakeNetwork(contract, &Archive{Descriptor: Descriptor{AID: "OTHER"}}, "01012345678", "Emulator") != nil ||
		newAuthenticationHandshakeNetwork(contract, archive, "01012345678", "") != nil {
		t.Fatal("mismatched archive or empty model accepted")
	}
}

func TestAuthenticationHandshakeSessionSelectionAndOptOut(t *testing.T) {
	_, contractCode := authenticationHandshakeFixture(t, 0)
	code, entry, initFunction, startClet, handleEvent, pauseClet, resumeClet := fixtureModule()
	copy(contractCode, code)
	archive := zipOf(t, map[string][]byte{
		"app_info": []byte("AID=APP00001\nPID=PF000001\nMClass=Fixture\n"),
		"APP00001.jar": zipOf(t, map[string][]byte{
			binaryModuleName: fixtureELF(contractCode, fixtureData(initFunction, startClet, handleEvent, pauseClet, resumeClet), entry),
		}),
	})
	for _, disabled := range []bool{false, true} {
		session, err := StartSession(context.Background(), archive, SessionOptions{
			DisableAuthentication: disabled, SaveStore: backend.NewDirectorySaveStore(t.TempDir()), Width: 16, Height: 8, MaxSteps: 1 << 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close(context.Background()) })
		want := backend.AuthenticationLGTHandshake
		if disabled {
			want = backend.AuthenticationOff
		}
		if session.Authentication() != want {
			t.Fatalf("disabled %v status = %s, want %s", disabled, session.Authentication(), want)
		}
		if (session.client.notificationNetwork == nil) != disabled {
			t.Fatalf("disabled %v local network presence is wrong", disabled)
		}
	}
}

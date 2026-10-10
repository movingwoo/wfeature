package ktf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

func TestSlotRelayConversationAndRegisteredName(t *testing.T) {
	var service slotRelay
	exchange := func(kind, command uint32, body []byte) []byte {
		t.Helper()
		response, err := service.respond(slotMessage(kind, command, body))
		if err != nil {
			t.Fatal(err)
		}
		if binary.BigEndian.Uint32(response) != uint32(len(response)) || binary.BigEndian.Uint32(response[4:]) != kind || binary.BigEndian.Uint32(response[8:]) != command {
			t.Fatalf("invalid response %x", response)
		}
		return response[12:]
	}
	if got := exchange(1, 1000, []byte{30}); !bytes.Equal(got, []byte{0}) {
		t.Fatalf("handshake=%x", got)
	}
	identity := []byte{3, 1, 2, 3}
	if got := exchange(5, 1400, identity); len(got) != 0 {
		t.Fatalf("identity acknowledgement=%x", got)
	}
	offer := exchange(5, 1410, []byte{1})
	if len(offer) < 3 || offer[0] != 1 || int(offer[1]) != len(offer)-2 || offer[1] > 127 {
		t.Fatalf("invalid offer %x", offer)
	}
	if got := decodeEUCKR(offer[2:]); got != "\uC774 \uAE30\uAE30\uC5D0 \uC2AC\uB86F\uC744 \uC0DD\uC131\uD569\uB2C8\uB2E4." {
		t.Fatalf("unexpected offer encoding %x", offer)
	}
	quote := exchange(5, 1420, nil)
	if int(quote[0]) != len(quote)-1 || quote[0] > 127 {
		t.Fatalf("invalid quote %x", quote)
	}
	// The receipt says the slot holds no character, which is what opens the
	// guest's name screen; asking again does not change it.
	if receipt := exchange(5, 1430, nil); !bytes.Equal(receipt, []byte{0}) {
		t.Fatalf("receipt before a name = %x, want status zero alone", receipt)
	}
	if receipt := exchange(5, 1430, nil); !bytes.Equal(receipt, []byte{0}) {
		t.Fatalf("repeated receipt before a name = %x", receipt)
	}
	if got := exchange(0, 10, nil); len(got) != 0 {
		t.Fatal("heartbeat changed payload")
	}
	name := encodeEUCKR("\uD64D\uAE38\uB3D9")
	if got := exchange(5, 1440, append([]byte{byte(len(name))}, name...)); !bytes.Equal(got, make([]byte, 8)) {
		t.Fatalf("registration reply = %x, want the eight-byte field alone", got)
	}
	if service.phase != 6 || !bytes.Equal(service.label, name) {
		t.Fatalf("registration left phase %d label %x", service.phase, service.label)
	}
	receipt := exchange(5, 1430, nil)
	if receipt[0] != 1 || int(receipt[1]) != len(receipt)-10 || !bytes.Equal(receipt[2:len(receipt)-8], name) || binary.BigEndian.Uint64(receipt[len(receipt)-8:]) != 0 {
		t.Fatalf("receipt after a name = %x, want the registered name", receipt)
	}
	identity[1] = 99
	if service.identity[0] != 1 {
		t.Fatal("retained caller-owned identity")
	}
}

// A conversation an earlier build answered with its own display name stays on
// that name: it was never asked for one, and the guest has the label already.
func TestSlotRelayKeepsAnEarlierBuildsDisplayName(t *testing.T) {
	label := encodeEUCKR(" \uB85C\uCEEC3")
	service := slotRelay{phase: 5, identity: []byte{1, 2, 3}, slot: 2, label: label}
	reply, err := service.respond(slotMessage(5, 1430, nil))
	if err != nil {
		t.Fatal(err)
	}
	if reply[12] != 1 || !bytes.Equal(reply[14:len(reply)-8], label) {
		t.Fatalf("receipt = %x, want the earlier label", reply[12:])
	}
	name := encodeEUCKR("\uD64D\uAE38\uB3D9")
	if _, err := service.respond(slotMessage(5, 1440, append([]byte{byte(len(name))}, name...))); err == nil {
		t.Fatal("a slot that already has a name took another")
	}
	if service.phase != 5 || !bytes.Equal(service.label, label) {
		t.Fatal("a refused registration changed the slot")
	}
}

func TestSlotRelayRejectsMalformedOrOutOfOrderRequests(t *testing.T) {
	name := encodeEUCKR("\uD64D\uAE38\uB3D9")
	register := func(name []byte) []byte { return slotMessage(5, 1440, append([]byte{byte(len(name))}, name...)) }
	for _, test := range []struct {
		name    string
		phase   uint8
		request []byte
	}{
		{"truncated", 0, []byte{0, 0}},
		{"wrong length", 0, []byte{0, 0, 0, 13, 0, 0, 0, 1, 0, 0, 3, 232}},
		{"unknown protocol", 0, slotMessage(1, 1000, []byte{31})},
		{"identity before handshake", 0, slotMessage(5, 1400, []byte{1, 1})},
		{"identity length mismatch", 1, slotMessage(5, 1400, []byte{2, 1})},
		{"empty identity", 1, slotMessage(5, 1400, []byte{0})},
		{"missing slot", 2, slotMessage(5, 1410, nil)},
		{"extra slot data", 2, slotMessage(5, 1410, []byte{0, 1})},
		{"signed slot overflow", 2, slotMessage(5, 1410, []byte{128})},
		{"commit before confirmation", 3, slotMessage(5, 1430, nil)},
		{"commit with extra data", 4, slotMessage(5, 1430, []byte{0})},
		{"name before the receipt", 4, register(name)},
		{"empty name", 5, slotMessage(5, 1440, []byte{0})},
		{"no name at all", 5, slotMessage(5, 1440, nil)},
		{"name length mismatch", 5, slotMessage(5, 1440, append([]byte{byte(len(name) + 1)}, name...))},
		{"signed name length overflow", 5, register(bytes.Repeat([]byte{'A'}, 128))},
		{"name the guest font cannot draw", 5, register([]byte{0x8c, 0x63})},
		{"name with a control byte", 5, register([]byte{'A', 0x0a})},
		{"half a KSC5601 pair", 5, register(name[:len(name)-1])},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := slotRelay{phase: test.phase}
			if _, err := service.respond(test.request); err == nil {
				t.Fatal("accepted invalid request")
			}
			if service.phase != test.phase || len(service.label) != 0 {
				t.Fatal("failed request advanced state")
			}
		})
	}
	named := slotRelay{phase: 6, identity: []byte{1}, label: name}
	if _, err := named.respond(register(name)); err == nil || named.phase != 6 {
		t.Fatal("a second registration was accepted")
	}
}

func TestSlotRelayCreatesZeroBasedSlots(t *testing.T) {
	for _, slot := range []byte{0, 1, 2, 3, 4, 5} {
		t.Run(fmt.Sprint(slot), func(t *testing.T) {
			var service slotRelay
			name := encodeEUCKR(fmt.Sprintf("NAME%d", slot))
			requests := [][]byte{
				slotMessage(1, 1000, []byte{30}),
				slotMessage(5, 1400, []byte{3, 1, 2, 3}),
				slotMessage(5, 1410, []byte{slot}),
				slotMessage(5, 1420, nil),
				slotMessage(5, 1430, nil),
				slotMessage(5, 1440, append([]byte{byte(len(name))}, name...)),
			}
			for _, request := range requests {
				if _, err := service.respond(request); err != nil {
					t.Fatal(err)
				}
			}
			if service.phase != 6 || service.slot != slot || !bytes.Equal(service.label, name) {
				t.Fatalf("slot not committed: %+v", service)
			}
		})
	}
}

func TestRelaySocketFragmentationBoundsAndClose(t *testing.T) {
	var socket relaySocket
	wire, err := encodeRelayFrame(relayFrame{payload: slotMessage(1, 1000, []byte{30})})
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range wire {
		if err := socket.write([]byte{b}); err != nil {
			t.Fatal(err)
		}
		if i < len(wire)-1 && len(socket.incoming) != 0 {
			t.Fatal("response before complete request")
		}
	}
	response, n, err := decodeRelayFrame(socket.incoming)
	if err != nil || n != len(socket.incoming) || !bytes.Equal(response.payload, slotMessage(1, 1000, []byte{0})) {
		t.Fatalf("response=%x consumed=%d error=%v", response.payload, n, err)
	}
	heartbeat, _ := encodeRelayFrame(relayFrame{payload: slotMessage(0, 10, nil)})
	socket.incoming = nil
	if err := socket.write(append(bytes.Clone(heartbeat), heartbeat...)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, n, err := decodeRelayFrame(socket.incoming)
		if err != nil {
			t.Fatal(err)
		}
		socket.incoming = socket.incoming[n:]
	}
	if len(socket.incoming) != 0 {
		t.Fatal("extra response bytes")
	}
	socket.incoming = make([]byte, maxRelayFrameSize-1)
	if err := socket.write(heartbeat); err == nil || !socket.closed || len(socket.incoming) != 0 {
		t.Fatal("unbounded response queue")
	}
	if err := socket.write(nil); err == nil {
		t.Fatal("write after close succeeded")
	}
	socket = relaySocket{}
	if err := socket.write([]byte{0, 255, 255, 255, 255}); err == nil || !socket.closed {
		t.Fatal("oversized prefix accepted")
	}
	socket = relaySocket{}
	if err := socket.write(make([]byte, maxRelayFrameSize+1)); err == nil || !socket.closed {
		t.Fatal("oversized write accepted")
	}
}

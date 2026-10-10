package ktf

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// slotRelay implements the recovered slot-service conversation, not general
// networking. The guest names the character itself: the creation receipt
// says the slot holds no character yet, the guest shows its own name screen
// and applies its own rules to the name, and it registers the name it accepted
// with one more request. Phases 0 to 4 are the handshake, the identity, the
// slot, the confirmation text and the receipt request before it; phase 5 is a
// receipt given and phase 6 a name registered.
//
// An earlier build answered the receipt with a display name of its own — a
// space, the Korean word for "local" and the slot number — so the guest never
// asked for one. A conversation restored from that build is at phase 5 with
// its label set, and it keeps answering with that label.
type slotRelay struct {
	phase    uint8
	identity []byte
	slot     byte
	// label is the slot's character name as the service knows it: the name
	// the guest registered, or the one an earlier build's receipt gave.
	label []byte
}

const slotMessageHeaderSize = 12

func slotMessage(kind, command uint32, body []byte) []byte {
	data := make([]byte, slotMessageHeaderSize+len(body))
	binary.BigEndian.PutUint32(data, uint32(len(data)))
	binary.BigEndian.PutUint32(data[4:], kind)
	binary.BigEndian.PutUint32(data[8:], command)
	copy(data[12:], body)
	return data
}

func (service *slotRelay) respond(payload []byte) ([]byte, error) {
	if len(payload) < slotMessageHeaderSize || len(payload) > maxRelayFrameSize-relayPrefixSize || uint64(binary.BigEndian.Uint32(payload)) != uint64(len(payload)) {
		return nil, fmt.Errorf("invalid slot message length")
	}
	kind, command := binary.BigEndian.Uint32(payload[4:]), binary.BigEndian.Uint32(payload[8:])
	body := payload[12:]
	reply := func(body []byte) ([]byte, error) { return slotMessage(kind, command, body), nil }
	if kind == 0 && command == 10 && len(body) == 0 && service.phase > 0 {
		return reply(nil)
	}
	switch {
	case kind == 1 && command == 1000 && service.phase == 0 && len(body) == 1 && body[0] == 30:
		service.phase = 1
		return reply([]byte{0})
	case kind == 5 && command == 1400 && service.phase == 1 && len(body) > 1 && int(body[0]) == len(body)-1 && body[0] <= 127:
		service.identity = append([]byte(nil), body[1:]...)
		service.phase = 2
		return reply(nil)
	case kind == 5 && command == 1410 && service.phase == 2 && len(body) == 1 && body[0] <= 127:
		service.slot = body[0]
		service.phase = 3
		message := encodeEUCKR("이 기기에 슬롯을 생성합니다.")
		return reply(append([]byte{1, byte(len(message))}, message...))
	case kind == 5 && command == 1420 && service.phase == 3 && len(body) == 0:
		service.phase = 4
		message := encodeEUCKR("비용은 청구되지 않습니다.")
		return reply(append([]byte{byte(len(message))}, message...))
	case kind == 5 && command == 1430 && service.phase >= 4 && len(body) == 0:
		service.phase = max(service.phase, 5)
		if len(service.label) == 0 {
			// Status zero is a slot with no character in it. The guest reads
			// that byte alone and opens its name screen.
			return reply([]byte{0})
		}
		// The name is consumed directly by the guest bitmap font renderer;
		// arbitrary binary identifiers are not valid here. The following
		// eight-byte field retains the local service value of zero.
		result := append([]byte{1, byte(len(service.label))}, service.label...)
		result = append(result, make([]byte, 8)...)
		return reply(result)
	case kind == 5 && command == 1440 && service.phase == 5 && len(service.label) == 0 &&
		len(body) > 1 && int(body[0]) == len(body)-1 && body[0] <= 127 && displayableKSC5601(body[1:]):
		// The guest sends the name it accepted as its EUC-KR bytes. What it
		// reads back is only the eight-byte field a receipt ends with, which
		// it stores where the receipt's goes; zero, as there.
		service.label = bytes.Clone(body[1:])
		service.phase = 6
		return reply(make([]byte, 8))
	default:
		return nil, fmt.Errorf("unsupported slot request kind=%d command=%d phase=%d length=%d", kind, command, service.phase, len(body))
	}
}

// displayableKSC5601 reports whether a name is printable ASCII and KSC5601
// pairs only, which is what the guest's bitmap font draws. Host text is held to
// the same set before it reaches a field (see encodeKSC5601), so a name the
// guest registers fails this only if it came from somewhere else.
func displayableKSC5601(name []byte) bool {
	for index := 0; index < len(name); index++ {
		switch {
		case name[index] >= 0x20 && name[index] < 0x7f:
		case name[index] >= 0xa1 && name[index] < 0xff && index+1 < len(name) && name[index+1] >= 0xa1 && name[index+1] < 0xff:
			index++
		default:
			return false
		}
	}
	return true
}

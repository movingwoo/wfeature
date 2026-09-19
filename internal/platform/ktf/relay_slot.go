package ktf

import (
	"encoding/binary"
	"fmt"
)

// slotRelay implements the recovered slot-service conversation, not general
// networking. The creation reply carries an EUC-KR display name, which the
// guest both persists and renders in its slot selector.
type slotRelay struct {
	phase    uint8
	identity []byte
	slot     byte
	label    []byte
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
	case kind == 5 && command == 1410 && service.phase == 2 && len(body) == 1 && body[0] > 0 && body[0] <= 127:
		service.slot = body[0]
		service.phase = 3
		message := encodeEUCKR("\uC774 \uAE30\uAE30\uC5D0 \uC2AC\uB86F\uC744 \uC0DD\uC131\uD569\uB2C8\uB2E4.")
		return reply(append([]byte{1, byte(len(message))}, message...))
	case kind == 5 && command == 1420 && service.phase == 3 && len(body) == 0:
		service.phase = 4
		message := encodeEUCKR("\uBE44\uC6A9\uC740 \uCCAD\uAD6C\uB418\uC9C0 \uC54A\uC2B5\uB2C8\uB2E4.")
		return reply(append([]byte{byte(len(message))}, message...))
	case kind == 5 && command == 1430 && (service.phase == 4 || service.phase == 5) && len(body) == 0:
		if service.phase == 4 {
			service.label = encodeEUCKR(fmt.Sprintf(" \uB85C\uCEEC%d", int(service.slot)+1))
			service.phase = 5
		}
		// The name is consumed directly by the guest bitmap font renderer;
		// arbitrary binary identifiers are not valid here. The following
		// eight-byte field retains the local service value of zero.
		result := append([]byte{1, byte(len(service.label))}, service.label...)
		result = append(result, make([]byte, 8)...)
		return reply(result)
	default:
		return nil, fmt.Errorf("unsupported slot request kind=%d command=%d phase=%d length=%d", kind, command, service.phase, len(body))
	}
}

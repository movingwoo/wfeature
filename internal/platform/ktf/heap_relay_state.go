package ktf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/movingwoo/wfeature/internal/jvm"
)

const heapRelayHeaderSize = 20

func validateHeapRelay(socket *relaySocket) error {
	service := &socket.service
	if service.phase > 5 || len(service.identity) > 127 || len(service.label) > 255 || service.slot > 127 ||
		(service.phase >= 2) != (len(service.identity) > 0) || (service.phase == 5) != (len(service.label) > 0) || service.phase < 3 && service.slot != 0 ||
		len(socket.pending) > maxRelayFrameSize || len(socket.incoming) > maxRelayFrameSize {
		return fmt.Errorf("KTF relay state has invalid phase or data bounds")
	}
	if socket.closed && (!socket.socketClosed || !socket.inputClosed || !socket.outputClosed || len(socket.pending) != 0 || len(socket.incoming) != 0) {
		return fmt.Errorf("KTF closed relay retains open handles or queued bytes")
	}
	if len(socket.pending) != 0 {
		if _, _, err := decodeRelayFrame(socket.pending); !errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("KTF pending relay frame is not an incomplete envelope")
		}
	}
	return nil
}

func captureHeapRelay(socket *relaySocket) (jvm.HeapExternalPayload, error) {
	if socket == nil {
		return jvm.HeapExternalPayload{}, fmt.Errorf("KTF relay payload is null")
	}
	if err := validateHeapRelay(socket); err != nil {
		return jvm.HeapExternalPayload{}, err
	}
	for i, stream := range []*jvm.Object{socket.input, socket.output} {
		if stream != nil && (stream.Native != socket || stream.ClassName != []string{runtimeRelayInputClass, runtimeRelayOutputClass}[i]) {
			return jvm.HeapExternalPayload{}, fmt.Errorf("KTF relay stream has invalid ownership")
		}
	}
	data := make([]byte, heapRelayHeaderSize, heapRelayHeaderSize+len(socket.service.identity)+len(socket.service.label)+len(socket.pending)+len(socket.incoming))
	data[0], data[1] = socket.service.phase, socket.service.slot
	for i, flag := range []bool{socket.closed, socket.socketClosed, socket.inputClosed, socket.outputClosed} {
		if flag {
			data[2] |= 1 << i
		}
	}
	for i, value := range [][]byte{socket.service.identity, socket.service.label, socket.pending, socket.incoming} {
		binary.LittleEndian.PutUint32(data[4+i*4:], uint32(len(value)))
		data = append(data, value...)
	}
	return jvm.HeapExternalPayload{Kind: "ktf-relay-v1", Data: data, References: []*jvm.Object{socket.input, socket.output}}, nil
}

// The returned byte slices borrow data until the actual native restore copies
// them. This also validates records before the JVM adopts its detached heap.
func parseHeapRelay(data []byte) (*relaySocket, error) {
	if len(data) < heapRelayHeaderSize || len(data) > heapRelayHeaderSize+127+255+2*maxRelayFrameSize || data[2]&^15 != 0 || data[3] != 0 {
		return nil, fmt.Errorf("KTF relay payload header is invalid")
	}
	values := [4][]byte{}
	offset := uint64(heapRelayHeaderSize)
	for i := range values {
		size := uint64(binary.LittleEndian.Uint32(data[4+i*4:]))
		if size > uint64(len(data))-offset {
			return nil, fmt.Errorf("KTF relay payload is truncated")
		}
		values[i] = data[offset : offset+size]
		offset += size
	}
	if offset != uint64(len(data)) {
		return nil, fmt.Errorf("KTF relay payload has trailing data")
	}
	socket := &relaySocket{service: slotRelay{phase: data[0], slot: data[1], identity: values[0], label: values[1]}, pending: values[2], incoming: values[3],
		closed: data[2]&1 != 0, socketClosed: data[2]&2 != 0, inputClosed: data[2]&4 != 0, outputClosed: data[2]&8 != 0}
	if err := validateHeapRelay(socket); err != nil {
		return nil, err
	}
	return socket, nil
}

func restoreHeapRelay(payload jvm.HeapExternalPayload) (*relaySocket, error) {
	if len(payload.References) != 2 {
		return nil, fmt.Errorf("KTF relay stream reference count is invalid")
	}
	socket, err := parseHeapRelay(payload.Data)
	if err != nil {
		return nil, err
	}
	socket.service.identity, socket.service.label = bytes.Clone(socket.service.identity), bytes.Clone(socket.service.label)
	socket.pending, socket.incoming = bytes.Clone(socket.pending), bytes.Clone(socket.incoming)
	socket.input, socket.output = payload.References[0], payload.References[1]
	return socket, nil
}

func validateRelayHeap(saved runtimeHeapState) error {
	heap := &saved.JVM
	if len(heap.Objects) > maxHeapRootRecords || len(heap.Payloads) > maxHeapRootRecords {
		return fmt.Errorf("KTF relay heap exceeds object limit")
	}
	payloadFor := func(object uint32) uint32 {
		if object == 0 || uint64(object) > uint64(len(heap.Objects)) {
			return 0
		}
		return heap.Objects[object-1].Native
	}
	isRelay := func(id uint32) bool {
		return id > 0 && uint64(id) <= uint64(len(heap.Payloads)) && heap.Payloads[id-1].Kind == "external" && heap.Payloads[id-1].ExternalKind == "ktf-relay-v1"
	}
	if root := saved.Roots.RelaySocket; root != 0 {
		if uint64(root) > uint64(len(heap.Roots)) || !isRelay(payloadFor(heap.Roots[root-1])) {
			return fmt.Errorf("KTF runtime relay root has no socket payload")
		}
	}
	for i, payload := range heap.Payloads {
		if !isRelay(uint32(i + 1)) {
			continue
		}
		if _, err := parseHeapRelay(payload.Data); err != nil {
			return err
		}
		if len(payload.References) != 2 {
			return fmt.Errorf("KTF relay stream reference count is invalid")
		}
		for index, ref := range payload.References {
			if ref == 0 {
				continue
			}
			if payloadFor(ref) != uint32(i+1) || heap.Objects[ref-1].Class != []string{runtimeRelayInputClass, runtimeRelayOutputClass}[index] {
				return fmt.Errorf("KTF relay stream reference has different ownership or class")
			}
		}
	}
	return nil
}

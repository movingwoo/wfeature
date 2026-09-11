package lgt

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// notificationNetwork is an in-process notification receipt and empty remote-save
// service. It cannot open an OS socket, send SMS, upload saves or fetch data.
// Guest choice and ordinary guest file writes remain authoritative.
type notificationNetwork struct {
	contract notificationContract
	identity string
	active   bool
	next     uint32
	serial   uint64
	sockets  map[uint32]*notificationSocketState
}
type notificationCallback struct {
	address, param uint32
	serial         uint64
}
type notificationSocketState struct {
	connected            bool
	failed               bool
	stage                uint8
	request, response    []byte
	connect, read, write notificationCallback
}

func (n *notificationNetwork) acceptsDial(callback uint32) bool {
	return n != nil && (callback == n.contract.dials[0] || callback == n.contract.dials[1])
}
func (n *notificationNetwork) close() {
	if n != nil {
		n.active = false
		n.sockets = nil
	}
}

// writeRequest accepts bounded stream fragments. Only exact session identity and
// application tokens can complete a recognized request. Trailing data, reordered
// commands, uploads and unknown operations fail without a success response.
func (n *notificationNetwork) writeRequest(s *notificationSocketState, data []byte) bool {
	if s.failed || len(s.response) != 0 || len(data) > 100-len(s.request) {
		return false
	}
	s.request = append(s.request, data...)
	suffix := n.identity + " " + n.contract.application
	var candidates [][]byte
	if s.stage == 0 {
		for _, choice := range []string{"N", "Y"} {
			b := make([]byte, 100)
			copy(b, "SMSAGREE "+suffix+" "+choice)
			candidates = append(candidates, b)
		}
		candidates = append(candidates, []byte("IS_SAVEDATA_EXIST "+suffix))
	} else if s.stage == 1 {
		candidates = append(candidates, []byte("FINISH_SAVEDATA "+suffix))
	}
	for i, candidate := range candidates {
		if !bytes.HasPrefix(candidate, s.request) {
			continue
		}
		if len(s.request) < len(candidate) {
			return true
		}
		if s.stage == 1 {
			s.response = []byte{7, 0, 0}
			s.stage = 2
		} else if i == 2 {
			s.response = []byte{2, 0, 0, 0, 0, 0, 0, 0}
			s.stage = 1
		} else {
			s.response = make([]byte, 100)
			s.response[0] = byte(i + 2)
			s.stage = 2
		}
		s.request = nil
		return true
	}
	return false
}

// Called under the ordinary WIPI service lock. Callback execution happens later,
// between guest calls, as required by the same boundary as timers and dial results.
func (client *Client) notificationSocketCall(thread *armcore.Thread, slot uint32) int32 {
	n := client.notificationNetwork
	if n == nil || !n.active {
		return wipiError
	}
	var args [5]uint32
	count := 3
	if slot == slotNetSocketConnect {
		count = 5
	}
	for i := 0; i < count; i++ {
		v, err := client.wipicArgument(thread, i)
		if err != nil {
			return wipiError
		}
		args[i] = v
	}
	if slot == slotNetSocket || slot == slotNetSocketStandard {
		if args[0] != 2 || args[1] != 1 || len(n.sockets) >= 4 || n.next >= math.MaxInt32-100 {
			return wipiError
		}
		if n.sockets == nil {
			n.sockets = make(map[uint32]*notificationSocketState)
		}
		n.next++
		fd := n.next + 100
		n.sockets[fd] = &notificationSocketState{}
		return int32(fd)
	}
	fd := args[0]
	s := n.sockets[fd]
	if s == nil {
		return wipiError
	}
	switch slot {
	case slotNetSocketClose:
		delete(n.sockets, fd)
		return 0
	case slotNetSocketConnect:
		if s.connected || s.connect.address != 0 || args[1] != n.contract.address || uint16(args[2]) != n.contract.port || args[3] != n.contract.socketCallback {
			return wipiError
		}
		n.serial++
		s.connect = notificationCallback{args[3], args[4], n.serial}
		return 0
	case slotNetSetReadCB:
		n.serial++
		s.read = notificationCallback{args[1], args[2], n.serial}
		return 0
	case slotNetSetWriteCB:
		n.serial++
		s.write = notificationCallback{args[1], args[2], n.serial}
		return 0
	case slotNetSocketWrite:
		if !s.connected || s.failed || args[2] > 100 {
			return wipiError
		}
		if args[2] == 0 {
			return 0
		}
		data := make([]byte, args[2])
		if err := client.core.Memory().Read(args[1], data); err != nil {
			return wipiError
		}
		if !n.writeRequest(s, data) {
			s.failed = true
			s.request = nil
			s.response = nil
			return wipiError
		}
		return int32(len(data))
	case slotNetSocketRead:
		if !s.connected || s.failed || args[2] > 65536 {
			return wipiError
		}
		if args[2] == 0 {
			return 0
		}
		if len(s.response) == 0 {
			return -19
		} // M_E_WOULDBLOCK, also consumed by the recognized read pump.
		count := min(int(args[2]), len(s.response))
		if err := client.core.Memory().Write(args[1], s.response[:count]); err != nil {
			return wipiError
		}
		s.response = s.response[count:]
		return int32(count)
	}
	return wipiError
}

func (client *Client) serviceNotificationSockets(ctx context.Context) error {
	// At most three one-shot callbacks per socket. Snapshot identities, then check
	// each registration again: an earlier callback can close a socket or replace
	// another callback before it is dispatched.
	type event struct {
		fd   uint32
		s    *notificationSocketState
		kind uint8
		cb   notificationCallback
	}
	client.mu.Lock()
	n := client.notificationNetwork
	if n == nil {
		client.mu.Unlock()
		return nil
	}
	events := make([]event, 0, 12)
	fds := make([]uint32, 0, len(n.sockets))
	for fd := range n.sockets {
		fds = append(fds, fd)
	}
	slices.Sort(fds)
	for _, fd := range fds {
		s := n.sockets[fd]
		if s.connect.address != 0 {
			events = append(events, event{fd, s, 0, s.connect})
		}
		if s.read.address != 0 && (s.failed || len(s.response) > 0) {
			events = append(events, event{fd, s, 1, s.read})
		}
		if s.write.address != 0 && s.connected {
			events = append(events, event{fd, s, 2, s.write})
		}
	}
	client.mu.Unlock()
	for _, e := range events {
		client.mu.Lock()
		if n.sockets[e.fd] != e.s {
			client.mu.Unlock()
			continue
		}
		s := e.s
		registration := &s.connect
		if e.kind == 1 {
			registration = &s.read
		} else if e.kind == 2 {
			registration = &s.write
		}
		if *registration != e.cb {
			client.mu.Unlock()
			continue
		}
		*registration = notificationCallback{}
		result := int32(0)
		if s.failed {
			result = wipiError
		} else if e.kind == 0 {
			s.connected = true
		}
		client.mu.Unlock()
		if _, err := client.call(ctx, e.cb.address, []uint32{e.fd, uint32(result), e.cb.param}); err != nil {
			return fmt.Errorf("run LGT local protocol callback: %w", err)
		}
	}
	return nil
}

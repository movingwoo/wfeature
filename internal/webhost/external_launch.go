package webhost

import "sync/atomic"

// externalLaunchMailbox is the browser Host's bounded handoff from one guest.
// It intentionally holds only the first request until the page acknowledges
// it. The guest does not wait for that acknowledgement, so accumulating every
// timer-produced handoff would only grow a Host queue the guest cannot observe.
type externalLaunchMailbox struct {
	pending externalLaunchMessage
}

// Request identities span mailbox and game lifetimes. A delayed acknowledgement
// from a stopped game must not equal the first request of the next game on the
// same connection.
var externalLaunchSerial atomic.Uint64

func (m *externalLaunchMailbox) request(destination string) {
	if m == nil || m.pending.Request != 0 {
		return
	}
	request := externalLaunchSerial.Add(1)
	if request == 0 {
		request = externalLaunchSerial.Add(1)
	}
	m.pending = externalLaunchMessage{Request: request, URL: destination}
}

func (m *externalLaunchMailbox) snapshot() (externalLaunchMessage, bool) {
	if m == nil || m.pending.Request == 0 {
		return externalLaunchMessage{}, false
	}
	return m.pending, true
}

func (m *externalLaunchMailbox) acknowledge(request uint64) {
	if m != nil && request != 0 && m.pending.Request == request {
		m.pending = externalLaunchMessage{}
	}
}

// flushExternalLaunchRequest turns the standing mailbox entry into one event
// per connection. A resumed connection resets the edge and sees the request
// again; duplicate guest calls cannot fill the outbound queue.
func (r *sessionRunner) flushExternalLaunchRequest() {
	pending, ok := r.externalLaunch.snapshot()
	if !ok || pending.Request == r.externalLaunchRequest {
		return
	}
	r.externalLaunchRequest = pending.Request
	r.send(serverMessage{Kind: serverExternalLaunch, ExternalLaunch: &pending})
}

func (r *sessionRunner) handleExternalLaunch(message clientMessage) {
	if r.game != nil {
		r.externalLaunch.acknowledge(message.Request)
	}
	// A stale acknowledgement is harmless. It belongs only to the Host notice
	// and never enters guest code or clears a newer request.
	r.send(serverMessage{Kind: serverResult, ID: message.ID})
}

package ktf

import "github.com/movingwoo/wfeature/internal/platform/wipicrt"

// Where a tick's time goes.
//
// A Host that runs badly on one machine and well on another cannot be
// diagnosed from the outside: the machine serving a session is whatever the
// user left running, and its slowness is not visible in the emulator's own
// numbers. So the tick reports its own breakdown, and a report taken on the
// slow machine says which phase grew.
//
// Counting is unconditional because an increment costs nothing. Timing is not,
// and the clock is read often enough to be worth measuring rather than
// assuming, so a Host asks for phase timing explicitly and the cost of asking
// is attributed rather than hidden.

// TimeHostPhases turns phase timing on or off. It is off by default because
// the clock reads it adds are themselves a suspect on a slow host.
func (session *Session) TimeHostPhases(on bool) {
	if session == nil || session.Client == nil {
		return
	}
	session.Client.Client.TimeHostPhases(on)
}

// HostCosts reports what this session has spent, by phase.
func (session *Session) HostCosts() wipicrt.HostCosts {
	if session == nil || session.Client == nil {
		return wipicrt.HostCosts{}
	}
	return session.Client.Client.HostCosts()
}

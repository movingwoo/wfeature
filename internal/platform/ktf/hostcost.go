package ktf

import (
	"fmt"
	"strings"
	"time"
)

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

// phaseCost is one phase's total and its single worst occurrence. The worst is
// what a report needs to tell a host that is uniformly slow from one that runs
// well except for occasional stalls seconds long: the averages look similar and
// only one of them is a game you can play.
type phaseCost struct {
	Total time.Duration
	Worst time.Duration
}

// HostCosts is what one session has spent, by phase.
type HostCosts struct {
	Rounds uint64
	// ClockReads is how often the session has asked the host for the time.
	// A pacing bug shows up here first: the count moves long before the
	// durations do.
	ClockReads uint64
	Timers     phaseCost
	Threads    phaseCost
	Events     phaseCost
	Paint      phaseCost
	Cheat      phaseCost
	Audio      phaseCost
	Collect    phaseCost
	// PaintsDropped is how many rounds gave up their card paint to keep the
	// guest's logic on schedule. Zero on a host that is keeping up.
	PaintsDropped uint64
	// PaintLoad is the ratio that decided those drops: what an entry costs
	// against the wait the guest asked for afterwards. Anything under 1 is a
	// Host with time to spare. It is reported because a report that says
	// paints were dropped without saying how oversubscribed the Host was
	// leaves the reader guessing between "the machine is slow" and "the Host
	// is charging the guest for its own work".
	PaintLoad float64
	// Timed says whether the durations mean anything; a session that was
	// never asked to time its phases reports zero for all of them.
	Timed bool
}

// String renders the breakdown for a debug report, longest phase first.
func (costs HostCosts) String() string {
	if costs.Rounds == 0 {
		return ""
	}
	summary := fmt.Sprintf("rounds=%d paints_dropped=%d paint_load=%.2f clock_reads=%d (%.0f/round)",
		costs.Rounds, costs.PaintsDropped, costs.PaintLoad, costs.ClockReads,
		float64(costs.ClockReads)/float64(costs.Rounds))
	if !costs.Timed {
		return summary
	}
	phases := []struct {
		name string
		cost phaseCost
	}{
		{"threads", costs.Threads},
		{"paint", costs.Paint},
		{"events", costs.Events},
		{"timers", costs.Timers},
		{"collect", costs.Collect},
		{"audio", costs.Audio},
		{"cheat", costs.Cheat},
	}
	total := time.Duration(0)
	for _, phase := range phases {
		total += phase.cost.Total
	}
	parts := make([]string, 0, len(phases))
	for _, phase := range phases {
		if phase.cost.Total == 0 {
			continue
		}
		share := 0.0
		if total > 0 {
			share = 100 * phase.cost.Total.Seconds() / total.Seconds()
		}
		// Averaging over rounds a phase did not run in understates it, and
		// paint is exactly that case once a saturated host starts dropping
		// them: dividing its total by every round made a phone look as fast
		// as a desktop at painting when it was twenty times slower.
		over := costs.Rounds
		if phase.name == "paint" {
			over = costs.Rounds - costs.PaintsDropped
		}
		if over == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%v/worst %v(%.0f%%)",
			phase.name, (phase.cost.Total/time.Duration(over)).Round(time.Microsecond),
			phase.cost.Worst.Round(time.Millisecond), share))
	}
	return summary + " per round: " + strings.Join(parts, " ")
}

// TimeHostPhases turns phase timing on or off. It is off by default because
// the clock reads it adds are themselves a suspect on a slow host.
func (session *Session) TimeHostPhases(on bool) {
	if session == nil || session.Client == nil {
		return
	}
	session.Client.timePhases = on
}

// HostCosts reports what this session has spent, by phase.
func (session *Session) HostCosts() HostCosts {
	if session == nil || session.Client == nil {
		return HostCosts{}
	}
	costs := session.Client.costs
	costs.Timed = session.Client.timePhases
	costs.PaintsDropped = session.Client.paintsDrop
	costs.PaintLoad = session.Client.paintLoad
	return costs
}

// phaseClock reads the clock only when phase timing is on, so a Host that did
// not ask for it pays nothing.
func (client *Client) phaseClock() time.Time {
	if client == nil || !client.timePhases {
		return time.Time{}
	}
	return time.Now()
}

// sincePhase adds the time since a phase started to its total, keeping the
// worst single occurrence alongside it.
func (client *Client) sincePhase(started time.Time, cost *phaseCost) {
	if client == nil || !client.timePhases {
		return
	}
	elapsed := time.Since(started)
	cost.Total += elapsed
	if elapsed > cost.Worst {
		cost.Worst = elapsed
	}
}

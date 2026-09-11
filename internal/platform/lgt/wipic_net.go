package lgt

import (
	"context"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The network block normally reports failure. A recognized offline notification
// contract has a bounded in-process responder; see authentication_notification.go.
// MC_netConnect still owns the asynchronous result contract, because
// it cannot report an accepted dial through its return value alone.
//
//	M_Int32 MC_netConnect(NETCONNECTCB cb, void *param)
//	typedef void (*NETCONNECTCB)(M_Int32 error, void *param)
//
// The specification says a zero return schedules the result callback, while
// a call that returns an error never invokes its callback. Answering the error alone is therefore only half an answer.
// A title that checks the return sees the refusal, but one that ignores it and
// waits for the callback — which is how the asynchronous half of this API is
// meant to be used — waits forever, and a local title does exactly that.
//
// So the dial is accepted and then fails, which is what a handset with no
// coverage does: the attempt starts, the radio finds nothing, and the callback
// says so. Both kinds of caller then reach the same offline path.
const (
	// netConnectFailureDelay is how long the refusal takes to arrive. It is
	// deliberately not immediate: a callback that fires before the caller has
	// finished entering its own "connecting" state is a failure delivered to a
	// state machine that is not listening yet, and the game then waits for a
	// second one that never comes. Four ticks is long enough for the caller to
	// settle and far short of any dial timeout a game would run.
	netConnectFailureDelay = 200 * time.Millisecond
)

// pendingNetConnect is one accepted dial waiting to report its result.
// Each dial carries its own callback and parameter rather than replacing a
// single slot, so every accepted call is answered exactly once.
type pendingNetConnect struct {
	offline    bool
	generation uint64
	callback   uint32
	param      uint32
	dueAt      time.Duration
}

// connectNetwork serves MC_netConnect by accepting a bounded number of dials
// and scheduling each result. Like the timer calls it runs under the lock handleWIPICSVC already
// holds, so it must not take that lock itself.
func (client *Client) connectNetwork(thread *armcore.Thread) int32 {
	callback, err := thread.Register(0)
	if err != nil {
		return wipiError
	}
	param, err := thread.Register(1)
	if err != nil {
		return wipiError
	}
	if len(client.netConnects) >= 64 {
		return wipiError
	}
	if callback == 0 {
		// With no callback there is nothing that could carry the refusal
		// later, so it goes back now as the rest of the block's does.
		return wipiError
	}
	client.netConnects = append(client.netConnects, pendingNetConnect{
		callback:   callback,
		generation: client.netGeneration,
		offline:    client.notificationNetwork.acceptsDial(callback),
		param:      param,
		dueAt:      time.Duration(client.clock.millis())*time.Millisecond + netConnectFailureDelay,
	})
	return wipiSuccess
}

// cancelNetConnects drops the dials that have not reported yet. MC_netClose
// ends the application's internet access, so a refusal arriving after it would
// be answering a question the game has already withdrawn. It runs under the
// same lock as connectNetwork.
func (client *Client) cancelNetConnects() {
	client.netGeneration++
	client.netConnects = nil
	client.notificationNetwork.close()
}

// serviceNetConnects reports the dials whose result is due. Like the timers it
// runs between guest calls and never inside one, so a callback cannot reenter
// the guest mid-frame.
func (client *Client) serviceNetConnects(ctx context.Context) error {
	now := time.Duration(client.clock.millis()) * time.Millisecond
	client.mu.Lock()
	var due, waiting []pendingNetConnect
	for _, entry := range client.netConnects {
		if entry.dueAt <= now {
			due = append(due, entry)
			continue
		}
		waiting = append(waiting, entry)
	}
	client.netConnects = waiting
	client.mu.Unlock()

	// wipiError is M_E_ERROR, which is what the specification names as the
	// callback's failure value.
	for _, entry := range due {

		client.mu.Lock()
		if entry.generation != client.netGeneration {
			client.mu.Unlock()
			continue
		}
		failure := wipiError
		if entry.offline {
			client.notificationNetwork.active = true
			failure = 0
		}
		client.mu.Unlock()
		if _, err := client.call(ctx, entry.callback, []uint32{uint32(failure), entry.param}); err != nil {
			return fmt.Errorf("run LGT network callback at %#x: %w", entry.callback, err)
		}
	}
	return client.serviceNotificationSockets(ctx)
}

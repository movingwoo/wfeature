package ktf

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// There is no network here, and the interesting question is not whether to
// refuse but *how*. `MC_netConnect(cb, param)` has two documented ways to say
// no, and they are not interchangeable:
//
//   - return an error, in which case the specification is explicit that the
//     callback is never called ("이 함수가 에러 값을 리턴할 경우에는 이 함수로
//     등록하는 콜백이 불리지 않는다"), or
//   - return 0, in which case the callback is what reports success or failure:
//     `void cb(M_Int32 error, void *param)` with `error` 0 or `M_E_ERROR`.
//
// Both are legal. Refusing synchronously is the one this platform used to take,
// and it leaves a title that only has the callback path with nothing at all: one
// local title asks once, is refused, and sits on its "waiting for the server"
// screen for twenty thousand ticks without touching the platform again — no
// retry, no timeout of its own, no dialog. It is not hung on anything this
// runtime can see; it is waiting for a call it is owed.
//
// A handset out of coverage answers the second way, because a radio reports
// failure when it has finished trying rather than at the call. So a caller that
// registers a callback now gets `0` and a callback carrying `M_E_ERROR`, which
// is the same "no" delivered where the game is listening for it. This is the
// KTF record-database lesson again, in a different table: a call that answers
// "no" is not a safe default — it is a value the game will believe, and the
// shape of the "no" decides whether the game can act on it.
//
// **A caller that passes no callback keeps the old answer**, because there is
// nowhere to deliver the failure and the synchronous error is then the only
// "no" available. Titles that never register one cannot be affected by this.
type wipicNetCallback struct {
	callback uint32
	param    uint32
}

// maxPendingNetCallbacks bounds the queue. The callback address comes from the
// guest, and a title looping on connect would otherwise grow this without end.
const maxPendingNetCallbacks = 16

// handleWIPICNetCall services the net table.
func (runtime *initializationRuntime) handleWIPICNetCall(thread *armcore.Thread, function uint32) (uint32, error) {
	switch function {
	case wipicNetConnect:
		callback, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		param, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		if callback == 0 {
			runtime.countDiagnostic("wipic net connect refused")
			return wipiErrorCode, nil
		}
		if len(runtime.pendingNetCallbacks) >= maxPendingNetCallbacks {
			runtime.countDiagnostic("wipic net connect refused")
			return wipiErrorCode, nil
		}
		runtime.pendingNetCallbacks = append(runtime.pendingNetCallbacks,
			wipicNetCallback{callback: callback, param: param})
		runtime.countDiagnostic("wipic net connect failing through its callback")
		return 0, nil
	case wipicNetClose:
		// Closing drops every callback the net API registered, which the
		// specification requires and which also keeps a title that gives up
		// before the failure arrives from being called back afterwards.
		runtime.pendingNetCallbacks = nil
		runtime.countDiagnostic("wipic net close")
		return 0, nil
	default:
		// The rest of the table cannot be served without a connection, and
		// refusing is what a game's own error path expects. The same decision
		// org.kwis.msf.io.Network already makes on the Java side.
		runtime.countDiagnostic(fmt.Sprintf("wipic net function %d refused", function))
		return wipiErrorCode, nil
	}
}

// serviceNetCallbacks delivers the failures owed to callers of MC_netConnect.
// They run where timer callbacks run, for the same reason: it is a point where
// the guest is between its own calls and the platform may enter it.
func (client *Client) serviceNetCallbacks(ctx context.Context) (int, error) {
	pending := client.runtime.pendingNetCallbacks
	if len(pending) == 0 {
		return 0, nil
	}
	client.runtime.pendingNetCallbacks = nil
	for _, entry := range pending {
		if _, err := client.core.Call(
			ctx,
			client.thread,
			entry.callback,
			ReturnAddress,
			[]uint32{wipiErrorCode, entry.param},
			client.runtime.handleSupervisorCall,
		); err != nil {
			return 0, fmt.Errorf("deliver KTF network failure to %#x: %w", entry.callback, err)
		}
	}
	return len(pending), nil
}

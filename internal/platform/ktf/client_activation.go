package ktf

import (
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/textinput"
)

// clientActivation exists only while a validated client is detached. Retaining
// the restored editors here also reaches editors not currently focused. No
// extra clock work runs on ordinary ticks or key delivery.
type clientActivation struct {
	client      *Client
	constructed time.Time
	editors     []*textinput.State
	vibration   backend.VibrationState
}

// activate cannot fail after the durable generation commits. All offsets and
// device records were validated during detached construction, and no worker
// has received a grant since then. The owning session supplies that exclusion.
func (activation *clientActivation) activate() {
	client := activation.client
	if client == nil {
		return
	}
	now := client.Now()
	rebase := func(instant time.Time) time.Time {
		return captureHeapDeadline(instant, activation.constructed).restore(now)
	}
	// The guest clock anchor is never a missing deadline, even at epoch zero.
	client.runtime.ClockBase = now.Add(client.runtime.ClockBase.Sub(activation.constructed))
	client.runtime.serialDueAt = rebase(client.runtime.serialDueAt)
	for i := range client.runtime.PendingTimers {
		client.runtime.PendingTimers[i].Due = rebase(client.runtime.PendingTimers[i].Due)
	}
	for _, worker := range client.workers {
		worker.wakeAt = rebase(worker.wakeAt)
	}
	client.ClientWakeAt = rebase(client.ClientWakeAt)
	client.lastPaint = rebase(client.lastPaint)
	client.NextRoundPaint = rebase(client.NextRoundPaint)
	for _, editor := range activation.editors {
		editor.RebaseClock(activation.constructed, now)
	}
	client.Audio.ActivateOutputClock()
	// This is the already validated record applied to the motor's own clock.
	_ = client.Vibrator.RestoreState(activation.vibration)
	*activation = clientActivation{}
}

package ktf

import (
	"context"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// cInputOffered decides whether the C input method can take a Host
// composition. Under the first rules that is its live editor: activated on the
// card on top, kept alive by the input-method activity of every press, or
// answered by a recognized controller's own state.
//
// The relaxed rule exists because for an unknown caller that activity is the
// only evidence there is, and a widget that takes a press without calling the
// input method — a cursor move, a soft key — looks closed afterwards although
// it is still on the screen. So once a title of unknown shape has used its
// input method at all, its editor is offered on whatever card is on top. A
// carrier no widget takes reaches the game as the key it is, once: it fails
// that commit, and the offer waits for the input method to be called again or
// for the player to press a key (see cInputState.unreached). A recognized
// controller answers for its own editor and is held to that under both rules.
//
// A title that runs its own event loop takes keys from its queue in its own
// time, so whether a carrier was consumed is unknown; a vendor form that owns
// the keypad takes every press, so a carrier would reach the form rather than
// the card (see dispatchKeyToVendorForm). sync brings a recognized
// controller's recorded state up to date, which a snapshot has always done and
// a commit does not.
func (client *Client) cInputOffered(relaxed, sync bool) bool {
	runtime := client.runtime
	state := &runtime.CInput
	active := state.Active
	if state.Owner.Address != 0 {
		live := state.Owner.Current(runtime.Runtime)
		if sync && state.Active != live {
			state.Active = live
			state.Revision++
		}
		active = live
	}
	if runtime.GuestEventLoop || len(runtime.displayCards) == 0 {
		return false
	}
	if _, form := runtime.activeVendorTextInput(); form {
		return false
	}
	if relaxed && state.Owner.Address == 0 {
		return runtime.CInputUsed() && !state.Unreached
	}
	return active && runtime.cInputCardOwner == runtime.cInputCard()
}

// cTextInput offers completed text to the C widget. The caller holds
// client.run. C timer delivery is completed before returning; general guest
// event loops remain excluded because consumption is unknown. Keypad language
// modes are not field constraints: completed OS text is validated for
// encoding, while the guest retains its own content policy.
func (client *Client) cTextInput(target textInputTarget) *backend.TextInput {
	runtime := client.runtime
	state := &runtime.CInput
	revision, mode := state.Revision, state.Mode
	owner := state.Owner
	cards := len(runtime.displayCards)
	card := runtime.displayCards[cards-1]
	return &backend.TextInput{Append: true, MaxLength: wipicrt.MaxCInputLength, InputMode: "text",
		Commit: func(ctx context.Context, text string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			client.Run.Lock()
			defer client.Run.Unlock()
			// The input method is still the editor a snapshot would choose,
			// for the same controller, on the same card, and no key or
			// input-method call since has spent the snapshot.
			unchanged := func() bool {
				if client.runtime != runtime || client.workersStopped || state.Owner != owner ||
					state.Revision != revision || state.Mode != mode ||
					len(runtime.displayCards) != cards || runtime.displayCards[cards-1] != card {
					return false
				}
				current, ok := client.resolveTextInputLocked(textInputResolution{})
				return ok && current.identifies(target)
			}
			if !unchanged() || len(state.Pending) != 0 || state.DiscardCarrier {
				return backend.ErrTextInputChanged
			}
			encoded, err := wipicrt.ValidateCInput(text)
			if err != nil {
				return err
			}
			if len(encoded) == 0 {
				return nil
			}
			inserted := false
			defer func() {
				state.Pending = nil
				state.PendingValid = nil
				if inserted {
					state.Revision++
				}
			}()
			state.PendingValid = unchanged
			// Only the guest runs between here and the deferred reset, so an
			// input-method call it makes is part of delivering this text.
			state.Delivering = true
			defer func() { state.Delivering = false }()
			defer client.BeginHostService(ctx)()
			previousThread, previousContext := runtime.CurrentThread, runtime.CurrentContext
			runtime.CurrentThread, runtime.CurrentContext = client.Thread, ctx
			defer func() { runtime.CurrentThread, runtime.CurrentContext = previousThread, previousContext }()
			// A completion buffer holds one automaton result, not the entire field.
			// Replay complete EUC-KR characters through bounded guest key callbacks.
			// Never split a two-byte character or infer a field limit from this buffer.
			for offset := 0; offset < len(encoded); {
				if err := ctx.Err(); err != nil {
					return err
				}
				// The check after the previous character answers for this
				// one too: no guest code runs between them.
				if offset == 0 && !unchanged() {
					return backend.ErrTextInputChanged
				}
				size := 1
				if encoded[offset] >= 0x80 {
					size = 2
				}
				calls := state.Calls
				if state.TimerCallback != 0 {
					if err := client.waitCInputTimerLocked(ctx); err != nil {
						return err
					}
				}
				state.Pending = encoded[offset : offset+size]
				// A carrier is a key: an uncaught guest exception ends that
				// key, as it does for SendKey, and consumption decides the rest.
				err := client.absorbUncaughtCallback("key", runtime.dispatchKeyToCards(wipicrt.KeyPressed, wipicrt.CInputCarrier))
				if err == nil && state.Calls == calls && state.Revision == revision && len(state.Pending) != 0 && state.TimerCallback != 0 {
					_, err = client.ServiceTimersLocked(ctx, wipicrt.MaxPendingTimers)
				}
				consumed := len(state.Pending) == 0
				reached := state.Calls != calls
				inserted = inserted || consumed
				state.DiscardCarrier = !consumed && !reached && state.TimerCallback != 0
				if err != nil {
					return err
				}
				if !reached {
					// The carrier never reached the input method, so the card
					// took it as the key it is. Its release follows, as it
					// would a person's, so the key is not left down. Nothing
					// on this screen takes keys through the input method, so
					// the editor is not active, and the relaxed rule waits for
					// new evidence before it offers one again; this snapshot is
					// spent with it.
					state.Pending = nil
					state.Active, state.Unreached = false, true
					state.Revision++
					if err := client.absorbUncaughtCallback("key", runtime.dispatchKeyToCards(wipicrt.KeyReleased, wipicrt.CInputCarrier)); err != nil {
						return err
					}
					return backend.ErrTextInputChanged
				}
				if !unchanged() {
					return backend.ErrTextInputChanged
				}
				if !consumed {
					if state.Revision != revision {
						state.Active = false
						return backend.ErrTextInputChanged
					}
					return backend.ErrInvalidTextInput
				}
				offset += size
			}
			return nil
		},
	}
}

// Wait before injecting a key, so cancellation or a removed timer cannot leave
// an undelivered character carrier in the guest's queue. A C card may queue its
// keys for the timer that activated its input method. The caller holds the run
// lock and services due timers through the ordinary scheduler after dispatch.
func (client *Client) waitCInputTimerLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state := &client.runtime.CInput
	for _, timer := range client.runtime.PendingTimers {
		if timer.Task != nil || timer.Pointer != state.TimerPointer || timer.Callback != state.TimerCallback {
			continue
		}
		due := timer.Due
		if client.ClientWakeAt.After(due) {
			due = client.ClientWakeAt
		}
		wait := due.Sub(client.Now())
		if wait > time.Second {
			return backend.ErrTextInputChanged
		}
		if wait > 0 {
			if manual, ok := client.Clock.(*wipicrt.ManualClock); ok {
				manual.Set(due)
			} else {
				alarm := time.NewTimer(wait)
				defer alarm.Stop()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-alarm.C:
				}
			}
		}
		return nil
	}
	return backend.ErrTextInputChanged
}

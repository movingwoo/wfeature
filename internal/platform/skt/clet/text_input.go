package clet

import (
	"context"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// Text entry through the page's text box.
//
// A Clet's text field is the program's own: it draws the field, keeps the
// value and the cursor, and composes characters through the input-method
// functions, handing each key it receives to MC_imHandleInput with a buffer
// for the completed text. The platform cannot read or replace that value. What
// it can do is what the input method on a handset did: complete text. So the
// page's text is delivered one character at a time through the key the field
// already takes. A carrier key goes to the program's event entry, the field
// hands it to the input method, and the input method answers with the next
// character as the completed text — the field then appends it under its own
// rules, its length limit and its redraw included.
//
// That makes the input method's activity the evidence there is a field: a
// Clet has no widget the platform could see, and WIPI-C names none. Once the
// program has used its input method — set a mode, asked for its modes, or
// handed it a key — a field is offered. A field that takes a press without
// calling the input method looks closed afterwards although it is still on
// the screen, so the offer stays until a carrier reaches no input method: the
// program took it as the key it is, so nothing on this screen takes keys
// through the input method, and the offer waits for the program to call the
// input method again or for the player to press a key. A screen offered input
// it has no field for costs a commit that fails; a field the player cannot
// type into is the failure that matters.

// TextInput snapshots the program's input method for a Host that composes text
// with its own keyboard. Guest execution and this snapshot share the client
// run lock.
func (session *Session) TextInput(ctx context.Context) (*backend.TextInput, error) {
	if session == nil || session.Client == nil {
		return nil, backend.ErrNoTextInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := session.Client
	client.Run.Lock()
	defer client.Run.Unlock()
	if !client.textInputReady() || !client.cInputOffered(true) {
		return nil, backend.ErrNoTextInput
	}
	return client.cTextInput(), nil
}

// textInputReady reports that the program is running and can be handed keys.
func (client *Client) textInputReady() bool {
	return client.runtime != nil && !client.workersStopped && client.clet != nil && !client.clet.destroyStarted
}

// cInputOffered decides whether the input method can take a Host composition.
// A recognized controller answers for its own field (see cInputOwner); for any
// other program the input method's use is the evidence, as the overview above
// says. sync brings a recognized controller's recorded state up to date, which
// a snapshot does and a commit does not.
func (client *Client) cInputOffered(sync bool) bool {
	runtime := client.runtime
	state := &runtime.CInput
	if state.Owner.Address != 0 {
		live := state.Owner.Current(runtime.Runtime)
		if sync && state.Active != live {
			state.Active = live
			state.Revision++
		}
		return live
	}
	// A program that drains its own event queue takes keys in its own time,
	// so whether a carrier was consumed would be unknown. A Clet's events are
	// handed to it, so this holds for none today.
	if runtime.GuestEventLoop {
		return false
	}
	return runtime.CInputUsed() && !state.Unreached
}

// cTextInput offers completed text to the program's field. The caller holds
// client.run. Keypad language modes are not field constraints: completed text
// is checked for its encoding, and the program keeps its own content rules.
func (client *Client) cTextInput() *backend.TextInput {
	runtime := client.runtime
	state := &runtime.CInput
	revision, mode, owner := state.Revision, state.Mode, state.Owner
	return &backend.TextInput{Append: true, MaxLength: wipicrt.MaxCInputLength, InputMode: "text",
		Commit: func(ctx context.Context, text string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			client.Run.Lock()
			defer client.Run.Unlock()
			// The input method is still the one a snapshot would offer, for
			// the same controller, and no key or input-method call since has
			// spent the snapshot.
			unchanged := func() bool {
				return client.runtime == runtime && client.textInputReady() && state.Owner == owner &&
					state.Revision == revision && state.Mode == mode && client.cInputOffered(false)
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
			// Only the program runs between here and the deferred reset, so an
			// input-method call it makes is part of delivering this text.
			state.Delivering = true
			defer func() { state.Delivering = false }()
			defer client.BeginHostService(ctx)()
			// A completion buffer holds one result of the input method, not the
			// whole field: the text goes a complete character at a time, and a
			// two-byte character is never split.
			for offset := 0; offset < len(encoded); {
				if err := ctx.Err(); err != nil {
					return err
				}
				// The check after the previous character answers for this one
				// too: no guest code runs between them.
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
				// The carrier is a key, and a key ends the program on a fault
				// the way SendKey's does.
				err := client.sendCletEvent(ctx, CletEventKeyPressed, wipicrt.CInputCarrier, 0)
				if err == nil && state.Calls == calls && state.Revision == revision && len(state.Pending) != 0 && state.TimerCallback != 0 {
					// A field that takes its keys on the timer that opened it
					// has queued this one; its round is due now.
					_, err = client.ServiceTimersLocked(ctx, wipicrt.MaxPendingTimers)
				}
				consumed := len(state.Pending) == 0
				reached := state.Calls != calls
				inserted = inserted || consumed
				state.DiscardCarrier = !consumed && !reached && state.TimerCallback != 0
				if err != nil {
					return err
				}
				state.Pending = nil
				// The press is followed by its release, as a person's is. The
				// event entry takes both, and a program that keeps its own key
				// state would otherwise hold the carrier down after the text.
				if err := client.sendCletEvent(ctx, CletEventKeyReleased, wipicrt.CInputCarrier, 0); err != nil {
					return err
				}
				if !reached {
					// The carrier reached no input method: the program took it
					// as the key it is. Nothing on this screen takes keys
					// through the input method, so the offer waits for new
					// evidence, and this snapshot is spent with it.
					state.Active, state.Unreached = false, true
					state.Revision++
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

// waitCInputTimerLocked waits for the timer that opened the input method to
// fall due before a carrier is sent, so a cancellation or a removed timer
// cannot leave an undelivered carrier in the program's queue. The caller holds
// the run lock and services the timer through the ordinary round afterwards.
func (client *Client) waitCInputTimerLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state := &client.runtime.CInput
	for _, timer := range client.runtime.PendingTimers {
		if timer.Pointer != state.TimerPointer || timer.Callback != state.TimerCallback {
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
		if wait <= 0 {
			return nil
		}
		// A Host that moves its own clock is asked to move it; any such clock
		// answers Set, whichever platform's package made it.
		if manual, ok := client.Clock.(interface{ Set(time.Time) }); ok {
			manual.Set(due)
			return nil
		}
		alarm := time.NewTimer(wait)
		defer alarm.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-alarm.C:
		}
		return nil
	}
	return backend.ErrTextInputChanged
}

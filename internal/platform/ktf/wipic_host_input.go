package ktf

import (
	"context"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/movingwoo/wfeature/internal/backend"
	"golang.org/x/text/encoding/korean"
)

const maxCInputLength = 64

// cTextInputLocked offers completed text to the currently active C widget.
// The caller holds client.run. C timer delivery is completed before returning;
// general guest event loops remain excluded because consumption is unknown.
// Keypad language modes are not field constraints: completed OS text is
// validated for encoding, while the guest retains its own content policy.
func (client *Client) cTextInputLocked() (*backend.TextInput, error) {
	runtime := client.runtime
	state := &runtime.cInput
	if !state.active || runtime.guestEventLoop || len(runtime.displayCards) == 0 || state.card != runtime.cInputCard() {
		return nil, backend.ErrNoTextInput
	}
	revision, mode := state.revision, state.mode
	cards := len(runtime.displayCards)
	card := runtime.displayCards[cards-1]
	shell := client.shellTextInput()
	vendor, vendorActive := runtime.activeVendorTextInput()
	focus := runtime.runtimeObjects["lwc:focus"]
	return &backend.TextInput{Append: true, MaxLength: maxCInputLength, InputMode: "text",
		Commit: func(ctx context.Context, text string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			client.run.Lock()
			defer client.run.Unlock()
			unchanged := func() bool {
				if client.runtime != runtime || client.workersStopped {
					return false
				}
				currentVendor, currentVendorActive := runtime.activeVendorTextInput()
				return runtime.runtimeObjects["lwc:focus"] == focus && client.shellTextInput() == shell &&
					currentVendorActive == vendorActive && (!vendorActive || sameVendorTextInputState(vendor, currentVendor)) &&
					!runtime.guestEventLoop && state.active && state.revision == revision && state.mode == mode &&
					len(runtime.displayCards) == cards && runtime.displayCards[cards-1] == card
			}
			if !unchanged() || len(state.pending) != 0 || state.discardCarrier {
				return backend.ErrTextInputChanged
			}
			encoded, err := validateCInput(text)
			if err != nil {
				return err
			}
			if len(encoded) == 0 {
				return nil
			}
			inserted := false
			defer func() {
				state.pending = nil
				state.pendingValid = nil
				if inserted {
					state.revision++
				}
			}()
			state.pendingValid = unchanged
			defer client.beginHostService(ctx)()
			previousThread, previousContext := runtime.currentThread, runtime.currentContext
			runtime.currentThread, runtime.currentContext = client.thread, ctx
			defer func() { runtime.currentThread, runtime.currentContext = previousThread, previousContext }()
			// A completion buffer holds one automaton result, not the entire field.
			// Replay complete EUC-KR characters through bounded guest key callbacks.
			// Never split a two-byte character or infer a field limit from this buffer.
			for offset := 0; offset < len(encoded); {
				if err := ctx.Err(); err != nil {
					return err
				}
				if !unchanged() {
					return backend.ErrTextInputChanged
				}
				size := 1
				if encoded[offset] >= 0x80 {
					size = 2
				}
				calls := state.calls
				if state.timerCallback != 0 {
					if err := client.waitCInputTimerLocked(ctx); err != nil {
						return err
					}
				}
				state.pending = encoded[offset : offset+size]
				err := runtime.dispatchKeyToCards(KeyPressed, cInputCarrier)
				if err == nil && state.calls == calls && state.revision == revision && len(state.pending) != 0 && state.timerCallback != 0 {
					_, err = client.serviceTimersLocked(ctx, maxPendingTimers)
				}
				consumed := len(state.pending) == 0
				inserted = inserted || consumed
				state.discardCarrier = !consumed && state.calls == calls && state.timerCallback != 0
				if err != nil {
					return err
				}
				if !unchanged() {
					return backend.ErrTextInputChanged
				}
				if !consumed {
					if state.calls == calls || state.revision != revision {
						state.active = false
						return backend.ErrTextInputChanged
					}
					return backend.ErrInvalidTextInput
				}
				offset += size
			}
			return nil
		},
	}, nil
}

// Wait before injecting a key, so cancellation or a removed timer cannot leave
// an undelivered character carrier in the guest's queue. A C card may queue its
// keys for the timer that activated its input method. The caller holds the run
// lock and services due timers through the ordinary scheduler after dispatch.
func (client *Client) waitCInputTimerLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state := &client.runtime.cInput
	for _, timer := range client.runtime.pendingTimers {
		if timer.task != nil || timer.pointer != state.timerPointer || timer.callback != state.timerCallback {
			continue
		}
		due := timer.due
		if client.clientWakeAt.After(due) {
			due = client.clientWakeAt
		}
		wait := due.Sub(client.now())
		if wait > time.Second {
			return backend.ErrTextInputChanged
		}
		if wait > 0 {
			if manual, ok := client.clock.(*ManualClock); ok {
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

func validateCInput(text string) ([]byte, error) {
	if err := backend.ValidateTextInput(text); err != nil || utf8.RuneCountInString(text) > maxCInputLength {
		return nil, backend.ErrInvalidTextInput
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return nil, backend.ErrInvalidTextInput
		}
	}
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return nil, backend.ErrInvalidTextInput
	}
	return encoded, nil
}

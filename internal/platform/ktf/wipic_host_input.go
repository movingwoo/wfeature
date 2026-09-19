package ktf

import (
	"context"
	"unicode"
	"unicode/utf8"

	"github.com/movingwoo/wfeature/internal/backend"
	"golang.org/x/text/encoding/korean"
)

const maxCInputLength = 64

// cTextInputLocked offers completed text to the currently active C widget.
// The caller holds client.run. Queued event loops are excluded because a
// commit must know whether the guest consumed the text before returning.
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
			if client.runtime != runtime || client.workersStopped {
				return backend.ErrTextInputChanged
			}
			currentVendor, currentVendorActive := runtime.activeVendorTextInput()
			if runtime.runtimeObjects["lwc:focus"] != focus || client.shellTextInput() != shell ||
				currentVendorActive != vendorActive || (vendorActive && !sameVendorTextInputState(vendor, currentVendor)) ||
				runtime.guestEventLoop || !state.active ||
				state.revision != revision || state.mode != mode || len(state.pending) != 0 ||
				len(runtime.displayCards) != cards || runtime.displayCards[cards-1] != card {
				return backend.ErrTextInputChanged
			}
			encoded, err := validateCInput(text)
			if err != nil {
				return err
			}
			if len(encoded) == 0 {
				return nil
			}
			calls := state.calls
			state.pending = encoded
			defer func() { state.pending = nil }()
			defer client.beginHostService(ctx)()
			previousThread, previousContext := runtime.currentThread, runtime.currentContext
			runtime.currentThread, runtime.currentContext = client.thread, ctx
			defer func() { runtime.currentThread, runtime.currentContext = previousThread, previousContext }()
			err = runtime.dispatchKeyToCards(KeyPressed, cInputCarrier)
			consumed := len(state.pending) == 0
			if consumed {
				state.revision++
			}
			if err != nil {
				return err
			}
			if consumed {
				return nil
			}
			if state.calls == calls || state.revision != revision {
				state.active = false
				return backend.ErrTextInputChanged
			}
			return backend.ErrInvalidTextInput
		},
	}, nil
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

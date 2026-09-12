package skt

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"golang.org/x/text/encoding/korean"
)

const scriptTextInputMaxBytes = 32

type scriptTextInput struct {
	request      uint64
	destination  int
	resourceData []byte
	prompt       string
	text         string
}

func (s *ScriptSession) beginTextInput(vm *sgsvm.VM) error {
	arguments := vm.Args(2)
	promptID, destinationID := int(arguments[0]), int(arguments[1])
	prompt := vm.Resource(promptID)
	destination := vm.Resource(destinationID)
	if err := vm.Error(); err != nil {
		return err
	}
	promptLength, err := scriptResourceStringLength(vm, prompt.Data)
	if err != nil {
		return fmt.Errorf("SGS input prompt: %w", err)
	}
	textLength, err := scriptResourceStringLength(vm, destination.Data)
	if err != nil {
		return fmt.Errorf("SGS input value: %w", err)
	}
	decodedPrompt, err := decodeScriptInputText(prompt.Data[:promptLength])
	if err != nil {
		return fmt.Errorf("SGS input prompt: %w", err)
	}
	decodedText, err := decodeScriptInputText(destination.Data[:textLength])
	if err != nil {
		return fmt.Errorf("SGS input value: %w", err)
	}

	for index := range s.timers {
		s.timers[index].active = false
	}
	s.textInputSerial++
	if s.textInputSerial == 0 {
		s.textInputSerial++
	}
	s.textInput = &scriptTextInput{
		request:      s.textInputSerial,
		destination:  destinationID,
		resourceData: bytes.Clone(destination.Data),
		prompt:       string(decodedPrompt),
		text:         string(decodedText),
	}
	vm.Yield()
	return nil
}

// TextInputRequest identifies the native dialog currently waiting for a Host.
// Zero means no dialog is pending. The number changes for each new request,
// including one opened synchronously by the previous request's callback.
func (s *ScriptSession) TextInputRequest() uint64 {
	if s == nil || s.closed || s.paused || s.Exited() || s.textInput == nil {
		return 0
	}
	return s.textInput.request
}

// TextInput snapshots the pending native dialog for the Host keyboard path.
func (s *ScriptSession) TextInput(ctx context.Context) (*backend.TextInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, backend.ErrTextInputChanged
	}
	if s.TextInputRequest() == 0 {
		return nil, backend.ErrNoTextInput
	}
	pending := s.textInput
	return &backend.TextInput{
		Text:     pending.text,
		Prompt:   pending.prompt,
		MaxBytes: scriptTextInputMaxBytes,
		Commit: func(ctx context.Context, text string) error {
			if err := backend.ValidateTextInput(text); err != nil || strings.IndexByte(text, 0) >= 0 {
				return backend.ErrInvalidTextInput
			}
			encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(text))
			if err != nil || len(encoded) > scriptTextInputMaxBytes {
				return backend.ErrInvalidTextInput
			}
			return s.completeTextInput(ctx, pending.request, encoded, true)
		},
		Cancel: func(ctx context.Context) error {
			return s.completeTextInput(ctx, pending.request, nil, false)
		},
	}, nil
}

func decodeScriptInputText(encoded []byte) ([]byte, error) {
	decoded, err := korean.EUCKR.NewDecoder().Bytes(encoded)
	if err != nil {
		return nil, fmt.Errorf("text is not EUC-KR: %w", err)
	}
	roundTrip, err := korean.EUCKR.NewEncoder().Bytes(decoded)
	if err != nil || !bytes.Equal(roundTrip, encoded) {
		return nil, fmt.Errorf("text is not valid EUC-KR")
	}
	return decoded, nil
}

func (s *ScriptSession) completeTextInput(ctx context.Context, request uint64, encoded []byte, commit bool) error {
	if err := ctx.Err(); err != nil {
		return backend.ErrTextInputChanged
	}
	pending := s.textInput
	if pending == nil || pending.request != request || s.closed || s.paused || s.Exited() {
		return backend.ErrTextInputChanged
	}
	destination := s.vm.Resource(pending.destination)
	if err := s.vm.Error(); err != nil || !bytes.Equal(destination.Data, pending.resourceData) {
		return backend.ErrTextInputChanged
	}
	if commit {
		data := append(bytes.Clone(encoded), 0)
		ok, err := resizeScriptResource(s.vm, destination, len(data))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("SGS input destination could not be resized")
		}
		copy(destination.Data, data)
	}

	// Consume before entering guest code. Callback 6 may synchronously request
	// another dialog or exit; either outcome must remain distinct from this one.
	s.textInput = nil
	return s.event(ctx, 6, 2)
}

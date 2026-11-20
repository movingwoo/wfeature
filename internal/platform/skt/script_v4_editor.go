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

type wideEditor struct {
	media int32
	limit int
	mode  int32
}

func (s *ScriptSession) openWideEditor(vm *sgsvm.WideVM) error {
	a, err := vm.Args(4)
	if err != nil {
		return err
	}
	if _, err := wideResource(vm, a[0], false); err != nil {
		return err
	}
	if _, err := wideResource(vm, a[1], true); err != nil {
		return err
	}
	if a[2] < 0 || a[2] > backend.MaxTextInputBytes || a[3] < 0 || a[3] > 2 {
		return fmt.Errorf("SGS: invalid editor length or mode")
	}
	s.wideEditor = &wideEditor{media: a[1], limit: int(a[2]), mode: a[3]}
	s.wideHeld = 0
	vm.EndEvent()
	return nil
}

func (s *ScriptSession) finishWideEditor(ctx context.Context) error {
	s.wideEditor = nil
	return s.event(ctx, 6, 2)
}

func (s *ScriptSession) TextInput(ctx context.Context) (*backend.TextInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	editor := s.wideEditor
	if editor == nil || s.closed {
		return nil, backend.ErrNoTextInput
	}
	value := s.wide.Resources[editor.media].Data
	size := min(len(value), editor.limit+1)
	value, _, _ = bytes.Cut(value[:size], []byte{0})
	if len(value) > editor.limit {
		return nil, backend.ErrInvalidTextInput
	}
	decoded, err := korean.EUCKR.NewDecoder().Bytes(value)
	if err != nil {
		return nil, err
	}
	snapshot := string(decoded)
	field := &backend.TextInput{Text: snapshot, MaxLength: editor.limit, Password: editor.mode == 2}
	if editor.mode != 0 {
		field.InputMode = "numeric"
	}
	field.Commit = func(ctx context.Context, text string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.wideEditor != editor || s.closed {
			return backend.ErrTextInputChanged
		}
		if err := backend.ValidateTextInput(text); err != nil {
			return err
		}
		encoded, ok := backend.EncodeKSC5601(text)
		if !ok || len(encoded) > editor.limit || strings.IndexByte(text, 0) >= 0 {
			return backend.ErrInvalidTextInput
		}
		if editor.mode != 0 && strings.Trim(text, "0123456789") != "" {
			return backend.ErrInvalidTextInput
		}
		if err := putWideString(s.wide, editor.media, encoded); err != nil {
			return err
		}
		return s.finishWideEditor(ctx)
	}
	return field, nil
}

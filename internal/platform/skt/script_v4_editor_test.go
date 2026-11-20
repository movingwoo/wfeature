package skt

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestWideEditorLimitsAndCommit(t *testing.T) {
	program := &sgsvm.Program{Wide: &sgsvm.WideProgram{Variables: [][]int32{{0}, {0}, {0}, {0}}}, Resources: []sgsvm.Resource{{Data: []byte("Title")}, {Mutable: true, Data: []byte("5\x00")}}}
	vm := sgsvm.NewWide(program, nil)
	s := &ScriptSession{wide: vm, archive: &Archive{Script: program}}
	for _, v := range []int32{0, 1, 3, 1} {
		if err := vm.Push(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.openWideEditor(vm); err != nil {
		t.Fatal(err)
	}
	field, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if field.Text != "5" || field.InputMode != "numeric" {
		t.Fatalf("editor %q %q", field.Text, field.InputMode)
	}
	for _, candidate := range []string{"1234", "abc", "１"} {
		if err := field.Commit(context.Background(), candidate); !errors.Is(err, backend.ErrInvalidTextInput) {
			t.Fatalf("accepted %q: %v", candidate, err)
		}
	}
	if err := field.Commit(t.Context(), "678"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(vm.Resources[1].Data, []byte("678\x00")) || vm.Variables[0][0] != 2 {
		t.Fatal("editor result or callback parameter lost")
	}
	if err := field.Commit(t.Context(), "0"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatal("accepted stale edit")
	}
}

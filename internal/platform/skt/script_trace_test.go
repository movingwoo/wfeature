package skt

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestScriptTraceUsesLoggerAndPreservesState(t *testing.T) {
	for _, op := range []byte{0xf0, 0xf1} {
		for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo} {
			s := newScriptTest(t, []byte{0xff}, nil)
			var output bytes.Buffer
			s.options.Logger = slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: level}))
			format := []byte("%d:%d:%d\x00")
			args := []int16{0, -32768, 0, 32767}
			if op == 0xf1 {
				format = []byte("trace:%s\x00")
				args = []int16{0, 1}
			}
			s.vm.Resources = []sgsvm.Resource{{Data: format}, {Data: []byte{0xb0, 0xa1, 0}}}
			before := bytes.Clone(s.graphics.pixels)
			s.vm.Push(123)
			for _, v := range args {
				s.vm.Push(v)
			}
			if err := s.Call(op, s.vm); err != nil {
				t.Fatal(err)
			}
			if s.vm.Pop() != 123 || !bytes.Equal(before, s.graphics.pixels) || !bytes.Equal(format, s.vm.Resources[0].Data) {
				t.Fatal("trace changed guest state")
			}
			if level == slog.LevelInfo {
				if output.Len() != 0 {
					t.Fatal("trace bypassed log level")
				}
				continue
			}
			if !strings.Contains(output.String(), "SGS trace") {
				t.Fatal("trace missed logging boundary")
			}
			if op == 0xf0 && !strings.Contains(output.String(), "-32768:0:32767") {
				t.Fatalf("wrong scalar order: %q", output.String())
			}
			for _, b := range output.Bytes() {
				if b > 127 {
					t.Fatal("trace emitted non-ASCII guest bytes")
				}
			}
		}
	}
}

func TestScriptTraceRejectsInvalidInputBeforeLogging(t *testing.T) {
	for _, op := range []byte{0xf0, 0xf1} {
		s := newScriptTest(t, []byte{0xff}, nil)
		var out bytes.Buffer
		s.options.Logger = slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
		if err := s.Call(op, s.vm); err == nil {
			t.Fatal("trace accepted stack underflow")
		}
		if out.Len() != 0 {
			t.Fatal("invalid trace emitted log")
		}
	}
}

func TestScriptTraceMalformedResourcesDoNotLog(t *testing.T) {
	for _, tc := range []struct {
		op        byte
		resources []sgsvm.Resource
		args      []int16
	}{
		{0xf0, nil, []int16{0, 1, 2, 3}},
		{0xf0, []sgsvm.Resource{{Data: bytes.Repeat([]byte{'A'}, 4097)}}, []int16{0, 1, 2, 3}},
		{0xf1, []sgsvm.Resource{{Data: []byte("%s\x00")}, {Data: []byte("missing terminator")}}, []int16{0, 1}},
		{0xf1, []sgsvm.Resource{{Data: []byte("%s")}, {Data: []byte("value\x00")}}, []int16{0, 1}},
	} {
		s := newScriptTest(t, []byte{0xff}, nil)
		var out bytes.Buffer
		s.options.Logger = slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
		s.vm.Resources = tc.resources
		for _, v := range tc.args {
			s.vm.Push(v)
		}
		if err := s.Call(tc.op, s.vm); err == nil {
			t.Fatal("malformed trace accepted")
		}
		if out.Len() != 0 {
			t.Fatal("malformed trace emitted output")
		}
	}
}

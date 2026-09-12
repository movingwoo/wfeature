package skt

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestScriptFormatWord(t *testing.T) {
	for _, tt := range []struct {
		format string
		value  int16
		want   string
	}{
		{"%d", -32768, "-32768"}, {"%+06d", 23, "+00023"}, {"%-5d!", 7, "7    !"},
		{"%.4d", 3, "0003"}, {"%5.0d", 0, "     "}, {"%#X", -1, "0XFFFFFFFF"},
		{"%x", 255, "ff"}, {"%c", 65, "A"}, {"%% %d", 9, "% 9"},
		{"\xb0\xa1%d", 12, "\xb0\xa112"}, {"A%cB", 0, "A"},
	} {
		got, err := scriptFormat([]byte(tt.format), tt.value)
		if err != nil || string(got) != tt.want+"\x00" {
			t.Fatalf("format %q: %q, %v; want %q", tt.format, got, err, tt.want)
		}
	}
}

func TestScriptFormatBoundsAndAliasing(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Mutable: true, Data: []byte("%04d\x00")}}
	for _, v := range []int16{0, 0, 12} {
		s.vm.Push(v)
	}
	if err := s.Call(0x8a, s.vm); err != nil {
		t.Fatal(err)
	}
	if string(s.vm.Resources[0].Data) != "0012\x00" {
		t.Fatal("aliased format overwritten")
	}
	got, err := scriptFormat([]byte("%999999999999999999999999d"), 1)
	if err != nil || len(got) != 256 || got[255] != 0 {
		t.Fatalf("unbounded width: %d %v", len(got), err)
	}
	before := []byte("unchanged")
	s.vm.Resources = []sgsvm.Resource{{Data: bytes.Clone(before)}, {Data: []byte("%d%d")}}
	for _, v := range []int16{0, 1, 2} {
		s.vm.Push(v)
	}
	if err := s.Call(0x8a, s.vm); err == nil {
		t.Fatal("missing second argument accepted")
	}
	if !bytes.Equal(s.vm.Resources[0].Data, before) {
		t.Fatal("invalid formatting changed destination")
	}
}

func TestScriptFormatServiceArgumentOrder(t *testing.T) {
	for count := 1; count <= 4; count++ {
		s := newScriptTest(t, []byte{0xff}, nil)
		format, want := "", ""
		for i := 0; i < count; i++ {
			format += "%d/"
			want += string(rune('1'+i)) + "/"
		}
		s.vm.Resources = []sgsvm.Resource{{Mutable: true}, {Data: []byte(format)}}
		s.vm.Push(0)
		s.vm.Push(1)
		for i := 1; i <= count; i++ {
			s.vm.Push(int16(i))
		}
		if err := s.Call(byte(0x89+count), s.vm); err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(s.vm.Resources[0].Data, []byte(want+"\x00")) {
			t.Fatalf("%d arguments: %q", count, s.vm.Resources[0].Data)
		}
	}
}

func TestScriptFormatFiveArguments(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Mutable: true, Data: []byte("old")}, {Data: []byte("%d/%d/%x/%c/%+d\x00")}}
	s.vm.Push(123)
	for _, v := range []int16{0, 1, -32768, 32767, 255, 65, -2} {
		s.vm.Push(v)
	}
	if err := s.Call(0x8e, s.vm); err != nil {
		t.Fatal(err)
	}
	got := s.vm.Resources[0].Data
	if end := bytes.IndexByte(got, 0); end >= 0 {
		got = got[:end]
	}
	if string(got) != "-32768/32767/ff/A/-2" {
		t.Fatalf("five argument format: %q", got)
	}
	if s.vm.Pop() != 123 {
		t.Fatal("format changed caller stack")
	}
}

func TestScriptFormatFiveUnderflowPreservesDestination(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Mutable: true, Data: []byte("unchanged")}}
	for i := 0; i < 6; i++ {
		s.vm.Push(int16(i))
	}
	if err := s.Call(0x8e, s.vm); err == nil {
		t.Fatal("format accepted missing seventh operand")
	}
	if string(s.vm.Resources[0].Data) != "unchanged" || s.vm.Pop() != 5 {
		t.Fatal("format underflow changed state")
	}
}

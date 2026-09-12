package skt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestScriptStringFormatProtocol(t *testing.T) {
	for _, tc := range []struct{ format, source, want string }{
		{"[%s][%s]", "ab", "[ab][ab]"},
		{"[%5s][%-5s][%05s][%-05s]", "ab", "[   ab][ab   ][000ab][ab   ]"},
		{"%% %d %x %c %+s %#s %9q", "unused", "% d x c +s #s q"},
		{"%.2s|%5.2s|%05.2s|%-5.2s", "abcd", "ab|   ab|000ab|ab   "},
		{"[%.s][%.0s]", "abcd", "[][]"},
		{"%s\x00ignored", "ab\x00ignored", "ab"},
		{"trailing%", "unused", "trailing"},
		{"[%s]", "", "[]"},
	} {
		got, err := scriptStringFormat([]byte(tc.format), []byte(tc.source))
		if err != nil || string(got) != tc.want+"\x00" {
			t.Fatalf("format %q source %q: got %q, err %v; want %q", tc.format, tc.source, got, err, tc.want)
		}
	}
}

func TestScriptStringFormatByteCap(t *testing.T) {
	// Output is a byte resource: truncating an EUC-KR pair retains the first
	// byte instead of replacing it with a Unicode replacement character.
	format := []byte(strings.Repeat("a", 254) + "%s")
	got, err := scriptStringFormat(format, []byte{0xb0, 0xa1})
	if err != nil || len(got) != 256 || got[254] != 0xb0 || got[255] != 0 {
		t.Fatalf("byte cap: len=%d result=%v err=%v", len(got), got, err)
	}
	for _, format := range []string{"%" + strings.Repeat("9", 2000) + "s", "%0" + strings.Repeat("9", 2000) + "s", "%." + strings.Repeat("9", 2000) + "s"} {
		got, err := scriptStringFormat([]byte(format), bytes.Repeat([]byte{'x'}, 300))
		if err != nil || len(got) != 256 || got[255] != 0 {
			t.Fatalf("large width/precision: len=%d err=%v", len(got), err)
		}
	}
	if _, err := scriptStringFormat(bytes.Repeat([]byte{'a'}, 4097), nil); err == nil {
		t.Fatal("accepted oversized format")
	}
}

func TestScriptStringFormatResourceAliasing(t *testing.T) {
	for _, target := range []int16{0, 1, 2} {
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{
			{Mutable: true, Data: []byte("old\x00")},
			{Mutable: true, Data: []byte("<%s>:%s\x00")},
			{Mutable: true, Data: []byte("abc\x00")},
		}}, nil)
		vm.Push(1234)
		vm.Push(target)
		vm.Push(1)
		vm.Push(2)
		if err := formatScriptStringResource(vm); err != nil {
			t.Fatal(err)
		}
		got := vm.Resource(int(target)).Data
		if !bytes.HasPrefix(got, []byte("<abc>:abc\x00")) {
			t.Fatalf("destination %d: %q", target, got)
		}
		if vm.Pop() != 1234 {
			t.Fatal("changed surrounding operand stack")
		}
	}
}

func TestScriptStringFormatInvalidInputsAreAtomic(t *testing.T) {
	for _, tc := range []struct {
		format, source []byte
		arguments      []int16
	}{
		{[]byte("%s\x00"), []byte("ok\x00"), []int16{0, 1}},
		{[]byte("%s\x00"), []byte("ok\x00"), []int16{0, 1, 3}},
		{[]byte("%s"), []byte("ok\x00"), []int16{0, 1, 2}},
		{[]byte("%s\x00"), []byte("bad"), []int16{0, 1, 2}},
	} {
		vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{
			{Mutable: true, Data: []byte("before\x00")}, {Mutable: true, Data: tc.format}, {Mutable: true, Data: tc.source},
		}}, nil)
		for _, a := range tc.arguments {
			vm.Push(a)
		}
		if err := formatScriptStringResource(vm); err == nil {
			t.Fatal("accepted invalid input")
		}
		if !bytes.Equal(vm.Resources[0].Data, []byte("before\x00")) {
			t.Fatal("invalid format changed destination")
		}
	}
}

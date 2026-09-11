package skt

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestScriptResourceResizePreservesBytesAndAllocatorSlack(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Data: []byte{1, 2}}}
	for _, test := range []struct{ request, length int }{{5, 6}, {3, 6}, {25, 26}, {2, 2}} {
		s.vm.Push(0)
		s.vm.Push(int16(test.request))
		if err := scriptResourceCall(0x7a, s.vm); err != nil {
			t.Fatal(err)
		}
		if s.vm.Pop() != 1 || len(s.vm.Resources[0].Data) != test.length {
			t.Fatalf("resize %d: got %v", test.request, s.vm.Resources[0].Data)
		}
		want := make([]byte, test.length)
		copy(want, []byte{1, 2})
		if !bytes.Equal(s.vm.Resources[0].Data, want) {
			t.Fatal("resize lost bytes or exposed nondeterministic data")
		}
	}
}

func TestScriptResourceStringCopyUsesDeclaredSizeAndTerminator(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Mutable: true, Data: []byte("xxxxxx")}, {Data: []byte("ab\x00trailing")}}
	s.vm.Push(0)
	s.vm.Push(1)
	if err := scriptResourceCall(0x7d, s.vm); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Resources[0].Data; len(got) != 12 || !bytes.Equal(got[:6], []byte("ab\x00xxx")) {
		t.Fatalf("copy result %q", got)
	}
	s.vm.Push(0)
	if err := scriptResourceCall(0x7c, s.vm); err != nil || s.vm.Pop() != 2 {
		t.Fatalf("string length: %v", err)
	}
}

func TestScriptResourceInvalidCallsDoNotMutate(t *testing.T) {
	for _, test := range []struct {
		name string
		op   byte
		args []int16
	}{{"negative size", 0x7a, []int16{0, -1}}, {"invalid source", 0x7d, []int16{0, 2}}, {"unterminated source", 0x7d, []int16{0, 1}}, {"missing argument", 0x7a, []int16{0}}} {
		t.Run(test.name, func(t *testing.T) {
			s := newScriptTest(t, []byte{0xff}, nil)
			s.vm.Resources = []sgsvm.Resource{{Data: []byte("original")}, {Data: []byte("unterminated")}}
			for _, value := range test.args {
				s.vm.Push(value)
			}
			if err := scriptResourceCall(test.op, s.vm); err == nil {
				t.Fatal("invalid call accepted")
			}
			if string(s.vm.Resources[0].Data) != "original" {
				t.Fatal("invalid call changed destination")
			}
		})
	}
}

func TestScriptResourceResizeLimitReturnsFailureWithoutMutation(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Data: []byte{7, 8}}, {Data: make([]byte, (16<<20)-2)}}
	s.vm.Push(0)
	s.vm.Push(4)
	if err := scriptResourceCall(0x7a, s.vm); err != nil {
		t.Fatal(err)
	}
	if s.vm.Pop() != 0 || !bytes.Equal(s.vm.Resources[0].Data, []byte{7, 8}) {
		t.Fatal("failed allocation changed the resource or reported success")
	}
}

func TestScriptResourceCopyToSelfAndWordSizeBoundary(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Data: []byte("same\x00")}}
	s.vm.Push(0)
	s.vm.Push(0)
	if err := scriptResourceCall(0x7d, s.vm); err != nil {
		t.Fatal(err)
	}
	if string(s.vm.Resources[0].Data) != "same\x00" {
		t.Fatal("self copy corrupted data")
	}
	if ok, err := resizeScriptResource(s.vm, &s.vm.Resources[0], 65535); err != nil || !ok {
		t.Fatalf("maximum representable odd size: %v, %v", ok, err)
	}
	if len(s.vm.Resources[0].Data) != 65535 {
		t.Fatal("maximum size was truncated")
	}
}

func TestScriptResourceSubstring(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		offset int16
		length int16
		want   string
	}{
		{"bounded bytes", "abcdef", 1, 3, "bcd\x00"},
		{"terminator padding", "ab\x00tail", 1, 5, "b\x00\x00\x00\x00\x00"},
		{"terminator before bank end", "ab\x00", 0, 7, "ab\x00\x00\x00\x00\x00\x00"},
		{"empty at bank end", "abc", 3, 0, "\x00"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newScriptTest(t, []byte{0xff}, nil)
			s.vm.Resources = []sgsvm.Resource{{Mutable: true}, {Data: []byte(test.source)}}
			for _, value := range []int16{0, 1, test.offset, test.length} {
				s.vm.Push(value)
			}
			if err := scriptResourceCall(0x7e, s.vm); err != nil {
				t.Fatal(err)
			}
			if got := string(s.vm.Resources[0].Data[:len(test.want)]); got != test.want {
				t.Fatalf("substring = %q, want %q", got, test.want)
			}
		})
	}
}

func TestScriptResourceSubstringSelfCopySurvivesShrink(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Mutable: true, Data: []byte("012345678901234567890123456789")}}
	for _, value := range []int16{0, 0, 25, 4} {
		s.vm.Push(value)
	}
	if err := scriptResourceCall(0x7e, s.vm); err != nil {
		t.Fatal(err)
	}
	if got := string(s.vm.Resources[0].Data); got != "5678\x00" {
		t.Fatalf("self substring = %q", got)
	}
}

func TestScriptResourceSubstringInvalidRangeIsAtomic(t *testing.T) {
	for _, args := range [][]int16{{0, 1, -1, 2}, {0, 1, 0, -1}, {0, 1, 4, 0}, {0, 1, 0, 4}, {0, 1, 3, 1}, {0, 2, 0, 1}, {0, 1, 0}} {
		s := newScriptTest(t, []byte{0xff}, nil)
		s.vm.Resources = []sgsvm.Resource{{Data: []byte("original")}, {Data: []byte("abc")}}
		for _, value := range args {
			s.vm.Push(value)
		}
		if err := scriptResourceCall(0x7e, s.vm); err == nil {
			t.Fatalf("accepted arguments %v", args)
		}
		if string(s.vm.Resources[0].Data) != "original" {
			t.Fatalf("invalid arguments %v changed destination", args)
		}
	}
}

func TestScriptResourceStringCompare(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        int16
	}{
		{"same\x00left", "same\x00right", 0},
		{"ab\x00", "ac\x00", -1},
		{"ac\x00", "ab\x00", 1},
		{"a\x00", "ab\x00", -1},
		{"\xff\x00", "\x7f\x00", 1},
		{"\x00", "\x00", 0},
	} {
		s := newScriptTest(t, []byte{0xff}, nil)
		s.vm.Resources = []sgsvm.Resource{{Data: []byte(test.left)}, {Data: []byte(test.right)}}
		s.vm.Push(0)
		s.vm.Push(1)
		if err := scriptResourceCall(0x80, s.vm); err != nil {
			t.Fatal(err)
		}
		if got := s.vm.Pop(); got != test.want {
			t.Fatalf("compare %q, %q = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestScriptResourceStringCompareRejectsUnterminatedBank(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Data: []byte("unterminated")}, {Data: []byte("other\x00")}}
	s.vm.Push(0)
	s.vm.Push(1)
	if err := scriptResourceCall(0x80, s.vm); err == nil {
		t.Fatal("unterminated string accepted")
	}
}

func TestScriptResourceDecimalConversion(t *testing.T) {
	for _, test := range []struct {
		text string
		want int16
	}{
		{"123tail", 123}, {" \t\n\v\f\r-42", -42}, {"+17", 17},
		{"", 0}, {"nondigit", 0}, {"-", 0}, {"--5", 0}, {"0x10", 0},
		{"32768", -32768}, {"65535", -1}, {"65536", 0},
		{"4294967297", 1}, {"-4294967297", -1},
	} {
		s := newScriptTest(t, []byte{0xff}, nil)
		s.vm.Resources = []sgsvm.Resource{{Data: []byte(test.text + "\x00ignored")}}
		s.vm.Push(0)
		if err := scriptResourceCall(0x83, s.vm); err != nil {
			t.Fatal(err)
		}
		if got := s.vm.Pop(); got != test.want {
			t.Fatalf("decimal %q = %d, want %d", test.text, got, test.want)
		}
	}
}

func TestScriptResourceDecimalConversionRejectsUnterminatedBank(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Data: []byte("123")}}
	s.vm.Push(0)
	if err := scriptResourceCall(0x83, s.vm); err == nil {
		t.Fatal("unterminated decimal resource accepted")
	}
}

func TestScriptResourceConcatenation(t *testing.T) {
	for _, self := range []bool{false, true} {
		s := newScriptTest(t, []byte{0xff}, nil)
		s.vm.Resources = []sgsvm.Resource{{Mutable: true, Data: []byte("left\x00")}, {Data: []byte("right\x00ignored")}}
		s.vm.Push(0)
		want := "leftright\x00"
		if self {
			s.vm.Push(0)
			want = "leftleft\x00"
		} else {
			s.vm.Push(1)
		}
		if err := scriptResourceCall(0x7f, s.vm); err != nil {
			t.Fatal(err)
		}
		if got := string(s.vm.Resources[0].Data[:len(want)]); got != want {
			t.Fatalf("concatenation = %q, want %q", got, want)
		}
	}
}

func TestScriptResourceConcatenationRejectsInvalidInputAtomically(t *testing.T) {
	for _, source := range [][]byte{[]byte("unterminated"), append(bytes.Repeat([]byte{'x'}, 65530), 0)} {
		s := newScriptTest(t, []byte{0xff}, nil)
		s.vm.Resources = []sgsvm.Resource{{Data: []byte("original\x00")}, {Data: source}}
		s.vm.Push(0)
		s.vm.Push(1)
		if err := scriptResourceCall(0x7f, s.vm); err == nil {
			t.Fatal("invalid concatenation accepted")
		}
		if string(s.vm.Resources[0].Data) != "original\x00" {
			t.Fatal("invalid concatenation mutated destination")
		}
	}
}

func TestScriptResourceIntegerFormatting(t *testing.T) {
	for _, test := range []struct {
		value int16
		want  string
	}{{0, "0\x00"}, {32767, "32767\x00"}, {-32768, "-32768\x00"}, {-1, "-1\x00"}} {
		s := newScriptTest(t, []byte{0xff}, nil)
		s.vm.Resources = []sgsvm.Resource{{Mutable: true, Data: []byte("existing trailing bytes")}}
		s.vm.Push(0)
		s.vm.Push(test.value)
		if err := scriptResourceCall(0x84, s.vm); err != nil {
			t.Fatal(err)
		}
		if got := string(s.vm.Resources[0].Data[:len(test.want)]); got != test.want {
			t.Fatalf("integer text = %q, want %q", got, test.want)
		}
	}
}

func TestScriptResourceIntegerFormattingRejectsMissingValue(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Data: []byte("original")}}
	s.vm.Push(0)
	if err := scriptResourceCall(0x84, s.vm); err == nil {
		t.Fatal("missing integer accepted")
	}
	if string(s.vm.Resources[0].Data) != "original" {
		t.Fatal("invalid formatting mutated destination")
	}
}

func TestScriptResourceZeroingRetainsAllocationSlack(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Data: []byte{1, 2, 3, 4, 5, 6}}}
	s.vm.Push(0)
	s.vm.Push(3)
	if err := s.Call(0x7b, s.vm); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s.vm.Resources[0].Data, []byte{0, 0, 0, 4, 5, 6}) {
		t.Fatal("zeroing changed retained allocation slack")
	}
	s.vm.Push(0)
	if err := s.Call(0x79, s.vm); err != nil {
		t.Fatal(err)
	}
	if s.vm.Pop() != 6 {
		t.Fatal("allocation size was replaced with requested size")
	}
}

package skt

import "testing"

func TestScriptRandomNativeSequence(t *testing.T) {
	// Each row is a fresh native run of the same mixed service sequence.
	// Equal bounds do not advance state; impossible percentage tests do.
	cases := []struct {
		seed  int16
		want  [28]int16
		state uint32
	}{
		{-32768, [28]int16{-8664, 9, 0, 1, 75, 1, 20648, -6413, 9, 0, 1, -35, 0, 26572, -27552, 9, 0, 1, -25, 0, 18952, -20574, 9, 0, 1, 69, 1, 6338}, 2562861912},
		{-12345, [28]int16{-7507, 9, 0, 1, -26, 0, 189, -10747, 9, 0, 1, 3, 1, 8633, -18158, 9, 0, 1, 44, 0, 9585, -20081, 9, 0, 1, 18, 0, 3760}, 2393927999},
		{-1, [28]int16{-32733, 9, 0, 1, -46, 1, 7870, -27515, 9, 0, 1, -67, 1, 28056, -20997, 9, 0, 1, -32, 0, 7860, -31602, 9, 0, 1, 75, 1, 18892}, 3385594487},
		{0, [28]int16{-32730, 9, 0, 1, -38, 0, 11797, -24403, 9, 0, 1, 21, 0, 28100, -31626, 9, 0, 1, -18, 1, 26285, -29771, 9, 0, 1, -86, 0, 25906}, 3845303128},
		{1, [28]int16{-32727, 9, 0, 1, 67, 0, 15724, -21290, 9, 0, 1, 11, 1, 28145, -9487, 9, 0, 1, -4, 0, 11942, -27941, 9, 0, 1, 51, 1, 153}, 10044473},
		{42, [28]int16{-32593, 9, 0, 1, 59, 0, 12879, -24752, 9, 0, 1, 86, 1, 29950, -19297, 9, 0, 1, -40, 0, 13712, -18417, 9, 0, 1, -99, 0, 25607}, 1678229570},
		{32767, [28]int16{-24031, 9, 0, 1, -61, 0, 31787, -12737, 9, 0, 1, 90, 0, 29585, -25070, 9, 0, 1, 74, 0, 15192, -8032, 9, 0, 1, 19, 0, 5692}, 373068407},
	}
	for _, tc := range cases {
		s := newScriptTest(t, []byte{0xff}, nil)
		s.vm.Push(1234)
		s.vm.Push(tc.seed)
		if err := s.Call(0xa0, s.vm); err != nil {
			t.Fatal(err)
		}
		for i, want := range tc.want {
			op := byte(0xa1)
			var args []int16
			switch i % 7 {
			case 0:
				args = []int16{-32768, 32767}
			case 1:
				args = []int16{9, 9}
			case 2:
				op = 0xa2
				args = []int16{-1}
			case 3:
				op = 0xa2
				args = []int16{101}
			case 4:
				args = []int16{99, -99}
			case 5:
				op = 0xa2
				args = []int16{50}
			case 6:
				args = []int16{0, 32767}
			}
			for _, arg := range args {
				s.vm.Push(arg)
			}
			if err := s.Call(op, s.vm); err != nil {
				t.Fatal(err)
			}
			if got := s.vm.Pop(); got != want {
				t.Fatalf("seed %d step %d: got %d, want %d", tc.seed, i, got, want)
			}
		}
		if uint32(s.random) != tc.state {
			t.Fatalf("seed %d: state %d, want %d", tc.seed, s.random, tc.state)
		}
		if got := s.vm.Pop(); got != 1234 {
			t.Fatalf("caller stack changed: %d", got)
		}
	}
}

func TestScriptRandomSessionIsolation(t *testing.T) {
	first := newScriptTest(t, []byte{0xff}, nil)
	second := newScriptTest(t, []byte{0xff}, nil)
	for _, want := range []int{41, 18467, 6334} {
		if got := first.random.next(); got != want {
			t.Fatalf("default sequence: got %d, want %d", got, want)
		}
	}
	if got := second.random.next(); got != 41 {
		t.Fatalf("another session changed default state: %d", got)
	}
}

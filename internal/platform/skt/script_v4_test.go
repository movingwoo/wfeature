package skt

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestWideRandomBounds(t *testing.T) {
	source := rand.NewPCG(1, 2)
	s := &ScriptSession{randomSource: source, random: rand.New(source)}
	vm := new(sgsvm.WideVM)
	call := func(id uint16, args ...int32) {
		t.Helper()
		for _, arg := range args {
			if err := vm.Push(arg); err != nil {
				t.Fatal(err)
			}
		}
		if handled, err := s.wideMath(id, vm); !handled || err != nil {
			t.Fatalf("math service %#x: handled %t, error %v", id, handled, err)
		}
	}
	random := func(lo, hi int32) int32 {
		t.Helper()
		call(0xc9, lo, hi)
		values, err := vm.Args(1)
		if err != nil {
			t.Fatal(err)
		}
		return values[0]
	}
	call(0xc8, -315)
	first := random(math.MinInt32, math.MaxInt32)
	call(0xc8, -315)
	if got := random(-7, -7); got != -7 {
		t.Fatalf("equal bounds returned %d", got)
	}
	if got := random(math.MaxInt32, math.MinInt32); got != first || got == math.MaxInt32 {
		t.Fatalf("reversed bounds after a constant result: got %d, want %d", got, first)
	}
	for range 100 {
		if got := random(1, -1); got != 0 && got != -1 {
			t.Fatalf("value outside [-1, 1): %d", got)
		}
	}
}

package lgt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func TestJavaWideArrayInterfaceWordOrder(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.javaArrayType(1, "J", 8)
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.allocateJavaArray(class.Object, 4)
	if err != nil {
		t.Fatal(err)
	}
	// The compiler helper takes (array, index, high, low), while a long
	// result returns low in r0 and high in r1. Include a timer interval and
	// epoch time: swapping either prevents a deadline from expiring.
	for index, value := range []uint64{1000, 1199145600000, 0x0123456789abcdef, 0xfffffffffffffff9} {
		thread := armcore.NewThread(armcore.NewContext())
		for register, word := range []uint32{array, uint32(index), uint32(value >> 32), uint32(value)} {
			if err := thread.SetRegister(register, word); err != nil {
				t.Fatal(err)
			}
		}
		if err := client.handleJavaSVC(t.Context(), thread, javaSVCStoreWide); err != nil {
			t.Fatal(err)
		}
		if err := thread.SetRegister(0, array); err != nil {
			t.Fatal(err)
		}
		if err := client.handleJavaSVC(t.Context(), thread, javaSVCLoadWide); err != nil {
			t.Fatal(err)
		}
		low, err := thread.Register(0)
		if err != nil {
			t.Fatal(err)
		}
		high, err := thread.Register(1)
		if err != nil {
			t.Fatal(err)
		}
		if got := uint64(high)<<32 | uint64(low); got != value {
			t.Fatalf("element %d = %#x, want %#x", index, got, value)
		}
	}
}

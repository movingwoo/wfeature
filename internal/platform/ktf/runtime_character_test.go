package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestCharacterDigitResolvesThroughAOTClass(t *testing.T) {
	client, runtime := newTestRuntime(t)
	address, err := runtime.ensureJavaClass(jvm.CharacterClass)
	if err != nil {
		t.Fatal(err)
	}
	method, found, err := client.vm.FindAOTMethod(address, "isDigit", "(C)Z")
	if err != nil || !found || method.Body == 0 {
		t.Fatalf("Character.isDigit unresolved: found=%v error=%v", found, err)
	}
	for _, test := range []struct {
		ch   int32
		want int32
	}{{'0', 1}, {'9', 1}, {'/', 0}, {':', 0}, {'a', 0}, {0xac00, 0}} {
		result, err := client.vm.InvokeStatic(jvm.CharacterClass, "isDigit", "(C)Z", jvm.IntValue(test.ch))
		if err != nil {
			t.Fatal(err)
		}
		got, err := result.Int32()
		if err != nil || got != test.want {
			t.Fatalf("isDigit(%x) = %d/%v, want %d", test.ch, got, err, test.want)
		}
	}
}

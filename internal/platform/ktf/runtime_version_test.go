package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestHandsetVersionCanBuildRelayIdentity(t *testing.T) {
	client, runtime := newTestRuntime(t)
	key := jvm.ReferenceValue(client.vm.NewString("WIPISTANDARDVERSION"))
	for _, get := range []func(*initializationRuntime, *jvm.VM, []jvm.Value) (jvm.Value, error){runtimeGetSystemProperty, runtimeSystemGetProperty} {
		value, err := get(runtime, client.vm, []jvm.Value{key})
		if err != nil {
			t.Fatal(err)
		}
		object, _ := value.Reference()
		encoded, err := client.vm.InvokeVirtual(object, "getBytes", "()[B")
		if err != nil {
			t.Fatal(err)
		}
		array, _ := encoded.Reference()
		_, bytes, err := jvm.ArraySnapshot(array)
		if err != nil || len(bytes) == 0 {
			t.Fatalf("relay version identity is empty: %v", err)
		}
		first, err := bytes[0].Int32()
		if err != nil || first < '0' || first > '9' {
			t.Fatalf("version must start with an ASCII digit: %d/%v", first, err)
		}
	}
}

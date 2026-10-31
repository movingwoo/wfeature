package ktf

import (
	goruntime "runtime"
	"testing"
)

// TestHookedRoutinesCheckTheRangeBeforeAllocating pins the order a hooked
// memset or memcpy works in. The length is the guest's r2, so a negative
// computed size arrives as nearly four gigabytes; the destination or source
// that cannot hold it has to be refused before the host allocates and fills a
// buffer of that size, not after.
func TestHookedRoutinesCheckTheRangeBeforeAllocating(t *testing.T) {
	client, runtime := newHookRuntime(t)
	// The scratch mapping is 64 KiB, so 64 MiB from it runs off the end.
	const length = 64 << 20
	for _, call := range []struct {
		name      string
		kind      binaryHookKind
		arguments []uint32
	}{
		{name: "memset", kind: hookMemset, arguments: []uint32{hookScratch, 0x55, length}},
		{name: "memcpy", kind: hookMemcpy, arguments: []uint32{hookScratch, hookScratch, length}},
	} {
		t.Run(call.name, func(t *testing.T) {
			for register, value := range call.arguments {
				if err := client.Thread.SetRegister(register, value); err != nil {
					t.Fatal(err)
				}
			}
			var before, after goruntime.MemStats
			goruntime.ReadMemStats(&before)
			_, err := runtime.handleBinaryHookCall(client.Thread, uint32(call.kind))
			goruntime.ReadMemStats(&after)
			if err == nil {
				t.Fatal("a range past the mapping was accepted")
			}
			if grown := after.TotalAlloc - before.TotalAlloc; grown >= length/2 {
				t.Fatalf("the refused call allocated %d bytes first", grown)
			}
			// What lies inside the mapping was not touched either.
			if got := readScratch(t, client, hookScratch, 1); got[0] == 0x55 {
				t.Fatal("memset wrote part of a range it then refused")
			}
		})
	}
}

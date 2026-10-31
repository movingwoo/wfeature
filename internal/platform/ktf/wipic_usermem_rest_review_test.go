package ktf

import (
	goruntime "runtime"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// TestUserMemoryPoolChecksTheRangeBeforeAllocating pins the order the pool
// check works in. The pool's size is the guest's own argument, so a garbage
// size has to be refused against guest memory before the host allocates a
// buffer of that size to read the pool through.
func TestUserMemoryPoolChecksTheRangeBeforeAllocating(t *testing.T) {
	_, runtime := newTestRuntime(t)
	// The platform data area is 64 MiB, so 128 MiB from inside it runs off
	// the end.
	const size = 128 << 20
	thread := armcore.NewThread(armcore.Context{})
	for register, value := range map[int]uint32{0: userMemoryPoolAddress, 1: size} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	var before, after goruntime.MemStats
	goruntime.ReadMemStats(&before)
	_, err := runtime.HandleUserMemoryCall(thread, wipicrt.UserMemoryCreate)
	goruntime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("a pool past the end of guest memory was accepted")
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown >= size/2 {
		t.Fatalf("the refused pool allocated %d bytes first", grown)
	}
	if len(runtime.UserMemoryPools) != 0 {
		t.Fatal("the refused pool was recorded")
	}
}

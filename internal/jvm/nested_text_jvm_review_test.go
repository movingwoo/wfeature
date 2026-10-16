package jvm

import (
	_ "embed"
	"testing"
	"time"
)

//go:embed testdata/NestedText.class
var nestedTextClass []byte

//go:embed testdata/NestedText$Sync.class
var nestedTextSyncClass []byte

//go:embed testdata/NestedText$Item.class
var nestedTextItemClass []byte

//go:embed testdata/NestedText$Locked.class
var nestedTextLockedClass []byte

func nestedTextSource() mapClassSource {
	return mapClassSource{
		"NestedText":        nestedTextClass,
		"NestedText$Sync":   nestedTextSyncClass,
		"NestedText$Item":   nestedTextItemClass,
		"NestedText$Locked": nestedTextLockedClass,
	}
}

// invokeWithin runs one call and fails the test if it has not returned in
// time, which is how a callback that waits on its own caller shows up.
func invokeWithin(t *testing.T, limit time.Duration, call func() (Value, error)) (Value, error) {
	t.Helper()
	type outcome struct {
		value Value
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		value, err := call()
		done <- outcome{value, err}
	}()
	select {
	case result := <-done:
		return result.value, result.err
	case <-time.After(limit):
		t.Fatalf("call did not return within %v", limit)
		return VoidValue(), nil
	}
}

// A toString that a StringBuffer append, String.valueOf or a string
// concatenation asks for, and the reads readUTF makes, are guest code the
// caller is still underneath. Each one has to run as that caller: the monitor
// a synchronized caller holds is the callee's too, and a class initializer
// that formats one of its own instances must not wait for itself to finish.
func TestNativeCallbacksRunOnTheCallersExecution(t *testing.T) {
	for _, probe := range []struct {
		method string
		want   string
	}{
		{"synchronizedAppend", "xsync"},
		{"synchronizedValueOf", "sync"},
		{"synchronizedBufferAppend", "sync"},
		{"initializerAppend", "item:one"},
		{"lockedReadUTF", "ok"},
	} {
		t.Run(probe.method, func(t *testing.T) {
			vm := New(nestedTextSource(), Options{})
			defer vm.Close()
			result, err := invokeWithin(t, 5*time.Second, func() (Value, error) {
				return vm.InvokeStatic("NestedText", probe.method, "()Ljava/lang/String;")
			})
			if err != nil {
				t.Fatalf("%s() error = %v", probe.method, err)
			}
			object, err := result.Reference()
			if err != nil {
				t.Fatal(err)
			}
			if text, _ := StringText(object); text != probe.want {
				t.Fatalf("%s() = %q, want %q", probe.method, text, probe.want)
			}
			if depth := vm.toStringDepth.Load(); depth != 0 {
				t.Fatalf("toString depth after %s() = %d, want 0", probe.method, depth)
			}
		})
	}
}

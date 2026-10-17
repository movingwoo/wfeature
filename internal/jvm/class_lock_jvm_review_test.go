package jvm

import (
	_ "embed"
	"testing"
)

//go:embed testdata/ClassLock.class
var classLockClass []byte

// `synchronized (Foo.class)` and Foo's static synchronized methods take the
// same lock. The fixture notifies and waits on the class literal from inside a
// static synchronized method, which is legal only when the method's lock is
// the class object's.
func TestClassLiteralLockIsTheStaticSynchronizedLock(t *testing.T) {
	vm := New(mapClassSource{"ClassLock": classLockClass}, Options{})
	for _, test := range []struct {
		method string
		want   int32
	}{
		{"notifyFromStaticMethod", 1},
		{"waitInStaticMethod", 2},
		{"notifyFromBlock", 3},
	} {
		result, err := vm.InvokeStatic("ClassLock", test.method, "()I")
		if err != nil {
			t.Fatalf("%s() error = %v", test.method, err)
		}
		if got, _ := result.Int32(); got != test.want {
			t.Fatalf("%s() = %d, want %d", test.method, got, test.want)
		}
	}
	// The lock is let go of: the class monitor is free once the calls return.
	m := vm.classMonitor("ClassLock")
	m.mu.Lock()
	owner, depth := m.owner, m.depth
	m.mu.Unlock()
	if owner != 0 || depth != 0 {
		t.Fatalf("class monitor still held: owner %d depth %d", owner, depth)
	}
}

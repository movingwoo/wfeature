package jvm

import (
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

// Compare backing addresses instead of relying on GC timing or heap statistics:
// an interior string pointer keeps the entire original allocation alive.
func requireDetachedStringBacking(t *testing.T, text, backing string) {
	t.Helper()
	start := uintptr(unsafe.Pointer(unsafe.StringData(backing)))
	pointer := uintptr(unsafe.Pointer(unsafe.StringData(text)))
	shares := pointer >= start && pointer-start < uintptr(len(backing))
	runtime.KeepAlive(text)
	runtime.KeepAlive(backing)
	if shares {
		t.Fatalf("%d-byte string retains a %d-byte backing allocation", len(text), len(backing))
	}
}

func TestStringInternTrimOwnsVisibleText(t *testing.T) {
	vm := internTestVM(t)
	const text = "trimmed interned value"
	padded := vm.NewString(strings.Repeat(" ", 1<<20) + text + " ")
	backing, _ := StringText(padded)
	value, err := vm.InvokeVirtual(padded, "trim", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	trimmed, err := value.Reference()
	if err != nil || trimmed == nil {
		t.Fatalf("trim returned %v, %v", trimmed, err)
	}
	value, err = vm.InvokeVirtual(trimmed, "intern", "()Ljava/lang/String;")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := value.Reference()
	if err != nil || canonical != trimmed {
		t.Fatalf("intern changed its receiver identity: %v", err)
	}
	got, ok := StringText(canonical)
	if !ok || got != text {
		t.Fatalf("interned text = %q, %v; want %q", got, ok, text)
	}
	requireDetachedStringBacking(t, got, backing)
	if requireInternedString(t, vm, text) != canonical || vm.internedStringBytes != len(text) {
		t.Fatal("interned receiver or visible-text budget changed")
	}
}

func TestNativeInternStringDetachesPayloadAndPoolKey(t *testing.T) {
	vm := internTestVM(t)
	const padding = 1 << 20
	const text = "native interned value"
	backing := strings.Repeat("x", padding) + text + strings.Repeat("y", padding)
	input := backing[padding : padding+len(text)]
	canonical := requireInternedString(t, vm, input)
	got, ok := StringText(canonical)
	if !ok || got != text {
		t.Fatalf("interned text = %q, %v; want %q", got, ok, text)
	}
	requireDetachedStringBacking(t, got, backing)
	if len(vm.internedStrings) != 1 || vm.internedStringBytes != len(text) {
		t.Fatal("native interning did not charge exactly the visible text")
	}
	for key, object := range vm.internedStrings {
		if key != text || object != canonical {
			t.Fatal("pool key lost its canonical object")
		}
		requireDetachedStringBacking(t, key, backing)
	}
	if requireInternedString(t, vm, input) != canonical {
		t.Fatal("looking up the original substring changed the canonical object")
	}
}

package ktf

import "testing"

// A compiled caller can resolve setString on TextFieldComponent while its
// receiver is a GTextField. Both classes must agree on the virtual slot.
func TestGTextFieldDispatchThroughTextFieldComponent(t *testing.T) {
	_, runtime := newTestRuntime(t)
	parent, err := runtime.ensureJavaClass(runtimeTextFieldComponentClass)
	if err != nil {
		t.Fatal(err)
	}
	child, err := runtime.ensureJavaClass(runtimeGTextFieldClass)
	if err != nil {
		t.Fatal(err)
	}
	parentTable, err := runtime.inheritedVTable(parent)
	if err != nil {
		t.Fatal(err)
	}
	childTable, err := runtime.inheritedVTable(child)
	if err != nil {
		t.Fatal(err)
	}
	for slot, method := range parentTable.entries {
		if method.name != "setString" || method.descriptor != "(Ljava/lang/String;)V" {
			continue
		}
		if slot >= len(childTable.entries) {
			t.Fatalf("receiver has no slot %d", slot)
		}
		got := childTable.entries[slot]
		if got.name != method.name || got.descriptor != method.descriptor {
			t.Fatalf("TextFieldComponent.setString slot %d dispatches to %s%s", slot, got.name, got.descriptor)
		}
		return
	}
	t.Fatal("TextFieldComponent.setString missing")
}

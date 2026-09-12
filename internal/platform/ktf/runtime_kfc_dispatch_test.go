package ktf

import (
	"github.com/movingwoo/wfeature/internal/jvm"
	"testing"
)

func TestGFormVisibilityThroughPublishedMethods(t *testing.T) {
	client, runtime := newTestRuntime(t)
	for _, class := range []string{runtimeGFormClass, runtimeGMenubarFormClass, runtimeGMsgBoxClass} {
		t.Run(class, func(t *testing.T) {
			object := newWidget(class)
			definition := runtimeGFormClassDefinition(class, runtimeShellComponentClass)
			call := func(name string) {
				t.Helper()
				for _, method := range definition.methods {
					if method.name == name && method.descriptor == "()V" {
						if _, err := method.implementation(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(object)}); err != nil {
							t.Fatal(err)
						}
						return
					}
				}
				t.Fatalf("missing %s", name)
			}
			call("show")
			if got := widgetInt(t, object, componentShownField); got != 1 {
				t.Fatalf("show state = %d", got)
			}
			call("hide")
			if got := widgetInt(t, object, componentShownField); got != 0 {
				t.Fatalf("hide state = %d", got)
			}
		})
	}
}

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

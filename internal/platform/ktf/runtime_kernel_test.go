package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestKernelExecNamesFiltersCurrentApplication(t *testing.T) {
	client, runtime := newTestRuntime(t)
	client.SetProgramName("test-application")
	client.AttachAppProperties(map[string]string{"NAME": "Example", "VER": "1.0", "VDR": "Test", "REQLIB": "required-service|1.0"})
	if _, err := runtime.ensureJavaClass(runtimeKernelClass); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		filters []jvm.Value
		count   int
	}{
		{"all", []jvm.Value{jvm.ReferenceValue(nil), jvm.ReferenceValue(nil), jvm.ReferenceValue(nil)}, 1},
		{"match", []jvm.Value{jvm.ReferenceValue(client.vm.NewString("Example")), jvm.ReferenceValue(client.vm.NewString("1.0")), jvm.ReferenceValue(client.vm.NewString("Test"))}, 1},
		{"different name", []jvm.Value{jvm.ReferenceValue(client.vm.NewString("Missing")), jvm.ReferenceValue(nil), jvm.ReferenceValue(nil)}, 0},
		{"declared dependency is not installed", []jvm.Value{jvm.ReferenceValue(client.vm.NewString("required-service")), jvm.ReferenceValue(nil), jvm.ReferenceValue(nil)}, 0},
		{"different version", []jvm.Value{jvm.ReferenceValue(nil), jvm.ReferenceValue(client.vm.NewString("2.0")), jvm.ReferenceValue(nil)}, 0},
		{"different vendor", []jvm.Value{jvm.ReferenceValue(nil), jvm.ReferenceValue(nil), jvm.ReferenceValue(client.vm.NewString("Elsewhere"))}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := client.vm.InvokeStatic(runtimeKernelClass, "getExecNames", "(Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;)[Ljava/lang/String;", test.filters...)
			if err != nil {
				t.Fatal(err)
			}
			array, err := result.Reference()
			if err != nil {
				t.Fatal(err)
			}
			_, values, err := jvm.ArraySnapshot(array)
			if err != nil {
				t.Fatal(err)
			}
			if len(values) != test.count {
				t.Fatalf("count = %d, want %d", len(values), test.count)
			}
			if len(values) > 0 {
				object, _ := values[0].Reference()
				if text, ok := jvm.StringText(object); !ok || text != "test-application" {
					t.Fatalf("unexpected identifier %q", text)
				}
			}
		})
	}
	if _, err := runtimeKernelGetExecNames(runtime, client.vm, []jvm.Value{jvm.IntValue(1), jvm.ReferenceValue(nil), jvm.ReferenceValue(nil)}); err == nil {
		t.Fatal("accepted non-reference filter")
	}
}

func TestInputMethodReportsCurrentMode(t *testing.T) {
	client, runtime := newTestRuntime(t)
	class, err := runtime.ensureJavaClass(runtimeInputMethodHandlerClass)
	if err != nil {
		t.Fatal(err)
	}
	_, object, err := runtime.allocateAOTInstance(class)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.vm.InvokeVirtual(object, "<init>", "(I)V", jvm.IntValue(2)); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []int32{2, 1} {
		if mode == 1 {
			if _, err := client.vm.InvokeVirtual(object, "setCurrentMode", "(I)Z", jvm.IntValue(mode)); err != nil {
				t.Fatal(err)
			}
		}
		result, err := client.vm.InvokeVirtual(object, "getCurrentMode", "()I")
		if err != nil {
			t.Fatal(err)
		}
		got, err := result.Int32()
		if err != nil || got != mode {
			t.Fatalf("mode = %d/%v, want %d", got, err, mode)
		}
	}
}

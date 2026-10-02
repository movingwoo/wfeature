package ktf

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// BenchmarkNativeReturnScope fixes the work at one native return environment.
// It excludes guest instructions so recording the scope's cost stays visible.
func BenchmarkNativeReturnScope(b *testing.B) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scope, err := runtime.beginNativeReturn(client.thread)
		if err != nil {
			b.Fatal(err)
		}
		value, err := scope.result(42)
		if err != nil || value != 42 {
			b.Fatalf("native result = %d, %v", value, err)
		}
		scope.restore()
	}
}

func BenchmarkRuntimeJavaCrossing(b *testing.B) {
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		b.Fatal(err)
	}
	runtime.nativeMethods[1] = runtimeJavaInvocation{method: runtimeJavaMethod{class: "java/lang/Math", name: "abs", descriptor: "(I)I", accessFlags: 9}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := client.thread.SetRegister(1, ^uint32(41)); err != nil {
			b.Fatal(err)
		}
		value, err := runtime.handleRuntimeJavaCall(client.thread, 1)
		if err != nil || value != 42 {
			b.Fatalf("native result = %d, %v", value, err)
		}
	}
}

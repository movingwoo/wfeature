package armcore

import (
	"context"
	"encoding/binary"
	"testing"
)

// BenchmarkCoreNestedCalls measures two short guest calls and their ordinary
// supervisor boundary. Keep the work fixed when comparing checkpoint support.
func BenchmarkCoreNestedCalls(b *testing.B) {
	core := NewCore(CoreOptions{})
	if err := core.Memory().Map(0x1000, 0x1000, PermissionReadExecute); err != nil {
		b.Fatal(err)
	}
	code := make([]byte, 0x20)
	for offset, instruction := range map[int]uint32{
		0x00: 0xef000001, // svc #1
		0x04: 0xe12fff1e, // bx lr
		0x10: 0xe3a0002a, // mov r0, #42
		0x14: 0xe12fff1e, // bx lr
	} {
		binary.LittleEndian.PutUint32(code[offset:], instruction)
	}
	if err := core.Memory().Load(0x1000, code); err != nil {
		b.Fatal(err)
	}
	parent := NewThread(NewContext())
	ctx := context.Background()
	handler := func(ctx context.Context, thread *Thread, _ SupervisorCall) error {
		result, err := core.Call(ctx, thread, 0x1010, 0x2000, nil, nil)
		if err != nil {
			return err
		}
		return thread.SetRegister(0, result.Context.Registers[0])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := core.Call(ctx, parent, 0x1000, 0x2000, nil, handler)
		if err != nil || result.Context.Registers[0] != 42 {
			b.Fatalf("nested result = %v, %v", result, err)
		}
	}
}

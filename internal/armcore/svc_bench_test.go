package armcore

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

// The supervisor-call boundary, measured on the shape that makes it matter: a
// guest loop that calls a platform stub every few instructions. See
// FastSupervisorCall for why the boundary is worth a benchmark of its own —
// the loop below is what a title polling the platform clock runs.
//
// svcLoopMemory lays out `filler` ALU instructions, a call to a stub that
// raises SVC, and a branch back. Two fillers give the two points a straight
// line needs, so the crossing's own cost separates from the instructions
// around it.
func svcLoopMemory(t testing.TB, filler int) (*Memory, uint32) {
	const base = uint32(0x10000)
	const stub = uint32(0x11000)
	memory := NewMemory()
	if err := memory.Map(base, 1<<16, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(0x20000, 1<<16, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	code := []uint16{}
	for index := 0; index < filler; index++ {
		code = append(code, 0x3001) // adds r0, #1
	}
	offset := (int32(stub) - (int32(base) + int32(len(code))*2 + 4)) >> 1
	code = append(code,
		uint16(0xf000|((offset>>11)&0x7ff)), // bl stub
		uint16(0xf800|(offset&0x7ff)),
	)
	code = append(code, 0xe000|uint16(-(len(code)+2))&0x7ff) // b top
	encoded := make([]byte, len(code)*2)
	for index, instruction := range code {
		binary.LittleEndian.PutUint16(encoded[index*2:], instruction)
	}
	if err := memory.Load(base, encoded); err != nil {
		t.Fatal(err)
	}
	// The stub is the shape both platforms build: push a scratch register,
	// load the slot word that follows the code, svc, return.
	if err := memory.Load(stub, []byte{
		0x10, 0xb4, // push {r4}
		0x02, 0x4c, // ldr  r4, [pc, #8]
		0xa4, 0x46, // mov  ip, r4
		0x10, 0xbc, // pop  {r4}
		0x02, 0xdf, // svc  #2
		0x70, 0x47, // bx   lr
		0x7d, 0x00, 0x00, 0x00,
	}); err != nil {
		t.Fatal(err)
	}
	return memory, base
}

var errBenchmarkDone = errors.New("benchmark done")

// BenchmarkCoreSupervisorLoop is the crossing as it is served today: the
// quantum ends, the thread suspends, the handler reads and writes its
// registers through the thread, and the quantum starts again.
func BenchmarkCoreSupervisorLoop(b *testing.B) {
	benchmarkSupervisorLoop(b, false)
}

// BenchmarkCoreFastSupervisorLoop is the same loop answered inside the
// quantum. The difference between the two is what a fast slot saves.
func BenchmarkCoreFastSupervisorLoop(b *testing.B) {
	benchmarkSupervisorLoop(b, true)
}

func benchmarkSupervisorLoop(b *testing.B, fast bool) {
	for _, filler := range []int{4, 32} {
		name := "filler4"
		if filler == 32 {
			name = "filler32"
		}
		b.Run(name, func(b *testing.B) {
			memory, base := svcLoopMemory(b, filler)
			core := NewCore(CoreOptions{MaxSteps: 1 << 62})
			core.memory = memory
			initial := NewContext()
			if err := initial.SetPC(base | 1); err != nil {
				b.Fatal(err)
			}
			initial.Registers[RegisterSP] = 0x28000
			thread := NewThread(initial)
			crossings := 0
			count := func() error {
				crossings++
				if crossings >= b.N {
					return errBenchmarkDone
				}
				return nil
			}
			// A fast slot cannot fail, so the benchmark ends the run the way
			// a guest would: by branching to the address Run was given as its
			// end.
			const halt = uint32(0x11100)
			if fast {
				core.SetFastSupervisorCall(func(context *Context, _ uint32) bool {
					context.Registers[0] = context.Registers[12]
					context.Registers[1] = 0
					crossings++
					if crossings >= b.N {
						context.setThumbPC(halt)
					}
					return true
				})
			}
			handler := func(_ context.Context, thread *Thread, _ SupervisorCall) error {
				slot, err := thread.Register(12)
				if err != nil {
					return err
				}
				if err := thread.SetRegister(0, slot); err != nil {
					return err
				}
				if err := thread.SetRegister(1, 0); err != nil {
					return err
				}
				return count()
			}
			end := uint32(0xffffffff)
			if fast {
				end = halt
			}
			b.ResetTimer()
			_, err := core.Run(context.Background(), thread, end, handler)
			if err != nil && !errors.Is(err, errBenchmarkDone) {
				b.Fatal(err)
			}
			b.StopTimer()
			steps := core.Steps()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N), "ns/crossing")
			b.ReportMetric(float64(steps)/float64(b.N), "steps/crossing")
		})
	}
}

package armcore

import (
	"encoding/binary"
	"testing"
)

// Interpreter throughput, measured on the three shapes that answer different
// questions: what an instruction costs with no memory traffic, what one costs
// with it, and what the code real games actually run costs. See the throughput
// section of docs/armcore.md — optimising against the first two alone misleads,
// which is what the third is here to prevent.
//
// aluLoopMemory lays out a Thumb loop of `adds r0, #1` ending in a backward
// branch, so the only per-instruction cost is fetch and decode: no loads, no
// stores, no branching out.
func aluLoopMemory(t testing.TB, instructions int) (*Memory, uint32) {
	const base = uint32(0x10000)
	memory := NewMemory()
	if err := memory.Map(base, 1<<16, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	code := make([]byte, instructions*2)
	for index := 0; index < instructions-1; index++ {
		binary.LittleEndian.PutUint16(code[index*2:], 0x3001) // adds r0, #1
	}
	// B back to base: target = (pc+4) + (offset<<1), pc = base+2*(n-1).
	offset := -(instructions + 1)
	binary.LittleEndian.PutUint16(code[(instructions-1)*2:], 0xe000|uint16(offset)&0x7ff)
	if err := memory.Load(base, code); err != nil {
		t.Fatal(err)
	}
	return memory, base
}

func BenchmarkEngineALULoop(b *testing.B) {
	memory, base := aluLoopMemory(b, 64)
	context := NewContext()
	if err := context.SetPC(base | 1); err != nil {
		b.Fatal(err)
	}
	engine := Engine{}
	b.ResetTimer()
	steps := 0
	for steps < b.N {
		result, err := engine.Run(&context, memory, 0xffffffff, 1_000_000)
		if err != nil {
			b.Fatal(err)
		}
		steps += int(result.Steps)
	}
	b.ReportMetric(float64(steps)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkThumbExecuteOnly removes the fetch: the instruction is already in
// hand, so what is left is the match tree and the operation.
func BenchmarkThumbExecuteOnly(b *testing.B) {
	memory, base := aluLoopMemory(b, 64)
	context := NewContext()
	if err := context.SetPC(base | 1); err != nil {
		b.Fatal(err)
	}
	memory.beginQuantum()
	defer memory.endQuantum()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := executeThumb(&context, memory, base, 0x3001); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkFetchOnly isolates what one instruction fetch costs now that the
// lock is no longer part of it.
func BenchmarkFetchOnly(b *testing.B) {
	memory, base := aluLoopMemory(b, 64)
	memory.beginQuantum()
	defer memory.endQuantum()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := memory.fetch16(base + uint32(index%64)*2); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// loadStoreLoopMemory lays out a Thumb loop that actually touches memory:
// a word load and a word store per iteration, which is what real game code
// does and what the ALU loop deliberately leaves out.
func loadStoreLoopMemory(t testing.TB, iterations int) (*Memory, uint32) {
	const base = uint32(0x10000)
	const data = uint32(0x20000)
	memory := NewMemory()
	if err := memory.Map(base, 1<<16, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(data, 1<<16, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	// r1 = data base, then repeat: ldr r0,[r1,#0]; str r0,[r1,#4]; b back.
	code := []uint16{}
	for index := 0; index < iterations; index++ {
		code = append(code, 0x6808) // ldr r0, [r1, #0]
		code = append(code, 0x6048) // str r0, [r1, #4]
	}
	offset := -(len(code) + 2)
	code = append(code, 0xe000|uint16(offset)&0x7ff)
	encoded := make([]byte, len(code)*2)
	for index, instruction := range code {
		binary.LittleEndian.PutUint16(encoded[index*2:], instruction)
	}
	if err := memory.Load(base, encoded); err != nil {
		t.Fatal(err)
	}
	if err := memory.Write(data, make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	return memory, base
}

func BenchmarkEngineLoadStoreLoop(b *testing.B) {
	memory, base := loadStoreLoopMemory(b, 32)
	context := NewContext()
	if err := context.SetPC(base | 1); err != nil {
		b.Fatal(err)
	}
	context.Registers[1] = 0x20000
	b.ResetTimer()
	steps := 0
	for steps < b.N {
		result, err := (Engine{}).Run(&context, memory, 0xffffffff, 1_000_000)
		if err != nil {
			b.Fatal(err)
		}
		steps += int(result.Steps)
	}
	b.ReportMetric(float64(steps)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// gameShapedLoop mirrors what a measured KTF title actually executes. Its frames are
// 58% immediate-form ALU, 19% halfword transfer, and 20% conditional branch —
// 97% of everything — so the loop below is three immediates, one halfword
// load, and one untaken conditional branch per iteration. Optimising against
// the ALU loop optimises against code no game runs.
func gameShapedLoop(t testing.TB, bodies int) (*Memory, uint32) {
	const base = uint32(0x10000)
	const data = uint32(0x20000)
	memory := NewMemory()
	if err := memory.Map(base, 1<<16, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(data, 1<<16, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	code := []uint16{}
	for index := 0; index < bodies; index++ {
		code = append(code,
			0x8808, // ldrh r0, [r1, #0]
			0x2800, // cmp r0, #0
			0xd000, // beq +0   (never taken: the halfword read is nonzero)
			0x3201, // add r2, #1
			0x3a01, // sub r2, #1
		)
	}
	code = append(code, 0xe000|uint16(-(len(code)+2))&0x7ff)
	encoded := make([]byte, len(code)*2)
	for index, instruction := range code {
		binary.LittleEndian.PutUint16(encoded[index*2:], instruction)
	}
	if err := memory.Load(base, encoded); err != nil {
		t.Fatal(err)
	}
	if err := memory.Write(data, []byte{0xff, 0xff, 0, 0}); err != nil {
		t.Fatal(err)
	}
	return memory, base
}

func BenchmarkEngineGameShapedLoop(b *testing.B) {
	memory, base := gameShapedLoop(b, 32)
	context := NewContext()
	if err := context.SetPC(base | 1); err != nil {
		b.Fatal(err)
	}
	context.Registers[1] = 0x20000
	b.ResetTimer()
	steps := 0
	for steps < b.N {
		result, err := (Engine{}).Run(&context, memory, 0xffffffff, 1_000_000)
		if err != nil {
			b.Fatal(err)
		}
		steps += int(result.Steps)
	}
	b.ReportMetric(float64(steps)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// blitLoop is the fourth shape, and the one the three above between them do
// not have: a loop that streams. It reads a halfword from one buffer, writes
// it to another, and advances both pointers, which is what every sprite blit,
// tile draw and screen clear in these games is made of. The two buffers are a
// page apart or more, so each pair of accesses lands in two different pages.
//
// It exists because the game-shaped loop's one load hits the same address
// every iteration. That hides what a transfer costs: against the game-shaped
// loop this one says an aligned load or store costs about three times what an
// ALU instruction does, which is the split any work on throughput has to aim
// at.
func blitLoop(t testing.TB, bufferKB int, destinationOffset uint32) (*Memory, uint32, uint32) {
	const base = uint32(0x100000)
	const data = uint32(0x400000)
	size := uint64(bufferKB) * 1024
	memory := NewMemory()
	if err := memory.Map(base, 4096, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(data, size*2+uint64(destinationOffset), PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	body := []uint16{
		0x8808, // ldrh r0, [r1, #0]
		0x8010, // strh r0, [r2, #0]
		0x3102, // adds r1, #2
		0x3202, // adds r2, #2
	}
	code := []uint16{}
	for index := 0; index < 64; index++ {
		code = append(code, body...)
	}
	code = append(code, 0xe000|uint16(-(len(code)+2))&0x7ff)
	encoded := make([]byte, len(code)*2)
	for index, instruction := range code {
		binary.LittleEndian.PutUint16(encoded[index*2:], instruction)
	}
	if err := memory.Load(base, encoded); err != nil {
		t.Fatal(err)
	}
	if err := memory.Write(data, make([]byte, size)); err != nil {
		t.Fatal(err)
	}
	return memory, base, data
}

func benchmarkBlit(b *testing.B, bufferKB int, destinationOffset uint32) {
	memory, base, data := blitLoop(b, bufferKB, destinationOffset)
	context := NewContext()
	engine := Engine{}
	b.ResetTimer()
	steps := 0
	for steps < b.N {
		if err := context.SetPC(base | 1); err != nil {
			b.Fatal(err)
		}
		context.Registers[1] = data
		context.Registers[2] = data + destinationOffset
		result, err := engine.Run(&context, memory, 0xffffffff, uint32(bufferKB)*1024/2*4)
		if err != nil {
			b.Fatal(err)
		}
		steps += int(result.Steps)
	}
	b.ReportMetric(float64(steps)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// The destination follows the source inside one page, so the cached data page
// serves both accesses. This is the floor the cross-page case is read against.
func BenchmarkEngineBlitSamePage(b *testing.B) { benchmarkBlit(b, 512, 8) }

// The destination is its own region, so the two accesses alternate pages —
// which is what a real blit does, and what one cached page cannot hold. See
// the throughput section of docs/armcore.md for the two cache shapes that were
// built to close the gap this reports and rejected on real games.
func BenchmarkEngineBlitCrossPage(b *testing.B) { benchmarkBlit(b, 512, 512*1024) }

// armALULoopMemory is the ALU loop above in ARM rather than Thumb: `adds r0,
// r0, #1` ending in a backward branch. The pair exists because **the two
// instruction sets take different paths through the engine** — Thumb reaches a
// decode cache and a routed switch, ARM is fetched and matched from scratch
// every time — and the local titles are split between them: an LGT Clet runs
// almost entirely Thumb, and an LGT Java title, compiled ahead of time, runs
// mostly ARM. Optimising one path says nothing about the other, and until this
// benchmark existed only the Thumb one could be measured at all.
func armALULoopMemory(t testing.TB, instructions int) (*Memory, uint32) {
	const base = uint32(0x10000)
	memory := NewMemory()
	if err := memory.Map(base, 1<<16, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	code := make([]byte, instructions*4)
	for index := 0; index < instructions-1; index++ {
		binary.LittleEndian.PutUint32(code[index*4:], 0xe2900001) // adds r0, r0, #1
	}
	// B back to base: target = (pc+8) + (offset<<2), pc = base+4*(n-1).
	offset := -(instructions + 1)
	binary.LittleEndian.PutUint32(code[(instructions-1)*4:], 0xea000000|uint32(offset)&0xffffff)
	if err := memory.Load(base, code); err != nil {
		t.Fatal(err)
	}
	return memory, base
}

func BenchmarkEngineARMALULoop(b *testing.B) {
	memory, base := armALULoopMemory(b, 64)
	context := NewContext()
	if err := context.SetPC(base); err != nil {
		b.Fatal(err)
	}
	engine := Engine{}
	b.ResetTimer()
	steps := 0
	for steps < b.N {
		result, err := engine.Run(&context, memory, 0xffffffff, 1_000_000)
		if err != nil {
			b.Fatal(err)
		}
		steps += int(result.Steps)
	}
	b.ReportMetric(float64(steps)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkARMExecuteOnly and BenchmarkARMFetchOnly split the ARM step the way
// the two Thumb benchmarks above split a Thumb one, which is what says where
// the difference between them lives: the fetch, the match chain, or the
// operation itself.
func BenchmarkARMExecuteOnly(b *testing.B) {
	memory, base := armALULoopMemory(b, 64)
	context := NewContext()
	if err := context.SetPC(base); err != nil {
		b.Fatal(err)
	}
	memory.beginQuantum()
	defer memory.endQuantum()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := executeARM(&context, memory, base, 0xe2900001); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds()/1e6, "MIPS")
}

func BenchmarkARMFetchOnly(b *testing.B) {
	memory, base := armALULoopMemory(b, 64)
	memory.beginQuantum()
	defer memory.endQuantum()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := memory.fetch32(base + uint32(index%64)*4); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkARMDataProcessingOnly is the operation with the match chain taken
// off: `executeARM` reaches data processing after about ten mask-and-compare
// tests, and this calls the handler directly. The gap between this and
// BenchmarkARMExecuteOnly is what a cached form would remove — the ARM half of
// what the Thumb path's routed switch already does.
func BenchmarkARMDataProcessingOnly(b *testing.B) {
	memory, base := armALULoopMemory(b, 64)
	context := NewContext()
	if err := context.SetPC(base); err != nil {
		b.Fatal(err)
	}
	memory.beginQuantum()
	defer memory.endQuantum()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := executeARMDataProcessing(&context, base, 0xe2900001); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds()/1e6, "MIPS")
}

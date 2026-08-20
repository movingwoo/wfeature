package armcore

import (
	"encoding/binary"
	"testing"
)

// The translator gate.
//
// docs/armcore.md, "Why a translator is not the answer even with unlimited
// effort", closes the question on two grounds: a closure per instruction pays
// more in indirect calls than it removes in dispatch, and a translator that
// emits machine code is out on how this ships. Neither is a measurement of the
// third form — a translator that keeps the guest's registers in the host's,
// lets a compare feed a branch instead of materialising NZCV, and reaches
// memory through an inline page check. That form is the only one that attacks
// what an instruction costs rather than what finding it costs, and nothing
// here had measured it.
//
// This is what would decide it, and it is deliberately an **upper bound**: it
// can refuse the design but it cannot approve it.
//
// What is measured is one real block chain rather than a synthetic loop: the
// run-length token decoder a local LGT title spends its interpreted time in.
// A host profile of that title's field scene puts 93.3% of the run inside
// `Engine.Run`, and inside that the largest thing still being interpreted —
// the stand-ins already cover the pixel loops around it — is this decoder's
// six addresses. The bytes below are that routine, at the address it occupies
// in its own module so its literal-pool loads resolve where they really do.
//
// **The bar is 2.4x.** Of the host time that title spends, 8.1% is already in
// stand-ins and 6.7% is outside the core; only 85.2% is interpretation a
// translator could reach. Making that part N times faster moves the whole run
// by 1/(0.148 + 0.852/N), and the session needs 1.96x to reach its own clock:
//
//	N = 1.64 (dispatch removed entirely — the ceiling already measured
//	          in "The block interpreter, measured before building it again")
//	          -> 1.50x, short
//	N = 2.4   -> 1.96x, exactly the bar
//	N = 4     -> 2.77x
//
// So a translated block has to beat the interpreter by 2.4x on the same
// instructions before any of the rest — two code generators, W^X on three
// operating systems, and the seven invariants the interpreter carries — is
// worth costing.
//
// The comparison is honest about the direction it errs in. `translatedTokenDecode`
// is Go rather than emitted code, so it is missing the block entry and exit, the
// lookup that finds a block in the first place, and any deoptimisation edge; it
// is also handed its guest registers as locals, which is the whole point. It
// pays what a translator would pay for memory — a page check per token and the
// engine's own `pageAt` on a miss — and it charges the same step budget. Read
// it as "no translator of this block can do better than this", not as "a
// translator would do this".
const (
	// tokenDecodeBase is where the routine sits in its own module. The address
	// is kept because two of its instructions are `ldr rN, [pc, #imm]` into a
	// literal pool eight words further on, and a pool only resolves where the
	// code was linked.
	tokenDecodeBase = uint32(0x51df4)
	// tokenDecodeEntry is the loop head, which is where the profile's samples
	// land. The three instructions before it are the function's prologue and
	// run once.
	tokenDecodeEntry = uint32(0x51dfa)
	// tokenDecodeSteps is what one transparent token costs the interpreter.
	// TestTokenDecodeCostsSeventeenSteps holds it to the code.
	tokenDecodeSteps = 17
	// tokenDecodeEndSteps is what the end marker costs: the seven instructions
	// up to the branch that leaves, rather than a whole round.
	tokenDecodeEndSteps = 7

	tokenDecodeCodeRegion  = uint32(0x50000)
	tokenDecodeStream      = uint32(0x300000)
	tokenDecodeStreamBytes = 1 << 20
	tokenDecodeDestination = uint32(0x400000)
	tokenDecodeStride      = uint32(240)
	// tokenDecodeBatch is one call into the engine. It is under the stream's
	// length in tokens, so a batch never reads past what is mapped.
	tokenDecodeBatch = 1_000_000
)

// tokenDecodeCode is the routine as its module carries it. The run-length
// decoder is the first half; the second is the pixel loop it calls, which is
// present because the literal pool sits behind it and because a branch into it
// has to land somewhere real.
var tokenDecodeCode = []uint16{
	0xb570, 0x1c16, 0x1c1d, // 51df4: prologue
	0x784b, 0x780a, 0x021b, 0x4c13, 0x431a, 0x42a2, 0xd01f, // 51dfa: read a token, test for the end marker
	0x4b12, 0x3102, 0x429a, 0xd101, // 51e08: test for the row marker
	0x006b, 0xe003, // 51e10: a row ends, advance the destination by a stride
	0x0413, 0x2b00, 0xdb02, // 51e14: is the run drawn or transparent
	0x0053, 0x18c0, 0xe7ec, // 51e1a: transparent, advance and go round
	0x4b0d, 0x401a, 0x1e53, 0x041b, 0x0c1a, 0x42a2, 0xd0e5, // 51e20: a drawn run's setup
	0x780b, 0x3101, 0x005b, 0x5b9b, 0x8003, 0x1e53, 0x041b, // 51e2e: the pixel loop, which
	0x0c1a, 0x4b04, 0x3002, 0x429a, 0xd1f3, 0xe7d8, //         a stand-in already covers
	0xbc70, 0xbc01, 0x4700, 0x0000, // 51e48: return
	0xffff, 0x0000, // 51e50: the end marker
	0xfffe, 0x0000, // 51e54: the row marker
	0x7fff, 0x0000, // 51e58: the run-length mask
}

// tokenDecodeReturn is where the routine leaves, which is what an engine run is
// given as its `end` when the run is meant to stop rather than be counted out.
const tokenDecodeReturn = uint32(0x51e48)

// tokenDecodeMemory maps the routine and a stream of transparent tokens for it
// to walk. Every token is a transparent run, because that is the path the
// profile says the loop spends its time on: a drawn run leaves this block for
// the pixel loop, and the pixel loop is not what is in question — a stand-in
// already runs it in Go, six times cheaper per instruction than either side of
// this benchmark.
//
// The stream is page aligned and its tokens are two bytes, so no token ever
// straddles a page. `translatedTokenDecode` relies on that, and a translator
// emitting a real fast path would have to handle the case this avoids.
func tokenDecodeMemory(t testing.TB, terminated bool) *Memory {
	memory := NewMemory()
	if err := memory.Map(tokenDecodeCodeRegion, 1<<16, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	encoded := make([]byte, len(tokenDecodeCode)*2)
	for index, instruction := range tokenDecodeCode {
		binary.LittleEndian.PutUint16(encoded[index*2:], instruction)
	}
	if err := memory.Load(tokenDecodeBase, encoded); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(tokenDecodeStream, tokenDecodeStreamBytes, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(tokenDecodeDestination, 1<<16, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	stream := make([]byte, tokenDecodeStreamBytes)
	for offset := 0; offset < len(stream); offset += 2 {
		// A transparent run of four pixels: bit fifteen clear, and neither
		// marker.
		binary.LittleEndian.PutUint16(stream[offset:], 0x0004)
	}
	if terminated {
		binary.LittleEndian.PutUint16(stream[len(stream)-2:], 0xffff)
	}
	if err := memory.Write(tokenDecodeStream, stream); err != nil {
		t.Fatal(err)
	}
	return memory
}

func tokenDecodeContext() Context {
	context := NewContext()
	context.Registers[0] = tokenDecodeDestination
	context.Registers[1] = tokenDecodeStream
	context.Registers[5] = tokenDecodeStride
	return context
}

// TestTokenDecodeCostsSeventeenSteps pins the number the gate divides by. The
// decoder reads two bytes, folds them into a halfword, tests it against two
// markers and against bit fifteen, advances the destination and goes round:
// seventeen instructions, of which two are loads from its own literal pool and
// four are conditional branches. The last token is the end marker and leaves
// after seven of them.
func TestTokenDecodeCostsSeventeenSteps(t *testing.T) {
	memory := tokenDecodeMemory(t, true)
	context := tokenDecodeContext()
	if err := context.SetPC(tokenDecodeEntry | 1); err != nil {
		t.Fatal(err)
	}
	tokens := tokenDecodeStreamBytes/2 - 1
	result, err := (Engine{}).Run(&context, memory, tokenDecodeReturn, uint32(tokens)*tokenDecodeSteps+64)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != StopEnd {
		t.Fatalf("the decoder did not reach its return: %v after %d steps", result.Reason, result.Steps)
	}
	const endMarkerSteps = 7
	if want := uint32(tokens)*tokenDecodeSteps + endMarkerSteps; result.Steps != want {
		t.Errorf("the decoder took %d steps over %d tokens, want %d (%d each and %d to leave)",
			result.Steps, tokens, want, tokenDecodeSteps, endMarkerSteps)
	}
	if got := context.Registers[0]; got != tokenDecodeDestination+uint32(tokens)*8 {
		t.Errorf("the destination ended at %#x, want %#x", got, tokenDecodeDestination+uint32(tokens)*8)
	}
}

// translatedTokenDecode is the block chain above as a translator that allocates
// registers would have run it: r0, r1 and r5 are locals rather than an array,
// each compare feeds its branch directly instead of writing NZCV that nothing
// else reads, and the two byte loads share one page check. On a miss it calls
// the engine's own `pageAt`, which is the slow path a real fast path would call
// out to. It charges the same steps so the two sides are counted alike.
func translatedTokenDecode(memory *Memory, r0, r1, r5 uint32, budget int) (uint32, uint32, int) {
	memory.beginQuantum()
	defer memory.endQuantum()

	var page *memoryPage
	tag := ^uint32(0)
	executed := 0
	for executed+tokenDecodeSteps <= budget {
		if index := r1 >> memoryPageShift; index != tag {
			page = memory.pageAt(r1)
			if page == nil || page.data == nil {
				break
			}
			tag = index
		}
		offset := r1 & memoryPageMask
		token := uint32(page.data[offset]) | uint32(page.data[offset+1])<<8
		if token == 0xffff {
			executed += tokenDecodeEndSteps
			break
		}
		executed += tokenDecodeSteps
		r1 += 2
		if token == 0xfffe {
			r0 += r5 << 1
			continue
		}
		if token&0x8000 != 0 {
			// A drawn run leaves this block for the pixel loop. A translator
			// would chain to it; here it ends the run, and the stream this
			// benchmark walks never takes the edge.
			break
		}
		r0 += token << 1
	}
	return r0, r1, executed
}

// tokenDecodeSink keeps the translated side from being folded away.
var tokenDecodeSink uint32

func BenchmarkTokenDecodeInterpreted(b *testing.B) {
	memory := tokenDecodeMemory(b, false)
	context := tokenDecodeContext()
	engine := Engine{}
	b.ResetTimer()
	steps := 0
	for steps < b.N {
		// The stream is rewound once per batch rather than per token, so the
		// reset is one event in a million steps.
		context = tokenDecodeContext()
		if err := context.SetPC(tokenDecodeEntry | 1); err != nil {
			b.Fatal(err)
		}
		result, err := engine.Run(&context, memory, tokenDecodeReturn, tokenDecodeBatch)
		if err != nil {
			b.Fatal(err)
		}
		steps += int(result.Steps)
	}
	tokenDecodeSink += context.Registers[0]
	b.ReportMetric(float64(steps)/b.Elapsed().Seconds()/1e6, "MIPS")
}

func BenchmarkTokenDecodeTranslated(b *testing.B) {
	memory := tokenDecodeMemory(b, false)
	b.ResetTimer()
	steps := 0
	destination := uint32(0)
	for steps < b.N {
		r0, _, executed := translatedTokenDecode(
			memory, tokenDecodeDestination, tokenDecodeStream, tokenDecodeStride, tokenDecodeBatch)
		destination += r0
		steps += executed
	}
	tokenDecodeSink += destination
	b.ReportMetric(float64(steps)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// translatedTokenDecodeLiteral is the same block with one generosity removed.
// `translatedTokenDecode` folds the two `ldr rN, [pc, #imm]` loads into
// immediates, which a translator may do — the pool is read-only code and the
// page that holds it already drops everything derived from it when its bytes
// change. This one does not fold them: it reads both literals out of guest
// memory every token, through a page cache of their own.
//
// The second cache is the point. The engine keeps **one** data page, so the
// real loop alternates between the stream page and the code page and misses on
// every access; a translator has a check per site and each site sees one page.
// The gap between this benchmark and the one above is what folding is worth,
// and the gap between this one and the interpreter is what a translator that
// folded nothing at all would still get.
func translatedTokenDecodeLiteral(memory *Memory, r0, r1, r5 uint32, budget int) (uint32, uint32, int) {
	memory.beginQuantum()
	defer memory.endQuantum()

	var stream, pool *memoryPage
	streamTag, poolTag := ^uint32(0), ^uint32(0)
	executed := 0
	for executed+tokenDecodeSteps <= budget {
		if index := r1 >> memoryPageShift; index != streamTag {
			stream = memory.pageAt(r1)
			if stream == nil || stream.data == nil {
				break
			}
			streamTag = index
		}
		offset := r1 & memoryPageMask
		token := uint32(stream.data[offset]) | uint32(stream.data[offset+1])<<8

		const endLiteral = tokenDecodeBase + 0x5c // 0x51e50
		const rowLiteral = tokenDecodeBase + 0x60 // 0x51e54
		if index := endLiteral >> memoryPageShift; index != poolTag {
			pool = memory.pageAt(endLiteral)
			if pool == nil || pool.data == nil {
				break
			}
			poolTag = index
		}
		end := uint32(pool.data[endLiteral&memoryPageMask]) |
			uint32(pool.data[(endLiteral&memoryPageMask)+1])<<8
		row := uint32(pool.data[rowLiteral&memoryPageMask]) |
			uint32(pool.data[(rowLiteral&memoryPageMask)+1])<<8

		if token == end {
			executed += tokenDecodeEndSteps
			break
		}
		executed += tokenDecodeSteps
		r1 += 2
		if token == row {
			r0 += r5 << 1
			continue
		}
		if token&0x8000 != 0 {
			break
		}
		r0 += token << 1
	}
	return r0, r1, executed
}

func BenchmarkTokenDecodeTranslatedLiteral(b *testing.B) {
	memory := tokenDecodeMemory(b, false)
	b.ResetTimer()
	steps := 0
	destination := uint32(0)
	for steps < b.N {
		r0, _, executed := translatedTokenDecodeLiteral(
			memory, tokenDecodeDestination, tokenDecodeStream, tokenDecodeStride, tokenDecodeBatch)
		destination += r0
		steps += executed
	}
	tokenDecodeSink += destination
	b.ReportMetric(float64(steps)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// BenchmarkTokenDecodeInterpretedSamePage is a diagnostic rather than a
// workload, and it exists because the interpreted number above came out high.
//
// This block reads two literals out of its own code page and two bytes out of
// the sprite stream, every token. `Memory` keeps its instruction fetch on a
// page of its own but has **one** data page, so those two streams evict each
// other and every one of the four accesses takes `pageFor` — the two-level
// table walk the cache exists to avoid. Nothing about that is specific to a
// translator: it is the interpreter paying a miss it could have hit.
//
// The only difference here is where the stream lives. Putting it in the same
// page as the literal pool makes the one cache slot enough, and the gap
// between this and BenchmarkTokenDecodeInterpreted is what a second data slot
// would be worth on this block. Run the two together or neither.
func tokenDecodeSamePageMemory(t testing.TB) *Memory {
	memory := NewMemory()
	if err := memory.Map(tokenDecodeCodeRegion, 1<<16, PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	encoded := make([]byte, len(tokenDecodeCode)*2)
	for index, instruction := range tokenDecodeCode {
		binary.LittleEndian.PutUint16(encoded[index*2:], instruction)
	}
	if err := memory.Load(tokenDecodeBase, encoded); err != nil {
		t.Fatal(err)
	}
	// The stream sits below the routine in the page the literal pool is in.
	// A loader write is what puts it there, since the page is code.
	stream := make([]byte, tokenDecodeSamePageBytes)
	for offset := 0; offset < len(stream); offset += 2 {
		binary.LittleEndian.PutUint16(stream[offset:], 0x0004)
	}
	if err := memory.Load(tokenDecodeSamePageStream, stream); err != nil {
		t.Fatal(err)
	}
	if err := memory.Map(tokenDecodeDestination, 1<<16, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	return memory
}

const (
	tokenDecodeSamePageStream = uint32(0x51000)
	tokenDecodeSamePageBytes  = int(tokenDecodeBase - tokenDecodeSamePageStream)
	// One batch stays inside the shorter stream.
	tokenDecodeSamePageBatch = uint32(tokenDecodeSamePageBytes/2-8) * tokenDecodeSteps
)

func BenchmarkTokenDecodeInterpretedSamePage(b *testing.B) {
	memory := tokenDecodeSamePageMemory(b)
	context := tokenDecodeContext()
	engine := Engine{}
	b.ResetTimer()
	steps := 0
	for steps < b.N {
		context = tokenDecodeContext()
		context.Registers[1] = tokenDecodeSamePageStream
		if err := context.SetPC(tokenDecodeEntry | 1); err != nil {
			b.Fatal(err)
		}
		result, err := engine.Run(&context, memory, tokenDecodeReturn, tokenDecodeSamePageBatch)
		if err != nil {
			b.Fatal(err)
		}
		steps += int(result.Steps)
	}
	tokenDecodeSink += context.Registers[0]
	b.ReportMetric(float64(steps)/b.Elapsed().Seconds()/1e6, "MIPS")
}

// TestTranslatedTokenDecodeMatchesTheInterpreter is what makes the gate's two
// numbers comparable. A benchmark against a faster thing that computes
// something else measures nothing, so both models are run over the same stream
// as the engine and are required to leave the same registers behind and to
// have charged the same steps.
func TestTranslatedTokenDecodeMatchesTheInterpreter(t *testing.T) {
	memory := tokenDecodeMemory(t, true)
	context := tokenDecodeContext()
	if err := context.SetPC(tokenDecodeEntry | 1); err != nil {
		t.Fatal(err)
	}
	tokens := uint32(tokenDecodeStreamBytes/2 - 1)
	result, err := (Engine{}).Run(&context, memory, tokenDecodeReturn, tokens*tokenDecodeSteps+64)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != StopEnd {
		t.Fatalf("the interpreted run did not reach the return: %v", result.Reason)
	}

	budget := int(tokens)*tokenDecodeSteps + 64
	for _, model := range []struct {
		name string
		run  func(*Memory, uint32, uint32, uint32, int) (uint32, uint32, int)
	}{
		{"folded", translatedTokenDecode},
		{"literal", translatedTokenDecodeLiteral},
	} {
		r0, r1, executed := model.run(
			memory, tokenDecodeDestination, tokenDecodeStream, tokenDecodeStride, budget)
		if r0 != context.Registers[0] {
			t.Errorf("%s left the destination at %#x, the interpreter at %#x",
				model.name, r0, context.Registers[0])
		}
		if r1 != context.Registers[1] {
			t.Errorf("%s left the source at %#x, the interpreter at %#x",
				model.name, r1, context.Registers[1])
		}
		if uint32(executed) != result.Steps {
			t.Errorf("%s charged %d steps, the interpreter %d", model.name, executed, result.Steps)
		}
	}
}

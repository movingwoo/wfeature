package ktf

import (
	"sort"

	"github.com/movingwoo/wfeature/internal/guestprofile"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// Profiling answers "which guest code is this game spending its instructions
// in". The ARM core samples stacks and `internal/guestprofile` ranks and
// renders them; what is here is the part that is KTF's own — turning a raw
// guest address into an AOT method name, because a list of hex addresses is
// only useful to someone already holding a disassembly.
//
// The shared types are aliased rather than wrapped so that a Host holding a
// ktf.Profile keeps holding the same thing it always did.
type (
	// ProfileFrame is one guest address with whatever name could be resolved
	// for it. Symbol is empty when the address is not inside a registered AOT
	// method body — client.bin's own runtime helpers and any code the game
	// reaches through a function pointer we never saw registered look like
	// that.
	ProfileFrame = guestprofile.Frame
	// ProfileStack is one sampled stack, innermost frame first.
	ProfileStack = guestprofile.Stack
	// ProfileLeaf is the self time of one symbol.
	ProfileLeaf = guestprofile.Leaf
	// Profile is a symbolized snapshot of guest execution.
	Profile = guestprofile.Profile
)

// EnableProfile starts sampling guest stacks every interval executed
// instructions; zero selects the core default. Sampling costs a stack walk per
// interval, so it is opt-in rather than always on.
func (session *Session) EnableProfile(interval uint64) {
	if session == nil || session.Client == nil {
		return
	}
	session.Client.Core().EnableProfile(interval)
}

// DisableProfile stops sampling and discards what was collected.
func (session *Session) DisableProfile() {
	if session == nil || session.Client == nil {
		return
	}
	session.Client.Core().DisableProfile()
}

// ResetProfile discards the samples so far and keeps sampling. A harness that
// drives a game through minutes of loading to reach the scene it wants to
// measure calls this on arrival, so the profile covers the scene rather than
// the loading.
func (session *Session) ResetProfile() {
	if session == nil || session.Client == nil {
		return
	}
	session.Client.Core().ResetProfile()
}

// Profile returns the samples collected so far with every address resolved.
func (session *Session) Profile() Profile {
	if session == nil || session.Client == nil {
		return Profile{}
	}
	raw := session.Client.Core().Profile()
	// The symbol table is rebuilt per call rather than cached because classes
	// keep registering as the game loads: a table built at start would name
	// nothing that mattered.
	symbols := newSymbolTable(session.Client.JVM())
	return guestprofile.Build(raw, symbols.resolve)
}

// symbolTable maps a guest address to the AOT method whose body contains it.
//
// KTF method metadata says where each body starts and never how long it is, so
// every end here is inferred. For all but the highest body the next body's
// start is real evidence of an end. The highest body has no successor, and
// what lies above it is not game code at all but client.bin's own runtime
// helpers — the array-load helper among them. Letting it run unbounded is how
// that helper first came back named as a game class's static initializer
// holding more than half the profile, so the highest body is bounded by the
// only evidence available: how long the game's other bodies actually are.
type symbolTable struct {
	starts []uint32
	ends   []uint32
	names  []string
}

func newSymbolTable(vm *jvm.VM) *symbolTable {
	table := &symbolTable{}
	if vm == nil {
		return table
	}
	type entry struct {
		address uint32
		name    string
	}
	entries := make([]entry, 0, 4096)
	for _, class := range vm.AOTClasses() {
		for _, method := range class.Methods {
			// A native method's body is our own supervisor-call stub, not guest
			// code, so naming its address would attribute Host work to the game.
			if method.Body == 0 || method.NativeBody != 0 {
				continue
			}
			entries = append(entries, entry{
				address: method.Body &^ 1,
				name:    class.Name + "." + method.Name + method.Descriptor,
			})
		}
	}
	sort.Slice(entries, func(a, b int) bool {
		if entries[a].address != entries[b].address {
			return entries[a].address < entries[b].address
		}
		return entries[a].name < entries[b].name
	})
	table.starts = make([]uint32, 0, len(entries))
	table.names = make([]string, 0, len(entries))
	for _, item := range entries {
		// Two classes can alias one body address; keeping the first name is
		// enough, and dropping the duplicate keeps the lookup a clean search.
		if count := len(table.starts); count > 0 && table.starts[count-1] == item.address {
			continue
		}
		table.starts = append(table.starts, item.address)
		table.names = append(table.names, item.name)
	}
	table.inferEnds()
	return table
}

// inferEnds fills in where each body stops. A gap to the next body is capped:
// a large one is a hole between two classes' code far more often than it is a
// single method that long, and attributing a hole to the method below it is
// what turns an unknown address into a wrong name.
func (table *symbolTable) inferEnds() {
	table.ends = make([]uint32, len(table.starts))
	if len(table.starts) == 0 {
		return
	}
	gaps := make([]uint32, 0, len(table.starts))
	for index := 0; index+1 < len(table.starts); index++ {
		gaps = append(gaps, table.starts[index+1]-table.starts[index])
		table.ends[index] = table.starts[index] + min(gaps[index], maxSymbolSpan)
	}
	// The highest body is bounded by how long this game's bodies actually run:
	// the ninetieth-percentile gap is the longest body the table has direct
	// evidence for in nine cases out of ten.
	span := uint32(defaultSymbolSpan)
	if len(gaps) > 0 {
		sort.Slice(gaps, func(a, b int) bool { return gaps[a] < gaps[b] })
		span = min(gaps[len(gaps)*9/10], maxSymbolSpan)
	}
	table.ends[len(table.starts)-1] = table.starts[len(table.starts)-1] + span
}

// maxSymbolSpan bounds how far past a body's start an address is still
// attributed to it. KTF method metadata carries no size, so without a bound the
// highest registered body swallows everything above it — and everything above
// it is client.bin's own runtime helpers, which is how the array-load helper
// came back named as a game class's static initializer holding 57% of the
// profile. Measured over one image's 199 bodies, nine in ten sit within
// 0x4a0 bytes of the next; 0x1000 clears that comfortably while still refusing
// the multi-kilobyte reaches that were wrong. Past it an address is reported as
// an address, which is an honest unknown rather than a confident wrong answer.
const maxSymbolSpan = 0x1000

// defaultSymbolSpan bounds the one body of a table too small to have a gap
// distribution to calibrate against.
const defaultSymbolSpan = 0x200

func (table *symbolTable) resolve(address uint32) ProfileFrame {
	frame := ProfileFrame{Address: address}
	if table == nil || len(table.starts) == 0 {
		return frame
	}
	index := sort.Search(len(table.starts), func(i int) bool { return table.starts[i] > address })
	if index == 0 {
		return frame
	}
	start := table.starts[index-1]
	if address >= table.ends[index-1] {
		return frame
	}
	frame.Symbol = table.names[index-1]
	frame.Offset = address - start
	return frame
}

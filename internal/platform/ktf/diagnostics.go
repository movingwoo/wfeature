package ktf

import (
	"fmt"
	"sort"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// traceAOTArgumentShape records how many words an AOT invocation passes and
// flags any integral parameter carrying what looks like a heap pointer. The
// AAPCS puts the fifth word onward on the stack, so a method whose argument
// list crosses that boundary is exactly where a marshalling mistake stops
// being visible in registers — and a byte parameter holding an arena address
// is that mistake made concrete.
func (runtime *initializationRuntime) traceAOTArgumentShape(method jvm.AOTMethodMetadata, methodType jvm.MethodDescriptor, arguments []uint32) {
	if len(arguments) <= 4 {
		return
	}
	suspect := ""
	// arguments[0] is the context slot and arguments[1] the receiver for an
	// instance method, so the declared parameters start after them.
	first := 2
	if method.AccessFlags&0x0008 != 0 {
		first = 1
	}
	for index, parameter := range methodType.Parameters {
		word := first + index
		if word >= len(arguments) || word < 4 {
			continue
		}
		switch parameter.Kind {
		case jvm.TypeBoolean, jvm.TypeByte, jvm.TypeChar, jvm.TypeShort, jvm.TypeInt:
			if value := arguments[word]; value >= wipicrt.PlatformDataBase {
				suspect = fmt.Sprintf(" suspect word %d %v=%#x", word, parameter.Kind, value)
			}
		}
	}
	runtime.CountDiagnostic(fmt.Sprintf("aotcall %s%s words=%d%s",
		method.Name, method.Descriptor, len(arguments), suspect))
}

// aotSymbol is one registered method body, used to name guest code addresses.
type aotSymbol struct {
	body uint32
	name string
}

// aotSymbolIndex collects every registered method body in address order. Only
// classes the guest has loaded so far are registered, so the index grows as a
// session runs and is at its most complete at the moment of a failure.
func (runtime *initializationRuntime) aotSymbolIndex() []aotSymbol {
	symbols := make([]aotSymbol, 0, 256)
	for _, class := range runtime.client.vm.AOTClasses() {
		for _, method := range class.Methods {
			for _, body := range [2]uint32{method.Body, method.NativeBody} {
				if body == 0 {
					continue
				}
				symbols = append(symbols, aotSymbol{
					body: body &^ 1,
					name: class.Name + "." + method.Name + method.Descriptor,
				})
			}
		}
	}
	sort.Slice(symbols, func(left, right int) bool { return symbols[left].body < symbols[right].body })
	return symbols
}

// symbolizeAOTAddress names the AOT method containing a guest code address.
// The metadata carries no body length, so containment is approximated by the
// next registered body: an address past that belongs to a method this session
// has not registered, and naming it anyway would invent a call chain.
func symbolizeAOTAddress(symbols []aotSymbol, address uint32) (string, bool) {
	// Thumb code addresses carry their mode bit; method bodies do not.
	address &^= 1
	index := sort.Search(len(symbols), func(position int) bool { return symbols[position].body > address })
	if index == 0 {
		return "", false
	}
	current := symbols[index-1]
	if index < len(symbols) && address >= symbols[index].body {
		return "", false
	}
	offset := address - current.body
	// Registered bodies are sparse: classes register as they load, and the
	// platform's own runtime helpers never do. Past a plausible body length
	// the nearest name is a guess, and a guessed call chain is worse than an
	// unresolved address.
	if offset > maxAOTBodyLength {
		return "", false
	}
	if offset != 0 {
		return fmt.Sprintf("%s+%#x", current.name, offset), true
	}
	return current.name, true
}

// maxAOTBodyLength bounds how far past a registered body an address may be and
// still be attributed to it.
const maxAOTBodyLength = 0x1000

// Diagnostics snapshots the boundary events this session has crossed. It is
// safe to call between ticks; the returned maps and slices are copies.
func (session *Session) Diagnostics() wipicrt.Diagnostics {
	if session == nil || session.Client == nil {
		return wipicrt.Diagnostics{Counts: map[string]uint32{}}
	}
	client := session.Client
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.runtime == nil {
		return wipicrt.Diagnostics{Counts: map[string]uint32{}}
	}
	runtime := client.runtime
	return wipicrt.Diagnostics{
		Counts: runtime.DiagnosticCounts(),
		Trace:  runtime.Trace.Snapshot(),
		Traced: runtime.Trace.Total,
	}
}

// CountedEvents names every boundary event this session has counted, with the
// total number of events recorded. A live watcher polls for newly appearing
// names, so it takes this instead of Diagnostics: the ordered trace it does
// not read would be the larger half of the snapshot.
func (session *Session) CountedEvents() ([]string, uint64) {
	if session == nil || session.Client == nil {
		return nil, 0
	}
	client := session.Client
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.runtime == nil {
		return nil, 0
	}
	names := make([]string, 0, len(client.runtime.CallCounts))
	for event := range client.runtime.CallCounts {
		names = append(names, event.String())
	}
	return names, client.runtime.Trace.Total
}

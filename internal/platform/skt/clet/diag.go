package clet

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Diagnostics is a snapshot of the runtime boundary events one session has
// crossed. Counts answer "what did this game do", and Trace answers "what was
// it doing just before it stopped behaving" — the ordering the counts lose.
type Diagnostics struct {
	// Counts maps each boundary event name to how often it occurred.
	Counts map[string]uint32 `json:"counts"`
	// Trace holds the most recent events in order, oldest first. It is empty
	// unless the Host asked for tracing with SessionOptions.TraceLimit.
	Trace []TraceEntry `json:"trace"`
	// Traced is the total number of events recorded, including those the
	// bounded trace has already discarded.
	Traced uint64 `json:"traced"`
}

// TraceEntry is one recorded boundary event. Sequence is its position in the
// session's whole event stream, so a gap after a trim stays visible.
type TraceEntry struct {
	Sequence uint64 `json:"sequence"`
	Event    string `json:"event"`
}

// diagKind names the shape of a boundary event.
type diagKind uint8

const (
	// diagText carries a name that was already composed. Events rare enough
	// that composing one costs nothing measurable keep this form.
	diagText diagKind = iota
	diagJavaCall
	diagJVMToAOT
	diagJump
	diagNew
	diagNewArray
	diagCheckUnresolved
	diagCheckUndecided
	diagCheckReject
	// diagGuestEvent is one event handed to the guest's event loop. It is
	// counted per kind, and there is one of them per key, timer and repaint a
	// game receives for as long as it runs.
	diagGuestEvent
	// diagWIPICCall is one call into the WIPI-C interface, counted by slot.
	// It is the busiest crossing this platform counts — every graphics, file
	// and kernel call a title makes arrives through it — and it was still
	// composing its name per crossing after the kinds above had stopped.
	diagWIPICCall
)

// diagEvent identifies one boundary event without composing its name.
//
// Composing one per crossing was the most expensive thing the busy boundaries
// did: a formatted name cost more than the crossing it described, at 6% of host
// CPU in formatting alone, and the depth fmt adds to an already deep guest call
// chain drove most of a further 16% spent copying goroutine stacks. It ran in
// release builds too, which is why turning diagnostics off was never the
// difference anyone expected it to be.
//
// So the parts arrive as they are and the name is composed once per distinct
// event, when a report asks for one. Every field is comparable, which is what
// lets the event itself be the counter's key.
type diagEvent struct {
	kind diagKind
	// text is the whole name for a diagText event, and empty otherwise.
	text string
	// name is the class, method, or symbol the event names, and target the
	// second one where an event relates two.
	name, target, descriptor string
	// nums carries the event's numeric detail in the order its name prints it.
	nums [5]uint32
	// site is the guest address the event was reached from. Collapsing an
	// event drops it, because a name qualified by hundreds of call sites is
	// what exhausts the counter's name budget.
	site    uint32
	hasSite bool
}

// String composes the event's name. The forms here are the report's own, so a
// reader comparing two sessions sees the same text either was written with.
func (event diagEvent) String() string {
	switch event.kind {
	case diagJavaCall:
		name := "java " + event.name + "." + event.target + event.descriptor
		if event.hasSite {
			return fmt.Sprintf("%s @%#x", name, event.site)
		}
		return name
	case diagJVMToAOT:
		return "jvm->aot " + event.name + "." + event.target + event.descriptor
	case diagJump:
		name := fmt.Sprintf("jump%d %#x(%#x, %#x, %#x)",
			event.nums[0], event.nums[1], event.nums[2], event.nums[3], event.nums[4])
		if event.hasSite {
			return fmt.Sprintf("%s @%#x", name, event.site)
		}
		return name
	case diagNew:
		if event.hasSite {
			return fmt.Sprintf("new %s @%#x", event.name, event.site)
		}
		return "new " + event.name
	case diagNewArray:
		if event.hasSite {
			return fmt.Sprintf("newarray %s len=%d @%#x from %#x",
				event.name, event.nums[0], event.nums[1], event.site)
		}
		return fmt.Sprintf("newarray %s len=%d", event.name, event.nums[0])
	case diagCheckUnresolved:
		name := fmt.Sprintf("checktype target unresolved r0=%#x r1=%s", event.nums[0], event.name)
		if event.hasSite {
			return fmt.Sprintf("%s @%#x", name, event.site)
		}
		return name
	case diagCheckUndecided:
		return "checktype undecided " + event.name + " -> " + event.target
	case diagCheckReject:
		return "checktype reject " + event.name + " -> " + event.target
	case diagGuestEvent:
		// The kind is the guest event's own int32, carried in an unsigned
		// field: signed back on the way out so the name is the one this report
		// has always printed, whatever the value.
		return fmt.Sprintf("event %d", int32(event.nums[0]))
	case diagWIPICCall:
		return fmt.Sprintf("wipic %#x", event.nums[0])
	}
	return event.text
}

// collapse drops the call site an event was reached from, and any address that
// only means something alongside it. A collapsed event still counts; it just
// stops being counted once per address that produced it.
func (event diagEvent) collapse() diagEvent {
	if event.kind == diagText {
		if site := strings.LastIndex(event.text, " @0x"); site > 0 {
			event.text = event.text[:site]
		}
		return event
	}
	if event.kind == diagNewArray {
		// The array's own address prints between the length and the site, so
		// dropping the site alone would leave a name still qualified by it.
		event.nums[1] = 0
	}
	event.site = 0
	event.hasSite = false
	return event
}

// keepsDetail reports whether an event is one of the rare failures whose value
// is in its detail, so the name budget does not collapse it away.
func (event diagEvent) keepsDetail() bool {
	if event.kind != diagText {
		// Every taken-apart kind is a routine boundary crossing; the events
		// worth keeping whole are the failures, which all arrive as text.
		return false
	}
	return diagnosticKeepsDetail(event.text)
}

const (
	// DefaultTraceLimit is the window debug-profile Hosts retain. It is long
	// enough to cover the boundary crossings of several frames without the
	// retained strings outgrowing what they describe.
	DefaultTraceLimit = 16384
	// traceLimitCeiling bounds what a Host can ask to retain: a server holding
	// several sessions pays this per session.
	traceLimitCeiling = 65536
)

// traceRing retains the last limit events without growing unboundedly, so a
// game looping for minutes keeps a constant-size window of recent history.
type traceRing struct {
	entries []diagEvent
	cursor  int
	total   uint64
	limit   int
}

func (ring *traceRing) record(event diagEvent) {
	if ring.limit <= 0 {
		return
	}
	if len(ring.entries) < ring.limit {
		ring.entries = append(ring.entries, event)
	} else {
		ring.entries[ring.cursor] = event
		ring.cursor = (ring.cursor + 1) % ring.limit
	}
	ring.total++
}

// snapshot returns the retained events oldest first.
func (ring *traceRing) snapshot() []TraceEntry {
	if len(ring.entries) == 0 {
		return nil
	}
	// The first retained event's sequence is the total minus what is retained,
	// so a trimmed trace still reports where in the stream it resumes.
	first := ring.total - uint64(len(ring.entries)) + 1
	entries := make([]TraceEntry, 0, len(ring.entries))
	for index := range ring.entries {
		position := index
		if len(ring.entries) == ring.limit {
			position = (ring.cursor + index) % ring.limit
		}
		entries = append(entries, TraceEntry{Sequence: first + uint64(index), Event: ring.entries[position].String()})
	}
	return entries
}

// Diagnostics snapshots the boundary events this session has crossed. It is
// safe to call between ticks; the returned maps and slices are copies.
func (session *Session) Diagnostics() Diagnostics {
	if session == nil || session.Client == nil {
		return Diagnostics{Counts: map[string]uint32{}}
	}
	client := session.Client
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil {
		return Diagnostics{Counts: map[string]uint32{}}
	}
	runtime := client.runtime
	return Diagnostics{
		Counts: runtime.diagnosticCounts(),
		Trace:  runtime.trace.snapshot(),
		Traced: runtime.trace.total,
	}
}

// diagnosticCounts composes the name of every counted event. Events are keyed
// by identity while a session runs, so this is where they become the names a
// report is written in.
func (runtime *initializationRuntime) diagnosticCounts() map[string]uint32 {
	counts := make(map[string]uint32, len(runtime.callCounts))
	for event, count := range runtime.callCounts {
		// Two events can only share a name if one was collapsed onto the other,
		// which is the same bucket by intent, so the counts add.
		counts[event.String()] += count
	}
	// How much the use-after-free detector covered is reported alongside what
	// it found, because a report with no fault in it says nothing unless it
	// also says the detector was running. Both stay absent in a release build,
	// where it is not.
	if runtime.shadowedBlocks > 0 {
		counts["arena blocks recorded on release"] = countedTotal(runtime.shadowedBlocks)
	}
	if runtime.checkedBlocks > 0 {
		counts["arena blocks checked on reuse"] = countedTotal(runtime.checkedBlocks)
	}
	return counts
}

// countedTotal narrows a session total to what a report carries, holding at the
// ceiling rather than wrapping to a small number that would read as an idle
// session.
func countedTotal(total uint64) uint32 {
	if total > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(total)
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
	client.run.Lock()
	defer client.run.Unlock()
	if client.runtime == nil {
		return nil, 0
	}
	names := make([]string, 0, len(client.runtime.callCounts))
	for event := range client.runtime.callCounts {
		names = append(names, event.String())
	}
	return names, client.runtime.trace.total
}

// FormatCounts renders the counted events most frequent first, the same shape
// the acceptance probes print. A limit of zero renders every event.
func (diagnostics Diagnostics) FormatCounts(limit int) string {
	type entry struct {
		name  string
		count uint32
	}
	entries := make([]entry, 0, len(diagnostics.Counts))
	for name, count := range diagnostics.Counts {
		entries = append(entries, entry{name, count})
	}
	sort.Slice(entries, func(left, right int) bool {
		if entries[left].count != entries[right].count {
			return entries[left].count > entries[right].count
		}
		return entries[left].name < entries[right].name
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	var builder strings.Builder
	for _, current := range entries {
		fmt.Fprintf(&builder, "  %8d %s\n", current.count, current.name)
	}
	return builder.String()
}

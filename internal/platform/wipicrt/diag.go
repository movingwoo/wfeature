package wipicrt

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
	DiagJavaCall
	DiagJVMToAOT
	DiagJump
	DiagNew
	DiagNewArray
	DiagCheckUnresolved
	DiagCheckUndecided
	DiagCheckReject
	// DiagGuestEvent is one event handed to the guest's event loop. It is
	// counted per kind, and there is one of them per key, timer and repaint a
	// game receives for as long as it runs.
	DiagGuestEvent
	// DiagWIPICCall is one call into the WIPI-C interface, counted by slot.
	// It is the busiest crossing this platform counts — every graphics, file
	// and kernel call a title makes arrives through it — and it was still
	// composing its name per crossing after the kinds above had stopped.
	DiagWIPICCall
)

// DiagEvent identifies one boundary event without composing its name.
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
type DiagEvent struct {
	Kind diagKind
	// Text is the whole name for a diagText event, and empty otherwise.
	Text string
	// Name is the class, method, or symbol the event names, and target the
	// second one where an event relates two.
	Name, Target, Descriptor string
	// Nums carries the event's numeric detail in the order its name prints it.
	Nums [5]uint32
	// Site is the guest address the event was reached from. Collapsing an
	// event drops it, because a name qualified by hundreds of call sites is
	// what exhausts the counter's name budget.
	Site    uint32
	HasSite bool
}

// String composes the event's name. The forms here are the report's own, so a
// reader comparing two sessions sees the same text either was written with.
func (event DiagEvent) String() string {
	switch event.Kind {
	case DiagJavaCall:
		name := "java " + event.Name + "." + event.Target + event.Descriptor
		if event.HasSite {
			return fmt.Sprintf("%s @%#x", name, event.Site)
		}
		return name
	case DiagJVMToAOT:
		return "jvm->aot " + event.Name + "." + event.Target + event.Descriptor
	case DiagJump:
		name := fmt.Sprintf("jump%d %#x(%#x, %#x, %#x)",
			event.Nums[0], event.Nums[1], event.Nums[2], event.Nums[3], event.Nums[4])
		if event.HasSite {
			return fmt.Sprintf("%s @%#x", name, event.Site)
		}
		return name
	case DiagNew:
		if event.HasSite {
			return fmt.Sprintf("new %s @%#x", event.Name, event.Site)
		}
		return "new " + event.Name
	case DiagNewArray:
		if event.HasSite {
			return fmt.Sprintf("newarray %s len=%d @%#x from %#x",
				event.Name, event.Nums[0], event.Nums[1], event.Site)
		}
		return fmt.Sprintf("newarray %s len=%d", event.Name, event.Nums[0])
	case DiagCheckUnresolved:
		name := fmt.Sprintf("checktype target unresolved r0=%#x r1=%s", event.Nums[0], event.Name)
		if event.HasSite {
			return fmt.Sprintf("%s @%#x", name, event.Site)
		}
		return name
	case DiagCheckUndecided:
		return "checktype undecided " + event.Name + " -> " + event.Target
	case DiagCheckReject:
		return "checktype reject " + event.Name + " -> " + event.Target
	case DiagGuestEvent:
		// The kind is the guest event's own int32, carried in an unsigned
		// field: signed back on the way out so the name is the one this report
		// has always printed, whatever the value.
		return fmt.Sprintf("event %d", int32(event.Nums[0]))
	case DiagWIPICCall:
		return fmt.Sprintf("wipic %#x", event.Nums[0])
	}
	return event.Text
}

// collapse drops the call site an event was reached from, and any address that
// only means something alongside it. A collapsed event still counts; it just
// stops being counted once per address that produced it.
func (event DiagEvent) collapse() DiagEvent {
	if event.Kind == diagText {
		if site := strings.LastIndex(event.Text, " @0x"); site > 0 {
			event.Text = event.Text[:site]
		}
		return event
	}
	if event.Kind == DiagNewArray {
		// The array's own address prints between the length and the site, so
		// dropping the site alone would leave a name still qualified by it.
		event.Nums[1] = 0
	}
	event.Site = 0
	event.HasSite = false
	return event
}

// keepsDetail reports whether an event is one of the rare failures whose value
// is in its detail, so the name budget does not collapse it away.
func (event DiagEvent) keepsDetail() bool {
	if event.Kind != diagText {
		// Every taken-apart kind is a routine boundary crossing; the events
		// worth keeping whole are the failures, which all arrive as text.
		return false
	}
	return DiagnosticKeepsDetail(event.Text)
}

const (
	// DefaultTraceLimit is the window debug-profile Hosts retain. It is long
	// enough to cover the boundary crossings of several frames without the
	// retained strings outgrowing what they describe.
	DefaultTraceLimit = 16384
	// TraceLimitCeiling bounds what a Host can ask to retain: a server holding
	// several sessions pays this per session.
	TraceLimitCeiling = 65536
)

// TraceRing retains the last limit events without growing unboundedly, so a
// game looping for minutes keeps a constant-size window of recent history.
type TraceRing struct {
	entries []DiagEvent
	Cursor  int
	Total   uint64
	Limit   int
}

func (ring *TraceRing) Record(event DiagEvent) {
	if ring.Limit <= 0 {
		return
	}
	if len(ring.entries) < ring.Limit {
		ring.entries = append(ring.entries, event)
	} else {
		ring.entries[ring.Cursor] = event
		ring.Cursor = (ring.Cursor + 1) % ring.Limit
	}
	ring.Total++
}

// Snapshot returns the retained events oldest first.
func (ring *TraceRing) Snapshot() []TraceEntry {
	if len(ring.entries) == 0 {
		return nil
	}
	// The first retained event's sequence is the total minus what is retained,
	// so a trimmed trace still reports where in the stream it resumes.
	first := ring.Total - uint64(len(ring.entries)) + 1
	entries := make([]TraceEntry, 0, len(ring.entries))
	for index := range ring.entries {
		position := index
		if len(ring.entries) == ring.Limit {
			position = (ring.Cursor + index) % ring.Limit
		}
		entries = append(entries, TraceEntry{Sequence: first + uint64(index), Event: ring.entries[position].String()})
	}
	return entries
}

// Diagnostics snapshots the boundary events this client has crossed. It is
// safe to call between ticks; the returned maps and slices are copies.
func (client *Client) Diagnostics() Diagnostics {
	if client == nil {
		return Diagnostics{Counts: map[string]uint32{}}
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.Runtime == nil {
		return Diagnostics{Counts: map[string]uint32{}}
	}
	runtime := client.Runtime
	return Diagnostics{
		Counts: runtime.DiagnosticCounts(),
		Trace:  runtime.Trace.Snapshot(),
		Traced: runtime.Trace.Total,
	}
}

// DiagnosticCounts composes the name of every counted event. Events are keyed
// by identity while a session runs, so this is where they become the names a
// report is written in.
func (runtime *Runtime) DiagnosticCounts() map[string]uint32 {
	counts := make(map[string]uint32, len(runtime.CallCounts))
	for event, count := range runtime.CallCounts {
		// Two events can only share a name if one was collapsed onto the other,
		// which is the same bucket by intent, so the counts add.
		counts[event.String()] += count
	}
	// How much the use-after-free detector covered is reported alongside what
	// it found, because a report with no fault in it says nothing unless it
	// also says the detector was running. Both stay absent in a release build,
	// where it is not.
	if runtime.ShadowedBlocks > 0 {
		counts["arena blocks recorded on release"] = countedTotal(runtime.ShadowedBlocks)
	}
	if runtime.CheckedBlocks > 0 {
		counts["arena blocks checked on reuse"] = countedTotal(runtime.CheckedBlocks)
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

// CountedEvents names every boundary event this client has counted, with the
// total number of events recorded. A live watcher polls for newly appearing
// names, so it takes this instead of Diagnostics: the ordered trace it does
// not read would be the larger half of the snapshot.
func (client *Client) CountedEvents() ([]string, uint64) {
	if client == nil {
		return nil, 0
	}
	client.Run.Lock()
	defer client.Run.Unlock()
	if client.Runtime == nil {
		return nil, 0
	}
	names := make([]string, 0, len(client.Runtime.CallCounts))
	for event := range client.Runtime.CallCounts {
		names = append(names, event.String())
	}
	return names, client.Runtime.Trace.Total
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

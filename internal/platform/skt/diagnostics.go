package skt

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// Diagnostics answers the question a title cannot: what did it actually use.
//
// The other WIPI platform answers it with stub counts and an unimplemented
// list. Nothing here did, so finding a gap meant reading the debug build's
// instruction log by eye — folding millions of lines by class and method until
// the class that was running stood out. That works, and it is how the frame
// loop's defects and one title's guardian screen were found, but it is not
// something to reach for twice.
//
// Two lists answer most of it. **Classes** says which of this runtime's Java
// surface a title loaded, which is the census any pruning has to start from:
// a class no local title loads is a class nothing here proves is needed.
// **Natives** says which registered methods were called and how often, which is
// the same question one level down — a registration with a zero beside it is
// surface that has never been exercised by a real title.
//
// **Missing** is the other direction: a class the title asked for that this
// runtime does not have. That is the list to implement from.
type Diagnostics struct {
	// Classes are the classes the VM loaded, in name order.
	Classes []string `json:"classes"`
	// Missing maps each class the title asked for and did not get to why.
	Missing map[string]string `json:"missing"`
	// Natives counts the calls each registered native method received, keyed
	// by "class.name descriptor". A zero is the point of the map.
	Natives map[string]uint64 `json:"natives"`
}

// nativeCounter counts one registered native method's calls.
type nativeCounter struct {
	key   string
	calls atomic.Uint64
}

// registerNative registers one native and counts what it is called. The
// counter is an atomic add on a per-method value the closure already holds, so
// what it costs a call is one increment — cheap enough to leave on in both
// build profiles, and worth that because the report is only honest if it
// reflects an ordinary run rather than a specially built one.
func (runtime *Runtime) registerNative(class, name, descriptor string, method jvm.NativeMethod) error {
	counter := &nativeCounter{key: nativeKey(class, name, descriptor)}
	runtime.nativeMu.Lock()
	if runtime.natives == nil {
		runtime.natives = make(map[string]*nativeCounter)
	}
	runtime.natives[counter.key] = counter
	runtime.nativeMu.Unlock()
	return runtime.VM.RegisterNative(class, name, descriptor, func(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		counter.calls.Add(1)
		return method(vm, arguments)
	})
}

func nativeKey(class, name, descriptor string) string {
	return class + "." + name + descriptor
}

// Diagnostics snapshots what this run has used. It is safe to call between
// dispatches; the maps and slices are copies.
func (runtime *Runtime) Diagnostics() Diagnostics {
	report := Diagnostics{
		Missing: map[string]string{},
		Natives: map[string]uint64{},
	}
	if runtime == nil {
		return report
	}
	if runtime.VM != nil {
		loaded, missing := runtime.VM.ClassCensus()
		report.Classes = loaded
		if missing != nil {
			report.Missing = missing
		}
	}
	runtime.nativeMu.RLock()
	for key, counter := range runtime.natives {
		report.Natives[key] = counter.calls.Load()
	}
	runtime.nativeMu.RUnlock()
	return report
}

// FormatCounts renders the report the way the acceptance probes print one:
// what was called, most first, then what was never called, then what was
// missing. A limit of zero renders everything.
func (report Diagnostics) FormatCounts(limit int) string {
	type entry struct {
		name  string
		calls uint64
	}
	called := make([]entry, 0, len(report.Natives))
	unused := make([]string, 0, len(report.Natives))
	for name, calls := range report.Natives {
		if calls == 0 {
			unused = append(unused, name)
			continue
		}
		called = append(called, entry{name, calls})
	}
	sort.Slice(called, func(left, right int) bool {
		if called[left].calls != called[right].calls {
			return called[left].calls > called[right].calls
		}
		return called[left].name < called[right].name
	})
	sort.Strings(unused)

	var builder strings.Builder
	fmt.Fprintf(&builder, "%d classes loaded, %d natives called of %d registered\n",
		len(report.Classes), len(called), len(report.Natives))
	shown := called
	if limit > 0 && len(shown) > limit {
		shown = shown[:limit]
	}
	for _, current := range shown {
		fmt.Fprintf(&builder, "  %8d %s\n", current.calls, current.name)
	}
	if len(report.Missing) > 0 {
		names := make([]string, 0, len(report.Missing))
		for name := range report.Missing {
			names = append(names, name)
		}
		sort.Strings(names)
		builder.WriteString("\nasked for and not here:\n")
		for _, name := range names {
			fmt.Fprintf(&builder, "  %s: %s\n", name, report.Missing[name])
		}
	}
	if len(unused) > 0 {
		fmt.Fprintf(&builder, "\nnever called (%d):\n", len(unused))
		for _, name := range unused {
			fmt.Fprintf(&builder, "  %s\n", name)
		}
	}
	return builder.String()
}

package lgt

import (
	"sort"

	"github.com/movingwoo/wfeature/internal/guestprofile"
)

// Profiling answers "which guest code is this title spending its instructions
// in". The ARM core samples addresses and `internal/guestprofile` ranks them;
// what is here is the part that is LGT's own — deciding what an address is.
//
// A Clet carries no method names at all: it is a stripped ARM executable, so
// there is nothing to resolve an address against the way KTF resolves one
// against a registered AOT body. That is not a gap to fill with the section
// name, and doing so is worse than doing nothing: naming every address in the
// module `.text` collapses the whole ranking into one row. The shared region
// grouping is what a nameless image needs, and it only groups frames that
// carry no symbol — so **executable addresses are deliberately left unnamed**
// and come back as the address range of the loop that is running, which is
// what a disassembler is pointed at.
//
// What does get a name is everything that is not the title's code: the heap,
// the platform's stub area, the stack, and the module's own data sections. A
// sample there means something a bare hex number cannot say — code executing
// out of the stub area is a platform call in flight, and one executing out of
// the heap is a title that copied code there.
type (
	// ProfileFrame is one guest address with wherever it turned out to be.
	ProfileFrame = guestprofile.Frame
	// Profile is a snapshot of guest execution.
	Profile = guestprofile.Profile
)

// EnableProfile starts sampling guest addresses every interval executed
// instructions; zero selects the core default. Sampling costs a stack walk per
// interval, so it is opt-in rather than always on.
func (session *Session) EnableProfile(interval uint64) {
	if session == nil || session.client == nil {
		return
	}
	session.client.core.EnableProfile(interval)
}

// DisableProfile stops sampling and discards what was collected.
func (session *Session) DisableProfile() {
	if session == nil || session.client == nil {
		return
	}
	session.client.core.DisableProfile()
}

// ResetProfile discards the samples so far and keeps sampling. A run that
// drives a title through minutes of loading to reach the scene it wants to
// measure calls this on arrival, so the profile covers the scene rather than
// everything before it.
func (session *Session) ResetProfile() {
	if session == nil || session.client == nil {
		return
	}
	session.client.core.ResetProfile()
}

// Profile returns the samples collected so far, each address placed.
func (session *Session) Profile() Profile {
	if session == nil || session.client == nil {
		return Profile{}
	}
	raw := session.client.core.Profile()
	return guestprofile.Build(raw, session.client.newAddressPlacer().place)
}

// addressPlacer names the spans of the address space that are not the title's
// executable code. Held sorted, so a lookup is a search rather than a scan.
type addressPlacer struct {
	starts []uint32
	ends   []uint32
	names  []string
}

// newAddressPlacer builds the table once per Profile call. It is not cached on
// the client: a profile is asked for at the end of a run, and a table built at
// load time would be one more thing that can go stale for no gain.
func (client *Client) newAddressPlacer() *addressPlacer {
	placer := &addressPlacer{}
	if client == nil {
		return placer
	}
	type span struct {
		start uint32
		end   uint32
		name  string
	}
	spans := make([]span, 0, 8)
	if client.module != nil {
		for _, section := range client.module.Sections {
			// An executable section is left out on purpose: see the package
			// comment. Its addresses group into the loops that are running.
			if section.Size == 0 || section.Executable {
				continue
			}
			spans = append(spans, span{section.Address, section.Address + section.Size, section.Name})
		}
	}
	spans = append(spans,
		span{heapBase, heapBase + uint32(heapSize), "[heap]"},
		span{platformDataBase, platformDataBase + uint32(platformDataSize), "[platform data]"},
		span{platformCodeBase, platformCodeBase + uint32(platformCodeSize), "[platform stubs]"},
		span{stackBase, stackBase + uint32(stackSize), "[stack]"},
	)
	sort.Slice(spans, func(a, b int) bool { return spans[a].start < spans[b].start })
	for _, entry := range spans {
		placer.starts = append(placer.starts, entry.start)
		placer.ends = append(placer.ends, entry.end)
		placer.names = append(placer.names, entry.name)
	}
	return placer
}

// place answers the frame for one address. An address inside no named span
// keeps an empty symbol, which is what sends it to the shared region grouping.
func (placer *addressPlacer) place(address uint32) guestprofile.Frame {
	at := sort.Search(len(placer.starts), func(i int) bool { return placer.ends[i] > address })
	if at == len(placer.starts) || placer.starts[at] > address {
		return guestprofile.Frame{Address: address}
	}
	return guestprofile.Frame{
		Address: address,
		Symbol:  placer.names[at],
		Offset:  address - placer.starts[at],
	}
}

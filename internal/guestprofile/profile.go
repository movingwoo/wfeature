// Package guestprofile turns the ARM core's raw address samples into a
// readable ranking of the guest code a title is spending its instructions in.
//
// The sampling itself is `internal/armcore`, and naming an address is the
// platform's job — KTF resolves against registered AOT method bodies, LGT
// against its module's ELF sections. Everything between those two is the same
// question asked the same way, so it lives here rather than once per platform:
// the ranking, the stack rendering, and the grouping that makes a nameless
// image readable at all.
package guestprofile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Frame is one guest address with whatever name could be resolved for it.
// Symbol is empty when nothing covers the address, which is the normal case
// for a title whose code carries no names.
type Frame struct {
	Address uint32
	Symbol  string
	Offset  uint32
}

func (frame Frame) String() string {
	if frame.Symbol == "" {
		return fmt.Sprintf("%#x", frame.Address)
	}
	return fmt.Sprintf("%s+%#x", frame.Symbol, frame.Offset)
}

// symbolOrAddress groups the leaf ranking by name where there is one. Grouping
// by address instead would split one hot method across every instruction in
// its loop, which is the opposite of what a self-time ranking is for.
func (frame Frame) symbolOrAddress() string {
	if frame.Symbol == "" {
		return fmt.Sprintf("%#x", frame.Address)
	}
	return frame.Symbol
}

// Stack is one sampled stack, innermost frame first.
type Stack struct {
	Frames []Frame
	Count  uint64
	Share  float64
}

// Leaf is the self time of one symbol: how many samples stopped in it,
// regardless of who called it. This is the ranking to read first — it is the
// answer to "what is running", where the stacks answer "who asked for it".
type Leaf struct {
	Symbol string
	Count  uint64
	Share  float64
}

// Profile is a symbolized snapshot of guest execution.
type Profile struct {
	Stacks []Stack
	Leaves []Leaf
	// Samples and Steps carry over from the core: their ratio is the effective
	// sample interval, which is what makes counts comparable between runs of
	// different lengths.
	Samples uint64
	Steps   uint64
	// CallersDropped is how many samples lost their callers to the core's
	// distinct-stack limit.
	CallersDropped uint64
}

// Build resolves every sampled address through resolve and ranks the result.
// A nil resolve names nothing, which is what a platform with no symbols at all
// passes.
func Build(raw armcore.Profile, resolve func(uint32) Frame) Profile {
	if resolve == nil {
		resolve = func(address uint32) Frame { return Frame{Address: address} }
	}
	profile := Profile{Samples: raw.Taken, Steps: raw.Steps, CallersDropped: raw.CallersDropped}
	if raw.Taken == 0 {
		return profile
	}
	unnamed := make([]uint32, 0, len(raw.Samples))
	for _, sample := range raw.Samples {
		frames := make([]Frame, len(sample.Stack))
		for index, address := range sample.Stack {
			frames[index] = resolve(address)
		}
		profile.Stacks = append(profile.Stacks, Stack{
			Frames: frames,
			Count:  sample.Count,
			Share:  float64(sample.Count) / float64(raw.Taken),
		})
		if frames[0].Symbol == "" {
			unnamed = append(unnamed, frames[0].Address)
		}
	}
	regions := newRegionIndex(unnamed)
	leaves := make(map[string]uint64)
	for index, sample := range raw.Samples {
		leaves[regions.label(profile.Stacks[index].Frames[0])] += sample.Count
	}
	for symbol, count := range leaves {
		profile.Leaves = append(profile.Leaves, Leaf{
			Symbol: symbol,
			Count:  count,
			Share:  float64(count) / float64(raw.Taken),
		})
	}
	sort.Slice(profile.Leaves, func(a, b int) bool {
		if profile.Leaves[a].Count != profile.Leaves[b].Count {
			return profile.Leaves[a].Count > profile.Leaves[b].Count
		}
		return profile.Leaves[a].Symbol < profile.Leaves[b].Symbol
	})
	return profile
}

// regionGap is how far two sampled addresses may sit apart and still be called
// one region. Thumb instructions are two or four bytes and a hot loop is
// sampled at nearly every one of them, so a stretch this long with no sample
// in it is the end of the code that is running rather than a cold branch
// inside it. Erring small splits one region in two, which costs a reader a
// line; erring large merges two unrelated hot spots under one label, which
// costs them the answer.
const regionGap = 0x20

// regionIndex groups sampled addresses that no symbol covers into contiguous
// runs, so the self-time ranking answers "which code is running" for a game
// whose code carries no names at all.
//
// A pure WIPI C game is exactly that game: its logic is native ARM with no
// method bodies to resolve against, and ranking each instruction on its own
// spread one image-blitting loop of a drag-racing game over 2874 entries of
// three quarters of a percent each — a profile that measured the right thing
// and reported it as noise.
type regionIndex struct {
	starts []uint32
	ends   []uint32
}

func newRegionIndex(addresses []uint32) *regionIndex {
	index := &regionIndex{}
	if len(addresses) == 0 {
		return index
	}
	sorted := append([]uint32(nil), addresses...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	start, end := sorted[0], sorted[0]
	for _, address := range sorted[1:] {
		if address-end > regionGap {
			index.starts = append(index.starts, start)
			index.ends = append(index.ends, end)
			start = address
		}
		end = address
	}
	index.starts = append(index.starts, start)
	index.ends = append(index.ends, end)
	return index
}

// label names the bucket a leaf frame is counted under: its symbol where it
// has one, and the region containing it where it does not.
func (index *regionIndex) label(frame Frame) string {
	if frame.Symbol != "" {
		return frame.Symbol
	}
	at := sort.Search(len(index.starts), func(i int) bool { return index.ends[i] >= frame.Address })
	if at == len(index.starts) || index.starts[at] > frame.Address {
		return frame.symbolOrAddress()
	}
	if index.starts[at] == index.ends[at] {
		return fmt.Sprintf("%#x", index.starts[at])
	}
	return fmt.Sprintf("%#x-%#x", index.starts[at], index.ends[at])
}

// Folded renders the profile in flamegraph-folded form — one line per stack,
// outermost frame first, semicolon separated, followed by the count. It is the
// input format of flamegraph.pl and inferno-flamegraph.
func (profile Profile) Folded() string {
	var builder strings.Builder
	for _, stack := range profile.Stacks {
		for index := len(stack.Frames) - 1; index >= 0; index-- {
			if index != len(stack.Frames)-1 {
				builder.WriteByte(';')
			}
			builder.WriteString(stack.Frames[index].String())
		}
		fmt.Fprintf(&builder, " %d\n", stack.Count)
	}
	return builder.String()
}

// Report renders the human-readable summary: the self-time ranking, then the
// hottest stacks. limit bounds each section; zero selects a screenful.
func (profile Profile) Report(limit int) string {
	if limit <= 0 {
		limit = 20
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "guest profile: %d samples over %d instructions", profile.Samples, profile.Steps)
	if profile.Samples > 0 {
		fmt.Fprintf(&builder, " (1 sample / %d)", profile.Steps/profile.Samples)
	}
	builder.WriteByte('\n')
	if profile.CallersDropped > 0 {
		fmt.Fprintf(&builder, "  %d samples lost their callers at the distinct-stack limit\n", profile.CallersDropped)
	}
	if profile.Samples == 0 {
		return builder.String()
	}

	builder.WriteString("\nself time\n")
	for index, leaf := range profile.Leaves {
		if index >= limit {
			fmt.Fprintf(&builder, "  ... %d more\n", len(profile.Leaves)-limit)
			break
		}
		fmt.Fprintf(&builder, "  %6.2f%%  %8d  %s\n", leaf.Share*100, leaf.Count, leaf.Symbol)
	}

	if !profile.hasCallers() {
		// Every stack is its leaf. Printing them would repeat the ranking above
		// line for line under a heading promising callers, so the reason no
		// caller was found is the more useful thing to print.
		builder.WriteString("\nno stack walked past its leaf: this image keeps no frame pointer in r7\n")
		return builder.String()
	}
	builder.WriteString("\nhottest stacks\n")
	for index, stack := range profile.Stacks {
		if index >= limit {
			fmt.Fprintf(&builder, "  ... %d more\n", len(profile.Stacks)-limit)
			break
		}
		fmt.Fprintf(&builder, "  %6.2f%%  %8d  %s\n", stack.Share*100, stack.Count, stack.Frames[0])
		for _, frame := range stack.Frames[1:] {
			fmt.Fprintf(&builder, "                       from %s\n", frame)
		}
	}
	return builder.String()
}

// hasCallers reports whether any stack walked past its leaf. RVCT builds these
// images with r7 as a general register rather than a frame pointer, and the
// walk then correctly yields a single frame for every sample.
func (profile Profile) hasCallers() bool {
	for _, stack := range profile.Stacks {
		if len(stack.Frames) > 1 {
			return true
		}
	}
	return false
}

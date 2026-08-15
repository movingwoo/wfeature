package armcore

import (
	"encoding/binary"
	"sort"
	"sync"
)

const (
	// defaultProfileInterval is how many executed guest instructions separate
	// two samples. A thousand keeps the stack walk under a tenth of a percent
	// of run time while still filling a profile within a few seconds of play.
	defaultProfileInterval = uint64(1000)
	// profileMaxStack bounds one recorded stack. It caps memory per sample and
	// stops a corrupt frame chain from looping forever.
	profileMaxStack = 32
	// profileMaxStacks bounds how many distinct stacks are counted separately.
	// Past it the walk is folded away and only the leaf PC is counted, so a
	// game that produces unbounded stack shapes still yields a usable
	// instruction-level profile instead of silently dropping samples.
	profileMaxStacks = 1 << 16
)

// ProfileSample is one guest stack and how often sampling landed on it. Stack
// is innermost first: element zero is the sampled PC and the rest are return
// addresses walked out towards the thread entry.
type ProfileSample struct {
	Stack []uint32
	Count uint64
}

// Profile is a snapshot of everything sampled since the last reset.
type Profile struct {
	Samples []ProfileSample
	// Taken is the number of samples that went into Samples, and Steps is how
	// many guest instructions they cover. Their ratio is the effective sample
	// interval, which is what makes a count comparable across two runs of
	// different lengths.
	Taken uint64
	Steps uint64
	// CallersDropped counts samples whose stack was cut back to the leaf PC
	// because the distinct-stack limit was reached. A non-zero value means the
	// callers of a hot leaf are under-reported, not that samples were lost.
	CallersDropped uint64
}

// profiler accumulates guest stack samples. It is only allocated when
// profiling is switched on, so an unprofiled core carries a single nil check
// per quantum.
type profiler struct {
	mu       sync.Mutex
	interval uint64
	// debt is executed steps not yet accounted to a sample. Sampling on
	// accumulated steps rather than once per quantum is what keeps the profile
	// proportional to execution: quanta end early on every supervisor call, so
	// per-quantum sampling would over-count whatever code calls out most.
	debt      uint64
	stacks    map[string]uint64
	maxStacks int
	taken     uint64
	steps     uint64
	folded    bool
	foldedN   uint64
}

// EnableProfile starts sampling guest stacks every interval executed
// instructions. Zero uses the default interval. Sampling is off until this is
// called and stays on until DisableProfile.
func (core *Core) EnableProfile(interval uint64) {
	if core == nil {
		return
	}
	if interval == 0 {
		interval = defaultProfileInterval
	}
	core.profileMu.Lock()
	defer core.profileMu.Unlock()
	core.profile = &profiler{interval: interval, stacks: make(map[string]uint64), maxStacks: profileMaxStacks}
}

// DisableProfile stops sampling and discards what was collected.
func (core *Core) DisableProfile() {
	if core == nil {
		return
	}
	core.profileMu.Lock()
	defer core.profileMu.Unlock()
	core.profile = nil
}

// ResetProfile clears the collected samples but keeps sampling on. A harness
// that has to drive a game through minutes of loading to reach the scene it
// wants to measure calls this once it arrives, so the profile covers the scene
// and not the loading.
func (core *Core) ResetProfile() {
	profile := core.currentProfiler()
	if profile == nil {
		return
	}
	profile.mu.Lock()
	defer profile.mu.Unlock()
	profile.stacks = make(map[string]uint64)
	profile.taken = 0
	profile.steps = 0
	profile.debt = 0
	profile.folded = false
	profile.foldedN = 0
}

// Profile returns what has been sampled so far, hottest stack first. It leaves
// collection running.
func (core *Core) Profile() Profile {
	profile := core.currentProfiler()
	if profile == nil {
		return Profile{}
	}
	profile.mu.Lock()
	defer profile.mu.Unlock()

	samples := make([]ProfileSample, 0, len(profile.stacks))
	for key, count := range profile.stacks {
		samples = append(samples, ProfileSample{Stack: decodeProfileKey(key), Count: count})
	}
	sort.Slice(samples, func(a, b int) bool {
		if samples[a].Count != samples[b].Count {
			return samples[a].Count > samples[b].Count
		}
		// Ties break on the stack itself so a profile of a deterministic run is
		// byte-identical between runs and can be diffed.
		return compareStacks(samples[a].Stack, samples[b].Stack) < 0
	})
	return Profile{
		Samples:        samples,
		Taken:          profile.taken,
		Steps:          profile.steps,
		CallersDropped: profile.foldedN,
	}
}

func (core *Core) currentProfiler() *profiler {
	if core == nil {
		return nil
	}
	core.profileMu.RLock()
	defer core.profileMu.RUnlock()
	return core.profile
}

// chunkForSample shortens the next quantum so it ends exactly on the sample
// interval. Without it a sample lands wherever the quantum happened to stop,
// and quanta stop at supervisor calls: a profile of a game that calls into the
// Host constantly then reports the platform stubs as the hottest code in the
// game, which is an artefact of where execution was interrupted rather than of
// where it spends its instructions.
func (profile *profiler) chunkForSample(limit uint64) uint64 {
	profile.mu.Lock()
	defer profile.mu.Unlock()
	if due := profile.interval - profile.debt; due < limit {
		return due
	}
	return limit
}

// sampleProfile accounts steps executed guest instructions and records a stack
// once the sample interval is reached. It runs with the execution lock held and
// the memory quantum already released, so the guest stack it walks is the one
// the run just stopped on. Because chunkForSample stopped the quantum on the
// interval, that stopping point is the sampled instruction rather than the next
// supervisor call.
func (core *Core) sampleProfile(profile *profiler, context *Context, steps uint64) {
	profile.mu.Lock()
	defer profile.mu.Unlock()

	profile.steps += steps
	profile.debt += steps
	if profile.debt < profile.interval {
		return
	}
	profile.debt -= profile.interval

	stack := core.walkGuestStack(context)
	if profile.folded {
		stack = stack[:1]
		profile.foldedN++
	}
	key := encodeProfileKey(stack)
	if _, seen := profile.stacks[key]; !seen && len(profile.stacks) >= profile.maxStacks {
		profile.folded = true
		profile.foldedN++
		key = encodeProfileKey(stack[:1])
	}
	profile.stacks[key]++
	profile.taken++
}

// walkGuestStack returns the sampled PC followed by the return addresses of
// the frames above it. KTF guest code is Thumb built with a frame pointer in
// r7, where each frame stores the caller's r7 at [r7] and its return address
// at [r7+4]. The walk is a best effort: a leaf that has not built its frame
// yet, or hand-written assembly that keeps no frame, simply yields a shorter
// stack. The leaf PC — the part the instruction-level profile needs — is
// always right.
func (core *Core) walkGuestStack(context *Context) []uint32 {
	stack := make([]uint32, 1, 8)
	stack[0] = context.PC()
	if !context.Thumb() {
		return stack
	}

	frame := context.Registers[7]
	var words [8]byte
	for depth := 0; depth < profileMaxStack; depth++ {
		if frame == 0 || frame&3 != 0 {
			break
		}
		if err := core.memory.Read(frame, words[:]); err != nil {
			break
		}
		caller := binary.LittleEndian.Uint32(words[0:4])
		returnAddress := binary.LittleEndian.Uint32(words[4:8])
		// Bit zero of a saved return address is Thumb state. The guest only
		// ever returns into Thumb code, so a clear bit means these two words
		// are not a frame and the chain has run off into unrelated data.
		if returnAddress&1 == 0 {
			break
		}
		stack = append(stack, returnAddress&^1)
		// Frames are pushed downwards, so walking out has to move upwards.
		// Anything else is a cycle or garbage.
		if caller <= frame {
			break
		}
		frame = caller
	}
	return stack
}

func encodeProfileKey(stack []uint32) string {
	buffer := make([]byte, len(stack)*4)
	for index, address := range stack {
		binary.LittleEndian.PutUint32(buffer[index*4:], address)
	}
	return string(buffer)
}

func decodeProfileKey(key string) []uint32 {
	stack := make([]uint32, len(key)/4)
	for index := range stack {
		stack[index] = binary.LittleEndian.Uint32([]byte(key)[index*4:])
	}
	return stack
}

func compareStacks(a, b []uint32) int {
	for index := 0; index < len(a) && index < len(b); index++ {
		switch {
		case a[index] < b[index]:
			return -1
		case a[index] > b[index]:
			return 1
		}
	}
	return len(a) - len(b)
}

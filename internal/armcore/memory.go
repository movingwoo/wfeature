package armcore

import (
	"encoding/binary"
	"math/bits"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	// The page table is two levels, indexed by the top and middle bits of the
	// address, so finding a page is two array reads. It was a Go map, and a
	// map lookup per instruction fetch was the single most expensive thing
	// the interpreter did on real games — cheap in a benchmark whose map holds
	// two pages, costly against the thousands a game maps, where every lookup
	// is a hash plus a probe into cold memory.
	memoryDirectoryShift = 22
	memoryDirectoryCount = 1 << (32 - memoryDirectoryShift)
	memoryTableShift     = 12
	memoryTableCount     = 1 << (memoryDirectoryShift - memoryTableShift)

	memoryPageShift = 12
	// decodedPerPage is one cache entry per halfword of a page.
	decodedPerPage = 1 << (memoryPageShift - 1)
	// armDecodedPerPage is one cache entry per word of a page.
	armDecodedPerPage = 1 << (memoryPageShift - 2)
	memoryPageSize    = uint64(1 << memoryPageShift)
	memoryPageMask    = uint32(memoryPageSize - 1)
	guestAddressEnd   = uint64(1) << 32
	maxMappings       = 4096

	// dataPageWays is how many recently used data pages are remembered. It is
	// a power of two so the way is a mask of the page number rather than a
	// search. Swept on one title's field scene, the cost per guest instruction
	// fell 5.68ns at one way, 5.56 at four, 5.47 at eight, 5.31 at thirty-two
	// and 5.30 at sixty-four, and stopped moving at 5.28 from 128 upwards.
	//
	// **The count is 256 rather than the 64 that sweep would have settled on,
	// and a synthetic benchmark is why.** A blit between two surfaces half a
	// megabyte apart alternates between page numbers exactly 128 apart, which
	// at 64 ways is the same slot every time: `BenchmarkEngineBlitCrossPage`
	// reads 7.2ns a step with one way, 7.6 with 64, and 5.2 with 256, where
	// the two surfaces finally land in different slots. That stride is not
	// unusual — it is what a pair of framebuffers allocated in order looks
	// like — so the table is sized past it. See the Memory field for what the
	// ways are for.
	dataPageWays    = 256
	dataPageWayMask = dataPageWays - 1
)

type Permission uint8

const (
	PermissionRead Permission = 1 << iota
	PermissionWrite
	PermissionExecute

	PermissionReadWrite        = PermissionRead | PermissionWrite
	PermissionReadExecute      = PermissionRead | PermissionExecute
	PermissionReadWriteExecute = PermissionRead | PermissionWrite | PermissionExecute
)

type memoryPage struct {
	data []byte
	// permission is what the mappings allow across the whole of this page,
	// remembered here so that an access does not have to find that out from
	// the mapping list. Zero means "ask the mappings": either nothing is
	// mapped here, or a mapping covers only part of the page and the answer
	// therefore depends on which byte is being touched.
	//
	// It is what makes a miss cheap. The remembered mapping answers only while
	// consecutive accesses stay inside one mapping, and a real title's do not:
	// on two local archives 99% and 57% of the accesses that reach the general
	// path are there because the mapping changed, not because the page did.
	// Each of those used to pay a scan of the mapping list; the page carries
	// the same answer, and it is already in cache because the access needs its
	// bytes anyway.
	//
	// Mapping is the only thing that changes it, and Map clears every page's
	// copy along with the decode caches it already retires.
	permission Permission
	// decoded caches one entry per halfword of this page: the Thumb form the
	// encoding takes, plus the encoding itself. A hot loop then costs an index
	// rather than a mapping scan, a page lookup, and a walk down the match
	// tree. Committed lazily, and dropped whenever the page's bytes change —
	// the KTF loader relocates itself and patches SVC stubs over real code
	// pages, so stale entries are a real hazard rather than a theoretical one.
	//
	// It is an array pointer rather than a slice so that the interpreter's
	// index — a page offset, masked and halved — is provably inside it. A
	// slice costs a length load and a bounds check on every instruction for a
	// bound the type already carries.
	decoded *[decodedPerPage]decodedThumb
	// decodedARM is the same cache for ARM state, one entry per word.
	//
	// It holds the form alone where the Thumb entry also holds the encoding,
	// and the difference is not an oversight. A Thumb entry that carries its
	// halfword saves assembling one from two bytes; an ARM instruction is a
	// word the page already holds in the order the host wants it, so caching a
	// copy would buy nothing and cost the entry four more bytes. At one byte
	// per word this table is a quarter of the page it describes, where the
	// Thumb one is twice its page — which matters because an ARM module is a
	// megabyte of code where a Clet is a kilobyte of hot loop.
	decodedARM *[armDecodedPerPage]armForm
}

// decodedThumb is a classified instruction. A zero value means "not decoded
// yet", which works because thumbUndefined would have to be executed to report
// its error anyway.
//
// It stays four bytes deliberately. Widening it to carry each form's registers
// and immediate already pulled out — the change docs/armcore.md projected 1.4x
// for — was built and measured, and it loses twice: the width alone costs 7 to
// 9% and reading the extracted fields costs another 9 to 10% on top of that.
// See "A wider decode cache entry was built and lost, twice over".
type decodedThumb struct {
	form thumbForm
	// refusedLoop marks a backward branch that analysis has already refused to
	// stand in for. It lives in the padding byte the form leaves behind, so
	// the entry is still four bytes and the engine reads the answer out of a
	// word it had already loaded to decode the branch. See fill_loop.go.
	refusedLoop bool
	instruction uint16
}

// discardDecoded drops the page's decode cache. Every path that changes what
// a page's bytes are has to call it.
func (page *memoryPage) discardDecoded() {
	if page.decoded != nil {
		page.decoded = nil
	}
	if page.decodedARM != nil {
		page.decodedARM = nil
	}
}

type memoryMapping struct {
	start      uint64
	end        uint64
	permission Permission
}

type threadLocalState struct {
	mu    sync.RWMutex
	words map[uint32]uint32
}

func newThreadLocalState() *threadLocalState {
	return &threadLocalState{words: make(map[uint32]uint32)}
}

// Memory is a sparse, little-endian 32-bit guest address space. Mapping a
// range records permissions but does not commit backing storage until data is
// loaded or written.
//
// The guest-side accessors — instruction fetch, the loads and stores the
// decoders issue — do not lock. Taking mu per instruction cost more than the
// whole rest of a step: an uncontended RLock/RUnlock pair measured 13.8ns on
// an M1 against 0.8ns for the read it guarded, which is 82% of a step spent
// on a lock that no second goroutine was ever contending. So the lock moved
// out to the quantum: Engine.Run holds it across the instructions it runs
// (beginQuantum/endQuantum), and every guest accessor documents that its
// caller holds it. Nothing lost the protection — Core already serializes
// engine runs behind its execute mutex, and supervisor-call handlers run
// after the quantum returns, outside the lock, exactly as before.
//
// The lock is exclusive rather than shared because a guest store commits
// pages, and an RWMutex read lock cannot be upgraded to do that.
type memoryTable struct {
	pages [memoryTableCount]*memoryPage
}

type Memory struct {
	mu                  sync.RWMutex
	directory           [memoryDirectoryCount]*memoryTable
	maps                []memoryMapping
	threadLocalDefaults map[uint32]uint32
	// threadLocalLow and threadLocalHigh bound the registered word addresses,
	// so an access outside them skips the map entirely.
	threadLocalLow    uint32
	threadLocalHigh   uint32
	activeThreadLocal *threadLocalState
	// lastMapping is the mapping that satisfied the previous validation.
	// Guest code stays inside one region for long runs — a stack, a heap —
	// so re-checking it first turns the scan into two compares. Any change to
	// the mapping list clears it.
	lastMapping memoryMapping
	// dataPage holds the pages recent guest loads and stores landed in, and is
	// cleared whenever page storage changes. codePage is the same for
	// instruction fetch, kept separately so that a loop reading data does not
	// evict the page it is executing from.
	//
	// It is several pages rather than one because a real title's accesses
	// alternate between regions rather than walking one: counted over a scene
	// of one local title, a single remembered page answered 48% of the guest's
	// accesses, and **44% of the misses were the page remembered before it** —
	// a sprite read against a framebuffer written, a heap buffer against a
	// module's constants. Each of those cost a call into the general path for
	// an answer that had been in hand one access earlier.
	//
	// **The ways are indexed rather than searched**, which is what makes this
	// the shape that works where two earlier attempts at a second slot did
	// not: a hit is the same three compares it always was, `mappedPage` still
	// inlines at every call site, and a miss pays no extra compare. See
	// `docs/armcore.md`, "The second data slot was built, both ways, and
	// lost", for the two that were, and what replacing the search with an
	// index changed.
	dataIndex [dataPageWays]uint32
	dataPage  [dataPageWays]*memoryPage
	codeIndex uint32
	codePage  *memoryPage
	// armSteps counts the instructions retired in ARM state. Only the ARM
	// branch increments it, so the Thumb path — which is every instruction on
	// most titles — pays nothing, and Thumb steps are the core's total minus
	// this. It exists because "which titles are ARM" is the first question any
	// ARM throughput change has to answer, and answering it with a throwaway
	// patch each time is how the split in armcore.md came to be a number
	// nobody could re-take. Written under the quantum lock.
	armSteps uint64
	// standInsRefused turns every recogniser off for the life of this memory.
	// It is the seam the recogniser tests interpret their reference run
	// through, so the two sides of an A/B differ in nothing but whether a
	// stand-in was allowed.
	standInsRefused bool
	// Each recogniser builds one description of the loop it is looking at.
	// These are those descriptions, reused rather than allocated per attempt.
	// Analysis runs on every backward branch a title takes that has not
	// already been refused, and on the loops it does recognise it runs again
	// on every execution — so an allocation per attempt is an allocation per
	// fill, which on one local title was 350 MB of garbage in a fifteen-second
	// scene, collected on the machine that is also running the guest. Reuse is
	// safe because a description never outlives the call that filled it: the
	// stand-in reads it and returns, analysis runs under the execution lock,
	// and no two of the four are ever live at once.
	storeLoopScratch    storeLoop
	tableBlitScratch    tableBlit
	byteBlendScratch    byteBlend
	wordModulateScratch wordModulate
	// Write watching. watchCount, watchLow, and watchHigh are the span test an
	// ordinary store pays for; the map and the hits are only reached once a
	// store falls inside it. executingPC is where the engine currently is,
	// which is what makes a hit name an instruction. See watch.go.
	watchCount      int
	watchLow        uint32
	watchHigh       uint32
	watches         map[uint32]struct{}
	watchHits       map[watchKey]*WatchHit
	watchOverflowed bool
	// watchStores counts every recorded store across every watched address, so
	// a hit can say where in that order its first and last write fell.
	watchStores uint64
	executingPC uint32
	// fastSupervisor answers the supervisor calls that can be served inside
	// the quantum. It lives here rather than on Engine because Engine has to
	// stay a zero-size struct: giving it one field made the interpreter's own
	// loop 3% to 13% slower on the local titles, which is more than the
	// boundary it was there to remove. See FastSupervisorCall, and armcore.md.
	fastSupervisor FastSupervisorCall
}

func NewMemory() *Memory {
	return &Memory{threadLocalDefaults: make(map[uint32]uint32), watchLow: ^uint32(0)}
}

// pageFor answers the page holding address, or nil when none is committed.
func (memory *Memory) pageFor(address uint32) *memoryPage {
	table := memory.directory[address>>memoryDirectoryShift]
	if table == nil {
		return nil
	}
	return table.pages[(address>>memoryTableShift)&(memoryTableCount-1)]
}

// commitPage answers the page holding address with storage behind it,
// creating both if they are not there yet.
func (memory *Memory) commitPage(address uint32) *memoryPage {
	directoryIndex := address >> memoryDirectoryShift
	table := memory.directory[directoryIndex]
	if table == nil {
		table = &memoryTable{}
		memory.directory[directoryIndex] = table
	}
	tableIndex := (address >> memoryTableShift) & (memoryTableCount - 1)
	page := table.pages[tableIndex]
	if page == nil {
		page = &memoryPage{}
		table.pages[tableIndex] = page
	}
	if page.data == nil {
		page.data = make([]byte, memoryPageSize)
	}
	return page
}

// eachPage visits every committed page with its page index.
func (memory *Memory) eachPage(visit func(index uint32, page *memoryPage)) {
	for directoryIndex, table := range memory.directory {
		if table == nil {
			continue
		}
		for tableIndex, page := range table.pages {
			if page == nil {
				continue
			}
			visit(uint32(directoryIndex)<<(memoryDirectoryShift-memoryPageShift)|uint32(tableIndex), page)
		}
	}
}

func (memory *Memory) Map(address uint32, size uint64, permission Permission) error {
	if size == 0 {
		return &AccessError{Operation: "map", Address: address, Cause: ErrInvalidMemoryRange}
	}
	if permission == 0 || permission&^PermissionReadWriteExecute != 0 {
		return &AccessError{Operation: "map", Address: address, Size: size, Cause: ErrPermission}
	}
	end := uint64(address) + size
	if end > guestAddressEnd {
		return &AccessError{Operation: "map", Address: address, Size: size, Cause: ErrAddressOverflow}
	}

	memory.mu.Lock()
	defer memory.mu.Unlock()
	start := uint64(address)
	// Mapping is additive, so a range mapped again only ever grants more, and
	// that is a remap rather than a mistake. Two mappings that overlap while
	// each grants something the other withholds are a different thing: both
	// are in force over the overlap, an access there gets the union, and the
	// platform that laid them out that way does not find out from a fault. It
	// finds out much later, from whichever structure the other region's owner
	// wrote over — a read-write arena handed out on top of a read-execute stub
	// region is written over by the game itself, and the stubs only fail once
	// one of them is next called. Refusing the pair makes it a startup error
	// at the call that made it.
	for _, mapping := range memory.maps {
		if start >= mapping.end || mapping.start >= end {
			continue
		}
		if mapping.permission&^permission != 0 && permission&^mapping.permission != 0 {
			return &AccessError{Operation: "map", Address: address, Size: size, Cause: ErrOverlappingMapping}
		}
	}
	for index := 0; index < len(memory.maps); {
		mapping := memory.maps[index]
		if mapping.permission == permission && start <= mapping.end && mapping.start <= end {
			start = min(start, mapping.start)
			end = max(end, mapping.end)
			memory.maps[index] = memory.maps[len(memory.maps)-1]
			memory.maps = memory.maps[:len(memory.maps)-1]
			continue
		}
		index++
	}
	if len(memory.maps) >= maxMappings {
		return &AccessError{Operation: "map", Address: address, Size: size, Cause: ErrMappingLimit}
	}
	memory.maps = append(memory.maps, memoryMapping{start: start, end: end, permission: permission})
	// The decode cache commits a page only after checking execute permission,
	// so a remap has to retire what was cached under the old one, and the
	// mapping the last validation trusted may no longer be the one in force.
	memory.eachPage(func(_ uint32, page *memoryPage) {
		page.discardDecoded()
		// What every page was told the mappings permit is now a stale answer.
		page.permission = 0
	})
	memory.lastMapping = memoryMapping{}
	memory.dataPage = [dataPageWays]*memoryPage{}
	memory.codePage = nil
	return nil
}

// Load initializes mapped memory without applying guest write permission. It
// is intended for platform loaders placing code and read-only data.
func (memory *Memory) Load(address uint32, data []byte) error {
	return memory.write(address, data, 0, "load", true)
}

func (memory *Memory) Read(address uint32, destination []byte) error {
	memory.mu.RLock()
	defer memory.mu.RUnlock()
	return memory.readLocked(address, destination, PermissionRead, "read")
}

// Write is how this platform stores into the guest's address space. It is
// watched: see WriteOrigin in watch.go for why a store the guest did not make
// is still an answer to "what writes this".
func (memory *Memory) Write(address uint32, data []byte) error {
	return memory.write(address, data, PermissionWrite, "write", true)
}

// WriteUntracked is Write for a caller that is *asking* the watch questions
// rather than being investigated by them. A cheat session rewrites its frozen
// values every tick and pokes addresses on command; recording those as writers
// would bury the one the user is hunting under its own tooling, and would say a
// value is being written by something in the game when nothing in the game is
// writing it. Nothing else has any business using this.
func (memory *Memory) WriteUntracked(address uint32, data []byte) error {
	return memory.write(address, data, PermissionWrite, "write", false)
}

func (memory *Memory) write(address uint32, data []byte, required Permission, operation string, track bool) error {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	if err := memory.validateLocked(address, uint64(len(data)), required, operation); err != nil {
		return err
	}
	for offset := 0; offset < len(data); {
		current := address + uint32(offset)
		page := memory.commitPage(current)
		pageOffset := int(current & memoryPageMask)
		length := min(len(data)-offset, int(memoryPageSize)-pageOffset)
		copy(page.data[pageOffset:pageOffset+length], data[offset:offset+length])
		page.discardDecoded()
		offset += length
	}
	if track {
		memory.noteHostWrite(address, data)
	}
	return nil
}

func (memory *Memory) readLocked(address uint32, destination []byte, required Permission, operation string) error {
	if err := memory.validateLocked(address, uint64(len(destination)), required, operation); err != nil {
		return err
	}
	for offset := 0; offset < len(destination); {
		current := address + uint32(offset)
		page := memory.pageAt(current)
		pageOffset := int(current & memoryPageMask)
		length := min(len(destination)-offset, int(memoryPageSize)-pageOffset)
		if page == nil || page.data == nil {
			clear(destination[offset : offset+length])
		} else {
			copy(destination[offset:offset+length], page.data[pageOffset:pageOffset+length])
		}
		offset += length
	}
	return nil
}

func (memory *Memory) validateLocked(address uint32, size uint64, required Permission, operation string) error {
	if size == 0 {
		return nil
	}
	end := uint64(address) + size
	if end > guestAddressEnd {
		return &AccessError{Operation: operation, Address: address, Size: size, Cause: ErrAddressOverflow}
	}
	if cached := memory.lastMapping; cached.start <= uint64(address) && end <= cached.end &&
		(required == 0 || cached.permission&required == required) {
		return nil
	}
	for current := uint64(address); current < end; {
		coveredUntil := current
		mappedUntil := current
		for _, mapping := range memory.maps {
			if mapping.start > current || mapping.end <= current {
				continue
			}
			mappedUntil = max(mappedUntil, mapping.end)
			if required == 0 || mapping.permission&required == required {
				coveredUntil = max(coveredUntil, mapping.end)
			}
		}
		if coveredUntil == current {
			cause := ErrUnmapped
			if mappedUntil != current {
				cause = ErrPermission
			}
			return &AccessError{Operation: operation, Address: address, Size: size, Cause: cause}
		}
		current = coveredUntil
	}
	for _, mapping := range memory.maps {
		if mapping.start <= uint64(address) && end <= mapping.end &&
			(required == 0 || mapping.permission&required == required) {
			memory.lastMapping = mapping
			break
		}
	}
	return nil
}

// CommittedRegion is a contiguous span of guest memory with page storage
// actually committed behind it.
type CommittedRegion struct {
	Base uint32
	Size uint64
}

// CommittedRegions returns the coalesced spans of committed pages that carry
// the required permission. Memory scanners use this instead of the mapped
// ranges: mappings reserve far more than the guest ever touches, and
// uncommitted pages read as zero anyway. Pages partially covered by a
// mapping — a mapping's unaligned tail commits a whole page — contribute
// only their covered span.
func (memory *Memory) CommittedRegions(required Permission) []CommittedRegion {
	memory.mu.RLock()
	defer memory.mu.RUnlock()
	indices := make([]uint32, 0, 64)
	memory.eachPage(func(index uint32, page *memoryPage) {
		if page.data == nil {
			return
		}
		indices = append(indices, index)
	})
	slices.Sort(indices)
	var regions []CommittedRegion
	appendSpan := func(start, end uint64) {
		if len(regions) > 0 {
			last := &regions[len(regions)-1]
			if uint64(last.Base)+last.Size >= start {
				last.Size = max(last.Size, end-uint64(last.Base))
				return
			}
		}
		regions = append(regions, CommittedRegion{Base: uint32(start), Size: end - start})
	}
	var spans [][2]uint64
	for _, index := range indices {
		pageStart := uint64(index) << memoryPageShift
		pageEnd := pageStart + memoryPageSize
		spans = spans[:0]
		for _, mapping := range memory.maps {
			if required != 0 && mapping.permission&required != required {
				continue
			}
			start := max(mapping.start, pageStart)
			end := min(mapping.end, pageEnd)
			if start < end {
				spans = append(spans, [2]uint64{start, end})
			}
		}
		slices.SortFunc(spans, func(a, b [2]uint64) int {
			if a[0] < b[0] {
				return -1
			}
			if a[0] > b[0] {
				return 1
			}
			return 0
		})
		for _, span := range spans {
			appendSpan(span[0], span[1])
		}
	}
	return regions
}

// beginQuantum takes the memory lock for a run of guest instructions. See the
// Memory doc comment for why the guest accessors rely on it instead of locking
// themselves.
func (memory *Memory) beginQuantum() { memory.mu.Lock() }

func (memory *Memory) endQuantum() { memory.mu.Unlock() }

// decodeCacheEnabled is a diagnostic switch, not a tuning knob. Turning the
// cache off is how the "is the browser thrashing its working set" question was
// answered — the answer was no, and turning it off is slower everywhere — so
// the switch stays to keep that measurement repeatable rather than
// remembered. It is process-wide because it is set once at startup, before any
// core exists, by a Host that is answering that question.
var decodeCacheEnabled atomic.Bool

func init() { decodeCacheEnabled.Store(true) }

// SetDecodeCacheEnabled turns the per-page decode caches — both states' — on
// or off.
func SetDecodeCacheEnabled(enabled bool) { decodeCacheEnabled.Store(enabled) }

// DecodeCacheEnabled reports whether decoded instructions are being cached.
func DecodeCacheEnabled() bool { return decodeCacheEnabled.Load() }

// ARMSteps reports how many instructions this memory's engine has retired in
// ARM state. The core's total step count minus this is the Thumb half. See the
// field for why only one of the two is counted.
func (memory *Memory) ARMSteps() uint64 {
	memory.mu.RLock()
	defer memory.mu.RUnlock()
	return memory.armSteps
}

// DecodeCacheStats reports how many decode tables are committed and what they
// cost in bytes. It answers the question a wider cache entry asks first: a
// table is one array per code page, so its cost is not the working set but
// every page the guest has ever executed from.
//
// A page executed in both states holds two tables and is counted twice, which
// is the honest answer to "how much is this costing" and the reason the count
// is of tables rather than of pages.
func (memory *Memory) DecodeCacheStats() (tables int, bytes int) {
	memory.mu.RLock()
	defer memory.mu.RUnlock()
	memory.eachPage(func(_ uint32, page *memoryPage) {
		if page.decoded != nil {
			tables++
			bytes += int(unsafe.Sizeof([decodedPerPage]decodedThumb{}))
		}
		if page.decodedARM != nil {
			tables++
			bytes += int(unsafe.Sizeof([armDecodedPerPage]armForm{}))
		}
	})
	return tables, bytes
}

// decodeThumb answers the classified instruction at address, decoding and
// caching it on first use. The caller holds the memory lock.
//
// Only pages with storage behind them are cached: an uncommitted page reads as
// zero, and committing one later has to be free to change that answer.
func (memory *Memory) decodeThumb(address uint32) (decodedThumb, error) {
	if address&1 != 0 {
		return decodedThumb{}, &AccessError{Operation: "execute", Address: address, Size: 2, Cause: ErrUnaligned}
	}
	if !decodeCacheEnabled.Load() {
		// The uncached path is the same one an uncommitted page already
		// takes: fetch16 checks execute permission, so nothing is skipped
		// along with the cache.
		instruction, err := memory.fetch16(address)
		if err != nil {
			return decodedThumb{}, err
		}
		return decodedThumb{form: classifyThumb(uint32(instruction)), instruction: instruction}, nil
	}
	index := address >> memoryPageShift
	page := memory.codePage
	if page == nil || memory.codeIndex != index {
		page = memory.pageFor(address)
		if page == nil || page.data == nil {
			instruction, err := memory.fetch16(address)
			if err != nil {
				return decodedThumb{}, err
			}
			return decodedThumb{form: classifyThumb(uint32(instruction)), instruction: instruction}, nil
		}
		memory.codeIndex, memory.codePage = index, page
	}
	if page.decoded == nil {
		if err := memory.validateLocked(address, 2, PermissionExecute, "execute"); err != nil {
			return decodedThumb{}, err
		}
		// A cached page answers later addresses without re-checking, so it may
		// only be cached when the whole page is executable. A mapping that
		// ends inside this page would otherwise let the tail of it execute as
		// if it were code.
		pageStart := address &^ memoryPageMask
		if memory.validateLocked(pageStart, memoryPageSize, PermissionExecute, "execute") != nil {
			instruction, err := memory.fetch16(address)
			if err != nil {
				return decodedThumb{}, err
			}
			return decodedThumb{form: classifyThumb(uint32(instruction)), instruction: instruction}, nil
		}
		page.decoded = new([decodedPerPage]decodedThumb)
	}
	slot := &page.decoded[(address&memoryPageMask)>>1]
	if slot.form == thumbUndecoded {
		instruction := uint16(page.data[address&memoryPageMask]) | uint16(page.data[(address&memoryPageMask)+1])<<8
		form := classifyThumb(uint32(instruction))
		if form == thumbUndefined {
			// Leave the slot undecoded so the error is reported every time
			// rather than cached as a form.
			return decodedThumb{form: thumbUndefined, instruction: instruction}, nil
		}
		*slot = decodedThumb{form: form, instruction: instruction}
	}
	return *slot, nil
}

// decodeARM answers the classified instruction at address, decoding and
// caching it on first use. The caller holds the memory lock. It is the ARM
// counterpart of decodeThumb and follows the same rules for what may be
// cached; see there.
//
// It returns the encoding alongside the form because every ARM handler needs
// the word, and the page already holds it: the cache stores the classification
// only.
func (memory *Memory) decodeARM(address uint32) (armForm, uint32, error) {
	if address&3 != 0 {
		return armUndecoded, 0, &AccessError{Operation: "execute", Address: address, Size: 4, Cause: ErrUnaligned}
	}
	uncached := func() (armForm, uint32, error) {
		instruction, err := memory.fetch32(address)
		if err != nil {
			return armUndecoded, 0, err
		}
		return classifyARM(instruction), instruction, nil
	}
	if !decodeCacheEnabled.Load() {
		return uncached()
	}
	index := address >> memoryPageShift
	page := memory.codePage
	if page == nil || memory.codeIndex != index {
		page = memory.pageFor(address)
		if page == nil || page.data == nil {
			return uncached()
		}
		memory.codeIndex, memory.codePage = index, page
	}
	if page.decodedARM == nil {
		if err := memory.validateLocked(address, 4, PermissionExecute, "execute"); err != nil {
			return armUndecoded, 0, err
		}
		// A cached page answers later addresses without re-checking, so it may
		// only be cached when the whole page is executable. A mapping that
		// ends inside this page would otherwise let the tail of it execute as
		// if it were code.
		pageStart := address &^ memoryPageMask
		if memory.validateLocked(pageStart, memoryPageSize, PermissionExecute, "execute") != nil {
			return uncached()
		}
		page.decodedARM = new([armDecodedPerPage]armForm)
	}
	offset := address & memoryPageMask
	instruction := binary.LittleEndian.Uint32(page.data[offset:])
	slot := &page.decodedARM[offset>>2]
	if *slot == armUndecoded {
		form := classifyARM(instruction)
		if form == armUndefined || form == armUnconditionalUndefined {
			// Leave the slot undecoded so the fault is reported every time
			// rather than cached as a form.
			return form, instruction, nil
		}
		*slot = form
	}
	return *slot, instruction, nil
}

// decodedARMFast answers an already-cached ARM decode without a call into the
// general path, and reports whether it could. It exists for the same reason
// decodedThumbFast does: to be inlined into the interpreter loop. Every
// condition it checks is one decodeARM checks too, so a false answer only
// means "ask decodeARM".
func (memory *Memory) decodedARMFast(address uint32) (armForm, uint32, bool) {
	page := memory.codePage
	if page == nil || memory.codeIndex != address>>memoryPageShift || page.decodedARM == nil || address&3 != 0 {
		return armUndecoded, 0, false
	}
	offset := address & memoryPageMask
	form := page.decodedARM[offset>>2]
	if form == armUndecoded {
		return armUndecoded, 0, false
	}
	return form, binary.LittleEndian.Uint32(page.data[offset:]), true
}

// decodedThumbFast answers an already-cached decode without a call into the
// general path, and reports whether it could. It exists to be inlined into the
// interpreter loop, where removing one Go call per guest instruction is worth
// most of a step: the win was found by profiling the browser build, which paid
// a resume-PC store and a reload out of linear memory on top, but it nearly
// halved the native cost too. Every condition it checks is one the general path
// checks too, so a false answer only means "ask decodeThumb".
func (memory *Memory) decodedThumbFast(address uint32) (decodedThumb, bool) {
	page := memory.codePage
	if page == nil || memory.codeIndex != address>>memoryPageShift || page.decoded == nil || address&1 != 0 {
		return decodedThumb{}, false
	}
	slot := page.decoded[(address&memoryPageMask)>>1]
	return slot, slot.form != thumbUndecoded
}

// markLoopRefused remembers that the backward branch at address closes a loop
// none of the recognisers can stand in for. The mark rides in the branch's own
// decode-cache entry, so the engine reads it for free and it is dropped the
// moment the page's instructions change — a rewritten loop is analysed again
// rather than inheriting the old code's answer.
//
// A slot that is not cached — the diagnostic switch is off, or the page has no
// decode table yet — simply does not remember, and the loop is analysed again
// next time round. That costs an analysis, never a wrong stand-in.
func (memory *Memory) markLoopRefused(address uint32) {
	page := memory.codePage
	if page == nil || memory.codeIndex != address>>memoryPageShift || page.decoded == nil || address&1 != 0 {
		return
	}
	page.decoded[(address&memoryPageMask)>>1].refusedLoop = true
}

// fetch16 reads one Thumb instruction. The caller holds the memory lock.
func (memory *Memory) fetch16(address uint32) (uint16, error) {
	if address&1 != 0 {
		return 0, &AccessError{Operation: "execute", Address: address, Size: 2, Cause: ErrUnaligned}
	}
	var data [2]byte
	err := memory.readLocked(address, data[:], PermissionExecute, "execute")
	return uint16(data[0]) | uint16(data[1])<<8, err
}

// fetch32 reads one ARM instruction. The caller holds the memory lock.
func (memory *Memory) fetch32(address uint32) (uint32, error) {
	if address&3 != 0 {
		return 0, &AccessError{Operation: "execute", Address: address, Size: 4, Cause: ErrUnaligned}
	}
	var data [4]byte
	err := memory.readLocked(address, data[:], PermissionExecute, "execute")
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24, err
}

// The sized guest accessors below take a direct route when nothing about the
// access needs the general one: the bytes sit inside a single page — which an
// aligned access always does, page size being a multiple of four — and no
// thread-local word overlaps them. That route reads or writes the page's bytes
// where they are, instead of copying them through a temporary array a byte at
// a time. A four-byte load was an array, a copy, four byte loads, three
// shifts, and three ors; now it is one.
//
// directAccess answers the page to use, and whether the direct route applies
// at all. A nil page with direct set means the page has no storage yet: reads
// answer zero, writes commit it.
func (memory *Memory) directAccess(address uint32, length int, required Permission, operation string) (*memoryPage, bool, error) {
	if memory.activeThreadLocal != nil && memory.threadLocalCandidate(address, length) {
		return nil, false, nil
	}
	// A committed page that already knows what it permits answers without the
	// mapping scan. That is the miss this path is mostly made of: the page
	// changed and the mappings did not, and rediscovering the permission from
	// the mapping list is the most expensive thing a miss used to do.
	if uint64(length) <= memoryPageSize-uint64(address&memoryPageMask) {
		if page := memory.pageAt(address); page != nil && page.permission&required == required {
			if page.data == nil {
				return nil, true, nil
			}
			return page, true, nil
		}
	}
	if err := memory.validateLocked(address, uint64(length), required, operation); err != nil {
		return nil, false, err
	}
	page := memory.pageAt(address)
	if page != nil && page.data == nil {
		page = nil
	}
	return page, true, nil
}

// mappedPage is the inlined form of directAccess for an aligned access that
// the remembered mapping already permits and the remembered page already
// holds, which after the first touch of a page is every access a guest makes.
// It exists for the same reason decodedThumbFast does: reaching the general
// path costs a Go call, and the general path then costs two more — one to
// validate and one to find the page — for an answer that is three compares.
//
// Every condition here is one directAccess checks too, so a nil answer only
// ever means "ask directAccess". A page without storage answers nil rather
// than nil-with-direct, because committing one is the general path's business.
func (memory *Memory) mappedPage(address, length uint32, required Permission) *memoryPage {
	// threadLocalCandidate's span test, written out: calling it costs more
	// inline budget than this function has left, and its extra check — an empty
	// registration map — only ever makes the answer more conservative.
	if memory.activeThreadLocal != nil &&
		address <= memory.threadLocalHigh+3 && address+length > memory.threadLocalLow {
		return nil
	}
	index := address >> memoryPageShift
	way := dataPageWay(index)
	page := memory.dataPage[way]
	// The page's own permission stands in for the mapping test: it is what the
	// mappings allow across the whole page, and an aligned access of at most a
	// word cannot leave the page it starts in.
	if page == nil || memory.dataIndex[way] != index || page.data == nil ||
		page.permission&required != required {
		return nil
	}
	return page
}

// dataPageWay is which remembered slot a page number lands in.
//
// Folding the next bits up into it with an exclusive or was tried, to spread
// the pairs a power of two apart that the way count is chosen around. It fixes
// the same benchmark the count does and costs more than it saves everywhere
// else: 1 to 2% on three real titles, and 1.3% on the game-shaped loop that is
// the shape most of a run is made of.
func dataPageWay(index uint32) uint32 {
	return index & dataPageWayMask
}

// rememberPermission fills in a page's copy of what the mappings permit across
// the whole of it. A page only partly covered keeps zero, which sends every
// access to it down the general path, where the mapping list answers per byte.
func (memory *Memory) rememberPermission(page *memoryPage, address uint32) {
	if page == nil || page.permission != 0 {
		return
	}
	start := uint64(address) &^ uint64(memoryPageMask)
	end := start + memoryPageSize
	for _, mapping := range memory.maps {
		if mapping.start <= start && end <= mapping.end {
			page.permission |= mapping.permission
		}
	}
}

func (memory *Memory) read8(address uint32) (uint8, error) {
	// A byte load takes the same inlined fast path the wider ones do. It was
	// the one width without it, and it is the width a title's own rasteriser
	// reads its source bytes through.
	if page := memory.mappedPage(address, 1, PermissionRead); page != nil {
		return page.data[address&memoryPageMask], nil
	}
	page, direct, err := memory.directAccess(address, 1, PermissionRead, "read")
	if err != nil {
		return 0, err
	}
	if direct {
		if page == nil {
			return 0, nil
		}
		return page.data[address&memoryPageMask], nil
	}
	var data [1]byte
	err = memory.readGuest(address, data[:])
	return data[0], err
}

func (memory *Memory) read16(address uint32) (uint16, error) {
	if address&1 != 0 {
		return 0, &AccessError{Operation: "read", Address: address, Size: 2, Cause: ErrUnaligned}
	}
	if page := memory.mappedPage(address, 2, PermissionRead); page != nil {
		return binary.LittleEndian.Uint16(page.data[address&memoryPageMask:]), nil
	}
	page, direct, err := memory.directAccess(address, 2, PermissionRead, "read")
	if err != nil {
		return 0, err
	}
	if direct {
		if page == nil {
			return 0, nil
		}
		return binary.LittleEndian.Uint16(page.data[address&memoryPageMask:]), nil
	}
	var data [2]byte
	err = memory.readGuest(address, data[:])
	return uint16(data[0]) | uint16(data[1])<<8, err
}

func (memory *Memory) read32(address uint32) (uint32, error) {
	if address&3 != 0 {
		return 0, &AccessError{Operation: "read", Address: address, Size: 4, Cause: ErrUnaligned}
	}
	if page := memory.mappedPage(address, 4, PermissionRead); page != nil {
		return binary.LittleEndian.Uint32(page.data[address&memoryPageMask:]), nil
	}
	page, direct, err := memory.directAccess(address, 4, PermissionRead, "read")
	if err != nil {
		return 0, err
	}
	if direct {
		if page == nil {
			return 0, nil
		}
		return binary.LittleEndian.Uint32(page.data[address&memoryPageMask:]), nil
	}
	var data [4]byte
	err = memory.readGuest(address, data[:])
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24, err
}

func (memory *Memory) write8(address uint32, value uint8) error {
	if page := memory.mappedPage(address, 1, PermissionWrite); page != nil {
		page.data[address&memoryPageMask] = value
		page.discardDecoded()
		memory.noteStore(address, uint32(value), 1)
		return nil
	}
	page, direct, err := memory.directAccess(address, 1, PermissionWrite, "write")
	if err != nil {
		return err
	}
	if direct {
		if page == nil {
			page = memory.commitPageAt(address)
		}
		page.data[address&memoryPageMask] = value
		page.discardDecoded()
		memory.noteStore(address, uint32(value), 1)
		return nil
	}
	if err := memory.writeGuest(address, []byte{value}); err != nil {
		return err
	}
	memory.noteStore(address, uint32(value), 1)
	return nil
}

func (memory *Memory) write16(address uint32, value uint16) error {
	if address&1 != 0 {
		return &AccessError{Operation: "write", Address: address, Size: 2, Cause: ErrUnaligned}
	}
	if page := memory.mappedPage(address, 2, PermissionWrite); page != nil {
		binary.LittleEndian.PutUint16(page.data[address&memoryPageMask:], value)
		page.discardDecoded()
		memory.noteStore(address, uint32(value), 2)
		return nil
	}
	page, direct, err := memory.directAccess(address, 2, PermissionWrite, "write")
	if err != nil {
		return err
	}
	if direct {
		if page == nil {
			page = memory.commitPageAt(address)
		}
		binary.LittleEndian.PutUint16(page.data[address&memoryPageMask:], value)
		page.discardDecoded()
		memory.noteStore(address, uint32(value), 2)
		return nil
	}
	if err := memory.writeGuest(address, []byte{byte(value), byte(value >> 8)}); err != nil {
		return err
	}
	memory.noteStore(address, uint32(value), 2)
	return nil
}

func (memory *Memory) write32(address uint32, value uint32) error {
	if address&3 != 0 {
		return &AccessError{Operation: "write", Address: address, Size: 4, Cause: ErrUnaligned}
	}
	if page := memory.mappedPage(address, 4, PermissionWrite); page != nil {
		binary.LittleEndian.PutUint32(page.data[address&memoryPageMask:], value)
		page.discardDecoded()
		memory.noteStore(address, value, 4)
		return nil
	}
	page, direct, err := memory.directAccess(address, 4, PermissionWrite, "write")
	if err != nil {
		return err
	}
	if direct {
		if page == nil {
			page = memory.commitPageAt(address)
		}
		binary.LittleEndian.PutUint32(page.data[address&memoryPageMask:], value)
		page.discardDecoded()
		memory.noteStore(address, value, 4)
		return nil
	}
	if err := memory.writeGuest(address, []byte{byte(value), byte(value >> 8), byte(value >> 16), byte(value >> 24)}); err != nil {
		return err
	}
	memory.noteStore(address, value, 4)
	return nil
}

// registerThreadLocalWord marks one aligned guest word as logical-thread
// state. Host Read and Write continue to access the shared backing word, while
// guest instructions see a private value selected by the running Thread.
func (memory *Memory) registerThreadLocalWord(address uint32) error {
	if address&3 != 0 {
		return &AccessError{Operation: "register thread-local word", Address: address, Size: 4, Cause: ErrUnaligned}
	}
	memory.mu.Lock()
	defer memory.mu.Unlock()
	if err := memory.validateLocked(address, 4, PermissionReadWrite, "register thread-local word"); err != nil {
		return err
	}
	if _, ok := memory.threadLocalDefaults[address]; ok {
		return nil
	}
	var data [4]byte
	if err := memory.readLocked(address, data[:], PermissionRead, "register thread-local word"); err != nil {
		return err
	}
	memory.threadLocalDefaults[address] = uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
	if len(memory.threadLocalDefaults) == 1 || address < memory.threadLocalLow {
		memory.threadLocalLow = address
	}
	if address > memory.threadLocalHigh {
		memory.threadLocalHigh = address
	}
	return nil
}

// threadLocalCandidate answers whether an access could touch a registered
// word. Every guest load and store asks first, so it is a pair of compares
// against the registered span rather than a map lookup: KTF registers a
// handful of words and the answer is almost always no.
func (memory *Memory) threadLocalCandidate(address uint32, length int) bool {
	if len(memory.threadLocalDefaults) == 0 {
		return false
	}
	return address <= memory.threadLocalHigh+3 && address+uint32(length) > memory.threadLocalLow
}

func (memory *Memory) activateThreadLocal(state *threadLocalState) {
	memory.mu.Lock()
	memory.activeThreadLocal = state
	memory.mu.Unlock()
}

func (memory *Memory) threadLocalWord(state *threadLocalState, address uint32) (uint32, error) {
	if state == nil {
		return 0, ErrThreadState
	}
	memory.mu.RLock()
	value, ok := memory.threadLocalDefaults[address]
	memory.mu.RUnlock()
	if !ok {
		return 0, &AccessError{Operation: "read thread-local word", Address: address, Size: 4, Cause: ErrUnmapped}
	}
	state.mu.RLock()
	if local, exists := state.words[address]; exists {
		value = local
	}
	state.mu.RUnlock()
	return value, nil
}

// threadLocalWords collects one thread's value for every registered word.
func (memory *Memory) threadLocalWords(state *threadLocalState) []uint32 {
	memory.mu.RLock()
	values := make([]uint32, 0, len(memory.threadLocalDefaults))
	for address, value := range memory.threadLocalDefaults {
		if state != nil {
			state.mu.RLock()
			if local, exists := state.words[address]; exists {
				value = local
			}
			state.mu.RUnlock()
		}
		values = append(values, value)
	}
	memory.mu.RUnlock()
	return values
}

func (memory *Memory) setThreadLocalWord(state *threadLocalState, address, value uint32) error {
	if state == nil {
		return ErrThreadState
	}
	memory.mu.RLock()
	_, ok := memory.threadLocalDefaults[address]
	memory.mu.RUnlock()
	if !ok {
		return &AccessError{Operation: "write thread-local word", Address: address, Size: 4, Cause: ErrUnmapped}
	}
	state.mu.Lock()
	state.words[address] = value
	state.mu.Unlock()
	return nil
}

// readGuest performs one guest load. The caller holds the memory lock.
func (memory *Memory) readGuest(address uint32, destination []byte) error {
	if err := memory.readLocked(address, destination, PermissionRead, "read"); err != nil {
		return err
	}
	state := memory.activeThreadLocal
	if state == nil || len(destination) == 0 || !memory.threadLocalCandidate(address, len(destination)) {
		return nil
	}
	// Registered words are aligned, so the splice walks words rather than
	// bytes: a four-byte load asks the map once instead of four times.
	state.mu.RLock()
	defer state.mu.RUnlock()
	first, words := coveredWords(address, len(destination))
	for index := 0; index < words; index++ {
		wordAddress := first + uint32(index)*4
		value, ok := memory.threadLocalDefaults[wordAddress]
		if !ok {
			continue
		}
		if local, exists := state.words[wordAddress]; exists {
			value = local
		}
		for offset := range destination {
			current := address + uint32(offset)
			if current&^3 != wordAddress {
				continue
			}
			destination[offset] = byte(value >> ((current - wordAddress) * 8))
		}
	}
	return nil
}

// coveredWords answers the first aligned word address an access of length
// bytes at address falls inside, and how many words it spans. Guest accesses
// are at most one word wide, so the count is normally one.
func coveredWords(address uint32, length int) (uint32, int) {
	first := address &^ 3
	return first, (int(address-first) + length + 3) / 4
}

// writeGuest performs one guest store. The caller holds the memory lock.
func (memory *Memory) writeGuest(address uint32, data []byte) error {
	if err := memory.validateLocked(address, uint64(len(data)), PermissionWrite, "write"); err != nil {
		return err
	}
	// The ordinary store — no thread-local word anywhere near it — writes the
	// whole run page-wise. Going byte by byte cost a page lookup per byte, and
	// a four-byte store is four bytes.
	state := memory.activeThreadLocal
	if state == nil || !memory.threadLocalCandidate(address, len(data)) {
		memory.writeRunLocked(address, data)
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	for offset, value := range data {
		current := address + uint32(offset)
		wordAddress := current &^ 3
		fallback, local := memory.threadLocalDefaults[wordAddress]
		if local {
			word := fallback
			if existing, ok := state.words[wordAddress]; ok {
				word = existing
			}
			shift := (current - wordAddress) * 8
			word = word&^(uint32(0xff)<<shift) | uint32(value)<<shift
			state.words[wordAddress] = word
			continue
		}
		memory.writeByteLocked(current, value)
	}
	return nil
}

// pageAt answers the page holding address, remembering it for the next
// access. The caller holds the memory lock.
func (memory *Memory) pageAt(address uint32) *memoryPage {
	index := address >> memoryPageShift
	way := dataPageWay(index)
	if page := memory.dataPage[way]; page != nil && memory.dataIndex[way] == index {
		if page.permission == 0 {
			memory.rememberPermission(page, address)
		}
		return page
	}
	page := memory.pageFor(address)
	if page != nil {
		memory.rememberPermission(page, address)
		memory.dataIndex[way], memory.dataPage[way] = index, page
	}
	return page
}

// commitPageAt is pageAt for writes: it creates the page and its storage when
// they are not there yet.
func (memory *Memory) commitPageAt(address uint32) *memoryPage {
	index := address >> memoryPageShift
	way := dataPageWay(index)
	if page := memory.dataPage[way]; page != nil && memory.dataIndex[way] == index && page.data != nil {
		if page.permission == 0 {
			memory.rememberPermission(page, address)
		}
		return page
	}
	page := memory.commitPage(address)
	memory.rememberPermission(page, address)
	memory.dataIndex[way], memory.dataPage[way] = index, page
	return page
}

// writeRunLocked copies data into the pages behind it, committing storage as
// it goes. The caller holds the memory lock and has already validated the
// range.
func (memory *Memory) writeRunLocked(address uint32, data []byte) {
	for offset := 0; offset < len(data); {
		current := address + uint32(offset)
		page := memory.commitPageAt(current)
		pageOffset := int(current & memoryPageMask)
		length := min(len(data)-offset, int(memoryPageSize)-pageOffset)
		copy(page.data[pageOffset:pageOffset+length], data[offset:offset+length])
		page.discardDecoded()
		offset += length
	}
}

func (memory *Memory) writeByteLocked(address uint32, value byte) {
	page := memory.commitPage(address)
	page.data[address&memoryPageMask] = value
	page.discardDecoded()
}

// ARMv4T data accesses align halfwords downward. Unaligned word loads read the
// containing aligned word and rotate it right by one byte per address bit,
// while word stores ignore the bottom two address bits. Instruction fetches
// deliberately continue to use the strict aligned helpers above.
func (memory *Memory) readData16(address uint32) (uint16, error) {
	return memory.read16(address &^ 1)
}

func (memory *Memory) readData32(address uint32) (uint32, error) {
	value, err := memory.read32(address &^ 3)
	if err != nil {
		return 0, err
	}
	return bits.RotateLeft32(value, -int(address&3)*8), nil
}

func (memory *Memory) writeData16(address uint32, value uint16) error {
	return memory.write16(address&^1, value)
}

func (memory *Memory) writeData32(address, value uint32) error {
	return memory.write32(address&^3, value)
}

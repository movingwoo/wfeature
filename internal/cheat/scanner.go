package cheat

import (
	"errors"
	"fmt"
)

// Read the guest in chunks of this size rather than materializing a whole
// region at once.
const scanChunkSize = 0x10000

// How many previous candidate sets Undo can walk back through.
const maxScanHistory = 16

// Region is a contiguous span of guest memory worth scanning.
//
// Targets are expected to report only memory that is actually backed by
// storage: guest address spaces reserve far more than they commit, and
// scanning the reservation would make every scan orders of magnitude slower
// than it needs to be.
type Region struct {
	Base  uint32
	Size  uint32
	Label string
}

// MemoryTarget is memory that a cheat session can inspect and modify.
// Implemented by emulator clients that expose a flat guest address space.
// Purely JVM-backed platforms (SKT) have no such space and do not
// implement this.
type MemoryTarget interface {
	ReadMemory(address uint32, destination []byte) error
	WriteMemory(address uint32, data []byte) error
	Regions() []Region
}

// Candidate is one surviving address with the value observed at the end of
// the scan that kept it.
type Candidate struct {
	Address uint32
	Value   int64
}

// FilterOp selects the predicate applied to each candidate.
type FilterOp uint8

const (
	// FilterUnknown keeps everything; it seeds a search when the value is
	// not known.
	FilterUnknown FilterOp = iota
	FilterEq
	FilterNe
	FilterGt
	FilterLt
	FilterBetween
	FilterChanged
	FilterUnchanged
	FilterIncreased
	FilterDecreased
	FilterIncreasedBy
	FilterDecreasedBy
)

// ScanFilter is a predicate applied to each candidate. The comparison-to-
// previous variants need a prior observation, so they are only valid once a
// scan has been started.
type ScanFilter struct {
	Op FilterOp
	// A is the comparison operand; for FilterBetween it is the low bound.
	A int64
	// B is the high bound of FilterBetween.
	B int64
}

// NeedsPrevious reports whether evaluating this filter requires the value
// from the previous scan.
func (filter ScanFilter) NeedsPrevious() bool {
	switch filter.Op {
	case FilterChanged, FilterUnchanged, FilterIncreased, FilterDecreased, FilterIncreasedBy, FilterDecreasedBy:
		return true
	default:
		return false
	}
}

func (filter ScanFilter) accepts(current int64, previous int64, hasPrevious bool) bool {
	switch filter.Op {
	case FilterUnknown:
		return true
	case FilterEq:
		return current == filter.A
	case FilterNe:
		return current != filter.A
	case FilterGt:
		return current > filter.A
	case FilterLt:
		return current < filter.A
	case FilterBetween:
		return current >= filter.A && current <= filter.B
	case FilterChanged:
		return hasPrevious && current != previous
	case FilterUnchanged:
		return hasPrevious && current == previous
	case FilterIncreased:
		return hasPrevious && current > previous
	case FilterDecreased:
		return hasPrevious && current < previous
	case FilterIncreasedBy:
		return hasPrevious && current-previous == filter.A
	case FilterDecreasedBy:
		return hasPrevious && previous-current == filter.A
	default:
		return false
	}
}

// ErrNeedsPreviousValue reports a previous-value filter used before any scan
// had been started.
var ErrNeedsPreviousValue = errors.New("this filter compares against the previous scan")

// Scanner is a progressive value search over a MemoryTarget.
//
// The first scan sweeps every region; subsequent scans only re-read the
// surviving addresses, so a search narrows quickly even when it starts from
// an unknown value.
type Scanner struct {
	valueType  ValueType
	align      int
	candidates []Candidate
	history    [][]Candidate
	started    bool
}

// NewScanner builds a scanner for valueType with natural alignment.
func NewScanner(valueType ValueType) *Scanner {
	return &Scanner{valueType: valueType, align: valueType.Size()}
}

func (scanner *Scanner) ValueType() ValueType { return scanner.valueType }
func (scanner *Scanner) Align() int           { return scanner.align }
func (scanner *Scanner) Started() bool        { return scanner.started }
func (scanner *Scanner) Len() int             { return len(scanner.candidates) }

// Candidates exposes the surviving addresses of the current search.
func (scanner *Scanner) Candidates() []Candidate { return scanner.candidates }

// SetAlign sets the scan stride. Guest structures are naturally aligned in
// practice, so the default stride equals the value size; drop it to 1 when a
// value turns out to be packed.
func (scanner *Scanner) SetAlign(align int) {
	scanner.align = max(align, 1)
}

// Reset drops all results and starts over, keeping the value type and
// alignment.
func (scanner *Scanner) Reset() {
	scanner.candidates = nil
	scanner.history = nil
	scanner.started = false
}

// SetValueType changes the value type. Any in-progress search is discarded
// because the candidate addresses were produced at a different width.
func (scanner *Scanner) SetValueType(valueType ValueType) {
	scanner.valueType = valueType
	scanner.align = valueType.Size()
	scanner.Reset()
}

// Undo restores the candidate set from before the last scan.
func (scanner *Scanner) Undo() bool {
	if len(scanner.history) == 0 {
		return false
	}
	scanner.candidates = scanner.history[len(scanner.history)-1]
	scanner.history = scanner.history[:len(scanner.history)-1]
	scanner.started = true
	return true
}

// Scan runs one pass, sweeping all regions if this is the first one and
// otherwise filtering the surviving candidates.
func (scanner *Scanner) Scan(target MemoryTarget, filter ScanFilter) (int, error) {
	if !scanner.started {
		if filter.NeedsPrevious() {
			return 0, ErrNeedsPreviousValue
		}
		candidates := scanner.sweep(target, filter)
		scanner.pushHistory()
		scanner.candidates = candidates
		scanner.started = true
	} else {
		candidates := scanner.refilter(target, filter)
		scanner.pushHistory()
		scanner.candidates = candidates
	}
	return len(scanner.candidates), nil
}

// Refresh re-reads the current value of every candidate without filtering,
// so a listing shows live values.
func (scanner *Scanner) Refresh(target MemoryTarget) {
	buffer := make([]byte, scanner.valueType.Size())
	for index := range scanner.candidates {
		if target.ReadMemory(scanner.candidates[index].Address, buffer) != nil {
			continue
		}
		if value, ok := scanner.valueType.Decode(buffer); ok {
			scanner.candidates[index].Value = value
		}
	}
}

func (scanner *Scanner) pushHistory() {
	if len(scanner.history) == maxScanHistory {
		scanner.history = scanner.history[1:]
	}
	snapshot := make([]Candidate, len(scanner.candidates))
	copy(snapshot, scanner.candidates)
	scanner.history = append(scanner.history, snapshot)
}

// sweep is the first pass: walk every region at the configured stride.
// Chunks overlap by size-1 bytes so a value straddling a chunk boundary is
// still found, and unreadable chunks are skipped rather than aborting the
// scan, because region reporting and readability can differ.
func (scanner *Scanner) sweep(target MemoryTarget, filter ScanFilter) []Candidate {
	size := scanner.valueType.Size()
	align := uint32(scanner.align)
	var result []Candidate
	for _, region := range target.Regions() {
		regionSize := int(region.Size)
		if regionSize < size {
			continue
		}
		offset := 0
		for offset+size <= regionSize {
			chunkLength := min(scanChunkSize, regionSize-offset)
			buffer := make([]byte, chunkLength)
			chunkBase := region.Base + uint32(offset)
			if target.ReadMemory(chunkBase, buffer) != nil {
				offset += chunkLength
				continue
			}
			// Start at the first correctly aligned address in the chunk;
			// neither the region base nor the chunk boundary is guaranteed
			// to be aligned.
			address := chunkBase
			if remainder := address % align; remainder != 0 {
				next := uint64(address) + uint64(align-remainder)
				if next > 0xffffffff {
					break
				}
				address = uint32(next)
			}
			for {
				local := int(address - chunkBase)
				if local+size > chunkLength {
					break
				}
				if value, ok := scanner.valueType.Decode(buffer[local:]); ok && filter.accepts(value, 0, false) {
					result = append(result, Candidate{Address: address, Value: value})
				}
				next := uint64(address) + uint64(align)
				if next > 0xffffffff {
					break
				}
				address = uint32(next)
			}
			if chunkLength == regionSize-offset {
				break
			}
			offset += chunkLength - (size - 1)
		}
	}
	return result
}

// refilter is every later pass: only the surviving addresses are re-read.
func (scanner *Scanner) refilter(target MemoryTarget, filter ScanFilter) []Candidate {
	buffer := make([]byte, scanner.valueType.Size())
	var result []Candidate
	for _, candidate := range scanner.candidates {
		if target.ReadMemory(candidate.Address, buffer) != nil {
			continue
		}
		value, ok := scanner.valueType.Decode(buffer)
		if !ok {
			continue
		}
		if filter.accepts(value, candidate.Value, true) {
			result = append(result, Candidate{Address: candidate.Address, Value: value})
		}
	}
	return result
}

// ParseFilter parses the scan command's filter words: "= N", "!= N", "> N",
// "< N", "N..M", "unknown", "changed", "unchanged", "inc [N]", "dec [N]".
func ParseFilter(args []string, parseNumber func(string) (int64, bool)) (ScanFilter, error) {
	if len(args) == 0 {
		return ScanFilter{}, fmt.Errorf("missing scan filter")
	}
	operand := func(index int) (int64, error) {
		if index >= len(args) {
			return 0, fmt.Errorf("filter %q expects a number", args[0])
		}
		value, ok := parseNumber(args[index])
		if !ok {
			return 0, fmt.Errorf("invalid number %q", args[index])
		}
		return value, nil
	}
	switch args[0] {
	case "unknown", "u":
		return ScanFilter{Op: FilterUnknown}, nil
	case "changed":
		return ScanFilter{Op: FilterChanged}, nil
	case "unchanged":
		return ScanFilter{Op: FilterUnchanged}, nil
	case "inc", "increased":
		if len(args) > 1 {
			value, err := operand(1)
			if err != nil {
				return ScanFilter{}, err
			}
			return ScanFilter{Op: FilterIncreasedBy, A: value}, nil
		}
		return ScanFilter{Op: FilterIncreased}, nil
	case "dec", "decreased":
		if len(args) > 1 {
			value, err := operand(1)
			if err != nil {
				return ScanFilter{}, err
			}
			return ScanFilter{Op: FilterDecreasedBy, A: value}, nil
		}
		return ScanFilter{Op: FilterDecreased}, nil
	case "=", "==", "eq":
		value, err := operand(1)
		if err != nil {
			return ScanFilter{}, err
		}
		return ScanFilter{Op: FilterEq, A: value}, nil
	case "!=", "ne":
		value, err := operand(1)
		if err != nil {
			return ScanFilter{}, err
		}
		return ScanFilter{Op: FilterNe, A: value}, nil
	case ">", "gt":
		value, err := operand(1)
		if err != nil {
			return ScanFilter{}, err
		}
		return ScanFilter{Op: FilterGt, A: value}, nil
	case "<", "lt":
		value, err := operand(1)
		if err != nil {
			return ScanFilter{}, err
		}
		return ScanFilter{Op: FilterLt, A: value}, nil
	}
	// "N..M" range, or a bare number as equality.
	if low, high, found := cutRange(args[0]); found {
		lowValue, lowOK := parseNumber(low)
		highValue, highOK := parseNumber(high)
		if !lowOK || !highOK {
			return ScanFilter{}, fmt.Errorf("invalid range %q", args[0])
		}
		return ScanFilter{Op: FilterBetween, A: lowValue, B: highValue}, nil
	}
	if value, ok := parseNumber(args[0]); ok {
		return ScanFilter{Op: FilterEq, A: value}, nil
	}
	return ScanFilter{}, fmt.Errorf("unknown filter %q", args[0])
}

func cutRange(s string) (string, string, bool) {
	for index := 1; index+2 < len(s); index++ {
		if s[index] == '.' && s[index+1] == '.' {
			return s[:index], s[index+2:], true
		}
	}
	return "", "", false
}

// Package conformance is the corpus every armcore execution backend has to
// answer identically, and the harness that runs it.
//
// The expectations are the architecture's, not a recording of what this
// interpreter happens to do. Every case names the rule it is for — a flag that
// has to survive an instruction that does not write it, a shift amount the
// encoding cannot express as itself, the value R15 reads as, what a base
// register in a transfer list is worth — and its Want is worked out from that
// rule and written down. A case that fails is either a backend that is wrong or
// a rule that was read wrong, and both are worth finding; a corpus that records
// the answer it was given can only find the first.
//
// The comparison is the whole guest-visible state at the point a run returns:
// the sixteen registers and CPSR, the bytes of a scratch window, and the run's
// own result — why it stopped, where the PC is, and how many instructions it
// retired. The last of those is not a diagnostic. It is the unit a Host paces
// frames on and spots a runaway guest with, so a backend that retires an
// instruction without counting it moves the guest's clock, and the corpus is
// where that gets caught.
package conformance

import (
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The address map every case runs in. Code is separate from data so that a
// case which stores through a register cannot rewrite the instruction stream,
// and the scratch window is small so a snapshot of it is readable in a failure.
const (
	CodeBase    uint32 = 0x1000
	CodeSize    uint64 = 0x1000
	ScratchBase uint32 = 0x2000
	ScratchSize        = 32
)

// The condition-flag and state bits of the ARM program status register. They
// are named here rather than taken from armcore because they belong to the
// architecture the corpus is written against.
const (
	FlagN uint32 = 1 << 31
	FlagZ uint32 = 1 << 30
	FlagC uint32 = 1 << 29
	FlagV uint32 = 1 << 28
	// FlagT is the Thumb bit, and ModeUser the mode every case runs in.
	FlagT    uint32 = 1 << 5
	ModeUser uint32 = 0x10
)

// Case is one corpus entry.
type Case struct {
	Name string
	// Rule is the architectural rule the case exists for, in one line. It is
	// what a failure message quotes, because "the answer changed" is not
	// useful without it.
	Rule string
	// Thumb starts execution in Thumb state. The image is still laid out at
	// CodeBase either way, so a case may hold both instruction sets and switch
	// between them.
	Thumb bool
	// Registers is r0 to r14 before the run; r15 comes from CodeBase.
	Registers [15]uint32
	// Flags is the condition flags before the run, which is how a case asks
	// what an instruction does with a carry it did not set itself.
	Flags uint32
	// Image is the bytes at CodeBase, and Scratch the bytes at ScratchBase.
	Image   []byte
	Scratch []byte
	// End is the address a run stops at, and defaults to just past the image.
	// Count is the instruction budget, and defaults to enough for any case
	// here; a case that sets it is asking what a spent budget looks like.
	End   uint32
	Count uint32
	Want  Snapshot
}

// Snapshot is everything a run is compared on.
type Snapshot struct {
	Registers [16]uint32
	CPSR      uint32
	Scratch   []byte
	Reason    armcore.StopReason
	Steps     uint32
	// Supervisor and SupervisorAddress are the immediate a supervisor call
	// carried and where it was raised, and are zero for every other stop.
	Supervisor        uint32
	SupervisorAddress uint32
	// Error is the fault a run ended with, as its message, and empty when the
	// run finished.
	Error string
}

// defaultCount is enough for every case that does not ask for a budget of its
// own. It is small so that a case which fails to reach its end address stops
// rather than running out the clock.
const defaultCount = 4096

// Run executes one case on one backend and answers the state it left.
func Run(backend armcore.Backend, probe Case) (Snapshot, error) {
	memory := armcore.NewMemory()
	if err := memory.Map(CodeBase, CodeSize, armcore.PermissionReadExecute); err != nil {
		return Snapshot{}, fmt.Errorf("map the code region: %w", err)
	}
	if err := memory.Map(ScratchBase, ScratchSize, armcore.PermissionReadWrite); err != nil {
		return Snapshot{}, fmt.Errorf("map the scratch region: %w", err)
	}
	if err := memory.Load(CodeBase, probe.Image); err != nil {
		return Snapshot{}, fmt.Errorf("load the image: %w", err)
	}
	scratch := make([]byte, ScratchSize)
	copy(scratch, probe.Scratch)
	if err := memory.Load(ScratchBase, scratch); err != nil {
		return Snapshot{}, fmt.Errorf("load the scratch window: %w", err)
	}

	context := armcore.NewContext()
	copy(context.Registers[:15], probe.Registers[:])
	context.CPSR |= probe.Flags
	entry := CodeBase
	if probe.Thumb {
		entry |= 1
	}
	if err := context.SetPC(entry); err != nil {
		return Snapshot{}, fmt.Errorf("set the entry point: %w", err)
	}

	end := probe.End
	if end == 0 {
		end = CodeBase + uint32(len(probe.Image))
	}
	count := probe.Count
	if count == 0 {
		count = defaultCount
	}

	result, runErr := backend.Run(&context, memory, end, count)
	snapshot := Snapshot{
		Registers:         context.Registers,
		CPSR:              context.CPSR,
		Scratch:           make([]byte, ScratchSize),
		Reason:            result.Reason,
		Steps:             result.Steps,
		Supervisor:        result.SupervisorCall.Immediate,
		SupervisorAddress: result.SupervisorCall.Address,
	}
	if runErr != nil {
		snapshot.Error = runErr.Error()
	}
	if err := memory.Read(ScratchBase, snapshot.Scratch); err != nil {
		return snapshot, fmt.Errorf("read the scratch window back: %w", err)
	}
	return snapshot, nil
}

// Differences lists what one snapshot has that another does not, in the words
// a failure message wants. It is a list rather than a boolean because the first
// difference is rarely the whole story: a wrong shift shows up as one register
// and one flag, and seeing both is what says which of the two is the cause.
func Differences(want, got Snapshot) []string {
	var differences []string
	for register := range want.Registers {
		if want.Registers[register] != got.Registers[register] {
			differences = append(differences, fmt.Sprintf(
				"r%d = %#08x, want %#08x", register, got.Registers[register], want.Registers[register]))
		}
	}
	if want.CPSR != got.CPSR {
		differences = append(differences, fmt.Sprintf(
			"CPSR = %#08x (%s), want %#08x (%s)",
			got.CPSR, FormatFlags(got.CPSR), want.CPSR, FormatFlags(want.CPSR)))
	}
	if want.Reason != got.Reason {
		differences = append(differences, fmt.Sprintf(
			"stop reason = %d, want %d", got.Reason, want.Reason))
	}
	if want.Steps != got.Steps {
		differences = append(differences, fmt.Sprintf(
			"retired %d instructions, want %d", got.Steps, want.Steps))
	}
	if want.Supervisor != got.Supervisor || want.SupervisorAddress != got.SupervisorAddress {
		differences = append(differences, fmt.Sprintf(
			"supervisor call %#x at %#x, want %#x at %#x",
			got.Supervisor, got.SupervisorAddress, want.Supervisor, want.SupervisorAddress))
	}
	if want.Error != got.Error {
		differences = append(differences, fmt.Sprintf("error %q, want %q", got.Error, want.Error))
	}
	for offset := 0; offset < ScratchSize && offset < len(want.Scratch) && offset < len(got.Scratch); offset++ {
		if want.Scratch[offset] != got.Scratch[offset] {
			differences = append(differences, fmt.Sprintf(
				"scratch[%d] = %#02x, want %#02x", offset, got.Scratch[offset], want.Scratch[offset]))
		}
	}
	return differences
}

// FormatFlags renders the condition flags of a CPSR value.
func FormatFlags(cpsr uint32) string {
	letters := []byte("nzcvt")
	if cpsr&FlagN != 0 {
		letters[0] = 'N'
	}
	if cpsr&FlagZ != 0 {
		letters[1] = 'Z'
	}
	if cpsr&FlagC != 0 {
		letters[2] = 'C'
	}
	if cpsr&FlagV != 0 {
		letters[3] = 'V'
	}
	if cpsr&FlagT != 0 {
		letters[4] = 'T'
	}
	return string(letters)
}

// arm lays ARM words out little-endian, and thumb halfwords, so a case can be
// written as the instructions it is rather than as a byte slice.
func arm(words ...uint32) []byte {
	image := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(image[index*4:], word)
	}
	return image
}

func thumb(halfwords ...uint16) []byte {
	image := make([]byte, len(halfwords)*2)
	for index, halfword := range halfwords {
		binary.LittleEndian.PutUint16(image[index*2:], halfword)
	}
	return image
}

func join(parts ...[]byte) []byte {
	var image []byte
	for _, part := range parts {
		image = append(image, part...)
	}
	return image
}

// words lays a little-endian word image out for a scratch window.
func words(values ...uint32) []byte {
	return arm(values...)
}

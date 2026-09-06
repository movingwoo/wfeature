package cheat

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// A byte patch is how an investigation gets past a gate.
//
// Everything else here observes. A scan says where a value is, a watch says
// what writes it, a trace says what the guest asked the platform for — and
// none of them answers the question a title stopped at a check actually
// raises, which is "what would it do if that check had passed". The only way
// to see behind a gate is to go through it, and until now that meant editing
// a word of guest memory by hand for one run and throwing the edit away. The
// finding survived; the means never did, so the next gate started over.
//
// Two properties are what make a standing mechanism safe enough to leave in
// reach:
//
//   - **A patch declares the bytes it replaces.** An address is only an
//     address under the layout it was found in, and layouts move between
//     builds, between titles and between runs. Writing a replacement without
//     checking what is there would corrupt a running guest quietly, which is
//     the one failure mode worse than not having the tool: the run afterwards
//     looks like a bug in the emulator.
//   - **An entry applies as a unit.** Skipping a check is rarely one word —
//     it is a branch and the constant it compares against — and half of that
//     applied leaves the guest in a state neither the game nor the patch
//     describes. So every span is verified before any is written, and a write
//     that fails part of the way through puts back what it had already
//     replaced.

// maxPatchSpan bounds one span. A patch is a few instructions or a constant;
// anything longer is a different tool's job and is far more likely to be a
// mistyped length than an intention.
const maxPatchSpan = 4096

// HexBytes is a byte span written as hex, which is how a patch reads in a
// table a person edits beside a disassembly.
type HexBytes []byte

// MarshalJSON writes the span as one lower-case hex string.
func (span HexBytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(hex.EncodeToString(span))
}

// hexSeparators are the characters a person groups bytes with by hand.
var hexSeparators = strings.NewReplacer(" ", "", "\t", "", "-", "", ":", "", "_", "")

// UnmarshalJSON reads a hex string, ignoring the spacing a hand-written span
// carries: "de ad be ef" and "deadbeef" are the same four bytes.
func (span *HexBytes) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("a byte span must be a hex string")
	}
	decoded, err := hex.DecodeString(hexSeparators.Replace(text))
	if err != nil {
		return fmt.Errorf("%q is not a hex byte span", text)
	}
	*span = decoded
	return nil
}

// Address is a guest address in a patch. It is written as hex because a patch
// address is read off a disassembly, and it is accepted as either a hex string
// or a plain number so a hand-written table is not fussy about which.
type Address uint32

// MarshalJSON writes the address as "0x0012abcd".
func (address Address) MarshalJSON() ([]byte, error) {
	return json.Marshal(fmt.Sprintf("0x%08x", uint32(address)))
}

// UnmarshalJSON reads a number or a string in any base Go understands.
func (address *Address) UnmarshalJSON(data []byte) error {
	var number uint32
	if json.Unmarshal(data, &number) == nil {
		*address = Address(number)
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("an address must be a number or a hex string")
	}
	value, err := strconv.ParseUint(strings.TrimSpace(text), 0, 32)
	if err != nil {
		return fmt.Errorf("%q is not a guest address", text)
	}
	*address = Address(value)
	return nil
}

// Patch is one span of guest memory to replace.
type Patch struct {
	Address Address `json:"address"`
	// Expect is what has to be there already. Applying refuses when it is not,
	// which is the whole of what keeps a patch written against one layout from
	// landing in the middle of another.
	Expect  HexBytes `json:"expect"`
	Replace HexBytes `json:"replace"`
}

// End is the first address past the span.
func (patch Patch) End() uint64 {
	return uint64(patch.Address) + uint64(len(patch.Replace))
}

// PatchEntry is one named change. Its spans apply together or not at all.
type PatchEntry struct {
	Name string `json:"name"`
	// Note is what the entry is for, in the words of whoever found it. A patch
	// address means nothing on its own six months later.
	Note    string  `json:"note,omitempty"`
	Patches []Patch `json:"patches"`
}

// AppliedPatch is an entry currently held in the guest.
type AppliedPatch struct {
	Entry PatchEntry
	// original is what each span held when the patch replaced it, which is
	// what reverting puts back. It is the memory that was read rather than the
	// Expect the entry declared: the two are equal at the moment of applying,
	// and restoring what was read is what makes a revert restore memory rather
	// than restore an assertion.
	original [][]byte
}

// Patches lists the entries currently applied, in the order they went in.
func (session *Session) Patches() []PatchEntry {
	entries := make([]PatchEntry, 0, len(session.patches))
	for _, applied := range session.patches {
		entries = append(entries, applied.Entry)
	}
	return entries
}

// PatchApplied reports whether an entry of that name is currently held.
func (session *Session) PatchApplied(name string) bool {
	return session.findPatch(name) >= 0
}

func (session *Session) findPatch(name string) int {
	for index, applied := range session.patches {
		if applied.Entry.Name == name {
			return index
		}
	}
	return -1
}

// ApplyPatch verifies every span of the entry against guest memory and then
// writes them all. Nothing is written unless everything verified, and a write
// that fails part of the way through restores the spans it had already
// replaced, so the guest is never left holding half an entry.
func (session *Session) ApplyPatch(entry PatchEntry) error {
	entry.Name = strings.TrimSpace(entry.Name)
	if err := session.checkPatchEntry(entry); err != nil {
		return err
	}
	// Read and compare everything first. A verify pass that has already
	// written is not a verify pass.
	original := make([][]byte, len(entry.Patches))
	for index, patch := range entry.Patches {
		buffer := make([]byte, len(patch.Expect))
		if err := session.target.ReadMemory(uint32(patch.Address), buffer); err != nil {
			return fmt.Errorf("patch %q span %d at 0x%08x: %w", entry.Name, index+1, uint32(patch.Address), err)
		}
		if !bytes.Equal(buffer, patch.Expect) {
			return fmt.Errorf("patch %q span %d at 0x%08x expects %x but memory holds %x",
				entry.Name, index+1, uint32(patch.Address), []byte(patch.Expect), buffer)
		}
		original[index] = buffer
	}
	for index, patch := range entry.Patches {
		if err := session.target.WriteMemory(uint32(patch.Address), patch.Replace); err != nil {
			session.restore(entry.Patches[:index], original[:index])
			return fmt.Errorf("patch %q span %d at 0x%08x: %w", entry.Name, index+1, uint32(patch.Address), err)
		}
	}
	session.patches = append(session.patches, AppliedPatch{Entry: entry, original: original})
	return nil
}

// checkPatchEntry rejects an entry that could not be applied and reverted
// cleanly, before any memory is read.
func (session *Session) checkPatchEntry(entry PatchEntry) error {
	if entry.Name == "" {
		return fmt.Errorf("a patch entry needs a name")
	}
	if len(entry.Patches) == 0 {
		return fmt.Errorf("patch %q has no spans", entry.Name)
	}
	if session.findPatch(entry.Name) >= 0 {
		return fmt.Errorf("patch %q is already applied", entry.Name)
	}
	for index, patch := range entry.Patches {
		switch {
		case len(patch.Replace) == 0:
			return fmt.Errorf("patch %q span %d replaces nothing", entry.Name, index+1)
		case len(patch.Expect) != len(patch.Replace):
			return fmt.Errorf("patch %q span %d expects %d byte(s) and replaces %d, so it would change the length of the span",
				entry.Name, index+1, len(patch.Expect), len(patch.Replace))
		case len(patch.Replace) > maxPatchSpan:
			return fmt.Errorf("patch %q span %d is %d bytes, over the %d byte limit",
				entry.Name, index+1, len(patch.Replace), maxPatchSpan)
		case patch.End() > 1<<32:
			return fmt.Errorf("patch %q span %d runs past the end of the address space", entry.Name, index+1)
		}
	}
	// Two spans over the same byte have two answers about what was there
	// first, so a revert could not put back either of them.
	for left := range entry.Patches {
		for right := left + 1; right < len(entry.Patches); right++ {
			if patchesOverlap(entry.Patches[left], entry.Patches[right]) {
				return fmt.Errorf("patch %q spans %d and %d overlap, so reverting could not put back one answer",
					entry.Name, left+1, right+1)
			}
		}
	}
	for _, applied := range session.patches {
		for index, patch := range entry.Patches {
			for held, other := range applied.Entry.Patches {
				if patchesOverlap(patch, other) {
					return fmt.Errorf("patch %q span %d overlaps span %d of %q, which is already applied",
						entry.Name, index+1, held+1, applied.Entry.Name)
				}
			}
		}
	}
	return nil
}

func patchesOverlap(left, right Patch) bool {
	return uint64(left.Address) < right.End() && uint64(right.Address) < left.End()
}

// RevertPatch puts back what the named entry replaced and drops the record.
//
// It refuses when a span no longer holds what the patch wrote: something else
// owns those bytes now, and restoring over it would destroy state that has
// nothing to do with the patch. ForgetPatch drops such a record without
// writing.
func (session *Session) RevertPatch(name string) error {
	index := session.findPatch(name)
	if index < 0 {
		return fmt.Errorf("no patch named %q is applied", name)
	}
	applied := session.patches[index]
	for span, patch := range applied.Entry.Patches {
		current := make([]byte, len(patch.Replace))
		if err := session.target.ReadMemory(uint32(patch.Address), current); err != nil {
			return fmt.Errorf("patch %q span %d at 0x%08x: %w", name, span+1, uint32(patch.Address), err)
		}
		if !bytes.Equal(current, patch.Replace) {
			return fmt.Errorf("patch %q span %d at 0x%08x no longer holds what it wrote, so reverting would overwrite something else; `unpatch -forget %s` drops the record instead",
				name, span+1, uint32(patch.Address), name)
		}
	}
	for span, patch := range applied.Entry.Patches {
		if err := session.target.WriteMemory(uint32(patch.Address), applied.original[span]); err != nil {
			// Put the patch back over the spans already restored, so a failed
			// revert leaves the entry applied rather than half of it.
			session.reapply(applied.Entry.Patches[:span])
			return fmt.Errorf("patch %q span %d at 0x%08x: %w", name, span+1, uint32(patch.Address), err)
		}
	}
	session.patches = slices.Delete(session.patches, index, index+1)
	return nil
}

// RevertAllPatches reverts every applied entry, most recent first, and stops
// at the first one that refuses.
func (session *Session) RevertAllPatches() (int, error) {
	reverted := 0
	for len(session.patches) > 0 {
		name := session.patches[len(session.patches)-1].Entry.Name
		if err := session.RevertPatch(name); err != nil {
			return reverted, err
		}
		reverted++
	}
	return reverted, nil
}

// ForgetPatch drops a record without writing anything, which is what is left
// when the guest has taken those bytes back for something else.
func (session *Session) ForgetPatch(name string) bool {
	index := session.findPatch(name)
	if index < 0 {
		return false
	}
	session.patches = slices.Delete(session.patches, index, index+1)
	return true
}

func (session *Session) restore(patches []Patch, original [][]byte) {
	for index := len(patches) - 1; index >= 0; index-- {
		_ = session.target.WriteMemory(uint32(patches[index].Address), original[index])
	}
}

func (session *Session) reapply(patches []Patch) {
	for index := len(patches) - 1; index >= 0; index-- {
		_ = session.target.WriteMemory(uint32(patches[index].Address), patches[index].Replace)
	}
}

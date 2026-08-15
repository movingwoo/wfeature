package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Table 4 is the input-method block, and the title that reaches it said so
// itself. It calls two of the table's entries at startup, with no arguments,
// and looks its own four names up in what they answer: `KO`, `EN/S`, `EN/L`
// and `N123` — the specification's own vocabulary for an automaton's modes,
// an ISO 639 code with a `/S` or `/L` suffix where a script has case, and
// `N123` for digits. Nothing else in the WIPI C surface speaks in those
// strings.
//
// That also explains where the table sits. The specification prints the
// `MC_im*` functions at the end of its graphics section, which is why the
// input-method block follows graphics on the LGT side too, and why this one
// is table 4 immediately after table 3.
//
// Two entries are identified and the other three are not, which is exactly
// what the evidence supports:
//
//   - **entry 4** takes no arguments and answers a pointer the caller uses as
//     the base of a word array — `MC_imGetSupportedModes`, the only function
//     in the block returning `M_Char**`.
//   - **entry 3** takes no arguments and answers a count the caller runs down
//     to zero as the array's length — `MC_imGetSurpportModeCount`.
//
// The remaining `MC_imSetCurrentMode`, `MC_imGetCurrentMode` and
// `MC_imHandleInput` are somewhere in entries 0 to 2 — this vendor's order is
// not the specification's, since the specification would have put the count
// first — and no local title has called one, so they stay counted stubs with
// their call sites rather than guesses.
const (
	wipicIMGetSupportedModeCount = 3
	wipicIMGetSupportedModes     = 4
)

// inputModes are the language codes this platform's automaton offers. The list
// is the same one the LGT side answers with, because the specification fixes
// the vocabulary rather than the platform choosing it.
var inputModes = []string{"EN/L", "EN/S", "KO", "N123"}

// handleWIPICInputMethodCall services the input-method table.
func (runtime *initializationRuntime) handleWIPICInputMethodCall(thread *armcore.Thread, function uint32) (uint32, error) {
	switch function {
	case wipicIMGetSupportedModeCount:
		return uint32(len(inputModes)), nil
	case wipicIMGetSupportedModes:
		return runtime.inputModeTable()
	}
	// The entries that are not identified answer zero and are counted under the
	// name every stubbed table has always used, so a report from before this
	// table was named still lines up with one from after it.
	runtime.countDiagnostic(fmt.Sprintf("wipic stub table %d function %d%s",
		wipicTableInputMethod, function, runtime.callerMark(thread)))
	return 0, nil
}

// inputModeTable is the `M_Char **` the mode list is answered with: one pointer
// per code, into copies this platform owns. It is built once, because the
// caller takes no ownership of what it gets and a fresh table per call would
// leak the arena a block at a time.
func (runtime *initializationRuntime) inputModeTable() (uint32, error) {
	if runtime.inputModeTableAddress != 0 {
		return runtime.inputModeTableAddress, nil
	}
	pointers := make([]uint32, 0, len(inputModes))
	for _, mode := range inputModes {
		address, err := runtime.allocateBytes(append([]byte(mode), 0))
		if err != nil {
			return 0, err
		}
		pointers = append(pointers, address)
	}
	address, err := runtime.allocateWords(pointers)
	if err != nil {
		return 0, err
	}
	runtime.inputModeTableAddress = address
	return address, nil
}

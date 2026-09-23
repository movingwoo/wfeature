package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/jvm"
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
// Caller evidence establishes the vendor order; see the C input investigation.
const (
	wipicIMHandleInput           = 0
	wipicIMSetCurrentMode        = 1
	wipicIMGetCurrentMode        = 2
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
	case wipicIMSetCurrentMode:
		mode, err := runtime.wipicArgument(thread, 0)
		if err != nil {
			return 0, err
		}
		if mode >= uint32(len(inputModes)) {
			return 0, nil
		}
		runtime.cInput.mode = mode
		runtime.cInput.activations++
		runtime.cInput.active = true
		runtime.cInput.card = runtime.cInputCard()
		runtime.cInput.clearPending = false
		runtime.rememberCInputTimer()
		runtime.cInput.revision++
		return 1, nil
	case wipicIMGetCurrentMode:
		return runtime.cInput.mode, nil
	case wipicIMHandleInput:
		return runtime.wipicHandleInput(thread)

	case wipicIMGetSupportedModeCount:
		return uint32(len(inputModes)), nil
	case wipicIMGetSupportedModes:
		return runtime.inputModeTable()
	}
	// Unknown extensions keep the existing counted-stub behavior.
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

// A C widget owns its value and cursor. The Host can append completed text
// through the widget's existing key callback, but cannot replace its value.
type cInputState struct {
	card                        *jvm.Object
	active                      bool
	mode                        uint32
	revision                    uint64
	calls                       uint64
	activations                 uint64
	pending                     []byte
	pendingValid                func() bool
	discardCarrier              bool
	clearPending                bool
	timerPointer, timerCallback uint32
}

func (runtime *initializationRuntime) rememberCInputTimer() {
	runtime.cInput.timerPointer, runtime.cInput.timerCallback = 0, 0
	if timer := runtime.client.activeTimer; timer != nil {
		runtime.cInput.timerPointer = timer.pointer
		runtime.cInput.timerCallback = timer.callback
	}
}

const cInputCarrier int32 = '0'
const cInputFlush byte = 0x9d

// The observed KTF C widget supplies 2 for a pressed key. This is its C
// input-method type, not Java Card.KeyReleased despite the shared number.
const cInputPressed uint32 = 2

type cInputBuffer struct{ address, size, capacity uint32 }

func (runtime *initializationRuntime) wipicHandleInput(thread *armcore.Thread) (uint32, error) {
	var args [6]uint32
	for i := range args {
		value, err := runtime.wipicArgument(thread, i)
		if err != nil {
			return 0, err
		}
		args[i] = value
	}
	buffers := [2]cInputBuffer{{address: args[2], size: args[3]}, {address: args[4], size: args[5]}}
	for i := range buffers {
		if buffers[i].size != 0 {
			words, err := runtime.readAOTWords(buffers[i].size, 1, "input buffer capacity")
			if err != nil {
				return 0, err
			}
			buffers[i].capacity = words[0]
		}
	}
	state := &runtime.cInput
	host := (len(state.pending) != 0 || state.discardCarrier) && byte(args[0]) == byte(cInputCarrier) && args[1] == cInputPressed
	state.calls++
	if !host {
		// A queued CLR may flush composition only after keyNotify returns.
		// Restore availability only in its owning timer and visible card.
		timer := runtime.client.activeTimer
		clearing := state.clearPending && timer != nil && state.card == runtime.cInputCard() &&
			timer.pointer == state.timerPointer && timer.callback == state.timerCallback
		runtime.rememberCInputTimer()
		state.revision++
		// Flushing finishes composition; it does not dismiss the widget.
		if byte(args[0]) != cInputFlush || clearing {
			state.active = true
			state.activations++
			state.card = runtime.cInputCard()
		}
	}
	var value []byte
	if host {
		valid := state.pendingValid == nil || state.pendingValid()
		if timer := runtime.client.activeTimer; timer != nil && state.timerCallback != 0 {
			valid = valid && timer.pointer == state.timerPointer && timer.callback == state.timerCallback
		}
		if !valid {
			state.revision++
		} else if !state.discardCarrier {
			value = state.pending
		}
		state.discardCarrier = false
	} else if state.mode == 3 &&
		args[1] == cInputPressed && args[0] >= '0' && args[0] <= '9' {
		value = []byte{byte(args[0])}
	}
	complete := buffers[0]
	handled := len(value) > 0 && complete.address != 0 && complete.size != 0 && uint64(len(value))+1 <= uint64(complete.capacity)
	if !handled {
		value = nil
	}
	for i, buffer := range buffers {
		var text []byte
		if i == 0 {
			text = value
		}
		if buffer.address != 0 && buffer.capacity != 0 {
			data := append(append([]byte(nil), text...), 0)
			if err := runtime.client.core.Memory().Write(buffer.address, data); err != nil {
				return 0, err
			}
		}
		if buffer.size != 0 {
			if err := runtime.writeWord(buffer.size, uint32(len(text))); err != nil {
				return 0, err
			}
		}
	}
	if !handled {
		if host {
			// Consume the carrier with an empty result, retaining pending text so
			// the Host reports rejection. Returning unhandled makes the widget
			// flush and invalidates an otherwise retryable edit.
			return 1, nil
		}
		return 0, nil
	}
	if host {
		state.pending = nil
	}
	return 1, nil
}

// Remember which visible card owned activation so a later overlay cannot
// inherit an old C editor merely by requesting a fresh Host snapshot.
func (runtime *initializationRuntime) cInputCard() *jvm.Object {
	if len(runtime.displayCards) == 0 {
		return nil
	}
	return runtime.displayCards[len(runtime.displayCards)-1]
}

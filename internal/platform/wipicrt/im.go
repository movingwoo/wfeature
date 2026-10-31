package wipicrt

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
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
	WIPICIMHandleInput           = 0
	WIPICIMSetCurrentMode        = 1
	WIPICIMGetCurrentMode        = 2
	WIPICIMGetSupportedModeCount = 3
	WIPICIMGetSupportedModes     = 4
)

// InputModes are the language codes this platform's automaton offers. The list
// is the same one the LGT side answers with, because the specification fixes
// the vocabulary rather than the platform choosing it.
var InputModes = []string{"EN/L", "EN/S", "KO", "N123"}

// HandleWIPICInputMethodCall services the input-method table.
func (runtime *Runtime) HandleWIPICInputMethodCall(thread *armcore.Thread, function uint32) (uint32, error) {
	runtime.CInput.Used, runtime.CInput.Unreached = true, false
	switch function {
	case WIPICIMSetCurrentMode:
		mode, err := runtime.WIPICArgument(thread, 0)
		if err != nil {
			return 0, err
		}
		if mode >= uint32(len(InputModes)) {
			return 0, nil
		}
		runtime.RememberCInputOwner(thread)
		runtime.CInput.Mode = mode
		runtime.CInput.Activations++
		runtime.CInput.Active = true
		runtime.inputActivated()
		runtime.CInput.ClearPending = false
		runtime.rememberCInputTimer()
		runtime.CInput.Revision++
		return 1, nil
	case WIPICIMGetCurrentMode:
		return runtime.CInput.Mode, nil
	case WIPICIMHandleInput:
		return runtime.wipicHandleInput(thread)

	case WIPICIMGetSupportedModeCount:
		return uint32(len(InputModes)), nil
	case WIPICIMGetSupportedModes:
		return runtime.inputModeTable()
	}
	// Unknown extensions keep the existing counted-stub behavior.
	runtime.CountDiagnostic(fmt.Sprintf("wipic stub table %d function %d%s",
		WIPICTableInputMethod, function, runtime.CallerMark(thread)))
	return 0, nil
}

// inputModeTable is the `M_Char **` the mode list is answered with: one pointer
// per code, into copies this platform owns. It is built once, because the
// caller takes no ownership of what it gets and a fresh table per call would
// leak the arena a block at a time.
func (runtime *Runtime) inputModeTable() (uint32, error) {
	if runtime.InputModeTableAddress != 0 {
		return runtime.InputModeTableAddress, nil
	}
	pointers := make([]uint32, 0, len(InputModes))
	for _, mode := range InputModes {
		address, err := runtime.AllocateBytes(append([]byte(mode), 0))
		if err != nil {
			return 0, err
		}
		pointers = append(pointers, address)
	}
	address, err := runtime.AllocateWords(pointers)
	if err != nil {
		return 0, err
	}
	runtime.InputModeTableAddress = address
	return address, nil
}

// A C widget owns its value and cursor. The Host can append completed text
// through the widget's existing key callback, but cannot replace its value.
type CInputState struct {
	Owner                       CInputOwner
	Active                      bool
	Mode                        uint32
	Revision                    uint64
	Calls                       uint64
	Activations                 uint64
	Pending                     []byte
	PendingValid                func() bool
	DiscardCarrier              bool
	ClearPending                bool
	TimerPointer, TimerCallback uint32
	// Delivering covers the Host's own carrier event. Input-method calls the
	// widget makes while processing it — a flush after taking the completed
	// character, or the same key routed again — belong to that delivery and
	// are not a key the player pressed.
	Delivering bool
	// Used records that the title has called into this table at all, its
	// start-up questions included. It is not part of a checkpoint; see
	// cInputUsed for what stands in for it after one.
	Used bool
	// Unreached records that the Host's last carrier reached no input method:
	// the card took it as the key it is, so nothing on the screen takes keys
	// through the input method. The relaxed rule offers nothing until the
	// title calls the input method again or the player presses a key, which
	// is what can bring a field up; without it every retry of the commit
	// would hand the game another key. It is not part of a checkpoint.
	Unreached bool
}

// CInputUsed reports whether the title has ever used its input method, which
// is what the relaxed C rule offers an editor on; see cInputOffered. A
// checkpoint does not record the flag, so a restored session reads the same
// fact off what it does record: the calls and activations counted, and the
// mode table a title asking for its modes at start-up has built. A title that
// asked only for the mode count or the current mode before the checkpoint is
// not seen as using it until its next call.
func (runtime *Runtime) CInputUsed() bool {
	state := &runtime.CInput
	return state.Used || state.Calls != 0 || state.Activations != 0 || runtime.InputModeTableAddress != 0
}

func (runtime *Runtime) rememberCInputTimer() {
	runtime.CInput.TimerPointer, runtime.CInput.TimerCallback = 0, 0
	if timer := runtime.Client.ActiveTimer; timer != nil {
		runtime.CInput.TimerPointer = timer.Pointer
		runtime.CInput.TimerCallback = timer.Callback
	}
}

const CInputCarrier int32 = '0'
const CInputFlush byte = 0x9d

// The observed KTF C widget supplies 2 for a pressed key. This is its C
// input-method type, not Java Card.KeyReleased despite the shared number.
const CInputPressed uint32 = 2

type cInputBuffer struct{ Address, Size, capacity uint32 }

func (runtime *Runtime) wipicHandleInput(thread *armcore.Thread) (uint32, error) {
	var args [6]uint32
	for i := range args {
		value, err := runtime.WIPICArgument(thread, i)
		if err != nil {
			return 0, err
		}
		args[i] = value
	}
	buffers := [2]cInputBuffer{{Address: args[2], Size: args[3]}, {Address: args[4], Size: args[5]}}
	for i := range buffers {
		if buffers[i].Size != 0 {
			words, err := runtime.ReadAOTWords(buffers[i].Size, 1, "input buffer capacity")
			if err != nil {
				return 0, err
			}
			buffers[i].capacity = words[0]
		}
	}
	state := &runtime.CInput
	host := (len(state.Pending) != 0 || state.DiscardCarrier) && byte(args[0]) == byte(CInputCarrier) && args[1] == CInputPressed
	state.Calls++
	if !host {
		// A queued CLR may flush composition only after keyNotify returns.
		// Restore availability only in its owning timer and visible card.
		timer := runtime.Client.ActiveTimer
		clearing := state.ClearPending && timer != nil && runtime.inputCardCurrent() &&
			timer.Pointer == state.TimerPointer && timer.Callback == state.TimerCallback
		runtime.rememberCInputTimer()
		if !state.Delivering {
			state.Revision++
		}
		// Flushing finishes composition; it does not dismiss the widget.
		if byte(args[0]) != CInputFlush || clearing {
			state.Active = true
			state.Activations++
			runtime.inputActivated()
		}
	}
	var value []byte
	if host {
		valid := state.PendingValid == nil || state.PendingValid()
		if timer := runtime.Client.ActiveTimer; timer != nil && state.TimerCallback != 0 {
			valid = valid && timer.Pointer == state.TimerPointer && timer.Callback == state.TimerCallback
		}
		if !valid {
			state.Revision++
		} else if !state.DiscardCarrier {
			value = state.Pending
		}
		state.DiscardCarrier = false
	} else if state.Mode == 3 &&
		args[1] == CInputPressed && args[0] >= '0' && args[0] <= '9' {
		value = []byte{byte(args[0])}
	}
	complete := buffers[0]
	handled := len(value) > 0 && complete.Address != 0 && complete.Size != 0 && uint64(len(value))+1 <= uint64(complete.capacity)
	if !handled {
		value = nil
	}
	for i, buffer := range buffers {
		var text []byte
		if i == 0 {
			text = value
		}
		if buffer.Address != 0 && buffer.capacity != 0 {
			data := append(append([]byte(nil), text...), 0)
			if err := runtime.Client.core.Memory().Write(buffer.Address, data); err != nil {
				return 0, err
			}
		}
		if buffer.Size != 0 {
			if err := runtime.WriteWord(buffer.Size, uint32(len(text))); err != nil {
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
		state.Pending = nil
	}
	return 1, nil
}

// inputActivated and inputCardCurrent tie the input method to the screen it
// was opened on, for a platform whose screen is a stack of cards: a later
// overlay must not inherit an old editor merely by asking for a fresh Host
// snapshot. A platform without cards has one screen, which is always current.
func (runtime *Runtime) inputActivated() {
	if runtime.Hooks.InputActivated != nil {
		runtime.Hooks.InputActivated()
	}
}

func (runtime *Runtime) inputCardCurrent() bool {
	return runtime.Hooks.InputCardCurrent == nil || runtime.Hooks.InputCardCurrent()
}

// MaxCInputLength is the most characters a Host offers the C input method in
// one commit.
const MaxCInputLength = 64

// ValidateCInput is text a Host commits to the C input method, encoded the way
// the handset holds it: printable, KSC5601, and no longer than MaxCInputLength
// characters. Anything else is refused whole rather than cut or substituted.
func ValidateCInput(text string) ([]byte, error) {
	if err := backend.ValidateTextInput(text); err != nil || utf8.RuneCountInString(text) > MaxCInputLength {
		return nil, backend.ErrInvalidTextInput
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return nil, backend.ErrInvalidTextInput
		}
	}
	encoded, ok := backend.EncodeKSC5601(text)
	if !ok {
		return nil, backend.ErrInvalidTextInput
	}
	return encoded, nil
}

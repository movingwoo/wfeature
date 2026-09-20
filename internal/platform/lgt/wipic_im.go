package lgt

import (
	"encoding/binary"
	"math"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The input-method block: the five calls a text-entry widget makes to the
// platform's character automaton. They sit at the start of the block after
// graphics, in the specification's own order, and the arguments three of them
// are handed match that reading exactly — see docs/lgt.md.
const (
	slotIMGetSupportedModeCount uint32 = 0x12c
	slotIMGetSupportedModes     uint32 = 0x12d
	slotIMSetCurrentMode        uint32 = 0x12e
	slotIMGetCurrentMode        uint32 = 0x12f
	slotIMHandleInput           uint32 = 0x130
)

// inputModes are the language codes MC_imGetSupportedModes answers with. The
// specification fixes the vocabulary: an ISO 639 code, a "/S" or "/L" suffix
// where a script has case, and "N123" for digits. A widget indexes this list,
// so the order is part of the answer: one engine's widget builds a four-entry
// mode table, reads the first code to decide whether upper or lower case comes
// first, and then asks for mode 2.
var inputModes = []string{"EN/L", "EN/S", "KO", "N123"}

// handleInputMethod services one call in the input-method block.
func (client *Client) handleInputMethod(thread *armcore.Thread, slot uint32) error {
	answer := func(value uint32) error { return thread.SetRegister(0, value) }

	switch slot {
	case slotIMGetSupportedModeCount:
		return answer(uint32(len(inputModes)))

	case slotIMGetSupportedModes:
		table, err := client.inputModeTable()
		if err != nil {
			return err
		}
		return answer(table)

	case slotIMSetCurrentMode:
		mode, err := thread.Register(0)
		if err != nil {
			return err
		}
		// "1 if the mode was applied, 0 if it was not" — the one answer in
		// this block that is not an error code.
		if mode >= uint32(len(inputModes)) {
			return answer(0)
		}
		client.inputMode = mode
		client.cTextInput.active = true
		client.cTextInput.revision++
		return answer(1)

	case slotIMGetCurrentMode:
		return answer(client.inputMode)

	case slotIMHandleInput:
		return client.handleInputKey(thread)
	}
	return nil
}

// inputModeTable is the `M_Char **` MC_imGetSupportedModes answers with: one
// pointer per language code, into copies this platform owns. It is built once,
// because the caller keeps no ownership of what it gets and a fresh table per
// call would leak.
func (client *Client) inputModeTable() (uint32, error) {
	if client.inputModeTableAddress != 0 {
		return client.inputModeTableAddress, nil
	}
	pointers := make([]uint32, 0, len(inputModes))
	for _, mode := range inputModes {
		address, err := client.allocateBytes(append([]byte(mode), 0))
		if err != nil {
			return 0, err
		}
		pointers = append(pointers, address)
	}
	address, err := client.allocateWords(pointers)
	if err != nil {
		return 0, err
	}
	client.inputModeTableAddress = address
	return address, nil
}

// imaFlushKey is MH_IMA_FLUSH, the key value that means "finish whatever is
// being composed and hand it over". The specification defines it as -99 and
// the parameter is an M_Char, so it arrives as 0x9d.
const imaFlushKey = 0x9d

// hostTextInputCarrier is a numeric key that native widgets route to
// MC_imHandleInput. It is sent only while a serialized Host commit has pending
// text; the platform substitutes that complete text before the widget sees a
// normal zero-key result. Using a key the widget already routes is necessary:
// its dispatcher discards unknown key values before reaching the input method.
const hostTextInputCarrier = '0'

type cTextInputState struct {
	active   bool
	revision uint64
	calls    uint64
	pending  []byte
}

type inputBuffer struct {
	address  uint32
	size     uint32
	capacity uint32
}

// handleInputKey services MC_imHandleInput: a key in, a completed string and a
// composing string out, and 1 or 0 for whether the automaton took the key.
//
// Numeric mode returns one completed ASCII digit for a pressed or repeated
// key. A serialized Host commit returns its already-composed EUC-KR text as
// one completed string. Other modes do not emulate a handset keyboard layout:
// the browser or desktop IME owns letter and Hangul composition. The widget
// continues to own editing, deletion, limits and mode changes.
//
// `(key, type, buf1, size1)` arrive in registers and `(buf2, size2)` on the
// stack. The stacked pair used to be left alone because no caller had been
// available to confirm where it sits, and a pointer recovered wrongly writes
// into the game's own stack. One is available now: the caller builds both
// buffers on its own stack, stores their addresses at `[sp]` and `[sp+4]`, and
// the platform stub balances its own push before the supervisor call, so the
// stack pointer the handler sees is the one the caller had.
//
// The specification marks each size as an input capacity. The native widget
// observed here also reads it back as the output byte length. Read both
// capacities before changing either word, then write the produced lengths;
// leaving the capacities there makes that widget consume stale stack bytes.
func (client *Client) handleInputKey(thread *armcore.Thread) error {
	composing, composingSize := client.stackedInputBuffer(thread)
	completed, err := thread.Register(2)
	if err != nil {
		return err
	}
	completedSize, err := thread.Register(3)
	if err != nil {
		return err
	}
	key, err := thread.Register(0)
	if err != nil {
		return err
	}
	kind, err := thread.Register(1)
	if err != nil {
		return err
	}
	completedBuffer, err := client.readInputBuffer(completed, completedSize)
	if err != nil {
		return err
	}
	composingBuffer, err := client.readInputBuffer(composing, composingSize)
	if err != nil {
		return err
	}
	outputOnly := client.outputOnlyInputBuffers(thread, completedBuffer, composingBuffer)
	if outputOnly {
		completedBuffer.capacity, composingBuffer.capacity = 5, 5
	}

	hostCommit := key == hostTextInputCarrier && len(client.cTextInput.pending) != 0
	client.cTextInput.active = true
	client.cTextInput.calls++
	if !hostCommit {
		client.cTextInput.revision++
	}

	var value []byte
	if hostCommit {
		value = client.cTextInput.pending
		if outputOnly {
			// The SDK reserves four data bytes and a terminator. Deliver
			// complete EUC-KR characters over successive widget callbacks.
			n := 0
			for n < len(value) {
				width := 1
				if value[n] >= 0x80 {
					width = 2
				}
				if n+width > 4 || n+width > len(value) {
					break
				}
				n += width
			}
			value = value[:n]
		}
	} else if client.inputMode == 3 &&
		(kind == EventKeyPressed || kind == EventKeyRepeated) && key >= '0' && key <= '9' {
		value = []byte{byte(key)}
	}
	handled := len(value) != 0 && completedBuffer.fits(value)
	if !handled {
		value = nil
	}
	if err := client.writeInputBuffer(completedBuffer, value); err != nil {
		return err
	}
	if err := client.writeInputBuffer(composingBuffer, nil); err != nil {
		return err
	}
	if !handled {
		return thread.SetRegister(0, 0)
	}
	if hostCommit {
		client.cTextInput.pending = client.cTextInput.pending[len(value):]
	}
	return thread.SetRegister(0, 1)
}

// outputOnlyInputBuffers recognizes an SDK caller that initializes two local
// five-byte strings but treats the size words as outputs only. The published
// API specifies input capacities; applying this convention to an arbitrary
// caller would turn a zero-capacity buffer into an unchecked write. Require
// both the complete argument setup and its live stack addresses instead.
func (client *Client) outputOnlyInputBuffers(thread *armcore.Thread, completed, composing inputBuffer) bool {
	lr, err := thread.Register(armcore.RegisterLR)
	if err != nil || lr&1 == 0 || lr < 40 || client.module == nil {
		return false
	}
	end := lr &^ 1
	low, high := client.module.Span()
	if end-40 < low || end > high {
		return false
	}
	var code [40]byte
	if client.core.Memory().Read(end-40, code[:]) != nil {
		return false
	}
	// movs r6,0; address and terminate each string; pass buf2/size2 on
	// the stack and buf1/size1 in r2/r3, then call through the SDK stub.
	want := [...]uint16{0x2600, 0xaa4a, 0xab48, 0xa947, 0x7016, 0x701e,
		0x0638, 0x9100, 0xab05, 0x21fb, 0x9301, 0xaa49, 0x9649,
		0x9647, 0x0e00, 0x0049, 0xab06}
	for index, instruction := range want {
		if binary.LittleEndian.Uint16(code[index*2:]) != instruction {
			return false
		}
	}
	if binary.LittleEndian.Uint16(code[34:])&0xff00 != 0x4c00 ||
		binary.LittleEndian.Uint16(code[36:])&0xf800 != 0xf000 ||
		binary.LittleEndian.Uint16(code[38:])&0xf800 != 0xf800 {
		return false
	}
	stack, err := thread.Register(armcore.RegisterSP)
	return err == nil && stack <= math.MaxUint32-0x129 &&
		completed.address == stack+0x124 && completed.size == stack+0x18 &&
		composing.address == stack+0x11c && composing.size == stack+0x14
}

// stackedInputBuffer recovers `(buf2, size2)` — the fifth and sixth arguments,
// which the AAPCS puts on the stack. The platform stub balances its own push
// before the supervisor call, so the stack pointer here is the caller's and
// the pair sits at `[sp]` and `[sp+4]`.
//
// A stack that cannot be read gives up on the composing buffer rather than
// failing the call: the completion buffer is what a caller reads, and killing
// a game over an output it may not have passed would be the worse answer.
func (client *Client) stackedInputBuffer(thread *armcore.Thread) (buffer, size uint32) {
	stack, err := thread.Register(armcore.RegisterSP)
	if err != nil || stack == 0 {
		return 0, 0
	}
	buffer, err = client.readWord(stack)
	if err != nil {
		return 0, 0
	}
	size, err = client.readWord(stack + 4)
	if err != nil {
		return 0, 0
	}
	return buffer, size
}

func (client *Client) readInputBuffer(address, size uint32) (inputBuffer, error) {
	result := inputBuffer{address: address, size: size}
	if size == 0 {
		return result, nil
	}
	capacity, err := client.readWord(size)
	if err != nil {
		return inputBuffer{}, err
	}
	result.capacity = capacity
	return result, nil
}

func (buffer inputBuffer) fits(value []byte) bool {
	return buffer.address != 0 && buffer.size != 0 &&
		uint64(len(value))+1 <= uint64(buffer.capacity)
}

// writeInputBuffer writes one completed/composing result and its byte length.
// An empty result still terminates a writable buffer. A missing buffer is
// tolerated, but a supplied size word always receives zero so a caller never
// mistakes its old capacity for produced text.
func (client *Client) writeInputBuffer(buffer inputBuffer, value []byte) error {
	if len(value) != 0 {
		data := append(append([]byte(nil), value...), 0)
		if err := client.core.Memory().Write(buffer.address, data); err != nil {
			return err
		}
	} else if buffer.address != 0 && buffer.capacity != 0 {
		if err := client.core.Memory().Write(buffer.address, []byte{0}); err != nil {
			return err
		}
	}
	if buffer.size != 0 {
		return client.writeWord(buffer.size, uint32(len(value)))
	}
	return nil
}

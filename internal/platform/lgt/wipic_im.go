package lgt

import (
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

// handleInputKey services MC_imHandleInput: a key in, a completed string and a
// composing string out, and 1 or 0 for whether the automaton took the key.
//
// **This is the null automaton, and that is a defined answer rather than a
// stub.** The specification says what happens to a key the automaton cannot
// use: whatever is composing goes into the completion buffer, and the call
// returns 0. An automaton that composes nothing is never composing anything,
// so both buffers come back empty and every key answers 0 — which is exactly
// what this does, for every key including MH_IMA_FLUSH.
//
// What is missing is composition itself, and no local title asks for it. Three
// archives reach this call and each reaches it once, from a text widget's
// constructor, with MH_IMA_FLUSH and two freshly zeroed eight-byte buffers:
// the widget resets the automaton before it starts and then never routes a
// character key through the platform. So the gap stands where it did, and it
// stands deliberately — the mode that widget selects is Hangul, and composing
// Hangul from a twelve-key pad is a handset vendor's own layout rather than
// anything the specification fixes.
//
// `(key, type, buf1, size1)` arrive in registers and `(buf2, size2)` on the
// stack. The stacked pair used to be left alone because no caller had been
// available to confirm where it sits, and a pointer recovered wrongly writes
// into the game's own stack. One is available now: the caller builds both
// buffers on its own stack, stores their addresses at `[sp]` and `[sp+4]`, and
// the platform stub balances its own push before the supervisor call, so the
// stack pointer the handler sees is the one the caller had.
//
// A size is the caller's capacity and stays as it stands — the specification
// marks both as `[in]`, and the caller here reads its completion buffer as a
// string rather than by length.
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
	for _, buffer := range [2][2]uint32{{completed, completedSize}, {composing, composingSize}} {
		if err := client.emptyInputBuffer(buffer[0], buffer[1]); err != nil {
			return err
		}
	}
	// 0 is "the automaton did not handle it", which is the whole of what this
	// automaton ever answers.
	return thread.SetRegister(0, 0)
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

// emptyInputBuffer writes the terminator that says "nothing here" into one of
// MC_imHandleInput's output buffers, leaving a caller that reads it as a
// string with an empty one. A buffer with no capacity is left untouched
// rather than written to.
func (client *Client) emptyInputBuffer(buffer, size uint32) error {
	if buffer == 0 || size == 0 {
		return nil
	}
	capacity, err := client.readWord(size)
	if err != nil {
		return err
	}
	if capacity == 0 {
		return nil
	}
	return client.core.Memory().Write(buffer, []byte{0})
}

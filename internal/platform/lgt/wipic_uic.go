package lgt

import "github.com/movingwoo/wfeature/internal/armcore"

// Block eight is the user-interface component block, and its first three slots
// are what named it. A title's character-naming screen calls `0x320` with no
// arguments and keeps the result, calls `0x321` with a pointer to the string
// **"TextComponent"**, and calls `0x322` with the two of them — which is
// `MC_uicCreateApplicationContext`, `MC_uicGetClass` and `MC_uicCreate` in the
// specification's own order, with the class name it documents for a text
// widget. The block arithmetic agrees: every identified block here starts at a
// hundred times its number, and eight hundred is `0x320`.
//
// This replaces an earlier reading of `0x320` as a clock. Nothing rested on it
// — no local title reaches the slot except this one — and the two readings are
// not close: a title that asked for the time and got a widget context would
// have been given a small integer that happens to rise.
//
// The block runs to `MC_uicGetActiveListItem`, forty-one functions on, and the
// numbering below is the specification's list in order.
const (
	slotUicCreateApplicationContext uint32 = 0x320
	slotUicGetClass                 uint32 = 0x321
	slotUicCreate                   uint32 = 0x322
	slotUicLast                     uint32 = 0x348
)

// uicApplicationContext is what MC_uicCreateApplicationContext answers. It is a
// token: this platform draws no widgets, so there is nothing behind it, and a
// title only ever hands it back to MC_uicCreate.
const uicApplicationContext uint32 = 0x1c0000

// handleUIC serves the component block.
//
// **A component cannot be created here.** The widgets this block describes are
// drawn by the handset over the game's screen and typed into with the handset's
// own editor; a runtime that answered a handle and drew nothing would leave a
// title waiting for text from something invisible, which is worse than a
// refusal it can see. So the two calls that hand out an object fail, and the
// rest of the block is accepted and does nothing — the same shape the other
// WIPI platform's component table has, and for the same reason.
//
// The one title that reaches it asks for a text component to name a character
// with. What it does with the refusal is what the next run says; the platform's
// own text entry (wipic_im.go) is the path that does work, and a title that
// takes it needs nothing here.
func (client *Client) handleUIC(thread *armcore.Thread, slot uint32) error {
	switch slot {
	case slotUicCreateApplicationContext:
		return thread.SetRegister(0, uicApplicationContext)
	case slotUicGetClass, slotUicCreate:
		return answerCode(thread, wipiError)
	}
	return answerCode(thread, wipiSuccess)
}

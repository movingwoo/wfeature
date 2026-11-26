package lgt

import (
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/wipic"
)

// Block eight is the user-interface component block, and its first three slots
// are what named it. A title's character-naming screen calls `0x320` with no
// arguments and keeps the result, calls `0x321` with a pointer to the string
// **"TextComponent"**, and calls `0x322` with the two of them — which is
// `MC_uicCreateApplicationContext`, `MC_uicGetClass` and `MC_uicCreate` in the
// specification's own order, with the class name it documents for a text
// widget. The block arithmetic agrees: every identified block here starts at a
// hundred times its number, and eight hundred is `0x320`.
//
// An earlier reading treated `0x320` itself as a clock. A second title makes
// the distinction visible when saving. It obtains a context, looks up
// DateTimeComponent, creates it and asks MC_uicGetTime to fill a struct tm.
// The application context remains the same across those component instances.
//
// The block runs to `MC_uicGetActiveListItem`, forty-one functions on, and the
// numbering below is the specification's list in order.
const (
	slotUicCreateApplicationContext uint32 = 0x320
	slotUicGetClass                 uint32 = 0x321
	slotUicCreate                   uint32 = 0x322
	slotUicDestroy                  uint32 = 0x323
	slotUicSetTime                  uint32 = 0x336
	slotUicSetTimeLong              uint32 = 0x337
	slotUicGetTime                  uint32 = 0x338
	slotUicLast                     uint32 = 0x348
)

// MC_uicCreateApplicationContext returns an opaque token. The guest passes
// it back when creating each component.
const (
	uicApplicationContext uint32 = 0x1c0000
	uicDateClass                 = uicApplicationContext + 1
	uicFirstDate                 = uicDateClass + 1
	maxDateComponents            = 256
)

// A DateTimeComponent owns the local struct tm sampled at creation until the
// application sets another value. Its guest handle identifies a table entry.
type uicDate [wipic.TimeRecordSize]byte

// handleUIC serves the date value independently of a handset's widget editor.
// A character-naming screen also asks for TextComponent. Giving it a handle
// would make it wait for input from an invisible widget, so text creation
// still reports an error. The WIPI-C input method supplies the separate path
// for a title that owns its own editor.
func (client *Client) handleUIC(thread *armcore.Thread, slot uint32) error {
	args := thread.Context().Registers
	component, value := args[0], args[1]
	switch slot {
	case slotUicCreateApplicationContext:
		return thread.SetRegister(0, uicApplicationContext)
	case slotUicGetClass:
		name, err := client.readCString(component)
		if err == nil && name == "DateTimeComponent" {
			return thread.SetRegister(0, uicDateClass)
		}
		return answerCode(thread, wipiError)
	case slotUicCreate:
		if component != uicApplicationContext || value != uicDateClass {
			return answerCode(thread, wipiError)
		}
		index := slices.Index(client.dateComponents, nil)
		if index < 0 {
			index = len(client.dateComponents)
			if index == maxDateComponents {
				return answerCode(thread, wipiNoMemory)
			}
			client.dateComponents = append(client.dateComponents, nil)
		}
		date := uicDate(wipic.TimeRecord(time.UnixMilli(client.clock.unixMillis()).In(time.Local)))
		client.dateComponents[index] = &date
		return thread.SetRegister(0, uicFirstDate+uint32(index))
	}
	index := component - uicFirstDate
	if index >= uint32(len(client.dateComponents)) || client.dateComponents[index] == nil {
		return answerCode(thread, wipiSuccess)
	}
	date := client.dateComponents[index]
	switch slot {
	case slotUicDestroy:
		client.dateComponents[index] = nil
		for n := len(client.dateComponents); n > 0 && client.dateComponents[n-1] == nil; n-- {
			client.dateComponents = client.dateComponents[:n-1]
		}
	case slotUicSetTime:
		if value != 0 {
			var next uicDate
			if err := client.core.Memory().Read(value, next[:]); err != nil {
				return answerCode(thread, wipiError)
			}
			*date = next
		}
	case slotUicSetTimeLong:
		*date = uicDate(wipic.TimeRecord(time.Unix(int64(int32(value)), 0).In(time.Local)))
	case slotUicGetTime:
		if value != 0 {
			if err := client.core.Memory().Write(value, date[:]); err != nil {
				return answerCode(thread, wipiError)
			}
		}
	}
	return answerCode(thread, wipiSuccess)
}

package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Table 0x1f8 is LGT's own, and unlike the WIPI C table there is no
// specification for it: the slot numbers below are the ones modules here are
// observed to resolve, and neither the original runtime's symbols nor the
// reference implementation's name says what they do.
const (
	// oemSlotConfigure is what every non-Java module resolves and calls, twice,
	// as the first thing its initializer does. It takes four arguments and its
	// result is discarded by every caller here, so there is no return value to
	// get wrong.
	//
	// The two calls pass (0, 0x64, 240, 0) and (0, 0x65, 320, 0) — a pair of
	// small keys against this platform's LCD width and height, which reads like
	// the module declaring the screen it was built for. That reading is not
	// confirmed and nothing acts on it: the values already match the LCD, and
	// resizing the screen on an unconfirmed guess would break every title if
	// the guess were wrong. Some modules make a third call with the same key
	// and a value loaded from their own data, which the reading does not
	// explain.
	oemSlotConfigure uint32 = 0x16
	// oemSlotJava is resolved only along the Java path, which this platform
	// does not implement, so nothing here has ever reached it.
	oemSlotJava uint32 = 0x17
)

// knownOEMSlot reports whether reaching a slot is expected. As with the WIPI C
// table, every slot is handed a stub at resolution time and this only decides
// whether arriving at one is a gap worth reporting.
func knownOEMSlot(slot uint32) bool {
	return slot == oemSlotConfigure || slot == oemSlotJava
}

// handleOEMSVC services one call into table 0x1f8.
func (client *Client) handleOEMSVC(_ context.Context, thread *armcore.Thread, slot uint32) error {
	if slot != oemSlotConfigure {
		return fmt.Errorf("unimplemented LGT OEM slot %#x", slot)
	}
	// Accepted and ignored, which is all that is known to be right. The
	// arguments are logged because they are the only evidence about this call
	// that a real archive can produce.
	if client.logger != nil {
		arguments := make([]uint32, 4)
		for index := range arguments {
			value, err := thread.Register(index)
			if err != nil {
				return err
			}
			arguments[index] = value
		}
		client.logger.Debug("LGT OEM configure",
			"argument0", arguments[0], "argument1", arguments[1],
			"argument2", arguments[2], "argument3", arguments[3])
	}
	return thread.SetRegister(0, 0)
}

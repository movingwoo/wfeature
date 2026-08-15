package wipic

import "fmt"

// SystemProperties are the answers MC_knlGetSystemProperty gives. They match
// the original runtime's accepted set, and they are the same on every WIPI
// platform because the question is about the handset rather than about which
// runtime is asking.
//
// The phone number is a real-looking eleven digits rather than an empty
// string, because a title that takes the last four digits off the number it is
// given asks to copy minus four bytes and dies during its own startup, and a
// handset never answers with nothing.
//
// It is also the one property a user has a reason to change, which is why
// SetSubscriberNumber exists: two local titles read the number and want
// opposite things from it, and no single answer serves both. See
// docs/network.md, "The subscriber number is the one property worth changing".
//
// MIN, the subscriber's own identity, answers with the same number, because a
// handset has one line and titles read whichever of the two they were written
// against.
var SystemProperties = map[string]string{
	"RSSILEVEL":    "30",
	"BATTERYLEVEL": "100",
	"MAXRSSILEVEL": "100",
	"MAXBATTLEVEL": "100",
	// The number of steps the hardware's volume and vibrator have, not a
	// current setting. A title reads one of these and divides by it, so a
	// missing answer is not a title that skips the feature — it is
	// Integer.parseInt("") during startApp, which is where one of them died.
	// The vibrator answers zero because nothing here vibrates, which is the
	// documented way to say the hardware has no steps.
	"VOLUMELEVEL":   "5",
	"VIBRATORLEVEL": "0",
	// A Korean handset's zone, in the format the specification gives.
	"TIMEZONE":       "GMT+09:00",
	"PHONEMODEL":     "Emulator",
	"PHONENUMBER":    defaultSubscriberNumber,
	"MIN":            defaultSubscriberNumber,
	"ESN":            "00000000",
	"ANNUN_CALL":     "0",
	"ANNUN_SMS":      "0",
	"ANNUN_SILENT":   "0",
	"ANNUN_ALARM":    "0",
	"ANNUN_SECURITY": "0",
	"CURRENTCH":      "0",
	"AIRPLANE_MODE":  "0",
	"ROAMING_AREA":   "0",
	"DS_LOCK":        "0",
}

// defaultSubscriberNumber is what this platform answers with when nothing asks
// for another one.
const defaultSubscriberNumber = "01000000000"

// maxSubscriberNumber is the width the shortest buffer a title reads the
// number into can hold. One title copies it into twelve bytes and compares it
// as a string, so eleven digits and a terminator is the ceiling.
const maxSubscriberNumber = 11

// SetSubscriberNumber changes the number `PHONENUMBER` and `MIN` answer with.
//
// It exists because the number is a decision, not a fact: one local title
// treats any number of five digits or more as a subscriber it can bill and
// then gates on a receipt issued to another handset, while another needs a
// full-length number for the certificate it checks. A shorter answer opens the
// first and closes the second, so the choice belongs to whoever is running the
// emulator rather than to this file. docs/network.md has the measurements.
//
// It is called once, from a host's startup, before any session exists —
// nothing here is safe to change while a game is running.
func SetSubscriberNumber(number string) error {
	if number == "" {
		// A title that takes the last four digits off the number would ask to
		// copy minus four bytes, which is how this was learned.
		return fmt.Errorf("the subscriber number cannot be empty")
	}
	if len(number) > maxSubscriberNumber {
		return fmt.Errorf("the subscriber number %q is longer than the %d digits a title reads it into",
			number, maxSubscriberNumber)
	}
	for _, symbol := range number {
		if symbol < '0' || symbol > '9' {
			return fmt.Errorf("the subscriber number %q is not digits", number)
		}
	}
	SystemProperties["PHONENUMBER"] = number
	SystemProperties["MIN"] = number
	return nil
}

// SubscriberNumber is the number this platform is currently answering with.
func SubscriberNumber() string { return SystemProperties["PHONENUMBER"] }

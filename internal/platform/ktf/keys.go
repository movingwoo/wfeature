package ktf

import "strings"

// KeyCodeByName resolves the key names a route or a command line uses to the
// WIPI key code games compare against. Digits, star, and hash are their own
// characters.
func KeyCodeByName(name string) (int32, bool) {
	switch strings.ToLower(name) {
	case "up":
		return KeyUp, true
	case "down":
		return KeyDown, true
	case "left":
		return KeyLeft, true
	case "right":
		return KeyRight, true
	case "fire", "ok":
		return KeyFire, true
	case "soft1":
		return KeyLeftSoft, true
	case "soft2":
		return KeyRightSoft, true
	case "soft3", "ez":
		return KeyThirdSoft, true
	case "clear":
		return KeyClear, true
	case "call":
		return KeyCall, true
	case "hangup":
		return KeyHangup, true
	}
	if len(name) == 1 && (name[0] >= '0' && name[0] <= '9' || name[0] == '*' || name[0] == '#') {
		return int32(name[0]), true
	}
	return 0, false
}

package clet

// WIPI key event types and key codes match the original runtime values a game
// compares against; a Clet receives them through its event entry (see clet.go).
const (
	KeyPressed  int32 = 1
	KeyReleased int32 = 2
	KeyRepeated int32 = 3

	KeyUp         int32 = -1
	KeyDown       int32 = -2
	KeyLeft       int32 = -3
	KeyRight      int32 = -4
	KeyFire       int32 = -5
	KeyLeftSoft   int32 = -6
	KeyRightSoft  int32 = -7
	KeyThirdSoft  int32 = -8
	KeyCall       int32 = -10
	KeyHangup     int32 = -11
	KeyVolumeUp   int32 = -13
	KeyVolumeDown int32 = -14
	KeyClear      int32 = -16
	KeyNum0       int32 = '0'
	KeyNum9       int32 = '9'
	KeyHash       int32 = '#'
	KeyStar       int32 = '*'
)

// The platform's own screen when a title names none; a native package carries
// no descriptor screen, so these are what a Clet draws on.
const (
	runtimeDisplayPixelWidth  = 240
	runtimeDisplayPixelHeight = 320
)

// maxClipBufferBytes bounds one media clip's sound data.
const maxClipBufferBytes = 1 << 20

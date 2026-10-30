package clet

import "time"

// wipicTimer is a registered MC_knlSetTimer request. A Clet has no
// java/util/Timer and no card, so a timer is only a guest callback the service
// round runs when it is due. See wipicDefTimer/wipicSetTimer in runtime.go.
type wipicTimer struct {
	pointer  uint32
	callback uint32
	param    uint32
	delay    uint64
	// due is when the callback may run.
	due time.Time
}

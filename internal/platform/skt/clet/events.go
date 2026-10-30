package clet

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A Clet receives input and redraw requests through the WIPI event queue: it
// posts to the queue with MC_grpPostEvent and the service round delivers what
// is waiting to its event entry. See clet.go serviceCletEvents.

// Event kinds match the values the original runtime puts in the guest's int[4].
const (
	eventKindKey     int32 = 1
	eventKindPointer int32 = 2
	eventKindRepaint int32 = 41
	eventKindNotify  int32 = 1000

	// maxQueuedEvents bounds queue growth when a game stops draining it.
	maxQueuedEvents = 256
)

type guestEvent struct {
	kind   int32
	param1 int32
	param2 int32
	param3 int32
}

// postGuestEvent appends one event. A full queue drops its oldest entry: a game
// that stopped reading events must not grow Host memory without bound.
func (runtime *initializationRuntime) postGuestEvent(event guestEvent) {
	if len(runtime.events) >= maxQueuedEvents {
		copy(runtime.events, runtime.events[1:])
		runtime.events = runtime.events[:len(runtime.events)-1]
	}
	runtime.events = append(runtime.events, event)
}

func (runtime *initializationRuntime) nextGuestEvent() (guestEvent, bool) {
	if len(runtime.events) == 0 {
		return guestEvent{}, false
	}
	event := runtime.events[0]
	runtime.events = runtime.events[1:]
	return event, true
}

// postRepaintEvent queues a redraw request for a guest that drains its own
// event loop, coalescing it with one already waiting.
func (runtime *initializationRuntime) postRepaintEvent() {
	if !runtime.guestEventLoop {
		return
	}
	for _, pending := range runtime.events {
		if pending.kind == eventKindRepaint {
			return
		}
	}
	runtime.postGuestEvent(guestEvent{kind: eventKindRepaint})
}

// wipicPostEvent answers MC_grpPostEvent: the program queues an event for its
// own event entry. The specification gives it 1 for a queued event.
func (runtime *initializationRuntime) wipicPostEvent(thread *armcore.Thread) (uint32, error) {
	kind, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	first, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	second, err := thread.Register(3)
	if err != nil {
		return 0, err
	}
	runtime.countDiagnostic(fmt.Sprintf("postEvent %#x", kind))
	runtime.postGuestEvent(guestEvent{
		kind:   eventKindNotify,
		param1: int32(kind),
		param2: int32(first),
		param3: int32(second),
	})
	return wipicPostEventQueued, nil
}

// wipicPostEventQueued is MC_grpPostEvent's success answer.
const wipicPostEventQueued uint32 = 1

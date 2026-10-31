package wipicrt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A program receives input and redraw requests through the WIPI event queue: it
// posts to the queue with MC_grpPostEvent, and the platform's service round
// delivers what is waiting to the program's event handler.

// Event kinds match the values the original runtime puts in the guest's int[4].
const (
	EventKindKey     int32 = 1
	EventKindPointer int32 = 2
	EventKindRepaint int32 = 41
	EventKindNotify  int32 = 1000

	// MaxQueuedEvents bounds queue growth when a game stops draining it.
	MaxQueuedEvents = 256
)

type GuestEvent struct {
	Kind   int32
	Param1 int32
	Param2 int32
	Param3 int32
}

// PostGuestEvent appends one event. A full queue drops its oldest entry: a game
// that stopped reading events must not grow Host memory without bound.
func (runtime *Runtime) PostGuestEvent(event GuestEvent) {
	if len(runtime.Events) >= MaxQueuedEvents {
		copy(runtime.Events, runtime.Events[1:])
		runtime.Events = runtime.Events[:len(runtime.Events)-1]
	}
	runtime.Events = append(runtime.Events, event)
}

func (runtime *Runtime) NextGuestEvent() (GuestEvent, bool) {
	if len(runtime.Events) == 0 {
		return GuestEvent{}, false
	}
	event := runtime.Events[0]
	runtime.Events = runtime.Events[1:]
	return event, true
}

// PostRepaintEvent queues a redraw request for a guest that drains its own
// event loop, coalescing it with one already waiting.
func (runtime *Runtime) PostRepaintEvent() {
	if !runtime.GuestEventLoop {
		return
	}
	for _, pending := range runtime.Events {
		if pending.Kind == EventKindRepaint {
			return
		}
	}
	runtime.PostGuestEvent(GuestEvent{Kind: EventKindRepaint})
}

// wipicPostEvent answers MC_grpPostEvent: the program queues an event for its
// own event entry. The specification gives it 1 for a queued event.
func (runtime *Runtime) wipicPostEvent(thread *armcore.Thread) (uint32, error) {
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
	runtime.CountDiagnostic(fmt.Sprintf("postEvent %#x", kind))
	runtime.PostGuestEvent(GuestEvent{
		Kind:   EventKindNotify,
		Param1: int32(kind),
		Param2: int32(first),
		Param3: int32(second),
	})
	return WIPICPostEventQueued, nil
}

// WIPICPostEventQueued is MC_grpPostEvent's success answer.
const WIPICPostEventQueued uint32 = 1

package backend

import (
	"errors"
	"fmt"
	"sync"
)

const (
	defaultMaxPendingEvents = 1024
	defaultMaxEventsPerRun  = 4096
)

var (
	ErrEventQueueFull = errors.New("event queue is full")
	ErrEventRunLimit  = errors.New("event run limit exceeded")
	ErrEventLoopBusy  = errors.New("event loop is already running")
)

type EventLoopOptions struct {
	MaxPendingEvents int
	MaxEventsPerRun  int
}

type queuedEvent struct {
	name    string
	handler func() error
}

// EventLoop is a bounded cooperative FIFO used by platform runtimes. Handlers
// run on the caller's goroutine, which keeps every Host on the same execution
// path.
type EventLoop struct {
	mu               sync.Mutex
	events           []queuedEvent
	running          bool
	maxPendingEvents int
	maxEventsPerRun  int
}

func NewEventLoop(options EventLoopOptions) *EventLoop {
	if options.MaxPendingEvents <= 0 {
		options.MaxPendingEvents = defaultMaxPendingEvents
	}
	if options.MaxEventsPerRun <= 0 {
		options.MaxEventsPerRun = defaultMaxEventsPerRun
	}
	return &EventLoop{
		maxPendingEvents: options.MaxPendingEvents,
		maxEventsPerRun:  options.MaxEventsPerRun,
	}
}

func (loop *EventLoop) Post(name string, handler func() error) error {
	if name == "" {
		return fmt.Errorf("event name is empty")
	}
	if handler == nil {
		return fmt.Errorf("event %q has no handler", name)
	}

	loop.mu.Lock()
	defer loop.mu.Unlock()
	if len(loop.events) >= loop.maxPendingEvents {
		return fmt.Errorf("%w (limit %d)", ErrEventQueueFull, loop.maxPendingEvents)
	}
	loop.events = append(loop.events, queuedEvent{name: name, handler: handler})
	return nil
}

// Run drains queued events in FIFO order. Events posted by a handler are
// processed by the same run, subject to MaxEventsPerRun.
func (loop *EventLoop) Run() error {
	loop.mu.Lock()
	if loop.running {
		loop.mu.Unlock()
		return ErrEventLoopBusy
	}
	loop.running = true
	loop.mu.Unlock()
	defer func() {
		loop.mu.Lock()
		loop.running = false
		loop.mu.Unlock()
	}()

	for processed := 0; ; processed++ {
		loop.mu.Lock()
		if len(loop.events) == 0 {
			loop.mu.Unlock()
			return nil
		}
		if processed >= loop.maxEventsPerRun {
			loop.mu.Unlock()
			return fmt.Errorf("%w (limit %d)", ErrEventRunLimit, loop.maxEventsPerRun)
		}
		event := loop.events[0]
		loop.events[0] = queuedEvent{}
		loop.events = loop.events[1:]
		loop.mu.Unlock()

		if err := event.handler(); err != nil {
			return fmt.Errorf("handle event %q: %w", event.name, err)
		}
	}
}

func (loop *EventLoop) Pending() int {
	loop.mu.Lock()
	defer loop.mu.Unlock()
	return len(loop.events)
}

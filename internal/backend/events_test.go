package backend

import (
	"errors"
	"reflect"
	"testing"
)

func TestEventLoopRunsFIFOIncludingEventsPostedByHandler(t *testing.T) {
	loop := NewEventLoop(EventLoopOptions{})
	var order []string
	if err := loop.Post("first", func() error {
		order = append(order, "first")
		return loop.Post("third", func() error {
			order = append(order, "third")
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := loop.Post("second", func() error {
		order = append(order, "second")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := loop.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if want := []string{"first", "second", "third"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("event order = %v, want %v", order, want)
	}
}

func TestEventLoopBoundsPendingAndRepeatedEvents(t *testing.T) {
	loop := NewEventLoop(EventLoopOptions{MaxPendingEvents: 1, MaxEventsPerRun: 2})
	var repeat func() error
	repeat = func() error {
		return loop.Post("repeat", repeat)
	}
	if err := loop.Post("repeat", repeat); err != nil {
		t.Fatal(err)
	}
	if err := loop.Post("overflow", func() error { return nil }); !errors.Is(err, ErrEventQueueFull) {
		t.Fatalf("Post() error = %v, want ErrEventQueueFull", err)
	}
	if err := loop.Run(); !errors.Is(err, ErrEventRunLimit) {
		t.Fatalf("Run() error = %v, want ErrEventRunLimit", err)
	}
	if got := loop.Pending(); got != 1 {
		t.Fatalf("Pending() = %d, want 1", got)
	}
}

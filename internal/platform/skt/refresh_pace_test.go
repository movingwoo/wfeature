package skt

import (
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func TestXDisplayRefreshPacesProducerAtSelectedSpeed(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3), Speed: 0.25})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Destroy(true)
	refresh := func() {
		t.Helper()
		if _, err := runtime.VM.InvokeStatic(skvm.XDisplayClass, "refresh", "(IIII)V", jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(4), jvm.IntValue(3)); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	start := time.Now()
	refresh()
	refresh()
	if elapsed := time.Since(start); elapsed < 120*time.Millisecond {
		t.Fatalf("two refresh intervals at 0.25x took %v, want at least 120ms", elapsed)
	}
	// A live speed change shortens the next interval. Reading the guest clock
	// also accounts for the time the producer spends drawing between calls.
	runtime.SetSpeed(4)
	start = time.Now()
	refresh()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("refresh did not respond to live speed change: %v", elapsed)
	}
}

func TestRefreshPaceStopsWithoutHostTicks(t *testing.T) {
	archive, err := Open(canvasJAR)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 4, 3)})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Destroy(true)
	// A deliberately distant deadline must not keep a guest alive after the
	// session ends. No presentation or input event is needed to release it.
	runtime.lastRefresh = runtime.pace.Now().Add(time.Hour)
	done := make(chan error, 1)
	go func() { done <- runtime.paceRefresh() }()
	runtime.transition("test shutdown", StateDestroyed)
	select {
	case err := <-done:
		if !errors.Is(err, jvm.ErrClosed) {
			t.Fatalf("paced producer returned %v, want ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("paced producer remained blocked after shutdown")
	}
}

package lgt

import (
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func TestLGTWIPIListenerShortensTickAtScoreEnd(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	f.session.tick = time.Second
	f.setListener(f.listeners[0])
	f.call("play", true)
	if span := f.session.tickSpan(); span != 0 {
		t.Fatalf("queued START waits %v", span)
	}
	f.drain()
	length := f.duration()
	if length >= f.session.tick || f.session.tickSpan() != length {
		t.Fatalf("score end waits %v, want %v below the frame period", f.session.tickSpan(), length)
	}
	f.advance(length / 2)
	f.call("pause", true)
	f.drain()
	if span := f.session.tickSpan(); span != f.session.tick {
		t.Fatalf("paused audio left a running deadline: %v", span)
	}
	f.advance(time.Hour)
	f.call("resume", true)
	f.drain()
	if span := f.session.tickSpan(); span != length/2 {
		t.Fatalf("resumed score end waits %v, want %v", span, length/2)
	}
	if err := f.session.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.wantHistory(f.event(0, 2), f.event(0, 4), f.event(0, 5), f.event(0, 1))
	if span := f.session.tickSpan(); span != f.session.tick {
		t.Fatalf("completed score left a running deadline: %v", span)
	}
}

func TestLGTWIPIListenerZeroLengthOneShotIsDueImmediately(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	clip := f.client.clips[f.clip]
	handle, err := f.client.audio.LoadEvents([]smaf.Event{{Type: smaf.EventProgramChange, Program: 1}})
	if err != nil {
		t.Fatal(err)
	}
	clip.loaded, clip.handle = true, handle
	f.setListener(f.listeners[0])
	f.call("play", true)
	if span := f.session.tickSpan(); span != 0 {
		t.Fatalf("zero-length score waits %v", span)
	}
	if err := f.session.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.wantHistory(f.event(0, 2), f.event(0, 1))
	if f.drain() != 0 {
		t.Fatal("zero-length score kept completing")
	}
}

func TestLGTWIPIListenerQueuesAreBounded(t *testing.T) {
	t.Run("native transitions", func(t *testing.T) {
		f := newLGTWIPIListenerFixture(t)
		f.setListener(f.listeners[0])
		for index := 0; index < maxJavaMediaEvents/2; index++ {
			f.call("play", true)
			f.call("stop", true)
		}
		if _, err := javaPlayerPlay(f.client, t.Context(), f.client.thread, []uint32{f.clip, 0}); err == nil {
			t.Fatal("native transitions exceeded the queue limit")
		}
		if len(f.client.javaMediaEvents) != maxJavaMediaEvents {
			t.Fatal("overflow changed the pending queue")
		}
	})
	t.Run("natural completions", func(t *testing.T) {
		f := newLGTWIPIListenerFixture(t)
		f.setListener(f.listeners[0])
		f.call("play", true, true)
		f.client.clock.advance(maxJavaMediaEvents * f.duration())
		if err := f.client.serviceAudio(); err == nil {
			t.Fatal("natural completions exceeded the queue limit")
		}
		if len(f.client.javaMediaEvents) != 1 {
			t.Fatal("overflow partially appended completion history")
		}
	})
}

func TestLGTWIPIListenerRegistrationDoesNotReplayUnobservedPasses(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	f.call("play", true, true)
	f.advance((maxJavaMediaEvents + 1) * f.duration())
	f.setListener(f.listeners[0])
	if f.drain() != 0 {
		t.Fatal("late registration replayed old passes")
	}
	f.advance(f.duration())
	f.drain()
	f.wantHistory(f.event(0, 1))
}

func TestLGTWIPIListenerUncaughtExceptionDoesNotDropLaterEvents(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	f.setListener(f.listeners[0])
	f.call("play", true)
	f.call("stop", true)
	throw, err := f.client.stub(svcCategoryJava, javaSVCThrowNull)
	if err != nil {
		t.Fatal(err)
	}
	run := f.word(f.word(testfixture.LGTListenerClassHandle) - javaClassHeader + javaClassMethodRun)
	f.writeWord(run+4+javaMethodWords*4+20, throw)
	f.client.releaseJavaPins(0)
	if err := f.client.serviceMediaCallbacks(t.Context()); err != nil {
		t.Fatal(err)
	}
	if count, _ := f.client.UncaughtCallbacks(); count != 2 {
		t.Fatalf("the batch entered %d throwing callbacks, want 2", count)
	}
	if len(f.client.javaMediaEvents) != 0 || len(f.client.javaRun.pins) != 0 {
		t.Fatal("throwing callbacks left queued work or detached pins")
	}
}

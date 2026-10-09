package ktf

import (
	"math"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
)

type speedChangeClock struct {
	*ManualClock
	reads int
}

func (clock *speedChangeClock) Now() time.Time {
	clock.reads++
	return clock.ManualClock.Now()
}

func TestSpeedChangePreservesGuestTime(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	clock.Advance(time.Second)
	for _, speed := range []float64{0.5, 2, 0.1, 16} {
		elapsed, millis := runtime.guestElapsed(), runtime.guestMillis()
		client.SetSpeed(speed)
		if got, date := runtime.guestElapsed(), runtime.guestMillis(); got != elapsed || date != millis {
			t.Fatalf("changing to %gx moved guest time: elapsed %s -> %s, date %d -> %d", speed, elapsed, got, millis, date)
		}
		clock.Advance(100 * time.Millisecond)
		want := elapsed + time.Duration(float64(100*time.Millisecond)*speed)
		if got, date := runtime.guestElapsed(), runtime.guestMillis(); got != want || date != millis+int64((want-elapsed)/time.Millisecond) {
			t.Fatalf("%gx did not advance at its new rate: elapsed %s, want %s; date %d", speed, got, want, date)
		}
	}
}

func TestSpeedChangePreservesAudioGateAndRemainingPass(t *testing.T) {
	for _, speed := range []float64{0.5, 2} {
		t.Run(map[float64]string{0.5: "slower", 2: "faster"}[speed], func(t *testing.T) {
			clock := NewManualClock(time.Unix(1700000000, 0))
			client, runtime := newPacedTestRuntime(t, clock, 1)
			sink := &audioPauseProbe{}
			client.audio = backend.NewAudioWithClock(sink, clock.Now)
			handle, err := client.audio.LoadEvents([]smaf.Event{
				{Type: smaf.EventNoteOn, Note: 60, Velocity: 80},
				{Time: 200, Type: smaf.EventNoteOff, Note: 60},
				{Time: 300, Type: smaf.EventEnd},
			})
			if err != nil {
				t.Fatal(err)
			}
			clock.Advance(900 * time.Millisecond)
			if err := client.audio.PlayCount(handle, runtime.guestElapsed(), 2); err != nil {
				t.Fatal(err)
			}
			client.serviceAudio()
			clock.Advance(100 * time.Millisecond)
			client.serviceAudio()
			client.SetSpeed(speed)
			if err := client.audio.Pause(handle, runtime.guestElapsed()); err != nil {
				t.Fatalf("rate change invalidated a running clip's pause: %v", err)
			}
			saved, err := client.audio.CaptureState()
			if err != nil {
				t.Fatal(err)
			}
			sound := saved.Sounds[0]
			if !sound.Paused || sound.Position != 100*time.Millisecond || sound.Remaining != 1 || sound.Completed != 0 || len(sink.ofType(smaf.EventNoteOff)) != 0 {
				t.Fatalf("rate change consumed the remaining gate or repeat: position=%v, remaining=%d, completed=%d", sound.Position, sound.Remaining, sound.Completed)
			}
			clock.Advance(17 * time.Second)
			client.serviceAudio()
			if err := client.audio.Resume(handle, runtime.guestElapsed()); err != nil {
				t.Fatal(err)
			}
			if len(sink.resumed) != 1 || sink.resumed[0].age != 100*time.Millisecond {
				t.Fatalf("resumed output lost its unscaled envelope age: %+v", sink.resumed)
			}
			sink.events = nil
			advance := func(guest time.Duration) {
				clock.Advance(time.Duration(float64(guest) / speed))
				client.serviceAudio()
			}
			advance(99 * time.Millisecond)
			if len(sink.ofType(smaf.EventNoteOff)) != 0 {
				t.Fatal("resumed gate ended early")
			}
			advance(time.Millisecond)
			if len(sink.ofType(smaf.EventNoteOff)) != 1 {
				t.Fatal("resumed gate did not end at the new rate")
			}
			advance(100 * time.Millisecond)
			if len(sink.ofType(smaf.EventNoteOn)) != 1 {
				t.Fatal("remaining pass did not start at the score boundary")
			}
			advance(300 * time.Millisecond)
			progress, err := client.audio.Playback(handle, runtime.guestElapsed())
			if err != nil || progress.Playing || progress.Completed != 2 || len(sink.ofType(smaf.EventNoteOn)) != 1 || len(sink.ofType(smaf.EventNoteOff)) != 2 {
				t.Fatalf("finite pass count changed after speed and pause: %+v, %v", progress, err)
			}
		})
	}
}

func TestSpeedChangeRebasesActiveAndPreparedAtOneInstant(t *testing.T) {
	for _, mode := range []string{"same runtime", "distinct runtime", "prepared only"} {
		t.Run(mode, func(t *testing.T) {
			clock := &speedChangeClock{ManualClock: NewManualClock(time.Unix(1700000000, 0))}
			client, runtime := newPacedTestRuntime(t, clock, 1)
			client.prepared = runtime
			if mode == "distinct runtime" {
				client.prepared = &initializationRuntime{client: client, clockBase: runtime.clockBase.Add(-500 * time.Millisecond), virtualBaseMillis: runtime.virtualBaseMillis}
			} else if mode == "prepared only" {
				client.runtime = nil
			}
			clock.Advance(time.Second)
			active, prepared := runtime.guestElapsed(), client.prepared.guestElapsed()
			activeDate, preparedDate := runtime.guestMillis(), client.prepared.guestMillis()
			reads := clock.reads
			client.SetSpeed(2)
			if clock.reads-reads != 1 {
				t.Fatalf("rate change sampled %d Host instants, want one", clock.reads-reads)
			}
			if runtime.guestElapsed() != active || client.prepared.guestElapsed() != prepared || runtime.guestMillis() != activeDate || client.prepared.guestMillis() != preparedDate {
				t.Fatal("prepared/active clocks jumped or the shared runtime was rebased twice")
			}
			clock.Advance(50 * time.Millisecond)
			if runtime.guestElapsed() != active+100*time.Millisecond || client.prepared.guestElapsed() != prepared+100*time.Millisecond {
				t.Fatal("prepared and active runtimes did not adopt the new rate together")
			}
		})
	}
}

func TestSpeedChangeFractionalRoundingNeverMovesBackward(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	clock.Advance(123456789 * time.Nanosecond)
	for range 4 {
		for _, speed := range []float64{0.1, 16, 0.5, 2, 1.25, 0.7, 1} {
			before, date := runtime.guestElapsed(), runtime.guestMillis()
			client.SetSpeed(speed)
			after := runtime.guestElapsed()
			quantum := time.Duration(math.Ceil(speed)) * time.Nanosecond
			if after < before || after-before > quantum || runtime.guestMillis() < date {
				t.Fatalf("fractional rebase at %gx moved %s -> %s beyond %s", speed, before, after, quantum)
			}
			clock.Advance(37 * time.Nanosecond)
			step := runtime.guestElapsed() - after
			want := time.Duration(37 * speed)
			if step < want || step > want+time.Nanosecond {
				t.Fatalf("fractional new rate %gx advanced %s, want %s within 1ns", speed, step, want)
			}
		}
	}
}

func TestSpeedChangeClampedAndRepeatedRatesPreserveTime(t *testing.T) {
	clock := NewManualClock(time.Unix(1700000000, 0))
	client, runtime := newPacedTestRuntime(t, clock, 1)
	clock.Advance(time.Second)
	for _, test := range []struct{ request, want float64 }{
		{0.01, 0.1}, {1000, 16}, {0, 1}, {-3, 1}, {1, 1},
		{math.Inf(1), 16}, {math.Inf(-1), 1}, {math.NaN(), 1},
	} {
		elapsed, date, base := runtime.guestElapsed(), runtime.guestMillis(), runtime.clockBase
		previous := client.Speed()
		client.SetSpeed(test.request)
		if client.Speed() != test.want || runtime.guestElapsed() != elapsed || runtime.guestMillis() != date {
			t.Fatalf("request %g changed time or selected the wrong rate: speed=%g, elapsed=%s", test.request, client.Speed(), runtime.guestElapsed())
		}
		if previous == test.want && !runtime.clockBase.Equal(base) {
			t.Fatal("repeating the current rate changed its anchor")
		}
		clock.Advance(100 * time.Millisecond)
	}
}

func TestSpeedChangeOverflowRefusesBothAnchorsAtomically(t *testing.T) {
	for _, mode := range []string{"required age", "current scaled age", "prepared required age", "unrepresentable Host age"} {
		t.Run(mode, func(t *testing.T) {
			clock := NewManualClock(time.Unix(1700000000, 0))
			client, runtime := newPacedTestRuntime(t, clock, 1)
			clock.Advance(time.Second)
			prepared := &initializationRuntime{client: client, clockBase: runtime.clockBase, virtualBaseMillis: runtime.virtualBaseMillis}
			client.prepared = prepared
			next := 0.1
			switch mode {
			case "required age":
				runtime.clockBase = clock.Now().Add(-time.Duration(math.MaxInt64 / 2))
			case "current scaled age":
				client.SetSpeed(16)
				runtime.clockBase = clock.Now().Add(-time.Duration(math.MaxInt64 / 8))
				next = 2
			case "prepared required age":
				prepared.clockBase = clock.Now().Add(-time.Duration(math.MaxInt64 / 2))
			case "unrepresentable Host age":
				runtime.clockBase = clock.Now().AddDate(-400, 0, 0)
			}
			before, other, speed := runtime.clockBase, prepared.clockBase, client.Speed()
			client.SetSpeed(next)
			if client.Speed() != speed || !runtime.clockBase.Equal(before) || !prepared.clockBase.Equal(other) {
				t.Fatal("unrepresentable change partially changed the rate or clock anchors")
			}
		})
	}
}

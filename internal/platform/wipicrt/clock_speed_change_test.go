package wipicrt_test

import (
	"math"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
)

type speedChangeClock struct {
	*wipicrt.ManualClock
	reads int
}

func (clock *speedChangeClock) Now() time.Time {
	clock.reads++
	return clock.ManualClock.Now()
}

func TestSpeedChangePreservesGuestTime(t *testing.T) {
	clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
	client, runtime := wipicrttest.NewPacedRuntime(t, clock, 1)
	clock.Advance(time.Second)
	for _, speed := range []float64{0.5, 2, 0.1, 16} {
		elapsed, millis := runtime.GuestElapsed(), runtime.GuestMillis()
		client.SetSpeed(speed)
		if got, date := runtime.GuestElapsed(), runtime.GuestMillis(); got != elapsed || date != millis {
			t.Fatalf("changing to %gx moved guest time: elapsed %s -> %s, date %d -> %d", speed, elapsed, got, millis, date)
		}
		clock.Advance(100 * time.Millisecond)
		want := elapsed + time.Duration(float64(100*time.Millisecond)*speed)
		if got, date := runtime.GuestElapsed(), runtime.GuestMillis(); got != want || date != millis+int64((want-elapsed)/time.Millisecond) {
			t.Fatalf("%gx did not advance at its new rate: elapsed %s, want %s; date %d", speed, got, want, date)
		}
	}
}

func TestSpeedChangeRebasesActiveAndPreparedAtOneInstant(t *testing.T) {
	for _, mode := range []string{"same runtime", "distinct runtime", "prepared only"} {
		t.Run(mode, func(t *testing.T) {
			clock := &speedChangeClock{ManualClock: wipicrt.NewManualClock(time.Unix(1700000000, 0))}
			client, runtime := wipicrttest.NewPacedRuntime(t, clock, 1)
			client.Prepared = runtime
			if mode == "distinct runtime" {
				client.Prepared = &wipicrt.Runtime{Client: client, ClockBase: runtime.ClockBase.Add(-500 * time.Millisecond), VirtualBaseMillis: runtime.VirtualBaseMillis}
			} else if mode == "prepared only" {
				client.Runtime = nil
			}
			clock.Advance(time.Second)
			active, prepared := runtime.GuestElapsed(), client.Prepared.GuestElapsed()
			activeDate, preparedDate := runtime.GuestMillis(), client.Prepared.GuestMillis()
			reads := clock.reads
			client.SetSpeed(2)
			if clock.reads-reads != 1 {
				t.Fatalf("rate change sampled %d Host instants, want one", clock.reads-reads)
			}
			if runtime.GuestElapsed() != active || client.Prepared.GuestElapsed() != prepared || runtime.GuestMillis() != activeDate || client.Prepared.GuestMillis() != preparedDate {
				t.Fatal("prepared/active clocks jumped or the shared runtime was rebased twice")
			}
			clock.Advance(50 * time.Millisecond)
			if runtime.GuestElapsed() != active+100*time.Millisecond || client.Prepared.GuestElapsed() != prepared+100*time.Millisecond {
				t.Fatal("prepared and active runtimes did not adopt the new rate together")
			}
		})
	}
}

func TestSpeedChangeFractionalRoundingNeverMovesBackward(t *testing.T) {
	clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
	client, runtime := wipicrttest.NewPacedRuntime(t, clock, 1)
	clock.Advance(123456789 * time.Nanosecond)
	for range 4 {
		for _, speed := range []float64{0.1, 16, 0.5, 2, 1.25, 0.7, 1} {
			before, date := runtime.GuestElapsed(), runtime.GuestMillis()
			client.SetSpeed(speed)
			after := runtime.GuestElapsed()
			quantum := time.Duration(math.Ceil(speed)) * time.Nanosecond
			if after < before || after-before > quantum || runtime.GuestMillis() < date {
				t.Fatalf("fractional rebase at %gx moved %s -> %s beyond %s", speed, before, after, quantum)
			}
			clock.Advance(37 * time.Nanosecond)
			step := runtime.GuestElapsed() - after
			want := time.Duration(37 * speed)
			if step < want || step > want+time.Nanosecond {
				t.Fatalf("fractional new rate %gx advanced %s, want %s within 1ns", speed, step, want)
			}
		}
	}
}

func TestSpeedChangeClampedAndRepeatedRatesPreserveTime(t *testing.T) {
	clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
	client, runtime := wipicrttest.NewPacedRuntime(t, clock, 1)
	clock.Advance(time.Second)
	for _, test := range []struct{ request, want float64 }{
		{0.01, 0.1}, {1000, 16}, {0, 1}, {-3, 1}, {1, 1},
		{math.Inf(1), 16}, {math.Inf(-1), 1}, {math.NaN(), 1},
	} {
		elapsed, date, base := runtime.GuestElapsed(), runtime.GuestMillis(), runtime.ClockBase
		previous := client.Speed()
		client.SetSpeed(test.request)
		if client.Speed() != test.want || runtime.GuestElapsed() != elapsed || runtime.GuestMillis() != date {
			t.Fatalf("request %g changed time or selected the wrong rate: speed=%g, elapsed=%s", test.request, client.Speed(), runtime.GuestElapsed())
		}
		if previous == test.want && !runtime.ClockBase.Equal(base) {
			t.Fatal("repeating the current rate changed its anchor")
		}
		clock.Advance(100 * time.Millisecond)
	}
}

func TestSpeedChangeOverflowRefusesBothAnchorsAtomically(t *testing.T) {
	for _, mode := range []string{"required age", "current scaled age", "prepared required age", "unrepresentable Host age"} {
		t.Run(mode, func(t *testing.T) {
			clock := wipicrt.NewManualClock(time.Unix(1700000000, 0))
			client, runtime := wipicrttest.NewPacedRuntime(t, clock, 1)
			clock.Advance(time.Second)
			prepared := &wipicrt.Runtime{Client: client, ClockBase: runtime.ClockBase, VirtualBaseMillis: runtime.VirtualBaseMillis}
			client.Prepared = prepared
			next := 0.1
			switch mode {
			case "required age":
				runtime.ClockBase = clock.Now().Add(-time.Duration(math.MaxInt64 / 2))
			case "current scaled age":
				client.SetSpeed(16)
				runtime.ClockBase = clock.Now().Add(-time.Duration(math.MaxInt64 / 8))
				next = 2
			case "prepared required age":
				prepared.ClockBase = clock.Now().Add(-time.Duration(math.MaxInt64 / 2))
			case "unrepresentable Host age":
				runtime.ClockBase = clock.Now().AddDate(-400, 0, 0)
			}
			before, other, speed := runtime.ClockBase, prepared.ClockBase, client.Speed()
			client.SetSpeed(next)
			if client.Speed() != speed || !runtime.ClockBase.Equal(before) || !prepared.ClockBase.Equal(other) {
				t.Fatal("unrepresentable change partially changed the rate or clock anchors")
			}
		})
	}
}

package session

import (
	"math"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

func startSpeedChangeFixture(t *testing.T, speed float64) (*Session, *ktf.ManualClock) {
	t.Helper()
	archive, err := testfixture.KTFCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	clock := ktf.NewManualClock(time.Unix(1700000000, 0))
	running, err := Start(t.Context(), archive, Options{SaveStore: store, Clock: clock, Speed: speed})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(running.Close)
	if running.ktf == nil {
		t.Fatal("speed fixture did not start the KTF Java runtime")
	}
	return running, clock
}

func TestSpeedChangeRefusalPreservesPublicAndRepeatRate(t *testing.T) {
	running, clock := startSpeedChangeFixture(t, 1)
	if err := running.SendKey(t.Context(), KeyPress, '5'); err != nil {
		t.Fatal(err)
	}
	// This elapsed time fits a duration at 1x, but retaining it at 0.1x
	// would require a clock origin more than a duration into the past.
	clock.Advance(50 * 365 * 24 * time.Hour)
	before := running.ktf.GuestElapsed()
	running.SetSpeed(0.1)
	if got := running.ktf.Speed(); got != 1 {
		t.Fatalf("unrepresentable speed change was accepted: %v", got)
	}
	if got := running.ktf.GuestElapsed(); got != before {
		t.Errorf("refused speed change moved guest time: got %v, want %v", got, before)
	}
	if got, want := running.Speed(), running.ktf.Speed(); got != want {
		t.Errorf("public speed differs from accepted guest speed: got %v, want %v", got, want)
	}

	// Exercise the input clock without running fifty years of overdue guest
	// work: a held key must still repeat at the accepted 1x rate.
	running.lastTick = clock.Now()
	running.repeat.Holding(running.heldKey())
	clock.Advance(599 * time.Millisecond)
	elapsed := running.guestSinceLastTick()
	if elapsed != 599*time.Millisecond {
		t.Errorf("repeat clock used the refused rate: got %v, want 599ms", elapsed)
	}
	if _, due := running.repeat.Due(elapsed); due {
		t.Error("held key repeated before the handset delay")
	}
	clock.Advance(time.Millisecond)
	if code, due := running.repeat.Due(running.guestSinceLastTick()); !due || code != '5' {
		t.Errorf("held key missed the handset delay at the accepted rate: code=%d, due=%t", code, due)
	}
	if _, err := running.CaptureCheckpoint(t.Context()); err != nil {
		t.Errorf("refused speed change prevented checkpoint capture: %v", err)
	}
}

func TestSpeedChangeNormalizesPublicAndRepeatRate(t *testing.T) {
	for _, test := range []struct {
		name      string
		requested float64
		want      float64
	}{
		{name: "NaN selects default", requested: math.NaN(), want: 1},
		{name: "positive infinity clamps", requested: math.Inf(1), want: 16},
		{name: "negative infinity selects default", requested: math.Inf(-1), want: 1},
		{name: "above maximum clamps", requested: 1000, want: 16},
		{name: "below minimum clamps", requested: 0.01, want: 0.1},
		{name: "zero selects default", requested: 0, want: 1},
		{name: "negative selects default", requested: -3, want: 1},
		{name: "ordinary speed", requested: 2, want: 2},
	} {
		for _, mode := range []string{"start", "set"} {
			t.Run(mode+"/"+test.name, func(t *testing.T) {
				initial := test.requested
				if mode == "set" {
					initial = 1
				}
				running, clock := startSpeedChangeFixture(t, initial)
				if mode == "set" {
					running.SetSpeed(test.requested)
				} else if got := running.options.Speed; got != test.want {
					t.Errorf("stored initial speed: got %v, want %v", got, test.want)
				}
				if got := running.Speed(); got != test.want {
					t.Errorf("public speed: got %v, want %v", got, test.want)
				}
				if got := running.ktf.Speed(); got != test.want {
					t.Errorf("guest speed: got %v, want %v", got, test.want)
				}
				running.lastTick = clock.Now()
				clock.Advance(100 * time.Millisecond)
				if got, want := running.guestSinceLastTick(), time.Duration(float64(100*time.Millisecond)*test.want); got != want {
					t.Errorf("repeat clock did not use normalized speed: got %v, want %v", got, want)
				}
				if _, err := running.CaptureCheckpoint(t.Context()); err != nil {
					t.Errorf("normalized speed prevented checkpoint capture: %v", err)
				}
			})
		}
	}
}

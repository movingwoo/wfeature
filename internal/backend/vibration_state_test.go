package backend

import (
	"encoding/json"
	"testing"
	"time"
)

func TestVibrationStateContinuesAtAnotherHostEpoch(t *testing.T) {
	for _, test := range []struct {
		name         string
		milliseconds int
		elapsed      time.Duration
	}{
		{"timed", 1000, 375 * time.Millisecond},
		{"expired", 1000, 3 * time.Second},
		{"indefinite", 0, time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			var source Vibrator
			source.SetClock(func() time.Time { return now })
			source.Vibrate(72, test.milliseconds)
			now = now.Add(test.elapsed)
			saved, err := source.CaptureState()
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			var decoded VibrationState
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			freshNow := time.Unix(1900000000, 0)
			var fresh Vibrator
			fresh.SetClock(func() time.Time { return freshNow })
			if err := fresh.RestoreState(decoded); err != nil {
				t.Fatal(err)
			}
			for _, elapsed := range []time.Duration{0, 400 * time.Millisecond, 600 * time.Millisecond} {
				now, freshNow = now.Add(elapsed), freshNow.Add(elapsed)
				if got, want := fresh.State(), source.State(); got != want {
					t.Fatalf("restored motor = %+v, want %+v", got, want)
				}
			}
			fresh.Stop()
			if got := fresh.State(); got.Request != saved.Request+1 || got.Active() || got.Level != 0 {
				t.Fatalf("stop after restore = %+v", got)
			}
			if source.State().Request != saved.Request {
				t.Fatal("restored motor changed its source")
			}
		})
	}
}

func TestVibrationStateRejectsMalformedRecordWithoutNewRequest(t *testing.T) {
	now := time.Unix(1700000000, 0)
	var vibrator Vibrator
	vibrator.SetClock(func() time.Time { return now })
	vibrator.Vibrate(25, 500)
	saved, err := vibrator.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	want := vibrator.State()
	for _, damage := range []func(*VibrationState){
		func(s *VibrationState) { s.Version++ },
		func(s *VibrationState) { s.Level = 101 },
		func(s *VibrationState) { s.Duration = -1 },
		func(s *VibrationState) { s.DeadlineSet = false },
		func(s *VibrationState) { s.Offset = s.Duration + 1 },
		func(s *VibrationState) { s.Request = 0 },
	} {
		bad := saved
		damage(&bad)
		if err := vibrator.RestoreState(bad); err == nil || vibrator.State() != want {
			t.Fatal("malformed motor record changed its target")
		}
	}
}

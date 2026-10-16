package route

import (
	"context"
	"errors"
	"testing"
)

// A hold that is cut short — the route's tick budget runs out, the guest
// stalls, the Host fails a tick — still lets go of the key. The session stays
// open after a route that did not arrive, and whatever is driven next would
// otherwise run with the key stuck down.
func TestRunnerReleasesAKeyWhoseHoldWasCutShort(t *testing.T) {
	failure := errors.New("the tick failed")
	for _, testCase := range []struct {
		name    string
		advance func(context.Context) (bool, error)
		stalled func() bool
		wantErr error
	}{
		{name: "budget", advance: func(context.Context) (bool, error) { return true, nil }},
		{name: "stall", advance: func(context.Context) (bool, error) { return false, nil }, stalled: func() bool { return true }},
		{name: "error", advance: func(context.Context) (bool, error) { return false, failure }, wantErr: failure},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			held := map[int32]bool{}
			runner := &Runner{
				MaxTicks: 2,
				Hold:     5,
				Digest:   func() uint64 { return 0 },
				Advance:  testCase.advance,
				Stalled:  testCase.stalled,
				SendKey: func(_ context.Context, pressed bool, key int32) error {
					held[key] = pressed
					return nil
				},
			}
			script, err := parseForTest("key fire\n")
			if err != nil {
				t.Fatal(err)
			}
			result, err := runner.Run(context.Background(), script)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Run() error = %v, want %v", err, testCase.wantErr)
			}
			if err == nil && result.Completed {
				t.Fatal("the route completed although its hold was cut short")
			}
			if held[-5] {
				t.Fatal("the key is still held after the route stopped")
			}
		})
	}
}

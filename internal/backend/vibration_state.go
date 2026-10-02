package backend

import (
	"fmt"
	"time"
)

// VibrationState records a motor request relative to the motor's own Host
// clock. It does not scale with guest playback speed or drive an output device.
type VibrationState struct {
	Version     uint32
	Request     uint64
	Level       int
	Duration    time.Duration
	DeadlineSet bool
	Offset      time.Duration
}

// Validate checks a detached record before its owning session is adopted.
func (saved VibrationState) Validate() error {
	if saved.Version != 1 || saved.Level < 0 || saved.Level > vibrationMaxLevel || saved.Duration < 0 ||
		saved.DeadlineSet != (saved.Level > 0 && saved.Duration > 0) || !saved.DeadlineSet && saved.Offset != 0 ||
		saved.DeadlineSet && saved.Offset > saved.Duration || saved.Request == 0 && (saved.Level != 0 || saved.Duration != 0) {
		return fmt.Errorf("vibration state has an invalid version, request or deadline")
	}
	return nil
}

// CaptureState retains the request counter without issuing another request.
// Negative offsets retain expired deadlines. The owner serializes this with
// the rest of a session; the mutex protects the motor record itself.
func (v *Vibrator) CaptureState() (VibrationState, error) {
	saved := VibrationState{Version: 1}
	if v == nil {
		return saved, nil
	}
	v.mutex.Lock()
	defer v.mutex.Unlock()
	saved.Request, saved.Level, saved.Duration = v.request, v.level, v.duration
	saved.DeadlineSet = !v.deadline.IsZero()
	if saved.DeadlineSet {
		now := v.clock()
		saved.Offset = v.deadline.Sub(now)
		if !now.Add(saved.Offset).Equal(v.deadline) {
			return VibrationState{}, fmt.Errorf("vibration deadline exceeds duration range")
		}
	}
	return saved, saved.Validate()
}

// RestoreState reanchors the saved request to this motor's Host clock. The
// caller must reset Host output epochs separately so an older request number
// is observed after a session replacement.
func (v *Vibrator) RestoreState(saved VibrationState) error {
	if v == nil {
		return fmt.Errorf("cannot restore a nil vibrator")
	}
	if err := saved.Validate(); err != nil {
		return err
	}
	v.mutex.Lock()
	defer v.mutex.Unlock()
	deadline := time.Time{}
	if saved.DeadlineSet {
		deadline = v.clock().Add(saved.Offset)
	}
	v.request, v.level, v.duration, v.deadline = saved.Request, saved.Level, saved.Duration, deadline
	return nil
}

package backend

import (
	"fmt"
	"math"
	"time"
)

func (output *audioOutput) eventTime() time.Time {
	if output.eventAt != nil {
		return *output.eventAt
	}
	return output.currentTime()
}

func (output *audioOutput) timeAt(host time.Time) time.Time {
	if !output.clockSet {
		return host
	}
	return output.clockAt.Add(host.Sub(output.hostAt))
}

func (output *audioOutput) currentTime() time.Time { return output.timeAt(output.now()) }

// The presentation clock is authoritative at service boundaries. Between
// them, samples and envelopes continue on the unscaled Host clock. Keeping a
// stable presentation coordinate makes a manual or virtual guest jump produce
// the same retained output whether it is serviced once or at every deadline.
func (audio *Audio) advanceOutputClock(now time.Duration) time.Time {
	output := audio.sink
	host := output.now()
	if !output.clockSet {
		output.clockAt, output.clockSet = host, true
	} else {
		floor := output.timeAt(host)
		rate := audio.timing.rate
		if rate == 0 {
			rate = 1
		}
		seconds := max(now-audio.timing.current, 0).Seconds() / rate
		whole, fraction := math.Modf(seconds)
		output.clockAt = time.Unix(output.clockAt.Unix()+int64(whole), int64(output.clockAt.Nanosecond())+int64(fraction*1e9))
		// A slow guest cannot make an envelope younger than it already was
		// on the running output device. A fast virtual clock still advances
		// by its full presentation interval when the Host clock stands still.
		if output.clockAt.Before(floor) {
			output.clockAt = floor
		}
	}
	output.hostAt = host
	return output.clockAt
}

// Samples and envelopes run in real seconds, independently of score speed.
// A batched score event already has an age at the service boundary; recording
// it as newly started there would resurrect expired drums and sample prefixes
// when the Host reconstructs output after a dropped or late batch.
func (audio *Audio) outputDeadline(deadline, now time.Duration, host time.Time) {
	rate := audio.timing.rate
	if rate == 0 {
		rate = 1
	}
	delay := float64(now-deadline) / rate
	age := time.Duration(math.MaxInt64)
	if delay < float64(math.MaxInt64) {
		age = time.Duration(delay)
	}
	at := host.Add(-age)
	audio.sink.eventAt = &at
	audio.outputTime(deadline)
}

// TimedAudioSink receives the presentation time of subsequent output calls in
// seconds. The clock is local to one playback timeline, monotonic across rate
// changes, and independent of a Host's wall-clock epoch. Calls are serialized
// with every other sink operation under Audio.mutex. Older sinks may omit it.
type TimedAudioSink interface {
	AudioTime(seconds float64)
}

type audioTiming struct {
	guestAt time.Duration
	at      float64
	rate    float64
	current time.Duration
}

func (timing audioTiming) timeAt(guest time.Duration) float64 {
	rate := timing.rate
	if rate == 0 {
		rate = 1
	}
	return timing.at + (guest-timing.guestAt).Seconds()/rate
}

func (audio *Audio) outputTime(guest time.Duration) {
	if sink, ok := audio.sink.sink.(TimedAudioSink); ok {
		sink.AudioTime(audio.timing.timeAt(guest))
	}
}

func (audio *Audio) currentOutputTime(guest time.Duration) {
	audio.timing.current = guest
	audio.outputTime(guest)
}

// SetPlaybackRate services the old rate through now before changing the rate
// of future score intervals. Platforms use the same guest instant for their
// own speed change and reconcile any completion notifications at that boundary.
// Pitch, sample rate and envelope lengths remain unscaled.
func (audio *Audio) SetPlaybackRate(now time.Duration, rate float64) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	if now < 0 || now < audio.timing.current {
		return fmt.Errorf("audio rate clock moved backward")
	}
	audio.advanceSounds(now)
	audio.timing.at = audio.timing.timeAt(now)
	audio.timing.guestAt, audio.timing.rate = now, ClampSpeed(rate)
	audio.currentOutputTime(now)
	return nil
}

// RebasePlaybackClock binds a newly restored timeline to its saved guest clock.
// Presentation time starts at zero on the new output epoch; score cursors and
// retained output are unchanged. It must run before attaching/replaying output.
func (audio *Audio) RebasePlaybackClock(now time.Duration, rate float64) error {
	if audio == nil {
		return fmt.Errorf("audio is not configured")
	}
	audio.mutex.Lock()
	defer audio.mutex.Unlock()
	if now < 0 {
		return fmt.Errorf("audio playback clock is negative")
	}
	audio.timing = audioTiming{guestAt: now, rate: ClampSpeed(rate), current: now}
	host := audio.sink.now()
	audio.sink.clockAt, audio.sink.hostAt, audio.sink.clockSet = audio.sink.timeAt(host), host, true
	return nil
}

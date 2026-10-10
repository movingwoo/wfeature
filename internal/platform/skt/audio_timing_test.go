package skt

import (
	"math"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type sktAudioTimingGainProbe struct {
	sktGainSink
	at, gainAt float64
	changes    int
}

func (sink *sktAudioTimingGainProbe) AudioTime(at float64) { sink.at = at }

func (sink *sktAudioTimingGainProbe) SoundGain(sound backend.AudioHandle, gain uint16) {
	sink.sktGainSink.SoundGain(sound, gain)
	sink.gainAt = sink.at
	sink.changes++
}

func TestSKTAudioTimingWIPIClipVolumeUsesCommandTime(t *testing.T) {
	for _, descriptor := range []string{"(I)Z", "(I)V"} {
		t.Run(descriptor, func(t *testing.T) {
			fixture := newWIPIListenerFixture(t)
			fixture.begin(false)
			fixture.runtime.AdvanceAudio()
			sink := &sktAudioTimingGainProbe{}
			fixture.runtime.AttachAudioSink(sink)
			changes := sink.changes
			// The guest changes its clip level between Host ticks. Both public
			// signatures must stamp the call's clock, not the previous tick.
			fixture.clock.advance(100 * time.Millisecond)
			if _, err := fixture.runtime.VM.InvokeVirtual(fixture.clip(), "setVolume", descriptor, jvm.IntValue(25)); err != nil {
				t.Fatal(err)
			}
			if sink.changes != changes+1 || math.Abs(sink.gainAt-0.1) > 1e-12 {
				t.Fatalf("clip gain changes = %d at %g; want one change at 0.1", sink.changes-changes, sink.gainAt)
			}
		})
	}
}

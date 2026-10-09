package backend

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

func TestAudioDecodeLimitReportsCauseWithoutChangingLoadedSound(t *testing.T) {
	now := time.Unix(1000, 0)
	audio := NewAudioWithClock(&ownedAudioProbe{}, func() time.Time { return now })
	valid := oneNoteSMAF()
	handle, err := audio.Load(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := audio.Play(handle, 0, false); err != nil {
		t.Fatal(err)
	}
	audio.Advance(20 * time.Millisecond)
	before, err := audio.CaptureState()
	if err != nil {
		t.Fatal(err)
	}

	// A valid first track must not turn a later excessive declaration into a
	// usable partial sound. No compressed payload or large allocation is needed.
	track := append([]byte{1, 0, 0, 0}, make([]byte, 16)...)
	track = append(track, taggedChunk("Mtsq", []byte{255, 255, 255, 255})...)
	body := append([]byte(nil), valid[8:len(valid)-2]...)
	bad := smafFile(append(body, taggedChunk("MTR\x01", track)...))
	if events, err := smaf.Decode(bad); events != nil || !errors.Is(err, smaf.ErrResourceLimit) {
		t.Fatalf("Decode = %d events, %v; want resource refusal", len(events), err)
	}
	if rejected, err := audio.Load(bad); rejected != 0 || !errors.Is(err, smaf.ErrResourceLimit) {
		t.Fatalf("Load = %d, %v; want resource refusal", rejected, err)
	}
	after, err := audio.CaptureState()
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("refused decode changed active sound state: %v", err)
	}
	if next, err := audio.Load(valid); err != nil || next != handle+1 {
		t.Fatalf("refused decode consumed a handle: next=%d, err=%v", next, err)
	}
}

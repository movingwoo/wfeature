package lgt

import "testing"

// Volume.get answers the system level: the loudest until something sets it,
// as on KTF, then what Volume.set — or the C MC_mdaSetVolume, the same level —
// left it at, clamped to 0..100.
func TestJavaVolumeGetAnswersTheSystemLevel(t *testing.T) {
	const (
		get = "org/kwis/msp/media/Volume.get()I"
		set = "org/kwis/msp/media/Volume.set(I)V"
	)
	client := fixtureClient(t)
	if got := mustJavaCall(t, client, get); got != 100 {
		t.Errorf("Volume.get() before any set = %d, want 100", got)
	}
	for level, want := range map[uint32]uint32{40: 40, 0: 0, 150: 100, 0xffffffff: 0} {
		mustJavaCall(t, client, set, level)
		if got := mustJavaCall(t, client, get); got != want {
			t.Errorf("Volume.get() after Volume.set(%d) = %d, want %d", int32(level), got, want)
		}
	}
	callSlot(t, client, slotSetVolume, 25)
	if got := mustJavaCall(t, client, get); got != 25 {
		t.Errorf("Volume.get() after MC_mdaSetVolume(25) = %d, want 25", got)
	}
}

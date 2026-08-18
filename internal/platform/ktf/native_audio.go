package ktf

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// The title plays its music through an interface it queries by number and
// hands a block of memory to. What that block is settles what the interface
// is: dumping it at the call finds
//
//	4d 4d 4d 44 00 00 00 c6 43 4e 54 49 ... 53 54 3a 75 6e 74 69 74 6c 65 64
//
// "MMMD", a length, a `CNTI` chunk and `ST:untitled,VN:PsmPlayer V5` — a
// **SMAF** file, which is the same format the descriptor package's titles keep
// their sound in and which this project already decodes. So the earlier
// package's audio needed no new decoder, only the three calls around it.
//
// The three came off the module: one sets the clip, the next starts it, and a
// third one elsewhere stops it. What says they are those three is the flag the
// module keeps beside them — it sets "sounding" after the second and clears it
// in the function its own listener calls when the platform reports the end.
const (
	// nativeSoundSetClip takes a kind and a pointer to a SMAF file.
	nativeSoundSetClip = 0x0c
	// nativeSoundPlay starts what was set.
	nativeSoundPlay = 0x10
	// nativeSoundStop ends it early.
	nativeSoundStop = 0x14
	// nativeSoundAddReference is the count every object on this surface
	// carries.
	nativeSoundAddReference = 0x00
)

// nativeSoundFinished is what the module's own listener recognises as the end
// of a clip: its first argument is 1 and its second is 14. Reading that test
// is the only evidence for the pair — the module compares and does nothing
// else with them.
const (
	nativeSoundEventKind = 1
	nativeSoundEventEnd  = 14
)

// A SMAF file names its own length: four bytes of magic, four of a big-endian
// size, and the size covers everything after it. The call that sets a clip
// passes no length, so this is how one is known.
const (
	smafHeaderSize = 8
	maxNativeClip  = 4 << 20
)

// installSound registers the player, if there is anywhere for sound to go.
func (platform *NativePlatform) installSound() {
	surface := nativeInterfaceSurface(nativeInterfaceSound)
	platform.client.Serve(surface, nativeSoundAddReference, nativeAnswerOne)
	platform.client.Serve(surface, nativeSoundSetClip, platform.setClip)
	platform.client.Serve(surface, nativeSoundPlay, platform.playClip)
	platform.client.Serve(surface, nativeSoundStop, platform.stopClip)
}

// AttachAudio gives the platform somewhere to send what the title plays. A nil
// sink is silent, which is what a Host without an audio device wants: the
// title still runs its playback calls and still gets the answers it expects.
func (platform *NativePlatform) AttachAudio(sink backend.AudioSink) {
	platform.audio = backend.NewAudio(sink)
}

// setClip loads the SMAF file the title points at.
func (platform *NativePlatform) setClip(thread *armcore.Thread) (uint32, error) {
	arguments, err := nativeArguments(thread, 3)
	if err != nil {
		return 0, err
	}
	address := arguments[2]
	if address == 0 {
		return 0, nil
	}
	header := make([]byte, smafHeaderSize)
	memory := platform.client.core.Memory()
	if err := memory.Read(address, header); err != nil {
		return 0, fmt.Errorf("read KTF native clip header at %#x: %w", address, err)
	}
	if string(header[:4]) != "MMMD" {
		// A clip in a format this platform does not read is silence rather
		// than a failure: the title has no way to be told, and stopping the
		// run over a sound is worse than not hearing it.
		platform.clipRefusals++
		return 0, nil
	}
	length := uint64(binary.BigEndian.Uint32(header[4:])) + smafHeaderSize
	if length > maxNativeClip {
		platform.clipRefusals++
		return 0, nil
	}
	data := make([]byte, length)
	if err := memory.Read(address, data); err != nil {
		return 0, fmt.Errorf("read KTF native clip at %#x: %w", address, err)
	}
	if platform.audio == nil {
		return 1, nil
	}
	// The title sets a clip far more often than it has distinct ones — a
	// screen that plays the same effect on every hit sets it every time — so
	// what was loaded before is closed rather than left to accumulate.
	if platform.clip != 0 {
		_ = platform.audio.Close(platform.clip)
		platform.clip = 0
	}
	handle, err := platform.audio.Load(data)
	if err != nil {
		// A file this decoder refuses is the same case as an unknown format.
		platform.clipRefusals++
		return 0, nil
	}
	platform.clip = handle
	return 1, nil
}

// playClip starts what was set.
func (platform *NativePlatform) playClip(*armcore.Thread) (uint32, error) {
	if platform.audio == nil || platform.clip == 0 {
		return 1, nil
	}
	if err := platform.audio.Play(platform.clip, platform.guestElapsed(), false); err != nil {
		return 0, fmt.Errorf("play KTF native clip: %w", err)
	}
	platform.sounding = true
	return 1, nil
}

// stopClip ends it early.
func (platform *NativePlatform) stopClip(*armcore.Thread) (uint32, error) {
	if platform.audio == nil {
		return 1, nil
	}
	if platform.clip != 0 {
		platform.audio.Stop(platform.clip)
	}
	platform.sounding = false
	return 1, nil
}

// guestElapsed is the clock the audio timeline runs on, so a Host batching
// ticks through a manual clock hears the same sequence in the same order.
func (platform *NativePlatform) guestElapsed() time.Duration {
	return platform.clock.Now().Sub(platform.started)
}

// advanceSound moves the audio timeline and, when a clip has finished, tells
// the title so. The module keeps a flag it sets when it starts a sound and
// clears only in the function its listener calls, so a platform that never
// reports the end leaves a title believing its music is still playing.
func (platform *NativePlatform) advanceSound(ctx context.Context) error {
	if platform.audio == nil {
		return nil
	}
	platform.audio.Advance(platform.guestElapsed())
	if !platform.sounding || platform.clip == 0 || platform.audio.Playing(platform.clip) {
		return nil
	}
	platform.sounding = false
	listener, ok := platform.listenerFor(nativeInterfaceSound)
	if !ok {
		return nil
	}
	if _, err := platform.Deliver(ctx, listener, nativeSoundEventKind, nativeSoundEventEnd); err != nil {
		return fmt.Errorf("KTF native clip ended: %w", err)
	}
	return nil
}

// ClipRefusals reports clips this platform would not play, which is what says
// a silent title was refused rather than never asked.
func (platform *NativePlatform) ClipRefusals() int { return platform.clipRefusals }

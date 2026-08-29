package lgt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// The media block, at 0x4b0. Its order is the specification's MC_mda* list
// with one entry missing, and four slots pin that alignment independently:
// 0x4b0/0x4b1/0x4b3 are clip create/free/putData at indices 0/1/3, and past
// index 3 every entry sits one slot lower than the specification numbers it —
// 0x4bd is stop (14) and 0x4c1 is the vibrator (18). Both of those were
// established from title behaviour before this file existed, and they bracket
// the volume pair between them, which is what makes 0x4bf and 0x4c0 readable
// as MC_mdaGetVolume and MC_mdaSetVolume rather than a guess.
//
// **The alignment ends at the vibrator.** The next specification entry is
// MC_mdaSetMuteState, and that is not 0x4c2 — the mute pair is at 0x4d1/0x4d2,
// where the argument shape puts it, and 0x4c2 is a function the ordered list
// does not carry at all. Past 0x4c1 this block is a vendor's own arrangement,
// so a slot there is named from what its callers pass and from nothing else.
//
// A clip is a guest record: a game keeps the MC_MdaClip* it was handed and
// passes it back, so the handle has to be an address it can hold. The bytes
// themselves stay on the Host, because the guest never reads them back and a
// megabyte of sound in the guest heap is a megabyte the game cannot have.
const (
	slotClipGetType   uint32 = 0x4b2
	slotClipPutData   uint32 = 0x4b3
	slotClipClearData uint32 = 0x4b6
	slotClipGetVolume uint32 = 0x4b8
	slotClipSetVolume uint32 = 0x4b9
	slotClipPause     uint32 = 0x4bb
	slotClipResume    uint32 = 0x4bc
	slotGetVolume     uint32 = 0x4bf
	slotSetVolume     uint32 = 0x4c0
	// MC_mdaSetWaterMark(clip, percent) is the one media call whose contract is
	// a clip and a 0-100 value that is not a volume: it moves the level at
	// which a streaming clip raises END_OF_DATA or FULL_OF_DATA, it returns
	// void, and its default is 100%. One title calls this slot with (clip,
	// 100) between creating a clip and playing it and does not look at what
	// came back — which is that call exactly, asking for the default. The slot
	// sits past the point where the block still follows the specification's
	// order, so this is read from the argument shape rather than from
	// arithmetic; the specification lists the function in the HAL appendix
	// rather than in the ordered MC_mda* list, which is why the ordering never
	// named it.
	//
	// Nothing here streams — a clip is filled before it plays — so there is no
	// event whose threshold this would move, and accepting it leaves the clip
	// exactly as it was. That is also what the handset does with a request for
	// the value it already holds.
	slotClipSetWaterMark uint32 = 0x4c2
	slotClipAllocPlayer  uint32 = 0x4c5
	slotClipFreePlayer   uint32 = 0x4c6
	// The default volume of a system volume category, asked for by name.
	//
	// The specification's own pair is numbered — MC_mdaGetDefaultVolume(cateID)
	// against MC_MDA_VOLCATE_GENERAL and its nine siblings — and this block is
	// a vendor's arrangement past the vibrator, so the name is what the vendor
	// took. What settles the contract is the caller, and every local module
	// that reaches this slot is the same vendor SDK routine compiled into each
	// of them, byte for byte:
	//
	//	movs r0,#0 / bl backLight   ; leaves 2 in r1 and whatever was in r2
	//	ldr  r0,[pc,#..]            ; the only argument: "GENERAL"
	//	bl   0x4ce
	//	adds r1,r0,#0               ; the answer, straight into
	//	bl   <clamp to 0..100, store as this object's volume>
	//
	// So it takes **one** argument and the answer **is** read. The (2,
	// 0xffffff) that every trace shows after the name are the registers
	// backLight left behind, which is what made this look like a three-argument
	// call whose result nobody wanted. The clamp is the range: 0 to 100, the
	// WIPI volume scale.
	//
	// "GENERAL" is the specification's first system volume category — the set a
	// handset reports through the "DEFAULTVOLUME" system property — and it is
	// the one an ordinary application plays under. Every local caller asks for
	// that one and no other.
	//
	// Answering the system volume is what a handset does: the default volume of
	// the application category is the level MC_mdaGetVolume reports, and a
	// title that reads it, keeps it, and later restores it through
	// MC_mdaSetVolume round-trips. Answering zero, which is what an unknown
	// slot did, told six titles their volume was silence.
	slotGetDefaultVolume uint32 = 0x4ce
	// The last four media slots take a source as their first argument. Two are
	// the specification's MC_mdaSetMuteState and MC_mdaGetMuteState, and the
	// pair in front of them reads as the same thing for volume: one title's
	// startup calls 0x4d0(11), refuses to go on if it answers -1, hands what it
	// got to MC_mdaSetVolume, and then calls 0x4cf(11, level). A getter whose
	// answer is a volume and a setter taking that level back is what those two
	// are, whatever the handset called them. Source 11 is the only one any
	// local title names; sources are kept apart here only so that a title
	// setting one and reading another does not see its own write.
	slotSetSourceVolume uint32 = 0x4cf
	slotGetSourceVolume uint32 = 0x4d0
)

// systemVolumeCategories are the system volume categories the specification
// defines, in the set a handset reports through the "DEFAULTVOLUME" system
// property. They are here to tell a name apart from a name this has never
// seen, not to give each one a volume of its own: this platform mixes through
// one synthesiser and has one level.
var systemVolumeCategories = map[string]bool{
	"GENERAL": true, "VOICE": true, "RING": true, "KEY": true, "MESSAGE": true,
	"ALARM": true, "ALERT": true, "MMEDIA": true, "GAME": true, "OEM": true,
}

// mediaMaxVolume is the loudest a WIPI volume goes; zero is silent. One title
// sets a clip to 50, which is what settles the scale as a percentage rather
// than a handful of steps. Nothing here attenuates by it — the synthesiser's
// own velocities carry the mix — so what the number has to do is come back out
// the way it went in, because a game saves the level it found and restores it.
const mediaMaxVolume = 100

// mediaClipRecordSize is the guest record a clip handle points at. Nothing
// here reads it — the fields live on the Host — but a game that treats the
// handle as a struct pointer and pokes at it must not write over something
// else's memory.
const mediaClipRecordSize = 32

// mediaClip is one MC_MdaClip on the Host side.
type mediaClip struct {
	// mediaType is what MC_mdaClipCreate was asked for, answered back by
	// MC_mdaClipGetType.
	mediaType string
	data      []byte
	volume    int32

	// handle is the loaded sound, claimed on first play and dropped whenever
	// the data changes, so a clip refilled through putData reloads instead of
	// replaying what it held before.
	handle backend.AudioHandle
	loaded bool

	// listener is the `PlayListener` a Java title registered for this clip's
	// state changes, or zero. Nothing is delivered to it yet; see
	// javaClipSetListener for what it would take and why an invented event is
	// worse than the gap.
	listener uint32
}

// handleMedia services the media block. It is split out of handleWIPICSVC
// because the block is a surface of its own; the caller holds client.mu.
func (client *Client) handleMedia(thread *armcore.Thread, slot uint32) error {
	argument := func(index int) (uint32, error) { return thread.Register(index) }
	answer := func(value uint32) error { return thread.SetRegister(0, value) }
	answerInt := func(value int32) error { return thread.SetRegister(0, uint32(value)) }

	switch slot {
	case slotClipCreate:
		return client.createClip(thread)

	case slotClipFree:
		handle, err := argument(0)
		if err != nil {
			return err
		}
		clip := client.clips[handle]
		if clip == nil {
			return answerInt(wipiError)
		}
		client.releaseClipSound(clip)
		delete(client.clips, handle)
		return answerInt(wipiSuccess)

	case slotClipGetType:
		return client.clipType(thread)

	case slotClipPutData:
		return client.putClipData(thread)

	case slotClipClearData:
		clip, err := client.clipArgument(thread)
		if err != nil || clip == nil {
			return answerIfMissing(thread, err)
		}
		client.releaseClipSound(clip)
		clip.data = nil
		return answerInt(wipiSuccess)

	case slotClipGetVolume:
		clip, err := client.clipArgument(thread)
		if err != nil || clip == nil {
			return answerIfMissing(thread, err)
		}
		return answerInt(clip.volume)

	case slotClipSetVolume:
		clip, err := client.clipArgument(thread)
		if err != nil || clip == nil {
			return answerIfMissing(thread, err)
		}
		level, err := argument(1)
		if err != nil {
			return err
		}
		clip.volume = clampVolume(int32(level))
		return answerInt(wipiSuccess)

	case slotClipPlay:
		return client.playClip(thread)

	case slotClipPause, slotClipStop:
		// Pause and stop are the same here. Playback is tracked by clock
		// position rather than by a paused cursor, so resuming restarts the
		// clip; the alternative is a pause that silently keeps playing.
		clip, err := client.clipArgument(thread)
		if err != nil || clip == nil {
			return answerIfMissing(thread, err)
		}
		if clip.loaded && client.audio != nil {
			client.audio.Stop(clip.handle)
		}
		return answerInt(wipiSuccess)

	case slotClipResume:
		return client.playClip(thread)

	case slotGetVolume:
		return answerInt(client.volume)

	case slotGetDefaultVolume:
		address, err := argument(0)
		if err != nil {
			return err
		}
		name, err := client.readCString(address)
		if err != nil {
			return err
		}
		if !systemVolumeCategories[name] && client.logger != nil {
			// There is one volume here, so a category the specification does
			// not name gets the same answer. Saying so is what would surface
			// the caller that makes per-category volumes worth keeping.
			client.logger.Debug("LGT default volume asked for an unlisted category", "category", name)
		}
		return answerInt(client.volume)

	case slotSetVolume:
		level, err := argument(0)
		if err != nil {
			return err
		}
		client.volume = clampVolume(int32(level))
		return answerInt(wipiSuccess)

	case slotVibrator:
		// No vibrator here, and nothing observable is lost by saying so.
		return answerInt(wipiSuccess)

	case slotClipSetWaterMark:
		// The clip is still checked, because a bad handle is worth reporting
		// the same way every other per-clip call reports one. Nothing else
		// happens: see the slot's own note above.
		clip, err := client.clipArgument(thread)
		if err != nil || clip == nil {
			return answerIfMissing(thread, err)
		}
		return answerInt(wipiSuccess)

	case slotClipAllocPlayer, slotClipFreePlayer:
		// A player is the device a clip is bound to. This platform mixes every
		// clip through one sink, so there is no device to hand out; the calls
		// are accepted because a game that cannot get a player stops trying to
		// play anything at all.
		return answerInt(wipiSuccess)

	case slotGetSourceVolume:
		source, err := argument(0)
		if err != nil {
			return err
		}
		if level, set := client.sourceVolume[source]; set {
			return answerInt(level)
		}
		return answerInt(mediaMaxVolume)

	case slotSetSourceVolume:
		source, err := argument(0)
		if err != nil {
			return err
		}
		level, err := argument(1)
		if err != nil {
			return err
		}
		if client.sourceVolume == nil {
			client.sourceVolume = map[uint32]int32{}
		}
		client.sourceVolume[source] = clampVolume(int32(level))
		return answerInt(wipiSuccess)

	case slotSetMuteState:
		source, err := argument(0)
		if err != nil {
			return err
		}
		state, err := argument(1)
		if err != nil {
			return err
		}
		if client.sourceMuted == nil {
			client.sourceMuted = map[uint32]bool{}
		}
		client.sourceMuted[source] = state != 0
		return answerInt(wipiSuccess)

	case slotGetMuteState:
		source, err := argument(0)
		if err != nil {
			return err
		}
		if client.sourceMuted[source] {
			return answer(1)
		}
		return answer(0)
	}
	return fmt.Errorf("unimplemented LGT media slot %#x%s, with %s; %s", slot,
		client.describeJavaCallSite(thread),
		formatWords(registerWords(thread, 4)), client.describeCallWords(thread, 4))
}

// createClip serves MC_mdaClipCreate(type, bufSize, callback).
func (client *Client) createClip(thread *armcore.Thread) error {
	typePointer, err := thread.Register(0)
	if err != nil {
		return err
	}
	mediaType := ""
	if typePointer != 0 {
		if text, readErr := client.readCString(typePointer); readErr == nil {
			mediaType = text
		}
	}
	address, err := client.allocateBytes(make([]byte, mediaClipRecordSize))
	if err != nil {
		return err
	}
	if client.clips == nil {
		client.clips = map[uint32]*mediaClip{}
	}
	client.clips[address] = &mediaClip{mediaType: mediaType, volume: mediaMaxVolume}
	return thread.SetRegister(0, address)
}

// clipType serves MC_mdaClipGetType(clip, buf, bufSize), which answers the
// media type the clip was created with.
func (client *Client) clipType(thread *armcore.Thread) error {
	clip, err := client.clipArgument(thread)
	if err != nil || clip == nil {
		return answerIfMissing(thread, err)
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return err
	}
	size, err := thread.Register(2)
	if err != nil {
		return err
	}
	text := append([]byte(clip.mediaType), 0)
	if buffer == 0 || uint64(len(text)) > uint64(size) {
		return answerCode(thread, wipiShortBuffer)
	}
	if err := client.core.Memory().Write(buffer, text); err != nil {
		return fmt.Errorf("write LGT clip type: %w", err)
	}
	return answerCode(thread, wipiSuccess)
}

// maxClipBytes bounds what one clip can accumulate, because putData is how a
// game streams and nothing here obliges it to stop.
const maxClipBytes = 1 << 20

// putClipData serves MC_mdaClipPutData(clip, buf, size) and answers how many
// bytes the clip took.
func (client *Client) putClipData(thread *armcore.Thread) error {
	clip, err := client.clipArgument(thread)
	if err != nil || clip == nil {
		return answerIfMissing(thread, err)
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return err
	}
	size, err := thread.Register(2)
	if err != nil {
		return err
	}
	if buffer == 0 || int32(size) <= 0 {
		return thread.SetRegister(0, 0)
	}
	room := maxClipBytes - len(clip.data)
	if room <= 0 {
		return thread.SetRegister(0, 0)
	}
	if int(size) > room {
		size = uint32(room)
	}
	data := make([]byte, size)
	if err := client.core.Memory().Read(buffer, data); err != nil {
		return fmt.Errorf("read LGT clip data at %#x: %w", buffer, err)
	}
	client.releaseClipSound(clip)
	clip.data = append(clip.data, data...)
	return thread.SetRegister(0, size)
}

// playClip serves MC_mdaPlay(clip, repeat). A clip whose bytes this build
// cannot decode answers failure rather than stopping the game: silence is what
// a handset without that codec would give, and the game's own path handles it.
func (client *Client) playClip(thread *armcore.Thread) error {
	clip, err := client.clipArgument(thread)
	if err != nil || clip == nil {
		return answerIfMissing(thread, err)
	}
	repeat, err := thread.Register(1)
	if err != nil {
		return err
	}
	if client.audio == nil || len(clip.data) == 0 {
		return answerCode(thread, wipiError)
	}
	if !clip.loaded {
		handle, loadErr := client.audio.Load(clip.data)
		if loadErr != nil {
			if client.logger != nil {
				client.logger.Debug("LGT clip cannot be decoded", "type", clip.mediaType, "bytes", len(clip.data), "error", loadErr)
			}
			return answerCode(thread, wipiError)
		}
		clip.handle, clip.loaded = handle, true
	}
	if err := client.audio.Play(clip.handle, client.clock.now(), repeat != 0); err != nil {
		if client.logger != nil {
			client.logger.Debug("LGT clip cannot be played", "error", err)
		}
		return answerCode(thread, wipiError)
	}
	return answerCode(thread, wipiSuccess)
}

// clipArgument reads r0 as a clip handle. A handle nobody created answers nil,
// which every caller turns into the WIPI error code.
func (client *Client) clipArgument(thread *armcore.Thread) (*mediaClip, error) {
	handle, err := thread.Register(0)
	if err != nil {
		return nil, err
	}
	return client.clips[handle], nil
}

func answerIfMissing(thread *armcore.Thread, err error) error {
	if err != nil {
		return err
	}
	return answerCode(thread, wipiError)
}

func (client *Client) releaseClipSound(clip *mediaClip) {
	if clip.loaded && client.audio != nil {
		_ = client.audio.Close(clip.handle)
	}
	clip.loaded = false
}

func clampVolume(level int32) int32 {
	switch {
	case level < 0:
		return 0
	case level > mediaMaxVolume:
		return mediaMaxVolume
	}
	return level
}

// serviceAudio moves playback to where the guest's clock now is. It runs after
// a tick rather than inside the media calls, because a game starts a sound and
// then does nothing about it: without this nothing after the first event would
// ever sound.
func (client *Client) serviceAudio() {
	if client == nil || client.audio == nil {
		return
	}
	client.audio.Advance(client.clock.now())
}

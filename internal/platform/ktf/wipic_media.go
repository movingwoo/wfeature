package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The WIPI C media block, table 10. It is a second sound surface beside the
// Java one in runtime_media.go, and a title uses one or the other: a Clet that
// keeps its sound in C never constructs an `org.kwis.msp.media.Clip`, so a
// runtime that answers only the Java classes leaves it silent while every one
// of its calls is accepted and thrown away.
//
// **The function numbers are read off callers, not off the specification's
// print order.** The specification lists twenty-one `MC_mda*` functions and
// this block does not follow that list — the entries below sit at 0, 4, 8, 11,
// 15, 16 and 17, where the printed order would put them at 0, 3, 11, 14, 17,
// 18 and 19. What settles each one is the argument shape and the sequence a
// title calls them in:
//
//	0  (mType, bufSize, cb)  r0 is "Yamaha_MA2" or "Yamaha_MA3" and r1 is the
//	                         byte count of the sound about to be loaded, which
//	                         is MC_mdaClipCreate and nothing else.
//	4  (clip, buf, size)     r1 points at "MMMD" — the SMAF magic — and r2 is
//	                         the size the create asked for: MC_mdaClipPutData.
//	8  (clip, repeat)        called immediately after 4, once per clip, with
//	                         r1 alternating 0 and 1: MC_mdaPlay(clip, repeat).
//	11 (clip)                the first of the three calls that end a clip, and
//	                         in four other titles the run is 11, then 7, then 3
//	                         on the same clip before the next one is created.
//	                         Stop, clear, free is what a title does with a
//	                         sound it has finished with, and play, pause,
//	                         resume, stop in the run 8..11 puts stop first.
//	7  (clip)                the middle of that run: MC_mdaClipClearData.
//	3  (clip)                the last of it, after which the handle is never
//	                         named again: MC_mdaClipFree.
//	15 (level)               r0 walks 5, 10, 15 … 100 across a fade and takes
//	                         no second argument: MC_mdaSetVolume.
//	16 (level, timeout)      (20, 50) and (100, 50) during a fight, and (0, 0)
//	                         from a title that wants it off: MC_mdaVibrator.
//	17 (source, bmute)       (3, 1) once at startup: MC_mdaSetMuteState.
//
// One more is left alone deliberately. **26 takes (clip, 45) or (clip, 60)
// between a putData and its play**, which is either a per-clip volume or the
// water mark a streaming clip raises its callback at. Nothing here has a
// per-sound gain and nothing here streams, so both readings come out as the
// same accepted no-op, and choosing between them on this evidence would only
// put a guess in the record. Everything else in the block stays an accepted
// no-op too, because nothing has shown what it is. That is the same rule the rest of the WIPI C tables follow:
// a number answered from the specification's ordering alone is a value a game
// will believe.
//
// A clip is a guest record so the handle is an address the game can hold. The
// bytes stay on the Host — the guest never reads them back, and a megabyte of
// sound in the guest arena is a megabyte the game cannot have — which is the
// same split the Java Clip already uses.
const (
	wipicMediaClipCreate    = 0
	wipicMediaClipFree      = 3
	wipicMediaClipPutData   = 4
	wipicMediaClipClearData = 7
	wipicMediaPlay          = 8
	wipicMediaStop          = 11
	wipicMediaSetVolume     = 15
	wipicMediaVibrator      = 16
	wipicMediaSetMuteState  = 17
)

// wipicMediaClipRecordSize is the guest record a clip handle points at.
// Nothing here reads it — the fields live on the Host — but a game that treats
// the handle as a struct pointer and pokes at it must not land on something
// else's memory.
const wipicMediaClipRecordSize = 32

// maxWIPICMediaClips bounds how many clips keep their bytes. A title that
// creates one clip per sound effect would otherwise grow this for as long as it
// is played, and the guest chooses both the count and each clip's size. The
// oldest clip's data is dropped when the limit is reached; its record stays
// valid and answers as an empty clip, which is what a handset out of audio
// memory would give.
const maxWIPICMediaClips = 64

// wipicMediaClip is one MC_MdaClip on the Host side.
type wipicMediaClip struct {
	// mediaType is what MC_mdaClipCreate was asked for. It is kept for the
	// diagnostic that names a sound this build could not decode: "Yamaha_MA2"
	// and "Yamaha_MA3" are both SMAF, and a third name appearing there is what
	// would say a new codec is wanted.
	mediaType string
	state     clipState
}

// handleWIPICMediaCall services the media table.
func (runtime *initializationRuntime) handleWIPICMediaCall(thread *armcore.Thread, function uint32) (uint32, error) {
	switch function {
	case wipicMediaClipCreate:
		return runtime.wipicCreateClip(thread)

	case wipicMediaClipPutData:
		return runtime.wipicPutClipData(thread)

	case wipicMediaPlay:
		return runtime.wipicPlayClip(thread)

	case wipicMediaStop:
		clip, err := runtime.wipicClipArgument(thread)
		if err != nil {
			return 0, err
		}
		// A handle nobody created has nothing sounding under it, which is what
		// the caller asked for; one local title stops a null clip before it has
		// made any. The calls that would *produce* something — put data, play —
		// answer failure for the same handle, because there the caller is owed
		// the news that nothing will come of it.
		runtime.stopClip(clip)
		return 0, nil

	case wipicMediaClipClearData:
		clip, err := runtime.wipicClipArgument(thread)
		if err != nil {
			return 0, err
		}
		if clip != nil {
			runtime.stopClip(clip)
			runtime.invalidateClip(&clip.state)
			clip.state.data = nil
		}
		return 0, nil

	case wipicMediaClipFree:
		handle, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		runtime.freeWIPICClip(handle)
		return 0, nil

	case wipicMediaSetVolume:
		level, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		// The level is the device's, so it reaches the sound path rather than
		// being remembered and ignored: a title that fades its music out by
		// walking this down to zero is asking to be quiet, and a runtime that
		// keeps playing at full has heard the request and disregarded it. The
		// sound path clamps it to 0..100 and holds it, which is also where a
		// getter would read it back from.
		runtime.client.audio.SetVolume(int(int32(level)))
		return 0, nil

	case wipicMediaVibrator:
		// No vibrator here, and nothing observable is lost by saying so.
		return 0, nil

	case wipicMediaSetMuteState:
		// Accepted and not applied. The argument is a *source* — one of the
		// handset's audio sources — and nothing here says which source a
		// number names. Reading a per-source mute as a global one silences
		// every clip in the game, which is a worse answer than carrying on:
		// the one local caller mutes source 3 once at startup and never asks
		// again, so what it wanted cannot be read back from its behaviour
		// either. A title that turns its sound off through this and is still
		// heard is what would settle it.
		runtime.countDiagnostic("wipic media set mute state accepted")
		return 0, nil

	case 9, 10, 14, 18, 25, 26:
		// The rest of what the local titles reach: accepted and discarded,
		// because the argument shapes seen so far do not name them. See the
		// block comment for why a number is not enough to act on.
		runtime.countDiagnostic(fmt.Sprintf("wipic media function %d accepted", function))
		return 0, nil

	default:
		return 0, fmt.Errorf("KTF WIPI C media function %d is not implemented", function)
	}
}

// wipicCreateClip serves MC_mdaClipCreate(mType, bufSize, cb).
//
// The callback is read and dropped. It is the handset's way of reporting that
// a clip has run out of data or finished, and nothing here has a finish to
// report: playback is tracked by clock position rather than by a device that
// tells the runtime when it stopped. A title that waits for it would wait
// forever, which is the same gap the Java `PlayListener` has, and inventing an
// event is worse than the gap.
func (runtime *initializationRuntime) wipicCreateClip(thread *armcore.Thread) (uint32, error) {
	typePointer, err := thread.Register(0)
	if err != nil {
		return 0, err
	}
	mediaType := ""
	if typePointer != 0 {
		if text, readErr := runtime.readCString(typePointer, 64); readErr == nil {
			mediaType = text
		}
	}
	address, err := runtime.allocate(wipicMediaClipRecordSize)
	if err != nil {
		return 0, err
	}
	if err := runtime.client.core.Memory().Write(address, make([]byte, wipicMediaClipRecordSize)); err != nil {
		return 0, fmt.Errorf("clear KTF media clip record at %#x: %w", address, err)
	}
	if runtime.wipicClips == nil {
		runtime.wipicClips = map[uint32]*wipicMediaClip{}
	}
	runtime.wipicClips[address] = &wipicMediaClip{mediaType: mediaType}
	runtime.wipicClipOrder = append(runtime.wipicClipOrder, address)
	runtime.trimWIPICClips()
	return address, nil
}

// trimWIPICClips drops the bytes of the oldest clips once more than
// maxWIPICMediaClips exist. The record and its entry stay, so a handle the game
// still holds resolves; it simply has nothing to play.
func (runtime *initializationRuntime) trimWIPICClips() {
	for len(runtime.wipicClipOrder) > maxWIPICMediaClips {
		runtime.freeWIPICClip(runtime.wipicClipOrder[0])
	}
}

// freeWIPICClip serves MC_mdaClipFree and is what the trim above reuses.
//
// The guest record is not handed back to the arena. Releasing it would be
// right if this function is a free and a use-after-free if it is not, and what
// says it is a free is a call order rather than a contract: four titles stop,
// clear and then call this, and never name the handle again. Thirty-two bytes
// per sound a title finishes with is the price of not having to be right about
// that; the bytes that actually cost something are the sound's, and those do go.
func (runtime *initializationRuntime) freeWIPICClip(handle uint32) {
	clip := runtime.wipicClips[handle]
	if clip == nil {
		return
	}
	runtime.stopClip(clip)
	runtime.invalidateClip(&clip.state)
	delete(runtime.wipicClips, handle)
	for index, address := range runtime.wipicClipOrder {
		if address == handle {
			runtime.wipicClipOrder = append(runtime.wipicClipOrder[:index], runtime.wipicClipOrder[index+1:]...)
			break
		}
	}
}

// stopClip silences a clip if it is sounding. A nil clip is a handle nobody
// created, and there is nothing to stop.
func (runtime *initializationRuntime) stopClip(clip *wipicMediaClip) {
	if clip == nil || !clip.state.loaded || runtime.client.audio == nil {
		return
	}
	runtime.client.audio.Stop(clip.state.handle)
}

// wipicPutClipData serves MC_mdaClipPutData(clip, buf, size) and answers how
// many bytes the clip took.
func (runtime *initializationRuntime) wipicPutClipData(thread *armcore.Thread) (uint32, error) {
	clip, err := runtime.wipicClipArgument(thread)
	if err != nil {
		return 0, err
	}
	if clip == nil {
		// The answer is a byte count, so a handle nobody created took none of
		// them rather than failing with a count of -1.
		return 0, nil
	}
	buffer, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	size, err := thread.Register(2)
	if err != nil {
		return 0, err
	}
	if buffer == 0 || int32(size) <= 0 {
		return 0, nil
	}
	room := maxClipBufferBytes - len(clip.state.data)
	if room <= 0 {
		return 0, nil
	}
	if int(size) > room {
		size = uint32(room)
	}
	data := make([]byte, size)
	if err := runtime.client.core.Memory().Read(buffer, data); err != nil {
		return 0, fmt.Errorf("read KTF media clip data at %#x: %w", buffer, err)
	}
	// The clip's bytes changed, so whatever was decoded from the old ones is
	// no longer what it holds.
	runtime.invalidateClip(&clip.state)
	clip.state.data = append(clip.state.data, data...)
	return size, nil
}

// wipicPlayClip serves MC_mdaPlay(clip, repeat). A clip this build cannot
// decode answers the failure code rather than stopping the game: silence is
// what a handset without that codec gives, and the game's own path handles it.
func (runtime *initializationRuntime) wipicPlayClip(thread *armcore.Thread) (uint32, error) {
	clip, err := runtime.wipicClipArgument(thread)
	if err != nil {
		return 0, err
	}
	if clip == nil {
		return wipiErrorCode, nil
	}
	repeat, err := thread.Register(1)
	if err != nil {
		return 0, err
	}
	if runtime.client.audio == nil || len(clip.state.data) == 0 {
		return wipiErrorCode, nil
	}
	if !clip.state.loaded {
		handle, loadErr := runtime.client.audio.Load(clip.state.data)
		if loadErr != nil {
			runtime.countDiagnostic(fmt.Sprintf("wipic media clip %q cannot be decoded: %v", clip.mediaType, loadErr))
			return wipiErrorCode, nil
		}
		clip.state.handle, clip.state.loaded = handle, true
	}
	if err := runtime.client.audio.Play(clip.state.handle, runtime.guestElapsed(), repeat != 0); err != nil {
		runtime.countDiagnostic(fmt.Sprintf("wipic media clip cannot be played: %v", err))
		return wipiErrorCode, nil
	}
	return 0, nil
}

// wipicClipArgument reads r0 as a clip handle. A handle nobody created answers
// nil, which every caller turns into the WIPI failure code.
func (runtime *initializationRuntime) wipicClipArgument(thread *armcore.Thread) (*wipicMediaClip, error) {
	handle, err := thread.Register(0)
	if err != nil {
		return nil, err
	}
	return runtime.wipicClips[handle], nil
}

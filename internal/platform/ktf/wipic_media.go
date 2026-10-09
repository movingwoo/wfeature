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
// this block does not follow that list — the entries below sit at 0, 4, 8, 9,
// 10, 11, 15, 16, 17 and 18, where the printed order would put them at 0, 3,
// 11, 12, 13, 14, 17, 18, 19 and 20. What settles each one is the argument
// shape and the sequence a title calls them in:
//
//	0  (mType, bufSize, cb)  r0 is "Yamaha_MA2" or "Yamaha_MA3" and r1 is the
//	                         byte count of the sound about to be loaded, which
//	                         is MC_mdaClipCreate and nothing else.
//	4  (clip, buf, size)     r1 points at "MMMD" — the SMAF magic — and r2 is
//	                         the size the create asked for: MC_mdaClipPutData.
//	8  (clip, repeat)        called immediately after 4, once per clip, with
//	                         r1 alternating 0 and 1: MC_mdaPlay(clip, repeat).
//	9  (clip)                reached only from inside a title's pauseApp, with
//	                         the clip its last 8 played and has not stopped:
//	                         MC_mdaPause.
//	10 (clip)                reached only from inside the matching resumeApp,
//	                         with that same clip: MC_mdaResume. Two titles in
//	                         a lifecycle sweep of the local KTF set make the
//	                         pair, and no title reaches either anywhere else.
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
//	18 (source)              (3) beside 17, from the same titles, and with
//	                         nothing in r1 they set: MC_mdaGetMuteState.
//
// **The clip volume pair sits past the vibrator, at 25 and 26**, and the device
// getter at 14 in front of the setter:
//
//	14 ()                    r0 holds the same platform stub address in every
//	                         title, which is what a call with no arguments
//	                         leaves there: MC_mdaGetVolume.
//	25 (clip)                once, on a clip just filled: MC_mdaClipGetVolume.
//	26 (clip, level)         on every clip between its putData and its play,
//	                         with a level from 0 to 100: MC_mdaClipSetVolume.
//
// The three were tied together by answering 14 and 25 with marker values: in
// seven of the eight local titles that call either, the level 26 then receives
// is exactly what 14 answered, or what 25 answered where 14 is not called, and
// the eighth passes a constant of its own. So 14 and 25 read a volume and 26 is
// handed one, and the reading of 26 as the water mark a streaming clip raises
// its callback at — the other contract with a clip and a percentage — is out.
// Answering zero from both getters, as this block did while they were unnamed,
// put a zero in front of every clip of those titles: silence, had 26 not been
// discarded as well. One title's own volume option arrives as that level in
// steps of twenty, which is what discarding 26 had taken away.
//
// Every function a local title reaches is now named by its callers, and a
// number none of them reaches is refused rather than answered from the
// specification's ordering alone, which is the rule the rest of the WIPI C
// tables follow: a number answered that way is a value a game will believe.
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
	wipicMediaPause         = 9
	wipicMediaResume        = 10
	wipicMediaStop          = 11
	wipicMediaGetVolume     = 14
	wipicMediaSetVolume     = 15
	wipicMediaVibrator      = 16
	wipicMediaSetMuteState  = 17
	wipicMediaGetMuteState  = 18
	wipicMediaClipGetVolume = 25
	wipicMediaClipSetVolume = 26
)

// maxWIPICMutedSources bounds how many sources keep a mute state. The
// specification names three; the guest chooses the number, so one past the
// bound is refused rather than remembered.
const maxWIPICMutedSources = 16

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
	// volume is the clip's own level from 0 to 100, set before or after the
	// clip is loaded and applied to its sound beside the device level. A new
	// clip starts at 100, as a Java Clip does.
	volume int32
}

// wipicClipFullVolume is the level a clip starts at.
const wipicClipFullVolume = 100

// handleWIPICMediaCall services the media table.
func (runtime *initializationRuntime) handleWIPICMediaCall(thread *armcore.Thread, function uint32) (uint32, error) {
	// These calls can change existing output, including clip eviction during
	// creation. Reconcile every Java owner before a C command mutates the shared
	// timeline; callbacks retain the recipient that owned the elapsed pass.
	switch function {
	case wipicMediaClipCreate, wipicMediaClipPutData, wipicMediaStop,
		wipicMediaClipClearData, wipicMediaClipFree, wipicMediaSetVolume:
		if err := runtime.syncClipCompletions(runtime.guestElapsed()); err != nil {
			return 0, err
		}
	}
	switch function {
	case wipicMediaClipCreate:
		return runtime.wipicCreateClip(thread)

	case wipicMediaClipPutData:
		return runtime.wipicPutClipData(thread)

	case wipicMediaPlay:
		return runtime.wipicPlayClip(thread)

	case wipicMediaPause:
		return runtime.wipicPauseClip(thread)

	case wipicMediaResume:
		return runtime.wipicResumeClip(thread)

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
		// MC_mdaVibrator(level, timeout): a strength from 0 to 100 where zero
		// is off, and a time in milliseconds that only means anything above
		// zero. The arguments used to go unread on the reasoning that there is
		// no vibrator here — but whether there is one is the Host's answer, not
		// this runtime's, and a browser has `navigator.vibrate`. So the request
		// is recorded where a Host can read it and this still returns success.
		level, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		timeout, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		runtime.client.vibrator.Vibrate(int(int32(level)), int(int32(timeout)))
		return 0, nil

	case wipicMediaGetVolume:
		return uint32(runtime.client.audio.Volume()), nil

	case wipicMediaClipGetVolume:
		clip, err := runtime.wipicClipArgument(thread)
		if err != nil {
			return 0, err
		}
		if clip == nil {
			return wipiErrorCode, nil
		}
		return uint32(clip.volume), nil

	case wipicMediaClipSetVolume:
		clip, err := runtime.wipicClipArgument(thread)
		if err != nil {
			return 0, err
		}
		level, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		if clip == nil {
			// The function returns nothing to fail with, and one title sets
			// the level of a clip it never created.
			return 0, nil
		}
		clip.volume = min(max(int32(level), 0), wipicClipFullVolume)
		if clip.state.loaded && runtime.client.audio != nil {
			if err := runtime.syncClipCompletions(runtime.guestElapsed()); err != nil {
				return 0, err
			}
			if err := runtime.client.audio.SetSoundVolume(clip.state.handle, int(clip.volume)); err != nil {
				runtime.countDiagnostic(fmt.Sprintf("wipic media clip volume failed: %v", err))
			}
		}
		return 0, nil

	case wipicMediaSetMuteState:
		// Remembered and not applied to any sound. The argument is a
		// *source* — the specification's tone, sound and recorder — and the
		// local callers settle which one 3 is not: nineteen titles mute source
		// 3 at startup and then play their own music and effects through
		// MC_mdaPlay for the whole run, so on the handset those were audible
		// and source 3 is not the source a clip plays through. The tone a
		// handset makes on a key press fits, and this platform makes none to
		// silence. Reading the call as a global mute would have silenced all
		// nineteen.
		source, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		state, err := thread.Register(1)
		if err != nil {
			return 0, err
		}
		if _, known := runtime.wipicMutedSources[source]; !known && len(runtime.wipicMutedSources) >= maxWIPICMutedSources {
			return wipiErrorCode, nil
		}
		if runtime.wipicMutedSources == nil {
			runtime.wipicMutedSources = map[uint32]bool{}
		}
		runtime.wipicMutedSources[source] = state != 0
		return 0, nil

	case wipicMediaGetMuteState:
		// The titles that mute source 3 read it first, which is how a title
		// puts the handset's own setting back on the way out; the answer is
		// what they last set.
		source, err := thread.Register(0)
		if err != nil {
			return 0, err
		}
		if runtime.wipicMutedSources[source] {
			return 1, nil
		}
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
	runtime.wipicClips[address] = &wipicMediaClip{mediaType: mediaType, volume: wipicClipFullVolume}
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
	now := runtime.guestElapsed()
	if err := runtime.syncClipCompletions(now); err != nil {
		return 0, err
	}
	if !clip.state.loaded {
		handle, loadErr := runtime.client.audio.Load(clip.state.data)
		if loadErr != nil {
			runtime.countDiagnostic(fmt.Sprintf("wipic media clip %q cannot be decoded: %v", clip.mediaType, loadErr))
			return wipiErrorCode, nil
		}
		if err := runtime.client.audio.SetSoundVolume(handle, int(clip.volume)); err != nil {
			_ = runtime.client.audio.Close(handle)
			runtime.countDiagnostic(fmt.Sprintf("wipic media clip volume failed: %v", err))
			return wipiErrorCode, nil
		}
		clip.state.handle, clip.state.loaded = handle, true
	}
	if err := runtime.client.audio.Play(clip.state.handle, now, repeat != 0); err != nil {
		runtime.countDiagnostic(fmt.Sprintf("wipic media clip cannot be played: %v", err))
		return wipiErrorCode, nil
	}
	return 0, nil
}

// wipicPauseClip serves MC_mdaPause(clip): the clip stops where it is and keeps
// its place, and a clip that is not playing — already paused, stopped, ended,
// or a handle nobody created — answers M_E_ERROR, as the specification lists.
// The callback a handset would tell is not told, for the reason
// wipicCreateClip gives.
func (runtime *initializationRuntime) wipicPauseClip(thread *armcore.Thread) (uint32, error) {
	clip, err := runtime.wipicClipArgument(thread)
	if err != nil {
		return 0, err
	}
	if clip == nil || !clip.state.loaded || runtime.client.audio == nil {
		return wipiErrorCode, nil
	}
	now := runtime.guestElapsed()
	if err := runtime.syncClipCompletions(now); err != nil {
		return 0, err
	}
	if !runtime.client.audio.Playing(clip.state.handle) {
		return wipiErrorCode, nil
	}
	if err := runtime.client.audio.Pause(clip.state.handle, now); err != nil {
		runtime.countDiagnostic(fmt.Sprintf("wipic media clip cannot be paused: %v", err))
		return wipiErrorCode, nil
	}
	return 0, nil
}

// wipicResumeClip serves MC_mdaResume(clip): a paused clip continues from where
// it stopped, and anything else answers M_E_ERROR.
func (runtime *initializationRuntime) wipicResumeClip(thread *armcore.Thread) (uint32, error) {
	clip, err := runtime.wipicClipArgument(thread)
	if err != nil {
		return 0, err
	}
	if clip == nil || !clip.state.loaded || runtime.client.audio == nil || !runtime.client.audio.Paused(clip.state.handle) {
		return wipiErrorCode, nil
	}
	now := runtime.guestElapsed()
	if err := runtime.syncClipCompletions(now); err != nil {
		return 0, err
	}
	if err := runtime.client.audio.Resume(clip.state.handle, now); err != nil {
		runtime.countDiagnostic(fmt.Sprintf("wipic media clip cannot be resumed: %v", err))
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

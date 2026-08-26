package lgt

import (
	"context"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Sound, as a Java title plays it: `org/kwis/msp/media/Clip` and `Player`.
//
// **A Java title's clips are the same clips a Clet's are.** The C block builds
// one with `MC_mdaClipCreate` and fills it with `MC_mdaClipPutData`; the Java
// constructor here does both at once, out of the byte array the title read its
// own resource into. Everything past that — the decode, the volume, the mute
// state, the mixer — is the one media block in wipic_media.go.

const javaClipClass = "org/kwis/msp/media/Clip"

// javaClipConstructor is `Clip(String type, byte[] data)`: the type the
// specification names the encoding by, and the bytes to play.
func javaClipConstructor(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	object, kind, array := arguments[0], arguments[1], arguments[2]
	mediaType, ok := client.javaText(kind)
	if !ok && kind != 0 {
		return 0, fmt.Errorf("the type at %#x is not a string this platform built", kind)
	}
	data, err := client.readJavaArrayBytes(array)
	if err != nil {
		return 0, err
	}
	if client.clips == nil {
		client.clips = map[uint32]*mediaClip{}
	}
	client.clips[object] = &mediaClip{mediaType: mediaType, data: data, volume: mediaMaxVolume}
	if client.logger != nil {
		client.logger.Debug("LGT java clip built", "type", mediaType, "bytes", len(data))
	}
	return 0, nil
}

// javaClipFromFile is `Clip(String type, String filename)`: the same clip, out
// of a file rather than an array. The name is a resource in the title's own
// archive, which is where the C block's `MC_mdaClipPutDataByFile` would take
// one from too. **A name that is not there leaves an empty clip** rather than
// failing the constructor: the specification gives this form no exception, and
// a title that asked for a sound it does not have should go on without it.
func javaClipFromFile(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	object, kind, file := arguments[0], arguments[1], arguments[2]
	mediaType, _ := client.javaText(kind)
	name, ok := client.javaText(file)
	if !ok {
		return 0, fmt.Errorf("the name at %#x is not a string this platform built", file)
	}
	data, found := client.archive.Resource(name)
	if !found {
		if client.logger != nil {
			client.logger.Debug("LGT java clip names a resource the archive has not", "name", name)
		}
	}
	if client.clips == nil {
		client.clips = map[uint32]*mediaClip{}
	}
	client.clips[object] = &mediaClip{mediaType: mediaType, data: data, volume: mediaMaxVolume}
	return 0, nil
}

// javaClipSetListener is `Clip.setListener(PlayListener)`, the object a clip
// tells its state changes to. **The listener is recorded and nothing is
// delivered to it**, which is a gap rather than an answer, and it is worth
// being clear about which parts are which.
//
// The specification's events are ERROR, END_OF_DATA, START, STOP, PAUSE,
// RESUME, RECORD and FULL_OF_DATA. Of those, the ones a title could act on here
// are START and END_OF_DATA, and the mixer behind this platform has no
// end-of-clip signal to raise the second from — so a delivery would either be
// invented or would never come. Recording the listener is what lets a title
// that only registers one go on; a title that *waits* on an event will stop
// where it waits, which is a better place to find out than a callback made up
// from a clip's byte count.
//
// Registering is not nothing, either: the object has to be kept, because the
// specification lets a title read it back and because the next thing to
// implement here is the delivery, which needs to know where to send.
func javaClipSetListener(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	clip, err := client.javaClip(arguments[0])
	if err != nil {
		return 0, err
	}
	clip.listener = arguments[1]
	if client.logger != nil {
		client.logger.Debug("LGT java clip listener registered",
			"clip", arguments[0], "listener", arguments[1])
	}
	return 0, nil
}

// javaClipPutData is `BaseClip.putData(byte[] buf, int off, int len)`: the
// bytes a title appends to a clip it built empty. It answers how many were
// taken, which is what the specification's `int` is.
//
// **A clip that is already loaded is reloaded on the next play**, because the
// mixer holds a decode of the bytes as they were: appending to a clip that has
// played once and leaving the old decode in place would play the old sound.
func javaClipPutData(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	clip, err := client.javaClip(arguments[0])
	if err != nil {
		return 0, err
	}
	data, err := client.readJavaArrayBytes(arguments[1])
	if err != nil {
		return 0, err
	}
	offset, length := arguments[2], arguments[3]
	if uint64(offset)+uint64(length) > uint64(len(data)) {
		return 0, fmt.Errorf("%d bytes from %d is past the end of a %d-byte array",
			length, offset, len(data))
	}
	clip.data = append(clip.data, data[offset:offset+length]...)
	clip.loaded = false
	if client.logger != nil {
		client.logger.Debug("LGT java clip filled",
			"type", clip.mediaType, "added", length, "bytes", len(clip.data))
	}
	return length, nil
}

// javaClipClearData is `BaseClip.clearData()`, which throws away what a clip
// holds. The decode goes with it for the same reason `putData` drops it.
func javaClipClearData(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	clip, err := client.javaClip(arguments[0])
	if err != nil {
		return 0, err
	}
	clip.data, clip.loaded = nil, false
	return 0, nil
}

// javaClipSetVolume is `Clip.setVolume(int)`, which is this clip's own level
// rather than the handset's.
func javaClipSetVolume(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	clip, err := client.javaClip(arguments[0])
	if err != nil {
		return 0, err
	}
	clip.volume = clampVolume(int32(arguments[1]))
	return javaTrue, nil
}

// javaPlayerPlay is `Player.play(Clip, boolean repeat)`, and the pair below are
// `stop` and `resume`. They answer whether the request was taken, which is what
// the specification's `boolean` is.
func javaPlayerPlay(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if !javaPlayerHasClip(client, arguments[0]) {
		return javaFalse, nil
	}
	clip, err := client.javaClip(arguments[0])
	if err != nil {
		return 0, err
	}
	if client.audio == nil || len(clip.data) == 0 {
		return javaFalse, nil
	}
	if !clip.loaded {
		handle, loadErr := client.audio.Load(clip.data)
		if loadErr != nil {
			if client.logger != nil {
				client.logger.Debug("LGT java clip cannot be decoded",
					"type", clip.mediaType, "bytes", len(clip.data), "error", loadErr)
			}
			return javaFalse, nil
		}
		clip.handle, clip.loaded = handle, true
	}
	if err := client.audio.Play(clip.handle, client.clock.now(), arguments[1] != 0); err != nil {
		if client.logger != nil {
			client.logger.Debug("LGT java clip cannot be played", "error", err)
		}
		return javaFalse, nil
	}
	return javaTrue, nil
}

func javaPlayerStop(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if !javaPlayerHasClip(client, arguments[0]) {
		return javaFalse, nil
	}
	clip, err := client.javaClip(arguments[0])
	if err != nil {
		return 0, err
	}
	if client.audio == nil || !clip.loaded {
		return javaFalse, nil
	}
	client.audio.Stop(clip.handle)
	return javaTrue, nil
}

// javaPlayerResume is `Player.resume(Clip)`. There is nothing paused to take
// up again here — the mixer plays a clip or it does not — so it starts the clip
// once, which is what a title that stopped one and asks for it again means.
func javaPlayerResume(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	return javaPlayerPlay(client, ctx, thread, []uint32{arguments[0], 0})
}

// javaPlayerHasClip reports whether a `Player` call was handed a clip at all.
// **A null one is the title's own, and it answers `false` rather than failing**:
// one title stops the sound for a scene it never started, and the specification
// gives these calls a boolean for exactly the request that cannot be carried
// out. Refusing the call instead ends the game on a line the handset shrugged
// at.
func javaPlayerHasClip(client *Client, object uint32) bool {
	if object != 0 {
		return true
	}
	if client.logger != nil {
		client.logger.Debug("LGT java player call has no clip")
	}
	return false
}

// javaClip answers the clip an object stands for.
func (client *Client) javaClip(object uint32) (*mediaClip, error) {
	clip := client.clips[object]
	if clip == nil {
		return nil, fmt.Errorf("the object at %#x is not a clip this platform built", object)
	}
	return clip, nil
}

// The two words a Java boolean comes back in.
const (
	javaFalse uint32 = 0
	javaTrue  uint32 = 1
)

// javaClipEmpty is `Clip(String type)` and `Clip(String type, int bufSize)`: a
// clip of a named type with nothing in it yet. The buffer the second form sizes
// is this platform's rather than the title's, so both build the same thing —
// and a clip with no data plays as silence rather than failing, which is what a
// title that builds one up front and fills it later depends on.
func javaClipEmpty(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	object, kind := arguments[0], arguments[1]
	mediaType, ok := client.javaText(kind)
	if !ok && kind != 0 {
		return 0, fmt.Errorf("the type at %#x is not a string this platform built", kind)
	}
	if client.clips == nil {
		client.clips = map[uint32]*mediaClip{}
	}
	client.clips[object] = &mediaClip{mediaType: mediaType, volume: mediaMaxVolume}
	if client.logger != nil {
		client.logger.Debug("LGT java empty clip built", "type", mediaType)
	}
	return 0, nil
}

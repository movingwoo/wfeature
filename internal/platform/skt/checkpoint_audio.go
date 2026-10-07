package skt

import (
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// A generation identifies the particular playback a call started. A stopped
// wait must never attach to (or stop) a newer playback on the same AudioClip.
type audioCheckpointWait struct {
	runtime    *Runtime
	clip       *audioClipData
	generation uint64
	repeat     bool
}

func (wait *audioCheckpointWait) ValidateCheckpointWait(className, name, descriptor string, timed bool, remaining time.Duration) error {
	if (className != skvm.RuntimeAudioClipClass && className != skvm.AudioClipClass) || descriptor != "()V" || name != "play" && name != "loop" ||
		wait.generation == 0 || wait.repeat != (name == "loop") || timed == wait.repeat || remaining < 0 || !timed && remaining != 0 {
		return fmt.Errorf("SKT checkpoint has an invalid audio wait")
	}
	return nil
}

func (wait *audioCheckpointWait) ResumeCheckpointWait(call *jvm.Invocation, timed bool, remaining time.Duration) (jvm.Value, error) {
	clip := wait.clip
	clip.mu.Lock()
	stopped := clip.playing
	if clip.generation != wait.generation || stopped == nil {
		stopped = make(chan struct{})
		close(stopped)
	}
	clip.mu.Unlock()
	token := &jvm.Object{ClassName: jvm.ObjectClass, Native: wait}
	waited, err := call.WaitCheckpointAsGuestThread(token, timed, remaining, stopped)
	if err != nil {
		return jvm.VoidValue(), err
	}
	clip.mu.Lock()
	interrupted := clip.generation != wait.generation || clip.playing == nil
	if !interrupted {
		wait.runtime.endPlaying(clip)
	}
	clip.mu.Unlock()
	if waited && interrupted {
		return jvm.VoidValue(), newGuestException(skvm.UserStopExceptionClass, "audio playback stopped")
	}
	return jvm.VoidValue(), nil
}

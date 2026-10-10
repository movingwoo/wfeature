package skt

import (
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The caller holds player.mu and audioTimelineMu. Backend progress is authoritative; the Player
// remembers only which natural ends it has already turned into notifications.
func (runtime *Runtime) syncPlayerLocked(object *jvm.Object, player *playerData) error {
	return runtime.syncPlayerAtLocked(object, player, runtime.audioNow())
}

func (runtime *Runtime) syncPlayerAtLocked(object *jvm.Object, player *playerData, now time.Duration) error {
	if player.state == playerClosed {
		return nil
	}
	progress, err := runtime.audioTimeline().Playback(player.handle, now)
	if err != nil {
		return err
	}
	return runtime.recordPlayerProgressLocked(object, player, progress)
}

// A Host round has already merged all scores at one instant. Observe that
// progress without another clock read and global merge for every Player.
func (runtime *Runtime) observePlayerLocked(object *jvm.Object, player *playerData) error {
	if player.state == playerClosed {
		return nil
	}
	progress, err := runtime.audioTimeline().PlaybackState(player.handle)
	if err != nil {
		return err
	}
	return runtime.recordPlayerProgressLocked(object, player, progress)
}

func (runtime *Runtime) recordPlayerProgressLocked(object *jvm.Object, player *playerData, progress backend.AudioPlayback) error {
	if progress.Completed < player.completed {
		return fmt.Errorf("media player completion count moved backward")
	}
	count := progress.Completed - player.completed
	midpListener, wipiListener := len(player.listeners) != 0, player.wipiListenerLocked() != nil
	if midpListener || wipiListener {
		// Bound each protocol's notifications per repeated pass, including
		// Java allocations and delivery work after a long Host gap.
		perPass := uint64(0)
		if midpListener {
			perPass += 2
		}
		if wipiListener {
			perPass++
		}
		if count > maxPlayerEvents/perPass {
			return fmt.Errorf("media player completion notifications exceed limit")
		}
		for i := uint64(0); i < count; i++ {
			if err := runtime.queueWIPIEventLocked(object, player, wipi.PlayEventEndOfData); err != nil {
				return err
			}
			if err := runtime.queuePlayerTimeLocked(object, player, midp.PlayerEventEndOfMedia, progress.Length.Microseconds()); err != nil {
				return err
			}
			if i+1 < count || progress.Playing {
				if err := runtime.queuePlayerTimeLocked(object, player, midp.PlayerEventStarted, 0); err != nil {
					return err
				}
			}
		}
	}
	player.completed, player.mediaTime = progress.Completed, progress.Position.Microseconds()
	if !progress.Playing && !progress.Paused {
		player.wipiClip = nil
	}
	if player.state == playerStarted && !progress.Playing && !progress.Paused {
		player.state = playerPrefetched
	}
	return nil
}

func (runtime *Runtime) queuePlayerTimeLocked(object *jvm.Object, player *playerData, event string, micros int64) error {
	if len(player.listeners) == 0 {
		return nil
	}
	value, err := runtime.VM.NewObject(jvm.LongClass, "(J)V", jvm.LongValue(micros))
	if err != nil {
		return err
	}
	return runtime.queuePlayerEventLocked(object, player, event, value)
}

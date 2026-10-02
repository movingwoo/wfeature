package session

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/keypad"
	"github.com/movingwoo/wfeature/internal/platform/detect"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
)

var ErrCheckpointUnsupported = errors.New("session: execution checkpoints are unsupported for this platform or variant")

type checkpointPointerState struct {
	Down bool
	X, Y int32
}

type checkpointSessionState struct {
	Version     uint32
	Speed       float64
	Paused      bool
	LastTickSet bool
	LastTickAge time.Duration
	Pad         keypad.PadState
	Repeat      keypad.RepeatState
	HeldKeys    []int32
	Pointer     checkpointPointerState
}

func ktfOptions(options Options) ktf.SessionOptions {
	return ktf.SessionOptions{DisableAuthentication: options.DisableAuthentication, SaveStore: options.SaveStore,
		AudioSink: options.AudioSink, FrameSink: backend.FrameSink{Output: options.FrameUpdates, Scale: options.Scale, Epoch: options.FrameEpoch},
		Speed: options.Speed, TraceLimit: options.TraceLimit, Logger: options.Logger, Width: options.width(), Height: options.height(), Clock: options.Clock}
}

func ktfNativeOptions(options Options) ktf.NativeSessionOptions {
	return ktf.NativeSessionOptions{SaveStore: options.SaveStore, AudioSink: options.AudioSink,
		Speed: options.Speed, Logger: options.Logger, Width: options.width(), Height: options.height(), Clock: options.Clock}
}

// CanCheckpoint reports whether the current execution variant has a codec.
// Capture can still refuse a busy or unsupported continuation or save store.
func (s *Session) CanCheckpoint() bool { return s != nil && (s.ktf != nil || s.ktfNative != nil) }

// ArchiveIdentity names the immutable archive a checkpoint must reopen.
func (s *Session) ArchiveIdentity() [32]byte { return s.archiveIdentity }

// Speed reports the current guest pace, including a restored checkpoint's pace.
func (s *Session) Speed() float64 { return backend.ClampSpeed(s.speed) }

// ResumeCheckpointOutput reconstructs logical audio after the Host invalidates
// its previous frame/audio queues. It does not run guest code.
func (s *Session) ResumeCheckpointOutput() {
	if s != nil && s.ktf != nil {
		s.ktf.ResumeCheckpointOutput()
	}
	if s != nil && s.ktfNative != nil {
		s.ktfNative.ResumeCheckpointOutput()
	}
}

func (s *Session) checkpointNow() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Session) captureCheckpointSession() (checkpointSessionState, error) {
	saved := checkpointSessionState{Version: 1, Speed: s.speed, Paused: s.paused, LastTickSet: !s.lastTick.IsZero(), HeldKeys: slices.Clone(s.heldKeys), Pointer: s.pointerHeld}
	if saved.LastTickSet {
		now := s.checkpointNow()
		saved.LastTickAge = now.Sub(s.lastTick)
		if saved.LastTickAge == math.MinInt64 || !now.Add(-saved.LastTickAge).Equal(s.lastTick) {
			return checkpointSessionState{}, fmt.Errorf("session checkpoint repeat clock exceeds its duration range")
		}
	}
	var err error
	saved.Pad, err = s.held.CaptureState()
	if err != nil {
		return checkpointSessionState{}, err
	}
	saved.Repeat, err = s.repeat.CaptureState()
	if err != nil {
		return checkpointSessionState{}, err
	}
	_, _, err = saved.validate()
	return saved, err
}

func (saved checkpointSessionState) validate() (keypad.Pad, keypad.Repeat, error) {
	invalid := func() (keypad.Pad, keypad.Repeat, error) {
		return keypad.Pad{}, keypad.Repeat{}, fmt.Errorf("session checkpoint has invalid settings, input or clock")
	}
	if saved.Version != 1 || math.IsNaN(saved.Speed) || math.IsInf(saved.Speed, 0) || saved.Speed < 0 || saved.Speed > 16 || saved.Speed > 0 && saved.Speed < 0.1 ||
		saved.LastTickAge == math.MinInt64 || !saved.LastTickSet && saved.LastTickAge != 0 ||
		math.Abs(float64(saved.LastTickAge))*backend.ClampSpeed(saved.Speed) >= float64(math.MaxInt64) ||
		len(saved.HeldKeys) > 64 || !saved.Pointer.Down && (saved.Pointer.X != 0 || saved.Pointer.Y != 0) {
		return invalid()
	}
	pad, err := keypad.RestorePad(saved.Pad, nil)
	if err != nil {
		return keypad.Pad{}, keypad.Repeat{}, err
	}
	repeat, err := keypad.RestoreRepeat(saved.Repeat)
	if err != nil {
		return keypad.Pad{}, keypad.Repeat{}, err
	}
	seen := make(map[int32]bool, len(saved.HeldKeys))
	code, down := pad.Held()
	found := !down
	for _, key := range saved.HeldKeys {
		if seen[key] {
			return invalid()
		}
		seen[key] = true
		found = found || ktfKeyCode(key) == code
	}
	if !found {
		return invalid()
	}
	return pad, repeat, nil
}

// CaptureCheckpoint runs on the same owning goroutine as Tick and SendKey.
// Hosts enqueue it between whole rounds and exclude external save operations
// for the duration. It does not invoke lifecycle callbacks. A canceled request
// produces no checkpoint, and a refusal leaves the current session usable.
func (s *Session) CaptureCheckpoint(ctx context.Context) (data []byte, err error) {
	if s == nil || !s.Running() {
		return nil, ErrNotRunning
	}
	if !s.CanCheckpoint() {
		return nil, ErrCheckpointUnsupported
	}
	err = s.guarded("session checkpoint capture", func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var guestSpeed float64
		if s.ktf != nil {
			guestSpeed = s.ktf.Speed()
		} else {
			guestSpeed = s.ktfNative.Speed()
		}
		if backend.ClampSpeed(s.speed) != guestSpeed {
			return fmt.Errorf("session checkpoint guest and repeat speeds disagree")
		}
		captureShared := func() ([]byte, error) {
			shared, err := s.captureCheckpointSession()
			if err != nil {
				return nil, err
			}
			return backend.EncodeCheckpointRecord(shared)
		}
		var checkpoint backend.Checkpoint
		var err error
		if s.ktf != nil {
			checkpoint, err = s.ktf.CaptureCheckpointWithSession(ctx, captureShared)
		} else {
			checkpoint, err = s.ktfNative.CaptureCheckpointWithSession(ctx, captureShared)
		}
		if err != nil {
			return err
		}
		data, err = backend.EncodeCheckpoint(checkpoint)
		if err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		data = nil
	}
	return data, err
}

// RestoreCheckpoint constructs a session after process restart without replaying
// guest startup. The caller must own the destination save directory. Output and
// input reconciliation belong to the Host after this function succeeds.
func RestoreCheckpoint(ctx context.Context, archive, data []byte, options Options) (restored *Session, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			restored, err = nil, backend.GuestPanic(options.Logger, "session checkpoint restoration", recovered)
		}
	}()
	return restoreCheckpoint(ctx, archive, data, options, nil)
}

// LoadCheckpoint atomically adopts a prepared replacement on this session's
// owning goroutine. The original archive bytes are required even for a running
// session, so replacing a file in the library cannot bypass identity checks.
func (s *Session) LoadCheckpoint(ctx context.Context, archive, data []byte) error {
	if s == nil || !s.Running() {
		return ErrNotRunning
	}
	if !s.CanCheckpoint() {
		return ErrCheckpointUnsupported
	}
	return s.guarded("session checkpoint load", func() error {
		if backend.SaveIdentity(archive) != s.archiveIdentity {
			return backend.ErrCheckpointIdentity
		}
		restored, err := restoreCheckpoint(ctx, archive, data, s.options, s)
		if err != nil {
			return err
		}
		*s = *restored
		return nil
	})
}

func restoreCheckpoint(ctx context.Context, archive, data []byte, options Options, previous *Session) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	identity := backend.SaveIdentity(archive)
	checkpoint, err := backend.DecodeCheckpoint(data, identity)
	if err != nil {
		return nil, err
	}
	if checkpoint.Variant != backend.CheckpointKTFJava && checkpoint.Variant != backend.CheckpointKTFModule && checkpoint.Variant != backend.CheckpointKTFNative {
		return nil, ErrCheckpointUnsupported
	}
	var saved checkpointSessionState
	if err := backend.DecodeCheckpointRecord(checkpoint.Session, &saved); err != nil {
		return nil, err
	}
	pad, repeat, err := saved.validate()
	if err != nil {
		return nil, err
	}
	summary, err := Inspect(archive)
	if err != nil {
		return nil, err
	}
	if summary.Platform != string(detect.KTF) {
		return nil, ErrCheckpointUnsupported
	}
	restored := &Session{platform: detect.KTF, summary: summary, options: options, archiveIdentity: identity,
		speed: saved.Speed, paused: saved.Paused, held: pad, repeat: repeat, heldKeys: slices.Clone(saved.HeldKeys), pointerHeld: saved.Pointer}
	if options.Clock != nil {
		restored.now = options.Clock.Now
	}
	if previous != nil {
		restored.now = previous.now
	}
	// Every shared record was validated before the durable commit. From here
	// adoption only assigns already constructed state and fresh Host epochs.
	if checkpoint.Variant == backend.CheckpointKTFNative {
		if saved.Pointer.Down || saved.Pad.Down || saved.Repeat.Held {
			return nil, fmt.Errorf("native checkpoint has unsupported pointer or repeat ownership")
		}
		prepared, err := ktf.PrepareNativeSessionCheckpoint(archive, checkpoint, ktfNativeOptions(options))
		if err != nil {
			return nil, err
		}
		defer prepared.Discard()
		if prepared.Speed() != backend.ClampSpeed(saved.Speed) {
			return nil, fmt.Errorf("session checkpoint guest and repeat speeds disagree")
		}
		var old *ktf.NativeSession
		if previous != nil {
			old = previous.ktfNative
			if old == nil {
				return nil, ErrCheckpointUnsupported
			}
		}
		restored.ktfNative, err = prepared.Commit(ctx, old)
		if err != nil {
			return nil, err
		}
	} else {
		prepared, err := ktf.PrepareSessionCheckpoint(archive, checkpoint, ktfOptions(options))
		if err != nil {
			return nil, err
		}
		defer prepared.Discard()
		if prepared.Speed() != backend.ClampSpeed(saved.Speed) {
			return nil, fmt.Errorf("session checkpoint guest and repeat speeds disagree")
		}
		var old *ktf.Session
		if previous != nil {
			old = previous.ktf
			if old == nil {
				return nil, ErrCheckpointUnsupported
			}
		}
		restored.ktf, err = prepared.Commit(ctx, old)
		if err != nil {
			return nil, err
		}
		restored.ktf.TimeHostPhases(options.TraceLimit > 0)
	}
	restored.options.Speed = saved.Speed
	now := restored.checkpointNow()
	restored.startedAt = now
	if saved.LastTickSet {
		restored.lastTick = now.Add(-saved.LastTickAge)
	}
	return restored, nil
}

func (s *Session) checkKeyHold(action string, code int32) error {
	if action == KeyPress && len(s.heldKeys) >= 64 && !slices.Contains(s.heldKeys, code) {
		return fmt.Errorf("session: held key count exceeds 64")
	}
	return nil
}

func (s *Session) noteKeyHold(action string, code int32) {
	index := slices.Index(s.heldKeys, code)
	if action == KeyPress && index < 0 {
		s.heldKeys = append(s.heldKeys, code)
	} else if action == KeyRelease && index >= 0 {
		s.heldKeys = slices.Delete(s.heldKeys, index, index+1)
	}
}

// HeldKeys returns owned Host key codes in press order. It includes holds
// restored from a checkpoint, which a new page connection did not initiate.
func (s *Session) HeldKeys() []int32 { return slices.Clone(s.heldKeys) }

// ReleaseHeldInput delivers releases for the restored timeline's Host holds.
// It runs guest callbacks and may report ErrExited. Hosts call it after resetting
// their output/input epochs; capture and detached validation never call it.
func (s *Session) ReleaseHeldInput(ctx context.Context) error {
	for _, code := range s.HeldKeys() {
		if err := s.SendKey(ctx, KeyRelease, code); err != nil {
			return err
		}
	}
	if point := s.pointerHeld; point.Down {
		return s.SendPointer(ctx, PointerRelease, point.X, point.Y)
	}
	return nil
}

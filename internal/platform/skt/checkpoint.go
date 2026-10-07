package skt

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

type javaCheckpointState struct {
	Version   uint32
	Speed     float64
	Instant   int64
	Elapsed   time.Duration
	Platform  javaPlatformState
	Threads   jvm.ThreadCheckpointState
	Frame     backend.Frame
	Presents  uint64
	Audio     backend.AudioState
	Vibration backend.VibrationState
}

func (runtime *Runtime) CanCheckpoint() bool {
	if runtime == nil || runtime.Archive == nil || runtime.Archive.identity == ([32]byte{}) {
		return false
	}
	state := runtime.State()
	return state == StateActive || state == StatePaused
}

// parkCheckpoint excludes Host callbacks first, then parks every guest worker.
// Unsupported native callbacks are a bounded refusal, never a partial snapshot.
func (runtime *Runtime) parkCheckpoint(ctx context.Context) (*jvm.ParkedThreads, func(), error) {
	if !runtime.dispatchMu.TryLock() {
		return nil, nil, fmt.Errorf("SKT checkpoint Host dispatch is busy")
	}
	unlock := runtime.dispatchMu.Unlock
	if !runtime.CanCheckpoint() || runtime.saveStoreBoundary() == nil || runtime.now != nil {
		unlock()
		return nil, nil, fmt.Errorf("SKT checkpoint requires a running archive, save store and guest clock")
	}
	if runtime.cheat != nil && (runtime.cheat.Freezes().Len() != 0 || len(runtime.cheat.Patches()) != 0) {
		unlock()
		return nil, nil, fmt.Errorf("SKT checkpoint has active cheat freezes or patches")
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	parked, err := runtime.VM.PauseThreads(bounded)
	if err != nil {
		unlock()
		return nil, nil, fmt.Errorf("SKT checkpoint worker barrier: %w", err)
	}
	return parked, func() { parked.Resume(); unlock() }, nil
}

func (runtime *Runtime) captureJavaState(parked *jvm.ParkedThreads) (javaCheckpointState, *checkpointHeap, error) {
	now, instant := time.Now(), runtime.pace.Now()
	heap := newCheckpointHeap(runtime, now)
	heap.guestNow = instant
	saved := javaCheckpointState{Version: 1, Speed: runtime.Speed(), Instant: instant.UnixNano(), Elapsed: instant.Sub(runtime.paceStart)}
	platform, roots, err := runtime.capturePlatformState(heap)
	if err != nil {
		return saved, heap, err
	}
	saved.Platform = platform
	if saved.Threads, err = parked.CaptureState(roots, heap.codec()); err != nil {
		return saved, heap, err
	}
	if saved.Audio, err = runtime.audioTimeline().CaptureStateAt(now); err != nil {
		return saved, heap, err
	}
	if saved.Vibration, err = runtime.vibrator.CaptureState(); err != nil {
		return saved, heap, err
	}
	saved.Frame, saved.Presents = runtime.checkpointSurface.snapshot()
	return saved, heap, saved.validate()
}

func (saved javaCheckpointState) validate() error {
	w, h := saved.Platform.dimensions()
	length, err := backend.RGBAByteLength(w, h)
	if err != nil {
		return err
	}
	if saved.Version != 1 || math.IsNaN(saved.Speed) || math.IsInf(saved.Speed, 0) || saved.Speed < 0.1 || saved.Speed > 16 ||
		saved.Elapsed < 0 || saved.Elapsed > 100*365*24*time.Hour || saved.Instant < 0 ||
		saved.Frame.Width != w || saved.Frame.Height != h || saved.Presents == 0 && len(saved.Frame.RGBA) != 0 || saved.Presents != 0 && len(saved.Frame.RGBA) != length {
		return fmt.Errorf("SKT checkpoint has invalid settings, clock or presentation")
	}
	for _, sound := range saved.Audio.Sounds {
		if sound.StartedAt < 0 || sound.StartedAt > saved.Elapsed {
			return fmt.Errorf("SKT checkpoint audio origin exceeds guest clock")
		}
	}
	return saved.Vibration.Validate()
}

// CaptureCheckpointWithSession serializes at one Host/worker barrier. Ordinary
// save bytes never enter the record: only already-issued writes are flushed.
func (runtime *Runtime) CaptureCheckpointWithSession(ctx context.Context, captureSession func() ([]byte, error)) (backend.Checkpoint, error) {
	parked, release, err := runtime.parkCheckpoint(ctx)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	defer release()
	instant := runtime.pace.Now()
	defer runtime.pace.Rebase(instant)
	saved, heap, err := runtime.captureJavaState(parked)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	var shared []byte
	if captureSession != nil {
		if shared, err = captureSession(); err != nil {
			return backend.Checkpoint{}, err
		}
	}
	data, err := backend.EncodeCheckpointRecord(saved)
	if err != nil {
		return backend.Checkpoint{}, err
	}
	if _, err = runtime.flushCheckpointSaves(ctx, heap.files); err != nil {
		return backend.Checkpoint{}, err
	}
	if err = ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	return backend.Checkpoint{Identity: runtime.Archive.identity, Variant: backend.CheckpointSKTJava, Session: shared, Runtime: data}, nil
}

type PreparedJavaSession struct {
	runtime     *Runtime
	threads     *jvm.PreparedThreads
	heap        *checkpointHeap
	saved       javaCheckpointState
	placeholder *backend.DetachedSaveStore
}

func (p *PreparedJavaSession) Speed() float64 { return p.runtime.Speed() }
func (p *PreparedJavaSession) Paused() bool   { return p.runtime.State() == StatePaused }
func (p *PreparedJavaSession) Frame() (backend.Frame, uint64) {
	return p.runtime.checkpointSurface.snapshot()
}
func (p *PreparedJavaSession) PreparationStoreCalls() (int, string) {
	return p.placeholder.Calls(), p.placeholder.FirstCall()
}
func (p *PreparedJavaSession) Discard() {
	if p != nil && p.runtime != nil {
		if p.threads != nil {
			p.threads.Discard()
		}
		p.runtime.VM.Close()
		p.runtime = nil
	}
}

// PrepareJavaCheckpoint restores detached objects and continuations. It never
// runs a constructor, class initializer, lifecycle callback or save operation.
func PrepareJavaCheckpoint(archive []byte, checkpoint backend.Checkpoint, options Options) (*PreparedJavaSession, error) {
	if checkpoint.Identity != backend.SaveIdentity(archive) {
		return nil, backend.ErrCheckpointIdentity
	}
	if checkpoint.Variant != backend.CheckpointSKTJava {
		return nil, backend.ErrCheckpointVersion
	}
	var saved javaCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		return nil, err
	}
	if err := saved.validate(); err != nil {
		return nil, err
	}
	opened, err := Open(archive)
	if err != nil {
		return nil, err
	}
	if opened.Script != nil {
		return nil, backend.ErrCheckpointVersion
	}
	w, h := saved.Platform.dimensions()
	options.Framebuffer = &javaCheckpointFramebuffer{width: w, height: h}
	placeholder := backend.NewDetachedSaveStore()
	options.SaveStore, options.Speed = placeholder, saved.Speed
	runtime, err := newRuntime(opened, options)
	if err != nil {
		if runtime != nil {
			runtime.VM.Close()
		}
		return nil, err
	}
	p := &PreparedJavaSession{runtime: runtime, saved: saved, placeholder: placeholder}
	success := false
	defer func() {
		if !success {
			p.Discard()
		}
	}()
	if saved.Platform.Jlet != runtime.jlet || saved.Platform.LegacyClip != runtime.legacyClip || saved.Platform.ResumeWithoutStart != runtime.resumeWithoutStart || saved.Platform.Authentication != runtime.authentication {
		return nil, fmt.Errorf("SKT checkpoint platform policy differs from its archive")
	}
	if err = saved.Platform.restoreBuffers(runtime); err != nil {
		return nil, err
	}
	instant := time.Unix(0, saved.Instant)
	runtime.pace.Rebase(instant)
	runtime.paceStart = instant.Add(-saved.Elapsed)
	if runtime.audio, err = backend.NewAudioFromState(saved.Audio, nil); err != nil {
		return nil, err
	}
	heap := newCheckpointHeap(runtime, time.Now())
	heap.guestNow = instant
	p.heap = heap
	var roots []*jvm.Object
	if p.threads, roots, err = runtime.VM.PrepareThreadCheckpoint(saved.Threads, heap.codec()); err != nil {
		return nil, err
	}
	if err = heap.finish(); err != nil {
		return nil, err
	}
	if err = runtime.restorePlatformState(saved.Platform, roots, heap); err != nil {
		return nil, err
	}
	if runtime.rmsState == nil && len(heap.stores) != 0 {
		return nil, fmt.Errorf("SKT checkpoint RMS handles have no platform cache")
	}
	runtime.checkpointSurface.output = nil
	runtime.checkpointSurface.rgba = append([]byte(nil), saved.Frame.RGBA...)
	runtime.checkpointSurface.presents = saved.Presents
	if err = runtime.vibrator.RestoreState(saved.Vibration); err != nil {
		return nil, err
	}
	success = true
	return p, nil
}

// Commit reads the current saves before displacing the old session, flushes
// only its previously issued writes, then rebuilds once more if they changed
// those saves. All failure paths leave the old runtime alive.
func (p *PreparedJavaSession) Commit(ctx context.Context, previous *Runtime, live backend.SaveStore, framebuffer backend.Framebuffer, audio backend.AudioSink) (*Runtime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil || p.runtime == nil || live == nil {
		return nil, fmt.Errorf("SKT checkpoint requires preparation and a live save store")
	}
	runtime := p.runtime
	w, h, err := backend.ValidateFramebuffer(framebuffer)
	if err != nil {
		return nil, err
	}
	if w != runtime.frameWidth || h != runtime.frameHeight {
		return nil, fmt.Errorf("SKT checkpoint framebuffer dimensions disagree")
	}
	var oldHeap *checkpointHeap
	if previous != nil {
		if previous.Archive == nil || previous.Archive.identity != runtime.Archive.identity {
			return nil, backend.ErrCheckpointIdentity
		}
		parked, release, err := previous.parkCheckpoint(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
		instant := previous.pace.Now()
		defer previous.pace.Rebase(instant)
		_, oldHeap, err = previous.captureJavaState(parked)
		if err != nil {
			return nil, err
		}
	}
	if err = p.heap.rebuildCheckpointSaves(live); err != nil {
		return nil, err
	}
	if previous != nil {
		wrote, err := previous.flushCheckpointSaves(ctx, oldHeap.files)
		if err != nil {
			return nil, err
		}
		if wrote {
			if err = p.heap.rebuildCheckpointSaves(live); err != nil {
				return nil, err
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if previous != nil {
		previous.AttachSaveStore(nil)
		previous.audioTimeline().SetSink(nil)
		previous.transition("checkpoint replacement", StateDestroyed)
	}
	runtime.pace.Rebase(time.Unix(0, p.saved.Instant))
	_ = runtime.vibrator.RestoreState(p.saved.Vibration)
	runtime.AttachSaveStore(live)
	runtime.checkpointSurface.output = framebuffer
	runtime.audio.ActivateOutputClock()
	runtime.audio.SetSink(audio)
	p.threads.Start()
	p.runtime = nil
	return runtime, nil
}

func (runtime *Runtime) ResumeCheckpointOutput() {
	if runtime != nil {
		runtime.audioTimeline().ResumeOutput()
	}
}

package skt

import (
	"context"
	"fmt"
	"image"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

type scriptTimerState struct {
	Period, Due    time.Duration
	Repeat, Active bool
}

type scriptGraphicsState struct {
	Width, Height                                     int
	Pixels, Backup, Frame                             []byte
	Presents                                          uint64
	Clip                                              image.Rectangle
	Color, Bank, TextStyle, TextFG, TextBG, TextAlign int
	Queue                                             []scriptBitmapState
	Scratch                                           [74]byte
	Opaque                                            [74]bool
}
type scriptBitmapState struct{ X, Y, Mirror int16 }
type scriptCheckpointState struct {
	Version       uint32
	Speed         float64
	Clock         time.Duration
	Paused        bool
	OverlayPolicy byte
	Random        []byte
	Timers        [3]scriptTimerState
	VM            sgsvm.State
	Graphics      scriptGraphicsState
	Audio         backend.AudioState
	Sound         backend.AudioHandle
	Vibration     backend.VibrationState
}

func (s *ScriptSession) CanCheckpoint() bool {
	return s != nil && !s.closed && s.archive != nil && s.archive.identity != ([32]byte{})
}
func (s *ScriptSession) Speed() float64 { return backend.ClampSpeed(s.options.Speed) }

// CaptureCheckpointWithSession runs between complete events. SGS writes saves
// synchronously and retains no file handle or pending persistence operation.
func (s *ScriptSession) CaptureCheckpointWithSession(ctx context.Context, captureSession func() ([]byte, error)) (backend.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	if !s.CanCheckpoint() || s.options.SaveStore == nil || s.randomSource == nil {
		return backend.Checkpoint{}, fmt.Errorf("SGS checkpoint requires a running archive and save store")
	}
	g := s.graphics
	saved := scriptCheckpointState{Version: 1, Speed: s.Speed(), Clock: s.clock, Paused: s.paused, OverlayPolicy: s.overlayPolicy, Sound: s.sound,
		Graphics: scriptGraphicsState{Width: g.width, Height: g.height, Pixels: slices.Clone(g.pixels), Backup: slices.Clone(g.backup), Frame: slices.Clone(g.lastFrame), Presents: g.presents,
			Clip: g.clip, Color: g.color, Bank: g.bank, TextStyle: g.textStyle, TextFG: g.textFG, TextBG: g.textBG, TextAlign: g.textAlign, Scratch: g.bitmapQueueScratch, Opaque: g.bitmapQueueOpaque}}
	var external [][]byte
	for _, entry := range g.bitmapQueue {
		saved.Graphics.Queue = append(saved.Graphics.Queue, scriptBitmapState{X: entry.x, Y: entry.y, Mirror: entry.mirror})
		external = append(external, entry.data)
	}
	for i, timer := range s.timers {
		saved.Timers[i] = scriptTimerState{timer.period, timer.due, timer.repeat, timer.active}
	}
	var err error
	if saved.Random, err = s.randomSource.MarshalBinary(); err != nil {
		return backend.Checkpoint{}, err
	}
	if saved.VM, err = s.vm.CaptureState(external); err != nil {
		return backend.Checkpoint{}, err
	}
	if saved.Audio, err = s.audio.CaptureState(); err != nil {
		return backend.Checkpoint{}, err
	}
	if saved.Vibration, err = s.vibrator.CaptureState(); err != nil {
		return backend.Checkpoint{}, err
	}
	if err = saved.validate(); err != nil {
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
	if err = ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	return backend.Checkpoint{Identity: s.archive.identity, Variant: backend.CheckpointSKTScript, Session: shared, Runtime: data}, nil
}

func (saved scriptCheckpointState) validate() error {
	g := saved.Graphics
	length, err := backend.RGBAByteLength(g.Width, g.Height)
	if err != nil {
		return err
	}
	if saved.Version != 1 || saved.Clock < 0 || saved.Clock > time.Duration(math.MaxInt64)-time.Minute ||
		math.IsNaN(saved.Speed) || math.IsInf(saved.Speed, 0) || saved.Speed < 0.1 || saved.Speed > 16 ||
		len(g.Pixels) != length/4 || len(g.Backup) != length/4 || g.Presents == 0 && len(g.Frame) != 0 || g.Presents != 0 && len(g.Frame) != length ||
		g.Color < 0 || g.Color >= 182 || g.Bank < 0 || g.Bank > 6 || g.TextStyle < 0 || g.TextStyle > 3 || g.TextFG < 0 || g.TextFG >= 182 || g.TextBG < 0 || g.TextBG >= 182 || g.TextAlign < 0 || g.TextAlign > 2 ||
		len(g.Queue) > 20 || len(g.Queue) != len(saved.VM.External) || g.Clip != g.Clip.Intersect(image.Rect(0, 0, g.Width, g.Height)) {
		return fmt.Errorf("SGS checkpoint has invalid settings, graphics or clock")
	}
	for _, timer := range saved.Timers {
		if timer.Period < 0 || timer.Period > 32767*time.Millisecond || timer.Due < 0 || timer.Due > saved.Clock+32767*time.Millisecond ||
			timer.Active && timer.Period < 10*time.Millisecond {
			return fmt.Errorf("SGS checkpoint has an invalid timer")
		}
	}
	if err := saved.Vibration.Validate(); err != nil {
		return err
	}
	return validateCheckpointAudio(saved.Audio, saved.Clock)
}

// PreparedScriptSession owns a detached runtime. No initialization, exit event,
// save access or output occurs while a record is checked.
type PreparedScriptSession struct {
	session     *ScriptSession
	placeholder *backend.DetachedSaveStore
}

func (p *PreparedScriptSession) Speed() float64 { return p.session.Speed() }
func (p *PreparedScriptSession) Paused() bool   { return p.session.paused }
func (p *PreparedScriptSession) Frame() (backend.Frame, uint64) {
	g := p.session.graphics
	return backend.Frame{Width: g.width, Height: g.height, RGBA: slices.Clone(g.lastFrame)}, g.presents
}
func (p *PreparedScriptSession) PreparationStoreCalls() (int, string) {
	return p.placeholder.Calls(), p.placeholder.FirstCall()
}
func (p *PreparedScriptSession) Discard() {
	if p != nil {
		p.session = nil
	}
}

func PrepareScriptCheckpoint(archive []byte, checkpoint backend.Checkpoint, options ScriptOptions) (*PreparedScriptSession, error) {
	if checkpoint.Identity != backend.SaveIdentity(archive) {
		return nil, backend.ErrCheckpointIdentity
	}
	if checkpoint.Variant != backend.CheckpointSKTScript {
		return nil, backend.ErrCheckpointVersion
	}
	var saved scriptCheckpointState
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
	if opened.Script == nil {
		return nil, backend.ErrCheckpointVersion
	}
	placeholder := backend.NewDetachedSaveStore()
	options.SaveStore, options.Framebuffer, options.Speed = placeholder, nil, saved.Speed
	g := saved.Graphics
	source := rand.NewPCG(0, 0)
	if err := source.UnmarshalBinary(saved.Random); err != nil {
		return nil, fmt.Errorf("SGS checkpoint random state: %w", err)
	}
	s := &ScriptSession{archive: opened, options: options, clock: saved.Clock, paused: saved.Paused, overlayPolicy: saved.OverlayPolicy, sound: saved.Sound, randomSource: source, random: rand.New(source)}
	s.graphics = &scriptGraphics{width: g.Width, height: g.Height, pixels: slices.Clone(g.Pixels), backup: slices.Clone(g.Backup), lastFrame: slices.Clone(g.Frame), presents: g.Presents,
		clip: g.Clip, color: g.Color, bank: g.Bank, textStyle: g.TextStyle, textFG: g.TextFG, textBG: g.TextBG, textAlign: g.TextAlign, bitmapQueueScratch: g.Scratch, bitmapQueueOpaque: g.Opaque}
	var external [][]byte
	if s.vm, external, err = sgsvm.RestoreState(opened.Script, s, saved.VM); err != nil {
		return nil, err
	}
	if len(s.vm.Variables) < 16 {
		return nil, fmt.Errorf("SGS checkpoint system variable banks are missing")
	}
	for i := 0; i < 16; i++ {
		if len(s.vm.Variables[i].Values) == 0 {
			return nil, fmt.Errorf("SGS checkpoint system variable is empty")
		}
	}
	for i, entry := range g.Queue {
		s.graphics.bitmapQueue = append(s.graphics.bitmapQueue, scriptQueuedBitmap{data: external[i], x: entry.X, y: entry.Y, mirror: entry.Mirror})
	}
	for i, timer := range saved.Timers {
		s.timers[i] = scriptTimer{timer.Period, timer.Due, timer.Repeat, timer.Active}
	}
	if s.audio, err = backend.NewAudioFromState(saved.Audio, nil); err != nil {
		return nil, err
	}
	s.audio.SetLogger(s.options.Logger)
	if err = s.audio.RebasePlaybackClock(saved.Clock, saved.Speed); err != nil {
		return nil, err
	}
	if s.sound != 0 {
		if _, ok := s.audio.Length(s.sound); !ok {
			return nil, fmt.Errorf("SGS checkpoint sound handle is missing")
		}
	}
	s.vibrator.SetClock(func() time.Time { return time.Unix(0, int64(s.clock)) })
	if err = s.vibrator.RestoreState(saved.Vibration); err != nil {
		return nil, err
	}
	return &PreparedScriptSession{session: s, placeholder: placeholder}, nil
}

// Commit has no save handles to reconstruct. Reads and writes issued by SGS
// events address the live store directly; replacement runs no termination event.
func (p *PreparedScriptSession) Commit(ctx context.Context, previous *ScriptSession, live backend.SaveStore, framebuffer backend.Framebuffer) (*ScriptSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil || p.session == nil || live == nil {
		return nil, fmt.Errorf("SGS checkpoint requires a preparation and live save store")
	}
	s := p.session
	w, h, err := backend.ValidateFramebuffer(framebuffer)
	if err != nil {
		return nil, err
	}
	if w != s.graphics.width || h != s.graphics.height {
		return nil, fmt.Errorf("SGS checkpoint framebuffer dimensions disagree")
	}
	if previous != nil && (previous.archive == nil || previous.archive.identity != s.archive.identity || previous.closed) {
		return nil, backend.ErrCheckpointIdentity
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if previous != nil {
		previous.options.SaveStore = nil
		previous.audio.SetSink(nil)
		previous.closed = true
	}
	s.options.SaveStore, s.options.Framebuffer, s.graphics.framebuffer = live, framebuffer, framebuffer
	s.last = time.Now()
	s.audio.ActivateOutputClock()
	s.audio.SetSink(s.options.AudioSink)
	p.session = nil
	return s, nil
}
func (s *ScriptSession) ResumeCheckpointOutput() {
	if s != nil && s.audio != nil {
		s.audio.ResumeOutput()
	}
}

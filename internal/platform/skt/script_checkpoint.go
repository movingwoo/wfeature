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

// The legacy instantiation keeps the on-disk field names and types of existing slots.
type scriptCheckpointState = scriptCheckpointRecord[sgsvm.State, scriptGraphicsState]

type scriptCheckpointRecord[V, G any] struct {
	Version       uint32
	Speed         float64
	Clock         time.Duration
	Paused        bool
	OverlayPolicy byte
	Random        []byte
	Timers        [3]scriptTimerState
	VM            V
	Graphics      G
	Audio         backend.AudioState
	Sound         backend.AudioHandle
	Vibration     backend.VibrationState
}

func (s *ScriptSession) CanCheckpoint() bool {
	return s != nil && !s.closed && s.archive != nil && s.archive.identity != ([32]byte{})
}
func (s *ScriptSession) Speed() float64 { return backend.ClampSpeed(s.options.Speed) }

// CaptureCheckpointWithSession runs between complete events. SGS writes saves
// synchronously. Open file cursors are restored against the current save store.
func (s *ScriptSession) CaptureCheckpointWithSession(ctx context.Context, captureSession func() ([]byte, error)) (backend.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	if !s.CanCheckpoint() || s.options.SaveStore == nil || s.randomSource == nil {
		return backend.Checkpoint{}, fmt.Errorf("SGS checkpoint requires a running archive and save store")
	}
	var data []byte
	var err error
	if s.wide != nil {
		data, err = s.captureWideCheckpoint()
	} else {
		data, err = s.captureLegacyCheckpoint()
	}
	if err != nil {
		return backend.Checkpoint{}, err
	}
	var shared []byte
	if captureSession != nil {
		if shared, err = captureSession(); err != nil {
			return backend.Checkpoint{}, err
		}
	}
	if err = ctx.Err(); err != nil {
		return backend.Checkpoint{}, err
	}
	return backend.Checkpoint{Identity: s.archive.identity, Variant: backend.CheckpointSKTScript, Session: shared, Runtime: data}, nil
}

func (s *ScriptSession) captureLegacyCheckpoint() ([]byte, error) {
	g := s.graphics
	saved := scriptCheckpointState{Version: 1,
		Graphics: scriptGraphicsState{Width: g.width, Height: g.height, Pixels: slices.Clone(g.pixels), Backup: slices.Clone(g.backup), Frame: slices.Clone(g.lastFrame), Presents: g.presents,
			Clip: g.clip, Color: g.color, Bank: g.bank, TextStyle: g.textStyle, TextFG: g.textFG, TextBG: g.textBG, TextAlign: g.textAlign, Scratch: g.bitmapQueueScratch, Opaque: g.bitmapQueueOpaque}}
	var external [][]byte
	for _, entry := range g.bitmapQueue {
		saved.Graphics.Queue = append(saved.Graphics.Queue, scriptBitmapState{X: entry.x, Y: entry.y, Mirror: entry.mirror})
		external = append(external, entry.data)
	}
	var err error
	if saved.VM, err = s.vm.CaptureState(external); err != nil {
		return nil, err
	}
	if err := validateLegacyGraphics(saved.Graphics, len(saved.VM.External)); err != nil {
		return nil, err
	}
	return saved.encode(s)
}

func (saved *scriptCheckpointRecord[V, G]) encode(s *ScriptSession) ([]byte, error) {
	saved.Speed, saved.Clock, saved.Paused = s.Speed(), s.clock, s.paused
	saved.OverlayPolicy, saved.Sound = s.overlayPolicy, s.sound
	for i, timer := range s.timers {
		saved.Timers[i] = scriptTimerState{timer.period, timer.due, timer.repeat, timer.active}
	}
	var err error
	if saved.Random, err = s.randomSource.MarshalBinary(); err != nil {
		return nil, err
	}
	if saved.Audio, err = s.audio.CaptureState(); err != nil {
		return nil, err
	}
	if saved.Vibration, err = s.vibrator.CaptureState(); err != nil {
		return nil, err
	}
	if err := saved.validateDevices(); err != nil {
		return nil, err
	}
	return backend.EncodeCheckpointRecord(saved)
}

func validateLegacyGraphics(g scriptGraphicsState, external int) error {
	length, err := backend.RGBAByteLength(g.Width, g.Height)
	if err != nil {
		return err
	}
	if len(g.Pixels) != length/4 || len(g.Backup) != length/4 || g.Presents == 0 && len(g.Frame) != 0 || g.Presents != 0 && len(g.Frame) != length ||
		g.Color < 0 || g.Color >= 182 || g.Bank < 0 || g.Bank > 6 || g.TextStyle < 0 || g.TextStyle > 3 || g.TextFG < 0 || g.TextFG >= 182 || g.TextBG < 0 || g.TextBG >= 182 || g.TextAlign < 0 || g.TextAlign > 2 ||
		len(g.Queue) > 20 || len(g.Queue) != external || g.Clip != g.Clip.Intersect(image.Rect(0, 0, g.Width, g.Height)) {
		return fmt.Errorf("SGS checkpoint has invalid graphics")
	}
	return nil
}

func (saved *scriptCheckpointRecord[V, G]) validateDevices() error {
	minPeriod, maxPeriod := 10*time.Millisecond, 32767*time.Millisecond
	if saved.Version == 2 {
		minPeriod, maxPeriod = time.Millisecond, math.MaxInt32*time.Millisecond
	}
	if saved.Clock < 0 || saved.Clock > time.Duration(math.MaxInt64)-max(time.Minute, maxPeriod) ||
		math.IsNaN(saved.Speed) || math.IsInf(saved.Speed, 0) || saved.Speed < 0.1 || saved.Speed > 16 {
		return fmt.Errorf("SGS checkpoint has invalid speed or clock")
	}
	for _, timer := range saved.Timers {
		if timer.Period < 0 || timer.Period > maxPeriod || timer.Due < 0 || timer.Due > saved.Clock+maxPeriod ||
			timer.Active && timer.Period < minPeriod {
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
	files       [15]wideFileCheckpoint
}

func (p *PreparedScriptSession) Speed() float64 { return p.session.Speed() }
func (p *PreparedScriptSession) Paused() bool   { return p.session.paused }
func (p *PreparedScriptSession) Frame() (backend.Frame, uint64) {
	if w := p.session.screen; w != nil {
		frame := backend.Frame{Width: w.g.width, Height: w.g.height}
		if w.presents != 0 {
			frame.RGBA = slices.Clone(w.visible)
		}
		return frame, w.presents
	}
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
	opened, err := Open(archive)
	if err != nil {
		return nil, err
	}
	if opened.Script == nil {
		return nil, backend.ErrCheckpointVersion
	}
	if opened.Script.Wide != nil {
		return prepareWideCheckpoint(opened, checkpoint.Runtime, options)
	}
	var saved scriptCheckpointState
	if err := backend.DecodeCheckpointRecord(checkpoint.Runtime, &saved); err != nil {
		return nil, err
	}
	if saved.Version != 1 {
		return nil, backend.ErrCheckpointVersion
	}
	if err := validateLegacyGraphics(saved.Graphics, len(saved.VM.External)); err != nil {
		return nil, err
	}
	prepared, err := saved.prepareDevices(opened, options)
	if err != nil {
		return nil, err
	}
	s, g := prepared.session, saved.Graphics
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
	return prepared, nil
}

func (saved *scriptCheckpointRecord[V, G]) prepareDevices(opened *Archive, options ScriptOptions) (*PreparedScriptSession, error) {
	if err := saved.validateDevices(); err != nil {
		return nil, err
	}
	placeholder := backend.NewDetachedSaveStore()
	options.SaveStore, options.Framebuffer, options.Speed = placeholder, nil, saved.Speed
	source := rand.NewPCG(0, 0)
	if err := source.UnmarshalBinary(saved.Random); err != nil {
		return nil, fmt.Errorf("SGS checkpoint random state: %w", err)
	}
	s := &ScriptSession{archive: opened, options: options, clock: saved.Clock, paused: saved.Paused, overlayPolicy: saved.OverlayPolicy, sound: saved.Sound, randomSource: source, random: rand.New(source)}
	var err error
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

// Commit rebinds open files before replacing the running session.
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
	if s.screen != nil && (w != s.screen.g.width || h != s.screen.g.height) ||
		s.graphics != nil && (w != s.graphics.width || h != s.graphics.height) {
		return nil, fmt.Errorf("SGS checkpoint framebuffer dimensions disagree")
	}
	if previous != nil && (previous.archive == nil || previous.archive.identity != s.archive.identity || previous.closed) {
		return nil, backend.ErrCheckpointIdentity
	}
	if s.wide != nil {
		if err := s.restoreWideFiles(p.files, live); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if previous != nil {
		previous.options.SaveStore = nil
		previous.audio.SetSink(nil)
		previous.closed = true
	}
	s.options.SaveStore, s.options.Framebuffer = live, framebuffer
	if s.screen != nil {
		s.screen.fb = framebuffer
	} else {
		s.graphics.framebuffer = framebuffer
	}
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

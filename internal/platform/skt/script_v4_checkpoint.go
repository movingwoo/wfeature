package skt

import (
	"fmt"
	"image"
	"slices"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

type wideScriptCheckpoint = scriptCheckpointRecord[wideExecutionState, wideScreenState]

type wideExecutionState struct {
	Machine sgsvm.WideState
	Files   [15]wideFileCheckpoint
	Held    int16
	KeyDue  time.Duration
	Editor  *wideEditorState
}

type wideEditorState struct {
	Media int32
	Limit int
	Mode  int32
}

type wideScreenState struct {
	Width, Height               int
	Pixels, Backup, Visible     []byte
	Presents                    uint64
	Clip                        image.Rectangle
	Color                       uint32
	Palette                     [182]uint32
	Gamma, Font                 int
	Alpha, Zoom, Mirror, Rotate int32
	FG, BG, Align, Style        int32
}

func (s *wideScreen) checkpoint() wideScreenState {
	g := &s.g
	return wideScreenState{
		Width: g.width, Height: g.height, Color: g.color,
		Pixels: slices.Clone(g.pixels), Backup: slices.Clone(s.back), Visible: slices.Clone(s.visible), Presents: s.presents,
		Clip:    image.Rect(g.clip.minX, g.clip.minY, g.clip.maxX, g.clip.maxY),
		Palette: s.palette, Gamma: s.gamma, Font: s.font,
		Alpha: int32(s.alpha), Zoom: int32(s.zoom), Mirror: int32(s.mirror), Rotate: int32(s.rotate),
		FG: int32(s.fg), BG: int32(s.bg), Align: int32(s.align), Style: int32(s.style),
	}
}

func (g *wideScreenState) validate() error {
	n, err := backend.RGBAByteLength(g.Width, g.Height)
	if err != nil {
		return err
	}
	if len(g.Pixels) != n || len(g.Backup) != n || len(g.Visible) != n ||
		g.Clip != g.Clip.Intersect(image.Rect(0, 0, g.Width, g.Height)) || g.Color > wideTransparent ||
		g.Gamma < 0 || g.Gamma > 6 || g.Font < 0 || g.Font >= len(wideFontW) {
		return fmt.Errorf("SGS v4 checkpoint has invalid screen state")
	}
	for _, color := range g.Palette {
		if color > 0xffffff {
			return fmt.Errorf("SGS v4 checkpoint has invalid palette color")
		}
	}
	return nil
}

// The decoded buffers belong to the detached session. The live framebuffer is
// attached only after ordinary files have been reopened successfully.
func (g *wideScreenState) restore() (*wideScreen, error) {
	if err := g.validate(); err != nil {
		return nil, err
	}
	return &wideScreen{
		g: graphicsContext{width: g.Width, height: g.Height, pixels: g.Pixels, color: g.Color,
			clip: paintRect{g.Clip.Min.X, g.Clip.Min.Y, g.Clip.Max.X, g.Clip.Max.Y}},
		back: g.Backup, visible: g.Visible, presents: g.Presents,
		palette: g.Palette, gamma: g.Gamma, font: g.Font,
		alpha: int(g.Alpha), zoom: int(g.Zoom), mirror: int(g.Mirror), rotate: int(g.Rotate),
		fg: int(g.FG), bg: int(g.BG), align: int(g.Align), style: int(g.Style),
	}, nil
}

func (s *ScriptSession) captureWideCheckpoint() ([]byte, error) {
	machine, err := s.wide.CaptureState()
	if err != nil {
		return nil, err
	}
	saved := wideScriptCheckpoint{Version: 2, Graphics: s.screen.checkpoint(),
		VM: wideExecutionState{Machine: machine, Files: s.captureWideFiles(), Held: s.wideHeld, KeyDue: s.wideKeyDue}}
	if e := s.wideEditor; e != nil {
		saved.VM.Editor = &wideEditorState{e.media, e.limit, e.mode}
	}
	if err := saved.Graphics.validate(); err != nil {
		return nil, err
	}
	return saved.encode(s)
}

func prepareWideCheckpoint(archive *Archive, data []byte, options ScriptOptions) (*PreparedScriptSession, error) {
	var saved wideScriptCheckpoint
	if err := backend.DecodeCheckpointRecord(data, &saved); err != nil {
		return nil, err
	}
	if saved.Version != 2 {
		return nil, backend.ErrCheckpointVersion
	}
	prepared, err := saved.prepareDevices(archive, options)
	if err != nil {
		return nil, err
	}
	s, v := prepared.session, &saved.VM
	if s.screen, err = saved.Graphics.restore(); err != nil {
		return nil, err
	}
	if s.wide, err = restoreProgram(s, func(program *sgsvm.Program) (*sgsvm.WideVM, error) {
		return sgsvm.RestoreWideState(program, s.wideService, v.Machine)
	}); err != nil {
		return nil, err
	}
	if err := validateWideSystem(s.wide); err != nil {
		return nil, err
	}
	if err := validateWideFiles(v.Files); err != nil {
		return nil, err
	}
	if v.Held < 0 || v.Held == 15 || v.Held > 22 || v.KeyDue < 0 || v.KeyDue > s.clock+500*time.Millisecond ||
		v.Held != 0 && v.KeyDue == 0 {
		return nil, fmt.Errorf("SGS v4 checkpoint has invalid key repeat state")
	}
	s.wideHeld, s.wideKeyDue, prepared.files = v.Held, v.KeyDue, v.Files
	if e := v.Editor; e != nil {
		if _, err := wideResource(s.wide, e.Media, true); err != nil {
			return nil, err
		}
		if e.Limit < 0 || e.Limit > backend.MaxTextInputBytes || e.Mode < 0 || e.Mode > 2 || v.Held != 0 {
			return nil, fmt.Errorf("SGS v4 checkpoint has invalid text editor")
		}
		s.wideEditor = &wideEditor{media: e.Media, limit: e.Limit, mode: e.Mode}
	}
	return prepared, nil
}

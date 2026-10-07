package skt

import (
	"fmt"
	"reflect"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

// Native nodes that have no Java wrapper receive a private heap root. This
// lets the existing heap codec preserve aliases (including GameCanvas images)
// and cycles without encoding host pointers or maintaining a second heap.
type checkpointHeap struct {
	runtime   *Runtime
	now       time.Time
	guestNow  time.Time
	wrappers  map[any]*jvm.Object
	fixups    []func() error
	checks    []func() error
	files     []*xFileData
	stores    []*recordStore
	rmsCaches map[string]*recordStore
	editors   []*textinput.State
}

func newCheckpointHeap(runtime *Runtime, now time.Time) *checkpointHeap {
	guestNow := now
	if runtime.pace != nil {
		guestNow = runtime.pace.Now()
	}
	return &checkpointHeap{runtime: runtime, now: now, guestNow: guestNow, wrappers: make(map[any]*jvm.Object)}
}
func (heap *checkpointHeap) wrap(native any) *jvm.Object {
	if native == nil || reflect.ValueOf(native).IsNil() {
		return nil
	}
	if object := heap.wrappers[native]; object != nil {
		return object
	}
	object := &jvm.Object{ClassName: jvm.ObjectClass, Native: native}
	heap.wrappers[native] = object
	return object
}
func (heap *checkpointHeap) codec() jvm.HeapCodec {
	return jvm.HeapCodec{CaptureNative: heap.captureNative, RestoreNative: heap.restoreNative}
}
func (heap *checkpointHeap) finish() error {
	for _, fix := range heap.fixups {
		if err := fix(); err != nil {
			return err
		}
	}
	for _, check := range heap.checks {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}
func nativeCheckpoint(kind string, state any, refs ...*jvm.Object) (jvm.HeapExternalPayload, error) {
	data, err := backend.EncodeCheckpointRecord(state)
	return jvm.HeapExternalPayload{Kind: "skt." + kind, Data: data, References: refs}, err
}
func decodeNativeCheckpoint(payload jvm.HeapExternalPayload, state any, references int) error {
	if len(payload.References) != references {
		return fmt.Errorf("SKT checkpoint %s has invalid references", payload.Kind)
	}
	return backend.DecodeCheckpointRecord(payload.Data, state)
}
func checkpointNative[T any](object *jvm.Object) (*T, error) {
	if object == nil {
		return nil, nil
	}
	native, ok := object.Native.(*T)
	if !ok || native == nil {
		return nil, fmt.Errorf("SKT checkpoint native reference has an incompatible payload")
	}
	return native, nil
}

type checkpointRect struct{ MinX, MinY, MaxX, MaxY int }

func captureRect(r paintRect) checkpointRect { return checkpointRect{r.minX, r.minY, r.maxX, r.maxY} }
func (r checkpointRect) restore() (paintRect, error) {
	for _, v := range []int{r.MinX, r.MinY, r.MaxX, r.MaxY} {
		if int64(v) < -(1<<33) || int64(v) > 1<<33 {
			return paintRect{}, fmt.Errorf("SKT checkpoint rectangle exceeds coordinate limits")
		}
	}
	return paintRect{r.MinX, r.MinY, r.MaxX, r.MaxY}, nil
}

type checkpointImage struct {
	Width, Height int
	RGBA          []byte
	Mutable       bool
}
type checkpointGraphics struct {
	Width, Height          int
	Screen, Active         bool
	DeviceClip, Clip       checkpointRect
	TranslateX, TranslateY int32
	Color                  uint32
	Alpha                  uint8
}
type checkpointFont struct {
	Face, Style, Size       int32
	Scale, Height, Baseline int
}
type checkpointProgress struct {
	Title           string
	Value, MaxValue int32
}
type checkpointSMS struct {
	Message []byte
	Sender  string
}
type checkpointSIS struct{ Width, Height int32 }
type checkpointXFile struct {
	Name, Archive, Entry string
	Mode                 int32
	Cursor               int
	Open, Writable       bool
}
type checkpointXText struct {
	Revision               uint64
	Text                   []rune
	MaxSize, Constraints   int32
	PaintedDisplayRevision uint64
	Focus                  bool
	X, Y, Width, Height    int32
}
type checkpointAudioClip struct {
	Handle                backend.AudioHandle
	Loop, Paused, Playing bool
	Generation            uint64
}
type checkpointAudioWait struct {
	Generation uint64
	Repeat     bool
}
type checkpointFileStream struct {
	Mark   int
	Closed bool
}
type checkpointObject3D struct {
	Name      string
	Vertices  [][3]int32
	Triangles [][4]int32
	Matrix    [3][4]int64
}
type checkpointCommand struct {
	Label, LongLabel string
	Kind, Priority   int32
}
type checkpointChoice struct {
	Kind, FitPolicy int32
	Text            []string
	Selected        []bool
}
type checkpointItem struct {
	Revision                        uint64
	Kind                            itemKind
	Label                           string
	Layout                          int32
	Text                            []rune
	MaxSize, Constraint, Appearance int32
	AltText                         string
	Commands                        int
}
type checkpointScreen struct {
	Revision                        uint64
	Kind                            screenKind
	Items                           int
	Text                            []rune
	MaxSize, Constraint             int32
	Caret                           int
	AlertText                       string
	Timeout                         int32
	Selection, SubSelection, Scroll int
}
type checkpointDisplayable struct {
	Title        string
	Commands     int
	MenuOpen     bool
	MenuRevision uint64
	MenuIndex    int
}
type checkpointGameCanvas struct {
	SuppressKeys bool
	KeyStates    int32
}
type checkpointPlayer struct {
	State       int32
	ContentType string
	Handle      backend.AudioHandle
	Duration    time.Duration
	Loops       int32
	MediaTime   int64
	Listeners   int
}
type checkpointWIPIClip struct{ ContentType string }
type checkpointRMS struct {
	Name      string
	Version   int32
	Modified  int64
	Open      int32
	Listeners int
}

func (heap *checkpointHeap) captureNative(native any) (jvm.HeapExternalPayload, error) {
	switch n := native.(type) {
	case *imageData:
		return nativeCheckpoint("image", checkpointImage{n.width, n.height, n.rgba, n.mutable})
	case *graphicsContext:
		screen := n.destination == nil
		if screen && (n.width != heap.runtime.frameWidth || n.height != heap.runtime.frameHeight || len(n.pixels) != len(heap.runtime.frameRGBA) || len(n.pixels) > 0 && &n.pixels[0] != &heap.runtime.frameRGBA[0]) {
			return jvm.HeapExternalPayload{}, fmt.Errorf("SKT checkpoint graphics has an unknown destination")
		}
		return nativeCheckpoint("graphics", checkpointGraphics{n.width, n.height, screen, n.active, captureRect(n.deviceClip), captureRect(n.clip), n.translateX, n.translateY, n.color, n.alpha}, heap.wrap(n.destination), n.font)
	case *fontData:
		return nativeCheckpoint("font", checkpointFont{n.face, n.style, n.size, n.scale, n.height, n.baseline})
	case *graphics2DData:
		return nativeCheckpoint("graphics2d", struct{}{}, n.graphics)
	case *progressBarData:
		return nativeCheckpoint("progress", checkpointProgress{n.title, n.value, n.maxValue})
	case *smsMessageData:
		return nativeCheckpoint("sms", checkpointSMS{n.shortMessage, n.sender})
	case *sisImageData:
		return nativeCheckpoint("sis", checkpointSIS{n.width, n.height})
	case *xFileData:
		heap.files = append(heap.files, n)
		return nativeCheckpoint("file", checkpointXFile{n.name, n.archiveName, n.archiveEntry, n.mode, n.cursor, n.open, n.writable})
	case *xTextFieldData:
		return nativeCheckpoint("text", checkpointXText{n.textRevision, n.text, n.maxSize, n.constraints, n.paintedDisplayRevision, n.focus, n.x, n.y, n.width, n.height}, n.owner, n.paintedDisplay, heap.wrap(n.input))
	case *textinput.State:
		saved, err := n.CaptureState(heap.guestNow)
		if err != nil {
			return jvm.HeapExternalPayload{}, err
		}
		return nativeCheckpoint("editor", saved)
	case *audioClipData:
		return nativeCheckpoint("audio-clip", checkpointAudioClip{n.handle, n.loop, n.paused, n.playing != nil, n.generation})
	case *audioCheckpointWait:
		return nativeCheckpoint("audio-wait", checkpointAudioWait{n.generation, n.repeat}, heap.wrap(n.clip))
	case *wipiFileStreamData:
		return nativeCheckpoint("file-stream", checkpointFileStream{n.mark, n.closed}, heap.wrap(n.file))
	case *object3DData:
		return nativeCheckpoint("mesh", checkpointObject3D{n.name, n.vertices, n.triangles, n.matrix})
	case *commandData:
		return nativeCheckpoint("command", checkpointCommand{n.label, n.longLabel, n.kind, n.priority})
	case *choiceData:
		saved := checkpointChoice{Kind: n.kind, FitPolicy: n.fitPolicy}
		refs := make([]*jvm.Object, len(n.elements))
		for i, element := range n.elements {
			saved.Text = append(saved.Text, element.text)
			saved.Selected = append(saved.Selected, element.selected)
			refs[i] = element.image
		}
		return nativeCheckpoint("choice", saved, refs...)
	case *itemData:
		saved := checkpointItem{n.textRevision, n.kind, n.label, n.layout, n.text, n.maxSize, n.constraint, n.appearance, n.altText, len(n.commands)}
		refs := []*jvm.Object{n.image, n.font, heap.wrap(n.choice), n.listener, n.owner}
		return nativeCheckpoint("item", saved, append(refs, n.commands...)...)
	case *screenData:
		saved := checkpointScreen{n.textRevision, n.kind, len(n.items), n.text, n.maxSize, n.constraint, n.caret, n.alertText, n.timeout, n.selection, n.subSelection, n.scroll}
		refs := []*jvm.Object{heap.wrap(n.choice), n.alertImage, n.alertType, n.selectCommand, n.itemListener, heap.wrap(n.input)}
		return nativeCheckpoint("screen", saved, append(refs, n.items...)...)
	case *displayableData:
		saved := checkpointDisplayable{n.title, len(n.commands), n.menuOpen, n.menuRevision, n.menuIndex}
		refs := []*jvm.Object{n.ticker, n.listener, heap.wrap(n.screen), heap.wrap(n.game)}
		return nativeCheckpoint("displayable", saved, append(refs, n.commands...)...)
	case *gameCanvasData:
		return nativeCheckpoint("game-canvas", checkpointGameCanvas{n.suppressKeys, n.keyStates}, heap.wrap(n.buffer), n.graphics)
	case *playerData:
		return nativeCheckpoint("player", checkpointPlayer{n.state, n.contentType, n.handle, n.duration, n.loops, n.mediaTime, len(n.listeners)}, n.listeners...)
	case *wipiClipData:
		return nativeCheckpoint("wipi-clip", checkpointWIPIClip{n.contentType}, n.player, n.listener, n.object)
	case *recordStore:
		heap.stores = append(heap.stores, n)
		return nativeCheckpoint("rms", checkpointRMS{n.name, n.version, n.modified, n.open, len(n.listeners)}, n.listeners...)
	case *string:
		return nativeCheckpoint("ticker", *n)
	default:
		return jvm.HeapExternalPayload{}, fmt.Errorf("SKT checkpoint has unsupported native payload %T", native)
	}
}

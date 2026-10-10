package skt

import (
	"fmt"
	"slices"
	"unicode/utf8"
	"weak"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

const checkpointListLimit = 1 << 18

func checkpointText(text []rune) bool {
	if len(text) > 1<<20 {
		return false
	}
	for _, r := range text {
		if !utf8.ValidRune(r) {
			return false
		}
	}
	return true
}
func (heap *checkpointHeap) restoreNative(p jvm.HeapExternalPayload) (any, error) {
	invalid := func() (any, error) { return nil, fmt.Errorf("SKT checkpoint %s payload is invalid", p.Kind) }
	refs := p.References
	switch p.Kind {
	case "skt.image":
		var s checkpointImage
		if err := decodeNativeCheckpoint(p, &s, 0); err != nil {
			return nil, err
		}
		length, err := backend.RGBAByteLength(s.Width, s.Height)
		if err != nil || length != len(s.RGBA) {
			return invalid()
		}
		return &imageData{width: s.Width, height: s.Height, rgba: slices.Clone(s.RGBA), mutable: s.Mutable}, nil
	case "skt.graphics":
		var s checkpointGraphics
		if err := decodeNativeCheckpoint(p, &s, 2); err != nil {
			return nil, err
		}
		if _, err := backend.RGBAByteLength(s.Width, s.Height); err != nil {
			return nil, err
		}
		if s.Color > 0xffffff || s.Screen != (refs[0] == nil) {
			return invalid()
		}
		device, err := s.DeviceClip.restore()
		if err != nil {
			return nil, err
		}
		clip, err := s.Clip.restore()
		if err != nil {
			return nil, err
		}
		n := &graphicsContext{width: s.Width, height: s.Height, deviceClip: device, clip: clip, translateX: s.TranslateX, translateY: s.TranslateY, color: s.Color, alpha: s.Alpha, active: s.Active, font: refs[1]}
		heap.fixups = append(heap.fixups, func() error {
			if _, err := checkpointNative[fontData](n.font); err != nil {
				return err
			}
			if s.Screen {
				if n.width != heap.runtime.frameWidth || n.height != heap.runtime.frameHeight {
					return fmt.Errorf("SKT checkpoint screen graphics dimensions differ")
				}
				n.pixels, n.screen = heap.runtime.frameRGBA, &heap.runtime.renderMu
			} else {
				var err error
				n.destination, err = checkpointNative[imageData](refs[0])
				if err != nil {
					return err
				}
				if n.destination.width != n.width || n.destination.height != n.height {
					return fmt.Errorf("SKT checkpoint image graphics dimensions differ")
				}
				n.pixels = n.destination.rgba
			}
			return nil
		})
		return n, nil
	case "skt.font":
		var s checkpointFont
		if err := decodeNativeCheckpoint(p, &s, 0); err != nil {
			return nil, err
		}
		n := newFontData(fontKey{s.Face, s.Style, s.Size})
		if n.scale != s.Scale || n.height != s.Height || n.baseline != s.Baseline {
			return invalid()
		}
		return n, nil
	case "skt.graphics2d":
		if err := decodeNativeCheckpoint(p, &struct{}{}, 1); err != nil {
			return nil, err
		}
		n := &graphics2DData{graphics: refs[0]}
		heap.fixups = append(heap.fixups, func() error { _, err := checkpointNative[graphicsContext](n.graphics); return err })
		return n, nil
	case "skt.progress":
		var s checkpointProgress
		if err := decodeNativeCheckpoint(p, &s, 0); err != nil {
			return nil, err
		}
		return &progressBarData{title: s.Title, value: s.Value, maxValue: s.MaxValue}, nil
	case "skt.sms":
		var s checkpointSMS
		if err := decodeNativeCheckpoint(p, &s, 0); err != nil {
			return nil, err
		}
		return &smsMessageData{shortMessage: slices.Clone(s.Message), sender: s.Sender}, nil
	case "skt.sis":
		var s checkpointSIS
		if err := decodeNativeCheckpoint(p, &s, 0); err != nil {
			return nil, err
		}
		return &sisImageData{width: s.Width, height: s.Height}, nil
	case "skt.file":
		var s checkpointXFile
		if err := decodeNativeCheckpoint(p, &s, 0); err != nil {
			return nil, err
		}
		if s.Cursor < 0 || int64(s.Cursor) > 1<<31-1 || s.Writable != (s.Mode&xFileWrite != 0) || len(s.Name) > 4096 {
			return invalid()
		}
		if s.Name == "" {
			if s.Open || s.Writable || s.Mode != 0 || s.Cursor != 0 || s.Archive != "" || s.Entry != "" {
				return invalid()
			}
		} else if s.Archive != "" {
			if _, err := xFileKey(s.Archive); err != nil {
				return nil, err
			}
			entry, err := safeEntryName(s.Entry)
			if err != nil || entry != s.Entry || s.Name != s.Archive+"!/"+entry || s.Writable || s.Mode != xFileRead {
				return invalid()
			}
		} else {
			if s.Entry != "" {
				return invalid()
			}
			if _, err := xFileKey(s.Name); err != nil {
				return nil, err
			}
		}
		n := &xFileData{name: s.Name, archiveName: s.Archive, archiveEntry: s.Entry, mode: s.Mode, cursor: s.Cursor, open: s.Open, writable: s.Writable}
		heap.files = append(heap.files, n)
		return n, nil
	case "skt.text":
		var s checkpointXText
		if err := decodeNativeCheckpoint(p, &s, 3); err != nil {
			return nil, err
		}
		if !checkpointText(s.Text) || s.MaxSize < 0 || int64(len(s.Text)) > int64(s.MaxSize) {
			return invalid()
		}
		n := &xTextFieldData{textRevision: s.Revision, text: slices.Clone(s.Text), maxSize: s.MaxSize, constraints: s.Constraints, paintedDisplayRevision: s.PaintedDisplayRevision, focus: s.Focus, x: s.X, y: s.Y, width: s.Width, height: s.Height, owner: refs[0], paintedDisplay: refs[1]}
		heap.fixups = append(heap.fixups, func() error { var err error; n.input, err = checkpointNative[textinput.State](refs[2]); return err })
		return n, nil
	case "skt.editor":
		var s textinput.StateSnapshot
		if err := decodeNativeCheckpoint(p, &s, 0); err != nil {
			return nil, err
		}
		n, err := textinput.RestoreState(s, heap.guestNow)
		if err == nil {
			heap.editors = append(heap.editors, n)
		}
		return n, err
	case "skt.audio-clip":
		var s checkpointAudioClip
		if err := decodeNativeCheckpoint(p, &s, 0); err != nil {
			return nil, err
		}
		if s.Handle != 0 {
			if _, ok := heap.runtime.audioTimeline().Length(s.Handle); !ok {
				return invalid()
			}
		}
		n := &audioClipData{handle: s.Handle, loop: s.Loop, paused: s.Paused, generation: s.Generation}
		if s.Playing {
			if s.Handle == 0 || s.Paused {
				return invalid()
			}
			n.playing = make(chan struct{})
			if heap.runtime.audioWaits == nil {
				heap.runtime.audioWaits = make(map[chan struct{}]struct{})
			}
			heap.runtime.audioWaits[n.playing] = struct{}{}
		}
		return n, nil
	case "skt.audio-wait":
		var s checkpointAudioWait
		if err := decodeNativeCheckpoint(p, &s, 1); err != nil {
			return nil, err
		}
		if s.Generation == 0 || refs[0] == nil {
			return invalid()
		}
		n := &audioCheckpointWait{runtime: heap.runtime, generation: s.Generation, repeat: s.Repeat}
		heap.fixups = append(heap.fixups, func() error {
			var err error
			n.clip, err = checkpointNative[audioClipData](refs[0])
			if err == nil && n.generation > n.clip.generation {
				return fmt.Errorf("SKT checkpoint audio wait generation is in the future")
			}
			return err
		})
		return n, nil
	case "skt.file-stream":
		var s checkpointFileStream
		if err := decodeNativeCheckpoint(p, &s, 1); err != nil {
			return nil, err
		}
		if refs[0] == nil || s.Mark < 0 || int64(s.Mark) > 1<<31-1 {
			return invalid()
		}
		n := &wipiFileStreamData{mark: s.Mark, closed: s.Closed}
		heap.fixups = append(heap.fixups, func() error { var err error; n.file, err = checkpointNative[xFileData](refs[0]); return err })
		return n, nil
	case "skt.mesh":
		var s checkpointObject3D
		if err := decodeNativeCheckpoint(p, &s, 0); err != nil {
			return nil, err
		}
		if len(s.Vertices) > checkpointListLimit || len(s.Triangles) > checkpointListLimit {
			return invalid()
		}
		return &object3DData{name: s.Name, vertices: slices.Clone(s.Vertices), triangles: slices.Clone(s.Triangles), matrix: s.Matrix}, nil
	case "skt.command":
		var s checkpointCommand
		if err := decodeNativeCheckpoint(p, &s, 0); err != nil {
			return nil, err
		}
		return &commandData{label: s.Label, longLabel: s.LongLabel, kind: s.Kind, priority: s.Priority}, nil
	case "skt.choice":
		var s checkpointChoice
		if err := decodeNativeCheckpoint(p, &s, len(refs)); err != nil {
			return nil, err
		}
		if len(refs) > checkpointListLimit || len(s.Text) != len(refs) || len(s.Selected) != len(refs) {
			return invalid()
		}
		n := &choiceData{kind: s.Kind, fitPolicy: s.FitPolicy, elements: make([]choiceElement, len(refs))}
		for i := range refs {
			n.elements[i] = choiceElement{text: s.Text[i], image: refs[i], selected: s.Selected[i]}
		}
		return n, nil
	case "skt.item":
		var s checkpointItem
		if err := backend.DecodeCheckpointRecord(p.Data, &s); err != nil {
			return nil, err
		}
		if len(refs) < 5 || s.Commands != len(refs)-5 || s.Commands > checkpointListLimit || s.Kind > itemText || !checkpointText(s.Text) {
			return invalid()
		}
		n := &itemData{textRevision: s.Revision, kind: s.Kind, label: s.Label, layout: s.Layout, text: slices.Clone(s.Text), maxSize: s.MaxSize, constraint: s.Constraint, appearance: s.Appearance, altText: s.AltText, image: refs[0], font: refs[1], listener: refs[3], owner: refs[4], commands: slices.Clone(refs[5:])}
		heap.fixups = append(heap.fixups, func() error { var err error; n.choice, err = checkpointNative[choiceData](refs[2]); return err })
		return n, nil
	case "skt.screen":
		var s checkpointScreen
		if err := backend.DecodeCheckpointRecord(p.Data, &s); err != nil {
			return nil, err
		}
		if len(refs) < 6 || s.Items != len(refs)-6 || s.Items > checkpointListLimit || s.Kind > screenAlert || !checkpointText(s.Text) || s.Caret < 0 || s.Caret > len(s.Text) || s.Scroll < 0 || s.SubSelection > checkpointListLimit {
			return invalid()
		}
		n := &screenData{textRevision: s.Revision, kind: s.Kind, text: slices.Clone(s.Text), maxSize: s.MaxSize, constraint: s.Constraint, caret: s.Caret, alertText: s.AlertText, timeout: s.Timeout, selection: s.Selection, subSelection: s.SubSelection, scroll: s.Scroll, alertImage: refs[1], alertType: refs[2], selectCommand: refs[3], itemListener: refs[4], items: slices.Clone(refs[6:])}
		heap.checks = append(heap.checks, func() error {
			bad := func() error { return fmt.Errorf("SKT checkpoint screen selection is outside its content") }
			switch n.kind {
			case screenList:
				if n.choice == nil {
					return bad()
				}
				if len(n.choice.elements) == 0 {
					if n.selection < -1 || n.selection > 0 {
						return bad()
					}
				} else if n.selection < 0 || n.selection >= len(n.choice.elements) {
					return bad()
				}
			case screenForm:
				if n.selection < 0 || n.selection >= max(len(n.items), 1) {
					return bad()
				}
				for _, object := range n.items {
					item, err := checkpointNative[itemData](object)
					if err != nil || item == nil || !heap.runtime.VM.IsInstance(object, midp.ItemClass) {
						return fmt.Errorf("SKT checkpoint Form contains an invalid item")
					}
				}
				if len(n.items) > 0 {
					item := n.items[n.selection].Native.(*itemData)
					if n.subSelection < 0 && (n.subSelection != -1 || item.choice == nil || len(item.choice.elements) != 0) {
						return bad()
					}
				}
			}
			return nil
		})
		heap.fixups = append(heap.fixups, func() error {
			var err error
			n.choice, err = checkpointNative[choiceData](refs[0])
			if err != nil {
				return err
			}
			n.input, err = checkpointNative[textinput.State](refs[5])
			return err
		})
		return n, nil
	case "skt.displayable":
		var s checkpointDisplayable
		if err := backend.DecodeCheckpointRecord(p.Data, &s); err != nil {
			return nil, err
		}
		if len(refs) < 4 || s.Commands != len(refs)-4 || s.Commands > checkpointListLimit || s.MenuIndex < 0 || s.MenuIndex > checkpointListLimit {
			return invalid()
		}
		n := &displayableData{title: s.Title, ticker: refs[0], listener: refs[1], commands: slices.Clone(refs[4:]), menuOpen: s.MenuOpen, menuRevision: s.MenuRevision, menuIndex: s.MenuIndex}
		heap.fixups = append(heap.fixups, func() error {
			var err error
			n.screen, err = checkpointNative[screenData](refs[2])
			if err != nil {
				return err
			}
			n.game, err = checkpointNative[gameCanvasData](refs[3])
			return err
		})
		return n, nil
	case "skt.game-canvas":
		var s checkpointGameCanvas
		if err := decodeNativeCheckpoint(p, &s, 2); err != nil {
			return nil, err
		}
		n := &gameCanvasData{graphics: refs[1], suppressKeys: s.SuppressKeys, keyStates: s.KeyStates}
		heap.fixups = append(heap.fixups, func() error {
			var err error
			n.buffer, err = checkpointNative[imageData](refs[0])
			if err != nil {
				return err
			}
			if n.buffer == nil {
				return fmt.Errorf("SKT checkpoint GameCanvas lost its buffer")
			}
			_, err = checkpointNative[graphicsContext](n.graphics)
			return err
		})
		return n, nil
	case "skt.player", "skt.wipi-player":
		var s checkpointPlayer
		if err := backend.DecodeCheckpointRecord(p.Data, &s); err != nil {
			return nil, err
		}
		hasClip := p.Kind == "skt.wipi-player"
		expected := s.Listeners + 1
		if hasClip {
			expected++
		}
		if s.Listeners < 0 || s.Listeners > maxPlayerListeners || len(refs) != expected || s.Duration < 0 ||
			s.MediaTime < 0 || s.MediaTime > s.Duration.Microseconds() || s.Loops == 0 || s.Loops < -1 ||
			s.Completed > uint64((1<<63-1)/1000000)+1 || (s.State == playerClosed) != (s.Handle == 0) {
			return invalid()
		}
		switch s.State {
		case playerClosed, playerUnrealized, playerRealized, playerPrefetched, playerStarted:
		default:
			return invalid()
		}
		if s.Handle != 0 {
			if length, ok := heap.runtime.audioTimeline().Length(s.Handle); !ok || length != s.Duration {
				return invalid()
			}
			if heap.audio != nil {
				sound, ok := heap.audio[s.Handle]
				if !ok || sound.Transient || s.Completed > sound.Completed ||
					s.State == playerStarted && sound.Paused || s.State != playerStarted && sound.Playing && !sound.Paused {
					return invalid()
				}
			}
		}
		n := &playerData{state: s.State, contentType: s.ContentType, handle: s.Handle, duration: s.Duration, loops: s.Loops, mediaTime: s.MediaTime,
			completed: s.Completed, listeners: slices.Clone(refs[1 : 1+s.Listeners]), volumeControl: refs[0]}
		if hasClip {
			n.wipiClip = refs[len(refs)-1]
			if n.wipiClip == nil {
				return invalid()
			}
			n.wipiOwner = weak.Make(n.wipiClip)
		}
		heap.players = append(heap.players, n)
		heap.checks = append(heap.checks, func() error {
			if hasClip {
				clip, err := checkpointNative[wipiClipData](n.wipiClip)
				if err != nil || clip == nil || clip.player == nil || clip.player.Native != n {
					return fmt.Errorf("SKT checkpoint WIPI Player has a different Clip owner")
				}
			}
			seen := make(map[*jvm.Object]bool, len(n.listeners))
			for _, listener := range n.listeners {
				if listener == nil || seen[listener] || !heap.runtime.VM.IsInstance(listener, midp.PlayerListenerClass) {
					return fmt.Errorf("SKT checkpoint Player listener is invalid")
				}
				seen[listener] = true
			}
			if n.volumeControl == nil {
				return nil
			}
			control, err := checkpointNative[volumeControlData](n.volumeControl)
			if err != nil || n.volumeControl.ClassName != midp.RuntimeVolumeControlClass || control.player == nil || control.player.ClassName != midp.PlayerClass || control.player.Native != n {
				return fmt.Errorf("SKT checkpoint Player control has a different owner")
			}
			return nil
		})
		return n, nil
	case "skt.volume-control":
		if err := decodeNativeCheckpoint(p, &struct{}{}, 1); err != nil {
			return nil, err
		}
		n := &volumeControlData{player: refs[0]}
		heap.checks = append(heap.checks, func() error {
			owner, err := checkpointNative[playerData](n.player)
			if err != nil || owner == nil || n.player.ClassName != midp.PlayerClass || owner.volumeControl == nil || owner.volumeControl.Native != n {
				return fmt.Errorf("SKT checkpoint VolumeControl has no matching Player")
			}
			return nil
		})
		return n, nil
	case "skt.wipi-clip":
		var s checkpointWIPIClip
		if err := decodeNativeCheckpoint(p, &s, 3); err != nil {
			return nil, err
		}
		if s.Volume < 0 || s.Volume > 100 {
			return invalid()
		}
		n := &wipiClipData{contentType: s.ContentType, volume: s.Volume, player: refs[0], listener: refs[1], object: refs[2]}
		heap.fixups = append(heap.fixups, func() error {
			if n.player == nil {
				return nil
			}
			owner, err := checkpointNative[playerData](n.player)
			if err != nil || owner == nil || n.object == nil {
				return fmt.Errorf("SKT checkpoint WIPI Clip owner is invalid")
			}
			if previous := owner.wipiClipObjectLocked(); previous != nil && previous != n.object {
				return fmt.Errorf("SKT checkpoint WIPI Player has multiple Clip owners")
			}
			// Older payloads have only the forward Clip -> Player link. Bind
			// it before any reciprocal checks, independent of payload order.
			owner.wipiOwner = weak.Make(n.object)
			audio := heap.runtime.audioTimeline()
			if audio.Playing(owner.handle) || audio.Paused(owner.handle) {
				owner.wipiClip = n.object
			}
			return nil
		})
		heap.checks = append(heap.checks, func() error { return heap.runtime.validateCheckpointWIPIClip(n) })
		return n, nil
	case "skt.rms":
		var s checkpointRMS
		if err := backend.DecodeCheckpointRecord(p.Data, &s); err != nil {
			return nil, err
		}
		if s.Listeners != len(refs) || len(refs) > checkpointListLimit || !validRecordStoreName(s.Name) || s.Open < 0 {
			return invalid()
		}
		n := &recordStore{name: s.Name, version: s.Version, modified: s.Modified, open: s.Open, listeners: slices.Clone(refs)}
		heap.stores = append(heap.stores, n)
		return n, nil
	case "skt.ticker":
		var s string
		if err := decodeNativeCheckpoint(p, &s, 0); err != nil {
			return nil, err
		}
		return &s, nil
	default:
		return invalid()
	}
}

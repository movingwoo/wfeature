package skt

import (
	"fmt"
	"slices"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/keypad"
	"github.com/movingwoo/wfeature/internal/textinput"
)

type checkpointTime struct {
	Set bool
	Age time.Duration
}

func captureCheckpointTime(value, now time.Time) (checkpointTime, error) {
	saved := checkpointTime{Set: !value.IsZero()}
	if saved.Set {
		saved.Age = now.Sub(value)
		if saved.Age == -1<<63 || !now.Add(-saved.Age).Equal(value) {
			return checkpointTime{}, fmt.Errorf("SKT checkpoint time exceeds duration range")
		}
	}
	return saved, nil
}
func (saved checkpointTime) valid() bool { return saved.Age != -1<<63 && (saved.Set || saved.Age == 0) }
func (saved checkpointTime) restore(now time.Time) time.Time {
	if !saved.Set {
		return time.Time{}
	}
	return now.Add(-saved.Age)
}

type checkpointObjectPair struct{ Object, Native int }
type checkpointFullScreen struct {
	Object int
	Full   bool
}
type checkpointFontEntry struct {
	Face, Style, Size int32
	Object            int
}
type checkpointRMSCache struct {
	Name   string
	Native int
}
type checkpointPlayerEvent struct {
	Player, Data int
	Name         string
	Listeners    []int
}
type checkpointVendorState struct {
	BacklightColor                                         int32
	BacklightOn, KeyToneOn                                 bool
	AudioVolume                                            int32
	SMSListener, TextFieldOwner, FocusedTextField, Handler int
	Component                                              int
	Revision                                               uint64
	Mode                                                   textinput.Mode
	CycleKey                                               rune
	CyclePos                                               int
	LastKey                                                checkpointTime
	DrawImageReported, ClearImageReported                  bool
	ZBufferEnabled, BackfaceCulling                        bool
}

// Root IDs refer only to the platform roots returned with this record. Native
// state travels through the same heap graph as Java fields and thread roots.
type javaPlatformState struct {
	Version                                     uint32
	RootCount                                   int
	State                                       LifecycleState
	Authentication                              backend.AuthenticationStatus
	SubscriberNumber                            string
	ResumeWithoutStart, StartCompleted          bool
	Jlet, LegacyClip                            bool
	MIDlet, DisplayOwner, Display               int
	CurrentDisplayable, PendingDisplayable      int
	DisplayRevision                             uint64
	DisplayUpdateQueued, RefreshPending         bool
	LastRefresh                                 checkpointTime
	PendingSerial                               []int
	Players                                     []int
	MediaEvents                                 []checkpointPlayerEvent
	Width, Height                               int
	FrameRGBA, RefreshFrame                     []byte
	PendingPaint                                checkpointRect
	PaintCanvas                                 int
	PaintQueued, PaintPosted, PaintDeferred     bool
	ScreenPaintQueued                           bool
	CardStack                                   []int
	EventQueue                                  int
	ScreenGraphicsObject, ScreenGraphicsContext int
	FullScreen                                  []checkpointFullScreen
	Fonts                                       []checkpointFontEntry
	LCDUI                                       bool
	Displayables                                []checkpointObjectPair
	Vendor                                      *checkpointVendorState
	Pad                                         keypad.PadState
	RMS                                         bool
	Stores                                      []checkpointRMSCache
	FileStreams                                 []checkpointObjectPair
	Events                                      []string
}

func (saved javaPlatformState) dimensions() (int, int) { return saved.Width, saved.Height }

// restoreBuffers runs before heap reconstruction, because restored screen
// graphics must alias this storage rather than a later replacement buffer.
func (saved javaPlatformState) restoreBuffers(runtime *Runtime) error {
	if err := saved.validate(); err != nil {
		return err
	}
	if runtime == nil || runtime.frameWidth != saved.Width || runtime.frameHeight != saved.Height {
		return fmt.Errorf("SKT checkpoint render dimensions disagree with runtime")
	}
	runtime.frameRGBA, runtime.refreshFrame = slices.Clone(saved.FrameRGBA), slices.Clone(saved.RefreshFrame)
	return nil
}

func (runtime *Runtime) capturePlatformState(heap *checkpointHeap) (javaPlatformState, []*jvm.Object, error) {
	if runtime == nil || runtime.VM == nil || heap == nil || heap.runtime != runtime || runtime.events == nil ||
		runtime.painting || runtime.runningSerial || runtime.lastError != nil || runtime.asyncError != nil {
		return javaPlatformState{}, nil, fmt.Errorf("SKT checkpoint platform is not at an idle boundary")
	}
	if err := runtime.validateCheckpointDisplayOwner(runtime.display, runtime.displayOwner); err != nil {
		return javaPlatformState{}, nil, err
	}
	saved := javaPlatformState{Version: 1, State: runtime.state, Authentication: runtime.authentication, SubscriberNumber: runtime.subscriberNumber,
		ResumeWithoutStart: runtime.resumeWithoutStart, StartCompleted: runtime.startCompleted, Jlet: runtime.jlet, LegacyClip: runtime.legacyClip,
		DisplayRevision: runtime.displayRevision, DisplayUpdateQueued: runtime.displayUpdateQueued, RefreshPending: runtime.refreshPending,
		Width: runtime.frameWidth, Height: runtime.frameHeight, FrameRGBA: slices.Clone(runtime.frameRGBA), RefreshFrame: slices.Clone(runtime.refreshFrame),
		PendingPaint: captureRect(runtime.pendingPaint), PaintQueued: runtime.paintQueued, PaintPosted: runtime.paintPosted, PaintDeferred: runtime.paintDeferred,
		ScreenPaintQueued: runtime.screenPaintQueued, LCDUI: runtime.lcduiState != nil, RMS: runtime.rmsState != nil}
	var err error
	if saved.LastRefresh, err = captureCheckpointTime(runtime.lastRefresh, heap.guestNow); err != nil {
		return javaPlatformState{}, nil, err
	}
	if saved.Pad, err = runtime.pad.CaptureState(); err != nil {
		return javaPlatformState{}, nil, err
	}
	if saved.Events, err = runtime.events.CaptureNames(); err != nil {
		return javaPlatformState{}, nil, err
	}
	var roots []*jvm.Object
	indices := make(map[*jvm.Object]int)
	ref := func(object *jvm.Object) int {
		if object == nil {
			return 0
		}
		if index := indices[object]; index != 0 {
			return index
		}
		roots = append(roots, object)
		indices[object] = len(roots)
		return len(roots)
	}
	saved.MIDlet, saved.DisplayOwner, saved.Display = ref(runtime.MIDlet), ref(runtime.displayOwner), ref(runtime.display)
	saved.CurrentDisplayable, saved.PendingDisplayable = ref(runtime.currentDisplayable), ref(runtime.pendingDisplayable)
	saved.PaintCanvas, saved.EventQueue = ref(runtime.paintCanvas), ref(runtime.eventQueue)
	saved.ScreenGraphicsObject, saved.ScreenGraphicsContext = ref(runtime.screenGraphicsObject), ref(heap.wrap(runtime.screenGraphicsContext))
	for _, object := range runtime.pendingSerial {
		saved.PendingSerial = append(saved.PendingSerial, ref(object))
	}
	runtime.mediaMu.Lock()
	if len(runtime.mediaPlayers) > maxMediaPlayers || len(runtime.mediaEvents) > maxPlayerEvents {
		runtime.mediaMu.Unlock()
		return javaPlatformState{}, nil, fmt.Errorf("SKT checkpoint media state exceeds limits")
	}
	players := make(map[backend.AudioHandle]*jvm.Object, len(runtime.mediaPlayers))
	for handle, object := range runtime.mediaPlayers {
		players[handle] = object
	}
	mediaEvents := make([]playerEvent, len(runtime.mediaEvents))
	for i, event := range runtime.mediaEvents {
		if len(event.Listeners) > maxPlayerListeners {
			runtime.mediaMu.Unlock()
			return javaPlatformState{}, nil, fmt.Errorf("SKT checkpoint media listener count exceeds limit")
		}
		mediaEvents[i] = event
		mediaEvents[i].Listeners = slices.Clone(event.Listeners)
	}
	runtime.mediaMu.Unlock()
	if err := runtime.validateCheckpointMedia(players, mediaEvents); err != nil {
		return javaPlatformState{}, nil, err
	}
	handles := make([]backend.AudioHandle, 0, len(players))
	for handle := range players {
		handles = append(handles, handle)
	}
	slices.Sort(handles)
	for _, handle := range handles {
		saved.Players = append(saved.Players, ref(players[handle]))
	}
	for _, event := range mediaEvents {
		record := checkpointPlayerEvent{Player: ref(event.Player), Data: ref(event.Data), Name: event.Name}
		for _, listener := range event.Listeners {
			record.Listeners = append(record.Listeners, ref(listener))
		}
		saved.MediaEvents = append(saved.MediaEvents, record)
	}
	for _, object := range runtime.cardStack {
		saved.CardStack = append(saved.CardStack, ref(object))
	}
	for _, object := range checkpointObjectKeys(runtime, runtime.fullScreen) {
		saved.FullScreen = append(saved.FullScreen, checkpointFullScreen{ref(object), runtime.fullScreen[object]})
	}
	keys := make([]fontKey, 0, len(runtime.fonts))
	for key := range runtime.fonts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].face != keys[j].face {
			return keys[i].face < keys[j].face
		}
		if keys[i].style != keys[j].style {
			return keys[i].style < keys[j].style
		}
		return keys[i].size < keys[j].size
	})
	for _, key := range keys {
		saved.Fonts = append(saved.Fonts, checkpointFontEntry{key.face, key.style, key.size, ref(runtime.fonts[key])})
	}
	if runtime.lcduiState != nil {
		for _, object := range checkpointObjectKeys(runtime, runtime.lcduiState.displayables) {
			saved.Displayables = append(saved.Displayables, checkpointObjectPair{ref(object), ref(heap.wrap(runtime.lcduiState.displayables[object]))})
		}
	}
	if vendor := runtime.skvmState; vendor != nil {
		lastKey, err := captureCheckpointTime(vendor.textInput.lastKey, heap.guestNow)
		if err != nil {
			return javaPlatformState{}, nil, err
		}
		saved.Vendor = &checkpointVendorState{BacklightColor: vendor.backlightColor, BacklightOn: vendor.backlightOn, KeyToneOn: vendor.keyToneOn,
			AudioVolume: vendor.audioVolume, SMSListener: ref(vendor.smsListener), TextFieldOwner: ref(vendor.textFieldOwner), FocusedTextField: ref(vendor.focusedTextField),
			Handler: ref(vendor.textHandler), Component: ref(vendor.textInput.component), Revision: vendor.textInput.revision,
			Mode: vendor.textInput.mode, CycleKey: vendor.textInput.cycleKey, CyclePos: vendor.textInput.cyclePos, LastKey: lastKey,
			DrawImageReported: vendor.drawImageExReported, ClearImageReported: vendor.clearImageReported, ZBufferEnabled: vendor.zBufferEnabled, BackfaceCulling: vendor.backfaceCulling}
	}
	if state := runtime.rmsState; state != nil {
		names := make([]string, 0, len(state.stores))
		for name := range state.stores {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			saved.Stores = append(saved.Stores, checkpointRMSCache{name, ref(heap.wrap(state.stores[name]))})
		}
	}
	for _, object := range checkpointObjectKeys(runtime, runtime.wipiFileStreams) {
		saved.FileStreams = append(saved.FileStreams, checkpointObjectPair{ref(object), ref(heap.wrap(runtime.wipiFileStreams[object]))})
	}
	saved.RootCount = len(roots)
	if err := saved.validate(); err != nil {
		return javaPlatformState{}, nil, err
	}
	return saved, roots, nil
}

func checkpointObjectKeys[T any](runtime *Runtime, values map[*jvm.Object]T) []*jvm.Object {
	keys := make([]*jvm.Object, 0, len(values))
	identities := make(map[*jvm.Object]uint32, len(values))
	for object := range values {
		keys = append(keys, object)
		identities[object] = runtime.VM.Identity(object)
	}
	sort.Slice(keys, func(i, j int) bool { return identities[keys[i]] < identities[keys[j]] })
	return keys
}

func (saved javaPlatformState) validate() error {
	length, err := backend.RGBAByteLength(saved.Width, saved.Height)
	if err != nil {
		return err
	}
	if saved.Version != 1 || saved.RootCount < 0 || saved.RootCount > checkpointListLimit ||
		saved.State != StateCreated && saved.State != StateActive && saved.State != StatePaused ||
		saved.Authentication != backend.AuthenticationOff && saved.Authentication != backend.AuthenticationUnsupported && saved.Authentication != backend.AuthenticationSKTLicense ||
		len(saved.SubscriberNumber) > 128 || !utf8.ValidString(saved.SubscriberNumber) ||
		len(saved.FrameRGBA) != length || len(saved.RefreshFrame) != 0 && len(saved.RefreshFrame) != length || saved.RefreshPending && len(saved.RefreshFrame) != length ||
		!saved.LastRefresh.valid() || len(saved.PendingSerial) > maxPendingSerialRunnables || len(saved.CardStack) > checkpointListLimit ||
		len(saved.FullScreen) > checkpointListLimit || len(saved.Fonts) > checkpointListLimit || len(saved.Displayables) > checkpointListLimit ||
		len(saved.Stores) > checkpointListLimit || len(saved.FileStreams) > checkpointListLimit || len(saved.Events) > 1024 ||
		len(saved.Players) > maxMediaPlayers || len(saved.MediaEvents) > maxPlayerEvents ||
		!saved.LCDUI && len(saved.Displayables) != 0 || !saved.RMS && len(saved.Stores) != 0 || saved.PaintDeferred && !saved.PaintQueued ||
		(saved.ScreenGraphicsObject == 0) != (saved.ScreenGraphicsContext == 0) || saved.Display == 0 && saved.DisplayOwner != 0 {
		return fmt.Errorf("SKT checkpoint platform settings or buffers are invalid")
	}
	if _, err := saved.PendingPaint.restore(); err != nil {
		return err
	}
	var classifier func(int32) bool
	if saved.Pad.UsesPad {
		classifier = isPadKey
	}
	if _, err := keypad.RestorePad(saved.Pad, classifier); err != nil {
		return err
	}
	refs := []int{saved.MIDlet, saved.DisplayOwner, saved.Display, saved.CurrentDisplayable, saved.PendingDisplayable, saved.PaintCanvas,
		saved.EventQueue, saved.ScreenGraphicsObject, saved.ScreenGraphicsContext}
	if saved.State != StateCreated && saved.MIDlet == 0 || saved.PaintQueued && saved.PaintCanvas == 0 ||
		saved.DisplayUpdateQueued != (saved.PendingDisplayable != 0) {
		return fmt.Errorf("SKT checkpoint platform required root is missing")
	}
	for _, group := range [][]int{saved.PendingSerial, saved.CardStack} {
		for _, id := range group {
			if id == 0 {
				return fmt.Errorf("SKT checkpoint platform list contains a null root")
			}
			refs = append(refs, id)
		}
	}
	players := make(map[int]bool, len(saved.Players))
	for _, id := range saved.Players {
		if id == 0 || players[id] {
			return fmt.Errorf("SKT checkpoint media registry contains a null or duplicate player")
		}
		players[id] = true
		refs = append(refs, id)
	}
	for _, event := range saved.MediaEvents {
		if event.Player == 0 || len(event.Listeners) == 0 || len(event.Listeners) > maxPlayerListeners {
			return fmt.Errorf("SKT checkpoint media event has invalid player or listeners")
		}
		if _, isWIPI := wipiPlaybackEvent(event.Name); isWIPI {
			if event.Data == 0 || len(event.Listeners) != 1 {
				return fmt.Errorf("SKT checkpoint WIPI media event has invalid Clip or listener")
			}
		} else {
			switch event.Name {
			case midp.PlayerEventStarted, midp.PlayerEventStopped, "endOfMedia", "durationUpdated", midp.PlayerEventVolumeChanged:
				if event.Data == 0 {
					return fmt.Errorf("SKT checkpoint media event payload is missing")
				}
			case midp.PlayerEventClosed:
				if event.Data != 0 {
					return fmt.Errorf("SKT checkpoint closed media event has a payload")
				}
			default:
				return fmt.Errorf("SKT checkpoint media event name is invalid")
			}
		}
		refs = append(refs, event.Player, event.Data)
		listeners := make(map[int]bool, len(event.Listeners))
		for _, id := range event.Listeners {
			if id == 0 || listeners[id] {
				return fmt.Errorf("SKT checkpoint media event contains a null or duplicate listener")
			}
			listeners[id] = true
			refs = append(refs, id)
		}
	}
	for _, entry := range saved.FullScreen {
		if entry.Object == 0 {
			return fmt.Errorf("SKT checkpoint fullscreen key is null")
		}
		refs = append(refs, entry.Object)
	}
	for _, entry := range saved.Fonts {
		if entry.Object == 0 {
			return fmt.Errorf("SKT checkpoint font is null")
		}
		refs = append(refs, entry.Object)
	}
	for _, group := range [][]checkpointObjectPair{saved.Displayables, saved.FileStreams} {
		for _, entry := range group {
			if entry.Object == 0 || entry.Native == 0 {
				return fmt.Errorf("SKT checkpoint platform map contains a null root")
			}
			refs = append(refs, entry.Object, entry.Native)
		}
	}
	for _, entry := range saved.Stores {
		if !validRecordStoreName(entry.Name) || entry.Native == 0 {
			return fmt.Errorf("SKT checkpoint RMS cache entry is invalid")
		}
		refs = append(refs, entry.Native)
	}
	if vendor := saved.Vendor; vendor != nil {
		if vendor.AudioVolume < 0 || vendor.AudioVolume > 100 || vendor.Mode > textinput.ModeNumeric || vendor.CyclePos < 0 ||
			!vendor.LastKey.valid() || vendor.CycleKey == 0 && vendor.CyclePos != 0 {
			return fmt.Errorf("SKT checkpoint vendor settings are invalid")
		}
		if vendor.CycleKey != 0 {
			characters, ok := textinput.Characters(vendor.CycleKey)
			if !ok || vendor.CyclePos >= utf8.RuneCountInString(characters) || !vendor.LastKey.Set || vendor.Mode == textinput.ModeNumeric || vendor.Component == 0 {
				return fmt.Errorf("SKT checkpoint vendor input cycle is invalid")
			}
		}
		refs = append(refs, vendor.SMSListener, vendor.TextFieldOwner, vendor.FocusedTextField, vendor.Handler, vendor.Component)
	}
	used := make([]bool, saved.RootCount)
	for _, id := range refs {
		if id < 0 || id > saved.RootCount {
			return fmt.Errorf("SKT checkpoint platform root is out of bounds")
		}
		if id != 0 {
			used[id-1] = true
		}
	}
	for _, referenced := range used {
		if !referenced {
			return fmt.Errorf("SKT checkpoint platform has an unreferenced root")
		}
	}
	counts := make(map[string]int)
	for _, name := range saved.Events {
		switch name {
		case "Display.setCurrent", "Canvas.repaint", "Screen.paint", "MIDlet.notifyPaused", "MIDlet.notifyDestroyed", "System.exit", "MIDlet.resumeRequest":
			counts[name]++
		default:
			return fmt.Errorf("SKT checkpoint queued event %q has no restorable handler", name)
		}
	}
	for _, entry := range []struct {
		name   string
		queued bool
	}{{"Display.setCurrent", saved.DisplayUpdateQueued}, {"Canvas.repaint", saved.PaintPosted}, {"Screen.paint", saved.ScreenPaintQueued}} {
		if counts[entry.name] > 1 || (counts[entry.name] == 1) != entry.queued {
			return fmt.Errorf("SKT checkpoint queued %s ownership is invalid", entry.name)
		}
	}
	return nil
}

func (runtime *Runtime) restorePlatformState(saved javaPlatformState, roots []*jvm.Object, heap *checkpointHeap) error {
	if err := saved.validate(); err != nil {
		return err
	}
	if runtime == nil || runtime.VM == nil || heap == nil || heap.runtime != runtime || len(roots) != saved.RootCount ||
		runtime.frameWidth != saved.Width || runtime.frameHeight != saved.Height || len(runtime.frameRGBA) != len(saved.FrameRGBA) {
		return fmt.Errorf("SKT checkpoint platform restore boundary is invalid")
	}
	seenRoots := make(map[*jvm.Object]bool, len(roots))
	for _, object := range roots {
		if object == nil || seenRoots[object] {
			return fmt.Errorf("SKT checkpoint platform root is null or duplicated")
		}
		seenRoots[object] = true
	}
	ref := func(id int) *jvm.Object {
		if id == 0 {
			return nil
		}
		return roots[id-1]
	}
	checkClass := func(id int, class string) error {
		if object := ref(id); object != nil && !runtime.VM.IsInstance(object, class) {
			return fmt.Errorf("SKT checkpoint root is not a %s", class)
		}
		return nil
	}
	for _, entry := range []struct {
		id    int
		class string
	}{{saved.MIDlet, midp.MIDletClass}, {saved.DisplayOwner, midp.MIDletClass}, {saved.Display, midp.DisplayClass},
		{saved.CurrentDisplayable, midp.DisplayableClass}, {saved.PendingDisplayable, midp.DisplayableClass}, {saved.PaintCanvas, midp.CanvasClass},
		{saved.EventQueue, wipi.EventQueueClass}} {
		if err := checkClass(entry.id, entry.class); err != nil {
			return err
		}
	}
	if err := runtime.validateCheckpointDisplayOwner(ref(saved.Display), ref(saved.DisplayOwner)); err != nil {
		return err
	}
	serial, cards := make([]*jvm.Object, len(saved.PendingSerial)), make([]*jvm.Object, len(saved.CardStack))
	for i, id := range saved.PendingSerial {
		if err := checkClass(id, jvm.RunnableClass); err != nil {
			return err
		}
		serial[i] = ref(id)
	}
	for i, id := range saved.CardStack {
		if err := checkClass(id, wipi.CardClass); err != nil {
			return err
		}
		cards[i] = ref(id)
	}
	players := make(map[backend.AudioHandle]*jvm.Object, len(saved.Players))
	for _, id := range saved.Players {
		object := ref(id)
		player, err := checkpointNative[playerData](object)
		if err != nil || player == nil || players[player.handle] != nil {
			return fmt.Errorf("SKT checkpoint media registry has an invalid or duplicate handle")
		}
		players[player.handle] = object
	}
	mediaEvents := make([]playerEvent, len(saved.MediaEvents))
	for i, event := range saved.MediaEvents {
		record := playerEvent{Player: ref(event.Player), Data: ref(event.Data), Name: event.Name}
		for _, id := range event.Listeners {
			record.Listeners = append(record.Listeners, ref(id))
		}
		mediaEvents[i] = record
	}
	if err := runtime.validateCheckpointMedia(players, mediaEvents); err != nil {
		return err
	}
	fullScreen := make(map[*jvm.Object]bool, len(saved.FullScreen))
	for _, entry := range saved.FullScreen {
		object := ref(entry.Object)
		if err := checkClass(entry.Object, midp.CanvasClass); err != nil {
			return err
		}
		if _, duplicate := fullScreen[object]; duplicate {
			return fmt.Errorf("SKT checkpoint fullscreen map has duplicate keys")
		}
		fullScreen[object] = entry.Full
	}
	fonts := make(map[fontKey]*jvm.Object, len(saved.Fonts))
	for _, entry := range saved.Fonts {
		object, key := ref(entry.Object), (fontKey{entry.Face, entry.Style, entry.Size})
		font, err := checkpointNative[fontData](object)
		if err != nil || font == nil || font.fontKey != key || !isFontClass(object.ClassName) || fonts[key] != nil {
			return fmt.Errorf("SKT checkpoint font map has an invalid or duplicate entry")
		}
		fonts[key] = object
	}
	displayables := make(map[*jvm.Object]*displayableData, len(saved.Displayables))
	for _, entry := range saved.Displayables {
		object := ref(entry.Object)
		if err := checkClass(entry.Object, midp.DisplayableClass); err != nil {
			return err
		}
		native, err := checkpointNative[displayableData](ref(entry.Native))
		if err != nil || native == nil || displayables[object] != nil {
			return fmt.Errorf("SKT checkpoint display map has an invalid or duplicate entry")
		}
		displayables[object] = native
	}
	stores := make(map[string]*recordStore, len(saved.Stores))
	for _, entry := range saved.Stores {
		native, err := checkpointNative[recordStore](ref(entry.Native))
		if err != nil || native == nil || native.name != entry.Name || stores[entry.Name] != nil {
			return fmt.Errorf("SKT checkpoint RMS cache has an invalid or duplicate entry")
		}
		stores[entry.Name] = native
	}
	streams := make(map[*jvm.Object]*xFileData, len(saved.FileStreams))
	for _, entry := range saved.FileStreams {
		object := ref(entry.Object)
		native, err := checkpointNative[xFileData](ref(entry.Native))
		if err != nil || native == nil || streams[object] != nil ||
			!runtime.VM.IsInstance(object, jvm.InputStreamClass) && !runtime.VM.IsInstance(object, jvm.OutputStreamClass) {
			return fmt.Errorf("SKT checkpoint file stream map has an invalid or duplicate entry")
		}
		streams[object] = native
	}
	graphics, err := checkpointNative[graphicsContext](ref(saved.ScreenGraphicsContext))
	if err != nil {
		return err
	}
	if object := ref(saved.ScreenGraphicsObject); object != nil {
		if object.Native != graphics || !isGraphicsClass(object.ClassName) || graphics == nil || graphics.destination != nil ||
			graphics.width != saved.Width || graphics.height != saved.Height || len(graphics.pixels) != len(runtime.frameRGBA) || &graphics.pixels[0] != &runtime.frameRGBA[0] {
			return fmt.Errorf("SKT checkpoint screen graphics lost its screen alias")
		}
	}
	var vendor *skvmState
	if v := saved.Vendor; v != nil {
		for _, entry := range []struct {
			id    int
			class string
		}{{v.SMSListener, skvm.SMSListenerClass}, {v.TextFieldOwner, midp.CanvasClass}, {v.FocusedTextField, skvm.XTextFieldClass},
			{v.Handler, skvm.TextComponentHandlerClass}, {v.Component, skvm.TextComponentClass}} {
			if err := checkClass(entry.id, entry.class); err != nil {
				return err
			}
		}
		if v.FocusedTextField != 0 {
			field, err := checkpointNative[xTextFieldData](ref(v.FocusedTextField))
			if err != nil || field == nil {
				return fmt.Errorf("SKT checkpoint focused field has no text state")
			}
		}
		vendor = &skvmState{backlightColor: v.BacklightColor, backlightOn: v.BacklightOn, keyToneOn: v.KeyToneOn, audioVolume: v.AudioVolume,
			smsListener: ref(v.SMSListener), textFieldOwner: ref(v.TextFieldOwner), focusedTextField: ref(v.FocusedTextField), textHandler: ref(v.Handler),
			textInput:           textInputState{component: ref(v.Component), revision: v.Revision, mode: v.Mode, cycleKey: v.CycleKey, cyclePos: v.CyclePos, lastKey: v.LastKey.restore(heap.guestNow)},
			drawImageExReported: v.DrawImageReported, clearImageReported: v.ClearImageReported, zBufferEnabled: v.ZBufferEnabled, backfaceCulling: v.BackfaceCulling}
	}
	var classifier func(int32) bool
	if saved.Pad.UsesPad {
		classifier = isPadKey
	}
	pad, _ := keypad.RestorePad(saved.Pad, classifier)
	events := backend.NewEventLoop(backend.EventLoopOptions{})
	for _, name := range saved.Events {
		if err := events.Post(name, runtime.checkpointEvent(name)); err != nil {
			return err
		}
	}
	runtime.MIDlet, runtime.displayOwner, runtime.display = ref(saved.MIDlet), ref(saved.DisplayOwner), ref(saved.Display)
	runtime.currentDisplayable, runtime.pendingDisplayable = ref(saved.CurrentDisplayable), ref(saved.PendingDisplayable)
	runtime.displayRevision, runtime.displayUpdateQueued, runtime.refreshPending = saved.DisplayRevision, saved.DisplayUpdateQueued, saved.RefreshPending
	runtime.pendingSerial, runtime.runningSerial, runtime.lastRefresh = serial, false, saved.LastRefresh.restore(heap.guestNow)
	runtime.mediaPlayers, runtime.mediaEvents = players, mediaEvents
	runtime.pendingPaint, _ = saved.PendingPaint.restore()
	runtime.paintCanvas, runtime.paintQueued, runtime.paintPosted, runtime.paintDeferred = ref(saved.PaintCanvas), saved.PaintQueued, saved.PaintPosted, saved.PaintDeferred
	runtime.painting, runtime.screenPaintQueued = false, saved.ScreenPaintQueued
	runtime.cardStack, runtime.eventQueue, runtime.fullScreen, runtime.fonts = cards, ref(saved.EventQueue), fullScreen, fonts
	runtime.screenGraphicsObject, runtime.screenGraphicsContext = ref(saved.ScreenGraphicsObject), graphics
	runtime.state, runtime.authentication, runtime.subscriberNumber = saved.State, saved.Authentication, saved.SubscriberNumber
	runtime.resumeWithoutStart, runtime.startCompleted, runtime.jlet, runtime.legacyClip = saved.ResumeWithoutStart, saved.StartCompleted, saved.Jlet, saved.LegacyClip
	runtime.pad, runtime.events, runtime.wipiFileStreams = pad, events, streams
	if saved.LCDUI {
		runtime.lcduiState = &lcduiState{displayables: displayables}
		runtime.lcduiOnce.Do(func() {})
	}
	if vendor != nil {
		runtime.skvmState = vendor
		runtime.skvmOnce.Do(func() {})
	}
	if saved.RMS {
		runtime.rmsState = &rmsState{stores: stores}
		runtime.rmsOnce.Do(func() {})
	}
	return nil
}

// WIPI's static display factory has no MIDlet owner argument and may run before
// application construction completes. Only that display family permits an
// absent MIDP owner; the root's actual class is checked at both boundaries.
func (runtime *Runtime) validateCheckpointDisplayOwner(display, owner *jvm.Object) error {
	if display == nil {
		if owner != nil {
			return fmt.Errorf("SKT checkpoint Display owner has no Display")
		}
		return nil
	}
	if !runtime.VM.IsInstance(display, midp.DisplayClass) {
		return fmt.Errorf("SKT checkpoint Display has an invalid class")
	}
	if owner == nil {
		if !runtime.VM.IsInstance(display, wipi.DisplayClass) {
			return fmt.Errorf("SKT checkpoint MIDP Display has no owner")
		}
	} else if !runtime.VM.IsInstance(owner, midp.MIDletClass) {
		return fmt.Errorf("SKT checkpoint Display owner is not a MIDlet")
	}
	return nil
}

// validateCheckpointMedia runs only at a held capture barrier or on detached
// restored objects. It does not invoke Java or advance the audio timeline.
func (runtime *Runtime) validateCheckpointMedia(players map[backend.AudioHandle]*jvm.Object, events []playerEvent) error {
	if len(players) > maxMediaPlayers || len(events) > maxPlayerEvents {
		return fmt.Errorf("SKT checkpoint media state exceeds limits")
	}
	seen := make(map[*jvm.Object]bool, len(players))
	for handle, object := range players {
		player, err := checkpointNative[playerData](object)
		if err != nil || player == nil || object.ClassName != midp.PlayerClass || handle == 0 ||
			player.handle != handle || player.state == playerClosed || seen[object] {
			return fmt.Errorf("SKT checkpoint media registry ownership is invalid")
		}
		seen[object] = true
		if _, ok := runtime.audioTimeline().Length(handle); !ok {
			return fmt.Errorf("SKT checkpoint media registry sound is missing")
		}
	}
	for _, event := range events {
		player, err := checkpointNative[playerData](event.Player)
		if err != nil || player == nil || event.Player.ClassName != midp.PlayerClass ||
			player.state != playerClosed && players[player.handle] != event.Player ||
			len(event.Listeners) == 0 || len(event.Listeners) > maxPlayerListeners {
			return fmt.Errorf("SKT checkpoint media event ownership is invalid")
		}
		if _, isWIPI := wipiPlaybackEvent(event.Name); isWIPI {
			clip, err := checkpointNative[wipiClipData](event.Data)
			if err != nil || clip == nil || clip.object != event.Data || clip.player != event.Player ||
				len(event.Listeners) != 1 || event.Listeners[0] == nil || !runtime.VM.IsInstance(event.Listeners[0], wipi.PlayListenerClass) {
				return fmt.Errorf("SKT checkpoint WIPI media event ownership is invalid")
			}
			if err := runtime.validateCheckpointWIPIClip(clip); err != nil {
				return err
			}
			continue
		}
		listeners := make(map[*jvm.Object]bool, len(event.Listeners))
		for _, listener := range event.Listeners {
			if listener == nil || listeners[listener] || !runtime.VM.IsInstance(listener, midp.PlayerListenerClass) {
				return fmt.Errorf("SKT checkpoint media event listener is invalid")
			}
			listeners[listener] = true
		}
		switch event.Name {
		case midp.PlayerEventStarted, midp.PlayerEventStopped, "endOfMedia", "durationUpdated":
			if event.Data == nil || event.Data.ClassName != jvm.LongClass {
				return fmt.Errorf("SKT checkpoint media event time is not a Long")
			}
			value, ok := event.Data.Native.(int64)
			if !ok || value < 0 && (event.Name != "durationUpdated" || value != -1) {
				return fmt.Errorf("SKT checkpoint media event time is invalid")
			}
		case midp.PlayerEventVolumeChanged:
			control, err := checkpointNative[volumeControlData](event.Data)
			if err != nil || control == nil || event.Data.ClassName != midp.RuntimeVolumeControlClass ||
				control.player != event.Player || player.volumeControl != event.Data {
				return fmt.Errorf("SKT checkpoint media event control has a different owner")
			}
		case midp.PlayerEventClosed:
			if event.Data != nil || player.state != playerClosed {
				return fmt.Errorf("SKT checkpoint closed media event is invalid")
			}
		default:
			return fmt.Errorf("SKT checkpoint media event name is invalid")
		}
	}
	return nil
}

func (runtime *Runtime) checkpointEvent(name string) func() error {
	switch name {
	case "Display.setCurrent":
		return runtime.applyCurrentDisplayable
	case "Canvas.repaint":
		return func() error {
			runtime.displayMu.Lock()
			runtime.paintPosted = false
			runtime.displayMu.Unlock()
			return runtime.paintPendingCanvas()
		}
	case "Screen.paint":
		return runtime.paintPendingScreen
	case "MIDlet.notifyPaused":
		return func() error {
			if runtime.State() == StateActive {
				runtime.transition("notifyPaused", StatePaused)
			}
			return nil
		}
	case "MIDlet.notifyDestroyed", "System.exit":
		return func() error {
			if state := runtime.State(); state != StateDestroyed && state != StateError {
				event := "notifyDestroyed"
				if name == "System.exit" {
					event = "exit"
				}
				runtime.transition(event, StateDestroyed)
			}
			return nil
		}
	case "MIDlet.resumeRequest":
		return func() error {
			if runtime.State() != StatePaused {
				return nil
			}
			return runtime.resume(true)
		}
	default:
		return nil
	}
}

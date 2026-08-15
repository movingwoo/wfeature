package skt

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/wipic"
)

type LifecycleState string

const (
	StateCreated   LifecycleState = "created"
	StateActive    LifecycleState = "active"
	StatePaused    LifecycleState = "paused"
	StateDestroyed LifecycleState = "destroyed"
	StateError     LifecycleState = "error"
)

var (
	ErrInvalidLifecycleState = errors.New("invalid MIDlet lifecycle state")
	ErrMIDletDestroyRefused  = errors.New("MIDlet refused conditional destroy")
)

type Runtime struct {
	Archive *Archive
	VM      *jvm.VM
	MIDlet  *jvm.Object

	events *backend.EventLoop
	logger *slog.Logger
	// natives counts what each registered native was called, which is what a
	// diagnostic report is built from. See diagnostics.go.
	nativeMu   sync.RWMutex
	natives    map[string]*nativeCounter
	dispatchMu sync.Mutex
	stateMu    sync.RWMutex
	state      LifecycleState
	lastError  error

	displayMu           sync.RWMutex
	displayOwner        *jvm.Object
	display             *jvm.Object
	currentDisplayable  *jvm.Object
	pendingDisplayable  *jvm.Object
	displayUpdateQueued bool
	// pendingSerial holds the Runnables Display.callSerially was handed. One
	// comes off per Host pass rather than all of them at once: see
	// callSeriallyRunnable.
	pendingSerial []*jvm.Object

	framebuffer  backend.Framebuffer
	frameWidth   int
	frameHeight  int
	renderMu     sync.Mutex
	frameRGBA    []byte
	pendingPaint paintRect
	paintCanvas  *jvm.Object
	paintQueued  bool
	painting     bool
	fullScreen   map[*jvm.Object]bool
	// The one Graphics every Canvas paint draws the screen through, and which
	// stays usable after the paint returns. See screenGraphics.
	screenGraphicsObject  *jvm.Object
	screenGraphicsContext *graphicsContext

	fontMu sync.Mutex
	fonts  map[fontKey]*jvm.Object

	lcduiOnce  sync.Once
	lcduiState *lcduiState
	// screenPaintQueued keeps at most one runtime-drawn screen repaint on the
	// event queue at a time, the same way paintQueued does for a Canvas.
	screenPaintQueued bool

	skvmOnce  sync.Once
	skvmState *skvmState

	audioMu sync.Mutex
	audio   *backend.Audio

	saveMu    sync.RWMutex
	saveStore backend.SaveStore
	rmsOnce   sync.Once
	rmsState  *rmsState
	// now reads the wall clock RecordStore.getLastModified reports. It is a
	// field so a test can pin it; nothing else in this runtime depends on
	// real time.
	now func() int64
}

// renewGuestSteps grants another step window while the MIDlet is running. See
// where it is installed for why a window rather than a ceiling.
func (runtime *Runtime) renewGuestSteps() error {
	switch runtime.State() {
	case StateDestroyed, StateError:
		return jvm.ErrStepLimit
	}
	return nil
}

// clockMillis is the millisecond timestamp RMS stamps stores with.
func (runtime *Runtime) clockMillis() int64 {
	if runtime.now != nil {
		return runtime.now()
	}
	return time.Now().UnixMilli()
}

type Options struct {
	JVM         jvm.Options
	Framebuffer backend.Framebuffer
	// SaveStore persists RMS record stores and com.xce.io files. Without one
	// they live only as long as the session.
	SaveStore backend.SaveStore
}

type RuntimeSummary struct {
	Name      string          `json:"name"`
	MainClass string          `json:"mainClass"`
	State     LifecycleState  `json:"state"`
	Error     string          `json:"error,omitempty"`
	Display   *DisplaySummary `json:"display,omitempty"`
}

type DisplaySummary struct {
	CurrentClass string `json:"currentClass,omitempty"`
	Shown        bool   `json:"shown"`
	FullScreen   bool   `json:"fullScreen,omitempty"`
}

// Start constructs the manifest MIDlet and calls startApp through the backend
// event loop. Runtime-owned MIDP classes take precedence over classes bundled
// by the application.
func Start(archive *Archive, options Options) (*Runtime, error) {
	if archive == nil {
		return nil, fmt.Errorf("SKT archive is nil")
	}
	frameWidth, frameHeight, err := backend.ValidateFramebuffer(options.Framebuffer)
	if err != nil {
		return nil, fmt.Errorf("validate framebuffer: %w", err)
	}
	frameLength, err := backend.RGBAByteLength(frameWidth, frameHeight)
	if err != nil {
		return nil, fmt.Errorf("validate framebuffer: %w", err)
	}
	// The runtime libraries precede the application so a game cannot replace
	// a platform class, and SKVM follows MIDP because it is built on it. Both
	// are unconditional: every title this package runs is an SKT title, and
	// one that never touches com.skt.m simply never loads those classes.
	source := jvm.ClassSources{midp.Library{}, skvm.Library{}, archive}
	runtime := &Runtime{}
	// A title's own thread is the game: it decodes its images, loads its world
	// and runs its frames, and it does that for as long as the title is up. So
	// the JVM's step limit is a window here rather than a ceiling, renewed
	// while this runtime is alive and refused once it is not — which makes
	// destroying the MIDlet the way a runaway guest stops, since a spinning
	// thread will not ask anything else. A caller that wants the ceiling back
	// installs its own hook.
	if options.JVM.RenewSteps == nil {
		options.JVM.RenewSteps = runtime.renewGuestSteps
	}
	// These handsets' default charset is EUC-KR, and a title finds out through
	// its own text: `"…".getBytes()` handed to a renderer that indexes a glyph
	// table by the byte it was given. UTF-8 makes that index a different number
	// — an in-game menu died on `index 11034 for length 4700` drawing one label
	// while the labels beside it, byte arrays out of the title's own resources,
	// drew correctly. See "The default charset is the handset's" in skvm.md.
	if options.JVM.ByteDecoder == nil {
		options.JVM.ByteDecoder = decodePlatformBytes
	}
	if options.JVM.ByteEncoder == nil {
		options.JVM.ByteEncoder = encodePlatformString
	}
	machine := jvm.New(source, options.JVM)
	*runtime = Runtime{
		Archive:     archive,
		VM:          machine,
		events:      backend.NewEventLoop(backend.EventLoopOptions{}),
		logger:      options.JVM.Logger,
		state:       StateCreated,
		framebuffer: options.Framebuffer,
		frameWidth:  frameWidth,
		frameHeight: frameHeight,
		frameRGBA:   make([]byte, frameLength),
		fullScreen:  make(map[*jvm.Object]bool),
		fonts:       make(map[fontKey]*jvm.Object),
		saveStore:   options.SaveStore,
	}
	for index := 3; index < len(runtime.frameRGBA); index += 4 {
		runtime.frameRGBA[index] = 0xff
	}

	isMIDlet, err := machine.IsSubclassOf(archive.Descriptor.MainClass, midp.MIDletClass)
	if err != nil {
		return runtime, runtime.fail("validate", fmt.Errorf("main class: %w", err))
	}
	if !isMIDlet {
		return runtime, runtime.fail("validate", fmt.Errorf("main class does not extend %s", midp.MIDletClass))
	}
	if err := runtime.registerMIDletNatives(); err != nil {
		return runtime, runtime.fail("register MIDP services", err)
	}
	if err := runtime.registerRecordStoreNatives(); err != nil {
		return runtime, runtime.fail("register RMS services", err)
	}
	if err := runtime.registerHighLevelNatives(); err != nil {
		return runtime, runtime.fail("register high-level MIDP services", err)
	}
	if err := runtime.registerConnectorNatives(); err != nil {
		return runtime, runtime.fail("register MIDP connection services", err)
	}
	if err := runtime.registerSKVMNatives(); err != nil {
		return runtime, runtime.fail("register SKVM services", err)
	}
	if err := runtime.dispatch("start", runtime.start); err != nil {
		return runtime, err
	}
	return runtime, nil
}

func (runtime *Runtime) Pause() error {
	return runtime.dispatch("pause", runtime.pause)
}

func (runtime *Runtime) Resume() error {
	return runtime.dispatch("resume", func() error {
		return runtime.resume(true)
	})
}

func (runtime *Runtime) Destroy(unconditional bool) error {
	return runtime.dispatch("destroy", func() error {
		state := runtime.State()
		if state != StateActive && state != StatePaused {
			return runtime.invalidState("destroy", state)
		}
		argument := int32(0)
		if unconditional {
			argument = 1
		}
		if _, err := runtime.VM.InvokeVirtual(runtime.MIDlet, "destroyApp", "(Z)V", jvm.IntValue(argument)); err != nil {
			if runtime.isStateChangeException(err) {
				if !unconditional {
					if runtime.logger != nil {
						runtime.logger.Debug("MIDlet refused conditional destroy", "state", state, "error", err)
					}
					return fmt.Errorf("%w: %w", ErrMIDletDestroyRefused, err)
				}
				if runtime.logger != nil {
					runtime.logger.Debug("ignoring MIDletStateChangeException from unconditional destroy", "error", err)
				}
				runtime.transition("destroy", StateDestroyed)
				return nil
			}
			if runtime.isRuntimeException(err) {
				if runtime.logger != nil {
					runtime.logger.Debug("ignoring RuntimeException from destroyApp", "unconditional", unconditional, "error", err)
				}
				runtime.transition("destroy after RuntimeException", StateDestroyed)
				return nil
			}
			return runtime.fail("destroy", err)
		}
		runtime.transition("destroy", StateDestroyed)
		return nil
	})
}

// RunPending drains notifications posted by MIDlet native methods outside an
// existing host event callback. It is also the pass a serial Runnable is due
// on, which is what paces a frame loop written as a Runnable that re-queues
// itself.
func (runtime *Runtime) RunPending() error {
	runtime.dispatchMu.Lock()
	defer runtime.dispatchMu.Unlock()
	if err := runtime.postNextSerialRunnable(); err != nil {
		return err
	}
	return runtime.runEvents()
}

func (runtime *Runtime) State() LifecycleState {
	runtime.stateMu.RLock()
	defer runtime.stateMu.RUnlock()
	return runtime.state
}

func (runtime *Runtime) AppProperty(name string) (string, bool) {
	return runtime.Archive.Descriptor.Property(name)
}

func (runtime *Runtime) Summary() RuntimeSummary {
	runtime.stateMu.RLock()
	summary := RuntimeSummary{
		Name:      runtime.Archive.Descriptor.Name,
		MainClass: runtime.Archive.Descriptor.MainClass,
		State:     runtime.state,
	}
	if runtime.lastError != nil {
		summary.Error = runtime.lastError.Error()
	}
	runtime.stateMu.RUnlock()

	runtime.displayMu.RLock()
	if runtime.display != nil {
		display := &DisplaySummary{Shown: summary.State == StateActive && runtime.currentDisplayable != nil}
		if runtime.currentDisplayable != nil {
			display.CurrentClass = runtime.currentDisplayable.ClassName
			display.FullScreen = runtime.fullScreen[runtime.currentDisplayable]
		}
		summary.Display = display
	}
	runtime.displayMu.RUnlock()
	return summary
}

func (runtime *Runtime) start() error {
	if state := runtime.State(); state != StateCreated {
		return runtime.invalidState("start", state)
	}
	instance, err := runtime.VM.NewObject(runtime.Archive.Descriptor.MainClass, "()V")
	if err != nil {
		return runtime.fail("create", err)
	}
	runtime.MIDlet = instance
	return runtime.resume(false)
}

func (runtime *Runtime) pause() error {
	if state := runtime.State(); state != StateActive {
		return runtime.invalidState("pause", state)
	}
	if _, err := runtime.VM.InvokeVirtual(runtime.MIDlet, "pauseApp", "()V"); err != nil {
		if runtime.isRuntimeException(err) {
			return runtime.destroyAfterRuntimeException("pause", err)
		}
		return runtime.fail("pause", err)
	}
	runtime.transition("pause", StatePaused)
	return nil
}

func (runtime *Runtime) resume(requirePaused bool) error {
	state := runtime.State()
	if requirePaused && state != StatePaused {
		return runtime.invalidState("resume", state)
	}
	if !requirePaused && state != StateCreated {
		return runtime.invalidState("start", state)
	}
	action := "resume"
	if state == StateCreated {
		action = "start"
	}
	if _, err := runtime.VM.InvokeVirtual(runtime.MIDlet, "startApp", "()V"); err != nil {
		if runtime.isStateChangeException(err) {
			runtime.transition(action+" deferred", StatePaused)
			if runtime.logger != nil {
				runtime.logger.Debug("MIDlet deferred startApp", "action", action, "error", err)
			}
			return nil
		}
		if runtime.isRuntimeException(err) {
			return runtime.destroyAfterRuntimeException(action, err)
		}
		return runtime.fail(action, err)
	}
	runtime.transition(action, StateActive)
	return nil
}

func (runtime *Runtime) dispatch(name string, handler func() error) error {
	runtime.dispatchMu.Lock()
	defer runtime.dispatchMu.Unlock()
	if err := runtime.events.Post(name, handler); err != nil {
		return runtime.fail("queue "+name, err)
	}
	return runtime.runEvents()
}

func (runtime *Runtime) runEvents() error {
	err := runtime.events.Run()
	if errors.Is(err, ErrMIDletDestroyRefused) {
		return err
	}
	if err != nil && !runtime.isRecordedFailure(err) {
		return runtime.fail("run event loop", err)
	}
	return err
}

func (runtime *Runtime) isStateChangeException(err error) bool {
	return runtime.VM.IsGuestException(err, midp.MIDletStateChangeExceptionClass)
}

func (runtime *Runtime) isRuntimeException(err error) bool {
	return runtime.VM.IsGuestException(err, jvm.RuntimeExceptionClass)
}

// destroyAfterRuntimeException applies the MIDP AMS policy for unchecked
// startApp and pauseApp failures: offer destroyApp(true) one cleanup attempt,
// then enter Destroyed while preserving the original callback diagnostic.
func (runtime *Runtime) destroyAfterRuntimeException(action string, cause error) error {
	failure := runtime.lifecycleFailure(action, cause)
	_, cleanupErr := runtime.VM.InvokeVirtual(runtime.MIDlet, "destroyApp", "(Z)V", jvm.IntValue(1))
	if cleanupErr != nil && !runtime.isStateChangeException(cleanupErr) && !runtime.isRuntimeException(cleanupErr) {
		combined := errors.Join(failure, fmt.Errorf("forced destroy cleanup after %s: %w", action, cleanupErr))
		runtime.recordFailure("cleanup after "+action, cleanupErr, combined, StateError)
		return combined
	}
	if cleanupErr != nil && runtime.logger != nil {
		runtime.logger.Debug("ignoring guest exception from forced destroy cleanup", "action", action, "error", cleanupErr)
	}
	runtime.recordFailure(action, cause, failure, StateDestroyed)
	return failure
}

func (runtime *Runtime) registerMIDletNatives() error {
	registrations := []struct {
		class      string
		name       string
		descriptor string
		method     jvm.NativeMethod
	}{
		{midp.MIDletClass, "getAppProperty", "(Ljava/lang/String;)Ljava/lang/String;", runtime.getAppProperty},
		{"java/lang/System", "getProperty", "(Ljava/lang/String;)Ljava/lang/String;", runtime.getSystemProperty},
		{midp.MIDletClass, "notifyPaused", "()V", runtime.notifyPaused},
		{midp.MIDletClass, "notifyDestroyed", "()V", runtime.notifyDestroyed},
		{midp.MIDletClass, "resumeRequest", "()V", runtime.resumeRequest},
		{midp.DisplayClass, "getDisplay", "(Ljavax/microedition/midlet/MIDlet;)Ljavax/microedition/lcdui/Display;", runtime.getDisplay},
		{midp.DisplayClass, "getCurrent", "()Ljavax/microedition/lcdui/Displayable;", runtime.getCurrentDisplayable},
		{midp.DisplayClass, "setCurrent", "(Ljavax/microedition/lcdui/Displayable;)V", runtime.setCurrentDisplayable},
		{midp.DisplayClass, "callSerially", "(Ljava/lang/Runnable;)V", runtime.callSeriallyRunnable},
		{midp.DisplayableClass, "getWidth", "()I", runtime.getDisplayableWidth},
		{midp.DisplayableClass, "getHeight", "()I", runtime.getDisplayableHeight},
		{midp.DisplayableClass, "isShown", "()Z", runtime.isDisplayableShown},
		{midp.CanvasClass, "repaint", "(IIII)V", runtime.repaintCanvas},
		{midp.CanvasClass, "serviceRepaints", "()V", runtime.serviceCanvasRepaints},
		{midp.CanvasClass, "getKeyCode", "(I)I", runtime.getCanvasKeyCode},
		{midp.CanvasClass, "getKeyName", "(I)Ljava/lang/String;", runtime.getCanvasKeyName},
		{midp.CanvasClass, "setFullScreenMode", "(Z)V", runtime.setCanvasFullScreenMode},
		{midp.FontClass, "getDefaultFont", "()Ljavax/microedition/lcdui/Font;", runtime.getDefaultFont},
		{midp.FontClass, "getFont", "(I)Ljavax/microedition/lcdui/Font;", runtime.getFontBySpecifier},
		{midp.FontClass, "getFont", "(III)Ljavax/microedition/lcdui/Font;", runtime.getFontByAttributes},
		{midp.FontClass, "getFace", "()I", runtime.getFontFace},
		{midp.FontClass, "getStyle", "()I", runtime.getFontStyle},
		{midp.FontClass, "getSize", "()I", runtime.getFontSize},
		{midp.FontClass, "isPlain", "()Z", runtime.isFontPlain},
		{midp.FontClass, "isBold", "()Z", runtime.isFontBold},
		{midp.FontClass, "isItalic", "()Z", runtime.isFontItalic},
		{midp.FontClass, "isUnderlined", "()Z", runtime.isFontUnderlined},
		{midp.FontClass, "getHeight", "()I", runtime.getFontHeight},
		{midp.FontClass, "getBaselinePosition", "()I", runtime.getFontBaseline},
		{midp.FontClass, "charWidth", "(C)I", runtime.getFontCharWidth},
		{midp.FontClass, "charsWidth", "([CII)I", runtime.getFontCharsWidth},
		{midp.FontClass, "stringWidth", "(Ljava/lang/String;)I", runtime.getFontStringWidth},
		{midp.FontClass, "substringWidth", "(Ljava/lang/String;II)I", runtime.getFontSubstringWidth},
		{midp.GraphicsClass, "setColor", "(I)V", runtime.setGraphicsColor},
		{midp.GraphicsClass, "setColor", "(III)V", runtime.setGraphicsColorRGB},
		{midp.GraphicsClass, "getColor", "()I", runtime.getGraphicsColor},
		{midp.GraphicsClass, "getFont", "()Ljavax/microedition/lcdui/Font;", runtime.getGraphicsFont},
		{midp.GraphicsClass, "setFont", "(Ljavax/microedition/lcdui/Font;)V", runtime.setGraphicsFont},
		{midp.GraphicsClass, "setClip", "(IIII)V", runtime.setGraphicsClip},
		{midp.GraphicsClass, "clipRect", "(IIII)V", runtime.clipGraphicsRect},
		{midp.GraphicsClass, "getClipX", "()I", runtime.getGraphicsClipX},
		{midp.GraphicsClass, "getClipY", "()I", runtime.getGraphicsClipY},
		{midp.GraphicsClass, "getClipWidth", "()I", runtime.getGraphicsClipWidth},
		{midp.GraphicsClass, "getClipHeight", "()I", runtime.getGraphicsClipHeight},
		{midp.GraphicsClass, "translate", "(II)V", runtime.translateGraphics},
		{midp.GraphicsClass, "getTranslateX", "()I", runtime.getGraphicsTranslateX},
		{midp.GraphicsClass, "getTranslateY", "()I", runtime.getGraphicsTranslateY},
		{midp.GraphicsClass, "fillRect", "(IIII)V", runtime.fillGraphicsRect},
		{midp.GraphicsClass, "drawLine", "(IIII)V", runtime.drawGraphicsLine},
		{midp.GraphicsClass, "drawRect", "(IIII)V", runtime.drawGraphicsRect},
		{midp.GraphicsClass, "drawImage", "(Ljavax/microedition/lcdui/Image;III)V", runtime.drawGraphicsImage},
		{midp.GraphicsClass, "drawRegion", "(Ljavax/microedition/lcdui/Image;IIIIIIII)V", runtime.drawGraphicsRegion},
		{midp.GraphicsClass, "drawRGB", "([IIIIIIIZ)V", runtime.drawGraphicsRGB},
		{midp.GraphicsClass, "drawChar", "(CIII)V", runtime.drawGraphicsChar},
		{midp.GraphicsClass, "drawChars", "([CIIIII)V", runtime.drawGraphicsChars},
		{midp.GraphicsClass, "drawString", "(Ljava/lang/String;III)V", runtime.drawGraphicsString},
		{midp.GraphicsClass, "drawSubstring", "(Ljava/lang/String;IIIII)V", runtime.drawGraphicsSubstring},
		{midp.ImageClass, "createImage", "(II)Ljavax/microedition/lcdui/Image;", runtime.createMutableImage},
		{midp.ImageClass, "createImage", "(Ljavax/microedition/lcdui/Image;)Ljavax/microedition/lcdui/Image;", runtime.createImageCopy},
		{midp.ImageClass, "createImage", "(Ljavax/microedition/lcdui/Image;IIIII)Ljavax/microedition/lcdui/Image;", runtime.createImageRegion},
		{midp.ImageClass, "createImage", "(Ljava/lang/String;)Ljavax/microedition/lcdui/Image;", runtime.createImageFromResource},
		{midp.ImageClass, "createImage", "([BII)Ljavax/microedition/lcdui/Image;", runtime.createImageFromBytes},
		{midp.ImageClass, "createRGBImage", "([IIIZ)Ljavax/microedition/lcdui/Image;", runtime.createRGBImage},
		{midp.ImageClass, "getGraphics", "()Ljavax/microedition/lcdui/Graphics;", runtime.getImageGraphics},
		{midp.ImageClass, "getWidth", "()I", runtime.getImageWidth},
		{midp.ImageClass, "getHeight", "()I", runtime.getImageHeight},
		{midp.ImageClass, "isMutable", "()Z", runtime.isImageMutable},
		{midp.ImageClass, "getRGB", "([IIIIIII)V", runtime.getImageRGB},
		{jvm.ClassClass, "getResourceAsStream", "(Ljava/lang/String;)Ljava/io/InputStream;", runtime.getResourceAsStream},
	}
	for _, registration := range registrations {
		if err := runtime.registerNative(registration.class, registration.name, registration.descriptor, registration.method); err != nil {
			return fmt.Errorf("register %s.%s%s: %w", registration.class, registration.name, registration.descriptor, err)
		}
	}
	return nil
}

func (runtime *Runtime) getDisplay(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	owner, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if owner == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "Display.getDisplay MIDlet is null")
	}
	isMIDlet, err := vm.IsSubclassOf(owner.ClassName, midp.MIDletClass)
	if err != nil {
		return jvm.VoidValue(), fmt.Errorf("validate Display owner: %w", err)
	}
	if !isMIDlet {
		return jvm.VoidValue(), fmt.Errorf("Display owner %s is not a MIDlet", owner.ClassName)
	}

	runtime.displayMu.Lock()
	defer runtime.displayMu.Unlock()
	if runtime.displayOwner != nil && runtime.displayOwner != owner {
		return jvm.VoidValue(), fmt.Errorf("Display belongs to another MIDlet instance")
	}
	if runtime.display == nil {
		runtime.displayOwner = owner
		runtime.display = &jvm.Object{ClassName: midp.DisplayClass, Fields: make(map[string]jvm.Value)}
	}
	return jvm.ReferenceValue(runtime.display), nil
}

func (runtime *Runtime) getCurrentDisplayable(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if err := runtime.validateDisplayReceiver(arguments); err != nil {
		return jvm.VoidValue(), err
	}
	runtime.displayMu.RLock()
	current := runtime.currentDisplayable
	runtime.displayMu.RUnlock()
	return jvm.ReferenceValue(current), nil
}

func (runtime *Runtime) setCurrentDisplayable(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if err := runtime.validateDisplayReceiver(arguments); err != nil {
		return jvm.VoidValue(), err
	}
	next, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if next == nil {
		return jvm.VoidValue(), nil
	}
	isDisplayable, err := vm.IsSubclassOf(next.ClassName, midp.DisplayableClass)
	if err != nil {
		return jvm.VoidValue(), fmt.Errorf("validate current Displayable: %w", err)
	}
	if !isDisplayable {
		return jvm.VoidValue(), fmt.Errorf("current object %s is not a Displayable", next.ClassName)
	}

	runtime.displayMu.Lock()
	runtime.pendingDisplayable = next
	if runtime.displayUpdateQueued {
		runtime.displayMu.Unlock()
		return jvm.VoidValue(), nil
	}
	err = runtime.events.Post("Display.setCurrent", runtime.applyCurrentDisplayable)
	if err != nil {
		runtime.pendingDisplayable = nil
		runtime.displayMu.Unlock()
		return jvm.VoidValue(), fmt.Errorf("queue current Displayable: %w", err)
	}
	runtime.displayUpdateQueued = true
	runtime.displayMu.Unlock()
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) applyCurrentDisplayable() error {
	runtime.displayMu.Lock()
	previous := runtime.currentDisplayable
	next := runtime.pendingDisplayable
	runtime.currentDisplayable = next
	runtime.pendingDisplayable = nil
	runtime.displayUpdateQueued = false
	runtime.displayMu.Unlock()
	if runtime.logger != nil {
		runtime.logger.Debug("MIDP current Displayable changed", "from", objectClass(previous), "to", objectClass(next))
	}
	return runtime.repaintNewCurrentCanvas(next)
}

// maxPendingSerialRunnables bounds the callSerially queue. A game hands the
// event loop one Runnable at a time; a queue this long means the application
// is producing them faster than any pass can run them, and the archive is
// untrusted input.
const maxPendingSerialRunnables = 256

// callSeriallyRunnable queues a Runnable to run on the event loop after the
// repaints already on it, which is what MIDP promises. It is queued rather
// than posted, and RunPending takes one per pass.
//
// The distinction matters as much here as it does on the WIPI path. A frame
// loop is commonly written as a Runnable that draws and hands itself back to
// callSerially; posting it straight onto the event loop means it re-queues
// itself inside the same drain, the loop never empties, and the run ends on
// the events-per-run limit instead of a frame. One per pass makes the next one
// a Host frame away.
func (runtime *Runtime) callSeriallyRunnable(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if err := runtime.validateDisplayReceiver(arguments); err != nil {
		return jvm.VoidValue(), err
	}
	runnable, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if runnable == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "Display.callSerially Runnable is null")
	}
	if !vm.IsInstance(runnable, jvm.RunnableClass) {
		return jvm.VoidValue(), fmt.Errorf("serial object %s is not a Runnable", runnable.ClassName)
	}
	runtime.displayMu.Lock()
	defer runtime.displayMu.Unlock()
	if len(runtime.pendingSerial) >= maxPendingSerialRunnables {
		return jvm.VoidValue(), fmt.Errorf("pending serial Runnable count exceeds %d", maxPendingSerialRunnables)
	}
	runtime.pendingSerial = append(runtime.pendingSerial, runnable)
	return jvm.VoidValue(), nil
}

// postNextSerialRunnable moves at most one queued Runnable onto the event loop.
func (runtime *Runtime) postNextSerialRunnable() error {
	runtime.displayMu.Lock()
	if len(runtime.pendingSerial) == 0 || runtime.State() != StateActive {
		runtime.displayMu.Unlock()
		return nil
	}
	runnable := runtime.pendingSerial[0]
	runtime.pendingSerial = runtime.pendingSerial[1:]
	runtime.displayMu.Unlock()

	err := runtime.events.Post("Display.callSerially", func() error {
		_, err := runtime.VM.InvokeVirtual(runnable, "run", "()V")
		return err
	})
	if err != nil {
		return runtime.fail("queue serial Runnable", err)
	}
	return nil
}

func (runtime *Runtime) isDisplayableShown(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	displayable, err := referenceArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if displayable == nil {
		return jvm.VoidValue(), newGuestException("java/lang/NullPointerException", "Displayable receiver is null")
	}
	runtime.displayMu.RLock()
	shown := runtime.currentDisplayable == displayable
	runtime.displayMu.RUnlock()
	if shown && runtime.State() == StateActive {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

func (runtime *Runtime) validateDisplayReceiver(arguments []jvm.Value) error {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return err
	}
	if receiver == nil {
		return newGuestException("java/lang/NullPointerException", "Display receiver is null")
	}
	runtime.displayMu.RLock()
	display := runtime.display
	runtime.displayMu.RUnlock()
	if display == nil || receiver != display {
		return fmt.Errorf("invalid Display receiver")
	}
	return nil
}

func (runtime *Runtime) getAppProperty(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if err := validateMIDletReceiver(arguments); err != nil {
		return jvm.VoidValue(), err
	}
	key, err := javaString(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), fmt.Errorf("app property key: %w", err)
	}
	value, ok := runtime.AppProperty(key)
	if !ok {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(&jvm.Object{ClassName: "java/lang/String", Native: value}), nil
}

// systemProperties is what MIDP guarantees System.getProperty answers. A game
// reads them to decide what its host can do, so each one states what this
// runtime actually is rather than impersonating a handset: naming a real
// device would invite a title to take a path written for that device's quirks.
// The profile and configuration are the levels the class surface implements.
// The `m.` names below are the handset facts an SKT title reads through the
// same call. They are answered because a missing one is not read as "unknown":
// a title compares the vendor string against the one device it has a workaround
// for, and a null there throws inside the constructor that would have started
// its game thread. The MIDlet still reaches startApp, still shows a Canvas, and
// then never runs a frame — an absent property fails as far from its cause as a
// wrong one does.
//
// The subscriber number is the one the WIPI platforms answer with, since the
// question is about the handset rather than about which runtime is asking. The
// vendor deliberately names no real manufacturer, for the reason the MIDP names
// above do not.
var systemProperties = map[string]string{
	"microedition.platform":      "wfeature",
	"microedition.profiles":      "MIDP-1.0",
	"microedition.configuration": "CLDC-1.0",
	"microedition.encoding":      "ISO-8859-1",
	"microedition.locale":        "ko-KR",

	"MIN":                  wipic.SystemProperties["MIN"],
	"m.MIN":                wipic.SystemProperties["MIN"],
	"m.VENDER":             "wfeature",
	"m.CARRIER":            "SKT",
	"m.COLOR":              "7",
	"m.SK_VM":              "10",
	"com.xce.wipi.version": "",
}

// getSystemProperty answers the MIDP system properties. The specification says
// an unknown name is null rather than an error, and a title that probes for an
// optional capability depends on that: refusing would turn "this host does not
// have it" into a crash.
func (runtime *Runtime) getSystemProperty(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	key, err := javaString(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), fmt.Errorf("system property key: %w", err)
	}
	value, ok := systemProperties[key]
	if !ok {
		return jvm.ReferenceValue(nil), nil
	}
	return jvm.ReferenceValue(&jvm.Object{ClassName: "java/lang/String", Native: value}), nil
}

func (runtime *Runtime) notifyPaused(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if err := validateMIDletReceiver(arguments); err != nil {
		return jvm.VoidValue(), err
	}
	err := runtime.events.Post("MIDlet.notifyPaused", func() error {
		if runtime.State() == StateActive {
			runtime.transition("notifyPaused", StatePaused)
		}
		return nil
	})
	return jvm.VoidValue(), runtime.queueNotificationError("notifyPaused", err)
}

func (runtime *Runtime) notifyDestroyed(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if err := validateMIDletReceiver(arguments); err != nil {
		return jvm.VoidValue(), err
	}
	err := runtime.events.Post("MIDlet.notifyDestroyed", func() error {
		state := runtime.State()
		if state != StateDestroyed && state != StateError {
			runtime.transition("notifyDestroyed", StateDestroyed)
		}
		return nil
	})
	return jvm.VoidValue(), runtime.queueNotificationError("notifyDestroyed", err)
}

func (runtime *Runtime) resumeRequest(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if err := validateMIDletReceiver(arguments); err != nil {
		return jvm.VoidValue(), err
	}
	err := runtime.events.Post("MIDlet.resumeRequest", func() error {
		if runtime.State() != StatePaused {
			return nil
		}
		return runtime.resume(true)
	})
	return jvm.VoidValue(), runtime.queueNotificationError("resumeRequest", err)
}

func (runtime *Runtime) queueNotificationError(name string, err error) error {
	if err == nil {
		return nil
	}
	return runtime.fail("queue "+name, err)
}

func (runtime *Runtime) invalidState(action string, state LifecycleState) error {
	return fmt.Errorf("%w: cannot %s MIDlet in %s state", ErrInvalidLifecycleState, action, state)
}

func (runtime *Runtime) transition(event string, next LifecycleState) {
	runtime.stateMu.Lock()
	previous := runtime.state
	runtime.state = next
	runtime.stateMu.Unlock()
	if runtime.logger != nil {
		runtime.logger.Debug("MIDlet lifecycle transition", "event", event, "from", previous, "to", next)
	}
}

func (runtime *Runtime) fail(action string, cause error) error {
	failure := runtime.lifecycleFailure(action, cause)
	runtime.recordFailure(action, cause, failure, StateError)
	return failure
}

func (runtime *Runtime) lifecycleFailure(action string, cause error) error {
	return fmt.Errorf("%s MIDlet %s: %w", action, runtime.Archive.Descriptor.MainClass, cause)
}

func (runtime *Runtime) recordFailure(action string, cause, failure error, next LifecycleState) {
	runtime.stateMu.Lock()
	previous := runtime.state
	runtime.state = next
	runtime.lastError = failure
	runtime.stateMu.Unlock()
	if runtime.logger != nil {
		runtime.logger.Error("MIDlet lifecycle failed", "action", action, "from", previous, "to", next, "error", cause)
	}
}

func (runtime *Runtime) isRecordedFailure(err error) bool {
	runtime.stateMu.RLock()
	defer runtime.stateMu.RUnlock()
	return runtime.lastError != nil && errors.Is(err, runtime.lastError)
}

func validateMIDletReceiver(arguments []jvm.Value) error {
	receiver, err := referenceArgument(arguments, 0)
	if err != nil {
		return err
	}
	if receiver == nil {
		return fmt.Errorf("MIDlet receiver is null")
	}
	return nil
}

func javaString(arguments []jvm.Value, index int) (string, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return "", err
	}
	if object == nil {
		return "", fmt.Errorf("argument %d is null", index)
	}
	value, ok := object.Native.(string)
	if object.ClassName != "java/lang/String" || !ok {
		return "", fmt.Errorf("argument %d is not a string", index)
	}
	return value, nil
}

func referenceArgument(arguments []jvm.Value, index int) (*jvm.Object, error) {
	if index < 0 || index >= len(arguments) {
		return nil, fmt.Errorf("argument %d is missing", index)
	}
	object, err := arguments[index].Reference()
	if err != nil {
		return nil, fmt.Errorf("argument %d: %w", index, err)
	}
	return object, nil
}

func newGuestException(className, message string) error {
	return &jvm.GuestException{
		Object:  &jvm.Object{ClassName: className, Native: message},
		Message: message,
	}
}

func objectClass(object *jvm.Object) string {
	if object == nil {
		return ""
	}
	return object.ClassName
}

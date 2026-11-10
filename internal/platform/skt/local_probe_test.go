package skt_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/skt"
	"github.com/movingwoo/wfeature/internal/session"
)

// probeTitle is a started SKT archive as the local probes drive it. Most are
// MIDlets this package runs; an archive whose executable is a native `.daf`
// program is run by the SKT Clet engine, which these probes reach through the
// session a Host starts it with. The rungs ask both the same questions: before
// the native archives were driven this way, every one of them stopped at the
// boot rung on a missing MIDlet manifest while the CLI and the server played
// it.
type probeTitle interface {
	// tick runs one Host pass.
	tick() error
	// ended says why the title can no longer be asked anything, or "" while
	// it can.
	ended() string
	// presented is the frame on the screen and how many have been presented.
	presented() (backend.Frame, uint64)
	// key presses or releases one key, by the code the page sends for it.
	key(pressed bool, code int32) error
	close()
}

// startProbeTitle starts one archive the way a Host does, with its saves in the
// test's own directory: a probe must not read or write the progress a person
// made playing.
func startProbeTitle(t *testing.T, data []byte) probeTitle {
	t.Helper()
	saves := backend.NewDirectorySaveStore(t.TempDir())
	if skt.IsNativeArchive(data) {
		started, err := session.Start(context.Background(), data, session.Options{SaveStore: saves})
		if err != nil {
			t.Fatalf("start the native program: %v", err)
		}
		return &nativeTitle{session: started}
	}
	archive, err := skt.Open(data)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	framebuffer := localProbeFramebuffer(t, archive)
	runtime, err := skt.Start(archive, skt.Options{Framebuffer: framebuffer, SaveStore: saves})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return &midletTitle{runtime: runtime, framebuffer: framebuffer}
}

type midletTitle struct {
	runtime     *skt.Runtime
	framebuffer *backend.MemoryFramebuffer
}

func (title *midletTitle) tick() error {
	title.runtime.AdvanceAudio()
	return title.runtime.RunPending()
}

func (title *midletTitle) ended() string {
	switch title.runtime.State() {
	case skt.StateDestroyed:
		return "the MIDlet destroyed itself"
	case skt.StateError:
		return fmt.Sprintf("the MIDlet ended in error: %v", title.runtime.Summary().Error)
	}
	return ""
}

func (title *midletTitle) presented() (backend.Frame, uint64) { return title.framebuffer.Snapshot() }

func (title *midletTitle) key(pressed bool, code int32) error {
	event := skt.KeyReleased
	if pressed {
		event = skt.KeyPressed
	}
	return title.runtime.SendKey(event, code)
}

func (title *midletTitle) close() { _ = title.runtime.Destroy(true) }

// nativeTitle is a native program in the session a Host plays it in, ticked
// with the budget a server's pass has. A present is a pass that leaves a frame
// on the screen.
type nativeTitle struct {
	session  *session.Session
	presents uint64
	exited   string
}

// nativeTickBudget is how much guest time one pass covers, as a server's does.
const nativeTickBudget = 32 * time.Millisecond

func (title *nativeTitle) tick() error {
	if title.exited != "" {
		return errors.New(title.exited)
	}
	_, err := title.session.Tick(context.Background(), nativeTickBudget)
	if errors.Is(err, session.ErrExited) {
		title.exited = "the program ended itself: " + title.session.ExitReason()
		return nil
	}
	if err != nil {
		return err
	}
	if _, _, _, ok := title.session.Frame(); ok {
		title.presents++
	}
	return nil
}

func (title *nativeTitle) ended() string { return title.exited }

func (title *nativeTitle) presented() (backend.Frame, uint64) {
	rgba, width, height, ok := title.session.Frame()
	if !ok {
		return backend.Frame{}, title.presents
	}
	return backend.Frame{Width: width, Height: height, RGBA: rgba}, title.presents
}

func (title *nativeTitle) key(pressed bool, code int32) error {
	action := session.KeyRelease
	if pressed {
		action = session.KeyPress
	}
	err := title.session.SendKey(context.Background(), action, code)
	if errors.Is(err, session.ErrExited) {
		title.exited = "the program ended itself: " + title.session.ExitReason()
		return nil
	}
	return err
}

func (title *nativeTitle) close() { title.session.Close() }

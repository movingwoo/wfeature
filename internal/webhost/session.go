package webhost

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/wsproto"
)

// A session runs the emulator here, on the server, and sends the phone
// pictures. That is the whole point of this file: the page used to emulate for
// itself, and measured fifteen times slower on a phone than on a desktop for
// reasons that were Go's WebAssembly backend rather than the emulator, so the
// only way a phone plays at full speed is for it to stop emulating. Natively a frame costs a few percent of
// one core, and a finished frame is small enough that a home network carries
// twenty a second.
//
// One connection is one session. The connection closing ends the game, which
// is the same rule the page already had — a reload was always a fresh session
// — and it means nothing can outlive the thing that was watching it.

const (
	// tickBudget is how much guest execution one pass covers. A service round
	// cannot be cut short, so this bounds how many rounds are started rather
	// than how long one takes.
	//
	// The browser uses eight milliseconds because its Host shares a thread
	// with drawing the page, and a longer entry is a longer freeze. Nothing
	// here shares anything: the emulator has its own goroutine and the frames
	// are encoded on another. What the budget still buys is a bound on how
	// long a key waits behind a busy game, which is why it is not simply
	// removed.
	//
	// It matters that it is not too small. An entry that stops on its budget
	// is read by the platform as saturation — the Host used more time than the
	// guest's schedule left it — and that reading is what makes a session give
	// up frames to keep the game's logic on time. In a browser it is true. On
	// a server that re-enters immediately and for free, a budget-capped entry
	// says nothing except that the budget is small, so a budget short enough
	// to cut ordinary entries would have the session dropping frames on a
	// machine with a core to spare. Thirty-two milliseconds is under one frame
	// of input delay at the rate these games run, and long enough that an
	// entry normally ends where it should: at the guest parking.
	tickBudget = 32 * time.Millisecond

	// commandBuffer is how many messages from the page may queue while the
	// emulator is inside a tick. Input is the only thing that arrives in
	// bursts, and a burst deeper than this is a stuck key rather than a
	// player.
	commandBuffer = 64

	// statsInterval is how often the page is told what the session costs.
	statsInterval = 2 * time.Second

	// maxSessionMessage bounds a message from the page. Everything it sends is
	// a handful of fields; a cheat table load is the largest and is still
	// small.
	maxSessionMessage = 1 << 20

	// outboundBuffer is how many finished messages may wait for the socket.
	// Nothing but audio arrives fast enough to fill it, and a backlog deeper
	// than this is a connection that has stopped rather than one that is
	// behind.
	outboundBuffer = 64

	// writeTimeout bounds one write to the page. A phone that goes into a
	// tunnel stops reading without closing anything, and a socket with no
	// deadline on it waits for that forever — holding the last frame, and
	// every message queued behind it, until the operating system gives up
	// minutes later. Ten seconds is far longer than any write here takes and
	// far shorter than that.
	writeTimeout = 10 * time.Second
)

// outboundMessage is one thing on its way to the page. Text and pictures share
// a writer because they share a socket: two goroutines writing to one
// connection serialise on its write mutex anyway, and the one that loses is
// whichever asked second — which used to be the emulator. See writeMessages.
type outboundMessage struct {
	text   string
	binary []byte
}

// serveSession upgrades the connection and runs one game on it.
func (s *Server) serveSession(writer http.ResponseWriter, request *http.Request) {
	upgrader := wsproto.Upgrader{MaxMessageSize: maxSessionMessage}
	connection, err := upgrader.Accept(writer, request)
	if err != nil {
		// Accept has already answered the request; there is nothing to add.
		s.logger.Debug("session upgrade refused", "error", err)
		return
	}
	defer connection.Close()

	runner := &sessionRunner{
		server:     s,
		connection: connection,
		commands:   make(chan clientMessage, commandBuffer),
		frames:     make(chan pendingFrame, 1),
		outText:    make(chan outboundMessage, outboundBuffer),
		outFrames:  make(chan outboundMessage, 1),
		writerDone: make(chan struct{}),
	}
	runner.run(request.Context())
}

// pendingFrame is one finished picture on its way to the page. It carries its
// own copy of the pixels: the session's buffer is overwritten by the next
// tick, and the encoder runs on another goroutine.
type pendingFrame struct {
	rgba          []byte
	width, height int
}

type sessionRunner struct {
	server     *Server
	connection *wsproto.Conn

	commands chan clientMessage
	frames   chan pendingFrame

	// outText and outFrames are what the writer goroutine drains. They are
	// separate because their backlogs mean opposite things. Text is small and
	// mostly droppable, so a queue of it is cheap and worth keeping; a queue
	// of pictures is stale by the time it goes out, and a phone that is behind
	// is made later still by receiving it. One slot means the encoder blocks
	// on the second frame, which is what pushFrame is already reading when it
	// drops one — so the backpressure arrives where the session already knows
	// how to answer it.
	outText   chan outboundMessage
	outFrames chan outboundMessage
	// writerDone is closed when the writer goroutine has gone, so a send that
	// cannot be dropped waits for the socket rather than forever.
	writerDone chan struct{}

	game      *session.Session
	label     string
	platform  string
	presented uint64
	// saveDirectory is the claim this session holds on the game's saves for
	// as long as it has the game; see saveclaim.go. Empty for a game with no
	// saves of its own.
	saveDirectory string

	// started is what the page was told when the game came up, kept so a page
	// that reconnects can be told the same thing without the game restarting.
	started startedMessage
	// token names this game while its page is away; see resume.go. It is
	// issued when a game starts and spent when one is resumed.
	token string

	// gameCtx is the running game's own lifetime, and it is deliberately not
	// this connection's. A guest thread parks inside a Go call that captured
	// the context it was last granted a slice under, so a session ticked with
	// the socket's context dies the moment the socket does — with
	// `context canceled` surfacing from inside guest code on the next tick,
	// which is exactly the tick a resumed game takes. The game's context ends
	// when the game does: gameCancel runs when it is closed, and never when
	// its page merely goes away.
	gameCtx    context.Context
	gameCancel context.CancelFunc

	// postMortem is the report composed at the moment a game died, kept
	// because the diagnostics go with the session when it is closed. A game
	// that exits on its first tick is exactly the one worth a report, and
	// asking for one afterwards used to answer "no game is running".
	postMortem string

	audio *audioCollector

	// stats accumulate between the reports sent to the page. The page cannot
	// measure any of this itself any more — the emulator is not in it.
	//
	// sent, frameBytes and shed are counted by the writer goroutine and read
	// by the emulator's, so they are atomic; the rest never leave the emulator
	// loop.
	sent       atomic.Uint64
	frameBytes atomic.Uint64
	// shed counts droppable messages thrown away because the connection was
	// behind. It is the difference between a session the host cannot keep up
	// with and one the link cannot: the first shows in the tick cost and the
	// second shows here.
	//
	// It is two counters because it answers two questions. The page is shown a
	// rate, so its window is emptied every time one is sent; the session report
	// is a post-mortem and wants the whole run, and reading the window for it
	// answered with however little had been shed since the last report -- a
	// session that shed thousands early and none lately said zero.
	shed       atomic.Uint64
	shedTotal  atomic.Uint64
	skipped    uint64
	ticks      uint64
	tickTotal  time.Duration
	statsSince time.Time
	// guestSince is the guest clock at the start of the stats window, and
	// guestMarked says whether there is one to compare against — a game that
	// has just started or restarted has a clock that begins again at zero.
	guestSince  time.Duration
	guestMarked bool
}

func (r *sessionRunner) run(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	var waiting sync.WaitGroup
	waiting.Add(3)
	go func() { defer waiting.Done(); r.readCommands(cancel) }()
	go func() { defer waiting.Done(); r.writeFrames(ctx) }()
	go func() { defer waiting.Done(); r.writeMessages(ctx, cancel) }()

	r.send(serverMessage{Kind: serverReady, Profile: r.server.profile})
	r.loop(ctx)

	// A game still running here is one whose page went away rather than one
	// that ended: the loop only returns with a game in hand when the socket
	// closed under it. That game is parked instead of closed, so a phone that
	// backgrounded the page has something to come back to; see resume.go.
	// Everything else — a game that exited, a game the page stopped — has
	// already cleared this and is not parked.
	if r.game != nil {
		r.park()
	}
	close(r.frames)
	// The loop only returns once the socket is already gone, so nothing is
	// still owed to the page: cancelling here releases the encoder and the
	// writer from whatever they were waiting on rather than discarding a
	// message someone is still reading.
	cancel()
	_ = r.connection.Close()
	waiting.Wait()
}

// readCommands turns the page's messages into commands. It runs on its own
// goroutine because the emulator loop is usually inside guest code, and a key
// that arrives then has to be waiting when the loop next looks.
func (r *sessionRunner) readCommands(cancel context.CancelFunc) {
	defer cancel()
	defer close(r.commands)
	for {
		text, err := r.connection.ReadText()
		if err != nil {
			if !errors.Is(err, wsproto.ErrClosed) {
				r.server.logger.Debug("session read ended", "error", err)
			}
			return
		}
		var message clientMessage
		if err := decodeClientMessage(text, &message); err != nil {
			r.send(serverMessage{Kind: serverError, Message: err.Error()})
			continue
		}
		select {
		case r.commands <- message:
		default:
			// The queue is full, which means the emulator is not keeping up
			// with input rather than that this message is special. Dropping
			// the newest keeps the ones already accepted in order.
			r.server.logger.Debug("session command dropped", "kind", message.Kind)
		}
	}
}

// pngBuffers holds the compressor a PNG encode needs, so that encoding a
// stream of frames stops allocating one per frame.
//
// A game presents a picture twenty-odd times a second for as long as it is
// played, and png.Encoder builds a fresh zlib writer for every Encode call
// without this: 1.25 MB and thirty allocations a frame, measured on a 240x320
// frame, against 8 KB and none with it. That garbage is collected on the
// machine that is also running the emulator, so it is the guest's time it
// costs. The pool is shared across sessions because the buffers are
// interchangeable and a household server runs few sessions at once.
//
// It is the shape png.EncoderBufferPool asks for. Encoded bytes are identical
// either way — the pool holds scratch space, not state.
type pngBufferPool struct{ pool sync.Pool }

func (p *pngBufferPool) Get() *png.EncoderBuffer {
	buffer, _ := p.pool.Get().(*png.EncoderBuffer)
	return buffer
}

func (p *pngBufferPool) Put(buffer *png.EncoderBuffer) { p.pool.Put(buffer) }

var pngBuffers = &pngBufferPool{}

// writeFrames encodes pictures and hands them to the writer. Encoding is off
// the emulator's goroutine so a slow compression pass costs frames rather than
// guest speed, and the write itself is off it too — see writeMessages for why
// that took a goroutine of its own rather than this one.
func (r *sessionRunner) writeFrames(ctx context.Context) {
	encoder := png.Encoder{CompressionLevel: png.BestSpeed, BufferPool: pngBuffers}
	buffer := &bytes.Buffer{}
	for frame := range r.frames {
		picture := &image.RGBA{
			Pix:    frame.rgba,
			Stride: frame.width * 4,
			Rect:   image.Rect(0, 0, frame.width, frame.height),
		}
		buffer.Reset()
		if err := encoder.Encode(buffer, picture); err != nil {
			r.server.logger.Warn("frame could not be encoded", "error", err)
			continue
		}
		// The encoder's buffer is reused, so the picture travels as its own
		// bytes. Waiting here for the writer is deliberate: it is what makes
		// pushFrame drop the next one rather than queue it.
		encoded := append([]byte(nil), buffer.Bytes()...)
		select {
		case r.outFrames <- outboundMessage{binary: encoded}:
		case <-r.writerDone:
			return
		case <-ctx.Done():
			return
		}
	}
}

// loop is the emulator. It owns the session: guest code is not re-entrant, so
// every command that touches it is handled here rather than where it arrived.
func (r *sessionRunner) loop(ctx context.Context) {
	r.statsSince = time.Now()
	for {
		if ctx.Err() != nil {
			return
		}
		if r.game == nil {
			// Nothing is running: wait for a command rather than spin.
			select {
			case <-ctx.Done():
				return
			case message, ok := <-r.commands:
				if !ok {
					return
				}
				r.handle(ctx, message)
			}
			continue
		}

		entered := time.Now()
		progress, err := r.game.Tick(r.gameCtx, tickBudget)
		r.ticks++
		r.tickTotal += time.Since(entered)

		if progress.Flushes != r.presented {
			r.presented = progress.Flushes
			r.pushFrame()
		}
		r.flushAudio()
		r.reportStats()

		if err != nil {
			r.endGame("the game failed: "+err.Error(), true)
			r.send(serverMessage{Kind: serverError, Message: err.Error()})
			continue
		}
		if progress.Exited {
			// **An ending says where it came from.** A game of this era ends
			// itself for ordinary reasons, and a page that reports only "the
			// game exited" leaves its reader unable to tell a quit menu from a
			// title that gave up on its first screen — which is how a working
			// game gets written down as a broken one.
			r.endGame(endedByExit(progress.ExitReason), true)
			r.send(serverMessage{Kind: serverExited, Message: progress.ExitReason})
			continue
		}

		// The guest's own next deadline is what paces the game. Waiting on the
		// command channel rather than sleeping means a key does not have to
		// wait out the guest's idle time to be delivered.
		r.drainCommands(ctx, progress.Wait)
	}
}

// endedByExit is the cause line for a game that ended itself. The reason is the
// platform's own text, and it is appended rather than replacing the sentence so
// that the reports of every profile still start the same way — a sweep greps
// the first half, a person reads the second.
func endedByExit(reason string) string {
	if reason == "" {
		return "the game exited"
	}
	return "the game exited: " + reason
}

// drainCommands handles everything queued, then waits out the guest's idle
// time if it asked for any.
func (r *sessionRunner) drainCommands(ctx context.Context, wait time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-r.commands:
			if !ok {
				return
			}
			r.handle(ctx, message)
			continue
		default:
		}
		break
	}
	if wait <= 0 {
		return
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	case message, ok := <-r.commands:
		if ok {
			r.handle(ctx, message)
		}
	}
}

func (r *sessionRunner) handle(ctx context.Context, message clientMessage) {
	switch message.Kind {
	case clientStart:
		r.startGame(ctx, message)
	case clientResume:
		r.resumeGame(message)
	case clientKey:
		if r.game == nil {
			return
		}
		switch err := r.game.SendKey(r.gameCtx, message.Action, message.Code); {
		case err == nil:
		case errors.Is(err, session.ErrExited):
			// A game that ends on a key press has ended, not failed.
			r.endGame(endedByExit(r.game.ExitReason()), true)
			r.send(serverMessage{Kind: serverExited, Message: r.game.ExitReason()})
		default:
			r.send(serverMessage{Kind: serverError, Message: err.Error()})
		}
	case clientPointer:
		if r.game == nil {
			return
		}
		switch err := r.game.SendPointer(r.gameCtx, message.Action, message.X, message.Y); {
		case err == nil:
		case errors.Is(err, session.ErrNoPointer):
			// The page was told at the start whether this game takes a touch,
			// so one arriving here is a page that ignored the answer. It is
			// dropped rather than reported: the alternative is an error rail
			// filling up because somebody rested a thumb on the canvas.
		case errors.Is(err, session.ErrExited):
			r.endGame(endedByExit(r.game.ExitReason()), true)
			r.send(serverMessage{Kind: serverExited, Message: r.game.ExitReason()})
		default:
			r.send(serverMessage{Kind: serverError, Message: err.Error()})
		}
	case clientSpeed:
		if r.game != nil {
			r.game.SetSpeed(message.Value)
		}
	case clientScale:
		if r.game != nil {
			r.game.SetScale(int(message.Value))
			// The next frame is the magnified one, and the game may not draw
			// again for a while, so the current picture is resent at the new
			// size rather than leaving the page on the old one.
			r.pushFrame()
		}
	case clientCheat:
		r.runCheat(message)
	case clientReport:
		r.storeReport(message)
	case clientStop:
		// The numbers are kept even on a deliberate stop, so a report can be
		// asked for once the game is off the screen.
		r.endGame("the page stopped the game", false)
	default:
		r.send(serverMessage{Kind: serverError, ID: message.ID,
			Message: fmt.Sprintf("unknown message kind %q", message.Kind)})
	}
}

// The screen sizes a page may ask for. The floor is smaller than any handset
// these games shipped on and the ceiling is larger, because what this bounds is
// an allocation from a message rather than a taste in phones.
const (
	minScreen = 32
	maxScreen = 1024
)

func validScreen(width, height int) bool {
	return width >= minScreen && width <= maxScreen && height >= minScreen && height <= maxScreen
}

func (r *sessionRunner) startGame(ctx context.Context, message clientMessage) {
	r.stopGame()

	archive, label, err := r.server.readGameArchive(message.Game)
	if err != nil {
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: err.Error()})
		return
	}
	summary, err := session.Inspect(archive)
	if err != nil {
		// A bag of games is the one refusal a person can act on, so it is
		// said the way the rest of this page speaks to them: what the file
		// is, and what to do with it.
		if errors.Is(err, session.ErrArchiveOfArchives) {
			r.send(serverMessage{Kind: serverError, ID: message.ID,
				Message: "이 zip 안에는 다른 zip만 들어 있습니다. 게임이 아니라 게임 여러 개를 담은 묶음이니, 압축을 풀어 하나씩 추가하세요."})
			return
		}
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: err.Error()})
		return
	}

	// Two sessions on one save directory overwrite each other silently, so
	// the directory is claimed before anything is started; see saveclaim.go.
	directory := r.server.saveDirectory(summary.Platform, summary.SaveOwner)
	if claimed, holder := r.server.waitToClaimSaveDirectory(directory, label); !claimed {
		r.send(serverMessage{Kind: serverError, ID: message.ID,
			Message: fmt.Sprintf("다른 창에서 이미 실행 중입니다(%s). 그 창에서 게임을 멈추거나 창을 닫은 뒤 다시 시작하세요.", holder)})
		return
	}
	r.saveDirectory = directory

	r.audio = &audioCollector{}
	// The game's context keeps the request's values and drops its
	// cancellation: what ends this game is closing it, not the page that
	// happened to start it going away.
	r.gameCtx, r.gameCancel = context.WithCancel(context.WithoutCancel(ctx))
	scale := 1
	if message.Value >= 1 && message.Value <= 4 {
		scale = int(message.Value)
	}
	// A screen the page did not ask for is the default one, and a size outside
	// what a handset ever had is refused rather than clamped: it would come
	// from a page this server does not serve, and a game started on a screen
	// nothing was drawn for is worse than a game that did not start.
	screenWidth, screenHeight := 0, 0
	if message.Width != 0 || message.Height != 0 {
		if !validScreen(message.Width, message.Height) {
			r.send(serverMessage{Kind: serverError, ID: message.ID,
				Message: fmt.Sprintf("screen %dx%d is outside %d..%d", message.Width, message.Height, minScreen, maxScreen)})
			return
		}
		screenWidth, screenHeight = message.Width, message.Height
	}
	started, err := session.Start(r.gameCtx, archive, session.Options{
		SaveStore: r.server.saveStoreIn(directory),
		AudioSink: r.audio,
		Logger:    r.server.logger,
		Scale:     scale,
		Width:     screenWidth,
		Height:    screenHeight,
		// A debug build is the one that collects a report, and the ordered
		// trace is what it collects.
		TraceLimit: r.server.traceLimit,
	})
	if err != nil {
		r.gameCancel()
		r.gameCancel = nil
		r.server.releaseSaveDirectory(r.saveDirectory)
		r.saveDirectory = ""
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: err.Error()})
		return
	}
	r.game = started
	r.label = label
	r.platform = summary.Platform
	r.presented = 0
	r.postMortem = ""
	if r.server.traceLimit > 0 {
		// The phase costs say which part of a round is expensive; only the
		// guest profile says which guest code is. A build that already pays
		// for the trace can afford a stack walk every thousand instructions.
		started.KTF().EnableProfile(0)
	}
	// The token travels with the game's identity because that is the moment
	// there is something to come back to. A page that never hears one — a
	// server whose randomness failed — simply cannot resume, which is the
	// behaviour this had before parking existed.
	if token, tokenErr := newResumeToken(); tokenErr == nil {
		r.token = token
	} else {
		r.token = ""
		r.server.logger.Warn("session resume token unavailable", "error", tokenErr)
	}
	// The size reported is the one the platform took rather than the one the
	// page asked for: a platform may answer with a size of its own, and a page
	// that laid out for a screen the game is not drawing has the picture in
	// the wrong place.
	startedWidth, startedHeight := started.Screen()
	r.server.logger.Info("session started",
		"game", label, "platform", summary.Platform, "owner", summary.SaveOwner,
		"screen", fmt.Sprintf("%dx%d", startedWidth, startedHeight))
	r.started = startedMessage{
		Platform:  summary.Platform,
		AID:       summary.AID,
		PID:       summary.PID,
		Name:      summary.Name,
		SaveOwner: summary.SaveOwner,
		MainClass: summary.MainClass,
		Width:     startedWidth,
		Height:    startedHeight,
		Token:     r.token,
		CanWatch:  started.Cheat() != nil && started.Cheat().CanWatch(),
		CanTouch:  started.HasPointer(),
	}
	identity := r.started
	r.send(serverMessage{Kind: serverStarted, ID: message.ID, Started: &identity})
	// A game that painted while starting has a picture already.
	r.presented = started.Flushes()
	r.pushFrame()
	r.flushAudio()
}

// endGameContext releases the game's own lifetime. It runs where a game really
// ends and nowhere else — parking hands the context on with the game.
func (r *sessionRunner) endGameContext() {
	if r.gameCancel != nil {
		r.gameCancel()
		r.gameCancel = nil
	}
	r.gameCtx = nil
}

// park hands the running game to the server to hold under this runner's token,
// and forgets it here. The runner is about to end; the game is not.
func (r *sessionRunner) park() {
	game := r.game
	r.game = nil
	// The game is told before it is handed over. A handset suspended an
	// application when a call arrived and called its pause entry point, and
	// this is the same moment: nobody is watching, the game stops being
	// ticked, and a title that wants to stop its own animation or note the
	// time is only able to because it was asked. A title with nothing to do
	// answers immediately, and a platform with no lifecycle answers nil.
	//
	// A failure here is not a reason to lose the game. The page may still come
	// back for it, and a title that failed its pause is one to report rather
	// than one to throw away — the resume that follows is what will find out
	// whether the game is still alive.
	if r.gameCtx != nil {
		if err := game.Pause(r.gameCtx); err != nil {
			r.server.logger.Warn("pausing the game before parking failed", "game", r.label, "error", err)
		}
	}
	if r.token == "" {
		// No token was ever issued, so no page could ask for this game back.
		game.Close()
		r.server.releaseSaveDirectory(r.saveDirectory)
		r.saveDirectory = ""
		r.endGameContext()
		return
	}
	// The game keeps its save directory while it waits, and a start that finds
	// the claim parked may take it; see saveclaim.go.
	r.server.markSaveDirectoryParked(r.saveDirectory, true)
	r.server.parkSession(r.token, &parkedSession{
		game:          game,
		context:       r.gameCtx,
		cancel:        r.gameCancel,
		label:         r.label,
		platform:      r.platform,
		saveDirectory: r.saveDirectory,
		audio:         r.audio,
		started:       r.started,
		postMortem:    r.postMortem,
		presented:     r.presented,
	})
	// The context went with the game; this runner is not the one that ends it.
	r.gameCtx, r.gameCancel = nil, nil
	r.saveDirectory = ""
}

// resumeGame adopts a game the server has been holding. The page that asks
// already knows what it was playing, so the answer is the same `started` it
// got the first time, followed by the picture the game had when its page left.
func (r *sessionRunner) resumeGame(message clientMessage) {
	parked, ok := r.server.resumeSession(message.Token)
	if !ok {
		// An expired or unknown token is not an error the page can act on
		// beyond forgetting it, and it is the ordinary case after a long
		// absence, so it answers rather than fails.
		r.send(serverMessage{Kind: serverResumed, ID: message.ID, Resumed: false,
			Message: "이어서 진행할 게임이 없습니다."})
		return
	}
	// Whatever this connection had is not what the page asked for.
	r.stopGame()

	r.game = parked.game
	r.saveDirectory = parked.saveDirectory
	r.server.markSaveDirectoryParked(r.saveDirectory, false)
	r.gameCtx = parked.context
	r.gameCancel = parked.cancel
	r.label = parked.label
	r.platform = parked.platform
	r.audio = parked.audio
	r.started = parked.started
	r.postMortem = parked.postMortem
	r.presented = parked.presented
	r.token = message.Token

	// The other half of the pause above, and the half that matters most: the
	// clock moved while the game was parked, the same way it moves for a
	// handset that was suspended, and this is the call that lets a title
	// notice. It runs before the page is answered, so the picture that follows
	// is the one the resumed game has.
	if r.gameCtx != nil {
		if err := r.game.Resume(r.gameCtx); err != nil {
			r.server.logger.Warn("resuming the game failed", "game", r.label, "error", err)
		}
	}
	r.server.logger.Info("session resumed", "game", r.label)
	identity := r.started
	r.send(serverMessage{Kind: serverStarted, ID: message.ID, Started: &identity})
	// The game did not move while it was parked, so the picture it had is the
	// picture to show — the page has nothing on its canvas after reconnecting.
	r.pushFrame()
	r.flushAudio()
}

func (r *sessionRunner) stopGame() {
	if r.game == nil {
		return
	}
	r.game.Close()
	r.game = nil
	r.server.releaseSaveDirectory(r.saveDirectory)
	r.saveDirectory = ""
	r.endGameContext()
	// The token named this game; a page that reconnects after stopping one is
	// starting something new rather than resuming.
	r.token = ""
	if r.audio != nil {
		// Whatever was sounding when the game ended has to be released, or the
		// page holds the last note forever.
		r.send(serverMessage{Kind: serverAudio, Audio: []audioEvent{{Kind: audioAllOff}}})
	}
}

// pushFrame hands the current picture to the encoder, or drops it when the
// previous one has not gone out yet. Dropping is the right answer: the game
// has already moved on, and sending a backlog of stale pictures to a phone
// that is behind makes it later still.
func (r *sessionRunner) pushFrame() {
	if r.game == nil {
		return
	}
	rgba, width, height, ok := r.game.Frame()
	if !ok {
		return
	}
	// The frame is handed straight to the encoder's goroutine: session.Frame
	// answers with bytes that are the caller's, so copying them here would be
	// copying a copy — a third of a megabyte per frame, made only to be
	// collected.
	select {
	case r.frames <- pendingFrame{rgba: rgba, width: width, height: height}:
	default:
		r.skipped++
	}
}

func (r *sessionRunner) flushAudio() {
	if r.audio == nil {
		return
	}
	events := r.audio.take()
	if len(events) == 0 {
		return
	}
	r.sendDroppable(serverMessage{Kind: serverAudio, Audio: events})
}

func (r *sessionRunner) reportStats() {
	elapsed := time.Since(r.statsSince)
	if elapsed < statsInterval {
		return
	}
	sent := r.sent.Swap(0)
	frameBytes := r.frameBytes.Swap(0)
	stats := statsMessage{
		Fps:      float64(sent) / elapsed.Seconds(),
		Skipped:  r.skipped,
		Shed:     r.shed.Swap(0),
		TickRate: float64(r.ticks) / elapsed.Seconds(),
	}
	if r.ticks > 0 {
		stats.TickMillis = float64(r.tickTotal.Microseconds()) / float64(r.ticks) / 1000
	}
	if sent > 0 {
		stats.FrameBytes = int(frameBytes / sent)
	}
	stats.Speed = r.guestSpeed(elapsed, true)
	r.sendDroppable(serverMessage{Kind: serverStats, Stats: &stats})
	r.skipped, r.ticks, r.tickTotal = 0, 0, 0
	r.statsSince = time.Now()
}

// guestSpeed is how much guest time the window bought, against how much real
// time it cost.
//
// This is the number the other statistics cannot produce between them. A
// session short of time drops no frames and reports no error: it hands the
// game less clock than the wall gave it and everything else stays plausible.
// Reading a tick cost against the frame rate only says so if you already know
// how many ticks the game takes per picture, which varies by title and by
// scene. This says it outright.
//
// **Below one is the reading worth acting on.** A session that keeps up sits
// at one, because the platforms pace a tick of guest time to a tick of real
// time and wait out the difference. Short of processor there is no difference
// left to wait out, and the shortfall lands here in proportion: three quarters
// means the game is playing a quarter slower than it was written to.
//
// Above one is not a session running fast. A guest clock advances by the
// larger of the tick it stands for and the work the guest actually did — a
// frame that takes a modelled handset longer than its own timer asked for took
// that long, and saying otherwise would be claiming a phone the game never ran
// on. Loading a world does it every time. It says the guest was busy, not that
// the server was quick, and nothing here is wrong when it happens.
//
// It answers zero when there is nothing to compare — no game, a platform
// without a clock of its own, or the first window after one started, whose
// guest clock began again at zero.
//
// mark moves the window's start to now, which the report that closes a window
// does and the one composed part way through a window must not: the mark and
// statsSince are a pair, and moving one without the other would measure guest
// time over the wrong stretch of wall time.
func (r *sessionRunner) guestSpeed(elapsed time.Duration, mark bool) float64 {
	guest, ok := time.Duration(0), false
	if r.game != nil {
		guest, ok = r.game.GuestElapsed()
	}
	if !ok {
		if mark {
			r.guestMarked = false
		}
		return 0
	}
	marked, since := r.guestMarked, r.guestSince
	if mark {
		r.guestSince, r.guestMarked = guest, true
	}
	if !marked || elapsed <= 0 || guest < since {
		return 0
	}
	return float64(guest-since) / float64(elapsed)
}

// runCheat drives the cheat engine on behalf of the panel. Every operation
// runs here, on the emulator's own goroutine, because reading and freezing
// guest memory is only safe between ticks.
func (r *sessionRunner) runCheat(message clientMessage) {
	if r.game == nil || r.game.Cheat() == nil {
		// The engine needs a flat guest address space to sweep, and saying so
		// beats a silent failure on a platform where the panel should not have
		// appeared.
		r.send(serverMessage{Kind: serverResult, ID: message.ID,
			Message: "this game has no searchable guest memory",
			Cheat:   &cheatResult{},
		})
		return
	}
	if message.Op == "console" || message.Op == "" {
		console := r.game.CheatConsole()
		if console == nil {
			r.send(serverMessage{Kind: serverResult, ID: message.ID, Message: "this session has no cheat engine"})
			return
		}
		console.SetGame(r.label)
		text := console.Execute(message.Command)
		r.send(serverMessage{Kind: serverResult, ID: message.ID, Message: text,
			Cheat: &cheatResult{Text: text, Searchable: true}})
		return
	}

	result, err := runCheatOperation(r.game.Cheat(), r.label, message)
	if err != nil {
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: err.Error()})
		return
	}
	result.Searchable = true
	r.send(serverMessage{Kind: serverResult, ID: message.ID, Cheat: result.normalize()})
}

// runCheatOperation is the panel's vocabulary. It is a switch over names
// rather than a method per message so that adding one is a case, and so that
// the shapes it answers stay the ones internal/cheat defines.
func runCheatOperation(engine *cheat.Session, game string, message clientMessage) (*cheatResult, error) {
	switch message.Op {
	case "scan":
		candidates, err := cheat.PanelScan(engine, message.Type, message.Filter, message.Operand)
		if err != nil {
			return nil, err
		}
		return &cheatResult{Count: candidates.Count, Items: candidates.Items}, nil
	case "refresh":
		candidates, err := cheat.PanelRefresh(engine)
		if err != nil {
			return nil, err
		}
		return &cheatResult{Count: candidates.Count, Items: candidates.Items}, nil
	case "undo":
		candidates, err := cheat.PanelUndo(engine)
		if err != nil {
			return nil, err
		}
		return &cheatResult{Count: candidates.Count, Items: candidates.Items}, nil
	case "reset":
		candidates, err := cheat.PanelReset(engine)
		if err != nil {
			return nil, err
		}
		return &cheatResult{Count: candidates.Count, Items: candidates.Items}, nil
	case "freeze":
		frozen, err := cheat.PanelFreezeValue(engine, message.Address, message.Operand, message.Type)
		if err != nil {
			return nil, err
		}
		return &cheatResult{Frozen: frozen}, nil
	case "unfreeze":
		frozen, err := cheat.PanelUnfreeze(engine, message.Address)
		if err != nil {
			return nil, err
		}
		return &cheatResult{Frozen: frozen}, nil
	case "frozen":
		return &cheatResult{Frozen: cheat.PanelFrozen(engine)}, nil
	case "watch":
		watches, err := cheat.PanelWatch(engine, message.Address)
		if err != nil {
			return nil, err
		}
		return &cheatResult{Watches: watches}, nil
	case "unwatch":
		watches, err := cheat.PanelUnwatch(engine, message.Address, message.All)
		if err != nil {
			return nil, err
		}
		return &cheatResult{Watches: watches}, nil
	case "hits":
		hits, err := cheat.PanelWatchHits(engine)
		if err != nil {
			return nil, err
		}
		return &cheatResult{Hits: &hits}, nil
	case "saveTable":
		table, err := cheat.PanelSaveTable(engine, game)
		if err != nil {
			return nil, err
		}
		return &cheatResult{Table: table}, nil
	case "loadTable":
		applied, _, err := cheat.PanelLoadTable(engine, message.Table)
		if err != nil {
			return nil, err
		}
		return &cheatResult{Applied: applied, Frozen: cheat.PanelFrozen(engine)}, nil
	}
	return nil, fmt.Errorf("unknown cheat operation %q", message.Op)
}

// storeReport writes the session's diagnostics where the debug-log API puts
// the browser's. The report is written by the side that has the numbers, which
// is now this one — a page that is only drawing pictures has nothing to say
// about why a game is slow.
func (r *sessionRunner) storeReport(message clientMessage) {
	report := r.postMortem
	if r.game != nil {
		report = r.composeReport("")
	}
	if report == "" {
		r.send(serverMessage{Kind: serverResult, ID: message.ID, Message: "no game is running"})
		return
	}
	label := message.Label
	if label == "" {
		label = r.label
	}
	path, err := r.writeReport(label, report)
	if err != nil {
		r.send(serverMessage{Kind: serverError, ID: message.ID, Message: err.Error()})
		return
	}
	r.send(serverMessage{Kind: serverResult, ID: message.ID, Message: path})
}

// endGame keeps a report of the running game and then closes it. cause says
// how it ended. store asks for the report to be written unasked, which is what
// an unexpected end deserves: a game that dies on its first tick never gives
// anyone the chance to ask, and it is the one whose numbers matter most.
func (r *sessionRunner) endGame(cause string, store bool) {
	if r.game == nil {
		return
	}
	r.postMortem = r.composeReport(cause)
	// A release build keeps the snapshot for a later request but has no trace
	// to put in a file, so it does not write one.
	if store && r.server.traceLimit > 0 {
		if _, err := r.writeReport(r.label, r.postMortem); err != nil {
			r.server.logger.Warn("post-mortem report could not be stored", "game", r.label, "error", err)
		}
	}
	r.stopGame()
}

// writeReport puts a report where the debug-log API serves them from.
func (r *sessionRunner) writeReport(label, report string) (string, error) {
	_, path, err := r.server.storeReport(label, []byte(report), time.Now())
	if err != nil {
		return "", err
	}
	r.server.logger.Info("stored a session report", "path", path, "bytes", len(report))
	return path, nil
}

// composeReport renders everything the session knows. cause is empty for a
// report the page asked for, and says how the session ended for one written
// because it did.
func (r *sessionRunner) composeReport(cause string) string {
	var report strings.Builder
	fmt.Fprintf(&report, "wfeature session report\ngenerated: %s\nprofile: %s\ngame: %s\n",
		time.Now().Format(time.RFC3339), backend.BuildProfile(), r.label)
	fmt.Fprintf(&report, "platform: %s\n", r.game.Platform())
	if cause != "" {
		fmt.Fprintf(&report, "ended: %s\n", cause)
	}
	fmt.Fprintf(&report, "frames sent: %d, dropped: %d (since the last stats report)\n",
		r.sent.Load(), r.skipped)
	if r.ticks > 0 {
		fmt.Fprintf(&report, "tick cost: entries=%d average=%s\n",
			r.ticks, (r.tickTotal / time.Duration(r.ticks)).Round(time.Microsecond))
		// The rate against the cost is what says whether the loop was working
		// or waiting: ticks a second times the average is the share of a core
		// the emulator actually used.
		if elapsed := time.Since(r.statsSince); elapsed > 0 {
			fmt.Fprintf(&report, "tick rate: %.1f/s, busy %.0f%% of a core\n",
				float64(r.ticks)/elapsed.Seconds(),
				100*float64(r.tickTotal)/float64(elapsed))
		}
	}
	if speed := r.guestSpeed(time.Since(r.statsSince), false); speed > 0 {
		fmt.Fprintf(&report, "guest speed: %.2fx real time\n", speed)
	}
	if shed := r.shedTotal.Load(); shed > 0 {
		fmt.Fprintf(&report, "messages shed to a slow connection: %d\n", shed)
	}
	if lgtSession := r.game.LGT(); lgtSession != nil {
		// The same line the KTF branch below writes, for the same reason: a
		// paint that ended in an exception nothing caught no longer ends the
		// session, so a report that only carried `ended:` would read a title
		// failing every frame as a title that played.
		if uncaught, first := lgtSession.UncaughtCallbacks(); uncaught > 0 {
			fmt.Fprintf(&report, "callbacks ended by an uncaught exception: %d, first %s\n", uncaught, first)
		}
	}
	if ktfSession := r.game.KTF(); ktfSession != nil {
		// A callback that ended in an exception nothing caught no longer ends
		// the session, so a report that only carried `ended:` would show a
		// title failing every paint as a title that played. See docs/ktf.md,
		// "who catches what a callback threw".
		if uncaught, first := ktfSession.Client.UncaughtCallbacks(); uncaught > 0 {
			fmt.Fprintf(&report, "callbacks ended by an uncaught exception: %d, first %s\n", uncaught, first)
		}
		fmt.Fprintf(&report, "\n%s\n", ktfSession.HostCosts())
		if profile := ktfSession.Profile(); profile.Samples > 0 {
			fmt.Fprintf(&report, "\n===== guest profile =====\n%s\n", profile.Report(30))
		}
		diagnostics := ktfSession.Diagnostics()
		fmt.Fprintf(&report, "\nruntime events traced: %d\n", diagnostics.Traced)
		fmt.Fprintf(&report, "\n===== counted events =====\n%s\n", diagnostics.FormatCounts(0))
		if len(diagnostics.Trace) > 0 {
			fmt.Fprintf(&report, "\n===== recent boundary events =====\n")
			for _, event := range diagnostics.Trace {
				fmt.Fprintf(&report, "%d %s\n", event.Sequence, event.Event)
			}
		}
	}
	return report.String()
}

// writeMessages is the only goroutine that writes to the socket.
//
// It exists because a connection has one write mutex and everything here used
// to reach for it directly: pictures from the encoder, and audio and
// statistics from the emulator itself. The encoder blocking on a slow phone
// was the intended cost — a comment on writeFrames says as much — but the
// emulator blocking behind it was not, and that is what happened. A game in a
// busy scene sends a sound every tick, so every tick the emulator queued
// behind a frame going out over a phone link, and the wait landed on the
// guest's clock as slow motion. It never showed on a desktop, where a write to
// the loopback interface returns before the socket has done anything with it.
//
// Nothing on the emulator's path may wait for a socket. That is the whole rule
// this goroutine exists to keep.
func (r *sessionRunner) writeMessages(ctx context.Context, cancel context.CancelFunc) {
	// Cancelling on the way out ends the session rather than leaving a game
	// running for a page that can no longer be written to. The read side would
	// notice eventually; a dead socket does not deserve a core until it does.
	defer cancel()
	defer close(r.writerDone)
	for {
		var message outboundMessage
		select {
		case <-ctx.Done():
			return
		case message = <-r.outFrames:
		case message = <-r.outText:
		}
		// Text and pictures are ordered against themselves but not against
		// each other, which costs nothing: a picture carries no reference to
		// any message, and a frame that arrives just after the game said it
		// had exited is the last thing the player saw either way.
		_ = r.connection.SetWriteDeadline(time.Now().Add(writeTimeout))
		var err error
		if message.binary != nil {
			err = r.connection.WriteBinary(message.binary)
		} else {
			err = r.connection.WriteText(message.text)
		}
		if err != nil {
			if !errors.Is(err, wsproto.ErrClosed) {
				r.server.logger.Debug("session write failed", "error", err)
			}
			return
		}
		if message.binary != nil {
			r.frameBytes.Add(uint64(len(message.binary)))
			r.sent.Add(1)
		}
	}
}

// send queues a message for the page. It never blocks on the socket, and the
// callers that matter are on the emulator's goroutine.
//
// A message the session may not lose still waits for room in the queue, which
// is a backlog of sixty-odd messages deep and therefore a connection that has
// stopped rather than one that is behind. Everything sent from inside the tick
// loop while a game is playing — audio and statistics — is droppable, so that
// wait is not on the guest's path.
func (r *sessionRunner) send(message serverMessage) {
	r.queue(message, false)
}

// sendDroppable queues a message the session would rather lose than wait for.
// Audio and statistics both describe a moment that has already passed: a phone
// that is behind is better served by the next one than by all of them.
func (r *sessionRunner) sendDroppable(message serverMessage) {
	r.queue(message, true)
}

func (r *sessionRunner) queue(message serverMessage, droppable bool) {
	encoded, err := encodeMessage(message)
	if err != nil {
		r.server.logger.Warn("session message could not be encoded", "error", err)
		return
	}
	outgoing := outboundMessage{text: string(encoded)}
	if droppable {
		select {
		case r.outText <- outgoing:
		default:
			r.shed.Add(1)
			r.shedTotal.Add(1)
		}
		return
	}
	select {
	case r.outText <- outgoing:
	case <-r.writerDone:
		// The page cannot be told anything more. Saying so once is the
		// writer's job and it has already done it.
	}
}

// readGameArchive resolves the archive path the picker offered and reads it.
// The page sends back the same string games.json gave it, percent-encoding and
// all, and it is checked here exactly as the HTTP route checks it: a session
// is not a way around the game root.
func (s *Server) readGameArchive(gamePath string) ([]byte, string, error) {
	decoded, err := url.PathUnescape(gamePath)
	if err != nil {
		return nil, "", fmt.Errorf("game path is not a valid URL path: %w", err)
	}
	components, err := pathComponents(strings.TrimPrefix(decoded, "games"))
	if err != nil || len(components) == 0 {
		return nil, "", errors.New("game path is not inside the game directory")
	}
	file := filepath.Join(append([]string{s.gameRoot}, components...)...)
	archive, err := os.ReadFile(file)
	if err != nil {
		return nil, "", fmt.Errorf("read the game archive: %w", err)
	}
	name := components[len(components)-1]
	return archive, strings.TrimSuffix(name, filepath.Ext(name)), nil
}

// saveDirectory is where one game's saves live: the same
// savedata/<profile>/<platform>/<owner> tree the native CLI and the save API
// use, so a session on the server and a session in the browser boot from one
// set of saves and nothing crosses the network. It is also this game's
// identity for the claim a session takes on it; see saveclaim.go. Empty for a
// game with no saves of its own.
func (s *Server) saveDirectory(platform, owner string) string {
	if owner == "" || ownerRejected(owner) {
		return ""
	}
	root := s.saveRoot
	if platform != "" && platform != "ktf" && platforms[platform] {
		root = filepath.Join(filepath.Dir(root), platform)
	}
	return filepath.Join(root, owner)
}

// saveStoreIn opens the store for a directory saveDirectory already named.
func (s *Server) saveStoreIn(directory string) backend.SaveStore {
	if directory == "" {
		return nil
	}
	return backend.NewDirectorySaveStore(directory)
}

// audioCollector is the session's audio sink. The guest plays sound by calling
// it, and the events are batched until the end of the tick and sent as one
// message: the page's synthesiser takes the same calls it always did, so
// moving emulation to the server changed nothing about how sound is made.
type audioCollector struct {
	mutex  sync.Mutex
	events []audioEvent
}

// maxPendingAudio bounds what one tick may queue. A game that floods the sink
// while the connection is stalled must not grow this without limit.
const maxPendingAudio = 4096

func (a *audioCollector) append(event audioEvent) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if len(a.events) >= maxPendingAudio {
		return
	}
	a.events = append(a.events, event)
}

func (a *audioCollector) take() []audioEvent {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if len(a.events) == 0 {
		return nil
	}
	events := a.events
	a.events = nil
	return events
}

func (a *audioCollector) PlayWave(channels uint8, samplingRate uint32, samples []int16) {
	if len(samples) == 0 {
		return
	}
	raw := make([]byte, len(samples)*2)
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(raw[index*2:], uint16(sample))
	}
	a.append(audioEvent{
		Kind:     audioPlayWave,
		Channels: channels,
		Rate:     samplingRate,
		Samples:  base64.StdEncoding.EncodeToString(raw),
	})
}

func (a *audioCollector) MIDINoteOn(channel, note, velocity uint8) {
	a.append(audioEvent{Kind: audioNoteOn, Channel: channel, Note: note, Velocity: velocity})
}

func (a *audioCollector) MIDINoteOff(channel, note, velocity uint8) {
	a.append(audioEvent{Kind: audioNoteOff, Channel: channel, Note: note, Velocity: velocity})
}

func (a *audioCollector) MIDIProgramChange(channel, program uint8) {
	a.append(audioEvent{Kind: audioProgramChange, Channel: channel, Program: program})
}

func (a *audioCollector) MIDIControlChange(channel, control, value uint8) {
	a.append(audioEvent{Kind: audioControlChange, Channel: channel, Control: control, Value: uint16(value)})
}

func (a *audioCollector) MIDIPitchBend(channel uint8, value uint16) {
	a.append(audioEvent{Kind: audioPitchBend, Channel: channel, Value: value})
}

func (a *audioCollector) MIDISysEx(data []byte) {
	if len(data) == 0 {
		return
	}
	a.append(audioEvent{Kind: audioSysEx, Data: base64.StdEncoding.EncodeToString(data)})
}

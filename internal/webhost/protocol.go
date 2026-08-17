package webhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/movingwoo/wfeature/internal/cheat"
)

// The session protocol. The page and the server exchange JSON in text frames
// and frame images in binary ones, which is the whole reason for the split:
// a picture is bytes and everything else is a handful of fields, and neither
// should pay for the other's encoding.
//
// It is deliberately small. The page does not describe how to run a game, it
// says which game and which key; everything about how a game is run stays on
// the side that is running it.

// clientMessage is anything the page sends. Only Kind is always present; the
// rest is read according to it.
type clientMessage struct {
	Kind string `json:"kind"`

	// Game is the archive path from games.json, for kind "start".
	Game string `json:"game,omitempty"`

	// Action and Code carry one key event: "press", "release" or "repeat",
	// and the MIDP-style code the keypad has always sent.
	Action string `json:"action,omitempty"`
	Code   int32  `json:"code,omitempty"`

	// Value carries the single number of the settings messages: the speed
	// multiplier for "speed" and the magnification for "scale".
	Value float64 `json:"value,omitempty"`

	// Width and Height are the handset screen a "start" asks for. They are
	// optional and the server's default stands when they are absent, which is
	// what every game but a handful wants. A title packaged for a smaller
	// phone reads its screen and then loads artwork by that size, so the one
	// that ships no 240-wide set cannot be run on a 240-wide screen at all —
	// see docs/skvm.md. KTF ignores the request: its screen is the platform's.
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`

	// The cheat fields, for kind "cheat". Op names the operation; Command is
	// the line for the text console, and the rest are the panel's arguments.
	Op      string `json:"op,omitempty"`
	Command string `json:"command,omitempty"`
	Type    string `json:"type,omitempty"`
	Filter  string `json:"filter,omitempty"`
	Operand int64  `json:"operand,omitempty"`
	Address uint32 `json:"address,omitempty"`
	All     bool   `json:"all,omitempty"`
	Table   string `json:"table,omitempty"`

	// Label names a stored debug report, for kind "report".
	Label string `json:"label,omitempty"`

	// Token names a game the server parked when this page's last socket
	// closed, for kind "resume". The page was given it with "started".
	Token string `json:"token,omitempty"`

	// ID lets a page match an answer to the request that asked for it. Zero
	// means the page is not waiting for one.
	ID uint64 `json:"id,omitempty"`
}

// Message kinds the page may send.
const (
	clientStart  = "start"
	clientResume = "resume"
	clientKey    = "key"
	clientSpeed  = "speed"
	clientScale  = "scale"
	clientCheat  = "cheat"
	clientReport = "report"
	clientStop   = "stop"
)

// serverMessage is anything the server sends in a text frame. A frame image
// travels as a binary frame instead, with no envelope at all — it is the one
// message that is sent twenty times a second, and a PNG already says how wide
// it is.
type serverMessage struct {
	Kind string `json:"kind"`

	// ID echoes the request this answers, when it answers one.
	ID uint64 `json:"id,omitempty"`

	// Message carries an error's text or a human-readable answer.
	Message string `json:"message,omitempty"`

	// Profile is this server's build profile, sent once with "ready". The page
	// shows the developer's half of its interface — the run log and the report
	// button — only when a debug build answers, so a release is the same page
	// without the parts that only mean something to whoever is debugging it.
	Profile string `json:"profile,omitempty"`

	// Started describes the game that is now running.
	Started *startedMessage `json:"started,omitempty"`

	// Audio is a batch of sound events in the order the guest played them.
	Audio []audioEvent `json:"audio,omitempty"`

	// Stats is what the session is costing, for the page's status line.
	Stats *statsMessage `json:"stats,omitempty"`

	// Cheat answers a cheat operation. Only the fields the operation produces
	// are present, so a scan carries candidates and a freeze carries the
	// freeze list.
	Cheat *cheatResult `json:"cheat,omitempty"`

	// Resumed answers a "resume": false says there was no game under that
	// token, which is the ordinary answer after a long absence rather than a
	// failure. A true answer arrives as "started" instead, because a page that
	// has its game back needs everything a page that just started one does.
	Resumed bool `json:"resumed,omitempty"`
}

// cheatResult is the union of what the cheat operations answer. The panel that
// asked knows which field it wants; sending one shape keeps the protocol from
// needing a message kind per operation.
// Nothing here is omitempty. Zero is an answer: a reset says "no candidates",
// releasing the last freeze says "nothing is held", and a scan that narrows to
// nothing says so too. Leaving those out of the JSON turns the answer into an
// absent field, and the panel then renders `undefined` instead of zero — which
// is how a working reset looked like a broken one.
type cheatResult struct {
	Count      int                    `json:"count"`
	Items      []cheat.PanelCandidate `json:"items"`
	Frozen     []cheat.PanelFreeze    `json:"frozen"`
	Watches    []uint32               `json:"watches"`
	Hits       *cheat.PanelHits       `json:"hits,omitempty"`
	Applied    int                    `json:"applied"`
	Table      string                 `json:"table,omitempty"`
	Text       string                 `json:"text,omitempty"`
	Searchable bool                   `json:"searchable"`
}

// empty answers the fields a nil slice would otherwise marshal as `null`. A
// panel iterating the answer has to find a list there, empty or not.
func (result *cheatResult) normalize() *cheatResult {
	if result.Items == nil {
		result.Items = []cheat.PanelCandidate{}
	}
	if result.Frozen == nil {
		result.Frozen = []cheat.PanelFreeze{}
	}
	if result.Watches == nil {
		result.Watches = []uint32{}
	}
	return result
}

// Message kinds the server may send.
const (
	serverReady   = "ready"
	serverStarted = "started"
	serverExited  = "exited"
	serverError   = "error"
	serverAudio   = "audio"
	serverStats   = "stats"
	serverResult  = "result"
	serverResumed = "resumed"
)

type startedMessage struct {
	Platform  string `json:"platform"`
	AID       string `json:"aid,omitempty"`
	PID       string `json:"pid,omitempty"`
	Name      string `json:"name,omitempty"`
	SaveOwner string `json:"save_owner,omitempty"`
	MainClass string `json:"main_class,omitempty"`
	// Width and Height are the picture's size before magnification, so the
	// page can lay out before the first frame arrives.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Token names this game if its page has to come back to it. The page keeps
	// it for as long as the game runs and sends it with "resume" after a
	// dropped connection; see resume.go for what it is worth and for how long.
	Token string `json:"token,omitempty"`
}

// statsMessage is how the page knows whether the server is keeping up. It is
// the server's side of the tick-cost report the browser build collects for
// itself: on this path the emulator is not in the page, so the page cannot
// measure it.
type statsMessage struct {
	// Fps is frames actually sent over the last window.
	Fps float64 `json:"fps"`
	// Skipped counts frames the game finished that were dropped rather than
	// sent, because the connection was still busy with the previous one.
	Skipped uint64 `json:"skipped"`
	// TickMillis is the average cost of one tick on the server.
	TickMillis float64 `json:"tick_ms"`
	// FrameBytes is the average encoded size of a sent frame.
	FrameBytes int `json:"frame_bytes"`
}

// audioEvent is one call the guest made on the audio sink. The names match the
// methods the page's synthesiser exposes, so sound is a matter of forwarding
// the guest's calls rather than shipping samples: the events arrive over the
// socket and are played in the page.
type audioEvent struct {
	Kind     string `json:"kind"`
	Channel  uint8  `json:"channel,omitempty"`
	Note     uint8  `json:"note,omitempty"`
	Velocity uint8  `json:"velocity,omitempty"`
	Program  uint8  `json:"program,omitempty"`
	Control  uint8  `json:"control,omitempty"`
	Value    uint16 `json:"value,omitempty"`

	// Channels, Rate and Samples describe a sampled sound. Samples are
	// base64-encoded signed 16-bit little-endian frames rather than a JSON
	// array of numbers: a one-second sound is tens of thousands of samples,
	// and as text that is an order of magnitude more bytes than the sound.
	Channels uint8  `json:"channels,omitempty"`
	Rate     uint32 `json:"rate,omitempty"`
	Samples  string `json:"samples,omitempty"`

	// Data carries a SysEx message, base64-encoded for the same reason.
	Data string `json:"data,omitempty"`
}

// Audio event kinds.
const (
	audioNoteOn        = "noteOn"
	audioNoteOff       = "noteOff"
	audioProgramChange = "programChange"
	audioControlChange = "controlChange"
	audioPitchBend     = "pitchBend"
	audioSysEx         = "sysex"
	audioPlayWave      = "playWave"
	// audioAllOff has no counterpart in the guest's sink: it is what the
	// server says when a game ends, because a note that was sounding at that
	// moment has no one left to release it.
	audioAllOff = "allOff"
)

func encodeMessage(message serverMessage) ([]byte, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("webhost: encode %s message: %w", message.Kind, err)
	}
	return encoded, nil
}

// decodeClientMessage reads one message from the page. It is strict about the
// envelope and forgiving about nothing: everything here arrives from a browser
// tab that may not be the one that was meant to be there.
func decodeClientMessage(text string, message *clientMessage) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(message); err != nil {
		return fmt.Errorf("message is not a session command: %v", err)
	}
	if message.Kind == "" {
		return errors.New("message has no kind")
	}
	return nil
}

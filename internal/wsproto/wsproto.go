// Package wsproto is the WebSocket framing this project needs and nothing
// more. The session server pushes frames to a phone and reads input back, so
// what is required is a handshake, masked and unmasked data frames, and the
// control frames that keep a connection honest — close and ping. That is small
// enough to own, and owning it keeps the dependency list at x/text and x/image.
//
// The codec is deliberately transport-agnostic: it works on any
// io.ReadWriter, so both ends can be driven in a test without a browser or
// even a socket. Accept in handshake.go is the only part that knows about
// net/http.
//
// Everything arriving from a peer is untrusted input, so lengths are checked
// against a bound before anything is allocated, reserved bits and malformed
// fragmentation are rejected rather than ignored, and a server refuses the
// unmasked client frames RFC 6455 forbids.
package wsproto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
	"unicode/utf8"
)

// Opcode is the frame type from RFC 6455 section 5.2.
type Opcode byte

const (
	// OpContinuation carries the rest of a fragmented message.
	OpContinuation Opcode = 0x0
	// OpText carries UTF-8 text. The session protocol uses it for JSON.
	OpText Opcode = 0x1
	// OpBinary carries bytes. The session protocol uses it for frame images.
	OpBinary Opcode = 0x2
	// OpClose starts or answers the closing handshake.
	OpClose Opcode = 0x8
	// OpPing asks for a pong; OpPong answers one.
	OpPing Opcode = 0x9
	// OpPong answers a ping.
	OpPong Opcode = 0xA
)

func (o Opcode) String() string {
	switch o {
	case OpContinuation:
		return "continuation"
	case OpText:
		return "text"
	case OpBinary:
		return "binary"
	case OpClose:
		return "close"
	case OpPing:
		return "ping"
	case OpPong:
		return "pong"
	default:
		return fmt.Sprintf("opcode(%#x)", byte(o))
	}
}

func (o Opcode) control() bool { return o&0x8 != 0 }

// Close codes this package sends or recognises. The rest of the registry is
// the peer's business.
const (
	CloseNormal          = 1000
	CloseGoingAway       = 1001
	CloseProtocolError   = 1002
	CloseUnsupportedData = 1003
	// CloseNoStatus is never sent on the wire. It is what a close frame with
	// an empty payload means, and reporting it as a code keeps callers from
	// having to special-case the empty case.
	CloseNoStatus      = 1005
	CloseAbnormal      = 1006
	CloseInvalidData   = 1007
	CloseMessageTooBig = 1009
	CloseInternalError = 1011
)

// DefaultMaxMessageSize bounds one assembled message. Input from the page is
// a small JSON object and a game frame is tens of kilobytes, so this is far
// above anything legitimate and only exists to stop a peer from naming a
// length the host would try to allocate.
const DefaultMaxMessageSize = 8 << 20

// maxControlPayload is the limit RFC 6455 puts on a control frame.
const maxControlPayload = 125

// ErrClosed is returned by reads and writes once the connection has finished
// its closing handshake or the underlying transport is gone. Callers that only
// want to know "is this session over" can test for it without unwrapping a
// CloseError.
var ErrClosed = errors.New("wsproto: connection closed")

// CloseError reports that the peer sent a close frame. It wraps ErrClosed, so
// a session loop can end quietly on a normal close and log the rest.
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("wsproto: closed with code %d", e.Code)
	}
	return fmt.Sprintf("wsproto: closed with code %d: %s", e.Code, e.Reason)
}

func (e *CloseError) Unwrap() error { return ErrClosed }

// ProtocolError reports a frame that violates RFC 6455. It is separate from a
// transport error because the answer differs: a protocol error is reported to
// the peer as close code 1002 before the connection goes away.
type ProtocolError struct {
	Reason string
	// Code is the close code to send back, so a size violation reports 1009
	// and bad UTF-8 reports 1007 rather than everything collapsing to 1002.
	Code int
}

func (e *ProtocolError) Error() string { return "wsproto: " + e.Reason }

func protocolError(code int, format string, arguments ...any) *ProtocolError {
	return &ProtocolError{Reason: fmt.Sprintf(format, arguments...), Code: code}
}

// Conn is one WebSocket connection. Reads happen on one goroutine and writes
// may happen on another — the session loop pushes frames while the reader
// waits for input — so writes are serialised and the close state is shared.
// Two concurrent readers are not supported and would interleave fragments.
type Conn struct {
	transport io.ReadWriter
	closer    io.Closer

	// client is true when this end must mask what it sends and must accept
	// unmasked frames; a server is the mirror of both.
	client bool

	// MaxMessageSize bounds one assembled message, fragments included. Zero
	// means DefaultMaxMessageSize.
	MaxMessageSize int

	writeMutex sync.Mutex

	stateMutex sync.Mutex
	// sentClose records that a close frame has gone out, so answering a
	// peer's close and closing on our own initiative cannot both send one.
	sentClose bool
	closed    bool

	// header is scratch for frame headers so a busy session does not allocate
	// per frame. It is only touched by the single reader.
	header [14]byte

	// fragment accumulates a message across continuation frames.
	fragment       []byte
	fragmentOpcode Opcode
	fragmenting    bool
}

// Server wraps an already-upgraded transport as the server end: it requires
// masked frames from the peer and sends unmasked ones. Accept calls this after
// the handshake; a test calls it directly on an in-memory transport.
func Server(transport io.ReadWriter) *Conn {
	return newConn(transport, false)
}

// Client wraps a transport as the client end, masking what it sends. Nothing
// in the emulator speaks as a client — the browser does — but having both ends
// in Go is what makes the codec testable without a browser.
func Client(transport io.ReadWriter) *Conn {
	return newConn(transport, true)
}

func newConn(transport io.ReadWriter, client bool) *Conn {
	connection := &Conn{transport: transport, client: client}
	if closer, ok := transport.(io.Closer); ok {
		connection.closer = closer
	}
	return connection
}

func (c *Conn) maxMessageSize() int {
	if c.MaxMessageSize > 0 {
		return c.MaxMessageSize
	}
	return DefaultMaxMessageSize
}

// SetReadDeadline and SetWriteDeadline pass through to the transport when it
// is a socket. A session that stops hearing from a phone that went to sleep
// has to be able to time out rather than hold the goroutine forever; on a
// transport without deadlines they report that they did nothing.
func (c *Conn) SetReadDeadline(deadline time.Time) error {
	if socket, ok := c.transport.(net.Conn); ok {
		return socket.SetReadDeadline(deadline)
	}
	return errors.New("wsproto: transport has no read deadline")
}

func (c *Conn) SetWriteDeadline(deadline time.Time) error {
	if socket, ok := c.transport.(net.Conn); ok {
		return socket.SetWriteDeadline(deadline)
	}
	return errors.New("wsproto: transport has no write deadline")
}

// ReadMessage returns the next data message, reassembling fragments and
// answering the control frames that arrive in between: a ping is ponged and a
// close is echoed. It returns a *CloseError when the peer closes.
func (c *Conn) ReadMessage() (Opcode, []byte, error) {
	for {
		opcode, payload, final, err := c.readFrame()
		if err != nil {
			var protocol *ProtocolError
			if errors.As(err, &protocol) {
				// The peer gets told why before the connection goes; a
				// failure to say so changes nothing about the outcome.
				_ = c.WriteClose(protocol.Code, protocol.Reason)
				c.markClosed()
			}
			return 0, nil, err
		}

		if opcode.control() {
			if err := c.handleControl(opcode, payload); err != nil {
				return 0, nil, err
			}
			continue
		}

		if opcode == OpContinuation {
			if !c.fragmenting {
				return 0, nil, c.fail(protocolError(CloseProtocolError, "continuation frame with no message in progress"))
			}
		} else {
			if c.fragmenting {
				return 0, nil, c.fail(protocolError(CloseProtocolError, "%s frame interrupts a fragmented message", opcode))
			}
			c.fragmentOpcode = opcode
			c.fragment = c.fragment[:0]
			c.fragmenting = true
		}

		if len(c.fragment)+len(payload) > c.maxMessageSize() {
			return 0, nil, c.fail(protocolError(CloseMessageTooBig, "message exceeds %d bytes", c.maxMessageSize()))
		}
		c.fragment = append(c.fragment, payload...)
		if !final {
			continue
		}

		c.fragmenting = false
		messageOpcode := c.fragmentOpcode
		message := make([]byte, len(c.fragment))
		copy(message, c.fragment)
		if messageOpcode == OpText && !utf8.Valid(message) {
			return 0, nil, c.fail(protocolError(CloseInvalidData, "text message is not valid UTF-8"))
		}
		return messageOpcode, message, nil
	}
}

// ReadText is the common case for input: a text message, refusing binary
// rather than silently reinterpreting it.
func (c *Conn) ReadText() (string, error) {
	opcode, payload, err := c.ReadMessage()
	if err != nil {
		return "", err
	}
	if opcode != OpText {
		return "", fmt.Errorf("wsproto: expected a text message, got %s", opcode)
	}
	return string(payload), nil
}

// fail reports a protocol violation to the peer and returns it to the caller.
func (c *Conn) fail(err *ProtocolError) error {
	_ = c.WriteClose(err.Code, err.Reason)
	c.markClosed()
	return err
}

func (c *Conn) handleControl(opcode Opcode, payload []byte) error {
	switch opcode {
	case OpPing:
		// A pong carries the ping's payload back unchanged. A write failure
		// here is the transport dying, which the next read will report.
		if err := c.write(OpPong, payload, true); err != nil {
			return err
		}
		return nil
	case OpPong:
		// Nothing waits on a pong yet; a keepalive answer is enough.
		return nil
	case OpClose:
		code, reason, err := parseClosePayload(payload)
		if err != nil {
			return c.fail(err)
		}
		// The closing handshake is an echo: send the same code back, then
		// stop. Whoever sent the first close closes the transport.
		_ = c.WriteClose(closeEchoCode(code), "")
		c.markClosed()
		return &CloseError{Code: code, Reason: reason}
	default:
		return c.fail(protocolError(CloseProtocolError, "unknown control opcode %#x", byte(opcode)))
	}
}

// closeEchoCode maps what was received onto what may be sent: 1005 means the
// peer sent no code at all, and it is not a code that can go on the wire.
func closeEchoCode(code int) int {
	if code == CloseNoStatus {
		return CloseNormal
	}
	return code
}

func parseClosePayload(payload []byte) (int, string, *ProtocolError) {
	switch {
	case len(payload) == 0:
		return CloseNoStatus, "", nil
	case len(payload) == 1:
		return 0, "", protocolError(CloseProtocolError, "close payload of one byte")
	}
	code := int(binary.BigEndian.Uint16(payload[:2]))
	reason := payload[2:]
	if !utf8.Valid(reason) {
		return 0, "", protocolError(CloseInvalidData, "close reason is not valid UTF-8")
	}
	if !validCloseCode(code) {
		return 0, "", protocolError(CloseProtocolError, "reserved close code %d", code)
	}
	return code, string(reason), nil
}

// validCloseCode accepts the codes RFC 6455 lets a peer put on the wire: the
// defined range minus the three that only describe a local condition, plus the
// registered and private ranges.
func validCloseCode(code int) bool {
	switch {
	case code >= 3000 && code <= 4999:
		return true
	case code == CloseNoStatus || code == CloseAbnormal || code == 1015:
		return false
	case code >= 1000 && code <= 1014:
		return true
	default:
		return false
	}
}

// readFrame reads one frame header and its payload. The returned payload
// aliases an internal buffer only for control frames, which are consumed
// before the next read; data payloads are freshly allocated.
func (c *Conn) readFrame() (Opcode, []byte, bool, error) {
	if _, err := io.ReadFull(c.transport, c.header[:2]); err != nil {
		return 0, nil, false, readError(err)
	}
	first, second := c.header[0], c.header[1]
	final := first&0x80 != 0
	if first&0x70 != 0 {
		return 0, nil, false, protocolError(CloseProtocolError, "reserved bits set in frame header")
	}
	opcode := Opcode(first & 0x0f)
	switch opcode {
	case OpContinuation, OpText, OpBinary, OpClose, OpPing, OpPong:
	default:
		return 0, nil, false, protocolError(CloseProtocolError, "unknown opcode %#x", byte(opcode))
	}

	masked := second&0x80 != 0
	// A server must reject unmasked client frames and a client must reject
	// masked server frames; accepting either would let a proxy be poisoned.
	if masked == c.client {
		if c.client {
			return 0, nil, false, protocolError(CloseProtocolError, "masked frame from server")
		}
		return 0, nil, false, protocolError(CloseProtocolError, "unmasked frame from client")
	}

	length := int64(second & 0x7f)
	switch length {
	case 126:
		if _, err := io.ReadFull(c.transport, c.header[:2]); err != nil {
			return 0, nil, false, readError(err)
		}
		length = int64(binary.BigEndian.Uint16(c.header[:2]))
	case 127:
		if _, err := io.ReadFull(c.transport, c.header[:8]); err != nil {
			return 0, nil, false, readError(err)
		}
		unsigned := binary.BigEndian.Uint64(c.header[:8])
		// The high bit must be clear per RFC 6455, and the value is bounded
		// below anyway; checking it first keeps the conversion honest on a
		// 32-bit host.
		if unsigned&(1<<63) != 0 {
			return 0, nil, false, protocolError(CloseProtocolError, "frame length has the high bit set")
		}
		length = int64(unsigned)
	}

	if opcode.control() {
		if !final {
			return 0, nil, false, protocolError(CloseProtocolError, "fragmented %s frame", opcode)
		}
		if length > maxControlPayload {
			return 0, nil, false, protocolError(CloseProtocolError, "%s frame carries %d bytes", opcode, length)
		}
	} else if length > int64(c.maxMessageSize()) {
		// Refused before allocating: the length is the peer's claim, not a
		// measurement.
		return 0, nil, false, protocolError(CloseMessageTooBig, "frame of %d bytes exceeds %d", length, c.maxMessageSize())
	}

	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.transport, mask[:]); err != nil {
			return 0, nil, false, readError(err)
		}
	}

	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(c.transport, payload); err != nil {
			return 0, nil, false, readError(err)
		}
		if masked {
			applyMask(payload, mask)
		}
	}
	return opcode, payload, final, nil
}

// readError turns a transport read failure into something a session loop can
// test: a peer that vanishes without a close frame is still a closed
// connection, not a mystery.
func readError(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return &CloseError{Code: CloseAbnormal, Reason: "transport ended without a close frame"}
	}
	return err
}

// applyMask XORs the payload with the four-byte key. Frames are small enough
// here that the word-at-a-time version is not worth its edge cases.
func applyMask(payload []byte, mask [4]byte) {
	for index := range payload {
		payload[index] ^= mask[index&3]
	}
}

// WriteMessage sends one unfragmented data message. Fragmenting on write is
// not implemented because nothing here needs it: a frame image is one buffer
// and a JSON message is small.
func (c *Conn) WriteMessage(opcode Opcode, payload []byte) error {
	if opcode.control() {
		return fmt.Errorf("wsproto: WriteMessage called with the control opcode %s", opcode)
	}
	return c.write(opcode, payload, true)
}

// WriteText sends a text message.
func (c *Conn) WriteText(text string) error { return c.write(OpText, []byte(text), true) }

// WriteBinary sends a binary message.
func (c *Conn) WriteBinary(payload []byte) error { return c.write(OpBinary, payload, true) }

// Ping sends a ping with an optional payload the peer must echo.
func (c *Conn) Ping(payload []byte) error {
	if len(payload) > maxControlPayload {
		return fmt.Errorf("wsproto: ping payload of %d bytes exceeds %d", len(payload), maxControlPayload)
	}
	return c.write(OpPing, payload, true)
}

// WriteClose sends the close frame, once. Calling it again is a no-op so that
// answering a peer's close and shutting down locally cannot both write one.
// The reason is truncated to fit a control frame rather than rejected: a close
// that fails to send because its explanation was long is worse than a short
// explanation.
func (c *Conn) WriteClose(code int, reason string) error {
	c.stateMutex.Lock()
	if c.sentClose || c.closed {
		c.stateMutex.Unlock()
		return nil
	}
	c.sentClose = true
	c.stateMutex.Unlock()

	payload := make([]byte, 0, maxControlPayload)
	if code != CloseNoStatus {
		payload = binary.BigEndian.AppendUint16(payload, uint16(code))
		payload = append(payload, truncateUTF8(reason, maxControlPayload-2)...)
	}
	return c.write(OpClose, payload, true)
}

// truncateUTF8 cuts to a byte budget without splitting a rune, so a Korean
// reason string stays valid UTF-8 after truncation.
func truncateUTF8(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

func (c *Conn) write(opcode Opcode, payload []byte, final bool) error {
	c.stateMutex.Lock()
	closed := c.closed
	c.stateMutex.Unlock()
	if closed {
		return ErrClosed
	}

	var header [14]byte
	size := 2
	header[0] = byte(opcode)
	if final {
		header[0] |= 0x80
	}
	switch length := len(payload); {
	case length < 126:
		header[1] = byte(length)
	case length <= 0xffff:
		header[1] = 126
		binary.BigEndian.PutUint16(header[2:4], uint16(length))
		size = 4
	default:
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:10], uint64(length))
		size = 10
	}

	var mask [4]byte
	if c.client {
		if _, err := rand.Read(mask[:]); err != nil {
			return fmt.Errorf("wsproto: read mask key: %w", err)
		}
		header[1] |= 0x80
		copy(header[size:size+4], mask[:])
		size += 4
	}

	c.writeMutex.Lock()
	defer c.writeMutex.Unlock()
	if _, err := c.transport.Write(header[:size]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	if !c.client {
		_, err := c.transport.Write(payload)
		return err
	}
	// A client must not mask in place: the payload belongs to the caller.
	masked := make([]byte, len(payload))
	copy(masked, payload)
	applyMask(masked, mask)
	_, err := c.transport.Write(masked)
	return err
}

func (c *Conn) markClosed() {
	c.stateMutex.Lock()
	c.closed = true
	c.stateMutex.Unlock()
}

// Close sends a normal close frame if none has gone out yet and then drops the
// transport. It is safe to call more than once.
func (c *Conn) Close() error {
	c.stateMutex.Lock()
	alreadyClosed := c.closed
	c.stateMutex.Unlock()
	if !alreadyClosed {
		_ = c.WriteClose(CloseNormal, "")
	}
	c.markClosed()
	if c.closer != nil {
		return c.closer.Close()
	}
	return nil
}

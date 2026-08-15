package wsproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

// pair connects a client and a server Conn over an in-memory transport, which
// is what lets the whole codec be exercised without a browser or a port.
func pair(t *testing.T) (client, server *Conn) {
	t.Helper()
	clientTransport, serverTransport := memoryTransports()
	t.Cleanup(func() {
		_ = clientTransport.Close()
		_ = serverTransport.Close()
	})
	return Client(clientTransport), Server(serverTransport)
}

// rawPair gives the peer end as bytes, for the frames no correct client would
// ever produce.
func rawPair(t *testing.T) (peer io.ReadWriteCloser, server *Conn) {
	t.Helper()
	peerTransport, serverTransport := memoryTransports()
	t.Cleanup(func() {
		_ = peerTransport.Close()
		_ = serverTransport.Close()
	})
	return peerTransport, Server(serverTransport)
}

// readCloseFrame reads one close frame off the wire and reports what it said,
// because "the frame was refused" is only half the contract: the peer has to
// be told why.
func readCloseFrame(t *testing.T, peer io.Reader) (int, string) {
	t.Helper()
	var header [2]byte
	if _, err := io.ReadFull(peer, header[:]); err != nil {
		t.Fatalf("read close header: %v", err)
	}
	if opcode := Opcode(header[0] & 0x0f); opcode != OpClose {
		t.Fatalf("the answer was a %s frame, want close", opcode)
	}
	length := int(header[1] & 0x7f)
	if length > maxControlPayload {
		t.Fatalf("close frame carries %d bytes", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(peer, payload); err != nil {
		t.Fatalf("read close payload: %v", err)
	}
	if length == 0 {
		return CloseNoStatus, ""
	}
	return int(binary.BigEndian.Uint16(payload[:2])), string(payload[2:])
}

func TestMessagesSurviveBothDirections(t *testing.T) {
	// A client masks its frames and a server does not, so a message has to be
	// checked in both directions to know the two paths agree.
	for _, test := range []struct {
		name    string
		opcode  Opcode
		payload []byte
	}{
		{"text", OpText, []byte(`{"kind":"key","name":"FIRE"}`)},
		{"korean text", OpText, []byte("한글 제목")},
		{"empty text", OpText, []byte{}},
		{"binary", OpBinary, []byte{0x89, 'P', 'N', 'G', 0x00, 0xff}},
		// 126 crosses into the two-byte length field and 65536 into the
		// eight-byte one; both boundaries are off-by-one country.
		{"two byte length", OpBinary, bytes.Repeat([]byte{0xa5}, 126)},
		{"eight byte length", OpBinary, bytes.Repeat([]byte{0x5a}, 1<<16)},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, server := pair(t)
			for _, direction := range []struct {
				name     string
				from, to *Conn
			}{
				{"client to server", client, server},
				{"server to client", server, client},
			} {
				payload := append([]byte(nil), test.payload...)
				if err := direction.from.WriteMessage(test.opcode, payload); err != nil {
					t.Fatalf("%s: write: %v", direction.name, err)
				}
				opcode, got, err := direction.to.ReadMessage()
				if err != nil {
					t.Fatalf("%s: read: %v", direction.name, err)
				}
				if opcode != test.opcode {
					t.Errorf("%s: opcode = %s, want %s", direction.name, opcode, test.opcode)
				}
				if !bytes.Equal(got, test.payload) {
					t.Errorf("%s: payload of %d bytes did not survive, want %d", direction.name, len(got), len(test.payload))
				}
				// Masking must copy: the buffer belongs to the caller, and a
				// session that reuses one frame buffer would send garbage.
				if !bytes.Equal(payload, test.payload) {
					t.Errorf("%s: the write masked the caller's buffer in place", direction.name)
				}
			}
		})
	}
}

func TestReadTextRefusesBinary(t *testing.T) {
	client, server := pair(t)
	if err := client.WriteBinary([]byte{1, 2, 3}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := server.ReadText(); err == nil {
		t.Fatal("ReadText accepted a binary message")
	}
}

func TestFragmentedMessageIsReassembled(t *testing.T) {
	client, server := pair(t)
	// write is unexported on purpose: only a peer sends fragments here, so the
	// test drives the frame writer directly.
	for _, fragment := range []struct {
		opcode  Opcode
		payload string
		final   bool
	}{
		{OpText, "frag", false},
		{OpContinuation, "ment", false},
		{OpContinuation, "ed", true},
	} {
		if err := client.write(fragment.opcode, []byte(fragment.payload), fragment.final); err != nil {
			t.Fatalf("write %s: %v", fragment.opcode, err)
		}
	}
	opcode, payload, err := server.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if opcode != OpText || string(payload) != "fragmented" {
		t.Fatalf("got %s %q, want text \"fragmented\"", opcode, payload)
	}
}

func TestPingIsAnsweredAndDoesNotSurfaceAsAMessage(t *testing.T) {
	// A browser keepalive must be answered by the reader itself, without the
	// session loop having to know control frames exist.
	client, server := pair(t)
	if err := client.Ping([]byte("alive")); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := client.WriteText("after"); err != nil {
		t.Fatalf("write: %v", err)
	}
	opcode, payload, err := server.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if opcode != OpText || string(payload) != "after" {
		t.Fatalf("got %s %q, want text \"after\"", opcode, payload)
	}
	answerOpcode, answerPayload, _, err := client.readFrame()
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if answerOpcode != OpPong || string(answerPayload) != "alive" {
		t.Fatalf("got %s %q, want pong \"alive\"", answerOpcode, answerPayload)
	}
}

func TestPongIsIgnored(t *testing.T) {
	client, server := pair(t)
	if err := client.write(OpPong, []byte("late"), true); err != nil {
		t.Fatalf("write pong: %v", err)
	}
	if err := client.WriteText("after"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, payload, err := server.ReadMessage(); err != nil || string(payload) != "after" {
		t.Fatalf("got %q, %v; want \"after\" with no error", payload, err)
	}
}

func TestPingPayloadIsBounded(t *testing.T) {
	client, _ := pair(t)
	if err := client.Ping(bytes.Repeat([]byte{'x'}, maxControlPayload+1)); err == nil {
		t.Fatal("an oversized ping payload was accepted")
	}
}

func TestCloseReportsTheCodeAndReason(t *testing.T) {
	client, server := pair(t)
	if err := client.WriteClose(CloseGoingAway, "phone slept"); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, _, err := server.ReadMessage()
	var closeError *CloseError
	if !errors.As(err, &closeError) {
		t.Fatalf("read after close: %v, want a *CloseError", err)
	}
	if closeError.Code != CloseGoingAway || closeError.Reason != "phone slept" {
		t.Fatalf("got %d %q, want %d \"phone slept\"", closeError.Code, closeError.Reason, CloseGoingAway)
	}
	// A session loop should be able to end quietly on ErrClosed without
	// unwrapping anything.
	if !errors.Is(err, ErrClosed) {
		t.Error("a close error does not report itself as ErrClosed")
	}
	if code, _ := readCloseFrame(t, client.transport); code != CloseGoingAway {
		t.Errorf("close echo code = %d, want %d", code, CloseGoingAway)
	}
}

func TestCloseWithoutACodeIsNotAnError(t *testing.T) {
	client, server := pair(t)
	if err := client.write(OpClose, nil, true); err != nil {
		t.Fatalf("write close: %v", err)
	}
	_, _, err := server.ReadMessage()
	var closeError *CloseError
	if !errors.As(err, &closeError) || closeError.Code != CloseNoStatus {
		t.Fatalf("read: %v, want a close error with code %d", err, CloseNoStatus)
	}
	// 1005 says "the peer sent no code" and must never go on the wire, so the
	// echo has to translate it.
	if code, _ := readCloseFrame(t, client.transport); code != CloseNormal {
		t.Errorf("close echo code = %d, want %d", code, CloseNormal)
	}
}

func TestCloseReasonIsTruncatedWithoutSplittingARune(t *testing.T) {
	// Korean is the realistic long reason, and cutting it mid-rune would make
	// the close frame itself invalid UTF-8.
	client, server := pair(t)
	reason := strings.Repeat("한", 80)
	if err := client.WriteClose(CloseInternalError, reason); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, _, err := server.ReadMessage()
	var closeError *CloseError
	if !errors.As(err, &closeError) {
		t.Fatalf("read: %v, want a *CloseError", err)
	}
	if closeError.Code != CloseInternalError {
		t.Fatalf("code = %d, want %d", closeError.Code, CloseInternalError)
	}
	if closeError.Reason == "" || !strings.HasPrefix(reason, closeError.Reason) {
		t.Fatalf("reason %q is not a prefix of what was sent", closeError.Reason)
	}
	if len(closeError.Reason) > maxControlPayload-2 {
		t.Fatalf("reason is %d bytes, over the control frame budget", len(closeError.Reason))
	}
}

func TestCloseIsSentOnlyOnce(t *testing.T) {
	// Answering a peer's close and shutting the session down locally must not
	// both put a frame on the wire; the second one would arrive after the
	// connection is meant to be finished.
	peer, server := rawPair(t)
	if err := server.WriteClose(CloseNormal, "first"); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := server.WriteClose(CloseInternalError, "second"); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if code, reason := readCloseFrame(t, peer); code != CloseNormal || reason != "first" {
		t.Fatalf("got %d %q, want %d \"first\"", code, reason, CloseNormal)
	}
	_ = peer.Close()
	if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Errorf("a second close frame followed: %v", err)
	}
}

func TestTransportEndingWithoutACloseFrameReadsAsClosed(t *testing.T) {
	// A phone that goes into a tunnel sends no close frame. The read has to
	// end the session rather than surface a bare io.EOF the caller has to
	// know to expect.
	peer, server := rawPair(t)
	if err := peer.Close(); err != nil {
		t.Fatalf("close peer: %v", err)
	}
	if _, _, err := server.ReadMessage(); !errors.Is(err, ErrClosed) {
		t.Fatalf("read after the peer vanished: %v, want ErrClosed", err)
	}
}

func TestPartialFrameEndingReadsAsClosed(t *testing.T) {
	// A connection cut mid-frame is the same story as a clean disconnect, and
	// it arrives as ErrUnexpectedEOF rather than EOF.
	peer, server := rawPair(t)
	if _, err := peer.Write([]byte{0x82, 0x85, 0, 0, 0, 0, 'h'}); err != nil {
		t.Fatalf("write partial frame: %v", err)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("close peer: %v", err)
	}
	if _, _, err := server.ReadMessage(); !errors.Is(err, ErrClosed) {
		t.Fatalf("read of a truncated frame: %v, want ErrClosed", err)
	}
}

func TestWritingAfterCloseReportsClosed(t *testing.T) {
	client, server := pair(t)
	if err := client.WriteClose(CloseNormal, ""); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, _, err := server.ReadMessage(); !errors.Is(err, ErrClosed) {
		t.Fatalf("read: %v, want ErrClosed", err)
	}
	if err := server.WriteText("late"); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after close: %v, want ErrClosed", err)
	}
}

func TestWriteMessageRefusesControlOpcodes(t *testing.T) {
	client, _ := pair(t)
	if err := client.WriteMessage(OpClose, nil); err == nil {
		t.Fatal("WriteMessage accepted a close opcode")
	}
}

func TestProtocolViolationsAreRefused(t *testing.T) {
	// Every one of these is reachable from a hostile page, so each has to be
	// refused with a close frame rather than accepted or hung on.
	maskedFrame := func(first byte, payload []byte) []byte {
		frame := []byte{first, byte(0x80 | len(payload)), 0, 0, 0, 0}
		return append(frame, payload...)
	}
	// A control frame whose payload needs the extended length field at all is
	// already too long, which is the point of the case below.
	longPing := append([]byte{0x89, 0xfe, 0x00, 0x7e, 0, 0, 0, 0}, bytes.Repeat([]byte{'x'}, 126)...)
	for _, test := range []struct {
		name  string
		frame []byte
		code  int
	}{
		{"reserved bit", maskedFrame(0xC1, []byte("hi")), CloseProtocolError},
		{"unknown data opcode", maskedFrame(0x83, []byte("hi")), CloseProtocolError},
		{"unknown control opcode", maskedFrame(0x8b, []byte("hi")), CloseProtocolError},
		{"unmasked client frame", []byte{0x81, 0x02, 'h', 'i'}, CloseProtocolError},
		{"fragmented control frame", maskedFrame(0x09, []byte("hi")), CloseProtocolError},
		{"oversized control frame", longPing, CloseProtocolError},
		{"continuation with nothing in progress", maskedFrame(0x80, []byte("hi")), CloseProtocolError},
		{"invalid utf-8 text", maskedFrame(0x81, []byte{0xff, 0xfe}), CloseInvalidData},
		{"one byte close payload", maskedFrame(0x88, []byte{0x03}), CloseProtocolError},
		// 1006 describes a local condition and is never legal on the wire.
		{"reserved close code", maskedFrame(0x88, []byte{0x03, 0xee}), CloseProtocolError},
		{"invalid utf-8 close reason", maskedFrame(0x88, []byte{0x03, 0xe8, 0xff}), CloseInvalidData},
	} {
		t.Run(test.name, func(t *testing.T) {
			peer, server := rawPair(t)
			if _, err := peer.Write(test.frame); err != nil {
				t.Fatalf("write frame: %v", err)
			}
			if _, _, err := server.ReadMessage(); err == nil {
				t.Fatal("the frame was accepted")
			}
			if code, _ := readCloseFrame(t, peer); code != test.code {
				t.Errorf("close code = %d, want %d", code, test.code)
			}
		})
	}
}

func TestDataFrameInterruptingAFragmentIsRefused(t *testing.T) {
	// This is the one violation that needs two well-formed frames to reach.
	client, server := pair(t)
	if err := client.write(OpText, []byte("start"), false); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := client.write(OpText, []byte("again"), true); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := server.ReadMessage(); err == nil {
		t.Fatal("a text frame interrupting a fragment was accepted")
	}
}

func TestControlFrameBetweenFragmentsIsAllowed(t *testing.T) {
	// A ping may be interleaved with a fragmented message; refusing it would
	// break a keepalive that lands mid-frame.
	client, server := pair(t)
	if err := client.write(OpText, []byte("half "), false); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := client.Ping(nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := client.write(OpContinuation, []byte("way"), true); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, payload, err := server.ReadMessage(); err != nil || string(payload) != "half way" {
		t.Fatalf("got %q, %v; want \"half way\" with no error", payload, err)
	}
}

func TestOversizedFrameIsRefusedBeforeItIsRead(t *testing.T) {
	// The length is the peer's claim, not a measurement. A server that
	// allocates it first can be made to reserve gigabytes by a header of ten
	// bytes and no payload at all.
	peer, server := rawPair(t)
	server.MaxMessageSize = 1024
	header := []byte{0x82, 0xff, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := peer.Write(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, _, err := server.ReadMessage(); err == nil {
		t.Fatal("the oversized frame was accepted")
	}
	if code, _ := readCloseFrame(t, peer); code != CloseMessageTooBig {
		t.Errorf("close code = %d, want %d", code, CloseMessageTooBig)
	}
}

func TestFrameLengthWithTheHighBitSetIsRefused(t *testing.T) {
	peer, server := rawPair(t)
	header := []byte{0x82, 0xff, 0x80, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}
	if _, err := peer.Write(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, _, err := server.ReadMessage(); err == nil {
		t.Fatal("a frame length with the high bit set was accepted")
	}
}

func TestFragmentsAreBoundedInTotal(t *testing.T) {
	// Each fragment can sit under the limit while the assembled message does
	// not, so the bound has to be checked on the total.
	client, server := pair(t)
	server.MaxMessageSize = 8
	if err := client.write(OpBinary, bytes.Repeat([]byte{1}, 6), false); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := client.write(OpContinuation, bytes.Repeat([]byte{1}, 6), true); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := server.ReadMessage(); err == nil {
		t.Fatal("fragments summing over the limit were accepted")
	}
}

func TestClientRefusesMaskedFramesFromTheServer(t *testing.T) {
	// The mirror rule: only a client masks, so a masked frame arriving at a
	// client is as wrong as an unmasked one arriving at a server.
	peerTransport, clientTransport := memoryTransports()
	t.Cleanup(func() {
		_ = peerTransport.Close()
		_ = clientTransport.Close()
	})
	if _, err := peerTransport.Write([]byte{0x81, 0x82, 0, 0, 0, 0, 'h', 'i'}); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	if _, _, err := Client(clientTransport).ReadMessage(); err == nil {
		t.Fatal("a masked frame from the server was accepted")
	}
}

func TestMaskIsRandomPerFrame(t *testing.T) {
	// A fixed mask key would be a real weakness that every round-trip test
	// still passes, so it is checked on the wire.
	var wire bytes.Buffer
	client := Client(&wire)
	for range 4 {
		if err := client.WriteText("aaaa"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	const frameSize = 2 + 4 + 4
	frames := wire.Bytes()
	seen := map[string]bool{}
	for offset := 0; offset+frameSize <= len(frames); offset += frameSize {
		seen[string(frames[offset+2:offset+6])] = true
	}
	if len(seen) < 2 {
		t.Fatalf("%d distinct masks across 4 frames; the key is not per-frame", len(seen))
	}
}

func TestOpcodeNamesAreReadable(t *testing.T) {
	// These names end up in diagnostics, where "ping" beats "opcode(0x9)" and
	// an unknown opcode still has to say what it was.
	if got := OpPing.String(); got != "ping" {
		t.Errorf("OpPing = %q", got)
	}
	if got := Opcode(0x7).String(); !strings.Contains(got, "0x7") {
		t.Errorf("an unknown opcode rendered as %q", got)
	}
}

package wsproto

import (
	"io"
	"sync"
)

// memoryStream is one direction of an in-memory connection. Unlike net.Pipe a
// write never blocks, which matters here because the codec answers a frame
// while it is reading one — a ping is ponged and a bad frame is closed on. On
// a synchronous pipe every one of those answers would deadlock a test that is
// not already reading, and the test would be measuring the pipe rather than
// the codec.
type memoryStream struct {
	mutex   sync.Mutex
	ready   *sync.Cond
	pending []byte
	closed  bool
}

func newMemoryStream() *memoryStream {
	stream := &memoryStream{}
	stream.ready = sync.NewCond(&stream.mutex)
	return stream
}

func (s *memoryStream) Write(from []byte) (int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	s.pending = append(s.pending, from...)
	s.ready.Broadcast()
	return len(from), nil
}

func (s *memoryStream) Read(into []byte) (int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for len(s.pending) == 0 && !s.closed {
		s.ready.Wait()
	}
	if len(s.pending) == 0 {
		return 0, io.EOF
	}
	count := copy(into, s.pending)
	s.pending = s.pending[count:]
	return count, nil
}

func (s *memoryStream) Close() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.closed = true
	s.ready.Broadcast()
	return nil
}

// memoryTransport is one end of the connection: it reads what the other end
// wrote and writes what the other end will read.
type memoryTransport struct {
	incoming *memoryStream
	outgoing *memoryStream
}

func (t memoryTransport) Read(into []byte) (int, error)  { return t.incoming.Read(into) }
func (t memoryTransport) Write(from []byte) (int, error) { return t.outgoing.Write(from) }
func (t memoryTransport) Close() error {
	_ = t.outgoing.Close()
	return t.incoming.Close()
}

func memoryTransports() (memoryTransport, memoryTransport) {
	toServer, toClient := newMemoryStream(), newMemoryStream()
	return memoryTransport{incoming: toClient, outgoing: toServer},
		memoryTransport{incoming: toServer, outgoing: toClient}
}

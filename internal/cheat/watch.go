package cheat

import "sort"

// A scan finds where a value lives. A watch finds what changes it, which is
// the thing a durable cheat has to be built against: the address moves between
// runs, the instruction that writes it does not.
//
// The platform provides it — only an emulated address space has stores to
// instrument — so a session watches only when its target can.

// WatchHit is one instruction's writes to one watched address.
type WatchHit struct {
	Address uint32
	PC      uint32
	Value   uint32
	Size    uint8
	Count   uint64
}

// WatchTarget is a MemoryTarget that can report what writes an address.
// A target that cannot leaves the session's watch calls answering
// ErrWatchUnsupported rather than silently doing nothing.
type WatchTarget interface {
	Watch(address uint32)
	Unwatch(address uint32)
	ClearWatches()
	Watches() []uint32
	WatchHits() []WatchHit
	WatchHitsOverflowed() bool
}

// ErrWatchUnsupported reports a target with no store instrumentation behind it.
type watchUnsupportedError struct{}

func (watchUnsupportedError) Error() string {
	return "this platform cannot watch writes"
}

// ErrWatchUnsupported is returned by the watch calls on a target that does not
// implement WatchTarget.
var ErrWatchUnsupported error = watchUnsupportedError{}

func (session *Session) watchTarget() (WatchTarget, error) {
	target, ok := session.target.(WatchTarget)
	if !ok {
		return nil, ErrWatchUnsupported
	}
	return target, nil
}

// CanWatch reports whether this session's platform can watch writes, which is
// what a Host asks before offering the control.
func (session *Session) CanWatch() bool {
	_, ok := session.target.(WatchTarget)
	return ok
}

// Watch records what writes address.
func (session *Session) Watch(address uint32) error {
	target, err := session.watchTarget()
	if err != nil {
		return err
	}
	target.Watch(address)
	return nil
}

// Unwatch stops watching address and drops what it recorded.
func (session *Session) Unwatch(address uint32) error {
	target, err := session.watchTarget()
	if err != nil {
		return err
	}
	target.Unwatch(address)
	return nil
}

// ClearWatches stops watching everything.
func (session *Session) ClearWatches() error {
	target, err := session.watchTarget()
	if err != nil {
		return err
	}
	target.ClearWatches()
	return nil
}

// Watches lists the watched addresses.
func (session *Session) Watches() ([]uint32, error) {
	target, err := session.watchTarget()
	if err != nil {
		return nil, err
	}
	addresses := target.Watches()
	sort.Slice(addresses, func(left, right int) bool { return addresses[left] < addresses[right] })
	return addresses, nil
}

// WatchHits reports what has written the watched addresses, most frequent
// first, and whether the record is complete.
func (session *Session) WatchHits() (hits []WatchHit, overflowed bool, err error) {
	target, targetErr := session.watchTarget()
	if targetErr != nil {
		return nil, false, targetErr
	}
	return target.WatchHits(), target.WatchHitsOverflowed(), nil
}

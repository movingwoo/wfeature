package backend

import "time"

// The bounds a title's own code is held to on every platform, which a Host
// stopping its games waits out (see webhost.StopDeadline). They are kept here,
// below every platform and every Host, so that each wait derived from them
// moves with them rather than being a second number that can drift past them.

// WaitBound is how long one call a title makes may wait on the clock before it
// fails: a KTF service call polling the time inside a delay loop of its own,
// the longest legitimate one an opening sequence at 5.2 seconds. A round of a
// running game is inside such a call for at most this long.
const WaitBound = 15 * time.Second

// EndingBound is how long a title's own ending may run on every platform: an
// SKT destroyApp, an LGT destroyClet, a KTF Jlet's destroyApp.
const EndingBound = 15 * time.Second

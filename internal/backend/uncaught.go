package backend

// UncaughtCallbacks counts the guest callbacks that ended in an exception
// nothing caught, and keeps what the first one said. A platform absorbs such an
// exception so the next callback still runs, as the language has it for a
// thread; counting them is what keeps a title that fails every paint from
// reading as a title that plays. It is not safe for concurrent use: a platform
// whose callbacks run on more than one goroutine guards it itself.
type UncaughtCallbacks struct {
	count uint64
	first string
}

// Note records one callback an exception ended.
func (uncaught *UncaughtCallbacks) Note(callback string, err error) {
	uncaught.count++
	if uncaught.first == "" {
		uncaught.first = callback + ": " + err.Error()
	}
}

// Report answers how many callbacks ended in an exception nothing caught, and
// what the first one said. A Host reads it the way it reads a tick error: the
// session survived, and this is what it survived.
func (uncaught *UncaughtCallbacks) Report() (uint64, string) {
	if uncaught == nil {
		return 0, ""
	}
	return uncaught.count, uncaught.first
}

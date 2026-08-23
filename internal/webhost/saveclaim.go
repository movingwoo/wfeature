package webhost

import "time"

// One game, one save directory, one session.
//
// A save lives in a directory named by the archive, not by the page that
// opened it, so two pages that start the same game get one directory and two
// emulators writing into it. Nothing about the store notices: each session
// reads the files at the moment the guest asks and writes them back whole, so
// the second one to write wins and the first player's progress is gone with no
// error anywhere. It is the ordinary case rather than a contrived one — a
// phone and a desktop on the same household server, or one browser with the
// game open in two tabs.
//
// So a session **claims** its save directory for as long as it holds it, and a
// start that cannot get the claim is refused rather than run. The claim is the
// game's, not the connection's: it is taken when a game starts, released where
// the game is closed, and it travels with a game that is parked, because a
// parked game still owns the files it will write when its page comes back.
//
// A **parked** holder is taken over instead of refusing. Nobody is watching a
// parked game — it does not tick — and the person asking for it is here now,
// so the parked game is closed and the new one starts. That costs the parked
// game its resume window, which is the smaller loss: the alternative is a
// player locked out of their own game for five minutes by a tab they already
// closed.
//
// The save API takes the same claim for the length of one write, which is what
// puts it under this rule rather than beside it: a `PUT` into a directory a
// game holds is refused instead of landing under a session that will write the
// whole file back over it, and a game starting while a write is in flight
// waits the write out inside the grace below. The native CLI is the one road
// left unarbitrated — it is another process and this claim is in memory; see
// "What is not solved" in `docs/session.md`.

// claimGrace is how long a start waits for a claim it found held by a live
// session. It is there for one sequence: a page that reloads — which the
// restart button does, and a phone coming back from the app switcher may —
// opens its new socket before the server has noticed the old one is gone, so
// the game it is starting is still held by a session that is a moment away
// from parking. Waiting that gap out is what keeps a page from being refused
// by itself. A session that is genuinely playing is still playing when the
// grace runs out, so this costs a real refusal a two second delay and nothing
// else.
const claimGrace = 2 * time.Second

// saveClaim is one held save directory.
type saveClaim struct {
	// label is the game as the page named it, so a refusal can say what is
	// holding the directory.
	label string
	// parked reports that the holder is waiting for its page rather than
	// playing, which is what makes it takeable.
	parked bool
}

// waitToClaimSaveDirectory takes a claim, waiting out a holder that is on its
// way to parking; see claimGrace. It reports what claimSaveDirectory reports.
func (s *Server) waitToClaimSaveDirectory(directory, label string) (bool, string) {
	if directory == "" {
		return true, ""
	}
	deadline := time.Now().Add(claimGrace)
	for {
		claimed, holder := s.claimSaveDirectory(directory, label)
		if claimed || !time.Now().Before(deadline) {
			return claimed, holder
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// claimSaveDirectory takes the claim on a save directory for a game that is
// starting. It reports whether the claim was taken and, when it was not, the
// label of the live session that holds it.
func (s *Server) claimSaveDirectory(directory, label string) (bool, string) {
	if directory == "" {
		return true, ""
	}
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	if held, ok := s.claims[directory]; ok {
		if !held.parked {
			return false, held.label
		}
		// The holder is parked, so it is closed here and the directory taken.
		// Dropping it releases the claim, which is why this does not delete
		// the entry itself.
		for token, parked := range s.parked {
			if parked.saveDirectory == directory {
				s.dropLocked(token, parked, "another page started the same game")
				break
			}
		}
		// A claim with no parked game behind it is a game that was closing as
		// this start arrived; the directory is free either way.
		delete(s.claims, directory)
	}
	if s.claims == nil {
		s.claims = make(map[string]*saveClaim)
	}
	s.claims[directory] = &saveClaim{label: label}
	return true, ""
}

// holdSaveDirectory takes the claim for something that is not a game — the
// save API writing one entry — and reports whether it got it, naming the
// holder when it did not. It differs from claimSaveDirectory in the one way
// that matters: a parked game is a holder here rather than something to take
// over. Nobody would trade a player's parked game for a tool's write, and the
// caller has somewhere to put the refusal.
func (s *Server) holdSaveDirectory(directory, label string) (bool, string) {
	if directory == "" {
		return true, ""
	}
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	if held, ok := s.claims[directory]; ok {
		return false, held.label
	}
	if s.claims == nil {
		s.claims = make(map[string]*saveClaim)
	}
	s.claims[directory] = &saveClaim{label: label}
	return true, ""
}

// releaseSaveDirectory gives up a claim when its game is closed.
func (s *Server) releaseSaveDirectory(directory string) {
	if directory == "" {
		return
	}
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	s.releaseSaveDirectoryLocked(directory)
}

// releaseSaveDirectoryLocked is releaseSaveDirectory for a caller that already
// holds the mutex, which is every path that closes a parked game.
func (s *Server) releaseSaveDirectoryLocked(directory string) {
	if directory == "" {
		return
	}
	delete(s.claims, directory)
}

// markSaveDirectoryParked records whether the game holding a directory is
// waiting for a page. Only a parked holder can be taken over.
func (s *Server) markSaveDirectoryParked(directory string, parked bool) {
	if directory == "" {
		return
	}
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	if held, ok := s.claims[directory]; ok {
		held.parked = parked
	}
}

// claimCount is how many save directories are held, which is what a test reads
// to tell a released claim from one that outlived its game.
func (s *Server) claimCount() int {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	return len(s.claims)
}

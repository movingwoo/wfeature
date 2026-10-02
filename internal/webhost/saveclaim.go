package webhost

import (
	"errors"

	"github.com/movingwoo/wfeature/internal/backend"
)

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
// WebSocket starts require explicit approval before closing a parked holder;
// startapproval.go checks that approval and takes the claim atomically. The
// helpers below retain takeover behavior for save restoration and internal
// callers. Browser-token resumption keeps the existing game and its claim.
//
// The save API takes the same claim for the length of one write, which is what
// puts it under this rule rather than beside it: a `PUT` into a directory a
// game holds is refused instead of landing under a session that will write the
// whole file back over it, and a game starting while a write is in flight
// is refused until the write finishes. It is refused by a parked holder
// too, because nobody asked for it. A save import is a person asking, so it
// takes a parked holder over the way a start does — see holdSaveDirectory.
// The claim also holds a backend file lock, excluding other server processes
// and CLI tools that claim the same directory. A save location that cannot
// hold that lock — a folder that cannot be written, a file system without
// locks — is claimed in this process alone, which is what the claim was before
// the lock existed; the server says so in its log, once per directory.

// saveClaim is one held save directory.
type saveClaim struct {
	// label is the game as the page named it, so a refusal can say what is
	// holding the directory.
	label string
	// parked reports that the holder is waiting for its page rather than
	// playing, which is what makes it takeable.
	parked  bool
	release func()
}

// The caller holds parkedMu and has already released any approved predecessor.
// The error is the backend's own, so a caller can tell a directory somebody
// holds from one that could not be prepared.
func (s *Server) takeSaveClaimLocked(directory, label string) error {
	release, err := backend.ClaimSaveDirectory(directory)
	if err != nil {
		s.logger.Warn("save directory claim refused", "directory", directory, "error", err)
		return err
	}
	// The store has no logger, so a claim is where the server reports a
	// directory that only this process is kept out of.
	backend.WarnSaveLockFallback(s.logger, directory)
	if s.claims == nil {
		s.claims = make(map[string]*saveClaim)
	}
	s.claims[directory] = &saveClaim{label: label, release: release}
	return nil
}

// saveClaimRefusal words a refused claim for the person starting a game. Only
// contention has a remedy in another window, so only contention is told to
// close one: a save folder that could not be prepared would otherwise send
// somebody looking for a game that is not running. Both end with the backend's
// own sentence, which is what names the cause.
func saveClaimRefusal(err error) string {
	if errors.Is(err, backend.ErrSaveDirectoryBusy) {
		return "세이브를 사용할 수 없습니다. 다른 실행 중인 게임이나 도구를 종료한 뒤 다시 시도하세요. " + err.Error()
	}
	return "세이브 폴더를 준비하지 못했습니다. 다른 게임이 사용 중인 것이 아니라 폴더 자체의 문제입니다. " + err.Error()
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
		s.takeParkedHolderLocked(directory, "another page started the same game")
	}
	if err := s.takeSaveClaimLocked(directory, label); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// takeParkedHolderLocked closes the parked game holding a directory and leaves
// the directory free. The caller holds parkedMu and has already established
// that the holder is parked; `reason` is what the page that comes back for the
// game is told. Dropping the parked session releases its claim, which is why
// the delete below is for the other case: a claim with no parked game behind
// it is a game that was closing as this request arrived, and the directory is
// free either way.
func (s *Server) takeParkedHolderLocked(directory, reason string) {
	for token, parked := range s.parked {
		if parked.saveDirectory == directory {
			s.dropLocked(token, parked, reason)
			return
		}
	}
	s.releaseSaveDirectoryLocked(directory)
}

// holdSaveDirectory takes the claim for something that is not a game, and
// reports whether it got it, naming the holder when it did not.
//
// `takeParked` is the whole difference between its two callers, and the split
// is about who is asking rather than about what is written. The save API is a
// guest writing one entry with nobody watching, so a parked game outranks it:
// nobody would trade a player's parked game for a write they did not ask for.
// A save import is the opposite — a person on the pre-start screen who chose
// the file and the game it replaces — and there the rule at the top of this
// file applies unchanged: nobody is watching a parked game, the person asking
// is here now, so the parked game is closed and the restore proceeds.
//
// Refusing a parked holder there was a lock with no key. Parking is what
// survives a page reload, so the refusal outlived every reload the person
// tried, and the message told them to stop a game in a window that was already
// gone. The import buttons sit on the screen where the game is not running,
// which is exactly where its own parked session is the likeliest holder.
func (s *Server) holdSaveDirectory(directory, label string, takeParked bool) (bool, string) {
	if directory == "" {
		return true, ""
	}
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	if held, ok := s.claims[directory]; ok {
		if !held.parked || !takeParked {
			return false, held.label
		}
		s.takeParkedHolderLocked(directory, "a save was restored into this game")
	}
	if err := s.takeSaveClaimLocked(directory, label); err != nil {
		return false, err.Error()
	}
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
	if held := s.claims[directory]; held != nil && held.release != nil {
		held.release()
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

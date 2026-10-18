package webhost

import (
	"strconv"
	"time"
)

// An approval names one retained instance, not a reusable permission to evict.
// Recheck under the ownership lock so a dialog cannot approve a different game.
type startApproval struct {
	id, path, token string
	victim          *parkedSession
	parkedAt        time.Time
}

func (r *sessionRunner) admitStart(message clientMessage, directory, label string) bool {
	s := r.server
	s.parkedMu.Lock()
	var reply *serverMessage
	defer func() {
		s.parkedMu.Unlock()
		if reply != nil {
			r.send(*reply)
		}
	}()
	fail := func(text string) bool {
		r.startApproval = nil
		reply = &serverMessage{Kind: serverError, ID: message.ID, Message: text}
		return false
	}
	if s.sessionsClosed {
		return fail(s.phrase("start.shutting-down"))
	}
	var victim *parkedSession
	victimToken := ""
	sameGame := false
	if held := s.claims[directory]; directory != "" && held != nil {
		if !held.parked {
			return fail(s.phrase("start.running-elsewhere", "holder", held.label))
		}
		for token, parked := range s.parked {
			if parked.saveDirectory == directory {
				victim, victimToken, sameGame = parked, token, true
				break
			}
		}
		if victim == nil {
			return fail(s.phrase("start.save-busy"))
		}
	}
	// Only admitted games consume capacity; pending dialogs reserve no room.
	if victim == nil && s.retainedCountLocked() >= maxRetainedSessions {
		for token, parked := range s.parked {
			if victim == nil || parked.parkedAt.Before(victim.parkedAt) {
				victim, victimToken = parked, token
			}
		}
		if victim == nil {
			return fail(s.phrase("start.full", "limit", strconv.Itoa(maxRetainedSessions)))
		}
	}
	if victim != nil {
		approval := r.startApproval
		if approval == nil || message.Confirmation != approval.id || approval.path != message.Game || approval.token != message.Token || approval.victim != victim || approval.parkedAt != victim.parkedAt {
			id, err := newResumeToken()
			if err != nil {
				return fail(s.phrase("start.approval-failed"))
			}
			r.startApproval = &startApproval{id: id, path: message.Game, token: message.Token, victim: victim, parkedAt: victim.parkedAt}
			text := s.phrase("start.replace-oldest", "limit", strconv.Itoa(maxRetainedSessions), "game", victim.label)
			if sameGame {
				text = s.phrase("start.replace-same", "game", victim.label)
			}
			reply = &serverMessage{Kind: serverResult, ID: message.ID, Confirmation: id, Message: text}
			return false
		}
		s.dropLocked(victimToken, victim, "confirmed fresh start")
	}
	r.startApproval = nil
	if directory != "" {
		if err := s.takeSaveClaimLocked(directory, label); err != nil {
			return fail(s.saveClaimRefusal(err))
		}
	}
	r.admitted = true
	r.admittedAs = label
	return true
}

func (s *Server) retainedCountLocked() int {
	count := len(s.parked)
	for _, runner := range s.attached {
		if runner.admitted {
			count++
		}
	}
	return count
}

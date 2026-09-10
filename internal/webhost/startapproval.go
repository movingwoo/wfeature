package webhost

import (
	"fmt"
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
		return fail("서버가 종료 중입니다.")
	}
	var victim *parkedSession
	victimToken := ""
	sameGame := false
	if held := s.claims[directory]; directory != "" && held != nil {
		if !held.parked {
			return fail(fmt.Sprintf("다른 창에서 이미 실행 중입니다(%s). 그 창에서 게임을 멈춘 뒤 다시 시작하세요.", held.label))
		}
		for token, parked := range s.parked {
			if parked.saveDirectory == directory {
				victim, victimToken, sameGame = parked, token, true
				break
			}
		}
		if victim == nil {
			return fail("세이브를 사용 중입니다. 잠시 후 다시 시도하세요.")
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
			return fail("게임 4개가 모두 실행 중입니다. 하나를 종료한 뒤 시작하세요.")
		}
	}
	if victim != nil {
		approval := r.startApproval
		if approval == nil || message.Confirmation != approval.id || approval.path != message.Game || approval.token != message.Token || approval.victim != victim || approval.parkedAt != victim.parkedAt {
			id, err := newResumeToken()
			if err != nil {
				return fail("시작 확인을 준비하지 못했습니다.")
			}
			r.startApproval = &startApproval{id: id, path: message.Game, token: message.Token, victim: victim, parkedAt: victim.parkedAt}
			text := fmt.Sprintf("게임은 실행·대기 합계 4개까지 유지됩니다. 가장 오래된 대기 게임(%s)을 종료하고 시작할까요? 저장하지 않은 진행은 사라집니다.", victim.label)
			if sameGame {
				text = fmt.Sprintf("다른 기기에 이어할 게임(%s)이 있습니다. 새로 시작하면 저장하지 않은 진행이 사라집니다. 새로 시작할까요?", victim.label)
			}
			reply = &serverMessage{Kind: serverResult, ID: message.ID, Confirmation: id, Message: text}
			return false
		}
		s.dropLocked(victimToken, victim, "confirmed fresh start")
	}
	r.startApproval = nil
	r.admitted = true
	if directory != "" {
		if s.claims == nil {
			s.claims = make(map[string]*saveClaim)
		}
		s.claims[directory] = &saveClaim{label: label}
	}
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

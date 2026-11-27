package webhost

import "context"

// The wire token is useful to a browser across reloads. Only the verified
// account selects the slot on a shared host, even on a brand-new device.
func (r *sessionRunner) ownerKey(browserToken string) string {
	if r.login.User.ID != "" {
		return r.login.User.ID
	}
	return browserToken
}

// Reserve before inspecting or starting the archive. A confirmation stays
// with one socket and names one launch across a handoff or a later park.
func (r *sessionRunner) reserveAccountStart(ctx context.Context, message clientMessage) bool {
	s, key := r.server, r.login.User.ID
	for ctx.Err() == nil {
		s.parkedMu.Lock()
		if s.sessionsClosed {
			s.parkedMu.Unlock()
			r.send(serverMessage{Kind: serverError, ID: message.ID, Message: s.phrase("start.shutting-down")})
			return false
		}
		owner, parked := s.attached[key], s.parked[key]
		if owner == nil && parked == nil {
			if s.attached == nil {
				s.attached = make(map[string]*sessionRunner)
			}
			s.attached[key] = r
			r.startApproval = nil
			s.parkedMu.Unlock()
			return true
		}
		var instance *gameInstance
		if owner != nil {
			instance = owner.instance
		} else if parked.closing == nil {
			instance = parked.instance
		}
		if instance == nil {
			s.parkedMu.Unlock()
			r.send(serverMessage{Kind: serverResumed, ID: message.ID, Occupied: true})
			return false
		}
		approval := r.startApproval
		if approval == nil || approval.instance != instance || approval.path != message.Game ||
			approval.token != message.Token || approval.id != message.Confirmation {
			s.parkedMu.Unlock()
			id, err := newResumeToken()
			if err != nil {
				r.send(serverMessage{Kind: serverError, ID: message.ID, Message: s.phrase("start.approval-failed")})
				return false
			}
			r.startApproval = &startApproval{id: id, path: message.Game, token: message.Token, instance: instance}
			r.send(serverMessage{Kind: serverResult, ID: message.ID, Confirmation: id, Message: s.phrase("start.replace-account", "game", instance.label)})
			return false
		}
		if parked != nil {
			done := s.closeParkedLocked(key, parked, "confirmed replacement")
			s.parkedMu.Unlock()
			select {
			case <-done:
			case <-ctx.Done():
				return false
			}
			continue
		}
		s.parkedMu.Unlock()
		if owner == r {
			r.park()
		} else if err := owner.askHandoff(ctx, key, instance); err != nil {
			r.send(serverMessage{Kind: serverError, ID: message.ID, Message: err.Error()})
			return false
		}
		// The runner parked the approved instance or its game changed while
		// this request was in flight. Check the slot again before closing it.
	}
	return false
}

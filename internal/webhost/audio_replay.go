package webhost

func negotiatedPCM(value string, ownership bool) bool {
	return value == "1" && ownership
}

// PCMChannels describes the current page, which can change when a parked
// session reconnects. Incapable pages receive onset-scaled ordinary waves.
func (a *audioCollector) PCMChannels() bool {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.pcmChannels
}

// AudioTime selects the presentation instant for subsequent serialized sink
// calls. Each value gets separate storage because queued events outlive it.
func (a *audioCollector) AudioTime(seconds float64) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.at = &seconds
}

func (a *audioCollector) withClock(events []audioEvent) []audioEvent {
	if a.at != nil {
		events = append(events, audioEvent{Kind: audioClock, At: a.at})
	}
	return events
}

// A full reconstruction can contain 256 sound owners plus the legacy owner,
// 16 channels and 14 channel operations, plus 24 voices and 256 sampled sources.
// Another 2,048 PCM groups require at most three control operations each.
// Ordinary guest batches keep the smaller limit. This ceiling also bounds an
// interrupted replay.
const maxReplayAudio = 65536

func (a *audioCollector) IndependentAudioReplay() bool {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return !a.legacyReplay
}

func (a *audioCollector) takeBatch() ([]audioEvent, bool) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	events, overflow := a.withClock(a.events), a.overflowed
	a.events, a.overflowed = nil, false
	return events, overflow
}

func (a *audioCollector) collectReplay(replay func()) ([]audioEvent, bool) {
	a.mutex.Lock()
	a.events, a.overflowed, a.replaying = nil, false, true
	a.mutex.Unlock()
	replay()
	a.mutex.Lock()
	defer a.mutex.Unlock()
	events, overflow := a.withClock(a.events), a.overflowed
	a.events, a.overflowed, a.replaying = nil, false, false
	return events, overflow
}

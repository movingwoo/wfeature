package webhost

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/session"
)

// replaySignals reads the starts, gains and cancellations the page receives.
// Existing stream tests check the remaining controller and definition operands.
func replaySignals(t *testing.T, message outboundMessage) []audioEvent {
	t.Helper()
	if !message.audio {
		return nil
	}
	if message.binary == nil {
		var decoded serverMessage
		if err := json.Unmarshal([]byte(message.text), &decoded); err != nil {
			t.Fatal(err)
		}
		var events []audioEvent
		for _, event := range decoded.Audio {
			if event.Kind == audioNoteOn || event.Kind == audioNoteResume || event.Kind == audioNoteOff || event.Kind == audioStopSound || event.Kind == audioSoundGain || event.Kind == audioAllOff {
				events = append(events, event)
			}
		}
		return events
	}
	var events []audioEvent
	var sound uint32
	for _, operation := range readAudio(t, message.binary) {
		switch operation.op {
		case audioOpSelectSound:
			sound = operation.id
		case audioOpNoteOn, audioOpNoteOff:
			kind := audioNoteOn
			if operation.op == audioOpNoteOff {
				kind = audioNoteOff
			}
			events = append(events, audioEvent{Kind: kind, Sound: sound, Channel: operation.operands[0], Note: operation.operands[1], Velocity: operation.operands[2]})
		case audioOpStopSound:
			events = append(events, audioEvent{Kind: audioStopSound, Sound: sound})
		case audioOpNoteResume:
			events = append(events, audioEvent{Kind: audioNoteResume, Sound: sound, Channel: operation.operands[0], Note: operation.operands[1], Velocity: operation.operands[2], Age: binary.BigEndian.Uint32(operation.operands[3:])})
		case audioOpSoundGain:
			events = append(events, audioEvent{Kind: audioSoundGain, Sound: sound, Value: binary.BigEndian.Uint16(operation.operands)})
		case audioOpAllOff:
			events = append(events, audioEvent{Kind: audioAllOff})
		}
	}
	return events
}

func drainReplaySignals(t *testing.T, runner *sessionRunner) []audioEvent {
	t.Helper()
	var events []audioEvent
	for len(runner.outText) > 0 {
		events = append(events, replaySignals(t, <-runner.outText)...)
	}
	return events
}

func requireAudioFlushReturns(t *testing.T, runner *sessionRunner) {
	t.Helper()
	if !finishes(runner.flushAudio) {
		close(runner.writerDone)
		t.Fatal("audio cancellation blocked the guest on a full connection")
	}
}

func TestAudioCancellationDropRetriesResetWithoutAnotherGuestEvent(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		t.Run(fmt.Sprintf("protocol_%d", protocol), func(t *testing.T) {
			runner := stalledRunner(t, 1)
			runner.protocol, runner.soundOwnership = protocol, true
			runner.audio = &audioCollector{}
			runner.audio.AudioEvent(17, smaf.Event{Type: smaf.EventNoteOn, Note: 60, Velocity: 100})
			runner.flushAudio()
			runner.audio.StopSound(17)
			requireAudioFlushReturns(t, runner)
			if !runner.audioNeedsReset || runner.shed.Load() != 1 {
				t.Fatal("a dropped cancellation did not retain its recovery request")
			}
			requireAudioFlushReturns(t, runner)
			if !runner.audioNeedsReset {
				t.Fatal("an empty tick forgot the pending reset while the connection stayed full")
			}
			initial := drainReplaySignals(t, runner)
			if len(initial) != 1 || initial[0].Kind != audioNoteOn || initial[0].Sound != 17 {
				t.Fatalf("initial delivered sound = %+v", initial)
			}
			requireAudioFlushReturns(t, runner)
			if runner.audioNeedsReset {
				t.Fatal("a delivered reset remained pending")
			}
			if got := drainReplaySignals(t, runner); !reflect.DeepEqual(got, []audioEvent{{Kind: audioAllOff}}) {
				t.Fatalf("empty-tick recovery = %+v, want one global cancellation", got)
			}
			runner.flushAudio()
			if len(runner.outText) != 0 {
				t.Fatal("successful recovery kept replaying on later empty ticks")
			}
		})
	}
}

func TestAudioCollectorOverflowPreservesCancellationAndRequestsRecovery(t *testing.T) {
	collector := &audioCollector{}
	for range maxPendingAudio {
		collector.MIDINoteOn(0, 60, 100)
	}
	collector.StopSound(17)
	events, overflow := collector.takeBatch()
	if !overflow || len(events) > maxPendingAudio {
		t.Fatalf("overflow flag=%t, retained events=%d", overflow, len(events))
	}
	reset, stopped := false, false
	for _, event := range events {
		reset = reset || event.Kind == audioAllOff
		stopped = stopped || event.Kind == audioStopSound && event.Sound == 17
	}
	if !reset || !stopped {
		t.Fatalf("overflow lost its global reset or triggering cancellation: %+v", events)
	}
	if events, overflow = collector.takeBatch(); len(events) != 0 || overflow {
		t.Fatal("consuming the batch did not consume its overflow report")
	}
}

func newAudioReplayChannels() *[16]backend.AudioChannelState {
	channels := new([16]backend.AudioChannelState)
	for index := range channels {
		channels[index] = backend.AudioChannelState{Volume: 100, Expression: 127, Pan: 64, Bend: 8192,
			BendSemitones: 2, RPNMSB: 127, RPNLSB: 127, NRPNMSB: 127, NRPNLSB: 127}
	}
	return channels
}

// applyAudioReplayChannel interprets the controller messages a receiver gets;
// it does not use the backend's controller or replay implementation.
func applyAudioReplayChannel(state *backend.AudioChannelState, event audioEvent) {
	switch event.Kind {
	case audioProgramChange:
		state.Program = event.Program
	case audioPitchBend:
		state.Bend = event.Value
	case audioControlChange:
		value := uint8(event.Value)
		switch event.Control {
		case 7:
			state.Volume = value
		case 10:
			state.Pan = value
		case 11:
			state.Expression = value
		case 64:
			state.Sustain = value
		case 101:
			state.RPNMSB, state.NRPN = value, false
		case 100:
			state.RPNLSB, state.NRPN = value, false
		case 99:
			state.NRPNMSB, state.NRPN = value, true
		case 98:
			state.NRPNLSB, state.NRPN = value, true
		case 6, 38:
			if !state.NRPN && state.RPNMSB == 0 && state.RPNLSB == 0 {
				if event.Control == 6 {
					state.BendSemitones, state.BendCents = value, 0
				} else {
					state.BendCents = min(value, 99)
				}
			}
		}
	}
}

func TestAudioReplayFitsEveryLoadedSoundAndReturnsToOrdinaryBound(t *testing.T) {
	now := time.Unix(1700000000, 0)
	audio := backend.NewAudioWithClock(nil, func() time.Time { return now })
	audio.SetVolume(50)
	for owner := range 256 {
		var events []smaf.Event
		for group := uint16(1); group <= 8; group++ {
			events = append(events,
				smaf.Event{Type: smaf.EventPCMControl, PCMChannel: group, Control: 7, Value: uint8(31 + (owner+int(group))%97)},
				smaf.Event{Type: smaf.EventPCMControl, PCMChannel: group, Control: 11, Value: uint8(127 - (owner+int(group))%64)},
				smaf.Event{Type: smaf.EventPCMControl, PCMChannel: group, Control: 10, Value: uint8((owner*3 + int(group)) % 128)},
			)
		}
		for channel := range uint8(16) {
			events = append(events,
				smaf.Event{Type: smaf.EventProgramChange, Channel: channel, Program: uint8(owner+int(channel)) % 128},
				smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 7, Value: uint8(32 + owner%96)},
				smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 10, Value: uint8(owner*3+int(channel)) % 128},
				smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 101, Value: 0},
				smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 100, Value: 0},
				smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 6, Value: uint8(owner+int(channel)) % 128},
				smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 38, Value: uint8((owner*3 + int(channel)) % 100)},
				smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 99, Value: uint8(owner) % 128},
				smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 98, Value: channel},
				smaf.Event{Type: smaf.EventPitchBend, Channel: channel, Bend: uint16(8192 + owner)},
			)
			// Preserve inactive selector bytes as well as the active family.
			// A later one-byte selector change must still select the same RPN.
			selector := [2]uint8{0, 0}
			switch (owner + int(channel)) % 5 {
			case 1:
				selector = [2]uint8{127, 127} // Null.
			case 2:
				selector = [2]uint8{0, 127} // Partial selection.
			case 3, 4:
				selector = [2]uint8{3, 2} // Unsupported RPN.
			}
			events = append(events,
				smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 101, Value: selector[0]},
				smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 100, Value: selector[1]},
			)
			if (owner+int(channel))%5 == 4 {
				events = append(events, smaf.Event{Type: smaf.EventControlChange, Channel: channel, Control: 98, Value: channel})
			}
		}
		events = append(events,
			smaf.Event{Type: smaf.EventNoteOn, Note: 60, Velocity: 100},
			smaf.Event{Type: smaf.EventWave, PCMChannel: 1, WaveChannels: 1, SamplingRate: 8, Wave: []int16{int16(owner), 1, 2, 3, 4, 5, 6, 7}},
			smaf.Event{Type: smaf.EventEnd, Time: 60000},
		)
		handle, err := audio.LoadEvents(events)
		if err != nil {
			t.Fatal(err)
		}
		if err := audio.SetSoundVolume(handle, (owner%3)*50); err != nil {
			t.Fatal(err)
		}
		if err := audio.Play(handle, 0, false); err != nil {
			t.Fatal(err)
		}
	}
	audio.Advance(0)
	want, err := audio.CaptureState()
	if err != nil {
		t.Fatal(err)
	}
	if len(want.Output.Sounds) != 256 || len(want.Output.Notes) != 24 || len(want.Output.Waves) != 256 {
		t.Fatal("fixture did not reach the supported sound, voice and PCM limits")
	}
	collector := &audioCollector{pcmChannels: true}
	audio.SetSink(collector)
	events, overflow := collector.collectReplay(audio.ResumeOutput)
	if overflow || len(events) <= maxPendingAudio || len(events) > maxReplayAudio {
		t.Fatalf("full output replay produced %d events, overflow=%t", len(events), overflow)
	}
	channels := make(map[uint32]*[16]backend.AudioChannelState)
	waves := make(map[uint32][]byte)
	notes := make(map[uint32]bool)
	gains := make(map[uint32]uint16)
	pcmControls := make(map[[3]uint32]uint16)
	for _, event := range events {
		states := channels[event.Sound]
		if states == nil {
			states = newAudioReplayChannels()
			channels[event.Sound] = states
		}
		applyAudioReplayChannel(&states[event.Channel], event)
		switch event.Kind {
		case audioSoundGain:
			gains[event.Sound] = event.Value
		case audioPCMControl:
			key := [3]uint32{event.Sound, uint32(event.PCMChannel), uint32(event.Control)}
			if _, duplicate := pcmControls[key]; duplicate {
				t.Fatalf("PCM control replayed twice: %v", key)
			}
			pcmControls[key] = event.Value
		case audioPlayWave:
			if _, exists := gains[event.Sound]; !exists {
				t.Fatalf("PCM for owner %d replayed before its gain", event.Sound)
			}
			if _, exists := waves[event.Sound]; exists {
				t.Fatalf("PCM for owner %d replayed twice", event.Sound)
			}
			if event.PCMChannel != 1 {
				t.Fatalf("PCM owner %d lost its channel: %d", event.Sound, event.PCMChannel)
			}
			for _, control := range []uint32{7, 11, 10} {
				if _, exists := pcmControls[[3]uint32{event.Sound, 1, control}]; !exists {
					t.Fatalf("PCM owner %d replayed before control %d", event.Sound, control)
				}
			}
			waves[event.Sound] = event.pcm
		case audioNoteResume:
			if _, exists := gains[event.Sound]; !exists {
				t.Fatalf("note for owner %d replayed before its gain", event.Sound)
			}
			notes[event.Sound] = true
		case audioAllOff:
			t.Fatal("bounded replay discarded earlier output with a collector reset")
		}
	}
	for _, sound := range want.Output.Sounds {
		if got, exists := gains[uint32(sound.Sound)]; !exists || got != sound.Gain {
			t.Fatalf("replay gain for owner %d = %d, present=%t, want %d", sound.Sound, got, exists, sound.Gain)
		}
		if got := channels[uint32(sound.Sound)]; got == nil || *got != sound.Channels {
			t.Fatalf("replay lost channels for owner %d", sound.Sound)
		}
		owner := int(sound.Sound) - 1
		for group := uint32(1); group <= 8; group++ {
			for control, expected := range map[uint32]uint16{
				7: uint16(31 + (owner+int(group))%97), 11: uint16(127 - (owner+int(group))%64), 10: uint16((owner*3 + int(group)) % 128),
			} {
				key := [3]uint32{uint32(sound.Sound), group, control}
				if value, exists := pcmControls[key]; !exists || value != expected {
					t.Fatalf("replay lost PCM control %v: got %d, present=%t, want %d", key, value, exists, expected)
				}
			}
		}
	}
	for _, wave := range want.Output.Waves {
		got := waves[uint32(wave.Sound)]
		if len(got) != len(wave.Samples)*2 || int16(binary.LittleEndian.Uint16(got)) != wave.Samples[0] {
			t.Fatalf("replay changed PCM content or owner %d", wave.Sound)
		}
	}
	for _, note := range want.Output.Notes {
		if !notes[uint32(note.Sound)] {
			t.Fatalf("replay lost sounding owner %d", note.Sound)
		}
	}
	if len(waves) != 256 || len(notes) != 24 {
		t.Fatalf("replayed waves=%d, notes=%d", len(waves), len(notes))
	}
	if len(pcmControls) != 2048*3 {
		t.Fatalf("replayed %d PCM controls, want three for all 2048 groups", len(pcmControls))
	}
	for range maxPendingAudio {
		collector.MIDINoteOn(0, 60, 100)
	}
	collector.StopSound(17)
	events, overflow = collector.takeBatch()
	if !overflow || len(events) > maxPendingAudio {
		t.Fatal("a completed replay left the ordinary collector budget enlarged")
	}
}

// audioReplayArchive is an authored SGS program: startup holds one note, and
// any key stops it. No timer or lifecycle callback restarts the sound.
func audioReplayArchive(t *testing.T) []byte {
	t.Helper()
	chunk := func(tag string, data []byte) []byte {
		out := append([]byte(tag), byte(len(data)>>24), byte(len(data)>>16), byte(len(data)>>8), byte(len(data)))
		return append(out, data...)
	}
	sequence := []byte{0, 0x90, 60, 100, 0xff, 0x7f, 0, 0xff, 0x2f, 0}
	track := append([]byte{2, 0, 2, 2}, make([]byte, 16)...)
	track = append(track, chunk("Mtsq", sequence)...)
	wave := chunk("MMMD", append(chunk("MTR\x00", track), 0, 0))
	resource := append([]byte{0, 0}, wave...)
	data := make([]byte, 52)
	data[0] = 1
	copy(data[10:26], "Audio replay")
	for index, code := range [][]byte{{5, 0, 0x90, 0xff}, {0xff}, {0xff}, {0x91, 0xff}} {
		binary.LittleEndian.PutUint16(data[28+index*2:], uint16(len(data)))
		data = append(data, code...)
	}
	variables := len(data)
	for range 16 {
		data = append(data, 1, 1, 0, 0)
	}
	initializers := len(data)
	resources := len(data)
	data = append(data, 0, 8, byte(len(resource)), byte(len(resource)>>8))
	resourceData := len(data)
	data = append(data, resource...)
	for index, offset := range []int{variables, initializers, resources, resourceData} {
		binary.LittleEndian.PutUint16(data[44+index*2:], uint16(offset))
	}
	var descriptor []byte
	for _, value := range []string{"application/x-gnex-sgs", "SGS"} {
		descriptor = binary.LittleEndian.AppendUint32(descriptor, uint32(len(value)))
		descriptor = append(descriptor, value...)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, file := range []struct {
		name string
		data []byte
	}{{"replay.mod", descriptor}, {"replay.sgs", data}} {
		entry, err := writer.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func TestAudioReplayUsesLiveGuestStateAfterDropsQuickLoadAndReconnect(t *testing.T) {
	for _, protocol := range []int{protocolPictures, protocolStream} {
		t.Run(fmt.Sprintf("protocol_%d", protocol), func(t *testing.T) {
			root := t.TempDir()
			writeCheckpointGame(t, root, "skt", audioReplayArchive(t))
			server := checkpointServer(t, root)
			newRunner := func() *sessionRunner {
				return &sessionRunner{server: server, protocol: protocol, soundOwnership: true,
					frames: make(chan pendingFrame, 1), outText: make(chan outboundMessage, 64), writerDone: make(chan struct{})}
			}
			runner, resumed := newRunner(), newRunner()
			t.Cleanup(func() {
				runner.closeGame()
				resumed.closeGame()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				server.CloseSessions(ctx)
			})
			runner.startGame(t.Context(), clientMessage{Kind: clientStart, Game: "games/skt/checkpoint.zip", ID: 1})
			if runner.game == nil {
				t.Fatalf("audio fixture failed to start: %+v", readCheckpointReplies(t, runner))
			}
			if _, err := runner.game.Tick(t.Context(), 0); err != nil {
				t.Fatal(err)
			}
			runner.flushAudio()
			initial := drainReplaySignals(t, runner)
			var owner uint32
			for _, event := range initial {
				if event.Kind == audioNoteOn && event.Velocity != 0 {
					owner = event.Sound
				}
			}
			if owner == 0 {
				t.Fatalf("fixture emitted no owned note: %+v", initial)
			}
			runner.quickSave(t.Context(), clientMessage{Kind: clientQuickSave, ID: 2})
			if !runner.started.HasCheckpoint {
				t.Fatalf("audio checkpoint failed: %+v", readCheckpointReplies(t, runner))
			}
			drainReplaySignals(t, runner)
			fillQueue := func() {
				for len(runner.outText) < cap(runner.outText) {
					runner.outText <- outboundMessage{}
				}
			}
			wantOwnedNote := func(events []audioEvent, reset bool) {
				t.Helper()
				if reset && (len(events) == 0 || events[0].Kind != audioAllOff) {
					t.Fatalf("replay did not clear stale output first: %+v", events)
				}
				count := 0
				gain, hasGain := uint16(0), false
				for _, event := range events {
					if event.Kind == audioSoundGain && event.Sound == owner {
						gain, hasGain = event.Value, true
					}
					if event.Kind == audioNoteOn && event.Velocity != 0 {
						if event.Sound != owner || event.Note != 60 {
							t.Fatalf("replay changed note ownership: %+v", event)
						}
						if !hasGain || gain != backend.AudioGainUnity {
							t.Fatalf("replay started a note before restoring its unity gain: gain=%d, present=%t", gain, hasGain)
						}
						count++
					}
				}
				if count != 1 {
					t.Fatalf("replay emitted %d starts, want one held note: %+v", count, events)
				}
			}
			wantOwnedNote(initial, false)

			fillQueue()
			runner.game.ResumeCheckpointOutput()
			requireAudioFlushReturns(t, runner)
			if !runner.audioNeedsReset {
				t.Fatal("dropped live audio did not request reconstruction")
			}
			drainReplaySignals(t, runner)
			runner.flushAudio()
			wantOwnedNote(drainReplaySignals(t, runner), true)

			fillQueue()
			if err := runner.game.SendKey(t.Context(), session.KeyPress, '5'); err != nil {
				t.Fatal(err)
			}
			requireAudioFlushReturns(t, runner)
			if !runner.audioNeedsReset {
				t.Fatal("lost guest cancellation did not request reconstruction")
			}
			drainReplaySignals(t, runner)
			runner.flushAudio()
			if events := drainReplaySignals(t, runner); !reflect.DeepEqual(events, []audioEvent{{Kind: audioAllOff}}) {
				t.Fatalf("recovery restarted the guest's stopped sound: %+v", events)
			}

			runner.quickLoad(t.Context(), clientMessage{Kind: clientQuickLoad, ID: 3})
			wantOwnedNote(drainReplaySignals(t, runner), false)
			token := runner.token
			runner.park()
			resumed.resumeGame(t.Context(), clientMessage{Kind: clientResume, Token: token, ID: 4})
			if resumed.game == nil || resumed.token != token {
				t.Fatalf("parked guest did not reconnect: %+v", readCheckpointReplies(t, resumed))
			}
			wantOwnedNote(drainReplaySignals(t, resumed), true)
		})
	}
}

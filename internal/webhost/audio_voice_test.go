package webhost

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/audio/smaf"
)

func TestVoiceNegotiationNeedsOwnershipAndTheStream(t *testing.T) {
	for _, test := range []struct {
		value     string
		ownership bool
		protocol  int
		want      bool
	}{
		{"1", true, protocolStream, true},
		{"1", false, protocolStream, false},
		{"1", true, protocolPictures, false},
		{"", true, protocolStream, false},
		{"yes", true, protocolStream, false},
	} {
		if got := negotiatedVoice(test.value, test.ownership, test.protocol); got != test.want {
			t.Errorf("negotiatedVoice(%q, %t, %d) = %t", test.value, test.ownership, test.protocol, got)
		}
	}
}

func TestStreamAudioDefinesEachVoiceOnceAndNamesItsSlot(t *testing.T) {
	collector := &audioCollector{}
	voice := []byte{1, 0, 0, 1}
	collector.AudioEvent(3, smaf.Event{Type: smaf.EventVoice, Program: 7, SysEx: voice})
	voice[0] = 9 // The collector owns a copy; the timeline's bytes may change.
	collector.AudioEvent(3, smaf.Event{Type: smaf.EventNoteOn, Channel: 9, Note: 36, Velocity: 90, Program: 7, Value: 39})
	collector.AudioEvent(3, smaf.Event{Type: smaf.EventNoteOn, Channel: 0, Note: 60, Velocity: 80})
	collector.ResumeVoiceNote(3, 2, 64, 70, 7, 52, 1500*time.Millisecond)
	collector.AudioEvent(3, smaf.Event{Type: smaf.EventVoice, Program: 8, SysEx: []byte{1, 0, 0, 1}})
	events, _ := collector.takeBatch()
	var definitions audioDefinitions
	operations := readAudio(t, definitions.encode(events))
	var ops []byte
	for _, operation := range operations {
		ops = append(ops, operation.op)
	}
	want := []byte{audioOpSelectSound, audioOpDefine, audioOpVoice, audioOpVoiceNoteOn, audioOpNoteOn, audioOpVoiceResume, audioOpVoice}
	if !bytes.Equal(ops, want) {
		t.Fatalf("operations % x, want % x", ops, want)
	}
	if !bytes.Equal(operations[1].data, []byte{1, 0, 0, 1}) || operations[2].operands[0] != 7 || operations[2].id != operations[1].id {
		t.Fatalf("definition %+v and voice %+v", operations[1], operations[2])
	}
	if !bytes.Equal(operations[3].operands, []byte{9, 36, 90, 7, 39}) {
		t.Fatalf("voice note operands % x", operations[3].operands)
	}
	if got := operations[5].operands; !bytes.Equal(got[:3], []byte{2, 64, 70}) || binary.BigEndian.Uint32(got[3:7]) != 1500 || got[7] != 7 || got[8] != 52 {
		t.Fatalf("voice resume operands % x", got)
	}
	// The same bytes in another slot are named by the definition already held.
	if operations[6].operands[0] != 8 || operations[6].id != operations[1].id {
		t.Fatalf("second voice %+v", operations[6])
	}
}

func TestPagesWithoutVoicesHearTheSameNotes(t *testing.T) {
	events := withoutVoices([]audioEvent{
		{Kind: audioVoice, Voice: 1, raw: []byte{1}},
		{Kind: audioNoteOn, Channel: 9, Note: 36, Velocity: 90, Voice: 1, Pitch: 39},
		{Kind: audioNoteResume, Channel: 0, Note: 60, Velocity: 80, Voice: 1, Pitch: 48, Age: 5},
	})
	if len(events) != 2 || events[0].Kind != audioNoteOn || events[0].Note != 36 || events[0].Voice != 0 || events[0].Pitch != 0 ||
		events[1].Kind != audioNoteResume || events[1].Voice != 0 || events[1].Pitch != 0 || events[1].Age != 5 {
		t.Fatalf("events = %+v", events)
	}
	var definitions audioDefinitions
	for _, operation := range readAudio(t, definitions.encode(events)) {
		if operation.op == audioOpVoice || operation.op == audioOpVoiceNoteOn || operation.op == audioOpVoiceResume {
			t.Fatalf("a page without voices received % x", operation.op)
		}
	}
}

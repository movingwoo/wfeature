package webhost

import (
	"encoding/json"
	"testing"
)

// The page reads these field names, so they are part of the protocol rather
// than an implementation detail of this struct. `web/vibrate.js` takes `level`
// and `ms`, and a rename here that the page did not follow would be a
// vibration that silently stops happening — the failure this whole path was
// built to end.
func TestVibrateMessageIsTheShapeThePageReads(t *testing.T) {
	body, err := json.Marshal(serverMessage{
		Kind:    serverVibrate,
		Vibrate: &vibrateMessage{Level: 80, Milliseconds: 250},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"kind":"vibrate","vibrate":{"level":80,"ms":250}}`
	if string(body) != want {
		t.Errorf("message = %s\nwant     %s", body, want)
	}
}

// Zero is an answer here, twice over, so neither field may be omitted: a level
// of zero is the guest turning the motor off, and a duration of zero at a level
// above zero is the guest asking for a vibration that runs until it stops it. A
// page that received `{}` for either would read a stop as a buzz or an
// indefinite buzz as nothing.
func TestVibrateMessageSendsItsZeroes(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		message vibrateMessage
		want    string
	}{
		{"the guest turning it off", vibrateMessage{Level: 0, Milliseconds: 0},
			`{"level":0,"ms":0}`},
		{"until stopped", vibrateMessage{Level: 100, Milliseconds: 0},
			`{"level":100,"ms":0}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body, err := json.Marshal(testCase.message)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(body) != testCase.want {
				t.Errorf("message = %s, want %s", body, testCase.want)
			}
		})
	}
}

// A session with no game has nothing to report and must not touch a nil one.
func TestVibrationFlushDoesNothingWithoutAGame(t *testing.T) {
	runner := &sessionRunner{
		server:    newTestServer(t, Options{}),
		outText:   make(chan outboundMessage, 4),
		outFrames: make(chan outboundMessage, 1),
	}
	runner.flushVibration()
	if len(runner.outText) != 0 {
		t.Errorf("a session with no game sent %d messages", len(runner.outText))
	}
}

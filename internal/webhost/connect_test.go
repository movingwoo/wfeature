package webhost

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestConnectAnswersWithTheAddressAPhoneShouldOpen(t *testing.T) {
	const link = "http://192.168.0.5:11541/?k=" + testKey
	server := newTestServer(t, Options{ConnectURL: link})

	recorder := get(t, server, "/api/connect")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/connect = %d, want 200", recorder.Code)
	}
	var answer connectAnswer
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.URL != link {
		t.Errorf("url = %q, want %q", answer.URL, link)
	}
}

// A machine with nothing but loopback, or a server behind a proxy that owns
// the address a phone would use, has no address of its own to hand out. That
// is an ordinary state: the route answers, with nothing in it, and the page
// says so in a sentence rather than showing a link that goes nowhere.
func TestConnectSaysSoWhenThereIsNoAddressToGive(t *testing.T) {
	server := newTestServer(t, Options{})
	recorder := get(t, server, "/api/connect")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/connect = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); body != `{"url":""}` {
		t.Errorf("body = %s", body)
	}
}

// On a public server the answer *is* the key, so this route is inside the gate
// like everything else. A route that handed the link to whoever asked would be
// the door opening for anyone who knocked.
func TestConnectIsBehindTheKey(t *testing.T) {
	server := newTestServer(t, Options{
		AccessKey:  testKey,
		ConnectURL: "http://192.168.0.5:11541/?k=" + testKey,
	})
	recorder := get(t, server, "/api/connect")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("GET /api/connect without the key = %d, want 403", recorder.Code)
	}
	if body := recorder.Body.String(); body != "Forbidden" {
		t.Errorf("the refusal carried %q", body)
	}
}

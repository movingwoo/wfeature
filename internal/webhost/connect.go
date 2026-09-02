package webhost

import (
	"encoding/json"
	"net/http"
)

// The address to open on a phone.
//
// This is the last step of the thing this whole program is arranged around: the
// game runs on a machine in the house and the phone draws what it is sent, and
// the phone has to be told where to look. Until now that was a user reading
// `ipconfig` output and retyping four numbers and a port into a phone keyboard,
// which is the step most people give up on.
//
// **The page cannot work the address out for itself**, and both halves of why
// are worth writing down. Its own `location` is whatever was typed to reach it,
// which on the machine running the server is `127.0.0.1` — an address that
// means the phone, when a phone opens it. And on a `-public` server the link
// needs the access key, which the page cannot read: the cookie holding it is
// HttpOnly on purpose, and the address bar had the key taken out of it the
// moment the link was followed.
//
// So the server answers with the finished link. It is the side that knows its
// own address on the network, and the only side that still has the key.

// connectAnswer is what /api/connect says. One field, because there is one
// thing to say and an empty string is the whole of "there is no address to
// give" — a machine with no network beyond loopback, which is a real state and
// not an error.
type connectAnswer struct {
	// URL is the link to put in front of a phone, key and all.
	URL string `json:"url"`
}

// serveConnect answers with the address a phone should open. It is inside the
// access gate like everything else: on a public server the answer *is* the
// key, so handing it to a caller that arrived without one would be handing the
// door to whoever knocked.
func (s *Server) serveConnect(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	body, err := json.Marshal(connectAnswer{URL: s.connectURL})
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(writer, http.StatusOK, body)
}

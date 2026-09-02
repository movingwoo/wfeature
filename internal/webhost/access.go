package webhost

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
)

// The access key.
//
// This server has never had one, and on a home network that is the right
// trade: anything that can reach the port can list the games and read the
// saves, which is what makes a phone on the same Wi-Fi work with no setup at
// all. It becomes the wrong trade the moment the port is reachable from the
// internet, because the internet contains scanners and they find an open port
// within days.
//
// A key is the smallest thing that changes that, and it is deliberately not a
// login. There are no accounts here and nobody to name — one key, one door.
// The page is handed it once in a link, keeps it in a cookie, and every
// request after that carries it without the player ever seeing it again. What
// it stops is the scanner; it does not pretend to stop somebody the player
// sent the link to.
//
// The key is only asked for when a run says to ask for it. A server started
// the ordinary way is exactly what it was before.

// accessCookie holds the key once a link has been followed. A phone that
// installs the page as a PWA keeps it, which is why the key outlives a
// restart: a link that stopped working every time the server was restarted
// would be a link nobody keeps.
const accessCookie = "wfeature_key"

// accessQuery is how a key arrives the first time, and the only way it is ever
// written down: `http://host:11541/?k=…` is the whole of what a player is
// given.
const accessQuery = "k"

// accessCookieMaxAge is a year in seconds. The cookie is the phone's copy of
// the link, so it should outlive anything the player would think of as a
// session.
const accessCookieMaxAge = 365 * 24 * 60 * 60

// AdminHeader carries the second key, the one that is never in a link: it
// belongs to whoever can read the run directory on the server's own machine,
// which is who is allowed to stop the server. See serveShutdown.
//
// It is exported because the launcher sends it and this is the side that
// defines it; internal/launcher spells it again rather than importing a
// package this size, and a test holds the two spellings together.
const AdminHeader = "X-Wfeature-Admin"

// admitted reports whether this request may go on to a route, and answers it
// itself when it may not.
//
// Two paths are outside the gate. `/api/status` is how anything tells this
// server from a stranger holding the port, including the launcher that is
// about to stop it, and it says only what build is running. `/api/shutdown`
// carries the admin key instead, which is a different secret for a different
// audience — the player holding the link is not the person entitled to end
// everybody's game.
func (s *Server) admitted(writer http.ResponseWriter, request *http.Request) bool {
	if s.accessKey == "" {
		return true
	}
	switch request.URL.Path {
	case "/api/status", "/api/shutdown":
		return true
	}

	if key := request.URL.Query().Get(accessQuery); key != "" {
		if !s.keyMatches(key) {
			s.refuse(writer, request)
			return false
		}
		http.SetCookie(writer, &http.Cookie{
			Name:     accessCookie,
			Value:    key,
			Path:     "/",
			MaxAge:   accessCookieMaxAge,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		// A key in the address bar is a key in the browser history, in a
		// screenshot of the tab, and in whatever the page is later shared as.
		// Now that the cookie holds it, the same page without it is the one
		// worth being on — but only for a plain page load: a WebSocket cannot
		// be redirected, and neither can a request that is not a GET.
		if isRedirectable(request) {
			http.Redirect(writer, request, withoutKey(request.URL), http.StatusSeeOther)
			return false
		}
		return true
	}

	if cookie, err := request.Cookie(accessCookie); err == nil && s.keyMatches(cookie.Value) {
		return true
	}

	s.refuse(writer, request)
	return false
}

// keyMatches compares in constant time. The comparison is not the weak point
// of a key this size, but a timing answer is free to give away and there is no
// reason to give it.
func (s *Server) keyMatches(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(s.accessKey)) == 1
}

// refuse answers a request that arrived without the key. It says as little as
// it can: whoever is asking either has the link or is scanning, and neither of
// them is helped by a description of what is behind the door.
func (s *Server) refuse(writer http.ResponseWriter, request *http.Request) {
	s.logger.Debug("refused a request with no key", "path", request.URL.Path, "from", request.RemoteAddr)
	writeError(writer, http.StatusForbidden, "Forbidden")
}

// isRedirectable reports whether the key can be taken out of the address by
// answering with a redirect. A WebSocket handshake is a GET and must not be:
// the client would follow it and arrive without an upgrade.
func isRedirectable(request *http.Request) bool {
	if request.Method != http.MethodGet {
		return false
	}
	return !strings.EqualFold(request.Header.Get("Upgrade"), "websocket")
}

// withoutKey is the same address with the key removed, which is where a page
// that has just been given one is sent.
func withoutKey(address *url.URL) string {
	stripped := *address
	query := stripped.Query()
	query.Del(accessQuery)
	stripped.RawQuery = query.Encode()
	// Only the path and query travel: the scheme and host a proxy or a tunnel
	// put in front of this server are not ours to name, and a relative
	// redirect keeps the browser where it already is.
	if stripped.Path == "" {
		stripped.Path = "/"
	}
	return stripped.RequestURI()
}

// adminAllowed reports whether a request carries the admin key. A server with
// no admin key set is one that was not started for the network, and the
// loopback rule in serveShutdown is the whole of its answer.
func (s *Server) adminAllowed(request *http.Request) bool {
	if s.adminKey == "" {
		return true
	}
	return subtle.ConstantTimeCompare(
		[]byte(request.Header.Get(AdminHeader)), []byte(s.adminKey)) == 1
}

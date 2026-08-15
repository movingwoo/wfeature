package webhost

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/movingwoo/wfeature/internal/backend"
)

// platforms are the save trees a request may name. The platform-less form
// means KTF, which is the contract the page used before the other platforms
// existed and which a stored URL still uses.
var platforms = map[string]bool{"ktf": true, "lgt": true, "skt": true}

// ownerRejected reports whether an owner directory name carries something that
// could steer a path. An owner is a name the Host chose — a PID for KTF and
// LGT, the MIDlet-Name for SKT — and those carry spaces and Korean,
// so the rule is what a name must not contain rather than a list of what it
// may. The Windows-illegal set is refused too, so one save tree copied between
// machines still resolves.
func ownerRejected(owner string) bool {
	if owner == "" || len(owner) > 64 || owner == "." || owner == ".." {
		return true
	}
	for _, character := range owner {
		switch {
		case character < 0x20, character == 0x7f:
			return true
		case strings.ContainsRune(`/\:*?"<>|`, character):
			return true
		// The bidirectional overrides and the zero-width joiners are
		// invisible in a file listing, which is exactly what makes a name
		// carrying them worth refusing.
		case character == 0x200e || character == 0x200f || character == 0x061c:
			return true
		case character >= 0x202a && character <= 0x202e:
			return true
		case character >= 0x2066 && character <= 0x2069:
			return true
		case character == 0xfeff:
			return true
		}
	}
	return false
}

// saveResponse is the shape the page reads: every entry of one owner, keyed by
// the same slash-separated names the DirectorySaveStore writes.
type saveResponse struct {
	Saves map[string]string `json:"saves"`
}

// serveSaves answers the save API, where <owner> is the game's PID for KTF and
// LGT and its MIDlet-Name for SKT:
//
//	GET /api/saves/<owner>                  -> { saves: { key: base64 } }
//	PUT /api/saves/<owner>/<key>            <- the raw body persists one entry
//	GET /api/saves/<platform>/<owner>       -> the same, on another platform
//	PUT /api/saves/<platform>/<owner>/<key>
func (s *Server) serveSaves(writer http.ResponseWriter, request *http.Request) {
	segments, err := pathComponents(strings.TrimPrefix(request.URL.Path, "/api/saves"))
	if err != nil || len(segments) == 0 {
		writeError(writer, http.StatusBadRequest, "Bad Request")
		return
	}

	root := s.saveRoot
	if len(segments) > 1 && platforms[segments[0]] {
		// A platform segment reroots the request; the tree keeps its profile
		// and swaps only the platform directory.
		root = filepath.Join(filepath.Dir(root), segments[0])
		segments = segments[1:]
	}
	owner := segments[0]
	if ownerRejected(owner) {
		writeError(writer, http.StatusBadRequest, "Bad Request")
		return
	}
	ownerRoot := filepath.Join(root, owner)

	switch {
	case request.Method == http.MethodGet && len(segments) == 1:
		s.listSaves(writer, ownerRoot)
	case request.Method == http.MethodPut && len(segments) > 1:
		s.storeSave(writer, request, ownerRoot, strings.Join(segments[1:], "/"))
	default:
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
	}
}

func (s *Server) listSaves(writer http.ResponseWriter, ownerRoot string) {
	saves := map[string]string{}
	// A game with no saves yet is the normal first run, so a missing
	// directory is an empty answer rather than a 404 the page has to handle.
	_ = filepath.WalkDir(ownerRoot, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(name)
		if err != nil {
			s.logger.Warn("save entry unreadable", "path", name, "error", err)
			return nil
		}
		relative, err := filepath.Rel(ownerRoot, name)
		if err != nil {
			return nil
		}
		saves[filepath.ToSlash(relative)] = base64.StdEncoding.EncodeToString(content)
		return nil
	})
	body, err := json.Marshal(saveResponse{Saves: saves})
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(writer, http.StatusOK, body)
}

func (s *Server) storeSave(writer http.ResponseWriter, request *http.Request, ownerRoot, rawKey string) {
	// Both Hosts normalize on the way in, so a browser session and the CLI
	// address the same entry: a guest that opens "./OptionSave" persists to
	// the same file either way.
	key, err := backend.NormalizeSaveKey(rawKey)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Bad Request")
		return
	}
	body, err := readBody(request, maxRequestBody)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Bad Request")
		return
	}
	if err := backend.NewDirectorySaveStore(ownerRoot).StoreSave(key, body); err != nil {
		s.logger.Error("save could not be written", "owner", filepath.Base(ownerRoot), "key", key, "error", err)
		writeError(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	securityHeaders(writer.Header())
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

// readBody reads a request body up to a limit and refuses anything longer,
// rather than truncating it into a save the game would later fail to read.
func readBody(request *http.Request, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(request.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, io.ErrShortWrite
	}
	return body, nil
}

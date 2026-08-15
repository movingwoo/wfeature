package webhost

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/movingwoo/wfeature/internal/backend"
)

// A page's console output is lost the moment the tab closes, and it is the half
// of a run the server cannot see: a dropped socket or a draw failure shows up
// nowhere else. The page posts its log here, beside the report the session
// writes itself.

// maxLabelRunes bounds the part of a log file name the caller chose. The name
// is generated either way; the label only decorates it.
const maxLabelRunes = 48

// DebugLogName builds the file name for a report. The label never becomes the
// name — it decorates a server-generated timestamp — so no separator, control
// character, or leading dot can steer the path, while the Korean titles the
// picker offers stay readable.
func DebugLogName(label string, now time.Time) string {
	stamp := now.UTC().Format("2006-01-02T15-04-05-") + fmt.Sprintf("%03d", now.UTC().Nanosecond()/int(time.Millisecond))
	cleaned := strings.Map(func(character rune) rune {
		switch {
		case unicode.IsControl(character), unicode.Is(unicode.Cf, character):
			return -1
		case strings.ContainsRune(`/\:*?"<>|`, character):
			return -1
		}
		return character
	}, label)
	cleaned = strings.TrimLeft(strings.TrimSpace(cleaned), ".")
	if runes := []rune(cleaned); len(runes) > maxLabelRunes {
		cleaned = string(runes[:maxLabelRunes])
	}
	if cleaned == "" {
		return stamp + ".log"
	}
	return stamp + "-" + cleaned + ".log"
}

// serveDebugLog stores one report under a server-chosen name and answers with
// where it landed, so the page can tell the user where to look.
func (s *Server) serveDebugLog(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	body, err := readBody(request, maxRequestBody)
	if err != nil || len(body) == 0 {
		writeError(writer, http.StatusBadRequest, "Bad Request")
		return
	}
	if err := os.MkdirAll(s.logRoot, 0o755); err != nil {
		s.logger.Error("log directory could not be created", "path", s.logRoot, "error", err)
		writeError(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	name := DebugLogName(request.URL.Query().Get("label"), time.Now())
	file := filepath.Join(s.logRoot, name)
	if err := os.WriteFile(file, body, 0o644); err != nil {
		s.logger.Error("report could not be written", "path", file, "error", err)
		writeError(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	s.logger.Info("stored a debug report", "path", file, "bytes", len(body))
	response, err := json.Marshal(struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}{Name: name, Path: file})
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

// GameRootIn, SaveRootIn and LogRootIn name the runtime directories under a
// data root. In a checkout that root is the ignored var/ tree, which is the
// same layout the native CLI uses so both Hosts of one profile boot from the
// same data; a released binary passes the directory it was dropped into.
func GameRootIn(root string) string { return filepath.Join(root, "games") }
func LogRootIn(root string) string  { return filepath.Join(root, "logs") }
func SaveRootIn(root string) string {
	return filepath.Join(root, "savedata", backend.BuildProfile(), "ktf")
}

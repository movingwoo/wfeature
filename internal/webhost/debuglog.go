package webhost

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

// What the reports may cost. The server binds every interface so a phone can
// reach it, and this route has no authentication, so anyone who can reach the
// page can post one: without a bound the directory is a way to fill the disk
// of the machine somebody left the server running on. The three bounds are one
// report's size, how many may arrive in a window, and what the directory keeps.
const (
	// maxDebugReport bounds one report. The page's log is a ring of 2000
	// lines and a session report is a few pages, so a report that reaches
	// this is not one of ours.
	maxDebugReport = 2 << 20

	// debugLogBudget is what the directory holds before the oldest reports
	// are dropped to make room.
	debugLogBudget = 64 << 20

	// debugLogLife is how long a report is kept. A report is read in the
	// hour after the run that produced it or not at all, and the ones still
	// wanted after two weeks have been copied somewhere by then.
	debugLogLife = 14 * 24 * time.Hour

	// debugLogBurst and debugLogWindow are the rate. Reports are posted by a
	// person pressing a button — the page sends one, and its session report
	// beside it — so ten a minute is many times what a run produces and
	// still bounds a script.
	debugLogBurst  = 10
	debugLogWindow = time.Minute
)

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
//
// **A release does not answer here at all.** Collecting a run report is the
// debug profile's business — the page a release serves has no button that
// posts one and drops the console capture that fills one — so in a release
// this is a route that could only ever be reached by something other than the
// page it serves. It answers the way an address nobody serves does.
func (s *Server) serveDebugLog(writer http.ResponseWriter, request *http.Request) {
	if !s.diagnostics {
		writeError(writer, http.StatusNotFound, "Not Found")
		return
	}
	if request.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	body, err := readBody(request, maxDebugReport)
	if err != nil || len(body) == 0 {
		writeError(writer, http.StatusBadRequest, "Bad Request")
		return
	}
	if !s.allowDebugReport(time.Now()) {
		s.logger.Warn("refused a debug report over the rate", "from", request.RemoteAddr)
		writeError(writer, http.StatusTooManyRequests, "Too Many Requests")
		return
	}
	name, file, err := s.storeReport(request.URL.Query().Get("label"), body, time.Now())
	if err != nil {
		s.logger.Error("report could not be written", "path", s.logRoot, "error", err)
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

// allowDebugReport reports whether one more report may be written now, and
// counts it when it may. The window is rolling rather than a bucket that
// refills on a clock, so a run that presses the button twice in a row is never
// refused for something a minute ago.
func (s *Server) allowDebugReport(now time.Time) bool {
	s.debugLogMu.Lock()
	defer s.debugLogMu.Unlock()
	kept := s.debugLogPosts[:0]
	for _, at := range s.debugLogPosts {
		if now.Sub(at) < debugLogWindow {
			kept = append(kept, at)
		}
	}
	s.debugLogPosts = kept
	if len(kept) >= debugLogBurst {
		return false
	}
	s.debugLogPosts = append(s.debugLogPosts, now)
	return true
}

// storeReport writes one report under a server-chosen name and prunes what the
// directory has accumulated. Both halves of a run come through here — the
// session report the server composes itself and the page log posted to the
// API — so the directory is bounded whichever side wrote last.
func (s *Server) storeReport(label string, body []byte, now time.Time) (string, string, error) {
	if err := os.MkdirAll(s.logRoot, 0o755); err != nil {
		return "", "", err
	}
	name := DebugLogName(label, now)
	file := filepath.Join(s.logRoot, name)
	if err := os.WriteFile(file, body, 0o644); err != nil {
		return "", "", err
	}
	if removed := pruneReports(s.logRoot, now); removed > 0 {
		s.logger.Debug("dropped older run reports", "path", s.logRoot, "files", removed)
	}
	return name, file, nil
}

// pruneReports drops the reports that are past debugLogLife and then, while
// the directory is still over debugLogBudget, the oldest of what is left. It
// answers how many it removed.
//
// Only the files this server names are considered: the directory is under the
// user's var/ tree and whatever else is sitting in it is theirs. The newest
// report is never dropped — it is the one that was just written, and a budget
// smaller than a single report would otherwise delete the run somebody is
// looking at.
func pruneReports(root string, now time.Time) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	type report struct {
		name string
		size int64
	}
	reports := make([]report, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := reportTime(entry.Name()); !ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		reports = append(reports, report{name: entry.Name(), size: info.Size()})
		total += info.Size()
	}
	// The generated name opens with a UTC timestamp, so ordering by name is
	// ordering by age. The file's modification time is not used: it is the
	// user's to change, and a copied tree carries whatever the copy left.
	sort.Slice(reports, func(first, second int) bool { return reports[first].name < reports[second].name })
	removed := 0
	for index, entry := range reports {
		if index == len(reports)-1 {
			break
		}
		at, _ := reportTime(entry.name)
		if now.Sub(at) <= debugLogLife && total <= debugLogBudget {
			// Everything after this one is newer and the directory is inside
			// its budget, so there is nothing further to drop.
			break
		}
		if err := os.Remove(filepath.Join(root, entry.name)); err != nil {
			continue
		}
		total -= entry.size
		removed++
	}
	return removed
}

// reportTime reads the timestamp DebugLogName opens a report with, and reports
// whether the name is one of this server's at all.
func reportTime(name string) (time.Time, bool) {
	const layout = "2006-01-02T15-04-05"
	if !strings.HasSuffix(name, ".log") || len(name) < len(layout) {
		return time.Time{}, false
	}
	stamp, err := time.ParseInLocation(layout, name[:len(layout)], time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return stamp, true
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

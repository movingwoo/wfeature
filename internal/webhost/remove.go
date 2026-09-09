package webhost

import (
	"net/http"
	"os"
)

// Taking a game back off.
//
// A game could be added from the page and never removed from it, which is only
// untidy on a desktop — the file is in a folder, and a folder can be opened.
// On a phone it is a dead end: the archive lands in a directory Android has
// not let a file manager open since 11, so a game added by mistake, or a
// finished one, stays there for as long as the app is installed, and the only
// way to clear it out is to clear the app's data and lose every save with it.
//
// So the route that brought the archive in takes it out again. What it may
// reach is the whole of the design: the page deletes out of the added root and
// nowhere else, and it is `gameFile` that decides which root a path names.
// **The library beside the server is not the page's to delete** — those files
// are somebody's own, put there by hand and often by the hundred, and a button
// that could reach them would be one misclick away from a loss the page cannot
// undo. What the page wrote, the page may remove.
//
// A game that is playing while its archive is deleted keeps playing. The
// archive is read once, at the start, and a parked session holds the game
// rather than a path — so the file going away costs the running game nothing
// and the picker simply stops offering it. That is why there is no arbitration
// here: there is nothing for a claim to protect.
//
// The save tree is not touched. Saves are keyed by what is inside the archive
// rather than by where it sits (`internal/session`, `backend.SaveIdentity`),
// so adding the same file again lands on the same progress — which makes a
// removal something a player can change their mind about, and makes deleting
// their progress alongside the file a decision worth not taking on their
// behalf.

// removeGameQuery names the game to remove. It is the same string `games.json`
// handed the page, the way the save routes take it, rather than a bare file
// name: one spelling of "which game" across the API is one thing to get right.
const removeGameQuery = "game"

// serveGameRemoval deletes one archive the page added.
func (s *Server) serveGameRemoval(writer http.ResponseWriter, request *http.Request) {
	file, added, err := s.gameFileInQuery(request.URL.Query().Get(removeGameQuery))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "지울 게임을 찾지 못했습니다.")
		return
	}
	if !added {
		// The picker greys the button out for these, so arriving here is a
		// stale list or a hand-made request rather than a misclick — but it is
		// the server that decides, because a page is not a place to keep a
		// rule that protects files.
		s.logger.Warn("refused to remove a game outside the added root", "path", file)
		writeError(writer, http.StatusForbidden,
			"이 게임은 서버 폴더에 직접 넣은 것이라 여기서 지울 수 없습니다.")
		return
	}
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		// Already gone, or never there. The page reloads its list either way,
		// and that list is what was out of date.
		writeError(writer, http.StatusNotFound, "그 게임이 이미 없습니다.")
		return
	}
	if err := os.Remove(file); err != nil {
		s.logger.Error("could not remove a game", "path", file, "error", err)
		writeError(writer, http.StatusInternalServerError, "게임을 지우지 못했습니다.")
		return
	}
	s.logger.Info("a game was removed from the page", "path", file, "bytes", info.Size())
	writeJSON(writer, http.StatusOK, []byte(`{"removed":true}`))
}

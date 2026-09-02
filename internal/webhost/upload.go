package webhost

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Adding a game from the page.
//
// Until now a game arrived one way: the user put a file in a folder beside the
// server. That works on a desktop and is the whole story there — but the same
// binary is meant to run on a phone, and on Android since 11 a file manager
// cannot open `/Android/data/<package>/`, so "put the file in this folder" is
// an instruction nobody can carry out. Without a way in from the page, a
// phone build would have no games at all.
//
// So the archive travels the way everything else does: over the socket the
// page already has. The desktop gets the same button, which is a smaller win
// but a real one — the folder is one less thing to find.
//
// **What arrives is not trusted.** The name is checked here rather than
// sanitised, because a name that needs repairing is a name worth refusing, and
// what a bad one buys is a write outside the game root. The bytes are not
// examined at all: which platform an archive belongs to is the engine's answer
// from its content, and a file this route accepted still has to survive being
// loaded before it is a game.

// maxGameUpload bounds one archive. The largest of this era measures a few
// megabytes — the biggest in the local library is under four — so this is
// generous rather than tight, and it exists to keep a phone from filling its
// own storage through a mistyped request.
const maxGameUpload = 32 << 20

// uploadNameQuery carries the file's name. It is a query parameter rather than
// a header because these names are Korean: a header value is Latin-1 and would
// have to be escaped by hand on both sides, while a query parameter is
// percent-encoded by every client that has ever made a URL.
const uploadNameQuery = "name"

// serveGameUpload writes one archive into the game root.
func (s *Server) serveGameUpload(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	name, err := uploadName(request.URL.Query().Get(uploadNameQuery))
	if err != nil {
		s.logger.Warn("refused a game upload", "reason", err)
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	body, err := readBody(request, maxGameUpload)
	if err != nil {
		// A file too large is the one failure worth naming, since the fix is
		// the user's: everything else here is a broken request.
		if errors.Is(err, errBodyTooLarge) {
			writeError(writer, http.StatusRequestEntityTooLarge, "게임 파일이 너무 큽니다.")
			return
		}
		writeError(writer, http.StatusBadRequest, "Bad Request")
		return
	}
	if len(body) == 0 {
		writeError(writer, http.StatusBadRequest, "빈 파일입니다.")
		return
	}

	// The archive lands in the root rather than in a platform directory,
	// because which platform it belongs to is read from its bytes when it is
	// loaded and this route has not read them. The picker already has a place
	// for an archive with no group.
	if err := os.MkdirAll(s.gameRoot, 0o755); err != nil {
		s.logger.Error("could not make the game root", "path", s.gameRoot, "error", err)
		writeError(writer, http.StatusInternalServerError, "게임을 저장하지 못했습니다.")
		return
	}
	if err := writeFileAtomically(filepath.Join(s.gameRoot, name), body); err != nil {
		s.logger.Error("could not write an uploaded game", "name", name, "error", err)
		writeError(writer, http.StatusInternalServerError, "게임을 저장하지 못했습니다.")
		return
	}
	s.logger.Info("a game was added from the page", "name", name, "bytes", len(body))
	writeJSON(writer, http.StatusOK, []byte(`{"added":true}`))
}

// uploadName checks the name an upload carries and answers with the one to
// write, or with what is wrong with it in the words the page will show.
//
// It refuses rather than repairs. A name with a separator in it, or one that
// climbs, is not a mistake to be trimmed into something safe — it is a request
// to write somewhere else, and the only useful answer is no.
func uploadName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("파일 이름이 없습니다.")
	}
	if strings.ContainsAny(name, "/\\\x00") || name == "." || name == ".." {
		return "", errors.New("파일 이름을 쓸 수 없습니다.")
	}
	// A leading dot hides the file from every listing this project does, which
	// makes an upload that appears to work and then shows no game.
	if strings.HasPrefix(name, ".") {
		return "", errors.New("파일 이름을 쓸 수 없습니다.")
	}
	if !gameExtensions[strings.ToLower(filepath.Ext(name))] {
		return "", fmt.Errorf("%s 파일은 게임이 아닙니다. zip 또는 jar 파일을 골라주세요.", filepath.Ext(name))
	}
	return name, nil
}

// writeFileAtomically writes beside the target and renames over it, so that an
// upload that fails halfway leaves the previous archive rather than a
// truncated one. A game the picker lists but the loader cannot open is a
// worse state than no game.
func writeFileAtomically(path string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(name)
	}()
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	// A temporary file is created 0600, and an archive the user may want to
	// copy back out should not inherit that: this is their file, in their
	// games folder, and it reads like every other one there.
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

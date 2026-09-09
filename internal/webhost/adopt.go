package webhost

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// The one move an upgrade has to make.
//
// Adding a game from the page used to write into the game root, and a removal
// only ever reaches the added root — so every game added before there was an
// added root would be one the page can never take back. On a desktop that is
// an annoyance with an obvious fix, and on a phone it is the whole complaint:
// that directory is one Android has not let anything else open since 11, so
// the only other way out is clearing the app's data and losing every save.
//
// So on the first start after the upgrade, the archives sitting loose in the
// game root move across.
//
// **Loose only.** A group directory is a library somebody assembled — named by
// them, filed by them — and moving that would be answering a question nobody
// asked. What sits directly in the root is where an upload landed, and on a
// phone an upload is the only thing that could have put it there.
//
// **Once.** A person may still drop an archive straight into the game root by
// hand; the README has said so for as long as there has been a README, and a
// sweep that ran every start would keep picking those up and filing them
// somewhere the owner did not choose. The marker below is what makes this an
// upgrade step rather than a standing rule.

// adoptedMarker records that the move has run for this installation. It lives
// in the added root because that is the directory this is about: a root that
// was cleared out is one whose history is gone with it, and running the move
// again there costs nothing.
const adoptedMarker = ".adopted"

// AdoptLooseGames moves the archives lying directly in the game root into the
// added root, once, and reports how many moved. A host calls it at startup,
// before it serves anything.
//
// Nothing here is fatal. A move that fails leaves the archive where it is —
// still listed, still playable, only not removable from the page — and leaves
// the marker unwritten so the next start tries again.
func AdoptLooseGames(gameRoot, addedRoot string, logger *slog.Logger) int {
	if gameRoot == "" || addedRoot == "" || gameRoot == addedRoot {
		return 0
	}
	if _, err := os.Stat(filepath.Join(addedRoot, adoptedMarker)); err == nil {
		return 0
	}
	// A game root that is not there yet is a fresh install: nothing to move,
	// and the marker still gets written, because an installation that has been
	// through this step has been through it whether or not it had anything in
	// it. Leaving it unwritten would turn the step into a standing rule for
	// every install whose first start came before its first game.
	entries, err := os.ReadDir(gameRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		if logger != nil {
			logger.Warn("could not read the game root to adopt what is loose in it",
				"path", gameRoot, "error", err)
		}
		return 0
	}

	moved, failed := 0, 0
	for _, entry := range entries {
		if entry.IsDir() || !gameExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			continue
		}
		if err := os.MkdirAll(addedRoot, 0o755); err != nil {
			if logger != nil {
				logger.Warn("could not make the added root", "path", addedRoot, "error", err)
			}
			return moved
		}
		target := filepath.Join(addedRoot, entry.Name())
		if _, err := os.Stat(target); err == nil {
			// Two files, one name, and no way to tell from here whether they
			// are the same game. Neither is worth losing.
			if logger != nil {
				logger.Info("left a game in the game root: the added root has that name",
					"name", entry.Name())
			}
			failed++
			continue
		}
		if err := os.Rename(filepath.Join(gameRoot, entry.Name()), target); err != nil {
			if logger != nil {
				logger.Warn("could not move a game into the added root",
					"name", entry.Name(), "error", err)
			}
			failed++
			continue
		}
		moved++
	}
	if failed > 0 {
		// Leaving the marker unwritten is what makes the next start finish the
		// job — a directory that was read-only for a moment, or a name that
		// was taken and has since been freed.
		return moved
	}
	if err := os.MkdirAll(addedRoot, 0o755); err != nil {
		return moved
	}
	if err := os.WriteFile(filepath.Join(addedRoot, adoptedMarker), nil, 0o644); err != nil && logger != nil {
		logger.Warn("could not record that the move has run", "path", addedRoot, "error", err)
	}
	if moved > 0 && logger != nil {
		logger.Info("moved games added before there was an added root",
			"count", moved, "from", gameRoot, "to", addedRoot)
	}
	return moved
}

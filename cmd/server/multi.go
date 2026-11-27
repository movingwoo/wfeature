package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// A new database may not exist yet. Resolve its existing ancestors as well so
// a configured library or development shell cannot serve it through an alias.
func resolvedLocation(name string) (string, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return resolved, err
	}
	parentDir := filepath.Dir(absolute)
	if parentDir == absolute {
		return "", err
	}
	parent, err := resolvedLocation(parentDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}

func accountDatabasePath(root string, served ...string) (string, error) {
	database, err := resolvedLocation(filepath.Join(root, "accounts.sqlite"))
	if err != nil {
		return "", err
	}
	for _, name := range served {
		if name == "" {
			continue
		}
		dir, err := resolvedLocation(name)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(dir, database)
		if err == nil && filepath.IsLocal(rel) || containsDatabase(dir, database) {
			return "", fmt.Errorf("account store must be outside the served directory %s", name)
		}
	}
	return database, nil
}

// Some volumes preserve the spelling of a path but ignore its case. Compare
// existing directory identities too before exposing a configurable file tree.
func containsDatabase(dir, database string) bool {
	served, err := os.Stat(dir)
	if err != nil {
		return false
	}
	for ancestor := filepath.Dir(database); ; ancestor = filepath.Dir(ancestor) {
		info, err := os.Stat(ancestor)
		if err == nil && os.SameFile(served, info) {
			return true
		}
		if filepath.Dir(ancestor) == ancestor {
			return false
		}
	}
}

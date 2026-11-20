package skt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Ordinary saves own file contents. Only handle identity, mode and cursor
// return with execution state.
type wideFileCheckpoint struct {
	Name     string
	Position int
	ReadOnly bool
}

func (s *ScriptSession) captureWideFiles() [15]wideFileCheckpoint {
	var saved [15]wideFileCheckpoint
	for i, h := range s.wideFiles {
		if h.data != nil {
			saved[i] = wideFileCheckpoint{Name: h.name, Position: h.position, ReadOnly: h.readOnly}
		}
	}
	return saved
}

func validateWideFiles(saved [15]wideFileCheckpoint) error {
	for i, h := range saved {
		if h == (wideFileCheckpoint{}) {
			continue
		}
		if len(h.Name) > 512-len("files/") || h.Position < 0 || h.Position > wideFileLimit {
			return fmt.Errorf("SGS checkpoint file handle %d has an invalid name or position", i)
		}
		if name, valid := normalizeWideFileName(h.Name); !valid || name != h.Name {
			return fmt.Errorf("SGS checkpoint file handle %d has an invalid name", i)
		}
	}
	return nil
}

func (s *ScriptSession) restoreWideFiles(saved [15]wideFileCheckpoint, live backend.SaveStore) error {
	if err := validateWideFiles(saved); err != nil {
		return err
	}
	if live == nil {
		return fmt.Errorf("SGS checkpoint requires a live save store")
	}
	var files [15]wideFileHandle
	byName := make(map[string]*[]byte)
	for i, h := range saved {
		if h.Name == "" {
			continue
		}
		content := byName[h.Name]
		if content == nil {
			raw, present, err := backend.ReadSaveLimit(live, "files/"+h.Name, wideFileLimit+8)
			if err != nil {
				return err
			}
			data, _, err := s.decodeWideFile(h.Name, raw, present)
			if err != nil {
				return err
			}
			content = &data
			byName[h.Name] = content
		}
		files[i] = wideFileHandle{name: h.Name, position: h.Position, readOnly: h.ReadOnly, data: content}
	}
	s.wideFiles = files
	return nil
}

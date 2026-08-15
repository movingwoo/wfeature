package smaf

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Real games are the only source of SMAF files this project can check itself
// against: the format has three sequence dialects and the fixtures below would
// otherwise only ever exercise the one they were written in. The archives are
// ignored by git, so this is opt-in like the KTF acceptance probes.
//
//	WFEATURE_SMAF_ACCEPTANCE=1 go test ./internal/audio/smaf
func TestLocalArchiveSMAFFilesPlay(t *testing.T) {
	if os.Getenv("WFEATURE_SMAF_ACCEPTANCE") != "1" {
		t.Skip("set WFEATURE_SMAF_ACCEPTANCE=1 to play SMAF files out of local game archives")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate SMAF acceptance test source")
	}
	gameDirectory := filepath.Join(filepath.Dir(source), "..", "..", "..", "var", "games", "ktf")
	entries, err := os.ReadDir(gameDirectory)
	if err != nil {
		t.Fatalf("read local KTF game directory: %v", err)
	}

	totalFiles, totalEvents, totalNotes, totalWaves := 0, 0, 0, 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			continue
		}
		archive, err := os.ReadFile(filepath.Join(gameDirectory, entry.Name()))
		if err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
			continue
		}
		files, events, notes, waves := 0, 0, 0, 0
		// Most games package their sounds inside the JAR rather than beside
		// it, so the walk has to descend into nested archives to see them at
		// all — only one local family keeps them loose.
		walkArchive(archive, entry.Name(), func(name string, data []byte) {
			if len(data) < 4 || string(data[:4]) != "MMMD" {
				return
			}
			files++
			if _, err := Parse(data); err != nil {
				t.Errorf("%s: parse: %v", name, err)
				return
			}
			played := Play(data)
			if len(played) == 0 {
				t.Errorf("%s: parsed but produced no events", name)
				return
			}
			events += len(played)
			for _, event := range played {
				switch event.Type {
				case EventNoteOn:
					notes++
				case EventWave:
					waves++
				}
			}
			assertPlayableEvents(t, name, played)
		})
		if files > 0 {
			t.Logf("%s: %d SMAF files, %d events, %d notes, %d waves", entry.Name(), files, events, notes, waves)
		}
		totalFiles, totalEvents, totalNotes, totalWaves = totalFiles+files, totalEvents+events, totalNotes+notes, totalWaves+waves
	}
	if totalFiles == 0 {
		t.Skip("no SMAF files in the local archives")
	}
	t.Logf("played %d SMAF files: %d events, %d notes, %d waves", totalFiles, totalEvents, totalNotes, totalWaves)
	if totalNotes == 0 && totalWaves == 0 {
		t.Error("no local SMAF file produced a single note or wave")
	}
}

// assertPlayableEvents checks what a MIDI device would reject: a channel
// outside 0-15, a seven-bit field with its top bit set, a bend past 14 bits,
// or events that do not run forward in time.
func assertPlayableEvents(t *testing.T, name string, events []Event) {
	t.Helper()
	previous := uint32(0)
	for index, event := range events {
		if event.Time < previous {
			t.Errorf("%s: event %d goes back in time (%d after %d)", name, index, event.Time, previous)
			return
		}
		previous = event.Time
		if event.Channel > 15 {
			t.Errorf("%s: event %d is on MIDI channel %d", name, index, event.Channel)
			return
		}
		if event.Note > 127 || event.Velocity > 127 || event.Program > 127 || event.Control > 127 || event.Value > 127 {
			t.Errorf("%s: event %d has a value past seven bits: %+v", name, index, event)
			return
		}
		if event.Bend > 0x3fff {
			t.Errorf("%s: event %d bends to %d", name, index, event.Bend)
			return
		}
		if event.Type == EventWave && (event.SamplingRate == 0 || len(event.Wave) == 0) {
			t.Errorf("%s: event %d is an empty wave", name, index)
			return
		}
	}
}

// walkArchive visits every member of a zip, descending one level into members
// that are themselves zips (a KTF archive holds the game's JAR).
func walkArchive(archive []byte, prefix string, visit func(name string, data []byte)) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return
	}
	for _, member := range reader.File {
		data, err := readZipMember(member)
		if err != nil {
			continue
		}
		name := prefix + "/" + member.Name
		if len(data) >= 4 && string(data[:4]) == "PK\x03\x04" {
			walkArchive(data, name, visit)
			continue
		}
		visit(name, data)
	}
}

func readZipMember(member *zip.File) ([]byte, error) {
	file, err := member.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(file); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

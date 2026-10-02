package backend

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode"
)

// A Host picks its sentence with errors.Is, and the platform that refuses
// wraps the sentinel around its own reason and the store's error. So each one
// has to be found under that wrapping, and none may answer for another: an
// earlier-format slot reported as an unsupported variant, or a failed write
// reported as a failed read, puts the wrong sentence on the page.
func TestCheckpointSaveRefusalsStayApartUnderWrapping(t *testing.T) {
	added := []error{ErrCheckpointSaveWrite, ErrCheckpointSaveRead, ErrCheckpointLegacy}
	every := append([]error{ErrNotCheckpoint, ErrCheckpointVersion, ErrCheckpointDamaged, ErrCheckpointIdentity}, added...)

	texts := make(map[string]bool, len(every))
	for _, sentinel := range every {
		if texts[sentinel.Error()] {
			t.Errorf("two refusals read %q", sentinel)
		}
		texts[sentinel.Error()] = true
	}

	for _, sentinel := range added {
		text := sentinel.Error()
		// The CLI prints these as they are, and everything a shell shows is
		// English.
		if !strings.HasPrefix(text, "backend: ") || strings.IndexFunc(text, func(r rune) bool { return r > unicode.MaxASCII }) >= 0 {
			t.Errorf("%q does not read like the other backend refusals", text)
		}
		wrapped := fmt.Errorf("fs/slot.dat: %w: %w", sentinel, os.ErrPermission)
		if !errors.Is(wrapped, sentinel) {
			t.Errorf("%q is lost under a wrapper", text)
		}
		if !errors.Is(wrapped, os.ErrPermission) {
			t.Errorf("%q hides the store's own error", text)
		}
		for _, other := range every {
			if other != sentinel && errors.Is(wrapped, other) {
				t.Errorf("%q also answers for %q", text, other)
			}
		}
	}
}

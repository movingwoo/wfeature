package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIAuthenticationDefaultsAndDiagnosticOptOut(t *testing.T) {
	fixture := filepath.Join("..", "..", "internal", "platform", "skt", "testdata", "license.jar")
	for _, test := range []struct {
		name    string
		flags   []string
		applied bool
	}{
		{"automatic", nil, true}, {"diagnostic opt-out", []string{"-no-auth"}, false}, {"legacy flag", []string{"-auth"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, log bytes.Buffer
			args := append([]string{"runskt", fixture, "-ticks", "1", "-save", t.TempDir()}, test.flags...)
			if result := run(args, &out, &log); result != 0 {
				t.Fatalf("run=%d: %s", result, log.String())
			}
			if got := strings.Contains(log.String(), "authentication: skt-license"); got != test.applied {
				t.Fatalf("applied=%v, want %v: %s", got, test.applied, log.String())
			}
		})
	}
}

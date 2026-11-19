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
		name, number string
		flags        []string
		status       string
	}{
		{"configured handset", "01000000000", nil, "guest-managed"},
		{"automatic", "", nil, "skt-license"},
		{"diagnostic opt-out", "", []string{"-no-auth"}, ""},
		{"legacy flag", "", []string{"-auth"}, "skt-license"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("WFEATURE_PHONE_NUMBER", test.number)
			var out, log bytes.Buffer
			args := append([]string{"runskt", fixture, "-ticks", "1", "-save", t.TempDir()}, test.flags...)
			if result := run(args, &out, &log); result != 0 {
				t.Fatalf("run=%d: %s", result, log.String())
			}
			var got string
			for line := range strings.SplitSeq(log.String(), "\n") {
				if status, found := strings.CutPrefix(line, "authentication: "); found {
					got = status
					break
				}
			}
			if got != test.status {
				t.Fatalf("authentication status = %q, want %q", got, test.status)
			}
		})
	}
}

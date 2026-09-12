package skt

import (
	"context"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
	"golang.org/x/text/encoding/korean"
)

func TestScriptExternalLaunchHandsValidatedURLToHostAndYields(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte("https://example.invalid/한글?q=1"))
	if err != nil {
		t.Fatal(err)
	}
	s.vm.Resources = []sgsvm.Resource{{Data: append(encoded, 0)}}
	for index := range s.timers {
		s.timers[index].active = true
	}
	var destinations []string
	s.options.ExternalLaunch = func(destination string) { destinations = append(destinations, destination) }

	s.vm.Push(1234)
	s.vm.Push(0)
	if err := s.Call(0xc4, s.vm); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Pop(); got != 1234 {
		t.Fatalf("external launch changed caller stack: got %d", got)
	}
	if len(destinations) != 1 || destinations[0] != "https://example.invalid/한글?q=1" {
		t.Fatalf("destinations = %q", destinations)
	}
	for index, timer := range s.timers {
		if !timer.active {
			t.Fatalf("external launch stopped timer %d", index)
		}
	}

	entry := len(s.vm.Program.Data)
	s.vm.Program.Data = append(s.vm.Program.Data, 5, 0, 0xc4, 5, 9, 0x0a, 16, 0xff)
	s.vm.Program.Entries[6] = uint16(entry)
	if err := s.event(context.Background(), 6, 7); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Value(16, 0); got != 0 {
		t.Fatalf("external launch continued yielded callback: scratch = %d", got)
	}
	if len(destinations) != 2 {
		t.Fatalf("host calls = %d, want 2", len(destinations))
	}
}

func TestScriptExternalLaunchRejectsUnsafeDestinationAtomically(t *testing.T) {
	invalidEUC := []byte{0xff, 0}
	cases := []struct {
		name string
		data []byte
	}{
		{"relative", []byte("/download\x00")},
		{"script scheme", []byte("javascript:alert(1)\x00")},
		{"file scheme", []byte("file:///tmp/item\x00")},
		{"missing host", []byte("https:///download\x00")},
		{"credentials", []byte("https://user:pass@example.invalid/item\x00")},
		{"invalid EUC-KR", invalidEUC},
		{"over limit", append([]byte("https://example.invalid/"+strings.Repeat("a", scriptExternalURLMaxBytes)), 0)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			s := newScriptTest(t, []byte{0xff}, nil)
			s.vm.Resources = []sgsvm.Resource{{Data: test.data}}
			called := false
			s.options.ExternalLaunch = func(string) { called = true }
			s.vm.Push(44)
			s.vm.Push(0)
			if err := s.Call(0xc4, s.vm); err == nil {
				t.Fatal("invalid destination accepted")
			}
			if called {
				t.Fatal("invalid destination reached Host")
			}
			if got := s.vm.Pop(); got != 44 {
				t.Fatalf("invalid destination changed caller stack: got %d", got)
			}
		})
	}
}

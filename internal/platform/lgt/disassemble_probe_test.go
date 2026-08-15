package lgt

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestLocalDisassembleProbe dumps guest memory ranges as hex so they can be run
// through a disassembler, the LGT companion to the KTF probe of the same name.
// A platform-call trace names the address a slot was called from, and what the
// call meant is in the instructions around it — a trace reports registers, and
// only the call site says which of them were arguments.
//
// It skips unless an archive and a range list are supplied:
//
//	WFEATURE_DISASSEMBLE_ARCHIVE=/abs/path/game.zip \
//	WFEATURE_DISASSEMBLE_RANGES=0x4b340-0x4b3a0 \
//	go test ./internal/platform/lgt -run TestLocalDisassembleProbe -v
func TestLocalDisassembleProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_DISASSEMBLE_ARCHIVE")
	ranges := os.Getenv("WFEATURE_DISASSEMBLE_RANGES")
	if path == "" || ranges == "" {
		t.Skip("set WFEATURE_DISASSEMBLE_ARCHIVE and WFEATURE_DISASSEMBLE_RANGES")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := StartSession(context.Background(), data, SessionOptions{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Close(context.Background())
	for _, spec := range strings.Split(ranges, ",") {
		low, high, ok := strings.Cut(spec, "-")
		if !ok {
			t.Fatalf("range %q: want low-high", spec)
		}
		start, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(low), "0x"), 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		end, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(high), "0x"), 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, end-start+16)
		if err := session.client.core.Memory().Read(uint32(start), buffer); err != nil {
			t.Fatalf("read %s: %v", spec, err)
		}
		fmt.Printf("%#x %s\n", start, hex.EncodeToString(buffer))
	}
}

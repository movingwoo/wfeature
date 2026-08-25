package ktf

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// TestLocalDisassembleProbe dumps guest code ranges as hex so they can be run
// through a disassembler. It is the companion to the guest profiler: that
// names the address ranges a game spends its instructions in, and a pure WIPI
// C game has no AOT method bodies for those ranges to resolve against, so the
// only way to find out what a hot loop does is to read it.
//
// Like TestLocalSymbolizeProbe it is a throwaway investigation aid and skips
// unless an archive and a range list are supplied:
//
//	WFEATURE_DISASSEMBLE_ARCHIVE=/abs/path/game.zip \
//	WFEATURE_DISASSEMBLE_RANGES=0x107c40-0x107c8a,0x1028b8-0x102aa8 \
//	go test ./internal/platform/ktf -run TestLocalDisassembleProbe -v
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
	// A failed start is the usual reason to be reading code at all: the title
	// died somewhere and the address in the report is what has to be read.
	// A start that fails returns no session, so the client is loaded directly
	// and the start is only attempted for what it maps beyond the image — the
	// failure is logged and the dump goes ahead either way.
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	client, err := LoadClient(archive.JAR.Client, armcore.CoreOptions{MaxSteps: sessionDefaultMaxSteps})
	if err != nil {
		t.Fatal(err)
	}
	session, startErr := StartSession(context.Background(), data, SessionOptions{})
	if startErr != nil {
		t.Logf("start: %v", startErr)
	}
	if session != nil {
		defer session.Close()
		client = session.Client
	}
	for _, spec := range strings.Split(ranges, ",") {
		low, high, err := parseProbeRange(spec)
		if err != nil {
			t.Fatalf("range %q: %v", spec, err)
		}
		// The tail covers the instruction the profile's last sample started
		// on, plus enough of what follows to show where the loop returns to.
		buffer := make([]byte, high-low+16)
		if err := client.core.Memory().Read(low, buffer); err != nil {
			t.Fatalf("read %s: %v", spec, err)
		}
		fmt.Printf("%#x %s\n", low, hex.EncodeToString(buffer))
	}
}

func parseProbeRange(spec string) (uint32, uint32, error) {
	low, high, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, fmt.Errorf("want low-high")
	}
	start, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(low), "0x"), 16, 32)
	if err != nil {
		return 0, 0, err
	}
	end, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(high), "0x"), 16, 32)
	if err != nil {
		return 0, 0, err
	}
	if end < start {
		return 0, 0, fmt.Errorf("high %#x is below low %#x", end, start)
	}
	return uint32(start), uint32(end), nil
}

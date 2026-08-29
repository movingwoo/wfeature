package ktf

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// TestLocalBreakpointProbe stops at guest addresses and prints the registers
// and the words they point at. It is a throwaway investigation aid, the
// companion to the disassemble probe: that says what an instruction does, and
// this says what it was holding when it did it. It skips unless an archive is
// supplied.
//
//	WFEATURE_BREAKPOINT_ARCHIVE=/abs/path/game.zip \
//	WFEATURE_BREAKPOINTS=0x105b12,0x105b18 \
//	WFEATURE_BREAKPOINT_LIMIT=6 \
//	WFEATURE_BREAKPOINT_WATCH=0x179198 \
//	WFEATURE_BREAKPOINT_CLASSES=fm,GamePlay \
//	WFEATURE_BREAKPOINT_DUMP=0x30001450-0x30001530 \
//	WFEATURE_BREAKPOINT_SAVE=/abs/path/savedata/debug/ktf \
//	go test ./internal/platform/ktf -run TestLocalBreakpointProbe -v
//
// The class dump is what turns an AOT native's address into the name the
// archive gave it, and the watch list names every writer of an address.
//
// **Everything is armed before the first guest instruction**, through the
// session's `Debug` hook, so the client's own initialization and `startApp` are
// inside the horizon — which is where a title that never reaches a session
// fails. A start that fails is not the end of the run either: the breakpoint
// and watch output is printed for it as well, because that is the case this is
// most often reached for.
func TestLocalBreakpointProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_BREAKPOINT_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_BREAKPOINT_ARCHIVE")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hits := 0
	limit := 40
	if value := os.Getenv("WFEATURE_BREAKPOINT_LIMIT"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	var core *armcore.Core
	var client *Client
	// A title whose first run ends itself is a different program on its
	// second, so the save tree is part of what is being probed rather than a
	// detail of the Host.
	// The probe runs on a manual clock and jumps to each next deadline, the
	// same pacing `runktf` uses without -play. It is not only faster: a title
	// that seeds itself from the clock takes a different branch on every
	// wall-clock run, and a fault reached once in three runs cannot be read.
	clock := NewManualClock(time.Time{})
	options := SessionOptions{
		TraceLimit: 4096,
		SaveRoot:   os.Getenv("WFEATURE_BREAKPOINT_SAVE"),
		Clock:      clock,
	}
	options.Debug = func(attached *armcore.Core) {
		core = attached
		armProbe(t, core, &hits, limit, &client)
	}
	session, err := StartSession(context.Background(), data, options)
	if err != nil {
		t.Logf("start: %v", err)
	}
	if session != nil {
		client = session.Client
		defer session.Close()
	}
	if core == nil {
		t.Fatalf("the core was never handed over")
	}
	defer func() {
		for _, hit := range core.WatchHits() {
			fmt.Printf("watch %#x written from %#x (%s) %d times, last %#x, stores %d..%d\n",
				hit.Address, hit.PC, hit.Origin, hit.Count, hit.Value, hit.First, hit.Last)
		}
	}()
	if session == nil {
		return
	}
	dumpProbeState(t, session)
	for round := 0; round < 400; round++ {
		session.SkipToNextDeadline()
		if _, err := session.Tick(context.Background()); err != nil {
			t.Logf("tick %d: %v", round, err)
			break
		}
	}
	t.Logf("breakpoint hits: %d", hits)
}

// armProbe sets the breakpoints, the watches and the debugger the environment
// asked for, on a core no guest instruction has run on yet.
func armProbe(t *testing.T, core *armcore.Core, hits *int, limit int, client **Client) {
	list := os.Getenv("WFEATURE_BREAKPOINTS")
	core.AttachDebugger(func(ctx context.Context, core *armcore.Core, thread *armcore.Thread, stop armcore.DebugStop) error {
		if stop != armcore.DebugStopBreakpoint {
			return nil
		}
		*hits++
		if *hits > limit {
			return nil
		}
		pc, _ := thread.Register(armcore.RegisterPC)
		lr, _ := thread.Register(armcore.RegisterLR)
		var line strings.Builder
		fmt.Fprintf(&line, "hit %d pc=%#x lr=%#x", *hits, pc, lr)
		for register := 0; register < 13; register++ {
			value, _ := thread.Register(register)
			described := ""
			if *client != nil {
				described = (*client).runtime.describeGuestWord(value)
			}
			fmt.Fprintf(&line, "\n    r%-2d = %#010x  %s", register, value, described)
			var word [4]byte
			if err := core.Memory().Read(value&^3, word[:]); err == nil {
				fmt.Fprintf(&line, "  -> %02x%02x%02x%02x", word[3], word[2], word[1], word[0])
			}
		}
		fmt.Println(line.String())
		return nil
	})
	for _, spec := range strings.Split(list, ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		address, err := strconv.ParseUint(strings.TrimPrefix(spec, "0x"), 16, 32)
		if err != nil {
			t.Fatalf("breakpoint %q: %v", spec, err)
		}
		core.SetBreakpoint(uint32(address))
	}
	for _, spec := range strings.Split(os.Getenv("WFEATURE_BREAKPOINT_WATCH"), ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		address, err := strconv.ParseUint(strings.TrimPrefix(spec, "0x"), 16, 32)
		if err != nil {
			t.Fatalf("watch %q: %v", spec, err)
		}
		core.Watch(uint32(address))
	}
}

// dumpProbeState prints the memory range and the classes the environment asked
// about, once the session exists to name them.
func dumpProbeState(t *testing.T, session *Session) {
	core := session.Client.core
	if spec := os.Getenv("WFEATURE_BREAKPOINT_DUMP"); spec != "" {
		low, high, err := parseProbeRange(spec)
		if err != nil {
			t.Fatalf("dump %q: %v", spec, err)
		}
		names := map[uint32]string{}
		for name, address := range session.Client.runtime.classes {
			names[address] = name
		}
		for address := low &^ 3; address < high; address += 4 {
			var word [4]byte
			if err := core.Memory().Read(address, word[:]); err != nil {
				continue
			}
			fmt.Printf("dump %#x = %#010x %s\n", address, binary.LittleEndian.Uint32(word[:]), names[address])
		}
	}
	for _, name := range strings.Split(os.Getenv("WFEATURE_BREAKPOINT_CLASSES"), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		class, ok := session.Client.vm.AOTClass(name)
		if !ok {
			fmt.Printf("class %s: not registered\n", name)
			continue
		}
		fmt.Printf("class %s @%#x super=%s methods=%d\n", name, class.Address, class.SuperName, len(class.Methods))
		for _, method := range class.Methods {
			fmt.Printf("    %s%s addr=%#x body=%#x native=%#x flags=%#x vt=%d\n",
				method.Name, method.Descriptor, method.Address, method.Body, method.NativeBody, method.AccessFlags, method.VTableIndex)
		}
	}
}

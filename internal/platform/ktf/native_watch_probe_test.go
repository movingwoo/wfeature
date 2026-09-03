package ktf

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// TestLocalNativeWatchProbe answers "what writes this" for a native package.
//
// It is the tool for the state a title stops in when **nothing it asks for is
// unanswered**. A trap list says nothing then, and a trace says only that a
// branch was not taken. A write watch names the instruction that sets a word —
// or says that nothing sets it, which is the answer that ends a search rather
// than starting one.
//
// It has three modes and they answer three questions:
//
//   - `OFFSETS` watches words of the block the title keeps its own state in,
//     which is the word at +0x24 of the object its factory built. That is where
//     both local titles put it, and it is an offset rather than an address
//     because the arena hands out a different block every run.
//   - `PIXELS` watches every page of the pictures the platform is holding for
//     the title, after the given number of ticks. "Does the title ever draw
//     into the surface it blits from" is a question about pixels nobody wrote,
//     and one word is not enough to ask it.
//   - `TRACE` reports which of a list of addresses the guest ran through, which
//     is how a caller is picked out of the seventeen that reach one setter.
//
// Like the other local probes it is a throwaway investigation aid and skips
// unless it is given somewhere to look:
//
//	WFEATURE_NATIVE_WATCH_ARCHIVE=/abs/path/game.zip \
//	WFEATURE_NATIVE_WATCH_OFFSETS=0x1748,0x3380 \
//	WFEATURE_NATIVE_WATCH_TICKS=600 \
//	WFEATURE_NATIVE_WATCH_PIXELS=30 \
//	WFEATURE_NATIVE_WATCH_TRACE=0x11c116,0x10862c \
//	go test ./internal/platform/ktf -run TestLocalNativeWatchProbe -v
func TestLocalNativeWatchProbe(t *testing.T) {
	path := os.Getenv("WFEATURE_NATIVE_WATCH_ARCHIVE")
	offsets := os.Getenv("WFEATURE_NATIVE_WATCH_OFFSETS")
	if path == "" || offsets == "" {
		t.Skip("set WFEATURE_NATIVE_WATCH_ARCHIVE and WFEATURE_NATIVE_WATCH_OFFSETS")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := OpenNative(data)
	if err != nil {
		t.Fatal(err)
	}
	client, err := LoadNativeClient(archive, armcore.CoreOptions{MaxSteps: localAcceptanceMaxSteps(t)})
	if err != nil {
		t.Fatal(err)
	}
	clock := NewManualClock(time.Time{})
	platform := NewNativePlatform(client, archive, clock)
	platform.AttachAudio(nil)

	// The object has to exist before its state block can be found, and the
	// start event is where most of a title's own writing happens — so the
	// watch is armed between the two.
	if err := platform.Create(context.Background()); err != nil {
		t.Fatalf("create: %v", err)
	}
	fields, err := client.ReadFields(platform.Application(), 10)
	if err != nil {
		t.Fatalf("read the application object: %v", err)
	}
	base := fields[9]
	t.Logf("application %#x, state block %#x", platform.Application(), base)

	watched := []uint32{}
	for _, spec := range strings.Split(offsets, ",") {
		spec = strings.TrimSpace(spec)
		offset, err := strconv.ParseUint(strings.TrimPrefix(spec, "0x"), 16, 32)
		if err != nil {
			t.Fatalf("offset %q: %v", spec, err)
		}
		address := base + uint32(offset)
		client.core.Watch(address)
		watched = append(watched, address)
		t.Logf("watching %#x (state + %#x)", address, offset)
	}

	if err := platform.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	// A second mode: watch the pixels of the picture the title blits from,
	// which only exists once it has built one. The addresses come out of the
	// platform's own record rather than off a note, because the arena hands
	// out a different block every run.
	if settle := os.Getenv("WFEATURE_NATIVE_WATCH_PIXELS"); settle != "" {
		rounds, _ := strconv.Atoi(settle)
		for round := 0; round < rounds; round++ {
			interval := platform.FrameInterval()
			if interval == 0 {
				interval = 20 * time.Millisecond
			}
			clock.Advance(interval)
			if _, err := platform.Tick(context.Background()); err != nil {
				break
			}
		}
		for object, image := range platform.images {
			t.Logf("image object %#x bitmap %#x length %d", object, image.data, image.length)
			for at := uint32(0x440); at < image.length; at += 0x100 {
				client.core.Watch(image.data + at)
				watched = append(watched, image.data+at)
				t.Logf("watching %#x (bitmap + %#x)", image.data+at, at)
			}
		}
	}
	// A third mode: trace the ticks and report which of a list of addresses the
	// guest ran through. A watch names the instruction that wrote a word; this
	// names which of the callers reached the one that writes it.
	traced := []uint32{}
	for _, spec := range strings.Split(os.Getenv("WFEATURE_NATIVE_WATCH_TRACE"), ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		address, err := strconv.ParseUint(strings.TrimPrefix(spec, "0x"), 16, 32)
		if err != nil {
			t.Fatalf("trace address %q: %v", spec, err)
		}
		traced = append(traced, uint32(address))
	}
	if len(traced) > 0 {
		client.Trace(8_000_000)
	}
	ticks := 600
	if value := os.Getenv("WFEATURE_NATIVE_WATCH_TICKS"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			ticks = parsed
		}
	}
	for round := 0; round < ticks; round++ {
		interval := platform.FrameInterval()
		if interval == 0 {
			interval = 20 * time.Millisecond
		}
		clock.Advance(interval)
		if _, err := platform.Tick(context.Background()); err != nil {
			t.Logf("tick %d: %v", round, err)
			break
		}
	}

	if len(traced) > 0 {
		log := client.StopTrace()
		seen := map[uint32]int{}
		for _, address := range log {
			seen[address]++
		}
		t.Logf("traced %d instructions", len(log))
		for _, address := range traced {
			t.Logf("  %#x ran %d times", address, seen[address])
		}
	}
	hits := client.core.WatchHits()
	sort.Slice(hits, func(a, b int) bool { return hits[a].First < hits[b].First })
	t.Logf("%d writers over %d ticks (overflowed=%v)", len(hits), ticks, client.core.WatchHitsOverflowed())
	for _, hit := range hits {
		t.Logf("  %#x written by %-5s pc %#x value %#x size %d count %d first %d last %d",
			hit.Address, hit.Origin, hit.PC, hit.Value, hit.Size, hit.Count, hit.First, hit.Last)
	}
	for _, address := range watched {
		found := false
		for _, hit := range hits {
			if hit.Address == address {
				found = true
			}
		}
		if !found {
			t.Logf("  %#x: nothing wrote it", address)
		}
	}
	// The end state of each watched word, which is what the title is reading.
	for _, address := range watched {
		if word, err := client.ReadWord(address); err == nil {
			t.Logf("  %#x holds %#x at the end", address, word)
		}
	}
}

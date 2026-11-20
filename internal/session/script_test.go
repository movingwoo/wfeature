package session

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

// scriptArchiveFixture assembles newly authored bytecode, with one word per
// variable and no external assets. Initialization loads a counter and presents
// white; a key increments and saves it, then presents black. A timer presents
// white again, and termination increments and persists the counter once more.
func scriptArchiveFixture(t *testing.T, lcdMask ...byte) []byte {
	t.Helper()
	data := make([]byte, 52)
	data[0] = 1
	if len(lcdMask) != 0 {
		data[1] = lcdMask[0]
	}
	copy(data[10:26], "Script fixture")
	entry := func(index int, code ...byte) {
		binary.LittleEndian.PutUint16(data[28+index*2:], uint16(len(data)))
		data = append(data, code...)
	}
	entry(0, 5, 16, 5, 1, 0x98, 0x55, 0x78, 5, 10, 5, 1, 0x9a, 0xff)
	entry(1, 0x3a, 16, 1, 5, 16, 5, 1, 0x99, 0xff)
	entry(2, 0x55, 0x78, 0xff)
	entry(3, 0x3a, 16, 1, 5, 16, 5, 1, 0x99, 0x56, 0x78, 0xff)
	vd := len(data)
	for i := 0; i < 17; i++ {
		data = append(data, 1, 1, 0, 0)
	}
	vi := len(data)
	for i, v := range []int{vd, vi, vi, vi} {
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(v))
	}
	return packScriptFixture(t, data)
}

func packScriptFixture(t *testing.T, data []byte) []byte {
	t.Helper()
	var descriptor []byte
	for _, v := range []string{"application/x-gnex-sgs", "SGS"} {
		descriptor = binary.LittleEndian.AppendUint32(descriptor, uint32(len(v)))
		descriptor = append(descriptor, v...)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, b := range map[string][]byte{"script.mod": descriptor, "script.sgs": data} {
		f, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestScriptSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	archive := scriptArchiveFixture(t)
	summary, err := Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Platform != "skt" || summary.Name != "Script fixture" || !strings.HasPrefix(summary.SaveOwner, "sgs-") || summary.MainClass != "" {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	store := backend.NewDirectorySaveStore(t.TempDir())
	s, err := Start(ctx, archive, Options{SaveStore: store, Width: 3, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertFrame := func(white bool) {
		t.Helper()
		rgba, w, h, ok := s.Frame()
		if !ok || w != 3 || h != 2 || len(rgba) != 24 {
			t.Fatalf("frame %dx%d ok=%v bytes=%d", w, h, ok, len(rgba))
		}
		want := byte(0)
		if white {
			want = 255
		}
		if rgba[0] != want || rgba[3] != 255 {
			t.Fatalf("pixel=%v want gray %d", rgba[:4], want)
		}
	}
	assertSaved := func(want uint16) {
		t.Helper()
		data, ok := store.LoadSave("nv/data")
		if !ok || len(data) < 2 || binary.LittleEndian.Uint16(data) != want {
			t.Fatalf("save=%v found=%v want=%d", data, ok, want)
		}
	}
	if !s.Running() || s.Cheat() != nil || s.HasPointer() {
		t.Fatal("unexpected script capabilities")
	}
	assertFrame(true)
	if err := s.SendPointer(ctx, PointerPress, 0, 0); !errors.Is(err, ErrNoPointer) {
		t.Fatalf("pointer=%v", err)
	}
	if err := s.SendKey(ctx, KeyPress, '5'); err != nil {
		t.Fatal(err)
	}
	assertFrame(false)
	assertSaved(1)
	if err := s.SendKey(ctx, KeyRelease, '5'); err != nil {
		t.Fatal(err)
	}
	assertSaved(1)
	if err := s.SendKey(ctx, KeyRepeat, '5'); err != nil {
		t.Fatal(err)
	}
	assertSaved(1)
	s.SetSpeed(2)
	if _, err := s.sktScript().Advance(ctx, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	assertFrame(true)
	if err := s.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tick(ctx, time.Millisecond); !errors.Is(err, ErrPaused) {
		t.Fatalf("paused tick=%v", err)
	}
	if err := s.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	progress, err := s.Tick(ctx, time.Millisecond)
	if err != nil || progress.Wait <= 0 || progress.Exited {
		t.Fatalf("tick=%+v err=%v", progress, err)
	}
	s.Close()
	s.Close()
	assertSaved(2)
	if s.Running() {
		t.Fatal("closed session still running")
	}
	if _, _, _, ok := s.Frame(); ok {
		t.Fatal("closed session retained frame")
	}
	s, err = Start(ctx, archive, Options{SaveStore: store, Width: 3, Height: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SendKey(ctx, KeyPress, '5'); err != nil {
		t.Fatal(err)
	}
	assertSaved(3)
}

func TestScriptSessionDefaultScreen(t *testing.T) {
	s, err := Start(context.Background(), scriptArchiveFixture(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if w, h := s.Screen(); w != 128 || h != 160 {
		t.Fatalf("screen=%dx%d", w, h)
	}
}

func TestScriptSessionLargeLCDAndExplicitScreen(t *testing.T) {
	archive := scriptArchiveFixture(t, 8)
	for _, size := range [][2]int{{0, 0}, {128, 160}} {
		s, err := Start(context.Background(), archive, Options{Width: size[0], Height: size[1]})
		if err != nil {
			t.Fatal(err)
		}
		want := size
		if size[0] == 0 {
			want = [2]int{176, 176}
		}
		w, h := s.Screen()
		_, fw, fh, ok := s.Frame()
		s.Close()
		if w != want[0] || h != want[1] || fw != w || fh != h || !ok {
			t.Fatalf("requested %v: screen %dx%d frame %dx%d valid=%v, want %v", size, w, h, fw, fh, ok, want)
		}
	}
}

// Exercise the word interpreter through the same archive detector, framebuffer,
// clock and private save store that a player opens from the browser.
func TestWideScriptSessionLifecycle(t *testing.T) {
	data := make([]byte, 128)
	data[0], data[5] = 4, 4
	copy(data[10:26], "Wide fixture")
	entry := func(slot int, words ...uint16) {
		binary.LittleEndian.PutUint32(data[28+4*slot:], uint32(len(data)))
		for _, word := range words {
			data = binary.LittleEndian.AppendUint16(data, word)
		}
	}
	entry(0, 0x92, 0x3a, 0x92, 0x5f, 0xff)
	// Save the most recent key event as a full word, clear the LCD using RGB.
	entry(3, 0x8e, 0, 0x05, 1, 0x92, 0x94, 0x05, 12, 0x05, 34, 0x05, 56, 0x92, 0x38, 0x92, 0x5f, 0xff)
	vd := len(data)
	for range 20 {
		data = append(data, 1, 0, 1, 0, 0, 0, 0, 0)
	}
	vi := len(data)
	data = append(data, make([]byte, 80)...)
	for i, n := range []int{vd, vi, len(data), len(data)} {
		binary.LittleEndian.PutUint32(data[72+i*4:], uint32(n))
	}
	binary.LittleEndian.PutUint32(data[120:], uint32(len(data)))
	wrapper := make([]byte, 32)
	wrapper[0], wrapper[8] = 32, 111
	archive := packScriptFixture(t, append(wrapper, data...))
	ctx := context.Background()
	store := backend.NewDirectorySaveStore(t.TempDir())
	session, err := Start(ctx, archive, Options{Width: 5, Height: 7, SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	rgba, w, h, ok := session.Frame()
	if !ok || w != 5 || h != 7 || rgba[0] != 255 || rgba[3] != 255 || session.Summary().Name != "Wide fixture" {
		t.Fatal("wrong initial frame or title")
	}
	for _, event := range []struct {
		action   string
		expected uint32
	}{{KeyPress, 5}, {KeyRelease, 255}} {
		if err := session.SendKey(ctx, event.action, '5'); err != nil {
			t.Fatal(err)
		}
		stored, ok := store.LoadSave("nv/data")
		if !ok || len(stored) < 4 || binary.LittleEndian.Uint32(stored) != event.expected {
			t.Fatalf("%s: stored bytes %x", event.action, stored)
		}
	}
	rgba, _, _, _ = session.Frame()
	if rgba[3] != 255 || rgba[0] >= rgba[1] || rgba[1] >= rgba[2] {
		t.Fatalf("key frame pixel %v", rgba[:4])
	}
}

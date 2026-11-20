package skt

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func TestWideCheckpointScreenBuffers(t *testing.T) {
	fb, _ := backend.NewMemoryFramebuffer(3, 1)
	screen, err := newWideScreen(fb)
	if err != nil {
		t.Fatal(err)
	}
	screen.g.pixels[0], screen.back[0] = 255, 77
	screen.g.clip = paintRect{0, 0, 2, 1}
	screen.g.color = wideTransparent
	screen.palette[13] = 0x123456
	screen.gamma, screen.zoom, screen.font, screen.align = 4, 1, 5, -1
	if err := screen.present(screen.full()); err != nil {
		t.Fatal(err)
	}
	screen.g.pixels[0] = 33
	saved := screen.checkpoint()
	restored, err := saved.restore()
	if err != nil {
		t.Fatal(err)
	}
	if restored.presents != 1 || restored.g.color != wideTransparent || restored.palette[13] != 0x123456 ||
		restored.g.clip != screen.g.clip || restored.zoom != 1 || restored.gamma != 4 || restored.font != 5 || restored.align != -1 ||
		restored.g.pixels[0] != 33 || restored.back[0] != 77 || restored.visible[0] != 255 {
		t.Fatal("display, backup or drawing state changed on restore")
	}
	screen.g.pixels[0] = 0
	if bytes.Equal(screen.g.pixels, restored.g.pixels) {
		t.Fatal("restored drawing buffer aliases the running screen")
	}
	saved.Font = 6
	if _, err := saved.restore(); err == nil {
		t.Fatal("accepted invalid font index")
	}
	saved.Font = 2
	saved.Visible = saved.Visible[:4]
	if _, err := saved.restore(); err == nil {
		t.Fatal("accepted a truncated display")
	}
}

func TestWideCheckpointResumesTextEditor(t *testing.T) {
	banks := make([][]int32, 10)
	for i := range banks {
		banks[i] = []int32{0}
	}
	program := &sgsvm.Program{Data: []byte{0xff, 0}, CodeEnd: 2, Wide: &sgsvm.WideProgram{Variables: banks},
		Resources: []sgsvm.Resource{{Data: []byte("PIN")}, {Mutable: true, Data: []byte{'4', 0}}}}
	archive := &Archive{Script: program, identity: backend.SaveIdentity(program.Data)}
	fb, _ := backend.NewMemoryFramebuffer(2, 1)
	store, _ := backend.NewMemorySaveStore(nil)
	options := ScriptOptions{Framebuffer: fb, SaveStore: store, Speed: 2}
	ctx := t.Context()
	previous, err := StartScript(ctx, archive, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []int32{0, 1, 4, 2} {
		if err := previous.wide.Push(arg); err != nil {
			t.Fatal(err)
		}
	}
	if err := previous.openWideEditor(previous.wide); err != nil {
		t.Fatal(err)
	}
	oldField, err := previous.TextInput(ctx)
	if err != nil {
		t.Fatal(err)
	}
	previous.clock = 2 * time.Second
	previous.timers[0] = scriptTimer{period: time.Hour, due: previous.clock + time.Hour, repeat: true, active: true}
	previous.timers[1] = scriptTimer{period: time.Millisecond, due: previous.clock + time.Millisecond, active: true}
	previous.random.Uint64()
	cp, err := previous.CaptureCheckpointWithSession(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareWideCheckpoint(archive, cp.Runtime, options)
	if err != nil {
		t.Fatal(err)
	}
	if f, n := prepared.Frame(); n != 0 || len(f.RGBA) != 0 {
		t.Fatal("load presented an unflushed screen")
	}
	if n, key := prepared.PreparationStoreCalls(); n != 0 {
		t.Fatalf("preparation accessed %q", key)
	}
	restored, err := prepared.Commit(ctx, previous, store, fb)
	if err != nil {
		t.Fatal(err)
	}
	if restored.clock != previous.clock || restored.timers != previous.timers || restored.random.Uint64() != previous.random.Uint64() {
		t.Fatal("editor restore changed the clock, timers or random state")
	}
	if err := oldField.Commit(ctx, "3"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("old editor commit: %v", err)
	}
	field, err := restored.TextInput(ctx)
	if err != nil || field.Text != "4" || !field.Password || field.InputMode != "numeric" || field.MaxLength != 4 {
		t.Fatalf("restored editor %+v: %v", field, err)
	}
	if err := field.Commit(ctx, "7302"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.wide.Resources[1].Data, []byte{'7', '3', '0', '2', 0}) || restored.wide.Variables[0][0] != 2 {
		t.Fatal("restored editor failed to commit to guest memory or deliver its completion event")
	}
}

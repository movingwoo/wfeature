package lgt

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The backtrace is read out of the guest's own frame chain, so the fixture
// builds one: two frames of the shape the compiler emits, and a method index
// to name their return addresses with.
const (
	fixtureFrameBase uint32 = fixtureDataBase + 0x800
	fixtureBodyOuter uint32 = 0x40000
	fixtureBodyInner uint32 = 0x41000
)

func backtraceFixture(t *testing.T) *Client {
	t.Helper()
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	runtime.byHandle[fixtureClassHandle] = &javaRuntimeClass{
		Name: "w",
		Record: javaClass{Methods: []javaMember{
			{Name: "run", Descriptor: "()V", Body: fixtureBodyOuter},
			{Name: "i", Descriptor: "()V", Body: fixtureBodyInner},
		}},
	}
	return client
}

// An address inside a method is named by that method and its offset; one past
// the last body is named by nothing, because a method's extent stops where the
// next one starts and the last one is not unbounded.
func TestJavaAddressesAreNamedByTheMethodTheyAreIn(t *testing.T) {
	client := backtraceFixture(t)
	sites := client.javaMethodIndex()

	for _, want := range []struct {
		address uint32
		text    string
	}{
		{fixtureBodyOuter, "w.run()V+0x0"},
		{fixtureBodyOuter + 0x20, "w.run()V+0x20"},
		{fixtureBodyInner + 4, "w.i()V+0x4"},
		{fixtureBodyOuter - 4, "0x3fffc"},
		{fixtureBodyInner + maxJavaMethodSpan, "0x61000"},
	} {
		if got := javaNameAddress(sites, want.address); got != want.text {
			t.Errorf("javaNameAddress(%#x) = %q, want %q", want.address, got, want.text)
		}
	}
}

// A thread stopped inside a platform call is stopped in a stub, so the frame it
// is in is named by the link register; the frames under it come out of the
// chain, and a word that is not a frame pointer ends the walk rather than
// inventing depth.
func TestJavaBacktraceWalksTheFrameChain(t *testing.T) {
	client := backtraceFixture(t)

	// Two frames: the inner one's saved link register points into the outer
	// method, and its saved frame pointer names the outer frame, whose own
	// chain word is zero.
	inner, outer := fixtureFrameBase, fixtureFrameBase+0x40
	write := func(at uint32, value uint32) {
		word := make([]byte, 4)
		binary.LittleEndian.PutUint32(word, value)
		if err := client.core.Memory().Write(at, word); err != nil {
			t.Fatal(err)
		}
	}
	write(inner-4, fixtureBodyOuter+0x208) // the caller of the inner frame
	write(inner-12, outer)                 // the caller's frame
	write(outer-4, 0x7fff0000)             // the platform's sentinel return
	write(outer-12, 0)                     // and no frame beyond it

	context := armcore.NewContext()
	context.Registers[armcore.RegisterPC] = 0x31000268 // a platform stub
	context.Registers[armcore.RegisterLR] = fixtureBodyInner + 0xf0
	context.Registers[javaFramePointer] = inner
	thread := armcore.NewThread(context)

	got := client.javaBacktraceLine(thread)
	want := "w.i()V+0xf0 < w.run()V+0x208 < 0x7fff0000"
	if got != want {
		t.Errorf("backtrace = %q, want %q", got, want)
	}
	if strings.Count(got, "<") != 2 {
		t.Errorf("the walk did not stop at the frame with no caller: %q", got)
	}
}

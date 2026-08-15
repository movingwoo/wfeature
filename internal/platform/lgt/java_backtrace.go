package lgt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// Where a Java title is, in its own names.
//
// A platform call says which method the title asked for and, with the caller's
// address, where it asked from — and an address in a module of a megabyte of
// compiled Java is not an answer anyone can use. What names it is the class
// records: every method record carries the address its body starts at, so the
// nearest body at or below an address names the method the address is in.
//
// The frames come out of the guest's own stack. The compiler emits an APCS
// frame — `mov ip, sp; push {..., fp, ip, lr, pc}; sub fp, ip, #4` — so `fp`
// points at the saved `pc` and the three words below it are the return address,
// the caller's stack pointer and the caller's `fp`. Walking that chain gives
// the call stack of a title that is stuck without stopping it, which is the
// difference between "it waits in a loop" and "it waits in this method, called
// from that one".

// maxJavaFrames bounds the walk. A chain that goes deeper than this is a stack
// that has been overwritten, not a title that is nested that far.
const maxJavaFrames = 32

// maxJavaMethodSpan is how long a method body may be before an address that
// far past its start is taken to be somewhere else entirely. The largest body
// among the local titles is a few tens of kilobytes.
const maxJavaMethodSpan = 0x20000

// javaCodeSite is one method body, for naming addresses.
type javaCodeSite struct {
	Body  uint32
	Class string
	Name  string
}

// javaMethodIndex builds the address-ordered list of every method body in the
// classes this run has prepared. It is rebuilt on demand rather than kept: a
// backtrace is asked for when something has gone wrong or is taking too long,
// and the list is a few hundred entries.
func (client *Client) javaMethodIndex() []javaCodeSite {
	runtime := client.javaRun
	if runtime == nil {
		return nil
	}
	sites := make([]javaCodeSite, 0, len(runtime.byHandle)*8)
	for _, class := range runtime.byHandle {
		for _, method := range class.Record.Methods {
			if method.Body == 0 {
				continue
			}
			sites = append(sites, javaCodeSite{
				Body: method.Body, Class: class.Name, Name: method.Name + method.Descriptor,
			})
		}
	}
	sort.Slice(sites, func(a, b int) bool { return sites[a].Body < sites[b].Body })
	return sites
}

// javaNameAddress names one code address as a method and an offset into it.
// **A method's extent is where the next one starts**, so an address past the
// last body of a class run is reported as an address rather than as a long way
// into the method before it.
func javaNameAddress(sites []javaCodeSite, address uint32) string {
	if len(sites) == 0 || address == 0 {
		return fmt.Sprintf("%#x", address)
	}
	index := sort.Search(len(sites), func(i int) bool { return sites[i].Body > address }) - 1
	if index < 0 {
		return fmt.Sprintf("%#x", address)
	}
	site := sites[index]
	// A method's extent is where the next body starts, and the last body in
	// the list has no next one — so an address is also refused when it is
	// further into a method than any method is long. Without that, an address
	// on the platform's own side of the world reads as a huge offset into the
	// title's last method.
	limit := site.Body + maxJavaMethodSpan
	if index+1 < len(sites) && sites[index+1].Body < limit {
		limit = sites[index+1].Body
	}
	if address >= limit {
		return fmt.Sprintf("%#x", address)
	}
	return fmt.Sprintf("%s.%s+%#x", site.Class, site.Name, address-site.Body)
}

// javaBacktrace answers where a thread is, innermost frame first. The current
// address comes from the program counter and the rest from the frame chain; a
// frame whose words cannot be read ends the walk, because a stack that does not
// read is worth reporting as far as it went rather than not at all.
func (client *Client) javaBacktrace(thread *armcore.Thread) []string {
	if thread == nil {
		return nil
	}
	sites := client.javaMethodIndex()
	context := thread.Context()
	// **Inside a platform call the program counter is the platform's**, not the
	// title's: the thread is stopped in a stub. The link register is where it
	// came from, so it is what names the innermost frame there, and the frame
	// chain below is the same either way.
	inner := context.Registers[armcore.RegisterPC]
	if strings.HasPrefix(javaNameAddress(sites, inner), "0x") {
		inner = context.Registers[armcore.RegisterLR]
	}
	frames := []string{javaNameAddress(sites, inner)}
	frame := context.Registers[javaFramePointer]
	for depth := 0; depth < maxJavaFrames; depth++ {
		if frame < 0x1000 || frame&3 != 0 {
			break
		}
		caller, err := client.readWord(frame - 4)
		if err != nil {
			break
		}
		next, err := client.readWord(frame - 12)
		if err != nil || next <= frame {
			// A chain walks upwards through the stack; anything else is a
			// frame pointer that is not one.
			frames = append(frames, javaNameAddress(sites, caller))
			break
		}
		frames = append(frames, javaNameAddress(sites, caller))
		frame = next
	}
	return frames
}

// javaFramePointer is the register the compiled code keeps its frame in. It is
// `fp` in APCS, which ARM numbers 11.
const javaFramePointer = 11

func (client *Client) javaBacktraceLine(thread *armcore.Thread) string {
	return strings.Join(client.javaBacktrace(thread), " < ")
}

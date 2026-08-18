package skt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/cheat"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// cheatTarget adapts the synthetic heap map to the cheat engine, which is the
// same three questions the two ARM platforms answer — what can be read, what
// can be written, and which regions are worth sweeping. The difference is that
// there the answers come from a guest address space and here they are made:
// see heapmap.go.
//
// It is not a WatchTarget. A watch names the instruction that wrote an address,
// and finding that here would mean instrumenting every `putfield` and every
// array store in the interpreter — which is a real thing to build and a
// different one from this. The engine answers cheat.ErrWatchUnsupported until
// it exists, and a Host reads that as "no watch control on this platform"
// rather than as a failure.
type cheatTarget struct {
	runtime *Runtime
}

func (target cheatTarget) ReadMemory(address uint32, destination []byte) error {
	return target.runtime.withHeap(func(heap *heapMap) error { return heap.read(address, destination) })
}

func (target cheatTarget) WriteMemory(address uint32, data []byte) error {
	return target.runtime.withHeap(func(heap *heapMap) error { return heap.write(address, data) })
}

// Regions walks the object graph first. A search that has just been started
// wants the objects the game has allocated since the last one, and this is the
// call that begins every sweep.
func (target cheatTarget) Regions() []cheat.Region {
	var regions []cheat.Region
	_ = target.runtime.withHeap(func(heap *heapMap) error {
		heap.refresh()
		regions = heap.regions()
		return nil
	})
	return regions
}

// withHeap runs one operation against the map, building it on first use.
func (runtime *Runtime) withHeap(operation func(*heapMap) error) error {
	if runtime == nil || runtime.VM == nil {
		return fmt.Errorf("this game has no heap to search")
	}
	runtime.heapMu.Lock()
	defer runtime.heapMu.Unlock()
	if runtime.heap == nil {
		runtime.heap = newHeapMap(runtime.VM, runtime.platformRoots)
		runtime.heap.refresh()
	}
	return operation(runtime.heap)
}

// platformRoots are the objects this runtime holds in Go rather than in the
// guest's own graph. A MIDlet is one — nothing static points at it — and so is
// the screen being shown, which is where a title keeps the state of whatever
// the player is looking at.
func (runtime *Runtime) platformRoots() []*jvm.Object {
	var roots []*jvm.Object
	if runtime.MIDlet != nil {
		roots = append(roots, runtime.MIDlet)
	}
	runtime.displayMu.RLock()
	for _, object := range []*jvm.Object{
		runtime.displayOwner, runtime.display, runtime.currentDisplayable, runtime.pendingDisplayable,
	} {
		if object != nil {
			roots = append(roots, object)
		}
	}
	roots = append(roots, runtime.pendingSerial...)
	runtime.displayMu.RUnlock()

	runtime.renderMu.Lock()
	if runtime.paintCanvas != nil {
		roots = append(roots, runtime.paintCanvas)
	}
	runtime.renderMu.Unlock()

	// Every Displayable the runtime has state for, not only the one on screen.
	// A title keeps its menus and its map screen as objects it comes back to,
	// and between visits nothing static points at them; the runtime's own table
	// is the only thing that does.
	if state := runtime.lcduiState; state != nil {
		state.mu.Lock()
		for object := range state.displayables {
			roots = append(roots, object)
		}
		state.mu.Unlock()
	}
	if state := runtime.skvmState; state != nil {
		state.mu.Lock()
		for _, object := range []*jvm.Object{state.smsListener, state.textFieldOwner} {
			if object != nil {
				roots = append(roots, object)
			}
		}
		state.mu.Unlock()
	}
	return roots
}

// Cheat returns the session's attached cheat engine, creating it on first use.
// RunPending reapplies its frozen values after every Host pass.
func (runtime *Runtime) Cheat() *cheat.Session {
	if runtime == nil || runtime.VM == nil {
		return nil
	}
	runtime.heapMu.Lock()
	defer runtime.heapMu.Unlock()
	if runtime.cheat == nil {
		runtime.cheat = cheat.NewSession(cheatTarget{runtime: runtime})
	}
	return runtime.cheat
}

// CheatConsole is the text command console bound to Cheat().
func (runtime *Runtime) CheatConsole() *cheat.Console {
	if runtime == nil || runtime.VM == nil {
		return nil
	}
	session := runtime.Cheat()
	runtime.heapMu.Lock()
	defer runtime.heapMu.Unlock()
	if runtime.cheatConsole == nil {
		runtime.cheatConsole = cheat.NewConsole(session)
		// A saved table names the game it was made against, which is the only
		// thing that lets one be placed months later.
		if runtime.Archive != nil {
			runtime.cheatConsole.SetGame(SaveOwner(runtime.Archive.Descriptor))
		}
	}
	return runtime.cheatConsole
}

// serviceCheat rewrites frozen values after a Host pass so they win over
// whatever the game just wrote, and holds the objects they are in so the
// collector cannot take a frozen value out from under itself.
func (runtime *Runtime) serviceCheat() error {
	if runtime == nil || runtime.cheat == nil {
		return nil
	}
	frozen := runtime.cheat.Freezes().Entries()
	addresses := make([]uint32, 0, len(frozen))
	for _, entry := range frozen {
		addresses = append(addresses, entry.Address)
	}
	if err := runtime.withHeap(func(heap *heapMap) error {
		heap.retain(addresses)
		return nil
	}); err != nil {
		return err
	}
	if failed := runtime.cheat.Tick(); len(failed) > 0 {
		return fmt.Errorf("SKT cheat freeze failed at %#x", failed[0])
	}
	return nil
}

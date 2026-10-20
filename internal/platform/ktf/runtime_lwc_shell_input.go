package ktf

import (
	"slices"

	"github.com/movingwoo/wfeature/internal/jvm"
)

const (
	runtimeLWCShownShellObject            = "lwc:shownShell"
	componentShellVisibilityRevisionField = "shellVisibilityRevision:J"
	// runtimeLWCShellHistoryObject is the newest shell a title has shown and
	// not hidden since, and componentShellBelowField links each such shell to
	// the one shown before it. The record of the newest shell is dropped when
	// that shell is hidden, which leaves no way back to a shell it covered and
	// that is on the screen again; the history is that way back. Both are
	// ordinary heap state, so a checkpoint keeps them.
	runtimeLWCShellHistoryObject = "lwc:shellHistory"
	componentShellBelowField     = "host:shell-below"
	// maxShellHistory bounds how many shown shells the history keeps, and
	// maxShellHistoryWalk how far a walk follows it, so a malformed restored
	// chain is still a bounded one.
	maxShellHistory     = 16
	maxShellHistoryWalk = 64
	// maxShellWalkNodes and maxShellWalkDepth bound the walk through a shell's
	// containers. A field deeper than the depth could not prove its ownership
	// either: that walk stops at 64 ancestors.
	maxShellWalkNodes = 1024
	maxShellWalkDepth = 64
)

// shellTextComponent answers the shown shell's single direct text child. A
// local caller adds a TextBox and shows its shell without calling setFocus.
// This is the rule a Host edit has always been held to;
// relaxedShellTextComponent is the one consulted after it.
func (client *Client) shellTextComponent() *jvm.Object {
	shell := client.runtime.runtimeObjects[runtimeLWCShownShellObject]
	if !componentShown(shell) {
		return nil
	}
	children := runtimeComponentChildren(shell)
	work, _ := shell.Fields[componentWorkField].Reference()
	var field *jvm.Object
	switch {
	case len(children) == 1 && (work == nil || work == children[0]):
		field = children[0]
	case len(children) == 0:
		field = work
	default:
		return nil
	}
	if _, vendor, ok := client.lwcTextInputKind(field); ok && !vendor {
		return field
	}
	return nil
}

// componentShown reports what the title last said about a component's
// visibility.
func componentShown(component *jvm.Object) bool {
	if component == nil {
		return false
	}
	shown, err := component.Fields[componentShownField].Int32()
	return err == nil && shown != 0
}

// recordShellHistory rebuilds the history after a shell was shown or hidden:
// the shell just shown first, then the shells still shown in the order they
// were, and nothing hidden. A hidden shell is dropped rather than kept for a
// later show, which puts it at the front again anyway, so the history never
// holds on to a shell the title has put away.
func (runtime *initializationRuntime) recordShellHistory(shell *jvm.Object, shown bool) {
	var kept []*jvm.Object
	if shown {
		kept = append(kept, shell)
	}
	visited := make(map[*jvm.Object]bool)
	for node, steps := runtime.runtimeObjects[runtimeLWCShellHistoryObject], 0; node != nil && !visited[node] && steps < maxShellHistoryWalk; steps++ {
		visited[node] = true
		below, _ := node.Fields[componentShellBelowField].Reference()
		delete(node.Fields, componentShellBelowField)
		if node != shell && len(kept) < maxShellHistory && componentShown(node) {
			kept = append(kept, node)
		}
		node = below
	}
	if !shown {
		delete(shell.Fields, componentShellBelowField)
	}
	for index, node := range kept {
		if index+1 < len(kept) {
			node.Fields[componentShellBelowField] = jvm.ReferenceValue(kept[index+1])
		} else {
			delete(node.Fields, componentShellBelowField)
		}
	}
	if len(kept) == 0 {
		delete(runtime.runtimeObjects, runtimeLWCShellHistoryObject)
		return
	}
	runtime.runtimeObjects[runtimeLWCShellHistoryObject] = kept[0]
}

// shownShell is the newest shell still on the screen: the one shown last, or,
// once that one is hidden, the newest one it covered that is still shown. The
// record of the newest shell comes first, so a checkpoint taken before the
// history existed still answers it.
func (runtime *initializationRuntime) shownShell() *jvm.Object {
	if shell := runtime.runtimeObjects[runtimeLWCShownShellObject]; componentShown(shell) {
		return shell
	}
	visited := make(map[*jvm.Object]bool)
	for node, steps := runtime.runtimeObjects[runtimeLWCShellHistoryObject], 0; node != nil && !visited[node] && steps < maxShellHistoryWalk; steps++ {
		visited[node] = true
		if componentShown(node) {
			return node
		}
		node, _ = node.Fields[componentShellBelowField].Reference()
	}
	return nil
}

// relaxedShellTextComponent is the shell rule a Host snapshot falls back on
// once every route under its first rules found nothing: the text fields of the
// newest shell still shown, wherever its containers hold them. One field is
// the one to type into. Of several, the one the title gave focus is, through
// setFocus or by telling it with focusNotify, and a title that marked none is
// typed into at its first field. A field the choice falls on that cannot take
// a Host edit — a composition listener, an unknown constraint — refuses the
// route rather than handing the text to a field beside it.
func (client *Client) relaxedShellTextComponent() *jvm.Object {
	shell := client.runtime.shownShell()
	if shell == nil {
		return nil
	}
	fields := client.shellTextFields(shell)
	if len(fields) == 0 {
		return nil
	}
	if focus := client.runtime.runtimeObjects["lwc:focus"]; focus != nil && slices.Contains(fields, focus) {
		return focus
	}
	for _, field := range fields {
		if focused, err := field.Fields[componentFocusedField].Int32(); err == nil && focused != 0 {
			return field
		}
	}
	return fields[0]
}

// shellTextFields lists a shell's non-vendor text fields in the order the
// title placed them: its children, then its work component, each container
// walked where it stands. A shell nested in it is a screen of its own and is
// not walked; a vendor field belongs to its form's lifecycle.
func (client *Client) shellTextFields(shell *jvm.Object) []*jvm.Object {
	roots := slices.Clone(runtimeComponentChildren(shell))
	if work, _ := shell.Fields[componentWorkField].Reference(); work != nil && !slices.Contains(roots, work) {
		roots = append(roots, work)
	}
	var fields []*jvm.Object
	// A Host commit asks this again at every character it delivers, and a
	// fresh set for a screen of a thousand components was most of what that
	// cost, so the set is kept between walks and emptied after each, holding
	// on to nothing.
	runtime := client.runtime
	if runtime.shellWalkSeen == nil {
		runtime.shellWalkSeen = make(map[*jvm.Object]bool)
	}
	seen := runtime.shellWalkSeen
	defer clear(seen)
	seen[shell] = true
	budget := maxShellWalkNodes
	var walk func(nodes []*jvm.Object, depth int)
	walk = func(nodes []*jvm.Object, depth int) {
		for _, node := range nodes {
			if budget == 0 {
				return
			}
			if node == nil || seen[node] {
				continue
			}
			seen[node] = true
			budget--
			if _, vendor, ok := client.lwcTextInputKind(node); ok {
				if !vendor {
					fields = append(fields, node)
				}
				continue
			}
			if _, shell := node.Fields[componentShellVisibilityRevisionField]; shell || client.lwcShellClass(node.ClassName) || depth >= maxShellWalkDepth {
				continue
			}
			walk(runtimeComponentChildren(node), depth+1)
		}
	}
	walk(roots, 1)
	return fields
}

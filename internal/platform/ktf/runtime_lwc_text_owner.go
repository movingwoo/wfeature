package ktf

import "github.com/movingwoo/wfeature/internal/jvm"

const (
	componentHostParentField     = "hostParent:Lorg/kwis/msp/lwc/Component;"
	componentParentRevisionField = "hostParentRevision:J"
	componentFocusRevisionField  = "hostFocusRevision:J"
)

func runtimeComponentAttachChild(parent, child *jvm.Object) {
	if child == nil {
		return
	}
	if child.Fields == nil {
		child.Fields = make(map[string]jvm.Value)
	}
	child.Fields[componentHostParentField] = jvm.ReferenceValue(parent)
	runtimeComponentIncrementRevision(child, componentParentRevisionField)
}

func runtimeComponentHasChild(parent, child *jvm.Object) bool {
	work, _ := parent.Fields[componentWorkField].Reference()
	if work == child {
		return true
	}
	for _, candidate := range runtimeComponentChildren(parent) {
		if candidate == child {
			return true
		}
	}
	return false
}

func runtimeComponentDetachChild(parent, child *jvm.Object) {
	if child == nil || runtimeComponentHasChild(parent, child) {
		return
	}
	owner, _ := child.Fields[componentHostParentField].Reference()
	if owner == parent {
		runtimeComponentAttachChild(nil, child)
	}
}

type lwcTextAncestor struct {
	object                                 *jvm.Object
	parent, revision, children, visibility jvm.Value
}

// A field that has never joined a container may be drawn by its game card.
// Once attached, it must remain in that live hierarchy. A detached child is
// not a standalone editor, and a hidden shell cannot retain editable focus.
//
// The rules a Host edit has always been held to accept only the shell shown
// last, and only a field that never joined anything. relaxed is the second
// pass a Host snapshot makes once those found nothing: any shell that is still
// shown owns its fields — a title that shows a message shell over its form and
// hides it again is back on the form, although the record of the newest shell
// went with the message — and a container that never joined anything is the
// card's to draw, as a lone field is. The walk and the chain it answers are the
// same under both, so a commit compares one against the other.
func (client *Client) lwcTextOwnership(field *jvm.Object, relaxed bool) ([]lwcTextAncestor, bool) {
	var chain []lwcTextAncestor
	seen := make(map[*jvm.Object]bool)
	for node := field; node != nil && len(chain) < 64; {
		if seen[node] {
			return nil, false
		}
		seen[node] = true
		parent, attached := node.Fields[componentHostParentField]
		visibility, shell := node.Fields[componentShellVisibilityRevisionField]
		chain = append(chain, lwcTextAncestor{node, parent, node.Fields[componentParentRevisionField], node.Fields[componentChildrenRevisionField], visibility})
		if shell || client.lwcShellClass(node.ClassName) {
			return chain, componentShown(node) && (relaxed || client.runtime.runtimeObjects[runtimeLWCShownShellObject] == node)
		}
		if !attached {
			return chain, relaxed || node == field
		}
		owner, err := parent.Reference()
		if err != nil || owner == nil || !runtimeComponentHasChild(owner, node) {
			return nil, false
		}
		node = owner
	}
	return nil, false
}

func (client *Client) lwcShellClass(name string) bool {
	for depth := 0; depth < 64 && name != ""; depth++ {
		if name == runtimeShellComponentClass {
			return true
		}
		class, ok := client.vm.AOTClass(name)
		if !ok {
			return false
		}
		name = class.SuperName
	}
	return false
}

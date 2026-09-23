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
func (client *Client) lwcTextOwnership(field *jvm.Object) ([]lwcTextAncestor, bool) {
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
			shown, err := node.Fields[componentShownField].Int32()
			return chain, err == nil && shown != 0 && client.runtime.runtimeObjects[runtimeLWCShownShellObject] == node
		}
		if !attached {
			return chain, node == field
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

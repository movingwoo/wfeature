package ktf

import "github.com/movingwoo/wfeature/internal/jvm"

const (
	runtimeLWCShownShellObject            = "lwc:shownShell"
	componentShellVisibilityRevisionField = "shellVisibilityRevision:J"
)

type shellTextInputState struct {
	shell, field         *jvm.Object
	visibility, children jvm.Value
}

// shellTextInput exposes only a shell's single direct text child. A local
// caller adds a TextBox and shows its shell without calling setFocus. This
// Host adapter does not guess focus in nested or multi-component layouts.
func (client *Client) shellTextInput() shellTextInputState {
	shell := client.runtime.runtimeObjects[runtimeLWCShownShellObject]
	if shell == nil {
		return shellTextInputState{}
	}
	state := shellTextInputState{
		shell:      shell,
		visibility: shell.Fields[componentShellVisibilityRevisionField],
		children:   shell.Fields[componentChildrenRevisionField],
	}
	shown, err := shell.Fields[componentShownField].Int32()
	if err != nil || shown == 0 {
		return state
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
		return state
	}
	if _, vendor, ok := client.lwcTextInputKind(field); ok && !vendor {
		state.field = field
	}
	return state
}

package ktf

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/textinput"
)

func (context *heapNativeContext) captureEditor(editor *textinput.State) (uint32, error) {
	if editor == nil {
		return 0, nil
	}
	if id := context.editorIDs[editor]; id != 0 {
		return id, nil
	}
	if len(context.editors) >= maxHeapRootRecords {
		return 0, fmt.Errorf("KTF heap editor count exceeds limit")
	}
	saved, err := editor.CaptureState(context.now)
	if err != nil {
		return 0, err
	}
	context.editorBytes += uint64(len(saved.Text))*4 + 128
	if context.editorBytes > maxHeapStorageBytes {
		return 0, fmt.Errorf("KTF heap editor data exceeds limit")
	}
	if context.editorIDs == nil {
		context.editorIDs = make(map[*textinput.State]uint32)
	}
	id := uint32(len(context.editors) + 1)
	context.editorIDs[editor] = id
	context.editors = append(context.editors, saved)
	return id, nil
}

func (context *heapNativeContext) restoreEditors(saved []textinput.StateSnapshot) error {
	if len(saved) > maxHeapRootRecords {
		return fmt.Errorf("KTF heap editor count exceeds limit")
	}
	var size uint64
	for _, state := range saved {
		size += uint64(len(state.Text))*4 + 128
		if size > maxHeapStorageBytes {
			return fmt.Errorf("KTF heap editor data exceeds limit")
		}
	}
	context.restoredEditors = make([]*textinput.State, len(saved)+1)
	for i, state := range saved {
		editor, err := textinput.RestoreState(state, context.now)
		if err != nil {
			return err
		}
		context.restoredEditors[i+1] = editor
	}
	return nil
}

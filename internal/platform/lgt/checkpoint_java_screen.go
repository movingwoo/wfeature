package lgt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/movingwoo/wfeature/internal/backend"
)

// What the screen calls keep on this side that the Graphics record has no
// field for: the stroke style of each Graphics set to anything but SOLID
// (java_screen.go), and the face, style and size the last Font.getFont asked
// for (java_font.go).
//
// **It is a record of its own, wrapping the session record**, for the reason
// the class library's is (checkpoint_java_library.go): the checkpoint codec
// reads a strict schema, so a field added to the Graphics or the Java record
// would make every quick save an earlier build took unreadable. Version 5
// carries the session record and the media and class-library records beside
// this one; a session holding nothing here is written in the shape an earlier
// build reads, and every earlier shape still loads, with every stroke SOLID
// and no font asked for, which is what a build that kept neither left.
type sessionScreenCheckpointState struct {
	Version uint32
	Session sessionCheckpointState
	Media   *javaMediaState
	Library *javaLibraryState
	Screen  javaScreenState
}

const sessionScreenCheckpointVersion = 5

type javaScreenState struct {
	// Strokes is every Graphics whose stroke style is not SOLID, in the order
	// of the objects.
	Strokes []javaStrokeState
	Font    javaFontState
}

type javaStrokeState struct {
	Graphics uint32
	Style    int32
}

type javaFontState struct {
	Face, Style, Size int32
}

// captureJavaScreenState records the stroke styles and the font request, or
// answers nil when there is none of either, which is what keeps such a
// session's record in the earlier shape.
func (client *Client) captureJavaScreenState() (*javaScreenState, error) {
	runtime := client.javaRun
	if runtime == nil {
		return nil, nil
	}
	request := runtime.fontRequest
	saved := &javaScreenState{Font: javaFontState{Face: request.face, Style: request.style, Size: request.size}}
	for _, object := range slices.Sorted(maps.Keys(runtime.graphics)) {
		if state := runtime.graphics[object]; state != nil && state.stroke != 0 {
			saved.Strokes = append(saved.Strokes, javaStrokeState{Graphics: object, Style: state.stroke})
		}
	}
	if len(saved.Strokes) == 0 && saved.Font == (javaFontState{}) {
		return nil, nil
	}
	if len(saved.Strokes) > maxStateRecords {
		return nil, fmt.Errorf("LGT checkpoint has %d stroke styles, more than the %d a checkpoint restores", len(saved.Strokes), maxStateRecords)
	}
	return saved, nil
}

// restoreJavaScreenState gives a restored runtime its stroke styles and font
// request. A record without them restores none, and so does one from a Clet.
// Every stroke has to belong to a Graphics the Java record restored, once and
// in order, before any of them is applied.
func (client *Client) restoreJavaScreenState(saved *javaScreenState) error {
	if saved == nil {
		return nil
	}
	runtime := client.javaRun
	if runtime == nil {
		return fmt.Errorf("LGT checkpoint has screen state and no Java runtime")
	}
	if len(saved.Strokes) > maxStateRecords {
		return fmt.Errorf("LGT checkpoint screen state has a table past its limit")
	}
	for index, stroke := range saved.Strokes {
		if index > 0 && saved.Strokes[index-1].Graphics >= stroke.Graphics {
			return fmt.Errorf("LGT checkpoint screen state has stroke styles out of order")
		}
		if runtime.graphics[stroke.Graphics] == nil || stroke.Style == 0 {
			return fmt.Errorf("LGT checkpoint screen state has a stroke style for no Graphics it restored")
		}
	}
	for _, stroke := range saved.Strokes {
		runtime.graphics[stroke.Graphics].stroke = stroke.Style
	}
	runtime.fontRequest = javaFontRequest{face: saved.Font.Face, style: saved.Font.Style, size: saved.Font.Size}
	return nil
}

// encodeLGTScreenRecord writes a session record with the screen state beside
// it, or in the shape encodeLGTSessionRecord writes when there is none.
func encodeLGTScreenRecord(
	saved sessionCheckpointState, media *javaMediaState, library *javaLibraryState, screen *javaScreenState,
) ([]byte, error) {
	if screen == nil {
		return encodeLGTSessionRecord(saved, media, library)
	}
	return backend.EncodeCheckpointRecord(sessionScreenCheckpointState{
		Version: sessionScreenCheckpointVersion, Session: saved, Media: media, Library: library, Screen: *screen,
	})
}

// decodeLGTScreenRecord reads every shape encodeLGTScreenRecord writes: this
// record, and every shape decodeLGTSessionRecord reads, which carry no screen
// state.
//
// **The version is read before the record is.** Every shape opens with its
// version, and a strict decode that fails only at a record's end has walked the
// whole of it first, the memory image included; asking for this shape only
// when the record says it is one keeps a load of an earlier shape from paying
// for that walk.
func decodeLGTScreenRecord(data []byte) (
	sessionCheckpointState, *javaMediaState, *javaLibraryState, *javaScreenState, error,
) {
	if version, ok := leadingRecordVersion(data); ok && version == sessionScreenCheckpointVersion {
		var wrapped sessionScreenCheckpointState
		wrappedErr := backend.DecodeCheckpointRecord(data, &wrapped)
		if wrappedErr == nil {
			return wrapped.Session, wrapped.Media, wrapped.Library, &wrapped.Screen, nil
		}
		// Another record may share the number; the earlier shapes are asked
		// before the record is called damaged.
		saved, media, library, err := decodeLGTSessionRecord(data)
		if err != nil {
			return sessionCheckpointState{}, nil, nil, nil, wrappedErr
		}
		return saved, media, library, nil, nil
	}
	saved, media, library, err := decodeLGTSessionRecord(data)
	return saved, media, library, nil, err
}

// leadingRecordVersion reads the version a record opens with, without reading
// the rest of it: every record here is an object whose first member is its
// Version.
func leadingRecordVersion(data []byte) (uint32, bool) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return 0, false
	}
	if token, err := decoder.Token(); err != nil || token != "Version" {
		return 0, false
	}
	token, err := decoder.Token()
	if err != nil {
		return 0, false
	}
	number, ok := token.(json.Number)
	if !ok {
		return 0, false
	}
	version, err := strconv.ParseUint(number.String(), 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(version), true
}

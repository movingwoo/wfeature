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
// (java_screen.go), the face, style and size the last Font.getFont asked for
// (java_font.go), and the cards' boxes with the surface the shown one paints
// into (java_card.go).
//
// **It is a record of its own, wrapping the session record**, for the reason
// the class library's is (checkpoint_java_library.go): the checkpoint codec
// reads a strict schema, so a field added to the Graphics or the Java record
// would make every quick save an earlier build took unreadable. Version 5
// carries the session record and the media and class-library records beside
// the strokes and the font; version 6 carries the cards as well, because the
// strict schema cannot make them optional in version 5's shape. Each is written
// only when the session holds what it adds: a session holding nothing here is
// written in the shape an earlier build reads, and one holding no card state
// in exactly version 5's. Every earlier shape still loads, with every stroke
// SOLID, no font asked for and every card the whole screen, which is what a
// build that kept none of them left.
type sessionScreenCheckpointState struct {
	Version uint32
	Session sessionCheckpointState
	Media   *javaMediaState
	Library *javaLibraryState
	Screen  javaStrokeScreenState
}

// sessionCardScreenCheckpointState is version 6: version 5 with the cards.
type sessionCardScreenCheckpointState struct {
	Version uint32
	Session sessionCheckpointState
	Media   *javaMediaState
	Library *javaLibraryState
	Screen  javaScreenState
}

const (
	sessionScreenCheckpointVersion     = 5
	sessionCardScreenCheckpointVersion = 6
)

type javaScreenState struct {
	// Strokes is every Graphics whose stroke style is not SOLID, in the order
	// of the objects.
	Strokes []javaStrokeState
	Font    javaFontState
	Cards   javaCardsState
}

// javaStrokeScreenState is the screen state as version 5 holds it: the
// strokes and the font, and no cards.
type javaStrokeScreenState struct {
	Strokes []javaStrokeState
	Font    javaFontState
}

// javaCardsState is the cards' part of the screen state. Bounds is every card
// with a box, in the order of the objects; Surface is the surface the shown
// card paints into while it has a box, whose pixels the client record keeps
// with every other surface, and Owner the card it was made for — zero once a
// new card has been built at that card's address, and with no surface. The
// surface the next paint makes is decided by these, so they are recorded
// rather than made again.
type javaCardsState struct {
	Bounds  []javaCardBoundsState
	Surface uint32
	Owner   uint32
}

type javaCardBoundsState struct {
	Card                uint32
	X, Y, Width, Height int32
}

func (cards javaCardsState) empty() bool {
	return len(cards.Bounds) == 0 && cards.Surface == 0 && cards.Owner == 0
}

type javaStrokeState struct {
	Graphics uint32
	Style    int32
}

type javaFontState struct {
	Face, Style, Size int32
}

// captureJavaScreenState records the stroke styles, the font request and the
// cards, or answers nil when there is none of them, which is what keeps such a
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
	// A box whose card the collector has reclaimed belongs to nothing; it is
	// left out rather than restored onto whatever the address holds next.
	for _, card := range slices.Sorted(maps.Keys(runtime.cardBounds)) {
		if _, live := runtime.objects[card]; !live {
			continue
		}
		box := runtime.cardBounds[card]
		saved.Cards.Bounds = append(saved.Cards.Bounds, javaCardBoundsState{
			Card: card, X: box.x, Y: box.y, Width: box.width, Height: box.height,
		})
	}
	saved.Cards.Surface, saved.Cards.Owner = runtime.cardSurface, runtime.cardSurfaceOwner
	if len(saved.Strokes) == 0 && saved.Font == (javaFontState{}) && saved.Cards.empty() {
		return nil, nil
	}
	if len(saved.Strokes) > maxStateRecords || len(saved.Cards.Bounds) > maxStateRecords {
		return nil, fmt.Errorf("LGT checkpoint has %d stroke styles and %d card boxes, more than the %d of each a checkpoint restores",
			len(saved.Strokes), len(saved.Cards.Bounds), maxStateRecords)
	}
	// What a load would refuse is refused here, where the session that holds
	// it is still running, rather than in a quick save nothing can load.
	if err := client.validateJavaCards(saved.Cards); err != nil {
		return nil, err
	}
	return saved, nil
}

// restoreJavaScreenState gives a restored runtime its stroke styles, font
// request and cards. A record without them restores none, and so does one from
// a Clet. Everything the record claims is checked before any of it is applied:
// every stroke has to belong to a Graphics the Java record restored, and every
// card to an object it restored, once and in order.
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
	if err := client.validateJavaCards(saved.Cards); err != nil {
		return err
	}
	for _, stroke := range saved.Strokes {
		runtime.graphics[stroke.Graphics].stroke = stroke.Style
	}
	runtime.fontRequest = javaFontRequest{face: saved.Font.Face, style: saved.Font.Style, size: saved.Font.Size}
	runtime.cardBounds = make(map[uint32]javaCardBox, len(saved.Cards.Bounds))
	for _, bounds := range saved.Cards.Bounds {
		runtime.cardBounds[bounds.Card] = javaCardBox{x: bounds.X, y: bounds.Y, width: bounds.Width, height: bounds.Height}
	}
	runtime.cardSurface, runtime.cardSurfaceOwner = saved.Cards.Surface, saved.Cards.Owner
	return nil
}

// validateJavaCards checks the cards' part of a screen record against the
// session it would be restored onto. Every box belongs to an object the Java
// record holds, once, in order, and is not the whole screen, which is kept as
// no box. The surface, when there is one, is a surface the client record holds
// that nothing but the screen's Graphics draws on: not the screen, not a
// picture's, and not one another Graphics targets, because the next paint of a
// card without a box gives it back.
func (client *Client) validateJavaCards(saved javaCardsState) error {
	runtime := client.javaRun
	if len(saved.Bounds) > maxStateRecords {
		return fmt.Errorf("LGT checkpoint screen state has a table past its limit")
	}
	screen := client.javaScreenBox()
	for index, bounds := range saved.Bounds {
		if index > 0 && saved.Bounds[index-1].Card >= bounds.Card {
			return fmt.Errorf("LGT checkpoint screen state has card boxes out of order")
		}
		if _, live := runtime.objects[bounds.Card]; !live || bounds.Card == 0 {
			return fmt.Errorf("LGT checkpoint screen state has a box for no card it restored")
		}
		if (javaCardBox{x: bounds.X, y: bounds.Y, width: bounds.Width, height: bounds.Height}) == screen {
			return fmt.Errorf("LGT checkpoint screen state has a card box that is the whole screen")
		}
	}
	if saved.Surface == 0 {
		if saved.Owner != 0 {
			return fmt.Errorf("LGT checkpoint screen state has a card surface owner and no surface")
		}
		return nil
	}
	surface := client.framebuffer(saved.Surface)
	if surface == nil || surface.screen || surface.handle != saved.Surface {
		return fmt.Errorf("LGT checkpoint screen state has a card surface %#x it does not hold", saved.Surface)
	}
	for _, held := range runtime.images {
		if held == saved.Surface {
			return fmt.Errorf("LGT checkpoint screen state has a picture's surface as a card's")
		}
	}
	for _, held := range runtime.decodedImages {
		if held == saved.Surface {
			return fmt.Errorf("LGT checkpoint screen state has a picture's surface as a card's")
		}
	}
	for object, state := range runtime.graphics {
		if state != nil && state.surface == saved.Surface && object != runtime.screenGraphics {
			return fmt.Errorf("LGT checkpoint screen state has a card surface another Graphics draws on")
		}
	}
	return nil
}

// encodeLGTScreenRecord writes a session record with the screen state beside
// it: version 6 when the session holds card state, version 5 when it holds only
// strokes and a font request, and the shape encodeLGTSessionRecord writes when
// it holds none of them.
func encodeLGTScreenRecord(
	saved sessionCheckpointState, media *javaMediaState, library *javaLibraryState, screen *javaScreenState,
) ([]byte, error) {
	if screen == nil {
		return encodeLGTSessionRecord(saved, media, library)
	}
	if screen.Cards.empty() {
		return backend.EncodeCheckpointRecord(sessionScreenCheckpointState{
			Version: sessionScreenCheckpointVersion, Session: saved, Media: media, Library: library,
			Screen: javaStrokeScreenState{Strokes: screen.Strokes, Font: screen.Font},
		})
	}
	return backend.EncodeCheckpointRecord(sessionCardScreenCheckpointState{
		Version: sessionCardScreenCheckpointVersion, Session: saved, Media: media, Library: library, Screen: *screen,
	})
}

// decodeLGTScreenRecord reads every shape encodeLGTScreenRecord writes: the two
// screen records, and every shape decodeLGTSessionRecord reads, which carry no
// screen state.
//
// **The version is read before the record is.** Every shape opens with its
// version, and a strict decode that fails only at a record's end has walked the
// whole of it first, the memory image included; asking for a screen record only
// when the record says it is one keeps a load of an earlier shape from paying
// for that walk.
func decodeLGTScreenRecord(data []byte) (
	sessionCheckpointState, *javaMediaState, *javaLibraryState, *javaScreenState, error,
) {
	version, ok := leadingRecordVersion(data)
	var wrappedErr error
	switch {
	case ok && version == sessionCardScreenCheckpointVersion:
		var wrapped sessionCardScreenCheckpointState
		if wrappedErr = backend.DecodeCheckpointRecord(data, &wrapped); wrappedErr == nil {
			return wrapped.Session, wrapped.Media, wrapped.Library, &wrapped.Screen, nil
		}
	case ok && version == sessionScreenCheckpointVersion:
		var wrapped sessionScreenCheckpointState
		if wrappedErr = backend.DecodeCheckpointRecord(data, &wrapped); wrappedErr == nil {
			screen := &javaScreenState{Strokes: wrapped.Screen.Strokes, Font: wrapped.Screen.Font}
			return wrapped.Session, wrapped.Media, wrapped.Library, screen, nil
		}
	}
	saved, media, library, err := decodeLGTSessionRecord(data)
	if err != nil && wrappedErr != nil {
		// Another record may share the number, so the earlier shapes were asked
		// before the record was called damaged; it is the screen record's
		// failure that says what is wrong with it.
		return sessionCheckpointState{}, nil, nil, nil, wrappedErr
	}
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

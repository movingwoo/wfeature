package lgt

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// strokedGraphicsFixture builds three Graphics on a client's screen with the
// stroke styles given, and asks for a font, so that every kind of screen state
// is held.
func strokedGraphicsFixture(t *testing.T, client *Client, styles ...uint32) []uint32 {
	t.Helper()
	screen, err := client.screenSurface()
	if err != nil {
		t.Fatal(err)
	}
	objects := make([]uint32, len(styles))
	for index, style := range styles {
		if objects[index], err = client.newJavaGraphics(screen.handle); err != nil {
			t.Fatal(err)
		}
		mustJavaCall(t, client, javaGraphicsClass+".setStrokeStyle(I)V", objects[index], style)
	}
	mustJavaCall(t, client, javaFontClass+".getFont(III)Lorg/kwis/msp/lcdui/Font;", 64, 5, 16)
	return objects
}

// The stroke styles and the font request come back from a checkpoint as they
// were: in a record of the version this build writes for them, decoded and
// restored onto the Graphics the Java record restored.
func TestJavaScreenStateSurvivesACheckpoint(t *testing.T) {
	client := fixtureClient(t)
	objects := strokedGraphicsFixture(t, client, 1, 0, 0xffffffff)
	saved, err := client.captureJavaState(&stateBudget{})
	if err != nil {
		t.Fatal(err)
	}
	screen, err := client.captureJavaScreenState()
	if err != nil || screen == nil {
		t.Fatalf("captureJavaScreenState() = %v, %v", screen, err)
	}
	if len(screen.Strokes) != 2 {
		t.Fatalf("the record holds %d stroke styles, want the 2 that are not SOLID", len(screen.Strokes))
	}
	record, err := encodeLGTScreenRecord(sessionCheckpointState{Version: sessionCheckpointVersion, Client: clientState{Java: saved}}, nil, nil, screen)
	if err != nil {
		t.Fatal(err)
	}
	if version, ok := leadingRecordVersion(record); !ok || version != sessionScreenCheckpointVersion {
		t.Fatalf("the record opens with version %d (%v), want %d", version, ok, sessionScreenCheckpointVersion)
	}
	session, media, library, decoded, err := decodeLGTScreenRecord(record)
	if err != nil || media != nil || library != nil || decoded == nil {
		t.Fatalf("decodeLGTScreenRecord() = %v, %v, %v, %v", media, library, decoded, err)
	}
	if err := client.restoreJavaState(session.Client.Java); err != nil {
		t.Fatal(err)
	}
	if err := client.restoreJavaScreenState(decoded); err != nil {
		t.Fatal(err)
	}
	for index, want := range []uint32{1, 0, 0xffffffff} {
		if got := mustJavaCall(t, client, javaGraphicsClass+".getStrokeStyle()I", objects[index]); got != want {
			t.Errorf("Graphics %d's restored stroke style = %#x, want %#x", index, got, want)
		}
	}
	font := mustJavaCall(t, client, javaFontClass+".getDefaultFont()Lorg/kwis/msp/lcdui/Font;")
	for name, want := range map[string]uint32{"getFace()I": 64, "getStyle()I": 5, "getSize()I": 16} {
		if got := mustJavaCall(t, client, javaFontClass+"."+name, font); got != want {
			t.Errorf("the restored font's %s = %d, want %d", name, got, want)
		}
	}
}

// Every shape an earlier build wrote still decodes, with no screen state —
// which is what such a build left — and a session holding none is written in
// exactly the shape it would have been written in without this record.
func TestJavaScreenRecordLeavesEarlierRecordsReadable(t *testing.T) {
	session := sessionCheckpointState{Version: sessionCheckpointVersion}
	library := &javaLibraryState{Numbers: []javaNumberState{{Object: 0x1000, Value: 7}}}
	for _, shape := range []struct {
		name    string
		media   *javaMediaState
		library *javaLibraryState
	}{
		{"session alone", nil, nil},
		{"media wrapper", &javaMediaState{}, nil},
		{"class library", nil, library},
		{"both", &javaMediaState{}, library},
	} {
		earlier, err := encodeLGTSessionRecord(session, shape.media, shape.library)
		if err != nil {
			t.Fatalf("%s: %v", shape.name, err)
		}
		_, media, decodedLibrary, screen, err := decodeLGTScreenRecord(earlier)
		if err != nil {
			t.Fatalf("%s: %v", shape.name, err)
		}
		if screen != nil || (media != nil) != (shape.media != nil) || !reflect.DeepEqual(decodedLibrary, shape.library) {
			t.Errorf("%s came back as media %v, library %+v, screen %+v", shape.name, media, decodedLibrary, screen)
		}
		written, err := encodeLGTScreenRecord(session, shape.media, shape.library, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(written, earlier) {
			t.Errorf("%s with no screen state is not written in the earlier shape", shape.name)
		}

		// With screen state beside them, the other records come back as well.
		held := &javaScreenState{Strokes: []javaStrokeState{{Graphics: 0x2000, Style: 1}}, Font: javaFontState{Size: 8}}
		record, err := encodeLGTScreenRecord(session, shape.media, shape.library, held)
		if err != nil {
			t.Fatal(err)
		}
		_, media, decodedLibrary, screen, err = decodeLGTScreenRecord(record)
		if err != nil || !reflect.DeepEqual(screen, held) || (media != nil) != (shape.media != nil) ||
			!reflect.DeepEqual(decodedLibrary, shape.library) {
			t.Errorf("%s with screen state came back as media %v, library %+v, screen %+v, %v",
				shape.name, media, decodedLibrary, screen, err)
		}
	}
}

// What a record claims is checked before any of it is applied: every stroke
// belongs to a Graphics the Java record restored, once, in order, and is not
// SOLID, and a record a strict reader refuses is refused whole.
func TestJavaScreenRecordRefusesWhatItCannotRestore(t *testing.T) {
	client := fixtureClient(t)
	objects := strokedGraphicsFixture(t, client, 0, 0)
	first, second := min(objects[0], objects[1]), max(objects[0], objects[1])
	for name, saved := range map[string]*javaScreenState{
		"a stroke for no Graphics": {Strokes: []javaStrokeState{{Graphics: 0x7000, Style: 1}}},
		"strokes out of order":     {Strokes: []javaStrokeState{{Graphics: second, Style: 1}, {Graphics: first, Style: 1}}},
		"one Graphics twice":       {Strokes: []javaStrokeState{{Graphics: first, Style: 1}, {Graphics: first, Style: 1}}},
		"a SOLID stroke":           {Strokes: []javaStrokeState{{Graphics: first, Style: 0}}},
	} {
		if err := client.restoreJavaScreenState(saved); err == nil {
			t.Errorf("%s was restored", name)
		}
	}
	if got := mustJavaCall(t, client, javaGraphicsClass+".getStrokeStyle()I", first); got != 0 {
		t.Errorf("a refused record left a stroke style of %d behind", got)
	}
	if err := client.restoreJavaScreenState(nil); err != nil {
		t.Errorf("a record with no screen state was refused: %v", err)
	}

	record, err := encodeLGTScreenRecord(sessionCheckpointState{Version: sessionCheckpointVersion}, nil, nil,
		&javaScreenState{Font: javaFontState{Face: 32}})
	if err != nil {
		t.Fatal(err)
	}
	for name, damaged := range map[string][]byte{
		"unknown field": bytes.Replace(record, []byte(`"Screen":`), []byte(`"Unknown":0,"Screen":`), 1),
		"missing field": bytes.Replace(record, []byte(`"Screen":`), []byte(`"Other":`), 1),
		"other version": bytes.Replace(record, []byte(`"Version":5,`), []byte(`"Version":7,`), 1),
		// Version 6 is the shape with the cards, which this record lacks.
		"version 6": bytes.Replace(record, []byte(`"Version":5,`), []byte(`"Version":6,`), 1),
	} {
		if bytes.Equal(damaged, record) {
			t.Fatalf("%s: the record was not changed", name)
		}
		if _, _, _, _, err := decodeLGTScreenRecord(damaged); err == nil {
			t.Errorf("a record with %s was decoded", name)
		}
	}

	client.javaRun = nil
	if err := client.restoreJavaScreenState(&javaScreenState{Font: javaFontState{Face: 32}}); err == nil {
		t.Error("screen state was restored onto a session with no Java runtime")
	}
}

// Through a whole quick save and load, a stroke style and the font request
// come back on the restored session; a quick save taken before this record
// existed — the same session record without it — still loads, with every
// stroke SOLID and no font asked for; and a session holding none of either is
// written in that earlier shape.
func TestJavaScreenStateRoundTripsThroughAQuickSave(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	runtime := f.client.javaRuntimeState()
	runtime.fontRequest = javaFontRequest{}
	plain := f.checkpoint()
	if version, _ := leadingRecordVersion(plain.Runtime); version == sessionScreenCheckpointVersion {
		t.Fatal("a session holding no screen state was written in the screen record's shape")
	}
	if _, _, _, err := decodeLGTSessionRecord(plain.Runtime); err != nil {
		t.Fatalf("a session holding no screen state is not readable as the earlier shape: %v", err)
	}

	objects := strokedGraphicsFixture(t, f.client, 1)
	checkpoint := f.checkpoint()
	if version, _ := leadingRecordVersion(checkpoint.Runtime); version != sessionScreenCheckpointVersion {
		t.Fatalf("the quick save opens with version %d, want %d", version, sessionScreenCheckpointVersion)
	}
	restored := f.restore(checkpoint)
	if got := mustJavaCall(t, restored.client, javaGraphicsClass+".getStrokeStyle()I", objects[0]); got != 1 {
		t.Errorf("the restored stroke style = %d, want DOTTED", got)
	}
	if got := restored.client.javaRuntimeState().fontRequest; got != (javaFontRequest{face: 64, style: 5, size: 16}) {
		t.Errorf("the restored font request = %+v, want face 64, style 5, size 16", got)
	}

	// The same quick save as a build before this record wrote it.
	session, media, library, screen, err := decodeLGTScreenRecord(checkpoint.Runtime)
	if err != nil || screen == nil {
		t.Fatalf("decodeLGTScreenRecord() = %v, %v", screen, err)
	}
	earlier := checkpoint
	if earlier.Runtime, err = encodeLGTSessionRecord(session, media, library); err != nil {
		t.Fatal(err)
	}
	loaded := f.restore(earlier)
	if got := mustJavaCall(t, loaded.client, javaGraphicsClass+".getStrokeStyle()I", objects[0]); got != 0 {
		t.Errorf("an earlier-shape quick save restored a stroke style of %d, want SOLID", got)
	}
	if got := loaded.client.javaRuntimeState().fontRequest; got != (javaFontRequest{}) {
		t.Errorf("an earlier-shape quick save restored a font request %+v, want none", got)
	}
}

// boxedCardFixture builds, on a client, a card with a box that is shown and
// has made its surface the way its first paint makes it, and another card with
// a box that is not shown. It answers the two.
func boxedCardFixture(t *testing.T, client *Client) (shown, other uint32) {
	t.Helper()
	class, err := client.preparePlatformJavaClass(javaCardClass)
	if err != nil {
		t.Fatal(err)
	}
	if shown, err = client.allocateJavaObject(class); err != nil {
		t.Fatal(err)
	}
	if other, err = client.allocateJavaObject(class); err != nil {
		t.Fatal(err)
	}
	display := mustJavaCall(t, client, cardDisplayKey)
	mustJavaCall(t, client, cardAtKey, shown, 3, 2, 5, 4)
	mustJavaCall(t, client, cardOnAtKey, other, display, 0xffffffff, 1, 2, 30)
	mustJavaCall(t, client, cardPushKey, display, shown)
	frame, err := client.javaScreenGraphics()
	if err != nil || !frame.positioned {
		t.Fatalf("javaScreenGraphics() = %+v, %v, want the shown card's surface", frame, err)
	}
	frame.surface.pixels[0] = 0x4321
	return shown, other
}

// Through a whole quick save and load, the cards' boxes come back, and so does
// the surface the shown one paints into, pixels and all, with the screen's
// Graphics still drawing on it; the record is version 6.
func TestJavaCardsSurviveAQuickSave(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	shown, other := boxedCardFixture(t, f.client)
	runtime := f.client.javaRuntimeState()
	checkpoint := f.checkpoint()
	if version, _ := leadingRecordVersion(checkpoint.Runtime); version != sessionCardScreenCheckpointVersion {
		t.Fatalf("the quick save opens with version %d, want %d", version, sessionCardScreenCheckpointVersion)
	}
	restored := f.restore(checkpoint)
	loaded := restored.client.javaRuntimeState()
	want := map[uint32]javaCardBox{shown: {x: 3, y: 2, width: 5, height: 4}, other: {x: -1, y: 1, width: 2, height: 30}}
	if !reflect.DeepEqual(loaded.cardBounds, want) {
		t.Errorf("the restored boxes are %+v, want %+v", loaded.cardBounds, want)
	}
	if loaded.cardSurface != runtime.cardSurface || loaded.cardSurfaceOwner != shown {
		t.Errorf("the restored card surface is %#x for %#x, want %#x for %#x",
			loaded.cardSurface, loaded.cardSurfaceOwner, runtime.cardSurface, shown)
	}
	surface := restored.client.framebuffer(loaded.cardSurface)
	if surface == nil || surface.width != 5 || surface.height != 4 || surface.pixels[0] != 0x4321 {
		t.Fatalf("the restored card surface is %+v", surface)
	}
	if graphics, err := restored.client.javaGraphicsState(loaded.screenGraphics); err != nil || graphics.surface != surface.handle {
		t.Errorf("the restored screen Graphics draws on %+v, %v, want the card's surface", graphics, err)
	}
	for name, want := range map[string]uint32{"getX()I": 3, "getY()I": 2, "getWidth()I": 5, "getHeight()I": 4} {
		if got := mustJavaCall(t, restored.client, javaCardClass+"."+name, shown); got != want {
			t.Errorf("the restored card's %s = %d, want %d", name, got, want)
		}
	}
}

// A quick save in a shape written before the cards were — version 5 as it was
// first written, and the session, media and class-library records of versions
// 2 to 4 — loads with every card the whole screen: no box, no surface, and the
// next paint on the screen itself.
func TestJavaCardsAreTheWholeScreenAfterAnEarlierQuickSave(t *testing.T) {
	f := newLGTWIPIListenerFixture(t)
	shown, _ := boxedCardFixture(t, f.client)
	// A font request, so that a build before the cards wrote version 5.
	mustJavaCall(t, f.client, javaFontClass+".getFont(III)Lorg/kwis/msp/lcdui/Font;", 64, 1, 8)
	session, media, library, screen, err := decodeLGTScreenRecord(f.checkpoint().Runtime)
	if err != nil || screen == nil || media == nil {
		t.Fatalf("decodeLGTScreenRecord() = %v, %v, %v", media, screen, err)
	}
	if library == nil {
		library = &javaLibraryState{}
	}
	shapes := map[string]any{
		"version 2": session,
		"version 3": sessionMediaCheckpointState{Version: sessionMediaCheckpointVersion, Session: session, Media: *media},
		"version 4": sessionLibraryCheckpointState{
			Version: sessionLibraryCheckpointVersion, Session: session, Media: media, Library: *library},
		"version 5": sessionScreenCheckpointState{
			Version: sessionScreenCheckpointVersion, Session: session, Media: media, Library: library,
			Screen: javaStrokeScreenState{Strokes: screen.Strokes, Font: screen.Font}},
	}
	for name, shape := range shapes {
		record, err := backend.EncodeCheckpointRecord(shape)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) {
			earlier := f.checkpoint()
			earlier.Runtime = record
			loaded := f.restore(earlier)
			runtime := loaded.client.javaRuntimeState()
			if len(runtime.cardBounds) != 0 || runtime.cardSurface != 0 || runtime.cardSurfaceOwner != 0 {
				t.Errorf("an earlier quick save restored boxes %+v and the surface %#x", runtime.cardBounds, runtime.cardSurface)
			}
			if got := mustJavaCall(t, loaded.client, javaCardClass+".getWidth()I", shown); got != 16 {
				t.Errorf("the card is %d wide after an earlier quick save, want the screen", got)
			}
			frame, err := loaded.client.javaScreenGraphics()
			if err != nil || frame.positioned || frame.surface != loaded.client.screen {
				t.Errorf("after an earlier quick save the card paints through %+v, %v, want the screen", frame, err)
			}
		})
	}
}

// A session holding strokes or a font request and no card state is written in
// exactly the shape version 5 had before the cards — the bytes a build without
// them writes — and reads back with no cards.
func TestJavaScreenRecordWithoutCardsIsVersionFive(t *testing.T) {
	type earlierScreen struct {
		Strokes []javaStrokeState
		Font    javaFontState
	}
	type earlierRecord struct {
		Version uint32
		Session sessionCheckpointState
		Media   *javaMediaState
		Library *javaLibraryState
		Screen  earlierScreen
	}
	session := sessionCheckpointState{Version: sessionCheckpointVersion}
	strokes := []javaStrokeState{{Graphics: 0x2000, Style: 1}}
	font := javaFontState{Face: 32, Size: 8}
	want, err := backend.EncodeCheckpointRecord(earlierRecord{
		Version: sessionScreenCheckpointVersion, Session: session, Screen: earlierScreen{Strokes: strokes, Font: font},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := encodeLGTScreenRecord(session, nil, nil, &javaScreenState{Strokes: strokes, Font: font})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("a record with no cards is written as\n%s\nwant\n%s", got, want)
	}
	_, _, _, screen, err := decodeLGTScreenRecord(want)
	if err != nil || screen == nil || !reflect.DeepEqual(screen.Strokes, strokes) || screen.Font != font || !screen.Cards.empty() {
		t.Errorf("a version 5 record decodes as %+v, %v", screen, err)
	}
}

// What the cards' part of a record claims is checked before any of the record
// is applied: every box belongs to a card the Java record restored, once and in
// order, and is not the whole screen; the surface is one the client record
// holds that only the screen's Graphics draws on, and an owner comes with a
// surface. A version 6 record a strict reader refuses is refused whole.
func TestJavaCardRecordRefusesWhatItCannotRestore(t *testing.T) {
	client := fixtureClient(t)
	shown, other := boxedCardFixture(t, client)
	runtime := client.javaRuntimeState()
	surface, first, second := runtime.cardSurface, min(shown, other), max(shown, other)
	picture, err := javaCreateBlankImage(client, nil, nil, []uint32{2, 2})
	if err != nil {
		t.Fatal(err)
	}
	pictureSurface := runtime.images[picture]
	drawn, err := client.newFramebuffer(2, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.newJavaGraphics(drawn.handle); err != nil {
		t.Fatal(err)
	}
	box := func(card uint32) javaCardBoundsState {
		return javaCardBoundsState{Card: card, X: 1, Y: 1, Width: 2, Height: 2}
	}
	held := javaCardsState{Bounds: []javaCardBoundsState{box(first), box(second)}, Surface: surface, Owner: shown}
	if err := client.restoreJavaScreenState(&javaScreenState{Cards: held}); err != nil {
		t.Fatalf("a record the session can hold was refused: %v", err)
	}
	for name, cards := range map[string]javaCardsState{
		"a box for no card":          {Bounds: []javaCardBoundsState{box(0x7000)}},
		"a box for null":             {Bounds: []javaCardBoundsState{box(0)}},
		"boxes out of order":         {Bounds: []javaCardBoundsState{box(second), box(first)}},
		"one card twice":             {Bounds: []javaCardBoundsState{box(first), box(first)}},
		"a box that is the screen":   {Bounds: []javaCardBoundsState{{Card: first, Width: 16, Height: 8}}},
		"an owner and no surface":    {Owner: shown},
		"a surface it does not hold": {Surface: 0x7000, Owner: shown},
		"the screen as the surface":  {Surface: client.screen.handle, Owner: shown},
		"a picture's surface":        {Surface: pictureSurface, Owner: shown},
		"another Graphics' surface":  {Surface: drawn.handle, Owner: shown},
	} {
		strokes := []javaStrokeState{{Graphics: runtime.screenGraphics, Style: 1}}
		if err := client.restoreJavaScreenState(&javaScreenState{Strokes: strokes, Cards: cards}); err == nil {
			t.Errorf("%s was restored", name)
		}
	}
	if graphics, _ := client.javaGraphicsState(runtime.screenGraphics); graphics.stroke != 0 {
		t.Error("a refused record left a stroke style behind")
	}
	if len(runtime.cardBounds) != 2 || runtime.cardSurface != surface {
		t.Errorf("a refused record changed the cards to %+v and %#x", runtime.cardBounds, runtime.cardSurface)
	}

	record, err := encodeLGTScreenRecord(sessionCheckpointState{Version: sessionCheckpointVersion}, nil, nil,
		&javaScreenState{Cards: javaCardsState{Bounds: []javaCardBoundsState{box(first)}}})
	if err != nil {
		t.Fatal(err)
	}
	if version, _ := leadingRecordVersion(record); version != sessionCardScreenCheckpointVersion {
		t.Fatalf("a record with cards opens with version %d", version)
	}
	for name, damaged := range map[string][]byte{
		"unknown field": bytes.Replace(record, []byte(`"Cards":`), []byte(`"Unknown":0,"Cards":`), 1),
		"missing field": bytes.Replace(record, []byte(`"Owner":0`), []byte(`"Other":0`), 1),
		"version 5":     bytes.Replace(record, []byte(`"Version":6,`), []byte(`"Version":5,`), 1),
	} {
		if bytes.Equal(damaged, record) {
			t.Fatalf("%s: the record was not changed", name)
		}
		if _, _, _, _, err := decodeLGTScreenRecord(damaged); err == nil {
			t.Errorf("a record with %s was decoded", name)
		}
	}
}

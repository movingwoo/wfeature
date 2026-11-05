package lgt

import (
	"bytes"
	"reflect"
	"testing"
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
		"other version": bytes.Replace(record, []byte(`"Version":5,`), []byte(`"Version":6,`), 1),
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

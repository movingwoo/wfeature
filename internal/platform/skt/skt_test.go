package skt_test

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/skt"
)

//go:embed testdata/skvm.jar
var skvmJAR []byte

func TestSKVMFixedPointMathMatchesTheNineDecimalScale(t *testing.T) {
	runtime := startFixture(t, nil)

	// 1.0 is 1_000_000_000, and every operation rescales after multiplying.
	const want = "1000000000|3000000000|6000000000|500000000|7|3000000000|0.50"
	if state := fixtureString(t, runtime, "fixedPointState"); state != want {
		t.Fatalf("fixedPointState() = %q, want %q", state, want)
	}
	if !fixtureBoolean(t, runtime, "divideByZeroThrows") {
		t.Fatal("divide by zero did not throw ArithmeticException")
	}
}

func TestSKVMDeviceStateRoundTrips(t *testing.T) {
	runtime := startFixture(t, nil)
	// setMaxValue(50) then setValue(80) clamps to the maximum.
	if state := fixtureString(t, runtime, "deviceState"); state != "1193046|50|50|32x24" {
		t.Fatalf("deviceState() = %q", state)
	}
}

func TestSKVMGraphics2DReadsAndWritesPixels(t *testing.T) {
	runtime := startFixture(t, nil)
	// The pixel written comes back exactly; inverting black gives white.
	if state := fixtureString(t, runtime, "pixelState"); state != "3368601|16777215|2x2" {
		t.Fatalf("pixelState() = %q", state)
	}
}

func TestSKVMFilesPersistThroughTheSaveBoundary(t *testing.T) {
	root := t.TempDir()
	store := backend.NewDirectorySaveStore(filepath.Join(root, "SKVM Fixture"))
	runtime := startFixture(t, store)

	const want = "4|4|0|2|true|4|false"
	if state := fixtureString(t, runtime, "fileState"); state != want {
		t.Fatalf("fileState() = %q, want %q: %s", state, want, fixtureString(t, runtime, "failure"))
	}
	if !fixtureBoolean(t, runtime, "missingFileThrows") {
		t.Fatal("opening a missing file for reading did not throw")
	}

	// A second runtime over the same directory is what a later launch sees.
	next := startFixture(t, backend.NewDirectorySaveStore(filepath.Join(root, "SKVM Fixture")))
	if state := fixtureString(t, next, "fileState"); state != want {
		t.Fatalf("fileState() in a new session = %q, want the persisted file back", state)
	}
}

func TestSKVMMeshKeepsItsTransform(t *testing.T) {
	runtime := startFixture(t, nil)
	// translate(5,6,7) then scale(2,1,1) doubles row 0 including its offset,
	// and leaves row 1's scale at 1.0.
	const want = "cube|2000000000|10|1000000000"
	if state := fixtureString(t, runtime, "meshState"); state != want {
		t.Fatalf("meshState() = %q, want %q", state, want)
	}
	if !fixtureBoolean(t, runtime, "badTriangleThrows") {
		t.Fatal("a triangle naming a missing vertex did not throw")
	}
}

func TestSKVMTextFieldTruncatesToItsMaxSize(t *testing.T) {
	runtime := startFixture(t, nil)
	if state := fixtureString(t, runtime, "textFieldState"); state != "abcd|4|true" {
		t.Fatalf("textFieldState() = %q", state)
	}
}

// The vendor's input method is a handler a title reaches through a static and
// hands its own text component to. The component holds the text and takes the
// edits, so what this pins is a whole typed word arriving through an interface
// this runtime cannot read a character back out of.
func TestSKVMTextInputTypesThroughTheComponentInterface(t *testing.T) {
	runtime := startFixture(t, nil)
	// One handler; '2','2' cycles a→b; '3' inserts d; '#' deletes it; '*'
	// turns the mode to capitals, which is bit 1 and an 'A' from the next
	// press; a release is not the input method's. The platform's own field
	// starts from "ab" and takes a 'p' from key 7.
	const want = "true|true|false|1|bA|abp"
	if state := fixtureString(t, runtime, "textInputState"); state != want {
		t.Fatalf("textInputState() = %q, want %q: %s", state, want, fixtureString(t, runtime, "failure"))
	}
}

// A Timer runs its task on a thread of its own, which is why the fixture can
// wait for it on the thread that scheduled it. A task invoked inline would
// deadlock this test rather than fail it.
func TestSKVMTimerRunsATaskBesideTheCaller(t *testing.T) {
	runtime := startFixture(t, nil)
	const want = "true|true|true"
	if state := fixtureString(t, runtime, "timerState"); state != want {
		t.Fatalf("timerState() = %q, want %q: %s", state, want, fixtureString(t, runtime, "failure"))
	}
}

// The shape a handset was actually sent: a zip holding the JAR beside a .msd,
// with the JAR's own manifest naming no MIDlet. Reading it as a bare JAR finds
// no main class, so the descriptor has to come from the .msd.
func TestOpenReadsTheArchiveShapeAHandsetWasSent(t *testing.T) {
	const descriptor = "MIDlet-Name: 테스트게임\r\n" +
		"MIDlet-1: 테스트게임,23x23.bmp,SKVMMIDlet\r\n" +
		"DD-ProgName: 0056194389\r\n"
	archive, err := skt.Open(buildArchive(t, map[string][]byte{
		"0056194389.msd": []byte(descriptor),
		"0056194389.jar": stripManifestMIDlet(t, skvmJAR),
		"0056194389.wmr": []byte("icon"),
	}))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if archive.MainClass.Name != "SKVMMIDlet" {
		t.Fatalf("MainClass = %q, want SKVMMIDlet", archive.MainClass.Name)
	}
	// Saves are keyed by the program number the handset addressed the title
	// by, not by the EUC-KR name, which no decoder here agrees on.
	if owner := skt.SaveOwner(archive.Descriptor); owner != "0056194389" {
		t.Fatalf("SaveOwner() = %q, want the program number", owner)
	}
	// The game runs from there exactly as it does from a bare JAR.
	framebuffer, err := backend.NewMemoryFramebuffer(32, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skt.Start(archive, skt.Options{Framebuffer: framebuffer}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

// The three SKT titles here all call the device and audio surface before they
// draw anything, and each missing method stops the startup, so what the
// surface answers is the difference between a title running and not.
func TestDeviceAndAudioSurfaceAnswerTheStartupCalls(t *testing.T) {
	runtime := startFixture(t, nil)

	// The toggles report what was set. A title that turns the backlight off
	// and reads it back on has been told its setting did not take.
	invokeVoid(t, runtime, "com/skt/m/Device", "setBacklightEnabled", "(Z)V", jvm.IntValue(0))
	if invokeInt(t, runtime, "com/skt/m/Device", "isBacklightEnabled", "()Z") != 0 {
		t.Fatal("isBacklightEnabled() reported on after being turned off")
	}
	invokeVoid(t, runtime, "com/skt/m/Device", "setKeyToneEnabled", "(Z)V", jvm.IntValue(1))
	if invokeInt(t, runtime, "com/skt/m/Device", "isKeyToneEnabled", "()Z") != 1 {
		t.Fatal("isKeyToneEnabled() reported off after being turned on")
	}

	// Installing a wallpaper or a ringtone reaches a handset that is not
	// here, and both calls report whether it worked.
	name := jvm.ReferenceValue(runtime.VM.NewString("bell"))
	empty := jvm.ReferenceValue(nil)
	for _, method := range []string{"setSISImage", "setMelody"} {
		if invokeInt(t, runtime, "com/skt/m/Device", method, "(ILjava/lang/String;[B)Z", jvm.IntValue(0), name, empty) != 0 {
			t.Fatalf("%s() claimed an install that cannot have happened", method)
		}
	}

	// The volume calls come in a format-taking shape as well, which is the one
	// every local title uses, and both shapes reach the same volume.
	format := jvm.ReferenceValue(runtime.VM.NewString("audio/midi"))
	maximum := invokeInt(t, runtime, "com/skt/m/AudioSystem", "getMaxVolume", "(Ljava/lang/String;)I", format)
	if maximum <= 0 {
		t.Fatalf("getMaxVolume() = %d, want the scale setVolume clamps to", maximum)
	}
	invokeVoid(t, runtime, "com/skt/m/AudioSystem", "setVolume", "(Ljava/lang/String;I)V", format, jvm.IntValue(30))
	if volume := invokeInt(t, runtime, "com/skt/m/AudioSystem", "getVolume", "(Ljava/lang/String;)I", format); volume != 30 {
		t.Fatalf("getVolume(format) = %d, want 30", volume)
	}
	if volume := invokeInt(t, runtime, "com/skt/m/AudioSystem", "getVolume", "()I"); volume != 30 {
		t.Fatalf("getVolume() = %d, want the volume the format-taking call set", volume)
	}

	// A null format is the handset's NullPointerException, not a Host error.
	if _, err := runtime.VM.InvokeStatic("com/skt/m/AudioSystem", "getMaxVolume", "(Ljava/lang/String;)I", jvm.ReferenceValue(nil)); err == nil {
		t.Fatal("getMaxVolume(null) was accepted")
	}
}

func invokeVoid(t *testing.T, runtime *skt.Runtime, class, name, descriptor string, arguments ...jvm.Value) {
	t.Helper()
	if _, err := runtime.VM.InvokeStatic(class, name, descriptor, arguments...); err != nil {
		t.Fatalf("%s.%s%s error = %v", class, name, descriptor, err)
	}
}

func invokeInt(t *testing.T, runtime *skt.Runtime, class, name, descriptor string, arguments ...jvm.Value) int32 {
	t.Helper()
	result, err := runtime.VM.InvokeStatic(class, name, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s.%s%s error = %v", class, name, descriptor, err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestSKVMClassesComeFromThePlatformNotTheTitle(t *testing.T) {
	// com.skt.m is the platform's to supply. A machine built from the title's
	// own JAR alone must not resolve it — otherwise a title could ship its own
	// MathFP and quietly replace the one every other title measures against.
	archive, err := skt.Open(skvmJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	machine := jvm.New(archive, jvm.Options{})
	if _, err := machine.InvokeStatic("SKVMMIDlet", "fixedPointState", "()Ljava/lang/String;"); err == nil {
		t.Fatal("MathFP resolved without the SKVM library")
	}
}

func buildArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	for name, contents := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return buffer.Bytes()
}

// stripManifestMIDlet rewrites the fixture JAR the way a real SKT title is
// packaged: the manifest is there but names no MIDlet, because the identity
// lives in the .msd beside it.
func stripManifestMIDlet(t *testing.T, jar []byte) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(jar), int64(len(jar)))
	if err != nil {
		t.Fatal(err)
	}
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	for _, file := range reader.File {
		contents := []byte("Manifest-Version: 1.0\r\n")
		if file.Name != "META-INF/MANIFEST.MF" {
			opened, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			contents, err = io.ReadAll(opened)
			opened.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		entry, err := writer.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func startFixture(t *testing.T, store backend.SaveStore) *skt.Runtime {
	t.Helper()
	archive, err := skt.Open(skvmJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	framebuffer, err := backend.NewMemoryFramebuffer(32, 24)
	if err != nil {
		t.Fatalf("NewMemoryFramebuffer() error = %v", err)
	}
	runtime, err := skt.Start(archive, skt.Options{Framebuffer: framebuffer, SaveStore: store})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return runtime
}

func fixtureString(t *testing.T, runtime *skt.Runtime, method string) string {
	t.Helper()
	result, err := runtime.VM.InvokeStatic("SKVMMIDlet", method, "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("SKVMMIDlet.%s() error = %v", method, err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if object == nil {
		return ""
	}
	value, ok := object.Native.(string)
	if !ok {
		t.Fatalf("SKVMMIDlet.%s() did not return a String", method)
	}
	return value
}

func fixtureBoolean(t *testing.T, runtime *skt.Runtime, method string) bool {
	t.Helper()
	result, err := runtime.VM.InvokeStatic("SKVMMIDlet", method, "()Z")
	if err != nil {
		t.Fatalf("SKVMMIDlet.%s() error = %v", method, err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return value != 0
}

// An SKT container carries more than the JAR and its descriptor. A handset was
// sent one archive and unpacked it into the title's storage, so the bare-named
// files beside the JAR are the title's own filesystem — and a game opens them
// for writing without asking for creation, because on a handset they were
// already there. Discarding them turned one title's every save into an
// IOException at the open.
func TestArchiveKeepsTheFilesInstalledBesideTheJAR(t *testing.T) {
	const descriptor = "MIDlet-Name: 테스트게임\r\n" +
		"MIDlet-1: 테스트게임,23x23.bmp,SKVMMIDlet\r\n" +
		"DD-ProgName: 0056194389\r\n"
	archive, err := skt.Open(buildArchive(t, map[string][]byte{
		"0056194389.msd": []byte(descriptor),
		"0056194389.jar": stripManifestMIDlet(t, skvmJAR),
		"0056194389.wmr": []byte("icon"),
		"0056194389.mod": []byte("module"),
		"c":              []byte("config"),
		"dnlist2":        []byte("list"),
	}))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	for name, want := range map[string]string{"c": "config", "dnlist2": "list"} {
		data, ok := archive.Resource(name)
		if !ok {
			t.Errorf("installed file %q is not there", name)
			continue
		}
		if string(data) != want {
			t.Errorf("installed file %q = %q, want %q", name, data, want)
		}
	}
	// The descriptor, the module and the icon are the platform's, not the
	// title's, and a game never opens them by name.
	for _, name := range []string{"0056194389.msd", "0056194389.wmr", "0056194389.mod", "0056194389.jar"} {
		if _, ok := archive.Resource(name); ok {
			t.Errorf("%q was kept as a title file", name)
		}
	}
}

// The diagnostic report is what answers "what did this title actually use"
// without folding a million lines of instruction log by hand. Two things have
// to hold for it to be worth reading: a native that ran is counted, and one
// that never ran is still listed with a zero — the zeros are the point, since
// they are the surface nothing here proves is needed.
func TestDiagnosticsCountsWhatATitleUsed(t *testing.T) {
	runtime := startFixture(t, nil)
	before := runtime.Diagnostics()
	if len(before.Natives) == 0 {
		t.Fatal("no natives are registered")
	}
	if len(before.Classes) == 0 {
		t.Fatal("no classes are recorded as loaded")
	}
	// The root class is answered without a class file, so a lookup that
	// reached the loader for it is not a gap to report.
	if reason, ok := before.Missing["java/lang/Object"]; ok {
		t.Errorf("the root class is reported missing: %s", reason)
	}

	const counted = "com/xce/io/XFile.write([BII)I"
	if _, registered := before.Natives[counted]; !registered {
		t.Fatalf("%s is not registered", counted)
	}
	if _, err := runtime.VM.InvokeStatic("SKVMMIDlet", "fileState", "()Ljava/lang/String;"); err != nil {
		t.Fatalf("fileState() error = %v", err)
	}
	after := runtime.Diagnostics()
	if after.Natives[counted] <= before.Natives[counted] {
		t.Errorf("%s counted %d calls, want more than %d",
			counted, after.Natives[counted], before.Natives[counted])
	}

	// A surface the fixture never touches keeps its zero rather than falling
	// out of the report.
	zeros := 0
	for _, calls := range after.Natives {
		if calls == 0 {
			zeros++
		}
	}
	if zeros == 0 {
		t.Error("every registered native reports a call, so the report cannot name unused surface")
	}
	if text := after.FormatCounts(5); !strings.Contains(text, "natives called of") {
		t.Errorf("FormatCounts() = %q", text)
	}
}

// The other two platforms had to learn to see through a wrapping folder,
// because their loaders look their marker up by exact name. This one never
// needed to: it finds the descriptor by extension wherever it is, derives the
// JAR from the descriptor's own name so the two move together, and files the
// installed files by base name. That is worth a test rather than an assertion,
// because it is the reason the unwrap deliberately stops short of here.
func TestOpenReadsAnArchiveInsideAFolder(t *testing.T) {
	const descriptor = "MIDlet-Name: 테스트게임\r\n" +
		"MIDlet-1: 테스트게임,23x23.bmp,SKVMMIDlet\r\n" +
		"DD-ProgName: 0056194389\r\n"
	archive, err := skt.Open(buildArchive(t, map[string][]byte{
		"내 게임/0056194389.msd": []byte(descriptor),
		"내 게임/0056194389.jar": stripManifestMIDlet(t, skvmJAR),
		"내 게임/0056194389.wmr": []byte("icon"),
	}))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if archive.MainClass.Name != "SKVMMIDlet" {
		t.Fatalf("MainClass = %q, want SKVMMIDlet", archive.MainClass.Name)
	}
	if owner := skt.SaveOwner(archive.Descriptor); owner != "0056194389" {
		t.Fatalf("SaveOwner() = %q, want the program number", owner)
	}
}

// The audio timeline is part of the runtime, not part of having a speaker. A
// run with no Host sink used to have no timeline at all, so nothing decoded and
// no clip had a length — and a title whose music thread waits on `play` spun
// instead, at a rate no frame count would show. Opening a clip has to decode
// and answer a length whether or not anyone is listening, and `play` from the
// Host's own pass has to come straight back.
func TestAudioClipsDecodeAndPlayWithoutASpeaker(t *testing.T) {
	runtime := startFixture(t, nil)

	format := jvm.ReferenceValue(runtime.VM.NewString("audio/midi"))
	result, err := runtime.VM.InvokeStatic("com/skt/m/AudioSystem", "getAudioClip",
		"(Ljava/lang/String;)Lcom/skt/m/AudioClip;", format)
	if err != nil {
		t.Fatalf("getAudioClip() error = %v", err)
	}
	clip, err := result.Reference()
	if err != nil || clip == nil {
		t.Fatalf("getAudioClip() = %v, %v; want a clip", clip, err)
	}

	sound := oneNoteSMAF()
	if _, err := runtime.VM.InvokeVirtual(clip, "open", "([BII)V",
		jvm.ReferenceValue(jvm.NewByteArray(sound)), jvm.IntValue(0), jvm.IntValue(int32(len(sound)))); err != nil {
		t.Fatalf("open() error = %v", err)
	}

	// Decoding really happens with no sink attached, which is what makes the
	// length — and so the wait — real: data that is not a sound is refused
	// here rather than accepted and silently dropped.
	noise := []byte("this is not a sound")
	if _, err := runtime.VM.InvokeVirtual(clip, "open", "([BII)V",
		jvm.ReferenceValue(jvm.NewByteArray(noise)), jvm.IntValue(0), jvm.IntValue(int32(len(noise)))); err == nil {
		t.Fatal("open() accepted data that is not a sound, so nothing decoded")
	}

	// This is the Host's pass, so play returns rather than waiting the clip
	// out; a wait here would stop the screen and the timers with it.
	started := time.Now()
	if _, err := runtime.VM.InvokeVirtual(clip, "play", "()V"); err != nil {
		t.Fatalf("play() error = %v", err)
	}
	if took := time.Since(started); took > time.Second {
		t.Fatalf("play() on the Host pass took %v, want it to return at once", took)
	}
	if _, err := runtime.VM.InvokeVirtual(clip, "stop", "()V"); err != nil {
		t.Fatalf("stop() error = %v", err)
	}
}

// A sound has to reach the sink. The instant a clip starts from and the reading
// the timeline is advanced to are two answers that have to be the same clock,
// and they were not: the start was an absolute wall-clock stamp, decades ahead
// of the elapsed time a Host advances with, so no event was ever due and this
// platform never emitted a note through a sink on either Host.
func TestASoundStartedReachesTheSink(t *testing.T) {
	runtime := startFixture(t, nil)
	sink := backend.NewRecordingSink(nil)
	runtime.AttachAudioSink(sink)

	format := jvm.ReferenceValue(runtime.VM.NewString("audio/midi"))
	result, err := runtime.VM.InvokeStatic("com/skt/m/AudioSystem", "getAudioClip",
		"(Ljava/lang/String;)Lcom/skt/m/AudioClip;", format)
	if err != nil {
		t.Fatalf("getAudioClip() error = %v", err)
	}
	clip, err := result.Reference()
	if err != nil || clip == nil {
		t.Fatalf("getAudioClip() = %v, %v; want a clip", clip, err)
	}
	sound := oneNoteSMAF()
	if _, err := runtime.VM.InvokeVirtual(clip, "open", "([BII)V",
		jvm.ReferenceValue(jvm.NewByteArray(sound)), jvm.IntValue(0), jvm.IntValue(int32(len(sound)))); err != nil {
		t.Fatalf("open() error = %v", err)
	}
	if _, err := runtime.VM.InvokeVirtual(clip, "play", "()V"); err != nil {
		t.Fatalf("play() error = %v", err)
	}

	// The note is a few milliseconds in; a Host pass or two is enough for it
	// to fall due, and the runtime's own clock is what decides.
	deadline := time.Now().Add(2 * time.Second)
	for {
		runtime.AdvanceAudio()
		if messages, _ := sink.Summary(); messages > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a clip that was opened and played reached the sink with nothing")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A minimal SMAF file, the shape internal/backend's own tests use: one note on
// a four-millisecond timebase, which is enough for the decoder to answer a
// length.
func oneNoteSMAF() []byte {
	sequence := []byte{
		0x05, 0x90, 60, 100, 0x05,
		0x00, 0xff, 0x2f, 0x00,
	}
	track := append([]byte{2, 0, 2, 2}, make([]byte, 16)...)
	track = append(track, smafChunk("Mtsq", sequence)...)
	body := smafChunk("MTR\x00", track)
	file := make([]byte, 8)
	copy(file, "MMMD")
	length := uint32(len(body) + 2)
	file[4], file[5], file[6], file[7] = byte(length>>24), byte(length>>16), byte(length>>8), byte(length)
	return append(append(file, body...), 0, 0)
}

func smafChunk(tag string, payload []byte) []byte {
	header := make([]byte, 8)
	copy(header, tag)
	length := uint32(len(payload))
	header[4], header[5], header[6], header[7] = byte(length>>24), byte(length>>16), byte(length>>8), byte(length)
	return append(header, payload...)
}

package mobile_test

import (
	"regexp"
	"strings"
	"testing"
)

// A recreated Android activity must find the server and the game in it still
// running: the process belongs to the class, and only an activity that is not
// changing configuration ends it. The configuration changes a split screen, a
// pop-up view, an attached keyboard or controller and a display size change
// make are the activity's own, so they rebuild nothing.
func TestAndroidKeepsTheServerAcrossARecreation(t *testing.T) {
	activity := mobileSource(t, "android/src/com/movingwoo/wfeature/MainActivity.java")
	if !strings.Contains(activity, "private static Process server;") {
		t.Error("the server process belongs to one activity instance, so a recreation loses it")
	}
	start := strings.Index(activity, "protected void onDestroy()")
	if start < 0 {
		t.Fatal("no onDestroy")
	}
	onDestroy := activity[start:]
	check := strings.Index(onDestroy, "isChangingConfigurations()")
	destroy := strings.Index(onDestroy, "server.destroy()")
	if check < 0 || destroy < 0 || check > destroy {
		t.Error("onDestroy ends the server without asking whether the activity is only being recreated")
	}

	manifest := mobileSource(t, "android/AndroidManifest.xml")
	match := regexp.MustCompile(`android:configChanges="([^"]*)"`).FindStringSubmatch(manifest)
	if match == nil {
		t.Fatal("the activity declares no configChanges")
	}
	declared := map[string]bool{}
	for _, change := range strings.Split(match[1], "|") {
		declared[change] = true
	}
	for _, change := range []string{"orientation", "screenSize", "smallestScreenSize", "screenLayout", "keyboard", "keyboardHidden", "navigation", "density", "uiMode"} {
		if !declared[change] {
			t.Errorf("configChanges does not take %s, so it rebuilds the activity in the middle of a game", change)
		}
	}
}

// Leaving the screen saves nothing on its own on iOS: the player saves in the
// game, and the app does not drain or close the game when its scene goes to
// the background (docs/mobile.md, "When the app leaves the screen").
func TestIOSDoesNotSaveWhenItLeavesTheScreen(t *testing.T) {
	app := mobileSource(t, "ios/src/App.swift")
	for _, unwanted := range []string{"beginBackgroundTask(", "sceneDidEnterBackground"} {
		if strings.Contains(app, unwanted) {
			t.Errorf("App.swift has %s, so leaving the screen does something to the game", unwanted)
		}
	}
}

// The activity's web view goes with it, whether the activity is rebuilt or
// finished: a rebuilt activity shows the page in a web view of its own, and
// the old one left loaded could take the game back from the new page.
func TestAndroidDestroysTheWebViewWithItsActivity(t *testing.T) {
	activity := mobileSource(t, "android/src/com/movingwoo/wfeature/MainActivity.java")
	start := strings.Index(activity, "protected void onDestroy()")
	if start < 0 {
		t.Fatal("no onDestroy")
	}
	onDestroy := activity[start:]
	if close, check := strings.Index(onDestroy, "closePage();"), strings.Index(onDestroy, "isChangingConfigurations()"); close < 0 || close > check {
		t.Error("onDestroy does not close the page before it asks whether the activity is only being recreated")
	}
	page := strings.Index(activity, "private void closePage()")
	if page < 0 {
		t.Fatal("no closePage")
	}
	closePage := activity[page:]
	if remove, destroy := strings.Index(closePage, "removeView(webView)"), strings.Index(closePage, "webView.destroy()"); remove < 0 || destroy < 0 || remove > destroy {
		t.Error("closePage does not take the web view out of its parent and then destroy it")
	}
}

// An app finished and opened again within seconds must not start its server
// while the one it ended is still closing its games: the next activity waits
// for that server to exit, and its start-up wait takes an answer on the port
// only from a server this app started.
func TestAndroidWaitsForTheServerItEnded(t *testing.T) {
	activity := mobileSource(t, "android/src/com/movingwoo/wfeature/MainActivity.java")
	if !strings.Contains(activity, "private static Process ending;") {
		t.Fatal("nothing keeps the server a finished activity ended")
	}
	start := strings.Index(activity, "private void startServerAndShow()")
	if start < 0 {
		t.Fatal("no startServerAndShow")
	}
	show := activity[start:]
	if wait, launch := strings.Index(show, "previous.waitFor()"), strings.Index(show, "launch(port, root)"); wait < 0 || launch < 0 {
		t.Error("the next activity launches a server without waiting for the one still draining")
	}
	destroy := activity[strings.Index(activity, "protected void onDestroy()"):]
	if !strings.Contains(destroy, "ending = server;") {
		t.Error("onDestroy does not hand the ended server to the next activity to wait for")
	}
	serving := strings.Index(activity, "private boolean waitUntilServing(")
	if serving < 0 || !strings.Contains(activity[serving:], "startedHere(") {
		t.Error("the start-up wait accepts any answer on the port")
	}
	if here := strings.Index(activity, "private static boolean startedHere("); here < 0 ||
		!strings.Contains(activity[here:], "/proc/") || !strings.Contains(activity[here:], "android.os.Process.myPid()") {
		t.Error("startedHere does not ask the kernel whose child the answering server is")
	}
}

// A save backup is a type of its own on iOS, declared by its extension and
// with no MIME type, which would narrow the page's file picker to that type.
func TestIOSDeclaresTheSaveBackupType(t *testing.T) {
	plist := mobileSource(t, "ios/Info.plist")
	start := strings.Index(plist, "<key>UTExportedTypeDeclarations</key>")
	if start < 0 {
		t.Fatal("Info.plist exports no type, so a save backup has none")
	}
	declaration := plist[start:]
	declaration = declaration[:strings.Index(declaration, "</array>\n    <")+len("</array>")]
	for _, want := range []string{"<string>public.data</string>", "<key>public.filename-extension</key>", "<string>wfs</string>"} {
		if !strings.Contains(declaration, want) {
			t.Errorf("the save backup type lacks %s", want)
		}
	}
	if strings.Contains(declaration, "public.mime-type") {
		t.Error("the save backup type has a MIME type, which narrows the page's picker to it")
	}
}

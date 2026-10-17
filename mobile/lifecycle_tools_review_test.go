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

# The phone builds

The emulator is not ported to Android or iOS. **The app is the server with a
web view in front of it** — the same Go server a desktop runs, on loopback,
drawing the same page a desktop browser loads.

The architecture was already arranged for this: the game runs on the server and
the page draws what it sends. With the server on the phone, "the network" is
`127.0.0.1`, and the problems of reaching a server over a network disappear:

| | Over a network | In the app |
|---|---|---|
| Finding the address | `ipconfig`, four numbers, a port | none |
| Secure context | `http://192.168.x.x` is not one, so **no service worker** | `127.0.0.1` is one |
| Reaching it from outside | a router, a tunnel, or a name | none needed |
| Who else can reach it | anyone on the Wi-Fi | nobody |

## What runs where

```text
Android                              iOS
┌──────────────────────────┐         ┌──────────────────────────┐
│ MainActivity (Java)      │         │ App.swift (UIKit)        │
│   WebView ──► 127.0.0.1  │         │   WKWebView ──► 127.0.0.1│
│   exec ──► libwfeature.so│         │   call ──► libwfeature.a │
└──────────────────────────┘         └──────────────────────────┘
```

**The difference is imposed by the platforms.** Android lets an app run a file
from its extracted library directory, so the phone runs the same server binary
a desktop does, as a child process. **iOS does not let an app start a process**,
so there the Go code is compiled to a static archive (`-buildmode=c-archive`,
the one place cgo is used), linked into the app and called through three
exports — `WfeatureStart`, `WfeatureStop` and `WfeatureLastError` in
`mobile/ios/lib`. `internal/appserver` is the server without a `main`: no
flags, no signal handler, no exit, because those belong to the embedding app.

Neither app is a port of the page. The keypad, touch handling, audio
synthesiser, cheat panel and save API are the same files a desktop serves,
carried inside the binary.

Both servers bind loopback only, so the phone's games and saves are not offered
to the Wi-Fi. Both apps keep the screen on while they run.

| | Android | iOS |
|---|---|---|
| Data root | the app's external files directory (`getExternalFilesDir`), or its private one when there is none | the app's Documents directory |
| Server | `libwfeature.so` started with `-addr 127.0.0.1:11541`, `-games`, `-ext`, `-saves`, `-logs` under the root | `appserver.Start` with the root and port 11541 |
| Layout under the root | `games/`, `ext/`, `savedata/ktf/` (other platforms beside it), `logs/` | the same |
| Save export | the page calls a JavaScript bridge, which opens the system's create-document picker | the page posts a message, which opens a document picker |

The phone layout has no profile segment under `savedata/`: an app is always a
release build.

Large WebView audio buffers need scheduling ahead of the render clock to keep
short notes distinct. See [APK audio timing](history/audio.md#apk-audio-timing)
for the 0.5.0 report, the reproduced timing and envelope defects, the repair,
and the limits of physical-device validation.

Both apps serve the page from `http://127.0.0.1:11541` on every launch. The
port is part of the page's origin, so keeping it stable lets web view storage
retain volume, vibration, speed and keypad settings across a cold restart.
`mobile/origin_test.go` holds both apps to that port. If another process owns
it, the app reports a startup failure rather than moving to a new origin and
appearing to lose its settings. Games and in-game saves remain files in the
app's data directory. Settings left under the random ports older builds used
cannot cross the origin boundary, so the first launch after that change started
from the defaults once.

## When the app leaves the screen

**Saving is the player's to do.** A game's saves follow the common save rule
([architecture](architecture.md#one-save-rule)): what a title writes reaches the
store by the time it flushes or closes the file, and at the latest when the
game ends. Neither app saves anything because it left the screen: no background
drain, timer or lifecycle save. A player who leaves a game in the background
saves in the game first, as on a handset. What each app does make sure of is
that a game is not ended by something that is not an ending.

- **Android** runs the server as a child process. A configuration change the
  activity declares in `configChanges` — rotation, a density or display size
  change, night mode, a keyboard or navigation device — is taken without
  rebuilding the activity, so the page and its socket stay as they were. A
  change it does not declare (a font scale change, for one) rebuilds the
  activity in the same app process; the process handle belongs to the class,
  so the new activity finds the server and the game in it still running, and
  the new page resumes the game. Only an activity that is gone for good
  (`isChangingConfigurations()` false) ends the server, with SIGTERM, and the
  server's drain closes the game so its exit-time saves are written.
  `Process.destroy()` closes the server's output pipe right after the signal,
  so the server ignores SIGPIPE: a log line nobody reads fails instead of
  ending the drain. Before this, a density or font scale change ended the game
  and started a second server, and finishing the app lost a game's exit-time
  saves about half the time.

  Every activity destroys its own web view in `onDestroy`, rebuilt or
  finished, and leaves the server alone there. A rebuilt activity used to
  leave its page loaded and hidden behind the new one; hidden, it had parked
  its game and did nothing, but made visible again (a DevTools client did it)
  it took the game back from the new page. Now its socket closes, and the
  server parks the game, if the page had not, for the new page to resume.

  The app process outlives a finished activity, so the app can be opened
  again while the server it ended is still draining. The ended server is
  kept (`MainActivity.ending`), and the next activity starts a server only once
  that one has ended, saying 이전 실행을 마치는 중... meanwhile. The drain is
  what writes the games' exit-time saves, and the server bounds it (the
  bounds, and the deadline they make, are in
  [`session.md`](session.md#stopping-bounds)). An old process still there
  after the app's own wait, `ENDING_WAIT_SECONDS`, which a test holds past
  that deadline, will not end on its own, and the app ends it
  (`destroyForcibly`). Before, a parked game was closed ahead of the
  drain's deadline, so one whose `destroyApp` never returned kept the old
  server, and the app's 이전 실행을 마치는 중..., for ever. Before that, the new
  server started with the new activity and took the port the moment the old
  one stopped listening, while the old one could still be closing its games.
  The start-up wait then takes a `/api/status` answer only from a wfeature
  server whose parent process is the app (the answer's `pid`, its parent read
  from `/proc`), so neither a server still draining nor another program
  listening on the port passes for the new one.

  Only the activity created last starts a server, and the server ends with
  the last activity left. Opened again before Android has delivered the
  finished activity's `onDestroy`, the app takes over the server still
  running, and the finished activity's late `onDestroy` leaves it alone
  instead of ending the game under the new page; a start still waiting for an
  older activity launches nothing. A launch that is not the launcher's own
  gets a second activity over the first in the default launch mode, and the
  two share the server: finishing the second leaves the server and its game to
  the first, whose page takes the game back — at once, or, when it came back
  before the second let go of the game, by offering it back as a second tab of
  a browser does. Before, the second's `onDestroy` ended the server, and the
  first one's page came back with nothing behind it. The first may have let
  its own start go when the second was made over it while both waited for a
  server still draining — the second then started the server, and the first
  stopped waiting — so when the second finishes, the first, shown again with
  no page, starts again and shows it. Without that it kept saying 이전 실행을
  마치는 중... over a live server and the game parked in it until its task was
  removed.

  BACK on a task started from the home screen does not finish the activity on
  Android 12 and later: Android moves the task to the back. The activity only
  stops, the page parks its game, and the server keeps running; it is in the
  app's cgroup, so when Android freezes the cached app (about 70 s after it
  stopped, on the emulator) the server freezes with it and answers nothing
  until the app is back. Nothing is saved on the way out; opened again, the
  same page resumes the game. Nothing here needed a change for it. Android
  ending the cached app's process to reclaim memory is not exercised; the
  server is the app's child and would end with it, without the drain, as on
  iOS below.
- **iOS** links the server into the app. It drains the server only in
  `applicationWillTerminate`, which iOS calls for an app it ends while running.
  iOS ends a suspended app without telling it, so a game it reclaims while the
  app is in the background ends without its exit-time saves, as a handset whose
  battery is pulled does; the app does not close the game on its own to
  prevent that, by decision, because the player saves.

## Building

Neither app is part of `make dist`: each needs a toolchain the desktop build
does not, and a machine with neither must still be able to build the desktop
archives.

```sh
make mobile VERSION=0.5.2          # both, into build/dist/ beside the archives
make mobile-android VERSION=0.5.2  # just the APK: wfeature-<version>-android-arm64.apk
make mobile-ios VERSION=0.5.2      # just the IPA: wfeature-<version>-ios-arm64.ipa
mobile/android/build.sh            # the script the target runs (output in mobile/android/build/)
mobile/ios/build.sh                # likewise (output in mobile/ios/build/)
```

`make mobile` also rewrites `build/dist/SHA256SUMS`. The make targets pass the
Makefile's `VERSION` (default: the last release); a script run directly without
`VERSION` stamps `dev`.

**A release builds both in CI**, as two jobs on two machines: the Android SDK is
on the Linux runner and Xcode on the macOS one, and no runner has both. They
were once built locally and uploaded afterwards, a step that was forgotten.
Nothing is published unless all seven files are there; see
[`running.md`](running.md#the-release-archives).

**There is no Gradle and no Xcode project.** Each app is one source file and
one linked artifact, which the platform's own tools build in a handful of
commands; a wrapper, a daemon and a plugin graph would be several hundred
megabytes of machinery around a hundred lines of source. The two `build.sh`
scripts are the whole build.

### Android

Needs an Android SDK (`ANDROID_HOME`, default
`/opt/homebrew/share/android-commandlinetools`) with build-tools and a
platform, and a JDK 17 or newer (`JAVA_HOME`, default
`/opt/homebrew/opt/openjdk@17`). Command-line tools are enough; Android Studio
is only wanted for the emulator and logcat. `BUILD_TOOLS` and `PLATFORM` pick a
directory other than the newest, `VERSION_CODE` overrides the version code, and
`KEYSTORE` names another signing key. The APK targets SDK 35 with a minimum of
26 (Android 8.0). It requests `INTERNET` and `VIBRATE`, and its network
security configuration allows cleartext to `127.0.0.1` and `localhost` only.

The server is bundled as `lib/arm64-v8a/libwfeature.so`, stored uncompressed.
**Since Android 10 an app may not execute a file it wrote into its data
directory**, and the extracted library directory is the one place execution is
allowed. The build is arm64 only, which every phone since about 2017 is.

The APK is signed with a debug key at `mobile/android/debug.keystore`, made on
first build. **It is not in the repository**: Android accepts an update only
when it carries the same signature as the installed copy, so whoever holds the
key can build an APK that installs over someone's and inherits its games and
saves. **Back it up**: a key that changes between builds makes every tester
uninstall before the next version.

The release workflow needs the same key, or every release would be signed with
a different one and no phone could update. It is the repository secret
`ANDROID_KEYSTORE_BASE64`, the keystore file and nothing else (the passwords
are the literal `android` in `build.sh`). A fork's pull request is not given the
secret, so it stays with whoever can push here.

```sh
base64 -i mobile/android/debug.keystore | gh secret set ANDROID_KEYSTORE_BASE64
```

The workflow **fails** when the secret is missing rather than letting
`build.sh` make a key. An APK signed with a new key installs on a fresh phone
and cannot update anyone's, which stays invisible until a tester reports it.

The `versionCode` is the repository's commit count, because Android refuses to
install an older one over a newer and a fixed number would make every build
look the same to a tester's phone.

### iOS

Needs Xcode, not just the command line tools, for the iPhoneOS SDK.

**The floor is iOS 14.0**, set by how the app gets onto a phone. TrollStore
installs an unsigned IPA permanently on 14.0 through 16.6.1, and a build asking
for 15.0 gave up the bottom of that range for nothing: the one API here that
needs something newer is guarded at its own version, and the phones below 15
are the old ones this emulator is for.

The IPA is **unsigned** (ad-hoc at most), which is the distribution model:
whoever wants it installs it with their own Apple ID through AltStore,
Sideloadly or a similar tool, which re-signs it on the way in. A signature made
here would be one no phone accepts. Real distribution is TestFlight, an Apple
Developer Program decision rather than a build one.

The bundle carries what Xcode would have stamped on it —
`CFBundleSupportedPlatforms`, the `DT*` build metadata, `UIRequiredDeviceCapabilities`
and the icon keys. A plist written by hand has none of that by default, and the
first one is not cosmetic: **a re-signing service reads it to decide the IPA is
an iOS app at all**, so an unsigned build without it can be refused before
anything looks at the signature. The build stamps the SDK it actually used.

**The window comes from a scene** (`PlaySceneDelegate`, with one scene declared
under `UIApplicationSceneManifest`). An app built with the iOS 27 SDK that makes
its window in the app delegate does not launch on iOS 27 at all: UIKit stops it
before anything is drawn with "UIScene life cycle is required for apps built
with this SDK". Releases up to 0.5.2 were built by the release runner's Xcode
with the 26.5 SDK and launch on iOS 27 only because UIKit still excuses an older
SDK; a local build with Xcode 27, or a runner that moves to it, stopped at
launch. Checked on 2026-10-15 in the iOS 27 and 26.5 simulators with a 27 SDK
build: without the scene the app stops at launch on iOS 27, and with it the app
starts on both, loads the page and runs a game.

**Only the app's own window plays.** A display the phone is connected to — a
cable, AirPlay — opens a scene of its own (`windowExternalDisplayNonInteractive`),
and the app delegate gives the play delegate to the `windowApplication` scene
only. Given it as well, the display's scene made a window with a second page,
which took the game from the phone's page as a second tab would, where the
display had mirrored the phone before the app had scenes; without a delegate the
scene shows nothing of its own and the display mirrors the screen. This has not
been seen on a display: the simulator connects one only from its menu, and the
change was checked by building for the simulator and the device.

`UIFileSharingEnabled` puts the games and saves in the Files app, so an archive
can arrive either from the page's own ＋ 게임 추가 button or by being dropped
into the folder. Android has no equivalent — see below.

**The save backup is a declared type.** `UTExportedTypeDeclarations` names
`com.movingwoo.wfeature.save-backup`, extension `wfs`, conforming to
`public.data`, so a backup the page exported is a type of its own rather than
the dynamic type iOS makes up for an extension nobody declares. It has no MIME
type on purpose. WebKit's file panel turns the 세이브 가져오기 input's
`accept=".wfs"` into the picker's types through each extension's MIME type,
and with none it offers every file (`public.item`). That much is read from the
iOS 26.5 simulator's WebKit: `-[WKFileUploadPanel presentWithParameters:resultListener:]`
maps an extension with `MIMETypeRegistry::mimeTypeForExtension`, which asks
`NSURLFileTypeMappings`, and the MIME type with `+[UTType typeWithMIMEType:]`.
So the picker did not grey a backup out before the declaration either, and
the declaration changes what iOS calls the file, not what the picker offers.
A MIME type would narrow the picker to this one type, and a file provider that
typed a backup some other way would then show it greyed out. There is no
`CFBundleDocumentTypes`, so Files offers no Open In: a backup comes in through
the page. Checked on 2026-10-17 by installing a simulator build, without
launching it, on iOS 26.5 and 27.0 simulators: `UTType(filenameExtension:
"wfs")` changed from `dyn.ah62d4rv4ge81s3xx` to the declared type, and
`NSURLFileTypeMappings` still maps `wfs` to no MIME type. The picker itself
has not been looked at on a phone or in a simulator.

The icons are resized from `web/icon-512.png` at build time.
`mobile/ios/acceptance/` holds a simulator-only controller driver
(`load.c`, `PadDriver.swift`) that `web/acceptance/ios-gamepad.mjs` links beside
the unchanged `App.swift`; the app's own build compiles neither file.

## Adding a game, and removing it

The page has a **＋ 게임 추가** button, which is `POST /api/games`, and a
**게임 삭제** button beside it, which is `DELETE /api/games`; see
[`running.md`](running.md#adding-a-game-from-the-page-and-taking-it-back-off).

On a desktop that is a convenience. On Android it is the only way in: **since
Android 11 a file manager cannot open the directory an app keeps its files in**,
so "put the archive in games/" names a place nobody can reach. The button is
what gives a phone build any games, which is why that feature came before the
app did.

The same restriction makes removal the page's job. Without a button, the only
way to clear a game on Android is to clear the app's data, which takes every
save with it. What the page added is what the page may delete: it lands in
`ext/` beside `games/`, the only directory the delete button reaches. On a
phone every game arrives that way, so `ext/` holds the games and `games/` is the
empty folder beside it, which matters when the data directory is read over USB.
**The server moves what an older build left loose in `games/` into `ext/` on its
first start**, because adding a game used to write there; it is the same step
on every host, and it moves only loose files, since a folder of archives is a
library someone assembled over USB deliberately. A removal does not touch the
saves: they are keyed by what is inside the archive, so adding the same file
again comes back to the same progress.

## Game controllers

The page reads controllers itself (see [`web/README.md`](../web/README.md)), and
each web view hands it one under its own condition:

- **Android** delivers a controller's buttons to the activity as key events
  first. One the activity does not consume gets a system *fallback* — B becomes
  BACK, A the centre of the direction pad — and BACK walks the page's history or
  leaves the app, which is not what a player pressing 취소 on a controller
  means. `MainActivity.dispatchKeyEvent` hands every controller button to the
  web view and then reports it handled, so the page still reads it and no
  fallback is made. Back stays on the phone's own gesture. Keys and motion go to
  the *focused* view, and a phone opened by a touch is in touch mode, where
  nothing is focused until something is touched; a controller's buttons do not
  end touch mode. The web view therefore takes focus when the page is shown and
  again whenever a controller is used without it (`takeControllers`). Before
  that, a run started without a touch did not see the controller until the
  screen was touched or the direction pad moved.
- **iOS** exposes controllers to a `WKWebView` only while it is the first
  responder. The app makes it so when the page loads and again after the save
  export picker closes, since a picker over the web view leaves the responder
  chain elsewhere.

On Android, vibration reaches the phone's motor only with the `VIBRATE`
permission. Without it the web view still answers `navigator.vibrate` with
`true` and nothing moves, which is how the app once offered a 📳 진동 switch
that did nothing. A controller's motor goes through the input system and needs
no permission.

## What is not answered yet

- **A physical controller in either app.** Android's web view exposes a
  controller in the standard layout, with its rumble, on an emulator with a
  virtual controller made from a real one's HID descriptor
  ([`testing.md`](testing.md#the-android-app-on-an-emulator)); that path goes
  through the same kernel driver and key layout a paired controller does, but
  not through a radio. There the web view lets the first press after a page load
  or a connection go by without showing the controller; the second press is the
  first the page sees, and a page cannot change that. The iOS app's half is
  checked in the simulator with a virtual controller
  ([`testing.md`](testing.md#the-ios-app-on-a-simulator)): without first
  responder no controller input reaches the page, and taking first responder
  back restores it. WebKit's source says its own file picker and a tap on the
  page give the role back too. A physical controller's pairing awaits a phone.
- **Performance on a real handset.** Only a desktop measurement exists: on an
  M1, KTF titles take about 15% of a core and the MIDP path about 77%, which is
  6.6x and 1.3x of headroom. Every KTF and LGT title should therefore hold on a
  recent phone; **an SKT title needs a fast one**, and a mid-range phone will
  not hold it at speed. The ratio does not change with the device — improving
  it means the MIDP path, not the hardware.
- **The emulator's own frames.** On a phone the server encodes lossless picture
  updates (WebP where the web view decodes it) and the web view decodes them, on
  the same processor. Over a network that trade spared a phone from emulating;
  in-process it buys nothing, and handing the frame over as raw pixels is the
  next thing to measure.
- **The lifecycle on a phone.** What each app does on a configuration change,
  on leaving the screen and on finishing is checked on an Android emulator and
  an iOS simulator (see [testing](testing.md#the-android-apps-lifecycle-on-an-emulator)
  and [the iOS check](testing.md#the-ios-app-on-a-simulator)); a phone has not
  run them, and a simulator does not end a suspended app the way a phone does.
- **How far each app has been played.** Both builds are verified as far as
  their contents: signed, correct architecture, the server and the page inside.
  The Android app has run KTF titles in an emulator and for users, which is how
  its audio timing defect was found
  ([APK audio timing](history/audio.md#apk-audio-timing)); play on iOS and
  physical-phone acceptance remain unverified.

### Release version and build number

The release workflow passes the tag version to both mobile builds. On iOS,
`CFBundleShortVersionString` and the embedded server's `/api/status` report that
version; an unstamped library reports `dev`. `CFBundleVersion` defaults to the
repository commit count, as Android's `versionCode` does. Both release checkouts
fetch full history. Set `BUILD_NUMBER` to a positive integer when an iOS rebuild
needs a distinct build number or the source has no Git history (whose fallback
is 1). This changes metadata only; app identity and save locations stay the same.

# The phone builds

The emulator is not ported to Android or iOS. **The app is the server with a
web view in front of it** — the same Go server a desktop runs, on loopback,
drawing the same page a desktop browser loads.

That is worth stating plainly because it is what the whole architecture was
already arranged for. The game runs on the server and the page draws what it
sends; when the server is on the phone, "the network" is `127.0.0.1` and every
problem that came with reaching a server over one disappears at the same time:

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
│ MainActivity (Java)      │         │ AppDelegate (Swift)      │
│   WebView ──► 127.0.0.1  │         │   WKWebView ──► 127.0.0.1│
│   exec ──► libwfeature.so│         │   call ──► libwfeature.a │
└──────────────────────────┘         └──────────────────────────┘
```

**The difference is not a preference.** Android lets an app run a file out of
its extracted library directory, so the phone runs the same binary a desktop
does, as a child process. **iOS does not let an app start a process at all**,
so there the Go code is compiled to a static archive, linked into the app, and
called — which is what `internal/appserver` and `mobile/ios/lib` exist for.
`appserver` is the server without a `main`: no flags, no signal handler, no
exit, because those belong to whatever is embedding it.

Neither app is a port of the page. The keypad, the touch handling, the audio
synthesiser, the cheat panel and the save API are the same files a desktop
serves, carried inside the binary.

## Building

Neither is part of `make dist`: each needs a toolchain the desktop build does
not, and a machine with neither should still be able to cut a release.

```sh
make mobile VERSION=0.4.0     # both, into build/dist/ beside the archives
mobile/android/build.sh       # just the APK
mobile/ios/build.sh           # just the IPA
```

**There is no Gradle and no Xcode project.** Each app is one source file and
one linked artifact, and the platform's own tools do that in a handful of
commands; a wrapper, a daemon and a plugin graph would be several hundred
megabytes of machinery around a hundred lines of source. The two `build.sh`
scripts are the whole build.

### Android

Needs an Android SDK (`ANDROID_HOME`) with build-tools and a platform, and a
JDK 17 or newer (`JAVA_HOME`). Command-line tools are enough — Android Studio
is only wanted for the emulator and logcat.

The server is bundled as `lib/arm64-v8a/libwfeature.so`. The name is not a
disguise for its own sake: **since Android 10 an app may not execute a file it
wrote into its data directory**, and the extracted library directory is the one
place execution is allowed. arm64 only, which every phone since about 2017 is.

The APK is signed with a debug key at `mobile/android/debug.keystore`, made on
first build. **It is not in the repository**, and that is the point: Android
accepts an update only when it carries the same signature as the copy already
installed, so whoever holds this key can build an APK that installs over
somebody's and inherits its games and saves. Keeping it out costs nothing —
one person cuts the releases — and keeping it in would hand that to anyone who
cloned the repository.

The `versionCode` is the repository's commit count, because Android refuses to
install an older one over a newer and a number that never moved would make
every build look like the same one to a tester's phone.

The other half of the same rule: **back it up.** A key that changed between
builds would refuse to update the installed app, and every tester would have to
uninstall before the next version.

### iOS

Needs Xcode, not just the command line tools, for the iPhoneOS SDK.

The IPA is **unsigned** (ad-hoc at most), and that is the distribution story
rather than an omission: whoever wants it puts it on their own phone with their
own Apple ID through AltStore, Sideloadly or the like, and those tools re-sign
it on the way in. A signature made here would be one no phone accepts. Real
distribution is TestFlight, which is an Apple Developer Program decision and
not a build one.

`UIFileSharingEnabled` puts the games and saves in the Files app, so an archive
can arrive either from the page's own ＋ 게임 추가 button or by being dropped
into the folder. Android has no equivalent — see below.

## Adding a game

The page has a **＋ 게임 추가** button, which is `POST /api/games`; see
[`running.md`](running.md).

On a desktop that is a convenience. On Android it is the only way in: **since
Android 11 a file manager cannot open the directory an app keeps its files in**,
so "put the archive in games/" names a place nobody can reach. The button is
what makes a phone build have any games at all, and it is the reason that
feature came before the app did.

## What is not answered yet

- **Performance on a real handset.** A desktop measurement is what there is:
  on an M1, KTF titles take about 15% of a core and the MIDP path about 77%,
  which is 6.6x and 1.3x of headroom. The first says every KTF and LGT title
  should hold on any recent phone; **the second says an SKT title needs a fast
  one**, and a mid-range phone will not hold it at speed. That ratio does not
  change with the device — improving it means the MIDP path, not the hardware.
- **The emulator's own frames.** On a phone the server encodes a PNG per frame
  and the web view decodes it, on the same processor. Over a network that trade
  bought a phone that did not have to emulate; in-process it buys nothing, and
  handing the frame over as raw pixels is the obvious next thing to measure.
- **Whether either app runs at all beyond a first launch.** Both builds are
  verified as far as their contents: signed, correct architecture, the server
  and the page inside. That is not the same as playing a game on a phone.

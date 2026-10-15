# mobile

The phone apps. Each is the same Go server with a web view in front of it — not
a port of the emulator — and each is one source file plus a build script.

```
mobile/android/   AndroidManifest.xml  src/  res/  build.sh   → an APK
mobile/ios/       Info.plist           src/  lib/  build.sh   → an IPA
mobile/ios/acceptance/                 the simulator controller driver; not in the IPA
mobile/origin_test.go                  both apps keep the page on port 11541
```

`mobile/ios/lib` is the Go side of the iOS app: iOS does not let an app start a
process, so the server is compiled into an archive the app links and calls
rather than a binary it runs. Android executes the same server binary a desktop
does, bundled as a native library.

Neither is built by `make dist`. `make mobile` builds both into `build/dist/`
beside the desktop archives and needs an Android SDK and Xcode;
`make mobile-android` and `make mobile-ios` are the halves, which is how the
release workflow builds them on the two machines that each have one toolchain.

**They live in this repository** because the APK and the IPA carry a server
built from the source above them, and a mismatched pair is a broken app rather
than a version skew. They ship on this project's tag or not at all.

What runs where, what each platform forbids, how a game gets onto a phone, and
what is still unmeasured are in [`docs/mobile.md`](../docs/mobile.md).

# mobile

The phone apps. Each is the same Go server with a web view in front of it —
not a port of the emulator — and each is one source file plus a build script.

```
mobile/android/   AndroidManifest.xml  src/  res/  build.sh   → an APK
mobile/ios/       Info.plist           src/  lib/  build.sh   → an IPA
```

`mobile/ios/lib` is the Go side of the iOS app: iOS does not let an app start a
process, so the server is compiled into an archive the app links and calls
rather than a binary it runs. Android needs no such thing — it executes the
same binary a desktop does.

Neither is built by `make dist`. `make mobile` builds both into `build/dist/`
where the desktop archives land, and needs an Android SDK and Xcode to do it —
`make mobile-android` and `make mobile-ios` are the halves, which is how the
release workflow builds them on the two machines that have one toolchain each.

**Why these are here and not in a repository of their own:** the APK and the
IPA carry a server built from the source above them, and a mismatched pair is
not a version skew but a broken app. They ship on this project's tag or not at
all.

The whole story — what runs where, what each platform forbids, how a game gets
onto a phone, and what is still unmeasured — is in
[`docs/mobile.md`](../docs/mobile.md).

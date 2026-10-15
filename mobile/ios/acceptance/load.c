// Runs before main in the simulator build that web/acceptance/ios-gamepad.mjs
// makes, and hands over to the Swift driver beside it. The app's own build
// (mobile/ios/build.sh) compiles neither file.
extern void padcheck_start(void);

__attribute__((constructor)) static void padcheck_init(void) {
    padcheck_start();
}

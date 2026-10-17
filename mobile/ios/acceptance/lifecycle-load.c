// Runs before main in the simulator build that web/acceptance/ios-lifecycle.mjs
// makes, and hands over to the Swift driver beside it. The app's own build
// (mobile/ios/build.sh) compiles neither file.
extern void lifecheck_start(void);

__attribute__((constructor)) static void lifecheck_init(void) {
    lifecheck_start();
}

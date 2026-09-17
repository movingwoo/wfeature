public final class ReentryProbe {
    public static int calls;
    public static int initialized = ReentryBridge.through();

    public static synchronized int inner() {
        calls++;
        int result = 0;
        for (int i = 0; i < 10; i++) result += i;
        return result;
    }

    public static synchronized int outer() {
        return ReentryBridge.through();
    }

    public static int spend() {
        return ReentryBridge.through() + ReentryBridge.through()
            + ReentryBridge.through() + ReentryBridge.through()
            + ReentryBridge.through() + ReentryBridge.through();
    }
}

// Compile-time declaration only; the test installs an authored ARM body.
final class ReentryBridge {
    public static native int through();
}

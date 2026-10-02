public final class NativeCheckpointProbe {
    public static int before;
    public static int after;

    public static native int checkpoint();

    public static int run() {
        before++;
        int result = checkpoint();
        after++;
        return result + 1;
    }
}

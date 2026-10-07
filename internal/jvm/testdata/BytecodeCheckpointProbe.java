public final class BytecodeCheckpointProbe implements Runnable {
    public static int initialized = 7;
    public static int before;
    public static int after;
    public static int caught;
    public static int result;

    public static native long checkpoint(int stage, Object retained);

    public static int run(int mode) {
        before++;
        Object[] ring = new Object[2];
        ring[0] = ring;
        ring[1] = new int[] { 31 };
        return 5 + outer(ring, mode);
    }

    private static int outer(Object[] ring, int mode) {
        long wide = 0x123456789abcdefL;
        double fraction = -3.25;
        try {
            return 11 + inner(ring, mode, wide, fraction);
        } catch (IllegalStateException failure) {
            caught++;
            return 17;
        } finally {
            after++;
        }
    }

    private static int inner(Object[] ring, int mode, long wide, double fraction) {
        long first = checkpoint(1, ring);
        if (ring[0] != ring || wide != 0x123456789abcdefL || fraction != -3.25) return -100;
        ((int[]) ring[1])[0] += (int) first;
        if (mode == 1) throw new IllegalStateException("fixture continuation");
        long second = checkpoint(2, ring);
        return ((int[]) ring[1])[0] + (int) second + initialized;
    }

    public void run() {
        result = run(0);
    }
}

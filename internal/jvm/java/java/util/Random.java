package java.util;

public class Random {
    private long seed;

    public Random() {
        this(System.currentTimeMillis());
    }

    public Random(long seed) {
        setSeed(seed);
    }

    public void setSeed(long seed) {
        this.seed = (seed ^ 0x5deece66dL) & ((1L << 48) - 1);
    }

    protected int next(int bits) {
        seed = (seed * 0x5deece66dL + 11L) & ((1L << 48) - 1);
        return (int) (seed >>> (48 - bits));
    }

    public int nextInt() {
        return next(32);
    }

    public int nextInt(int bound) {
        if (bound <= 0) {
            throw new IllegalArgumentException();
        }
        if ((bound & -bound) == bound) {
            return (int) ((bound * (long) next(31)) >> 31);
        }
        int bits;
        int value;
        do {
            bits = next(31);
            value = bits % bound;
        } while (bits - value + (bound - 1) < 0);
        return value;
    }

    public long nextLong() {
        return ((long) next(32) << 32) + next(32);
    }
}

package com.skt.m;

/**
 * Fixed-point math with nine decimal places: 1.0 is 1_000_000_000. Every
 * value a game passes and receives is scaled that way, which is why the
 * constants below are the scaled forms rather than the mathematical ones.
 */
public final class MathFP {
    public static final long E = 2718281828L;
    public static final long PI = 3141592654L;
    public static final long MAX_VALUE = Long.MAX_VALUE;
    public static final long MIN_VALUE = Long.MIN_VALUE;

    private MathFP() {
    }

    public static native long abs(long value);
    public static native long acos(long value);
    public static native long add(long a, long b);
    public static native long asin(long value);
    public static native long atan(long value);
    public static native long cos(long value);
    public static native long divide(long a, long b);
    public static native long exp(long value);
    public static native long log(long value);
    public static native long max(long a, long b);
    public static native long min(long a, long b);
    public static native long multiply(long a, long b);
    public static native long parseFP(long value);
    public static native long parseFPString(String value);
    public static native long pow(long a, long b);
    public static native long round(long value);
    public static native long sin(long value);
    public static native long sqrt(long value);
    public static native long sub(long a, long b);
    public static native long tan(long value);
    public static native long toLong(long value);
    public static native String toStringE(long value);
    public static native String toStringLF(long value, int decimals);
}

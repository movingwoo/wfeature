package com.skt.m;

public class Vibration {
    private Vibration() {
    }

    public static native void start(int duration, int strength);
    public static native void stop();
}

package com.skt.m;

public class Device {
    public static final int NAI_MOBILE = 0;

    private Device() {
    }

    public static native void beep(int frequency, int duration);
    public static native void setNAI(int nai);
}

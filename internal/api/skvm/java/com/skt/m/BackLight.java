package com.skt.m;

public class BackLight {
    private BackLight() {
    }

    public static native void on(int duration);
    public static native void off();
    public static native int getColor();
    public static native void setColor(int color);
}

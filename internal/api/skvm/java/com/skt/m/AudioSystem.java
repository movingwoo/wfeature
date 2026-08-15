package com.skt.m;

public class AudioSystem {
    private AudioSystem() {
    }

    public static native AudioClip getAudioClip(String type) throws UnsupportedFormatException;
    public static native int getVolume();
    public static native void setVolume(int volume);
}

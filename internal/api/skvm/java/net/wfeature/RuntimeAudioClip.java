package net.wfeature;

import com.skt.m.AudioClip;
import com.skt.m.UnsupportedFormatException;

/**
 * The runtime's AudioClip. SKT handsets shipped a concrete implementation
 * behind AudioSystem.getAudioClip; this is it, playing through the same Host
 * audio timeline every other sound in this runtime uses.
 */
public class RuntimeAudioClip implements AudioClip {
    private int handle;
    private String type;

    public RuntimeAudioClip(String type) {
        this.type = type;
    }

    public native void open(byte[] data, int offset, int length) throws UnsupportedFormatException;
    public native void close();
    public native void play();
    public native void loop();
    public native void pause();
    public native void resume();
    public native void stop();
}

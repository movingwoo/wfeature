package com.skt.m;

/**
 * SKVM's sound interface. A game either implements it or uses the runtime's
 * RuntimeAudioClip, which stands in for the implementation SKT handsets
 * shipped.
 */
public interface AudioClip {
    void open(byte[] data, int offset, int length) throws UnsupportedFormatException;
    void close();
    void play();
    void loop();
    void pause();
    void resume();
    void stop();
}

package com.skt.m;

import javax.microedition.lcdui.Graphics;
import javax.microedition.lcdui.Image;

/**
 * SKT's sprite container format. This runtime reads the frame and object
 * tables and hands each entry back as an ordinary Image; see docs/skvm.md for
 * what is and is not decoded.
 */
public class SISImage {
    public SISImage() {
    }

    public native void createBuffer(int width, int height);
    public native void freeBuffer();
    public static native int getRequiredBufferSize(byte[] data, int offset, int length);

    public native int getBestID();
    public native int getDelay(int frameId);
    public native Image getFrame(int frameId);
    public native Image getObject(int objectId, boolean transparent);
    public native int getWidth();
    public native int getHeight();
    public native int getImageLevel();
    public native int getMaxFrameID();
    public native int getMaxObjectID();
    public native void paintFrame(Graphics graphics, int frameId, int x, int y);
    public native void paintObject(Graphics graphics, int objectId, int x, int y, boolean transparent);
}

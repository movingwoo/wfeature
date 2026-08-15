package com.skt.m;

import javax.microedition.lcdui.Graphics;
import javax.microedition.lcdui.Image;

/**
 * SKVM's pixel-level drawing surface. It wraps a MIDP Graphics and adds the
 * per-pixel and capture operations MIDP itself does not have.
 */
public class Graphics2D {
    public static final int SRC_COPY = 0;
    public static final int SRC_AND = 1;
    public static final int SRC_OR = 2;
    public static final int SRC_XOR = 3;

    public Graphics2D(Graphics graphics) {
        init(graphics);
    }

    private native void init(Graphics graphics);

    public native int getPixel(int x, int y);
    public native void setPixel(int x, int y, int color);
    public native boolean getPixelMask(int x, int y);
    public native void setPixelMask(int x, int y, boolean mask);
    public native void invertRect(int x, int y, int width, int height);
    public native Image captureLCD(int x, int y, int width, int height);
    public native void drawImage(int x, int y, Image image, int sourceX, int sourceY, int width, int height, int mode);
}

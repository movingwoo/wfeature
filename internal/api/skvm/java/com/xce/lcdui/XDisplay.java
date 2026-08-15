package com.xce.lcdui;

/**
 * SKVM's direct display refresh. A game that draws outside a paint callback
 * calls this to push what it drew.
 */
public class XDisplay {
    /**
     * The screen a game sizes itself against. The runtime fills these in from
     * the framebuffer it was given; a game reads them directly rather than
     * asking a Canvas, and a zero here is a game that draws nothing.
     */
    public static int width;
    public static int height;
    public static int height2;

    private XDisplay() {
    }

    public static native void refresh(int x, int y, int width, int height);
}

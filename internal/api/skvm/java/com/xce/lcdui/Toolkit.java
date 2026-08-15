package com.xce.lcdui;

import javax.microedition.lcdui.Graphics;

public class Toolkit {
    public Toolkit() {
    }

    public static native void drawString(String text, int x, int y, int anchor);
    public static native int getScreenWidth();
    public static native int getScreenHeight();
}

package com.xce.lcdui;

import javax.microedition.lcdui.Graphics;

/**
 * A text field a game positions and paints itself. The runtime keeps the
 * text, the bounds and the focus, and paints the text; there is no on-screen
 * input method, so inputChar is the only way characters arrive.
 */
public class XTextField {
    public XTextField() {
        init();
    }

    private native void init();

    public native String getText();
    public native void setText(String text);
    public native int getMaxSize();
    public native void setMaxSize(int maxSize);
    public native boolean hasFocus();
    public native void setFocus(boolean focus);
    public native void setBounds(int x, int y, int width, int height);
    public native void inputChar(char character);
    public native void keyPressed(int keyCode);
    public native void keyReleased(int keyCode);
    public native void keyRepeated(int keyCode);
    public native void paint(Graphics graphics);
    public native void repaint();
    public native void repaint(int x, int y, int width, int height);
}

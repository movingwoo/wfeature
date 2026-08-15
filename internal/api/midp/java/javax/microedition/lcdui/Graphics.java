package javax.microedition.lcdui;

/**
 * Minimal runtime-owned drawing context for Canvas paint callbacks.
 */
public final class Graphics {
    public static final int HCENTER = 1;
    public static final int VCENTER = 2;
    public static final int LEFT = 4;
    public static final int RIGHT = 8;
    public static final int TOP = 16;
    public static final int BOTTOM = 32;
    public static final int BASELINE = 64;

    private Graphics() {
    }

    public native Font getFont();

    public native void setFont(Font font);

    public native void setColor(int rgb);

    public native void setColor(int red, int green, int blue);

    public native int getColor();

    public native void setClip(int x, int y, int width, int height);

    public native void clipRect(int x, int y, int width, int height);

    public native int getClipX();

    public native int getClipY();

    public native int getClipWidth();

    public native int getClipHeight();

    public native void translate(int x, int y);

    public native int getTranslateX();

    public native int getTranslateY();

    public native void fillRect(int x, int y, int width, int height);

    public native void drawLine(int x1, int y1, int x2, int y2);

    public native void drawRect(int x, int y, int width, int height);

    public native void drawImage(Image image, int x, int y, int anchor);

    public native void drawRegion(Image image, int sourceX, int sourceY,
        int width, int height, int transform, int x, int y, int anchor);

    public native void drawRGB(int[] rgbData, int offset, int scanlength,
        int x, int y, int width, int height, boolean processAlpha);

    public native void drawChar(char character, int x, int y, int anchor);

    public native void drawChars(char[] data, int offset, int length,
        int x, int y, int anchor);

    public native void drawString(String text, int x, int y, int anchor);

    public native void drawSubstring(String text, int offset, int length,
        int x, int y, int anchor);
}

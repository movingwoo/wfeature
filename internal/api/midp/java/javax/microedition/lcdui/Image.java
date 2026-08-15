package javax.microedition.lcdui;

import java.io.IOException;

/**
 * Runtime-owned MIDP image surface.
 */
public class Image {
    private Image() {
    }

    public static native Image createImage(int width, int height);

    public static native Image createImage(Image source);

    public static native Image createImage(Image source, int x, int y,
        int width, int height, int transform);

    public static native Image createImage(String name) throws IOException;

    public static native Image createImage(byte[] data, int offset, int length);

    public static native Image createRGBImage(int[] rgb, int width, int height,
        boolean processAlpha);

    public native Graphics getGraphics();

    public native int getWidth();

    public native int getHeight();

    public native boolean isMutable();

    public native void getRGB(int[] rgbData, int offset, int scanlength,
        int x, int y, int width, int height);
}

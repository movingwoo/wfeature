package javax.microedition.lcdui;

/**
 * Runtime-owned deterministic bitmap font and metrics.
 */
public final class Font {
    public static final int FACE_SYSTEM = 0;
    public static final int FACE_MONOSPACE = 32;
    public static final int FACE_PROPORTIONAL = 64;

    public static final int STYLE_PLAIN = 0;
    public static final int STYLE_BOLD = 1;
    public static final int STYLE_ITALIC = 2;
    public static final int STYLE_UNDERLINED = 4;

    public static final int SIZE_MEDIUM = 0;
    public static final int SIZE_SMALL = 8;
    public static final int SIZE_LARGE = 16;

    public static final int FONT_STATIC_TEXT = 0;
    public static final int FONT_INPUT_TEXT = 1;

    private Font() {
    }

    public static native Font getDefaultFont();

    public static native Font getFont(int fontSpecifier);

    public static native Font getFont(int face, int style, int size);

    public native int getFace();

    public native int getStyle();

    public native int getSize();

    public native boolean isPlain();

    public native boolean isBold();

    public native boolean isItalic();

    public native boolean isUnderlined();

    public native int getHeight();

    public native int getBaselinePosition();

    public native int charWidth(char character);

    public native int charsWidth(char[] characters, int offset, int length);

    public native int stringWidth(String text);

    public native int substringWidth(String text, int offset, int length);
}

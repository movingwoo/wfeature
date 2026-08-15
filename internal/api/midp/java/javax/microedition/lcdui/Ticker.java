package javax.microedition.lcdui;

/**
 * The scrolling string a Screen shows above its content. The runtime draws it
 * in place without animating it; see docs/lcdui.md.
 */
public class Ticker {
    public Ticker(String string) {
        init(string);
    }

    private native void init(String string);

    public native String getString();

    public native void setString(String string);
}

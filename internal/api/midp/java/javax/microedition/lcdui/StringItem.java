package javax.microedition.lcdui;

public class StringItem extends Item {
    public StringItem(String label, String text) {
        this(label, text, PLAIN);
    }

    public StringItem(String label, String text, int appearanceMode) {
        setLabel(label);
        initText(text, appearanceMode);
    }

    private native void initText(String text, int appearanceMode);

    public native String getText();

    public native void setText(String text);

    public native int getAppearanceMode();

    public native void setFont(Font font);

    public native Font getFont();
}

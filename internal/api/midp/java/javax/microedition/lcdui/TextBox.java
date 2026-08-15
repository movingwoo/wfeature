package javax.microedition.lcdui;

public class TextBox extends Screen {
    public TextBox(String title, String text, int maxSize, int constraints) {
        setTitle(title);
        initText(text, maxSize, constraints);
    }

    private native void initText(String text, int maxSize, int constraints);

    public native String getString();

    public native void setString(String text);

    public native int getChars(char[] data);

    public native void setChars(char[] data, int offset, int length);

    public native void insert(String src, int position);

    public native void delete(int offset, int length);

    public native int size();

    public native int getMaxSize();

    public native int setMaxSize(int maxSize);

    public native int getCaretPosition();

    public native void setConstraints(int constraints);

    public native int getConstraints();

    public void setInitialInputMode(String characterSubset) {
    }
}

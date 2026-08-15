package javax.microedition.lcdui;

public class TextField extends Item {
    public static final int ANY = 0;
    public static final int EMAILADDR = 1;
    public static final int NUMERIC = 2;
    public static final int PHONENUMBER = 3;
    public static final int URL = 4;
    public static final int DECIMAL = 5;

    public static final int PASSWORD = 0x10000;
    public static final int UNEDITABLE = 0x20000;
    public static final int SENSITIVE = 0x40000;
    public static final int NON_PREDICTIVE = 0x80000;
    public static final int INITIAL_CAPS_WORD = 0x100000;
    public static final int INITIAL_CAPS_SENTENCE = 0x200000;

    public static final int CONSTRAINT_MASK = 0xFFFF;

    public TextField(String label, String text, int maxSize, int constraints) {
        setLabel(label);
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

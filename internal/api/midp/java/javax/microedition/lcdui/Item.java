package javax.microedition.lcdui;

/**
 * Base of the components a Form holds. State lives on the runtime side so the
 * Host that draws the Form reads it directly.
 */
public abstract class Item {
    public static final int LAYOUT_DEFAULT = 0;
    public static final int LAYOUT_LEFT = 1;
    public static final int LAYOUT_RIGHT = 2;
    public static final int LAYOUT_CENTER = 3;
    public static final int LAYOUT_TOP = 0x10;
    public static final int LAYOUT_BOTTOM = 0x20;
    public static final int LAYOUT_VCENTER = 0x30;
    public static final int LAYOUT_NEWLINE_BEFORE = 0x100;
    public static final int LAYOUT_NEWLINE_AFTER = 0x200;
    public static final int LAYOUT_SHRINK = 0x400;
    public static final int LAYOUT_EXPAND = 0x800;
    public static final int LAYOUT_VSHRINK = 0x1000;
    public static final int LAYOUT_VEXPAND = 0x2000;
    public static final int LAYOUT_2 = 0x4000;

    public static final int PLAIN = 0;
    public static final int HYPERLINK = 1;
    public static final int BUTTON = 2;

    protected Item() {
        initItem();
    }

    native void initItem();

    public native String getLabel();

    public native void setLabel(String label);

    public native int getLayout();

    public native void setLayout(int layout);

    public native void addCommand(Command command);

    public native void removeCommand(Command command);

    public native void setDefaultCommand(Command command);

    public native void setItemCommandListener(ItemCommandListener listener);

    public native int getPreferredWidth();

    public native int getPreferredHeight();

    public native int getMinimumWidth();

    public native int getMinimumHeight();

    public void setPreferredSize(int width, int height) {
    }

    public native void notifyStateChanged();
}

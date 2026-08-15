package javax.microedition.lcdui;

/**
 * Runtime-owned base class for screens selected through Display. Commands and
 * the command listener live here because MIDP puts them on every Displayable,
 * not only on the high-level screens.
 */
public abstract class Displayable {
    protected Displayable() {
    }

    public native int getWidth();

    public native int getHeight();

    public native boolean isShown();

    public native void addCommand(Command command);

    public native void removeCommand(Command command);

    public native void setCommandListener(CommandListener listener);

    public native String getTitle();

    public native void setTitle(String title);

    public native Ticker getTicker();

    public native void setTicker(Ticker ticker);

    /**
     * Called when the screen's usable area changes. The runtime never resizes
     * a display, so nothing calls this; it exists because applications
     * override it.
     */
    protected void sizeChanged(int width, int height) {
    }
}

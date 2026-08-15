package javax.microedition.lcdui;

import javax.microedition.midlet.MIDlet;

/**
 * Minimal runtime-owned MIDP display manager. Rendering and device capability
 * methods are added as the host display boundary expands.
 */
public class Display {
    private Display() {
    }

    public static native Display getDisplay(MIDlet midlet);

    public native Displayable getCurrent();

    public native void setCurrent(Displayable nextDisplayable);

    public native void callSerially(Runnable runnable);
}

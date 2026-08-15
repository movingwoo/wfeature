package javax.microedition.midlet;

/**
 * Minimal runtime-owned MIDP lifecycle surface. Additional MIDP methods are
 * added as emulator services need them.
 */
public abstract class MIDlet {
    protected MIDlet() {
    }

    protected abstract void startApp() throws MIDletStateChangeException;

    protected abstract void pauseApp();

    protected abstract void destroyApp(boolean unconditional) throws MIDletStateChangeException;

    public final native String getAppProperty(String key);

    public final native void notifyDestroyed();

    public final native void notifyPaused();

    public final native void resumeRequest();
}

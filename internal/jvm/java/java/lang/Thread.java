package java.lang;

/** Minimal runtime-owned CLDC thread surface backed by Go goroutines. */
public class Thread implements Runnable {
    private Runnable target;

    public Thread() {
    }

    public Thread(Runnable target) {
        this.target = target;
    }

    public native void start();

    public native void interrupt();

    public native boolean isAlive();

    public void run() {
        if (target != null) {
            target.run();
        }
    }

    public static native void sleep(long milliseconds) throws InterruptedException;

    public static native void yield();
}

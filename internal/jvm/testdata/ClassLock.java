/**
 * A static synchronized method and a block on the class literal take one
 * lock, as the language has it. Notifying requires holding the lock notified
 * on, so each method here notifies through the other form of the same lock:
 * with two locks the notify throws IllegalMonitorStateException.
 */
public final class ClassLock {
    public static synchronized int notifyFromStaticMethod() {
        ClassLock.class.notifyAll();
        return 1;
    }

    public static int notifyFromBlock() {
        synchronized (ClassLock.class) {
            return notifyFromStaticMethod() + waitInStaticMethod();
        }
    }

    public static synchronized int waitInStaticMethod() {
        try {
            ClassLock.class.wait(1);
        } catch (InterruptedException interrupted) {
            return -1;
        }
        return 2;
    }
}

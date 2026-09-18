import javax.microedition.midlet.MIDlet;

public final class AsyncFailureMIDlet extends MIDlet {
    private static int result;
    protected void startApp() {}
    protected void pauseApp() {}
    protected void destroyApp(boolean unconditional) {}

    public static int joinWorker() throws InterruptedException {
        Thread worker = new Thread(new Runnable() {
            public void run() { result = 42; }
        });
        worker.start();
        worker.join();
        try {
            worker.start();
            return -1;
        } catch (IllegalThreadStateException expected) {
            return result;
        }
    }

    public static void startFailure() {
        new Thread(new Runnable() {
            public void run() {
                throw new RuntimeException("background failure");
            }
        }).start();
    }
}

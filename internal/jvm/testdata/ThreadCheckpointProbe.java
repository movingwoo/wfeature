public final class ThreadCheckpointProbe extends Thread {
    public int ticks;
    public long delay;
    public boolean stopped;

    public static native void entered(ThreadCheckpointProbe thread);

    public ThreadCheckpointProbe(long delay) {
        this.delay = delay;
    }

    public synchronized void run() {
        entered(this);
        loop();
    }

    private synchronized void loop() {
        while (!stopped) {
            ticks++;
            if (delay >= 0) {
                try {
                    Thread.sleep(delay);
                } catch (InterruptedException ignored) {}
            }
        }
    }
}

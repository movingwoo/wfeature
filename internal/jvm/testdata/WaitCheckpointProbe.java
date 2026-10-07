public final class WaitCheckpointProbe extends Thread {
    public int mode;
    public WaitCheckpointProbe target;
    public int before, after;
    public long nativeResult;
    public boolean interrupted;

    public WaitCheckpointProbe(int mode, WaitCheckpointProbe target) {
        this.mode = mode;
        this.target = target;
    }

    public void run() {
        before++;
        try {
            if (mode == 0) hold();
            else if (mode == 1) awaitSignal();
            else if (mode == 2) target.join();
            else if (mode == 3) synchronized (target) { target.after++; }
            else if (mode == 4) target.work();
            else if (mode == 5) target.signalAndHold();
            else if (mode == 6) nativeResult = target.nativeWork(0x123456789abcdefL, target);
        } catch (InterruptedException e) {
            interrupted = true;
        }
        after++;
    }

    private synchronized void hold() throws InterruptedException { Thread.sleep(60000); }
    private synchronized void work() { after++; }
    public synchronized native long nativeWork(long value, Object alias);
    private synchronized void awaitSignal() throws InterruptedException {
        synchronized (this) { wait(); }
    }
    public synchronized void signal() { notify(); }
    private synchronized void signalAndHold() throws InterruptedException {
        notify();
        Thread.sleep(60000);
    }
}

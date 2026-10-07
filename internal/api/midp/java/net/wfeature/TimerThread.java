package net.wfeature;

import java.util.Timer;
import java.util.TimerTask;

// Runtime-owned worker: locals and pending task calls belong to JVM frames.
public final class TimerThread extends Thread {
    private Timer timer;
    private TimerTask task;
    private long delay, period;
    private boolean fixedRate;

    private static native boolean stopped(Timer timer, TimerTask task);
    private static native void scheduled(TimerTask task, long due);
    private static native void failed(InterruptedException exception);

    public void run() {
        Timer timer = this.timer;
        TimerTask task = this.task;
        long delay = this.delay;
        long period = this.period;
        boolean fixedRate = this.fixedRate;
        long due = System.currentTimeMillis() + delay;
        try {
            while (true) {
                if (delay > 0) Thread.sleep(delay);
                if (stopped(timer, task)) return;
                scheduled(task, due);
                task.run();
                if (period == 0) return;
                long now = System.currentTimeMillis();
                if (fixedRate) {
                    due += period;
                    delay = due - now;
                    if (delay < 0) delay = 0;
                } else {
                    due = now + period;
                    delay = period;
                }
            }
        } catch (InterruptedException exception) {
            failed(exception);
        }
    }
}

// A thread started a second time, on the same Thread object, once its run()
// has returned. The language refuses that, and a VM built with
// RestartableThreads runs it again; a thread still running is refused either
// way.
public final class ThreadRestart implements Runnable {
    private static int runs;
    private static volatile boolean hold;

    public void run() {
        runs++;
        while (hold) {
            try {
                Thread.sleep(5);
            } catch (InterruptedException stopped) {
                return;
            }
        }
    }

    // Runs a thread to its end, then starts the same Thread again and leaves
    // that second run going until release(). The second start throws
    // IllegalThreadStateException unless the VM restarts finished threads.
    public static Thread startAgain() throws InterruptedException {
        runs = 0;
        hold = false;
        Thread thread = new Thread(new ThreadRestart());
        thread.start();
        thread.join();
        hold = true;
        thread.start();
        return thread;
    }

    // Starts a thread and, while it runs, starts it again. Answers -1 when
    // that second start is refused and 1 when it is not.
    public static int startWhileAlive() throws InterruptedException {
        runs = 0;
        hold = true;
        Thread thread = new Thread(new ThreadRestart());
        thread.start();
        int answer = 1;
        try {
            thread.start();
        } catch (IllegalThreadStateException refused) {
            answer = -1;
        }
        hold = false;
        thread.join();
        return answer;
    }

    public static void release() {
        hold = false;
    }

    public static int runs() {
        return runs;
    }
}

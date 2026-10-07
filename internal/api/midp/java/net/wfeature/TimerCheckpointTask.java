package net.wfeature;

import java.util.TimerTask;

// A task whose interrupted sleep must resume inside its existing invocation.
public final class TimerCheckpointTask extends TimerTask {
    public static int constructed;
    public int before, after;

    public TimerCheckpointTask() {
        constructed++;
    }

    public void run() {
        before++;
        try {
            Thread.sleep(60000);
        } catch (InterruptedException expected) {
        }
        after++;
    }
}

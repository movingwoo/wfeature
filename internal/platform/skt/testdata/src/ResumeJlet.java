import org.kwis.msp.lcdui.Jlet;

// A Jlet that counts its lifecycle callbacks. Its resumeApp is what undoes its
// pauseApp, as a title's frame timer restart does.
public class ResumeJlet extends Jlet {
    static int starts;
    static int pauses;
    static int resumes;
    static boolean paused;

    protected void startApp(String[] args) {
        starts++;
    }

    protected void pauseApp() {
        pauses++;
        paused = true;
    }

    protected void resumeApp() {
        resumes++;
        paused = false;
    }

    protected void destroyApp(boolean unconditional) {
    }

    public static int starts() {
        return starts;
    }

    public static int pauses() {
        return pauses;
    }

    public static int resumes() {
        return resumes;
    }

    public static boolean paused() {
        return paused;
    }
}

import javax.microedition.midlet.MIDlet;

// Authored MIDlet that declares a resumeApp of its own, which MIDP does not
// have and most SKT MIDlets do. It counts its lifecycle callbacks, and its
// resumeApp undoes what its pauseApp did.
public final class ResumingMIDlet extends MIDlet {
    private static int starts, pauses, resumes;
    private static boolean paused, failNextResume;

    protected void startApp() { starts++; }
    protected void pauseApp() { pauses++; paused = true; }
    protected void resumeApp() {
        resumes++;
        if (failNextResume) {
            failNextResume = false;
            throw new ArithmeticException("resume failed");
        }
        paused = false;
    }
    protected void destroyApp(boolean unconditional) {}
    public static int starts() { return starts; }
    public static int pauses() { return pauses; }
    public static int resumes() { return resumes; }
    public static boolean paused() { return paused; }
    public static void failNextResume() { failNextResume = true; }
}

import javax.microedition.midlet.MIDlet;

/** Authored regression fixture for a shared item/ability name cache. */
public final class TutorialCacheMIDlet extends MIDlet {
    public static final long LONG_CONSTANT = 1234567890123L;
    public static final double DOUBLE_CONSTANT = 1.25;
    public static final String TEXT_CONSTANT = "\u0000\u20ac";
    public String[][] names;
    public int phase;
    private static int result;

    protected void startApp() {
        names = new String[28][5];
        transition();
        if (names == null) {
            names = new String[1][24];
            names[0][9] = "ability";
        }
        result = names[0][9].length();
    }

    public void transition() {
        phase = 17;
        phase = 5;
    }

    public static int result() { return result; }
    protected void pauseApp() {}
    protected void destroyApp(boolean unconditional) {}
}

import javax.microedition.midlet.MIDlet;
import javax.microedition.midlet.MIDletStateChangeException;

public final class LifecycleMIDlet extends MIDlet {
    private static int state;
    private static String appProperty;
    private static boolean failStart;
    private static boolean failPause;
    private static boolean failDestroy;
    private static boolean requestResume;
    private static boolean deferStart;
    private static boolean refuseConditionalDestroy = true;
    private static boolean refuseForcedDestroy;

    public LifecycleMIDlet() {
        state = 1;
        appProperty = getAppProperty("fixture-property");
        failStart = getAppProperty("fixture-fail-start") != null;
        deferStart = getAppProperty("fixture-defer-start") != null;
    }

    protected void startApp() throws MIDletStateChangeException {
        if (failStart) {
            failStart = false;
            int zero = 0;
            state = 1 / zero;
        }
        if (deferStart) {
            deferStart = false;
            state = 7;
            throw new MIDletStateChangeException("retry start");
        }
        state = 2;
    }

    protected void pauseApp() {
        if (failPause) {
            failPause = false;
            int zero = 0;
            state = 1 / zero;
        }
        state = 3;
        if (requestResume) {
            requestResume = false;
            resumeRequest();
        }
    }

    protected void destroyApp(boolean unconditional) throws MIDletStateChangeException {
        if (failDestroy) {
            failDestroy = false;
            state = 8;
            int zero = 0;
            state = 1 / zero;
        }
        if ((!unconditional && refuseConditionalDestroy) || refuseForcedDestroy) {
            refuseConditionalDestroy = false;
            refuseForcedDestroy = false;
            state = 6;
            throw new MIDletStateChangeException("retry destroy");
        }
        state = unconditional ? 4 : 5;
    }

    public static int state() {
        return state;
    }

    public static String appProperty() {
        return appProperty;
    }

    public static void failNextPause() {
        failPause = true;
    }

    public static void failNextStart() {
        failStart = true;
    }

    public static void failNextDestroy() {
        failDestroy = true;
    }

    public static void requestResumeOnPause() {
        requestResume = true;
    }

    public static void deferNextStart() {
        deferStart = true;
    }

    public static void refuseForcedDestroy() {
        refuseForcedDestroy = true;
    }
}

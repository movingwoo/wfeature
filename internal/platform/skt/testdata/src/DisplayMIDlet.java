import javax.microedition.lcdui.Display;
import javax.microedition.lcdui.Displayable;
import javax.microedition.midlet.MIDlet;

public final class DisplayMIDlet extends MIDlet {
    private static Display display;
    private static Displayable firstScreen;
    private static Displayable secondScreen;
    private static boolean stableDisplay;
    private static boolean delayedSwitch;
    private static int serialRuns;
    private static int loopRuns;

    public DisplayMIDlet() {
        display = Display.getDisplay(this);
        stableDisplay = display == Display.getDisplay(this);
    }

    protected void startApp() {
        firstScreen = new FirstScreen();
        secondScreen = new SecondScreen();
        display.setCurrent(firstScreen);
        display.setCurrent(secondScreen);
        delayedSwitch = display.getCurrent() == null;
    }

    protected void pauseApp() {
    }

    protected void destroyApp(boolean unconditional) {
    }

    public static int displayState() {
        int state = 0;
        if (stableDisplay) {
            state |= 1;
        }
        if (delayedSwitch) {
            state |= 2;
        }
        if (display.getCurrent() == secondScreen) {
            state |= 4;
        }
        if (!firstScreen.isShown()) {
            state |= 8;
        }
        if (secondScreen.isShown()) {
            state |= 16;
        }
        return state;
    }

    public static int visibleScreen() {
        if (firstScreen.isShown()) {
            return 1;
        }
        if (secondScreen.isShown()) {
            return 2;
        }
        return 0;
    }

    public static int currentScreen() {
        if (display.getCurrent() == firstScreen) {
            return 1;
        }
        if (display.getCurrent() == secondScreen) {
            return 2;
        }
        return 0;
    }

    public static void requestFirstScreen() {
        display.setCurrent(firstScreen);
    }

    public static void requestNoScreen() {
        display.setCurrent(null);
    }

    public static void requestSerial() {
        display.callSerially(new Counter());
        display.callSerially(new Counter());
    }

    public static int serialRuns() {
        return serialRuns;
    }

    public static void requestSerialLoop() {
        display.callSerially(new Loop());
    }

    public static int loopRuns() {
        return loopRuns;
    }

    public static boolean nullSerialRejected() {
        try {
            display.callSerially(null);
            return false;
        } catch (NullPointerException expected) {
            return true;
        }
    }

    public static boolean nullDisplayRejected() {
        try {
            Display.getDisplay(null);
            return false;
        } catch (NullPointerException expected) {
            return true;
        }
    }

    /** Counts one run per Runnable handed to callSerially. */
    private static final class Counter implements Runnable {
        public void run() {
            serialRuns++;
        }
    }

    /** The frame loop shape: a Runnable that hands itself straight back. */
    private static final class Loop implements Runnable {
        public void run() {
            loopRuns++;
            display.callSerially(this);
        }
    }

    private static final class FirstScreen extends Displayable {
        FirstScreen() {
        }
    }

    private static final class SecondScreen extends Displayable {
        SecondScreen() {
        }
    }
}

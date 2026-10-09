import javax.microedition.midlet.MIDlet;
import org.kwis.msp.media.Clip;
import org.kwis.msp.media.Player;
import org.kwis.msp.media.PlayListener;

interface WIPIListenerExtension extends PlayListener { }

// Authored fixture for WIPI transition ownership and deferred listener delivery.
public final class WIPIListenerMIDlet extends MIDlet implements WIPIListenerExtension {
    public static final int HISTORY_LIMIT = 128;
    public static Clip clip;
    public static volatile boolean nativeCallActive;
    public static volatile boolean restartOnceOnEnd;
    public static volatile int synchronousCallbacks;

    private final int listenerId;
    private static final Object historyLock = new Object();
    private static final int[] codes = new int[HISTORY_LIMIT];
    private static final int[] parameters = new int[HISTORY_LIMIT];
    private static final int[] listeners = new int[HISTORY_LIMIT];
    private static final Clip[] owners = new Clip[HISTORY_LIMIT];
    private static int count;

    public WIPIListenerMIDlet() { this(1); }
    private WIPIListenerMIDlet(int id) { listenerId = id; }

    protected void startApp() { }
    protected void pauseApp() { }
    protected void destroyApp(boolean unconditional) { }

    public static void create(byte[] sound) {
        resetHistory();
        restartOnceOnEnd = false;
        nativeCallActive = true;
        try {
            clip = new Clip("mmf", sound);
            clip.setListener(new WIPIListenerMIDlet(1));
        } finally { nativeCallActive = false; }
    }

    public static boolean begin(byte[] sound, boolean repeat) {
        create(sound);
        return play(repeat);
    }

    public static boolean play(boolean repeat) {
        nativeCallActive = true;
        try { return Player.play(clip, repeat); }
        finally { nativeCallActive = false; }
    }

    public static boolean stop() {
        nativeCallActive = true;
        try { return Player.stop(clip); }
        finally { nativeCallActive = false; }
    }

    public static boolean pause() {
        nativeCallActive = true;
        try { return Player.pause(clip); }
        finally { nativeCallActive = false; }
    }

    public static boolean resume() {
        nativeCallActive = true;
        try { return Player.resume(clip); }
        finally { nativeCallActive = false; }
    }

    public static void setListener(int id) {
        if (id < 0 || id > 2) throw new RuntimeException("invalid listener id");
        nativeCallActive = true;
        try { clip.setListener(id == 0 ? null : new WIPIListenerMIDlet(id)); }
        finally { nativeCallActive = false; }
    }

    public static Clip getClip() { return clip; }
    public static void dropClip() { clip = null; }

    public static void resetHistory() {
        synchronized (historyLock) {
            count = 0;
            synchronousCallbacks = 0;
            for (int i = 0; i < HISTORY_LIMIT; i++) {
                codes[i] = 0;
                parameters[i] = 0;
                listeners[i] = 0;
                owners[i] = null;
            }
        }
    }

    public static int eventCount() {
        synchronized (historyLock) { return count; }
    }
    public static int eventCode(int index) {
        synchronized (historyLock) { checkIndex(index); return codes[index]; }
    }
    public static int eventParameter(int index) {
        synchronized (historyLock) { checkIndex(index); return parameters[index]; }
    }
    public static int eventListener(int index) {
        synchronized (historyLock) { checkIndex(index); return listeners[index]; }
    }
    public static Clip eventOwner(int index) {
        synchronized (historyLock) { checkIndex(index); return owners[index]; }
    }
    private static void checkIndex(int index) {
        if (index < 0 || index >= count) throw new RuntimeException("event index outside history");
    }

    public void playUpdate(Clip owner, int event, int parm) {
        boolean restart = false;
        synchronized (historyLock) {
            if (owner == null || (clip != null && owner != clip)) {
                throw new RuntimeException("listener clip identity changed");
            }
            if (event != ERROR && (event < END_OF_DATA || event > FULL_OF_DATA)) {
                throw new RuntimeException("unexpected play event");
            }
            if (parm != 0) throw new RuntimeException("unexpected play parameter");
            if (count == HISTORY_LIMIT) throw new RuntimeException("listener history is full");
            codes[count] = event;
            parameters[count] = parm;
            listeners[count] = listenerId;
            owners[count] = owner;
            count++;
            if (nativeCallActive) synchronousCallbacks++;
            if (event == END_OF_DATA && restartOnceOnEnd) {
                restartOnceOnEnd = false;
                restart = true;
            }
        }
        if (restart) {
            nativeCallActive = true;
            try {
                if (!Player.play(owner, false)) throw new RuntimeException("callback restart failed");
            } finally { nativeCallActive = false; }
        }
    }
}

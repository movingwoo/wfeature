import java.io.ByteArrayInputStream;
import javax.microedition.media.Control;
import javax.microedition.media.Manager;
import javax.microedition.media.Player;
import javax.microedition.media.PlayerListener;
import javax.microedition.media.control.VolumeControl;
import javax.microedition.midlet.MIDlet;

// Authored guest fixture for playback position, finite loops and listener delivery.
public final class MediaLifecycleMIDlet extends MIDlet implements PlayerListener {
    public static final int STARTED = 1;
    public static final int STOPPED = 2;
    public static final int END_OF_MEDIA = 3;
    public static final int CLOSED = 4;
    public static final int VOLUME_CHANGED = 5;
    public static final int DURATION_UPDATED = 6;
    public static final int HISTORY_LIMIT = 64;

    public static Player player;
    public static VolumeControl volume;
    public static volatile int count;
    public static volatile int synchronousCallbacks;
    public static volatile boolean nativeCallActive;
    public static volatile boolean restartOnceOnEnd;
    public static volatile boolean muteOnceOnVolumeChanged;
    private static String lastEvent;

    private static final Object historyLock = new Object();
    private static final int[] codes = new int[HISTORY_LIMIT];
    private static final long[] times = new long[HISTORY_LIMIT];
    private static final int[] levels = new int[HISTORY_LIMIT];
    private static final boolean[] muted = new boolean[HISTORY_LIMIT];

    protected void startApp() { }
    protected void pauseApp() { }
    protected void destroyApp(boolean unconditional) { }

    public static void begin(byte[] sound, int loops) throws Exception {
        resetHistory();
        restartOnceOnEnd = false;
        muteOnceOnVolumeChanged = false;
        nativeCallActive = true;
        try {
            player = Manager.createPlayer(new ByteArrayInputStream(sound), "application/vnd.smaf");
            player.realize();
            volume = (VolumeControl) player.getControl("VolumeControl");
            checkControl();
            player.setLoopCount(loops);
            player.addPlayerListener(new MediaLifecycleMIDlet());
            player.start();
        } finally {
            nativeCallActive = false;
        }
    }

    public static void start() throws Exception {
        nativeCallActive = true;
        try { player.start(); } finally { nativeCallActive = false; }
    }

    public static void stop() throws Exception {
        nativeCallActive = true;
        try { player.stop(); } finally { nativeCallActive = false; }
    }

    public static void deallocate() {
        nativeCallActive = true;
        try { player.deallocate(); } finally { nativeCallActive = false; }
    }

    public static void close() {
        nativeCallActive = true;
        try { player.close(); } finally { nativeCallActive = false; }
    }

    public static void setLoops(int loops) {
        nativeCallActive = true;
        try { player.setLoopCount(loops); } finally { nativeCallActive = false; }
    }

    public static long rewind() throws Exception {
        nativeCallActive = true;
        try { return player.setMediaTime(0); } finally { nativeCallActive = false; }
    }

    public static int state() { return player.getState(); }
    public static long mediaTime() { return player.getMediaTime(); }
    public static long duration() { return player.getDuration(); }
    public static Player getPlayer() { return player; }

    public static VolumeControl getControl() {
        checkControl();
        return volume;
    }

    public static void checkControl() {
        Control[] controls = player.getControls();
        if (volume == null || controls.length != 1 || controls[0] != volume ||
            player.getControl("VolumeControl") != volume ||
            player.getControl("javax.microedition.media.control.VolumeControl") != volume) {
            throw new RuntimeException("volume control identity changed");
        }
    }

    public static int setLevel(int level) {
        nativeCallActive = true;
        try { return volume.setLevel(level); } finally { nativeCallActive = false; }
    }

    public static void setMute(boolean value) {
        nativeCallActive = true;
        try { volume.setMute(value); } finally { nativeCallActive = false; }
    }

    public static int level() { return volume.getLevel(); }
    public static boolean isMuted() { return volume.isMuted(); }

    public static void resetHistory() {
        synchronized (historyLock) {
            count = 0;
            synchronousCallbacks = 0;
            lastEvent = null;
            for (int i = 0; i < HISTORY_LIMIT; i++) {
                codes[i] = 0;
                times[i] = 0;
                levels[i] = 0;
                muted[i] = false;
            }
        }
    }

    public static int eventCount() {
        synchronized (historyLock) { return count; }
    }

    public static boolean eventNameIdentity() {
        synchronized (historyLock) {
            if (count == 0) return lastEvent == null;
            int code = codes[count - 1];
            return code == STARTED && lastEvent == PlayerListener.STARTED ||
                   code == STOPPED && lastEvent == PlayerListener.STOPPED ||
                   code == END_OF_MEDIA && lastEvent == PlayerListener.END_OF_MEDIA ||
                   code == CLOSED && lastEvent == PlayerListener.CLOSED ||
                   code == VOLUME_CHANGED && lastEvent == PlayerListener.VOLUME_CHANGED ||
                   code == DURATION_UPDATED && lastEvent == PlayerListener.DURATION_UPDATED;
        }
    }

    public static int eventCode(int index) {
        synchronized (historyLock) { checkIndex(index); return codes[index]; }
    }

    public static long eventTime(int index) {
        synchronized (historyLock) { checkIndex(index); return times[index]; }
    }

    public static int eventLevel(int index) {
        synchronized (historyLock) { checkIndex(index); return levels[index]; }
    }

    public static boolean eventMuted(int index) {
        synchronized (historyLock) { checkIndex(index); return muted[index]; }
    }

    private static void checkIndex(int index) {
        if (index < 0 || index >= count) {
            throw new RuntimeException("event index is outside recorded history");
        }
    }

    public void playerUpdate(Player owner, String event, Object data) {
        boolean restart = false;
        boolean mute = false;
        synchronized (historyLock) {
            if (owner != player) {
                throw new RuntimeException("lifecycle event owner changed");
            }
            int code;
            if (PlayerListener.STARTED == event) code = STARTED;
            else if (PlayerListener.STOPPED == event) code = STOPPED;
            else if (PlayerListener.END_OF_MEDIA == event) code = END_OF_MEDIA;
            else if (PlayerListener.CLOSED == event) code = CLOSED;
            else if (PlayerListener.VOLUME_CHANGED == event) code = VOLUME_CHANGED;
            else if (PlayerListener.DURATION_UPDATED == event) code = DURATION_UPDATED;
            else throw new RuntimeException("unexpected lifecycle event");
            lastEvent = event;

            long time = -1;
            int level = -1;
            boolean eventMute = false;
            if (code == STARTED || code == STOPPED || code == END_OF_MEDIA || code == DURATION_UPDATED) {
                if (!(data instanceof Long)) {
                    throw new RuntimeException("lifecycle event requires a Long payload");
                }
                time = ((Long) data).longValue();
            } else if (code == VOLUME_CHANGED) {
                if (!(data instanceof VolumeControl) || data != volume) {
                    throw new RuntimeException("volume event requires its VolumeControl");
                }
                VolumeControl changed = (VolumeControl) data;
                level = changed.getLevel();
                eventMute = changed.isMuted();
            } else if (data != null) {
                throw new RuntimeException("closed event requires a null payload");
            }
            if (count == HISTORY_LIMIT) {
                throw new RuntimeException("lifecycle event history is full");
            }
            codes[count] = code;
            times[count] = time;
            levels[count] = level;
            muted[count] = eventMute;
            count++;
            // Records entry before a wrapper returns, not host thread identity.
            if (nativeCallActive) synchronousCallbacks++;
            if (code == END_OF_MEDIA && restartOnceOnEnd) {
                restartOnceOnEnd = false;
                restart = true;
            }
            if (code == VOLUME_CHANGED && muteOnceOnVolumeChanged) {
                muteOnceOnVolumeChanged = false;
                mute = true;
            }
        }
        if (restart) {
            try { player.start(); }
            catch (Exception error) { throw new RuntimeException("callback restart failed"); }
        }
        if (mute) volume.setMute(true);
    }
}

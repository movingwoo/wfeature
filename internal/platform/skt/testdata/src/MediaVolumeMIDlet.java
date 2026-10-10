import com.skt.m.AudioSystem;
import java.io.ByteArrayInputStream;
import javax.microedition.media.Controllable;
import javax.microedition.media.Control;
import javax.microedition.media.Manager;
import javax.microedition.media.Player;
import javax.microedition.media.PlayerListener;
import javax.microedition.media.control.VolumeControl;
import javax.microedition.midlet.MIDlet;

// Authored guest fixture for control identity, events and live output levels.
public final class MediaVolumeMIDlet extends MIDlet implements PlayerListener {
    public static Player first;
    public static Player second;
    public static VolumeControl volume;
    public static int events;
    public static int eventLevel;
    public static boolean eventMuted;
    public static boolean muteFromCallback;

    protected void startApp() { }
    protected void pauseApp() { }
    protected void destroyApp(boolean unconditional) { }

    public static void begin(byte[] sound) throws Exception {
        AudioSystem.setVolume(100);
        first = Manager.createPlayer(new ByteArrayInputStream(sound), "application/vnd.smaf");
        second = Manager.createPlayer(new ByteArrayInputStream(sound), "application/vnd.smaf");
        first.realize();
        second.realize();
        Controllable source = first;
        volume = (VolumeControl) source.getControl("VolumeControl");
        checkIdentity();
        first.addPlayerListener(new MediaVolumeMIDlet());
        first.setLoopCount(-1);
        second.setLoopCount(-1);
        first.start();
        second.start();
    }

    public static void checkIdentity() {
        Controllable source = first;
        Control[] controls = source.getControls();
        if (controls.length != 1 || controls[0] != volume ||
            source.getControl("VolumeControl") != volume ||
            source.getControl("javax.microedition.media.control.VolumeControl") != volume ||
            source.getControl("ToneControl") != null) {
            throw new IllegalStateException("control identity changed");
        }
        controls[0] = null;
        if (source.getControls()[0] != volume) {
            throw new IllegalStateException("control list was mutated");
        }
    }

    public static int setLevel(int level) { return volume.setLevel(level); }
    public static int level() { return volume.getLevel(); }
    public static void setMute(boolean muted) { volume.setMute(muted); }
    public static boolean muted() { return volume.isMuted(); }
    public static void closeFirst() { first.close(); }

    public void playerUpdate(Player player, String event, Object data) {
        if (PlayerListener.VOLUME_CHANGED.equals(event)) {
            if (player != first || data != volume) {
                throw new IllegalStateException("volume event owner changed");
            }
            checkIdentity();
            events++;
            eventLevel = volume.getLevel();
            eventMuted = volume.isMuted();
            if (muteFromCallback) {
                muteFromCallback = false;
                volume.setMute(true);
            }
        }
    }
}

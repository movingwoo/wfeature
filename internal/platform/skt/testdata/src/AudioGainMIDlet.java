import com.skt.m.AudioSystem;
import javax.microedition.media.Manager;
import javax.microedition.midlet.MIDlet;
import org.kwis.msp.media.Clip;
import org.kwis.msp.media.Player;

// Authored guest calls verify independent clip and device gain end to end.
public final class AudioGainMIDlet extends MIDlet {
    public static Clip first;
    public static Clip second;

    protected void startApp() { }
    protected void pauseApp() { }
    protected void destroyApp(boolean unconditional) { }

    public static void begin(byte[] sound) {
        AudioSystem.setVolume(100);
        first = new Clip("smaf", sound);
        second = new Clip("smaf", sound);
        if (!first.setVolume(25) || !Player.play(first, true) || !Player.play(second, true)) {
            throw new IllegalStateException("clip setup failed");
        }
    }

    public static int setClipLevel(int level) {
        if (!first.setVolume(level)) throw new IllegalStateException("clip volume failed");
        return first.getVolume();
    }

    public static int getClipLevel() { return first.getVolume(); }

    public static int setDeviceLevel(int level) {
        AudioSystem.setVolume(level);
        return AudioSystem.getVolume();
    }

    public static void tone(int note, int duration, int volume) throws Exception {
        Manager.playTone(note, duration, volume);
    }
}

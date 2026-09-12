import com.skt.m.AudioClip;
import com.skt.m.UserStopException;
import javax.microedition.midlet.MIDlet;

// Reports which path a blocking audio call takes when another thread stops it.
public final class AudioStopMIDlet extends MIDlet implements Runnable {
    public static AudioClip clip;
    public static boolean loop;
    private static volatile int result;

    protected void startApp() { }
    protected void pauseApp() { }
    protected void destroyApp(boolean unconditional) { clip.close(); }

    public static void begin() {
        result = 0;
        new Thread(new AudioStopMIDlet()).start();
    }

    public void run() {
        try {
            if (loop) clip.loop(); else clip.play();
            result = 1;
        } catch (Exception error) {
            result = error instanceof UserStopException ? 2 : 3;
        }
    }

    public static int result() { return result; }
}

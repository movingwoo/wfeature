import javax.microedition.lcdui.Canvas;
import javax.microedition.lcdui.Display;
import javax.microedition.lcdui.Graphics;
import javax.microedition.midlet.MIDlet;

// Authored reproduction of a MIDlet written for MIDP's reactivation on a
// handset VM that let a finished thread start again: its pauseApp ends its
// loop, and its startApp starts that same Thread object again once the loop is
// no longer alive. It declares no resumeApp.
public final class RestartLoopMIDlet extends MIDlet implements Runnable {
    private static int starts, runs, refused;
    private static volatile boolean running;
    private static volatile int frames;
    private static Thread loop;
    private static Screen screen;

    protected void startApp() {
        starts++;
        if (screen == null) {
            screen = new Screen();
            loop = new Thread(this);
        }
        Display.getDisplay(this).setCurrent(screen);
        if (!loop.isAlive()) {
            running = true;
            try {
                loop.start();
            } catch (IllegalThreadStateException again) {
                refused++;
            }
        }
    }

    protected void pauseApp() {
        running = false;
    }

    protected void destroyApp(boolean unconditional) {
        running = false;
    }

    public void run() {
        runs++;
        while (running) {
            frames++;
            try {
                Thread.sleep(10);
            } catch (InterruptedException stopped) {
                return;
            }
        }
    }

    public static int starts() { return starts; }
    public static int runs() { return runs; }
    public static int refused() { return refused; }
    public static int frames() { return frames; }
    public static int alive() { return loop != null && loop.isAlive() ? 1 : 0; }

    static final class Screen extends Canvas {
        protected void paint(Graphics g) {
            g.setColor(0x223344);
            g.fillRect(0, 0, getWidth(), getHeight());
        }
    }
}

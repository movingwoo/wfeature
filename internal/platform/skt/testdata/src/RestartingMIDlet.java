import javax.microedition.lcdui.Canvas;
import javax.microedition.lcdui.Display;
import javax.microedition.lcdui.Graphics;
import javax.microedition.midlet.MIDlet;
import javax.microedition.midlet.MIDletStateChangeException;

// Authored reproduction of a start callback that always replaces its game.
public final class RestartingMIDlet extends MIDlet {
    private static int starts, pauses;
    private static Scene scene;

    protected void startApp() throws MIDletStateChangeException {
        starts++;
        if (starts == 1 && getAppProperty("defer-first-start") != null) {
            throw new MIDletStateChangeException();
        }
        scene = new Scene();
        Display.getDisplay(this).setCurrent(scene);
        new Thread(scene).start();
    }
    protected void pauseApp() { pauses++; }
    protected void destroyApp(boolean unconditional) {}
    public static int starts() { return starts; }
    public static int pauses() { return pauses; }
    public static int progress() { return scene.progress; }

    private static final class Scene extends Canvas implements Runnable {
        private int progress;
        protected void keyPressed(int key) { progress++; repaint(); }
        protected void paint(Graphics g) {
            g.setColor(0x112200 + progress);
            g.fillRect(0, 0, getWidth(), getHeight());
        }
        public void run() {
            try {
                while (true) Thread.sleep(100);
            } catch (InterruptedException stopped) {}
        }
    }
}

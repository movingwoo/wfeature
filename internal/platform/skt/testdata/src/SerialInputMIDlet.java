import javax.microedition.lcdui.Canvas;
import javax.microedition.lcdui.Display;
import javax.microedition.lcdui.Graphics;
import javax.microedition.midlet.MIDlet;

// Authored animation loop: paint consumes input before the next update clears it.
public final class SerialInputMIDlet extends MIDlet {
    private static Display display;
    private static Loop canvas;
    private static int runs, paints, pending, consumed, lastKey;
    private static boolean synchronous, fail;

    protected void startApp() {
        display = Display.getDisplay(this);
        canvas = new Loop();
        display.setCurrent(canvas);
        display.callSerially(canvas);
    }
    protected void pauseApp() {}
    protected void destroyApp(boolean unconditional) {}
    public static int runs() { return runs; }
    public static int paints() { return paints; }
    public static int consumed() { return consumed; }
    public static int lastKey() { return lastKey; }
    public static void synchronous() { synchronous = true; }
    public static void fail() { fail = true; }
    public static void repaint() { canvas.repaint(); }

    private static final class Loop extends Canvas implements Runnable {
        public void run() {
            runs++;
            pending = 0;
            display.callSerially(this);
            repaint();
            if (synchronous) serviceRepaints();
            if (fail) throw new RuntimeException();
        }
        protected void keyPressed(int key) { pending++; lastKey = key; }
        protected void paint(Graphics g) {
            paints++;
            consumed += pending;
            pending = 0;
            g.setColor(consumed == 0 ? 0 : 0x33aa55);
            g.fillRect(0, 0, getWidth(), getHeight());
        }
    }
}

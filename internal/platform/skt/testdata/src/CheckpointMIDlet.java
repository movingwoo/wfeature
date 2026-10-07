import javax.microedition.midlet.MIDlet;
import javax.microedition.lcdui.Canvas;
import javax.microedition.lcdui.Display;
import javax.microedition.lcdui.Graphics;
import javax.microedition.rms.RecordStore;
import com.xce.io.XFile;
import com.xce.io.FileInputStream;

// An authored execution/save fixture. Startup and worker prefixes are visible.
public final class CheckpointMIDlet extends MIDlet implements Runnable {
    public static int starts, destroys, before, after, value, readValue;
    public static Thread worker;
    public static RecordStore store;
    public static XFile file;
    public static FileInputStream input;
    private static Screen screen;

    protected void startApp() {
        starts++;
        if (screen != null) return;
        try {
            store = RecordStore.openRecordStore("checkpoint", true);
            if (store.getNumRecords() == 0) store.addRecord(new byte[] {1}, 0, 1);
            file = new XFile("checkpoint", 3);
            file.write(new byte[] {1, 2, 3}, 0, 3);
            input = new FileInputStream(file);
            input.mark(3);
        } catch (Exception error) { value = -1; }
        screen = new Screen();
        Display.getDisplay(this).setCurrent(screen);
        worker = new Thread(this);
        worker.start();
    }
    public void run() {
        before++;
        try { Thread.sleep(60000); } catch (InterruptedException error) { }
        after++;
    }
    protected void pauseApp() { }
    protected void destroyApp(boolean unconditional) { destroys++; }

    public static void readCurrent() throws Exception {
        readValue = store.getRecord(1)[0] * 100;
        file.seek(0, 0);
        readValue += input.read();
    }
    public static void writeCurrent(int next) throws Exception {
        value = next;
        store.setRecord(1, new byte[] {(byte) next}, 0, 1);
        file.seek(0, 0);
        file.write(new byte[] {(byte) next}, 0, 1);
    }
    private static final class Screen extends Canvas {
        protected void paint(Graphics graphics) {
            graphics.setColor(value == 0 ? 0x123456 : 0x654321);
            graphics.fillRect(0, 0, getWidth(), getHeight());
        }
        protected void keyPressed(int key) { value++; repaint(); }
    }
}

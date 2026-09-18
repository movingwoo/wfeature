import javax.microedition.lcdui.Canvas;
import javax.microedition.lcdui.Display;
import javax.microedition.lcdui.Graphics;
import javax.microedition.midlet.MIDlet;
import javax.microedition.rms.RecordStore;

/** A saved counter visible through the real browser framebuffer. */
public final class PersistenceMIDlet extends MIDlet {
    protected void startApp() {
        Display.getDisplay(this).setCurrent(new ProgressCanvas());
    }
    protected void pauseApp() {}
    protected void destroyApp(boolean unconditional) {}

    private static final class ProgressCanvas extends Canvas {
        private int progress;
        private boolean failed;
        private boolean held;

        ProgressCanvas() {
            try {
                RecordStore store = RecordStore.openRecordStore("progress", true);
                if (store.getNumRecords() > 0) progress = store.getRecord(1)[0];
                store.closeRecordStore();
            } catch (Exception error) { failed = true; }
        }
        protected void keyPressed(int key) {
            held = true;
            if (getGameAction(key) == FIRE) {
                progress = (progress + 1) % 3;
                try {
                    RecordStore store = RecordStore.openRecordStore("progress", true);
                    byte[] data = new byte[] { (byte) progress };
                    if (store.getNumRecords() == 0) store.addRecord(data, 0, 1);
                    else store.setRecord(1, data, 0, 1);
                    store.closeRecordStore();
                } catch (Exception error) { failed = true; }
            }
            repaint();
        }
        protected void keyReleased(int key) { held = false; repaint(); }
        protected void paint(Graphics graphics) {
            graphics.setColor(failed ? 0xff00ff : progress == 0 ? 0xff0000 : progress == 1 ? 0x00ff00 : 0x0000ff);
            graphics.fillRect(0, 0, getWidth(), getHeight());
            graphics.setColor(held ? 0xffffff : 0x000000);
            graphics.fillRect(0, 0, 16, 16);
        }
    }
}

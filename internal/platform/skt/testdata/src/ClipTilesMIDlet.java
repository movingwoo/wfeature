import javax.microedition.lcdui.Canvas;
import javax.microedition.lcdui.Display;
import javax.microedition.lcdui.Graphics;
import javax.microedition.lcdui.Image;
import javax.microedition.midlet.MIDlet;

// Authored fixture for an inclusive clipping compatibility path.
public final class ClipTilesMIDlet extends MIDlet {
    protected void startApp() {
        Display.getDisplay(this).setCurrent(new Tiles());
    }

    protected void pauseApp() {}
    protected void destroyApp(boolean unconditional) {}

    private static final class Tiles extends Canvas {
        protected void paint(Graphics graphics) {
            graphics.setColor(0);
            graphics.fillRect(0, 0, getWidth(), getHeight());
            Image tile = Image.createImage(16, 16);
            Graphics source = tile.getGraphics();
            source.setColor(0x33aa55);
            source.fillRect(0, 0, 16, 16);
            for (int y = 0; y < 32; y += 16) {
                for (int x = 0; x < 32; x += 16) {
                    graphics.setClip(x, y, 15, 15);
                    graphics.drawImage(tile, x, y, Graphics.TOP | Graphics.LEFT);
                }
            }
        }
    }
}

import org.kwis.msp.lcdui.Card;
import org.kwis.msp.lcdui.Display;
import org.kwis.msp.lcdui.Graphics;
import org.kwis.msp.lcdui.Image;
import org.kwis.msp.lcdui.Jlet;

/**
 * A Jlet whose card draws one frame with the WIPI Graphics members MIDP does
 * not have, and records what the getters and the refused calls answered in
 * RESULTS. wipi_graphics_members_test.go reads the frame and the array.
 */
public class WIPIGraphicsMembersJlet extends Jlet {
    static final int PAINTS = 0, XOR_DEFAULT = 1, XOR_ON = 2, XOR_OFF = 3,
        RED = 4, GREEN = 5, BLUE = 6, STROKE_DEFAULT = 7, STROKE_DOTTED = 8,
        PIXEL = 9, TRANSLATED_PIXEL = 10, PIXEL_LEFT_OF_SURFACE = 11,
        PIXEL_RIGHT_OF_SURFACE = 12, COPY_MUTABLE = 13, COPY_WIDTH = 14,
        COPY_HEIGHT = 15, NULL_POLYGON = 16, NULL_COPY = 17,
        XOR_AFTER_RESET = 18, STROKE_AFTER_RESET = 19,
        TRANSLATION_AFTER_RESET = 20;

    public static final int[] RESULTS = new int[21];

    /** A 2x1 palette PNG: a transparent pixel, then an opaque red one. */
    static final byte[] MASKED = {
        -119, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82,
        0, 0, 0, 2, 0, 0, 0, 1, 1, 3, 0, 0, 0, -50, -20, -19,
        -55, 0, 0, 0, 6, 80, 76, 84, 69, 0, 0, 0, -1, 0, 0, 27,
        -1, -115, 34, 0, 0, 0, 1, 116, 82, 78, 83, 0, 64, -26, -40, 102,
        0, 0, 0, 15, 73, 68, 65, 84, 120, -100, 0, 2, 0, -3, -1, 0,
        64, 3, 0, 0, 66, 0, 65, -56, -109, -37, -94, 0, 0, 0, 0, 73,
        69, 78, 68, -82, 66, 96, -126,
    };

    protected void startApp(String[] args) {
        Display.getDefaultDisplay().pushCard(new Scene());
    }

    protected void destroyApp(boolean unconditional) {
    }

    static final class Scene extends Card {
        protected void paint(Graphics g) {
            RESULTS[PAINTS]++;
            RESULTS[XOR_DEFAULT] = g.isXORMode() ? 1 : 0;
            RESULTS[STROKE_DEFAULT] = g.getStrokeStyle();

            g.setColor(0x2040c0);
            g.fillRect(0, 0, 48, 32);

            // XOR fills: white inverts, twice restores, black changes nothing.
            g.setXORMode(true);
            RESULTS[XOR_ON] = g.isXORMode() ? 1 : 0;
            g.setColor(0xffffff);
            g.fillRect(0, 0, 4, 4);
            g.fillRect(4, 0, 4, 4);
            g.fillRect(4, 0, 4, 4);
            g.setColor(0x000000);
            g.fillRect(8, 0, 4, 4);
            g.setXORMode(false);
            RESULTS[XOR_OFF] = g.isXORMode() ? 1 : 0;

            // The colour, read back in parts.
            g.setColor(0x123456);
            RESULTS[RED] = g.getRedComponent();
            RESULTS[GREEN] = g.getGreenComponent();
            RESULTS[BLUE] = g.getBlueComponent();

            // A dotted stroke is kept and drawn solid.
            g.setStrokeStyle(Graphics.DOTTED);
            RESULTS[STROKE_DOTTED] = g.getStrokeStyle();
            g.setColor(0xff0000);
            g.drawLine(0, 5, 7, 5);
            g.setStrokeStyle(Graphics.SOLID);

            // One pixel written and read, plain and translated.
            g.setColor(0x00ff00);
            g.setPixel(10, 5);
            RESULTS[PIXEL] = g.getPixel(10, 5);
            g.translate(12, 4);
            g.setColor(0x0000ff);
            g.setPixel(0, 0);
            RESULTS[TRANSLATED_PIXEL] = g.getPixel(0, 0);
            g.translate(-12, -4);
            RESULTS[PIXEL_LEFT_OF_SURFACE] = g.getPixel(-1, 0);
            RESULTS[PIXEL_RIGHT_OF_SURFACE] = g.getPixel(48, 0);

            // A triangle's outline.
            g.setColor(0xffff00);
            g.drawPolygon(new int[] {0, 6, 0}, new int[] {8, 8, 14});

            // A marker copied, the destination named first.
            g.setColor(0xff00ff);
            g.fillRect(10, 8, 2, 2);
            g.copyArea(14, 8, 10, 8, 2, 2);

            // A copy that overlaps itself copies what was there before it.
            g.setColor(0xffffff);
            g.setPixel(10, 11);
            g.setColor(0x000000);
            g.fillRect(11, 11, 3, 1);
            g.copyArea(11, 11, 10, 11, 4, 1);

            // A copy in XOR mode draws the difference.
            g.setColor(0xff0000);
            g.setPixel(10, 13);
            g.setColor(0x0000ff);
            g.setPixel(12, 13);
            g.setXORMode(true);
            g.copyArea(12, 13, 10, 13, 1, 1);
            g.setXORMode(false);

            // A character in XOR mode: drawn twice it is gone, drawn once it
            // is inverted.
            g.setXORMode(true);
            g.setColor(0xffffff);
            g.drawChar('W', 34, 2, Graphics.TOP | Graphics.LEFT);
            g.drawChar('W', 34, 2, Graphics.TOP | Graphics.LEFT);
            g.drawChar('W', 34, 16, Graphics.TOP | Graphics.LEFT);
            g.setXORMode(false);

            // A copy of an image is one that can be drawn on.
            Image cyan = Image.createImage(4, 4);
            Graphics cyanGraphics = cyan.getGraphics();
            cyanGraphics.setColor(0x00ffff);
            cyanGraphics.fillRect(0, 0, 4, 4);
            Image copy = Image.createImage(cyan);
            RESULTS[COPY_MUTABLE] = copy.isMutable() ? 1 : 0;
            RESULTS[COPY_WIDTH] = copy.getWidth();
            RESULTS[COPY_HEIGHT] = copy.getHeight();
            Graphics copyGraphics = copy.getGraphics();
            copyGraphics.setColor(0xff0000);
            copyGraphics.setPixel(0, 0);
            g.drawImage(copy, 0, 16, Graphics.TOP | Graphics.LEFT);
            g.setXORMode(true);
            g.drawImage(copy, 4, 16, Graphics.TOP | Graphics.LEFT);
            // A rectangle of it from column 1, which leaves its red corner
            // out: once inverted, twice gone.
            g.drawImage(copy, 0, 26, 1, 0, 2, 2, Graphics.TOP | Graphics.LEFT);
            g.drawImage(copy, 4, 26, 1, 0, 2, 2, Graphics.TOP | Graphics.LEFT);
            g.drawImage(copy, 4, 26, 1, 0, 2, 2, Graphics.TOP | Graphics.LEFT);
            g.setXORMode(false);

            // An outline and a line in XOR mode, each pixel inverted once.
            g.setXORMode(true);
            g.setColor(0xffffff);
            g.drawRect(10, 16, 3, 3);
            g.drawLine(16, 16, 19, 19);
            g.setXORMode(false);

            // A picture with a transparent pixel: its copy is white there, and
            // in XOR mode only the pixel it draws inverts.
            Image masked = Image.createImage(MASKED, 0, MASKED.length);
            g.drawImage(Image.createImage(masked), 24, 16, Graphics.TOP | Graphics.LEFT);
            g.setXORMode(true);
            g.drawImage(masked, 24, 18, Graphics.TOP | Graphics.LEFT);
            g.setXORMode(false);

            // What a missing array or image is answered with.
            try {
                g.drawPolygon(null, new int[] {0, 1});
            } catch (NullPointerException e) {
                RESULTS[NULL_POLYGON] = 1;
            }
            try {
                Image.createImage((Image) null);
            } catch (NullPointerException e) {
                RESULTS[NULL_COPY] = 1;
            }

            // reset puts back XOR mode and the stroke style with the rest.
            g.translate(3, 3);
            g.setXORMode(true);
            g.setStrokeStyle(Graphics.DOTTED);
            g.reset();
            RESULTS[XOR_AFTER_RESET] = g.isXORMode() ? 1 : 0;
            RESULTS[STROKE_AFTER_RESET] = g.getStrokeStyle();
            RESULTS[TRANSLATION_AFTER_RESET] = g.getTranslateX();
        }
    }
}

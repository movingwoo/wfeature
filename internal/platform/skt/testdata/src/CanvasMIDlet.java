import javax.microedition.lcdui.Canvas;
import javax.microedition.lcdui.Display;
import javax.microedition.lcdui.Font;
import javax.microedition.lcdui.Graphics;
import javax.microedition.lcdui.Image;
import javax.microedition.midlet.MIDlet;
import java.io.DataInputStream;
import java.io.IOException;

public final class CanvasMIDlet extends MIDlet {
    private static PaintCanvas canvas;

    public CanvasMIDlet() {
        canvas = new PaintCanvas();
    }

    protected void startApp() {
        Display.getDisplay(this).setCurrent(canvas);
    }

    protected void pauseApp() {
    }

    protected void destroyApp(boolean unconditional) {
    }

    public static int paintCount() {
        return canvas.paintCount;
    }

    /**
     * Draws through the Graphics the last paint was handed, from outside any
     * paint, the way a title that owns its own frame loop does. Pushing the
     * result is the vendor runtime's business, not this fixture's.
     */
    public static boolean drawAfterPaint() {
        if (canvas.kept == null) {
            return false;
        }
        canvas.kept.setColor(0x00ff00);
        canvas.kept.fillRect(0, 0, 1, 1);
        return true;
    }

    public static int dimensions() {
        return canvas.getWidth() * 1000 + canvas.getHeight();
    }

    public static int lastColor() {
        return canvas.lastColor;
    }

    public static int keyEvents() {
        return canvas.keyEvents;
    }

    public static int lastKeyCode() {
        return canvas.lastKeyCode;
    }

    public static int pointerEvents() {
        return canvas.pointerEvents;
    }

    public static int lastPointer() {
        return canvas.lastPointerX * 1000 + canvas.lastPointerY;
    }

    public static int graphicsState() {
        return canvas.graphicsState;
    }

    public static int imageState() {
        return canvas.imageState;
    }

    public static int fontState() {
        return canvas.fontState;
    }

    public static int canvasAPIState() {
        int state = 0;
        if (canvas.getGameAction(141) == Canvas.UP
                && canvas.getGameAction(Canvas.KEY_NUM2) == Canvas.UP
                && canvas.getGameAction(Canvas.KEY_NUM5) == Canvas.FIRE
                && canvas.getGameAction(Canvas.KEY_NUM1) == Canvas.GAME_A
                && canvas.getGameAction(999) == 0) {
            state |= 1;
        }
        if (canvas.getKeyCode(Canvas.UP) == 141
                && canvas.getKeyCode(Canvas.FIRE) == 148
                && canvas.getKeyCode(Canvas.GAME_D) == Canvas.KEY_NUM9) {
            state |= 2;
        }
        if (canvas.getKeyName(141).equals("UP")
                && canvas.getKeyName(Canvas.KEY_NUM0).equals("0")
                && canvas.getKeyName(Canvas.KEY_STAR).equals("*")) {
            state |= 4;
        }
        try {
            canvas.getKeyCode(0);
        } catch (IllegalArgumentException exception) {
            state |= 8;
        }
        try {
            canvas.getKeyName(999);
        } catch (IllegalArgumentException exception) {
            state |= 16;
        }
        if (canvas.isDoubleBuffered() && canvas.hasRepeatEvents()
                && canvas.hasPointerEvents() && canvas.hasPointerMotionEvents()) {
            state |= 32;
        }
        return state;
    }

    public static void setFullScreen(boolean fullScreen) {
        canvas.setFullScreenMode(fullScreen);
    }

    public static int resourceState() {
        try {
            DataInputStream input = new DataInputStream(
                canvas.getClass().getResourceAsStream("/data.bin"));
            int state = input.readInt() == 42 && input.readUTF().equals("OK") ? 1 : 0;
            if (input.available() == 0) {
                state |= 2;
            }
            input.close();
            return state;
        } catch (IOException exception) {
            return 0;
        }
    }

    public static void requestPartialRepaint() {
        canvas.mode = 1;
        canvas.repaint(0, 0, 2, 1);
        canvas.repaint(1, 0, 2, 1);
    }

    public static void requestSynchronousRepaint() {
        canvas.mode = 2;
        canvas.repaint();
        canvas.serviceRepaints();
    }

    public static void requestClipTranslationPaint() {
        canvas.mode = 6;
        canvas.repaint();
        canvas.serviceRepaints();
    }

    public static void requestExtremeLinePaint() {
        canvas.mode = 7;
        canvas.repaint();
        canvas.serviceRepaints();
    }

    public static void requestRectanglePaint() {
        canvas.mode = 8;
        canvas.repaint();
        canvas.serviceRepaints();
    }

    public static void requestImagePaint() {
        canvas.mode = 9;
        canvas.repaint();
        canvas.serviceRepaints();
    }

    public static void requestTextPaint() {
        canvas.mode = 10;
        canvas.repaint();
        canvas.serviceRepaints();
    }

    private static final class PaintCanvas extends Canvas {
        Graphics kept;
        int paintCount;
        int mode;
        int lastColor;
        int keyEvents;
        int lastKeyCode;
        int pointerEvents;
        int lastPointerX;
        int lastPointerY;
        int graphicsState;
        int imageState;
        int fontState;

        protected void paint(Graphics graphics) {
            paintCount++;
            kept = graphics;
            if (mode == 0) {
                graphics.setColor(0x102030);
                graphics.fillRect(0, 0, getWidth(), getHeight());
                graphics.setColor(0xff0000);
                graphics.fillRect(1, 1, 2, 2);
            } else if (mode == 1) {
                graphics.setColor(0x00ff00);
                graphics.fillRect(0, 0, getWidth(), getHeight());
            } else if (mode == 2) {
                graphics.setColor(0x0000ff);
                graphics.fillRect(0, 0, getWidth(), getHeight());
            } else if (mode == 3) {
                graphics.setColor(0xffff00);
                graphics.fillRect(0, 0, getWidth(), getHeight());
            } else if (mode == 4) {
                graphics.setColor(0xff00ff);
                graphics.fillRect(0, 0, getWidth(), getHeight());
            } else if (mode == 5) {
                graphics.setColor(0x00ffff);
                graphics.fillRect(0, 0, getWidth(), getHeight());
            } else if (mode == 6) {
                graphics.setColor(0x101010);
                graphics.fillRect(0, 0, getWidth(), getHeight());
                graphics.translate(1, 1);
                graphics.setClip(0, 0, 2, 2);
                graphics.clipRect(-1, -1, 3, 3);
                graphicsState = graphics.getTranslateX() * 100000
                    + graphics.getTranslateY() * 10000
                    + graphics.getClipX() * 1000
                    + graphics.getClipY() * 100
                    + graphics.getClipWidth() * 10
                    + graphics.getClipHeight();
                graphics.setColor(0, 255, 0);
                graphics.fillRect(-1000000000, -1000000000, 2000000000, 2000000000);
            } else if (mode == 7) {
                graphics.setColor(0x000000);
                graphics.fillRect(0, 0, getWidth(), getHeight());
                graphics.setColor(0xffffff);
                graphics.drawLine(-1000000000, 1, 1000000000, 1);
            } else if (mode == 8) {
                graphics.setColor(0x000000);
                graphics.fillRect(0, 0, getWidth(), getHeight());
                graphics.setColor(0xff0000);
                graphics.drawRect(1, 0, 2, 2);
                graphics.setColor(0x0000ff);
                graphics.drawRect(0, 0, 0, 2);
            } else if (mode == 9) {
                paintImages(graphics);
            } else {
                paintText(graphics);
            }
            lastColor = graphics.getColor();
        }

        private void paintImages(Graphics graphics) {
            graphics.setColor(0x000000);
            graphics.fillRect(0, 0, getWidth(), getHeight());

            int[] sourcePixels = {
                0xffff0000, 0x8000ff00,
                0x000000ff, 0xffffffff,
                0xff0000ff, 0xffffff00
            };
            Image source = Image.createRGBImage(sourcePixels, 2, 3, true);
            if (!source.isMutable() && source.getWidth() == 2 && source.getHeight() == 3) {
                imageState |= 1;
            }
            Image rotated = Image.createImage(source, 0, 0, 2, 3, 5);
            if (rotated.getWidth() == 3 && rotated.getHeight() == 2) {
                imageState |= 2;
            }
            graphics.drawRegion(source, 0, 0, 2, 3, 5, 0, 0, Graphics.TOP | Graphics.LEFT);

            Image mutable = Image.createImage(2, 1);
            Graphics imageGraphics = mutable.getGraphics();
            imageGraphics.setColor(0xff00ff);
            imageGraphics.fillRect(0, 0, 1, 1);
            Image snapshot = Image.createImage(mutable);
            int[] copiedPixels = new int[2];
            snapshot.getRGB(copiedPixels, 0, 2, 0, 0, 2, 1);
            if (mutable.isMutable() && !snapshot.isMutable()
                    && copiedPixels[0] == 0xffff00ff && copiedPixels[1] == 0xffffffff) {
                imageState |= 4;
            }

            try {
                Image resource = Image.createImage("/fixture.png");
                int[] resourcePixels = new int[2];
                resource.getRGB(resourcePixels, 0, 2, 0, 0, 2, 1);
                if (resourcePixels[0] == 0xff123456 && resourcePixels[1] == 0x00aabbcc) {
                    imageState |= 8;
                }
            } catch (java.io.IOException exception) {
            }

            Image decoded = Image.createImage(PNG_DATA, 0, PNG_DATA.length);
            int[] decodedPixels = new int[2];
            decoded.getRGB(decodedPixels, 0, 2, 0, 0, 2, 1);
            if (decodedPixels[0] == 0xff123456 && decodedPixels[1] == 0x00aabbcc) {
                imageState |= 16;
            }

            try {
                Image.createImage(0, 1);
            } catch (IllegalArgumentException exception) {
                imageState |= 32;
            }
            try {
                source.getGraphics();
            } catch (IllegalStateException exception) {
                imageState |= 64;
            }

            int[] scanlines = {0xff112233, 0xff445566, 0xff778899, 0xffaabbcc};
            graphics.setClip(0, 2, 2, 1);
            graphics.drawRGB(scanlines, 2, -2, 0, 1, 2, 2, true);
            graphics.setClip(0, 0, getWidth(), getHeight());
            graphics.drawImage(snapshot, 2, 2, Graphics.TOP | Graphics.LEFT);
        }

        private void paintText(Graphics graphics) {
            graphics.setColor(0x000000);
            graphics.fillRect(0, 0, getWidth(), getHeight());

            Font defaultFont = Font.getDefaultFont();
            if (defaultFont.getFace() == Font.FACE_SYSTEM
                    && defaultFont.getStyle() == Font.STYLE_PLAIN
                    && defaultFont.getSize() == Font.SIZE_MEDIUM) {
                fontState |= 1;
            }
            Font font = Font.getFont(Font.FACE_MONOSPACE,
                Font.STYLE_BOLD | Font.STYLE_UNDERLINED, Font.SIZE_SMALL);
            if (font.isBold() && font.isUnderlined() && !font.isItalic() && !font.isPlain()) {
                fontState |= 2;
            }
            char[] characters = {'A', 'B'};
            if (font.getHeight() == 8 && font.getBaselinePosition() == 7
                    && font.charWidth('A') == 7 && font.stringWidth("AB") == 14
                    && font.substringWidth("ZAB", 1, 2) == 14
                    && font.charsWidth(characters, 0, 2) == 14) {
                fontState |= 4;
            }
            graphics.setFont(font);
            if (graphics.getFont() == font) {
                fontState |= 8;
            }
            graphics.setFont(null);
            if (graphics.getFont() == defaultFont) {
                fontState |= 16;
            }
            graphics.setFont(font);
            graphics.setColor(0xffffff);
            graphics.drawString("A", 7, 8, Graphics.RIGHT | Graphics.BOTTOM);
            graphics.drawChars(characters, 1, 1, 8, 0, Graphics.TOP | Graphics.LEFT);
            graphics.drawSubstring("CD", 1, 1, 16, 0, Graphics.TOP | Graphics.LEFT);
            try {
                graphics.drawString("X", 0, 0, Graphics.VCENTER | Graphics.LEFT);
            } catch (IllegalArgumentException exception) {
                fontState |= 32;
            }
        }

        protected void keyPressed(int keyCode) {
            keyEvents = keyEvents * 10 + 1;
            lastKeyCode = keyCode;
            if (keyCode == '1') {
                mode = 9;
            } else if (keyCode == '2') {
                mode = 10;
            } else {
                mode = 3;
            }
            repaint();
        }

        protected void keyReleased(int keyCode) {
            keyEvents = keyEvents * 10 + 2;
            lastKeyCode = keyCode;
            mode = 5;
            repaint();
        }

        protected void keyRepeated(int keyCode) {
            keyEvents = keyEvents * 10 + 3;
            lastKeyCode = keyCode;
            mode = 4;
            repaint();
        }

        protected void pointerPressed(int x, int y) {
            pointerEvents = pointerEvents * 10 + 1;
            lastPointerX = x;
            lastPointerY = y;
        }

        protected void pointerReleased(int x, int y) {
            pointerEvents = pointerEvents * 10 + 2;
            lastPointerX = x;
            lastPointerY = y;
        }

        protected void pointerDragged(int x, int y) {
            pointerEvents = pointerEvents * 10 + 3;
            lastPointerX = x;
            lastPointerY = y;
        }
    }

    private static final byte[] PNG_DATA = {
        (byte)137, (byte)80, (byte)78, (byte)71, (byte)13, (byte)10, (byte)26, (byte)10,
        (byte)0, (byte)0, (byte)0, (byte)13, (byte)73, (byte)72, (byte)68, (byte)82,
        (byte)0, (byte)0, (byte)0, (byte)2, (byte)0, (byte)0, (byte)0, (byte)1,
        (byte)8, (byte)6, (byte)0, (byte)0, (byte)0, (byte)244, (byte)34, (byte)127,
        (byte)138, (byte)0, (byte)0, (byte)0, (byte)22, (byte)73, (byte)68, (byte)65,
        (byte)84, (byte)120, (byte)156, (byte)98, (byte)18, (byte)50, (byte)9, (byte)251,
        (byte)191, (byte)106, (byte)247, (byte)25, (byte)6, (byte)64, (byte)0, (byte)0,
        (byte)0, (byte)255, (byte)255, (byte)15, (byte)135, (byte)3, (byte)207, (byte)208,
        (byte)19, (byte)199, (byte)63, (byte)0, (byte)0, (byte)0, (byte)0, (byte)73,
        (byte)69, (byte)78, (byte)68, (byte)174, (byte)66, (byte)96, (byte)130
    };
}

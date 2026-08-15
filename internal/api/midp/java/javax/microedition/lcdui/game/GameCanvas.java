package javax.microedition.lcdui.game;

import javax.microedition.lcdui.Canvas;
import javax.microedition.lcdui.Graphics;

/**
 * A Canvas that owns an off-screen buffer and pushes it to the display when
 * the game says so, instead of waiting to be asked to paint.
 */
public abstract class GameCanvas extends Canvas {
    public static final int UP_PRESSED = 1 << 1;
    public static final int DOWN_PRESSED = 1 << 6;
    public static final int LEFT_PRESSED = 1 << 2;
    public static final int RIGHT_PRESSED = 1 << 5;
    public static final int FIRE_PRESSED = 1 << 8;
    public static final int GAME_A_PRESSED = 1 << 9;
    public static final int GAME_B_PRESSED = 1 << 10;
    public static final int GAME_C_PRESSED = 1 << 11;
    public static final int GAME_D_PRESSED = 1 << 12;

    protected GameCanvas(boolean suppressKeyEvents) {
        initBuffer(suppressKeyEvents);
    }

    private native void initBuffer(boolean suppressKeyEvents);

    protected native Graphics getGraphics();

    public native int getKeyStates();

    public native void flushGraphics();

    public native void flushGraphics(int x, int y, int width, int height);

    /**
     * A GameCanvas paints from its buffer, so the inherited abstract paint is
     * satisfied here and a subclass only overrides it if it wants the
     * ordinary Canvas contract as well.
     */
    protected void paint(Graphics graphics) {
        drawBuffer(graphics);
    }

    private native void drawBuffer(Graphics graphics);
}

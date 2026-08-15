package javax.microedition.lcdui;

/**
 * Minimal runtime-owned low-level drawing surface.
 */
public abstract class Canvas extends Displayable {
    public static final int UP = 1;
    public static final int LEFT = 2;
    public static final int RIGHT = 5;
    public static final int DOWN = 6;
    public static final int FIRE = 8;
    public static final int GAME_A = 9;
    public static final int GAME_B = 10;
    public static final int GAME_C = 11;
    public static final int GAME_D = 12;

    public static final int KEY_NUM0 = '0';
    public static final int KEY_NUM1 = '1';
    public static final int KEY_NUM2 = '2';
    public static final int KEY_NUM3 = '3';
    public static final int KEY_NUM4 = '4';
    public static final int KEY_NUM5 = '5';
    public static final int KEY_NUM6 = '6';
    public static final int KEY_NUM7 = '7';
    public static final int KEY_NUM8 = '8';
    public static final int KEY_NUM9 = '9';
    public static final int KEY_STAR = '*';
    public static final int KEY_POUND = '#';

    private static final int DEVICE_UP = 141;
    private static final int DEVICE_LEFT = 142;
    private static final int DEVICE_RIGHT = 145;
    private static final int DEVICE_DOWN = 146;
    private static final int DEVICE_FIRE = 148;

    protected Canvas() {
    }

    public final void repaint() {
        repaint(0, 0, getWidth(), getHeight());
    }

    public final native void repaint(int x, int y, int width, int height);

    public final native void serviceRepaints();

    public int getGameAction(int keyCode) {
        switch (keyCode) {
        case DEVICE_UP:
        case KEY_NUM2:
            return UP;
        case DEVICE_LEFT:
        case KEY_NUM4:
            return LEFT;
        case DEVICE_RIGHT:
        case KEY_NUM6:
            return RIGHT;
        case DEVICE_DOWN:
        case KEY_NUM8:
            return DOWN;
        case DEVICE_FIRE:
        case KEY_NUM5:
            return FIRE;
        case KEY_NUM1:
            return GAME_A;
        case KEY_NUM3:
            return GAME_B;
        case KEY_NUM7:
            return GAME_C;
        case KEY_NUM9:
            return GAME_D;
        default:
            return 0;
        }
    }

    public native int getKeyCode(int gameAction);

    public native String getKeyName(int keyCode);

    public native void setFullScreenMode(boolean fullScreen);

    public boolean isDoubleBuffered() {
        return true;
    }

    public boolean hasRepeatEvents() {
        return true;
    }

    public boolean hasPointerEvents() {
        return true;
    }

    public boolean hasPointerMotionEvents() {
        return true;
    }

    protected abstract void paint(Graphics graphics);

    protected void keyPressed(int keyCode) {
    }

    protected void keyReleased(int keyCode) {
    }

    protected void keyRepeated(int keyCode) {
    }

    protected void pointerPressed(int x, int y) {
    }

    protected void pointerReleased(int x, int y) {
    }

    protected void pointerDragged(int x, int y) {
    }
}

package javax.microedition.lcdui;

/**
 * A labelled action a Displayable offers. The runtime draws the labels and
 * decides which key activates which command; see docs/lcdui.md.
 */
public class Command {
    public static final int SCREEN = 1;
    public static final int BACK = 2;
    public static final int CANCEL = 3;
    public static final int OK = 4;
    public static final int HELP = 5;
    public static final int STOP = 6;
    public static final int EXIT = 7;
    public static final int ITEM = 8;

    public Command(String label, int commandType, int priority) {
        init(label, null, commandType, priority);
    }

    public Command(String shortLabel, String longLabel, int commandType, int priority) {
        init(shortLabel, longLabel, commandType, priority);
    }

    private native void init(String shortLabel, String longLabel, int commandType, int priority);

    public native String getLabel();

    public native String getLongLabel();

    public native int getCommandType();

    public native int getPriority();
}

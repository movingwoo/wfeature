package javax.microedition.lcdui;

/**
 * The choice screen a game menu is usually built from. It shares the runtime's
 * choice implementation with ChoiceGroup.
 */
public class List extends Screen implements Choice {
    public static final Command SELECT_COMMAND = new Command("Select", Command.SCREEN, 0);

    public List(String title, int listType) {
        this(title, listType, new String[0], null);
    }

    public List(String title, int listType, String[] stringElements, Image[] imageElements) {
        setTitle(title);
        initChoice(listType);
        if (stringElements != null) {
            for (int index = 0; index < stringElements.length; index++) {
                Image image = imageElements == null ? null : imageElements[index];
                append(stringElements[index], image);
            }
        }
    }

    native void initChoice(int listType);

    public native int size();

    public native String getString(int elementNum);

    public native Image getImage(int elementNum);

    public native int append(String stringPart, Image imagePart);

    public native void insert(int elementNum, String stringPart, Image imagePart);

    public native void delete(int elementNum);

    public native void deleteAll();

    public native void set(int elementNum, String stringPart, Image imagePart);

    public native boolean isSelected(int elementNum);

    public native int getSelectedIndex();

    public native int getSelectedFlags(boolean[] selectedArray);

    public native void setSelectedIndex(int elementNum, boolean selected);

    public native void setSelectedFlags(boolean[] selectedArray);

    public native void setFitPolicy(int fitPolicy);

    public native int getFitPolicy();

    public native void setSelectCommand(Command command);

    public native void setFont(int elementNum, Font font);

    public native Font getFont(int elementNum);
}

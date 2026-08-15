package javax.microedition.lcdui;

public class ChoiceGroup extends Item implements Choice {
    public ChoiceGroup(String label, int choiceType) {
        this(label, choiceType, new String[0], null);
    }

    public ChoiceGroup(String label, int choiceType, String[] stringElements, Image[] imageElements) {
        setLabel(label);
        initChoice(choiceType);
        if (stringElements != null) {
            for (int index = 0; index < stringElements.length; index++) {
                Image image = imageElements == null ? null : imageElements[index];
                append(stringElements[index], image);
            }
        }
    }

    native void initChoice(int choiceType);

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

    public native void setFont(int elementNum, Font font);

    public native Font getFont(int elementNum);
}

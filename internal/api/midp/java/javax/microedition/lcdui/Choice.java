package javax.microedition.lcdui;

public interface Choice {
    int EXCLUSIVE = 1;
    int MULTIPLE = 2;
    int IMPLICIT = 3;
    int POPUP = 4;

    int TEXT_WRAP_DEFAULT = 0;
    int TEXT_WRAP_ON = 1;
    int TEXT_WRAP_OFF = 2;

    int size();

    String getString(int elementNum);

    Image getImage(int elementNum);

    int append(String stringPart, Image imagePart);

    void insert(int elementNum, String stringPart, Image imagePart);

    void delete(int elementNum);

    void deleteAll();

    void set(int elementNum, String stringPart, Image imagePart);

    boolean isSelected(int elementNum);

    int getSelectedIndex();

    int getSelectedFlags(boolean[] selectedArray);

    void setSelectedIndex(int elementNum, boolean selected);

    void setSelectedFlags(boolean[] selectedArray);

    void setFitPolicy(int fitPolicy);

    int getFitPolicy();
}

package javax.microedition.lcdui;

public class Form extends Screen {
    public Form(String title) {
        setTitle(title);
    }

    public Form(String title, Item[] items) {
        setTitle(title);
        if (items != null) {
            for (int index = 0; index < items.length; index++) {
                append(items[index]);
            }
        }
    }

    public native int append(Item item);

    public int append(String string) {
        return append(new StringItem(null, string));
    }

    public int append(Image image) {
        return append(new ImageItem(null, image, Item.LAYOUT_DEFAULT, null));
    }

    public native void insert(int itemNum, Item item);

    public native void delete(int itemNum);

    public native void deleteAll();

    public native void set(int itemNum, Item item);

    public native Item get(int itemNum);

    public native int size();

    public native void setItemStateListener(ItemStateListener listener);
}

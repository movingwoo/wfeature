package javax.microedition.lcdui;

public class ImageItem extends Item {
    public ImageItem(String label, Image image, int layout, String altText) {
        setLabel(label);
        setLayout(layout);
        initImage(image, altText);
    }

    public ImageItem(String label, Image image, int layout, String altText, int appearanceMode) {
        this(label, image, layout, altText);
    }

    private native void initImage(Image image, String altText);

    public native Image getImage();

    public native void setImage(Image image);

    public native String getAltText();

    public native void setAltText(String altText);

    public int getAppearanceMode() {
        return PLAIN;
    }
}

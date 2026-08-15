package javax.microedition.lcdui;

public class Alert extends Screen {
    public static final int FOREVER = -2;

    public Alert(String title) {
        this(title, null, null, null);
    }

    public Alert(String title, String alertText, Image alertImage, AlertType alertType) {
        setTitle(title);
        initAlert(alertText, alertImage, alertType);
    }

    private native void initAlert(String alertText, Image alertImage, AlertType alertType);

    public native String getString();

    public native void setString(String alertText);

    public native Image getImage();

    public native void setImage(Image alertImage);

    public native AlertType getType();

    public native void setType(AlertType alertType);

    public native int getTimeout();

    public native void setTimeout(int time);

    public native int getDefaultTimeout();
}

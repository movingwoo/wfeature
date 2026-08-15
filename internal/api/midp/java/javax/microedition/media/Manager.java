package javax.microedition.media;

import java.io.IOException;
import java.io.InputStream;

public class Manager {
    public static final String TONE_DEVICE_LOCATOR = "device://tone";

    private Manager() {
    }

    public static native Player createPlayer(InputStream stream, String type) throws IOException, MediaException;

    public static native Player createPlayer(String locator) throws IOException, MediaException;

    public static native void playTone(int note, int duration, int volume) throws MediaException;

    public static native String[] getSupportedContentTypes(String protocol);

    public static native String[] getSupportedProtocols(String contentType);
}

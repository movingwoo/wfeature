package javax.microedition.lcdui;

/**
 * Base of the screens the runtime draws itself: Form, TextBox, List, Alert.
 * A Canvas owns its pixels; a Screen does not, so the Host renders its title,
 * content, and command labels.
 */
public abstract class Screen extends Displayable {
    protected Screen() {
    }
}

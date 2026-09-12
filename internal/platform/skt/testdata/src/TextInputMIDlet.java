import javax.microedition.lcdui.Command;
import javax.microedition.lcdui.CommandListener;
import javax.microedition.lcdui.Display;
import javax.microedition.lcdui.Displayable;
import javax.microedition.lcdui.TextBox;
import javax.microedition.lcdui.TextField;
import javax.microedition.midlet.MIDlet;

/** A small packaged target for Host keyboard and IME acceptance tests. */
public final class TextInputMIDlet extends MIDlet implements CommandListener {
    private Display display;
    private TextBox first;
    private TextBox second;
    private Command next;
    private TextBox active;

    protected void startApp() {
        if (display == null) {
            display = Display.getDisplay(this);
            first = new TextBox("Input", "", 16, TextField.ANY);
            second = new TextBox("Second", "", 4, TextField.ANY);
            next = new Command("Next", Command.OK, 1);
            first.addCommand(next);
            first.setCommandListener(this);
            active = first;
        }
        display.setCurrent(active);
    }

    protected void pauseApp() {
    }

    protected void destroyApp(boolean unconditional) {
    }

    public void commandAction(Command command, Displayable displayable) {
        if (command == next && displayable == first) {
            active = second;
            display.setCurrent(active);
        }
    }
}

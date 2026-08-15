import javax.microedition.lcdui.Alert;
import javax.microedition.lcdui.AlertType;
import javax.microedition.lcdui.Choice;
import javax.microedition.lcdui.ChoiceGroup;
import javax.microedition.lcdui.Command;
import javax.microedition.lcdui.CommandListener;
import javax.microedition.lcdui.Display;
import javax.microedition.lcdui.Displayable;
import javax.microedition.lcdui.Form;
import javax.microedition.lcdui.Graphics;
import javax.microedition.lcdui.Item;
import javax.microedition.lcdui.ItemStateListener;
import javax.microedition.lcdui.List;
import javax.microedition.lcdui.StringItem;
import javax.microedition.lcdui.TextBox;
import javax.microedition.lcdui.TextField;
import javax.microedition.lcdui.Ticker;
import javax.microedition.lcdui.game.GameCanvas;
import javax.microedition.midlet.MIDlet;

/**
 * Exercises the high-level lcdui surface: commands and their listener, a
 * List, a Form with items, a TextBox, an Alert, and a GameCanvas buffer.
 */
public final class UIMIDlet extends MIDlet implements CommandListener, ItemStateListener {
    private static Display display;
    private static List menu;
    private static Form form;
    private static TextBox textBox;
    private static Alert alert;
    private static Buffer buffer;

    private static ChoiceGroup group;
    private static TextField field;

    private static Command select;
    private static Command back;
    private static Command third;

    private static String lastCommand = "";
    private static int commandCount;
    private static int itemChanges;

    public UIMIDlet() {
        display = Display.getDisplay(this);
    }

    protected void startApp() {
        select = new Command("확인", Command.OK, 1);
        back = new Command("뒤로", Command.BACK, 2);
        third = new Command("도움말", Command.HELP, 3);

        menu = new List("메뉴", Choice.IMPLICIT, new String[] { "새 게임", "이어하기", "설정" }, null);
        menu.setTicker(new Ticker("환영합니다"));
        menu.addCommand(select);
        menu.addCommand(back);
        menu.setCommandListener(this);

        form = new Form("설정");
        form.append(new StringItem("제목", "설명 문구"));
        group = new ChoiceGroup("옵션", Choice.MULTIPLE, new String[] { "소리", "진동" }, null);
        form.append(group);
        field = new TextField("이름", "홍길동", 10, TextField.ANY);
        form.append(field);
        form.setItemStateListener(this);
        form.setCommandListener(this);

        textBox = new TextBox("메모", "abc", 16, TextField.ANY);
        alert = new Alert("알림", "저장했습니다", null, AlertType.INFO);
        buffer = new Buffer();

        display.setCurrent(menu);
    }

    protected void pauseApp() {
    }

    protected void destroyApp(boolean unconditional) {
    }

    public void commandAction(Command command, Displayable displayable) {
        lastCommand = command.getLabel();
        commandCount++;
    }

    public void itemStateChanged(Item item) {
        itemChanges++;
    }

    public static String lastCommand() {
        return lastCommand;
    }

    public static int commandCount() {
        return commandCount;
    }

    public static int itemChanges() {
        return itemChanges;
    }

    public static void showMenu() {
        display.setCurrent(menu);
    }

    public static void showForm() {
        display.setCurrent(form);
    }

    public static void showTextBox() {
        display.setCurrent(textBox);
    }

    public static void showAlert() {
        display.setCurrent(alert);
    }

    public static void showBuffer() {
        display.setCurrent(buffer);
    }

    public static void addThirdCommand() {
        menu.addCommand(third);
    }

    public static int menuSelection() {
        return menu.getSelectedIndex();
    }

    public static String menuSelectionText() {
        int index = menu.getSelectedIndex();
        return index < 0 ? "" : menu.getString(index);
    }

    /** groupFlags packs the multiple-choice selection into a bit field. */
    public static int groupFlags() {
        boolean[] flags = new boolean[group.size()];
        group.getSelectedFlags(flags);
        int packed = 0;
        for (int index = 0; index < flags.length; index++) {
            if (flags[index]) {
                packed |= 1 << index;
            }
        }
        return packed;
    }

    /** textState exercises the shared TextBox/TextField text operations. */
    public static String textState() {
        textBox.insert("XY", 1);
        textBox.delete(0, 1);
        char[] chars = new char[textBox.size()];
        int copied = textBox.getChars(chars);
        StringBuffer result = new StringBuffer();
        result.append(new String(chars, 0, copied));
        result.append('|');
        result.append(textBox.size());
        result.append('|');
        result.append(textBox.getMaxSize());
        result.append('|');
        result.append(field.getString());
        return result.toString();
    }

    /** typedText reports what the keypad put into the TextBox. */
    public static String typedText() {
        return textBox.getString();
    }

    public static String alertState() {
        alert.setTimeout(Alert.FOREVER);
        StringBuffer result = new StringBuffer();
        result.append(alert.getString());
        result.append('|');
        result.append(alert.getTimeout());
        result.append('|');
        result.append(alert.getType() == AlertType.INFO ? "info" : "other");
        return result.toString();
    }

    /** commandState reports the label, type and priority round-trip. */
    public static String commandState() {
        StringBuffer result = new StringBuffer();
        result.append(select.getLabel());
        result.append('|');
        result.append(select.getCommandType());
        result.append('|');
        result.append(select.getPriority());
        return result.toString();
    }

    /** drawBuffer paints a solid red rectangle and pushes it to the display. */
    public static void drawBuffer() {
        Graphics graphics = buffer.bufferGraphics();
        graphics.setColor(0xff0000);
        graphics.fillRect(0, 0, buffer.getWidth(), buffer.getHeight());
        buffer.flushGraphics();
    }

    public static int bufferKeyStates() {
        return buffer.getKeyStates();
    }

    static final class Buffer extends GameCanvas {
        Buffer() {
            super(false);
        }

        Graphics bufferGraphics() {
            return getGraphics();
        }
    }
}

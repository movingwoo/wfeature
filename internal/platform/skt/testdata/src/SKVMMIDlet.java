import com.skt.m.BackLight;
import com.skt.m.Graphics2D;
import com.skt.m.MathFP;
import com.skt.m.ProgressBar;
import com.skt.m.Vibration;
import com.skt.m3d.Object3D;
import com.xce.io.XFile;
import com.xce.lcdui.TextComponent;
import com.xce.lcdui.TextComponentHandler;
import com.xce.lcdui.Toolkit;
import com.xce.lcdui.XTextField;
import javax.microedition.lcdui.Canvas;
import javax.microedition.lcdui.Display;
import javax.microedition.lcdui.Graphics;
import javax.microedition.lcdui.Image;
import javax.microedition.midlet.MIDlet;
import java.util.Timer;
import java.util.TimerTask;

/** Exercises the SKVM class surface the SKT platform adds. */
public final class SKVMMIDlet extends MIDlet {
    private static Display display;
    private static Screen screen;
    private static String failure = "";

    public SKVMMIDlet() {
        display = Display.getDisplay(this);
    }

    protected void startApp() {
        screen = new Screen();
        display.setCurrent(screen);
    }

    protected void pauseApp() {
    }

    protected void destroyApp(boolean unconditional) {
    }

    public static String failure() {
        return failure;
    }

    /** fixedPointState checks the nine-decimal fixed point round-trips. */
    public static String fixedPointState() {
        long one = MathFP.parseFP(1);
        long two = MathFP.parseFP(2);
        StringBuffer result = new StringBuffer();
        result.append(one);
        result.append('|');
        result.append(MathFP.add(one, two));
        result.append('|');
        result.append(MathFP.multiply(two, MathFP.parseFP(3)));
        result.append('|');
        result.append(MathFP.divide(one, two));
        result.append('|');
        result.append(MathFP.toLong(MathFP.parseFP(7)));
        result.append('|');
        result.append(MathFP.sqrt(MathFP.parseFP(9)));
        result.append('|');
        result.append(MathFP.toStringLF(MathFP.divide(one, two), 2));
        return result.toString();
    }

    public static boolean divideByZeroThrows() {
        try {
            MathFP.divide(MathFP.parseFP(1), 0);
            return false;
        } catch (ArithmeticException expected) {
            return true;
        }
    }

    /** deviceState round-trips the backlight color and the progress bar. */
    public static String deviceState() {
        BackLight.setColor(0x123456);
        Vibration.start(10, 5);
        Vibration.stop();
        ProgressBar bar = new ProgressBar("로딩");
        bar.setMaxValue(50);
        bar.setValue(80);
        StringBuffer result = new StringBuffer();
        result.append(BackLight.getColor());
        result.append('|');
        result.append(bar.getValue());
        result.append('|');
        result.append(bar.getMaxValue());
        result.append('|');
        result.append(Toolkit.getScreenWidth());
        result.append('x');
        result.append(Toolkit.getScreenHeight());
        return result.toString();
    }

    /** pixelState draws through Graphics2D and reads the pixels back. */
    public static String pixelState() {
        Image buffer = Image.createImage(4, 4);
        Graphics graphics = buffer.getGraphics();
        graphics.setColor(0x000000);
        graphics.fillRect(0, 0, 4, 4);
        Graphics2D surface = new Graphics2D(graphics);
        surface.setPixel(1, 1, 0x336699);
        int direct = surface.getPixel(1, 1);
        surface.invertRect(0, 0, 1, 1);
        int inverted = surface.getPixel(0, 0);
        Image captured = Graphics2D.captureLCD(0, 0, 2, 2);
        StringBuffer result = new StringBuffer();
        result.append(Integer.toString(direct));
        result.append('|');
        result.append(Integer.toString(inverted));
        result.append('|');
        result.append(captured.getWidth());
        result.append('x');
        result.append(captured.getHeight());
        return result.toString();
    }

    /** fileState writes a file, reopens it, and reads it back. */
    public static String fileState() {
        try {
            XFile out = new XFile("save.dat", XFile.WRITE);
            out.write(new byte[] { 1, 2, 3, 4 }, 0, 4);
            out.close();

            XFile in = new XFile("save.dat", XFile.READ);
            byte[] buffer = new byte[4];
            int count = in.read(buffer, 0, 4);
            int available = in.available();
            in.seek(1, XFile.SEEK_SET);
            byte[] second = new byte[1];
            in.read(second, 0, 1);
            in.close();

            StringBuffer result = new StringBuffer();
            result.append(count);
            result.append('|');
            result.append(buffer[3]);
            result.append('|');
            result.append(available);
            result.append('|');
            result.append(second[0]);
            result.append('|');
            result.append(XFile.exists("save.dat"));
            result.append('|');
            result.append(XFile.filesize("save.dat"));
            result.append('|');
            result.append(XFile.exists("absent.dat"));
            return result.toString();
        } catch (Exception failed) {
            failure = failed.getClass().getName() + ": " + failed.getMessage();
            return "";
        }
    }

    public static boolean missingFileThrows() {
        try {
            new XFile("absent.dat", XFile.READ);
            return false;
        } catch (Exception expected) {
            return true;
        }
    }

    /** meshState builds a mesh and reads the transform back. */
    public static String meshState() {
        Object3D mesh = new Object3D("cube");
        mesh.addVertex(0, 0, 0);
        mesh.addVertex(1000000000, 0, 0);
        mesh.addVertex(0, 1000000000, 0);
        mesh.addTriangle(0, 1, 2, 0xff0000);
        mesh.translate(5, 6, 7);
        mesh.scale(2000000000, 1000000000, 1000000000);
        int[] row0 = mesh.getMatrixRow0();
        int[] row1 = mesh.getMatrixRow1();
        StringBuffer result = new StringBuffer();
        result.append(mesh.getName());
        result.append('|');
        result.append(row0[0]);
        result.append('|');
        result.append(row0[3]);
        result.append('|');
        result.append(row1[1]);
        return result.toString();
    }

    public static boolean badTriangleThrows() {
        Object3D mesh = new Object3D("bad");
        mesh.addVertex(0, 0, 0);
        try {
            mesh.addTriangle(0, 9, 0, 0);
            return false;
        } catch (IndexOutOfBoundsException expected) {
            return true;
        }
    }

    /** textFieldState checks the field keeps what it is given. */
    public static String textFieldState() {
        XTextField field = new XTextField();
        field.setMaxSize(4);
        field.setText("abcdef");
        field.inputChar('z');
        field.setFocus(true);
        StringBuffer result = new StringBuffer();
        result.append(field.getText());
        result.append('|');
        result.append(field.getMaxSize());
        result.append('|');
        result.append(field.hasFocus());
        return result.toString();
    }

    /**
     * textInputState checks the vendor's text input: the handler a title
     * reaches through a static, the component it attaches to it, and the
     * field built with the four-argument constructor a name screen uses.
     */
    public static String textInputState() {
        TextComponentHandler handler = TextComponentHandler.getTextComponentHandler();
        boolean same = handler == TextComponentHandler.getTextComponentHandler();
        Field component = new Field();
        handler.setTextComponent(component);

        // Two presses of the same key cycle one character; a different key
        // starts a new one, '#' deletes and '*' changes the mode.
        boolean took = handler.keyPressed('2');
        handler.keyPressed('2');
        handler.keyPressed('3');
        handler.keyPressed('#');
        handler.keyPressed('*');
        int mode = handler.getInputMode();
        handler.keyPressed('2');
        boolean release = handler.keyReleased('2');
        handler.setTextComponent(null);
        screen.repaintIM();

        // The platform's own field keeps the text itself, and types the same
        // way. It starts from the constructor a name screen uses.
        XTextField field = new XTextField("ab", 6, 0, screen);
        field.setBounds(1, 2, 3, 4);
        field.keyPressed('7');
        StringBuffer result = new StringBuffer();
        result.append(same);
        result.append('|');
        result.append(took);
        result.append('|');
        result.append(release);
        result.append('|');
        result.append(mode);
        result.append('|');
        result.append(component.text());
        result.append('|');
        result.append(field.getText());
        return result.toString();
    }

    /**
     * timerState schedules a repeating task and waits for it on the thread
     * that scheduled it, which is what a title's own loop does while a timer
     * runs beside it.
     */
    public static String timerState() {
        Timer timer = new Timer();
        Tick tick = new Tick();
        timer.scheduleAtFixedRate(tick, 1, 1);
        for (int waited = 0; tick.runs < 2 && waited < 500; waited++) {
            try {
                Thread.sleep(10);
            } catch (InterruptedException interrupted) {
                break;
            }
        }
        boolean ran = tick.runs >= 2;
        boolean scheduled = tick.scheduledExecutionTime() > 0;
        timer.cancel();
        boolean stopped = tick.cancel();
        StringBuffer result = new StringBuffer();
        result.append(ran);
        result.append('|');
        result.append(scheduled);
        result.append('|');
        result.append(stopped);
        return result.toString();
    }

    /** Tick counts what the timer thread came back for. */
    static final class Tick extends TimerTask {
        int runs;

        public void run() {
            runs++;
        }
    }

    /** Field is a title's own text buffer, which the input method edits. */
    static final class Field implements TextComponent {
        private final char[] buffer = new char[16];
        private int length;
        private int caret;

        String text() {
            StringBuffer out = new StringBuffer();
            for (int index = 0; index < length; index++) {
                out.append(buffer[index]);
            }
            return out.toString();
        }

        public int getCaretPosition() {
            return caret;
        }

        public int getConstraints() {
            return 0;
        }

        public int getMaxSize() {
            return 8;
        }

        public int size() {
            return length;
        }

        public void insert(char character) {
            if (length < buffer.length) {
                buffer[length++] = character;
                caret = length;
            }
        }

        public void delete() {
            if (length > 0) {
                length--;
                caret = length;
            }
        }

        public void clear() {
            length = 0;
            caret = 0;
        }

        /** replace writes at caret - 1, which is what a cycling key needs. */
        public void replace(char character) {
            if (length > 0) {
                buffer[length - 1] = character;
            } else {
                insert(character);
            }
        }

        public void moveCursor(int keyCode) {
        }

        public void setCaretPosition(int position) {
            caret = position;
        }

        public void setCaretVisible(boolean visible) {
        }

        public void repaint() {
        }

        public void repaintIM() {
        }
    }

    static final class Screen extends Canvas {
        protected void paint(Graphics graphics) {
            graphics.setColor(0x00ff00);
            graphics.fillRect(0, 0, getWidth(), getHeight());
        }
    }
}

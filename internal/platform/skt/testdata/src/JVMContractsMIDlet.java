import java.io.ByteArrayInputStream;
import java.io.DataInputStream;
import java.io.EOFException;
import java.io.IOException;
import javax.microedition.midlet.MIDlet;

/**
 * Runs the core-library and interpreter contracts the 2026-10-16 source review
 * corrected through a MIDlet JAR, the way a title reaches them. Each check sets
 * one bit so a single int reports which contract held.
 */
public final class JVMContractsMIDlet extends MIDlet {
    private static int flags;
    private static String failure = "";

    /** An interface whose constant is an array, built by its initializer. */
    interface Table {
        int[] VALUES = {7, 8, 9};
    }

    /** Reads the interface's array through the class that implements it. */
    static final class Reader implements Table {
        static int first() {
            return VALUES[0];
        }
    }

    /** A synchronized method whose string concatenation calls its own synchronized toString. */
    static final class Locked {
        synchronized String describe() {
            return "x" + this;
        }

        public synchronized String toString() {
            return "y";
        }
    }

    protected void startApp() {
    }

    protected void pauseApp() {
    }

    protected void destroyApp(boolean unconditional) {
    }

    public static String failure() {
        return failure;
    }

    private static void check(int bit, boolean ok, String name) {
        if (ok) {
            flags |= 1 << bit;
        } else if (failure.length() == 0) {
            failure = name;
        }
    }

    public static int run() {
        flags = 0;
        failure = "";

        // An interface's initializer runs before its array is read through an
        // implementing class.
        try {
            check(0, Reader.first() == 7, "interface static");
        } catch (RuntimeException e) {
            check(0, false, "interface static: " + e);
        }

        // String concatenation inside a synchronized method may call a
        // synchronized toString on the same object.
        check(1, "xy".equals(new Locked().describe()), "synchronized toString");

        // End of a data stream is an EOFException.
        try {
            new DataInputStream(new ByteArrayInputStream(new byte[] {1})).readInt();
            check(2, false, "readInt past the end returned");
        } catch (EOFException e) {
            check(2, true, "");
        } catch (IOException e) {
            check(2, false, "readInt past the end: " + e);
        }

        // Integer parsing takes no plus sign and no surrounding spaces.
        boolean strict = false;
        try {
            Integer.parseInt("+1");
        } catch (NumberFormatException e) {
            try {
                Byte.parseByte(" 1");
            } catch (NumberFormatException again) {
                strict = true;
            }
        }
        check(3, strict, "lenient number parsing");

        // An empty message stays empty.
        String message = new Exception("").getMessage();
        check(4, message != null && message.length() == 0, "empty exception message");

        // A null String argument is a catchable NullPointerException.
        boolean thrown = false;
        try {
            "abc".indexOf((String) null);
        } catch (NullPointerException e) {
            thrown = true;
        }
        check(5, thrown && !"abc".equalsIgnoreCase(null), "null String argument");

        // A class has one Class object.
        Object one = new Locked();
        Object two = new Locked();
        check(6, one.getClass() == two.getClass() && one.getClass() == Locked.class, "canonical Class");

        // wait(0, n) with n > 0 is a timed wait, not a wait for ever.
        Object lock = new Object();
        try {
            synchronized (lock) {
                lock.wait(0, 1);
            }
            check(7, true, "");
        } catch (InterruptedException e) {
            check(7, false, "wait(0, 1): " + e);
        }
        return flags;
    }
}

import java.io.ByteArrayInputStream;
import java.io.DataInputStream;
import java.io.IOException;
import java.io.InputStream;

/**
 * Guest code that a library native calls back into while its caller is still
 * inside something: a monitor the caller holds, or the initializer of the
 * caller's own class. A StringBuffer append and String.valueOf ask an object
 * for its toString, and readUTF asks the stream for its bytes, so each callback
 * has to run as the caller rather than as a stranger waiting on it.
 */
public final class NestedText {
    public static String synchronizedAppend() {
        return new Sync().describe();
    }

    public static String synchronizedValueOf() {
        return new Sync().valueOf();
    }

    public static String synchronizedBufferAppend() {
        return new Sync().buffer();
    }

    public static String initializerAppend() {
        return Item.LABEL;
    }

    public static String lockedReadUTF() throws IOException {
        byte[] data = {0, 2, 'o', 'k'};
        return new Locked(new ByteArrayInputStream(data)).name();
    }

    static final class Sync {
        synchronized String describe() {
            return "x" + this;
        }

        synchronized String valueOf() {
            return String.valueOf(this);
        }

        synchronized String buffer() {
            return new StringBuffer().append(this).toString();
        }

        public synchronized String toString() {
            return "sync";
        }
    }

    static final class Item {
        static final String[] NAMES = {"zero", "one"};
        static final Item DEFAULT = new Item(1);
        static final String LABEL = "item:" + DEFAULT;

        private final int id;

        Item(int id) {
            this.id = id;
        }

        public String toString() {
            return NAMES[id];
        }
    }

    static final class Locked extends DataInputStream {
        Locked(InputStream in) {
            super(in);
        }

        public synchronized int read() throws IOException {
            return super.read();
        }

        synchronized String name() throws IOException {
            return readUTF();
        }
    }
}

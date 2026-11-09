import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInput;
import java.io.DataInputStream;
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStreamWriter;
import java.io.Reader;
import java.io.UTFDataFormatException;
import java.io.UnsupportedEncodingException;
import java.io.Writer;
import java.util.Enumeration;
import java.util.Hashtable;
import java.util.Vector;

/**
 * CLDC 1.1 members of the class library that a compiled title links through
 * its constant pool: String's region tests, the rest of StringBuffer, Vector's
 * and Hashtable's searches and text, the readers' mark pair, the writers,
 * DataInputStream's static readUTF, ByteArrayOutputStream's text and
 * Class.newInstance. Each method answers one line that the Go tests compare,
 * so a member that does not link, or answers otherwise, names itself.
 *
 * Every check compares the runtime with itself where the platform's charset
 * decides the bytes, so the same lines hold under UTF-8 and under EUC-KR.
 */
public final class LibraryMembers {
    /** An element with text and an equality of its own. */
    static final class Item {
        private final int number;

        Item(int number) {
            this.number = number;
        }

        public boolean equals(Object other) {
            return other instanceof Item && ((Item) other).number == number;
        }

        public int hashCode() {
            return number;
        }

        public String toString() {
            return "#" + number;
        }
    }

    /** A table that asks for the growth the class leaves to a subclass. */
    static final class Table extends Hashtable {
        void grow() {
            rehash();
        }
    }

    /** A reader of the title's own, which names the lock it synchronizes on. */
    static final class Lines extends Reader {
        Lines() {
            super();
        }

        Lines(Object lock) {
            super(lock);
        }

        Object lockObject() {
            return lock;
        }

        public int read(char[] buffer, int offset, int length) {
            return -1;
        }

        public void close() {
        }
    }

    /** A stream with no mark, as InputStream's own. */
    static final class Plain extends InputStream {
        private int left = 3;

        public int read() {
            return left-- > 0 ? 'z' : -1;
        }
    }

    /** A writer of the title's own, which keeps what it is handed. */
    static final class Sink extends Writer {
        final StringBuffer seen = new StringBuffer();

        Sink() {
            super();
        }

        Object lockObject() {
            return lock;
        }

        public void write(char[] buffer, int offset, int length) {
            seen.append(buffer, offset, length).append('/');
        }

        public void flush() {
            seen.append('F');
        }

        public void close() {
            seen.append('C');
        }
    }

    /** A DataInput of the title's own over an array, with the two reads readUTF makes. */
    static final class Record implements DataInput {
        private final byte[] data;
        private int position;

        Record(byte[] data) {
            this.data = data;
        }

        public int readUnsignedShort() throws IOException {
            if (position + 2 > data.length) {
                throw new EOFException();
            }
            int value = (data[position] & 0xff) << 8 | data[position + 1] & 0xff;
            position += 2;
            return value;
        }

        public void readFully(byte[] buffer, int offset, int length) throws IOException {
            if (position + length > data.length) {
                throw new EOFException();
            }
            System.arraycopy(data, position, buffer, offset, length);
            position += length;
        }

        public void readFully(byte[] buffer) throws IOException {
            readFully(buffer, 0, buffer.length);
        }

        public String readUTF() throws IOException {
            return DataInputStream.readUTF(this);
        }

        public int skipBytes(int count) throws IOException {
            throw new IOException("unused");
        }

        public boolean readBoolean() throws IOException {
            throw new IOException("unused");
        }

        public byte readByte() throws IOException {
            throw new IOException("unused");
        }

        public int readUnsignedByte() throws IOException {
            throw new IOException("unused");
        }

        public short readShort() throws IOException {
            throw new IOException("unused");
        }

        public char readChar() throws IOException {
            throw new IOException("unused");
        }

        public int readInt() throws IOException {
            throw new IOException("unused");
        }

        public long readLong() throws IOException {
            throw new IOException("unused");
        }

        public float readFloat() throws IOException {
            throw new IOException("unused");
        }

        public double readDouble() throws IOException {
            throw new IOException("unused");
        }

        public String readLine() throws IOException {
            throw new IOException("unused");
        }
    }

    /** Made by name, counting how often. */
    public static final class Made {
        static int made;

        public Made() {
            made++;
        }
    }

    /** No instance of it can be made. */
    public abstract static class Abstract {
        public Abstract() {
        }
    }

    /** Its only constructor takes an argument. */
    public static final class Arguments {
        public Arguments(int value) {
        }
    }

    /** Its constructor is its own. */
    public static final class Hidden {
        private Hidden() {
        }
    }

    /** Its constructor refuses. */
    public static final class Failing {
        public Failing() {
            throw new IllegalStateException("refused");
        }
    }

    /** An interface, which has no instances. */
    public interface Shape {
    }

    public static String text() {
        String line = "Score: HIGH score";
        StringBuffer out = new StringBuffer();
        out.append(line.regionMatches(true, 7, "high", 0, 4)).append(',');
        out.append(line.regionMatches(false, 7, "high", 0, 4)).append(',');
        out.append(line.regionMatches(false, 12, "score", 0, 5)).append(',');
        out.append(line.regionMatches(false, 13, "score", 0, 5)).append(',');
        out.append(line.regionMatches(true, 0, "xscore", 1, 5)).append(',');
        out.append(line.regionMatches(false, -1, "S", 0, 1)).append(',');
        out.append(line.regionMatches(false, 3, "anything", 100, -1)).append(',');
        out.append(line.regionMatches(false, 3, "abc", 1, 0)).append(',');
        out.append(line.regionMatches(false, 0, "xyz", 0, -5)).append(',');
        // Case is ISO Latin-1's alone, which is all CLDC's Character maps.
        out.append("été".regionMatches(true, 0, "ÉTÉ", 0, 3)).append(',');
        out.append("д".regionMatches(true, 0, "Д", 0, 1)).append(',');
        try {
            line.regionMatches(true, 0, null, 0, 1);
            out.append("none");
        } catch (NullPointerException e) {
            out.append("NPE");
        }
        out.append('|');
        out.append(line.startsWith("HIGH", 7)).append(',');
        out.append(line.startsWith("HIGH", 8)).append(',');
        out.append(line.startsWith("", 17)).append(',');
        out.append(line.startsWith("", 18)).append(',');
        out.append(line.startsWith("x", -1)).append(',');
        try {
            line.startsWith(null, 0);
            out.append("none");
        } catch (NullPointerException e) {
            out.append("NPE");
        }
        out.append('|');
        out.append(String.valueOf(true)).append('/').append(String.valueOf(false));
        return out.toString();
    }

    public static String buffer() {
        StringBuffer out = new StringBuffer();
        StringBuffer empty = new StringBuffer();
        out.append(empty.capacity()).append(',');
        empty.append("0123456789abcdefg");
        out.append(empty.capacity()).append(',');
        out.append(new StringBuffer("abc").capacity()).append(',');
        StringBuffer sized = new StringBuffer(5);
        out.append(sized.capacity()).append(',');
        sized.ensureCapacity(7);
        out.append(sized.capacity()).append(',');
        sized.ensureCapacity(100);
        out.append(sized.capacity()).append(',');
        sized.ensureCapacity(-1);
        sized.setLength(0);
        out.append(sized.capacity()).append(',');
        sized.setLength(101);
        out.append(sized.capacity()).append(',');
        try {
            new StringBuffer(-1);
            out.append("none");
        } catch (NegativeArraySizeException e) {
            out.append("NASE");
        }
        out.append('|');
        StringBuffer edit = new StringBuffer("ce");
        edit.insert(0, true);
        edit.insert(4, new char[] {'-', '+'});
        edit.insert(edit.length(), 12345678901L);
        edit.insert(0, (Object) new Item(7));
        edit.insert(0, (Object) null);
        out.append(edit.toString()).append(',');
        out.append(edit.reverse().toString()).append(',');
        try {
            edit.insert(-1, false);
            out.append("none");
        } catch (StringIndexOutOfBoundsException e) {
            out.append("SIOOBE");
        }
        out.append(',');
        // The offset is checked before the array is read.
        try {
            edit.insert(edit.length() + 1, (char[]) null);
            out.append("none");
        } catch (StringIndexOutOfBoundsException e) {
            out.append("SIOOBE");
        } catch (NullPointerException e) {
            out.append("NPE");
        }
        out.append(',');
        try {
            edit.insert(0, (char[]) null);
            out.append("none");
        } catch (NullPointerException e) {
            out.append("NPE");
        }
        out.append(',');
        // A pair is reversed unit by unit.
        StringBuffer pair = new StringBuffer("a😀b");
        pair.reverse();
        out.append(pair.charAt(1) == '\ude00' && pair.charAt(2) == '\ud83d');
        return out.toString();
    }

    public static String collections() {
        StringBuffer out = new StringBuffer();
        Vector items = new Vector();
        items.addElement(new Item(1));
        items.addElement(null);
        items.addElement(new Item(2));
        items.addElement(new Item(1));
        out.append(items.indexOf(new Item(1), 1)).append(',');
        out.append(items.indexOf(null, 0)).append(',');
        out.append(items.indexOf(new Item(2), 3)).append(',');
        out.append(items.indexOf(new Item(1), 9)).append(',');
        out.append(items.lastIndexOf(new Item(1), 2)).append(',');
        out.append(items.lastIndexOf(null, 3)).append(',');
        out.append(items.lastIndexOf(new Item(1), -1)).append(',');
        try {
            items.indexOf(new Item(1), -1);
            out.append("none");
        } catch (ArrayIndexOutOfBoundsException e) {
            out.append("AIOOBE");
        }
        out.append(',');
        try {
            items.lastIndexOf(new Item(1), 4);
            out.append("none");
        } catch (ArrayIndexOutOfBoundsException e) {
            out.append("AIOOBE");
        } catch (IndexOutOfBoundsException e) {
            out.append("IOOBE");
        }
        out.append('|');
        Vector inner = new Vector();
        inner.addElement("x");
        items.addElement(inner);
        out.append(items.toString()).append('|');
        out.append(String.valueOf(new Vector())).append('|');
        Hashtable table = new Hashtable();
        table.put("one", new Item(1));
        table.put("two", "2");
        out.append(table.contains(new Item(1))).append(',');
        out.append(table.contains("2")).append(',');
        out.append(table.contains("one")).append(',');
        try {
            table.contains(null);
            out.append("none");
        } catch (NullPointerException e) {
            out.append("NPE");
        }
        out.append(',');
        out.append(table.toString()).append(',');
        Table grown = new Table();
        grown.put("a", "1");
        grown.put("b", "2");
        grown.put("c", "3");
        grown.grow();
        out.append(grown.toString()).append(',');
        for (Enumeration keys = grown.keys(); keys.hasMoreElements();) {
            out.append(keys.nextElement());
        }
        out.append(',').append(grown.get("b")).append(',');
        out.append(String.valueOf(new Hashtable()));
        return out.toString();
    }

    public static String readers() throws IOException {
        StringBuffer out = new StringBuffer();
        Object shared = new Object();
        Lines own = new Lines();
        out.append(own.lockObject() == own).append(',');
        out.append(new Lines(shared).lockObject() == shared).append(',');
        try {
            new Lines(null);
            out.append("none");
        } catch (NullPointerException e) {
            out.append("NPE");
        }
        out.append(',');
        out.append(own.markSupported()).append(',');
        try {
            own.mark(1);
            out.append("none");
        } catch (IOException e) {
            out.append("IOE");
        }
        out.append(',');
        try {
            own.reset();
            out.append("none");
        } catch (IOException e) {
            out.append("IOE");
        }
        out.append('|');
        InputStreamReader reader = new InputStreamReader(new ByteArrayInputStream("ab가cd".getBytes()));
        out.append(reader.markSupported()).append(',');
        out.append((char) reader.read());
        reader.mark(8);
        out.append((char) reader.read()).append((char) reader.read());
        reader.reset();
        out.append((char) reader.read()).append((char) reader.read()).append((char) reader.read()).append(',');
        InputStreamReader plain = new InputStreamReader(new Plain());
        out.append(plain.markSupported()).append(',');
        try {
            plain.mark(1);
            out.append("none");
        } catch (IOException e) {
            out.append("IOE");
        }
        out.append(',');
        try {
            plain.reset();
            out.append("none");
        } catch (IOException e) {
            out.append("IOE");
        }
        out.append(',');
        reader.close();
        out.append(reader.markSupported()).append(',');
        try {
            reader.reset();
            out.append("none");
        } catch (IOException e) {
            out.append("IOE");
        }
        return out.toString();
    }

    public static String writers() throws IOException {
        StringBuffer out = new StringBuffer();
        Sink sink = new Sink();
        sink.write('h');
        sink.write(new char[] {'i', '!'});
        sink.write("abc");
        sink.write("abcdef", 2, 3);
        out.append(sink.seen.toString()).append(',');
        out.append(sink.lockObject() == sink).append(',');
        try {
            sink.write("abc", 2, 2);
            out.append("none");
        } catch (StringIndexOutOfBoundsException e) {
            out.append("SIOOBE");
        }
        out.append(',');
        try {
            sink.write((String) null);
            out.append("none");
        } catch (NullPointerException e) {
            out.append("NPE");
        }
        out.append(',');
        try {
            sink.write((char[]) null);
            out.append("none");
        } catch (NullPointerException e) {
            out.append("NPE");
        }
        out.append('|');
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        OutputStreamWriter writer = new OutputStreamWriter(bytes);
        writer.write("ok가");
        writer.write('!');
        writer.write(new char[] {'x', 'y', 'z'}, 1, 2);
        writer.flush();
        out.append(same(bytes.toByteArray(), "ok가!yz".getBytes())).append(',');
        out.append(bytes.toString().equals("ok가!yz")).append(',');
        out.append(bytes.toString().equals(new String(bytes.toByteArray()))).append(',');
        ByteArrayOutputStream utf = new ByteArrayOutputStream();
        OutputStreamWriter utf8 = new OutputStreamWriter(utf, "UTF-8");
        utf8.write("é");
        // The first half of a pair waits for the second.
        utf8.write('\ud83d');
        utf8.write('\ude00');
        utf8.close();
        out.append(hex(utf.toByteArray())).append(',');
        try {
            utf8.write('x');
            out.append("none");
        } catch (IOException e) {
            out.append("IOE");
        }
        out.append(',');
        utf8.close();
        try {
            new OutputStreamWriter(utf, "NOT-A-CHARSET");
            out.append("none");
        } catch (UnsupportedEncodingException e) {
            out.append("UEE");
        }
        out.append(',');
        try {
            new OutputStreamWriter(null);
            out.append("none");
        } catch (NullPointerException e) {
            out.append("NPE");
        }
        out.append(',');
        try {
            new OutputStreamWriter(utf, (String) null);
            out.append("none");
        } catch (NullPointerException e) {
            out.append("NPE");
        }
        return out.toString();
    }

    public static String utf() throws IOException {
        StringBuffer out = new StringBuffer();
        byte[] record = {0, 7, 'h', 'i', (byte) 0xc3, (byte) 0xa9, (byte) 0xea, (byte) 0xb0, (byte) 0x80};
        out.append(DataInputStream.readUTF(new Record(record))).append(',');
        out.append(new Record(record).readUTF()).append(',');
        out.append(DataInputStream.readUTF(new DataInputStream(new ByteArrayInputStream(record)))).append(',');
        // A zero byte and the long form of one are characters to the class
        // library, as they are here.
        String lenient = DataInputStream.readUTF(new Record(new byte[] {0, 3, 'a', (byte) 0xc0, (byte) 0x80}));
        out.append(lenient.length()).append((int) lenient.charAt(1)).append(',');
        out.append(DataInputStream.readUTF(new Record(new byte[] {0, 1, 0})).length()).append(',');
        try {
            DataInputStream.readUTF(new Record(new byte[] {0, 1, (byte) 0x80}));
            out.append("none");
        } catch (UTFDataFormatException e) {
            out.append("UTFDFE");
        }
        out.append(',');
        try {
            DataInputStream.readUTF(new Record(new byte[] {0, 2, (byte) 0xe0, (byte) 0x80}));
            out.append("none");
        } catch (UTFDataFormatException e) {
            out.append("UTFDFE");
        }
        out.append(',');
        try {
            DataInputStream.readUTF(new DataInputStream(new ByteArrayInputStream(new byte[] {0, 5, 'a'})));
            out.append("none");
        } catch (EOFException e) {
            out.append("EOF");
        }
        out.append(',');
        try {
            DataInputStream.readUTF(null);
            out.append("none");
        } catch (NullPointerException e) {
            out.append("NPE");
        }
        return out.toString();
    }

    public static String classes() throws ClassNotFoundException {
        StringBuffer out = new StringBuffer();
        out.append(made("LibraryMembers$Made")).append(',');
        out.append(Made.made).append(',');
        out.append(made("LibraryMembers$Abstract")).append(',');
        out.append(made("LibraryMembers$Arguments")).append(',');
        out.append(made("LibraryMembers$Hidden")).append(',');
        out.append(made("LibraryMembers$Failing")).append(',');
        out.append(made("LibraryMembers$Shape")).append(',');
        out.append(made("[I")).append(',');
        out.append(made("java.util.Vector"));
        return out.toString();
    }

    /** Every line, in the order the methods above are written. */
    public static String report() throws IOException, ClassNotFoundException {
        return text() + "\n" + buffer() + "\n" + collections() + "\n" + readers() + "\n" + writers() + "\n"
                + utf() + "\n" + classes();
    }

    private static String made(String name) throws ClassNotFoundException {
        Class type = Class.forName(name);
        try {
            return type.newInstance().getClass().getName();
        } catch (InstantiationException e) {
            return "InstantiationException";
        } catch (IllegalAccessException e) {
            return "IllegalAccessException";
        } catch (IllegalStateException e) {
            return "IllegalStateException:" + e.getMessage();
        }
    }

    private static boolean same(byte[] left, byte[] right) {
        if (left.length != right.length) {
            return false;
        }
        for (int index = 0; index < left.length; index++) {
            if (left[index] != right[index]) {
                return false;
            }
        }
        return true;
    }

    private static String hex(byte[] data) {
        StringBuffer text = new StringBuffer();
        for (int index = 0; index < data.length; index++) {
            text.append(Integer.toHexString((data[index] & 0xff) | 0x100).substring(1));
        }
        return text.toString();
    }
}

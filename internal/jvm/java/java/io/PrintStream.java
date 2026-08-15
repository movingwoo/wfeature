package java.io;

/**
 * The stream behind {@code System.out} and {@code System.err}. Shipped titles
 * are full of leftover debug printing, and a game that cannot resolve the field
 * dies in whatever method happened to contain the call, so this exists to keep
 * that printing harmless rather than to be a console.
 *
 * Each call is one line at the logging boundary, including {@code print}, which
 * a real stream would have held until a newline arrived. Nothing in a game reads
 * back what it printed, and a partial line that never arrives is worse to debug
 * than one that arrives early.
 */
public class PrintStream extends OutputStream {
    private final int stream;

    public PrintStream(int stream) {
        this.stream = stream;
    }

    public void write(int value) {
        emit(stream, String.valueOf((char) value));
    }

    public void print(boolean value) {
        emit(stream, "" + value);
    }

    public void print(char value) {
        emit(stream, String.valueOf(value));
    }

    public void print(int value) {
        emit(stream, String.valueOf(value));
    }

    public void print(long value) {
        emit(stream, "" + value);
    }

    public void print(char[] value) {
        emit(stream, value == null ? "null" : new String(value));
    }

    public void print(String value) {
        emit(stream, value == null ? "null" : value);
    }

    public void print(Object value) {
        emit(stream, value == null ? "null" : value.toString());
    }

    public void println() {
        emit(stream, "");
    }

    public void println(boolean value) {
        print(value);
    }

    public void println(char value) {
        print(value);
    }

    public void println(int value) {
        print(value);
    }

    public void println(long value) {
        print(value);
    }

    public void println(char[] value) {
        print(value);
    }

    public void println(String value) {
        print(value);
    }

    public void println(Object value) {
        print(value);
    }

    public void flush() {
    }

    public void close() {
    }

    private static native void emit(int stream, String text);
}

package java.lang;

/**
 * The unsynchronized twin of StringBuffer. CLDC never had it, but javac emits
 * it for every string concatenation in a class compiled against a modern JDK —
 * including this repository's own fixtures — so the runtime provides it.
 */
public final class StringBuilder {
    public StringBuilder() {
    }

    public StringBuilder(int capacity) {
    }

    public StringBuilder(String value) {
    }

    public native StringBuilder append(char value);
    public native StringBuilder append(int value);
    public native StringBuilder append(long value);
    public native StringBuilder append(boolean value);
    public native StringBuilder append(String value);
    public native StringBuilder append(Object value);
    public native StringBuilder delete(int start, int end);
    public native void setLength(int length);
    public native int length();
    public native String toString();
}

package java.lang;

public final class StringBuffer {
    public StringBuffer() {
    }

    public StringBuffer(String value) {
    }

    public native StringBuffer append(char value);
    public native StringBuffer append(int value);
    public native StringBuffer append(String value);
    public native StringBuffer delete(int start, int end);
    public native int length();
    public native String toString();
}

package java.lang;

public final class String {
    public String() {
    }

    public String(String value) {
    }

    public String(char[] data) {
    }

    public String(char[] data, int offset, int count) {
    }

    public String(byte[] data) {
    }

    public String(byte[] data, int offset, int length) {
    }

    public String(byte[] data, String encoding) throws java.io.IOException {
    }

    public native int length();
    public native char charAt(int index);
    public native boolean equals(Object other);
    public native int hashCode();
    public native String concat(String other);
    public native byte[] getBytes();
    public native int indexOf(int character);
    public native int indexOf(String text);
    public native int indexOf(String text, int fromIndex);
    public native boolean startsWith(String prefix);
    public native String substring(int beginIndex);
    public native String substring(int beginIndex, int endIndex);
    public native int compareTo(String other);
    public native boolean equalsIgnoreCase(String other);
    public native boolean endsWith(String suffix);
    public native String toUpperCase();
    public native String toLowerCase();
    public native String trim();
    public native String toString();

    public static native String valueOf(char value);
    public static native String valueOf(int value);
    public static native String valueOf(Object value);
}

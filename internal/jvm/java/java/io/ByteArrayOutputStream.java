package java.io;

public class ByteArrayOutputStream extends OutputStream {
    protected byte[] buf;
    protected int count;

    public ByteArrayOutputStream() {
        this(32);
    }

    public ByteArrayOutputStream(int size) {
        if (size < 0) {
            throw new IllegalArgumentException();
        }
        buf = new byte[size];
    }

    public void write(int value) {
        ensureCapacity(count + 1);
        buf[count] = (byte) value;
        count++;
    }

    public void write(byte[] buffer, int offset, int length) {
        if (offset < 0 || length < 0 || offset + length > buffer.length) {
            throw new ArrayIndexOutOfBoundsException();
        }
        ensureCapacity(count + length);
        System.arraycopy(buffer, offset, buf, count, length);
        count += length;
    }

    private void ensureCapacity(int capacity) {
        if (capacity <= buf.length) {
            return;
        }
        int next = buf.length * 2;
        if (next < capacity) {
            next = capacity;
        }
        byte[] grown = new byte[next];
        System.arraycopy(buf, 0, grown, 0, count);
        buf = grown;
    }

    public byte[] toByteArray() {
        byte[] out = new byte[count];
        System.arraycopy(buf, 0, out, 0, count);
        return out;
    }

    public int size() {
        return count;
    }

    public void reset() {
        count = 0;
    }
}

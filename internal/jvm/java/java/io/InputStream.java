package java.io;

public abstract class InputStream {
    protected InputStream() {
    }

    public abstract int read() throws IOException;

    public int read(byte[] data) throws IOException {
        return read(data, 0, data.length);
    }

    public int read(byte[] data, int offset, int length) throws IOException {
        if (data == null) {
            throw new NullPointerException();
        }
        if (offset < 0 || length < 0 || offset > data.length - length) {
            throw new IndexOutOfBoundsException();
        }
        if (length == 0) {
            return 0;
        }
        int value = read();
        if (value < 0) {
            return -1;
        }
        data[offset] = (byte)value;
        int count = 1;
        while (count < length) {
            value = read();
            if (value < 0) {
                break;
            }
            data[offset + count] = (byte)value;
            count++;
        }
        return count;
    }

    public long skip(long count) throws IOException {
        long remaining = count;
        while (remaining > 0) {
            if (read() < 0) {
                break;
            }
            remaining--;
        }
        return count - remaining;
    }

    public int available() throws IOException {
        return 0;
    }

    public void close() throws IOException {
    }

    public void mark(int limit) {
    }

    public boolean markSupported() {
        return false;
    }

    public void reset() throws IOException {
        throw new IOException("mark/reset is not supported");
    }
}

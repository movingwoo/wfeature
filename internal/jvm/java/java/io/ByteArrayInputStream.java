package java.io;

public class ByteArrayInputStream extends InputStream {
    private byte[] data;
    private int position;

    public ByteArrayInputStream(byte[] data) {
        this.data = data;
    }

    public int read() {
        if (position >= data.length) {
            return -1;
        }
        return data[position++] & 0xff;
    }

    public int read(byte[] output, int offset, int length) {
        if (output == null) {
            throw new NullPointerException();
        }
        if (offset < 0 || length < 0 || offset > output.length - length) {
            throw new IndexOutOfBoundsException();
        }
        if (position >= data.length) {
            return length == 0 ? 0 : -1;
        }
        int count = Math.min(length, data.length - position);
        System.arraycopy(data, position, output, offset, count);
        position += count;
        return count;
    }

    public long skip(long count) {
        // The superclass skips by reading a byte at a time, which a game
        // stepping over a multi-kilobyte chunk of its own archive pays for one
        // interpreted call per byte. The whole stream is already in memory.
        long available = data.length - position;
        long skipped = count < available ? count : available;
        if (skipped < 0) {
            return 0;
        }
        position += (int)skipped;
        return skipped;
    }

    public int available() {
        return data.length - position;
    }
}

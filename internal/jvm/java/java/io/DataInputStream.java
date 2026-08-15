package java.io;

public class DataInputStream extends InputStream {
    private InputStream input;

    public DataInputStream(InputStream input) {
        this.input = input;
    }

    public int read() throws IOException {
        return input.read();
    }

    public int read(byte[] data, int offset, int length) throws IOException {
        return input.read(data, offset, length);
    }

    public int available() throws IOException {
        return input.available();
    }

    public long skip(long count) throws IOException {
        return input.skip(count);
    }

    public void close() throws IOException {
        input.close();
    }

    public final int skipBytes(int count) throws IOException {
        return (int)input.skip(count);
    }

    public final boolean readBoolean() throws IOException {
        return readByte() != 0;
    }

    public byte readByte() throws IOException {
        return (byte)readRequired();
    }

    public short readShort() throws IOException {
        return (short)((readRequired() << 8) | readRequired());
    }

    public int readUnsignedShort() throws IOException {
        return (readRequired() << 8) | readRequired();
    }

    public int readInt() throws IOException {
        return (readRequired() << 24) | (readRequired() << 16)
            | (readRequired() << 8) | readRequired();
    }

    public long readLong() throws IOException {
        return ((long)readInt() << 32) | ((long)readInt() & 0xffffffffL);
    }

    public native String readUTF() throws IOException;

    private int readRequired() throws IOException {
        int value = input.read();
        if (value < 0) {
            throw new IOException("unexpected end of stream");
        }
        return value;
    }
}

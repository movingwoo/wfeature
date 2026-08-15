package java.io;

public class DataOutputStream extends OutputStream {
    protected OutputStream out;

    public DataOutputStream(OutputStream out) {
        this.out = out;
    }

    public void write(int value) throws IOException {
        out.write(value);
    }

    public void write(byte[] buffer, int offset, int length) throws IOException {
        out.write(buffer, offset, length);
    }

    public void flush() throws IOException {
        out.flush();
    }

    public void close() throws IOException {
        out.close();
    }

    public final void writeBoolean(boolean value) throws IOException {
        out.write(value ? 1 : 0);
    }

    public final void writeByte(int value) throws IOException {
        out.write(value);
    }

    public final void writeShort(int value) throws IOException {
        out.write(value >>> 8);
        out.write(value);
    }

    public final void writeChar(int value) throws IOException {
        writeShort(value);
    }

    public final void writeInt(int value) throws IOException {
        out.write(value >>> 24);
        out.write(value >>> 16);
        out.write(value >>> 8);
        out.write(value);
    }

    public final void writeLong(long value) throws IOException {
        writeInt((int) (value >>> 32));
        writeInt((int) value);
    }
}

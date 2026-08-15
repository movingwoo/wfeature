package java.io;

public abstract class OutputStream {
    public OutputStream() {
    }

    public abstract void write(int value) throws IOException;

    public void write(byte[] buffer) throws IOException {
        write(buffer, 0, buffer.length);
    }

    public void write(byte[] buffer, int offset, int length) throws IOException {
        if (offset < 0 || length < 0 || offset + length > buffer.length) {
            throw new ArrayIndexOutOfBoundsException();
        }
        for (int index = 0; index < length; index++) {
            write(buffer[offset + index]);
        }
    }

    public void flush() throws IOException {
    }

    public void close() throws IOException {
    }
}

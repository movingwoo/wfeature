package com.xce.io;

import java.io.IOException;
import java.io.OutputStream;

public class FileOutputStream extends OutputStream {
    private XFile file;

    public FileOutputStream(int handle) {
        this.file = new XFile(handle);
    }

    public FileOutputStream(String name) throws IOException {
        this.file = new XFile(name, XFile.WRITE);
    }

    public FileOutputStream(String name, boolean append) throws IOException {
        this.file = new XFile(name, XFile.WRITE);
        if (append) {
            file.seek(0, XFile.SEEK_END);
        }
    }

    public FileOutputStream(XFile file) {
        this.file = file;
    }

    public void close() {
        file.close();
    }

    public void flush() {
        file.flush();
    }

    public void write(int value) {
        write(new byte[] { (byte) value }, 0, 1);
    }

    public void write(byte[] data, int offset, int length) {
        file.write(data, offset, length);
    }
}

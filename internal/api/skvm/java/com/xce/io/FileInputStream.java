package com.xce.io;

import java.io.IOException;
import java.io.InputStream;

public class FileInputStream extends InputStream {
    private XFile file;
    private int mark;

    public FileInputStream(int handle) {
        this.file = new XFile(handle);
    }

    public FileInputStream(String name) throws IOException {
        this.file = new XFile(name, XFile.READ);
    }

    public FileInputStream(XFile file) {
        this.file = file;
    }

    public int available() {
        return file.available();
    }

    public void close() {
        file.close();
    }

    public void mark(int readLimit) {
        mark = file.seek(0, XFile.SEEK_CUR);
    }

    public boolean markSupported() {
        return true;
    }

    public void reset() {
        file.seek(mark, XFile.SEEK_SET);
    }

    public int read() {
        byte[] one = new byte[1];
        int count = file.read(one, 0, 1);
        return count <= 0 ? -1 : one[0] & 0xff;
    }

    public int read(byte[] data) {
        return read(data, 0, data.length);
    }

    public int read(byte[] data, int offset, int length) {
        int count = file.read(data, offset, length);
        return count <= 0 ? -1 : count;
    }

    public long skip(long count) {
        int before = file.seek(0, XFile.SEEK_CUR);
        int after = file.seek((int) count, XFile.SEEK_CUR);
        return after - before;
    }
}

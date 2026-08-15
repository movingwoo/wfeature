package com.xce.io;

import java.io.IOException;

/**
 * SKVM's file handle. Reads and writes go through the same Host save boundary
 * MIDP RMS uses, so one directory holds everything a title persists.
 */
public class XFile {
    public static final int READ = 1;
    public static final int WRITE = 2;
    public static final int READ_WRITE = 3;
    public static final int READ_DIRECTORY = 4;
    public static final int READ_RESOURCE = 8;

    public static final int SEEK_SET = 0;
    public static final int SEEK_CUR = 1;
    public static final int SEEK_END = 2;

    public XFile(int handle) {
        initHandle(handle);
    }

    public XFile(String name, int mode) throws IOException {
        initName(name, mode);
    }

    public XFile(String name, String mode) throws IOException {
        initName(name, modeBits(mode));
    }

    private static int modeBits(String mode) {
        if (mode == null) {
            return READ;
        }
        int bits = 0;
        if (mode.indexOf('r') >= 0) {
            bits |= READ;
        }
        if (mode.indexOf('w') >= 0) {
            bits |= WRITE;
        }
        if (mode.indexOf('a') >= 0) {
            bits |= WRITE;
        }
        return bits == 0 ? READ : bits;
    }

    private native void initHandle(int handle);

    private native void initName(String name, int mode) throws IOException;

    public native int available();
    public native void close();
    public native void flush();
    public native int read(byte[] data, int offset, int length);
    public native int write(byte[] data, int offset, int length);
    public native int seek(int offset, int whence);
    public native String readdir();

    public static native boolean exists(String name);
    public static native int filesize(String name);
    public static native int fsavail();
    public static native int fsused();
    public static native void mkdir(String name);
    public static native void rmdir(String name);
    public static native void rmrdir(String name);
    public static native int unlink(String name);
}

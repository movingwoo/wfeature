package java.lang;

import java.io.InputStream;

public final class Class {
    private Class() {
    }

    public native InputStream getResourceAsStream(String name);

    public native String getName();

    public native String toString();
}

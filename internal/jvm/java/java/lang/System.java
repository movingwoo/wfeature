package java.lang;

import java.io.PrintStream;

/**
 * The CLDC system class. Its static methods were already answered as natives
 * without any class metadata; what needs a class file is {@code out} and
 * {@code err}, because a field read has to resolve the class that holds it.
 *
 * {@code getProperty} is declared here and implemented by whichever platform is
 * running, since the properties are that platform's answer about its handset.
 */
public final class System {
    public static final PrintStream out = new PrintStream(0);
    public static final PrintStream err = new PrintStream(1);

    private System() {
    }

    public static native long currentTimeMillis();

    public static native int identityHashCode(Object object);

    public static native void arraycopy(Object source, int sourceOffset, Object destination, int destinationOffset, int length);

    public static native void gc();

    public static native String getProperty(String key);
}

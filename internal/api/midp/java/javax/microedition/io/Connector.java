package javax.microedition.io;

import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;

/**
 * The MIDP 2.0 connection factory. Every method here is refused by the
 * runtime; see the Go side for why a refusal beats a fabricated connection.
 */
public class Connector {
    public static final int READ = 1;
    public static final int WRITE = 2;
    public static final int READ_WRITE = 3;

    private Connector() {
    }

    public static native Connection open(String name) throws IOException;

    public static native Connection open(String name, int mode) throws IOException;

    public static native Connection open(String name, int mode, boolean timeouts) throws IOException;

    public static native InputStream openInputStream(String name) throws IOException;

    public static native DataInputStream openDataInputStream(String name) throws IOException;

    public static native OutputStream openOutputStream(String name) throws IOException;

    public static native DataOutputStream openDataOutputStream(String name) throws IOException;
}

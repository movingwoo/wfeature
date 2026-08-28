import java.io.ByteArrayInputStream;

/**
 * The class-library members a compiled title reaches through its constant pool
 * rather than through a native dispatch: the boxed flag and its two published
 * instances, the char-array append, the character replace, and a stream
 * subclass that reads the protected buffer instead of copying it back out.
 *
 * Every one of these was a member this runtime answered from Go and declared
 * nowhere, so resolving it through the class — which is all a compiled title
 * can do — found nothing.
 */
public final class CoreMembers {
    /** A stream subclass that hands its own buffer on, as a decoder does. */
    static final class Source extends ByteArrayInputStream {
        Source(byte[] data) {
            super(data);
        }

        int first() {
            return buf[0];
        }
    }

    public static String text() {
        char[] characters = "WIPI 1.2".toCharArray();
        StringBuffer line = new StringBuffer(16);
        line.append(characters, 5, 3);
        line.append('/');
        line.append(new Boolean(true).booleanValue());
        line.append('/');
        line.append(Boolean.TRUE.booleanValue());
        line.append('/');
        // Appended as an Object, which is a different member of the class from
        // the String append beside it and was declared by neither.
        line.append((Object) "x");
        line.append('/');
        line.append(new Source(new byte[] { 7, 8 }).first());
        line.append('/');
        line.append(2L);
        return line.toString().replace('/', ':');
    }
}

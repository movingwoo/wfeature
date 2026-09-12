import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInput;
import java.io.DataInputStream;
import java.io.DataOutput;
import java.io.DataOutputStream;
import java.io.IOException;

/**
 * A save written and read back through the java.io interfaces rather than
 * through the stream classes. A title that splits its record code this way
 * compiles a reference to DataInput and DataOutput into its constant pool and
 * calls them with invokeinterface, and the stream it actually passes is what
 * has to answer.
 */
public final class Streams {
    public static int roundTrip() throws IOException {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        DataOutputStream out = new DataOutputStream(bytes);
        if (!(out instanceof DataOutput)) {
            return -1;
        }
        write(out);
        out.close();
        DataInputStream in = new DataInputStream(new ByteArrayInputStream(bytes.toByteArray()));
        if (!(in instanceof DataInput)) {
            return -2;
        }
        int total = read(in);
        in.close();
        return total;
    }

    private static void write(DataOutput out) throws IOException {
        out.writeShort(4660);
        out.writeByte(5);
        out.writeBoolean(true);
        out.writeInt(1000);
        out.writeFloat(1.5f);
        out.writeDouble(-2.25d);
    }

    private static int read(DataInput in) throws IOException {
        int total = in.readShort();
        total += in.readByte();
        if (in.readBoolean()) {
            total++;
        }
        total += in.readInt();
        if (in.readFloat() == 1.5f) {
            total++;
        }
        if (in.readDouble() == -2.25d) {
            total += 2;
        }
        return total;
    }

    public static byte[] encodeFloat(float value) throws IOException {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        DataOutput out = new DataOutputStream(bytes);
        out.writeFloat(value);
        return bytes.toByteArray();
    }

    public static byte[] encodeDouble(double value) throws IOException {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        DataOutput out = new DataOutputStream(bytes);
        out.writeDouble(value);
        return bytes.toByteArray();
    }
}

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInput;
import java.io.DataInputStream;
import java.io.DataOutput;
import java.io.DataOutputStream;
import java.io.IOException;
import javax.microedition.midlet.MIDlet;

/** Exercises CLDC stream interfaces from a packaged MIDlet lifecycle. */
public final class StreamMIDlet extends MIDlet {
    private static int result;

    protected void startApp() {
        try {
            result = roundTrip();
        } catch (IOException failure) {
            result = -99;
        }
    }

    protected void pauseApp() {
    }

    protected void destroyApp(boolean unconditional) {
    }

    public static int result() {
        return result;
    }

    private static int roundTrip() throws IOException {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        DataOutputStream stream = new DataOutputStream(bytes);
        if (!(stream instanceof DataOutput)) {
            return -1;
        }
        DataOutput output = stream;
        output.writeShort(4660);
        output.writeByte(5);
        output.writeBoolean(true);
        output.writeInt(1000);
        output.writeFloat(1.5f);
        output.writeDouble(-2.25d);
        stream.close();

        DataInputStream inputStream = new DataInputStream(new ByteArrayInputStream(bytes.toByteArray()));
        if (!(inputStream instanceof DataInput)) {
            return -2;
        }
        DataInput input = inputStream;
        int total = input.readShort() + input.readByte();
        if (input.readBoolean()) {
            total++;
        }
        total += input.readInt();
        if (input.readFloat() == 1.5f) {
            total++;
        }
        if (input.readDouble() == -2.25d) {
            total += 2;
        }
        inputStream.close();
        return total;
    }
}

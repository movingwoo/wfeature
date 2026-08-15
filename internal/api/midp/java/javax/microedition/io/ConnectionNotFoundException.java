package javax.microedition.io;

import java.io.IOException;

/**
 * What every Connector.open in this runtime throws. MIDP defines it for a
 * target that cannot be found or a protocol that is not supported, and both
 * are true here, so a game meets a failure its own error path was written to
 * handle. It extends IOException, which is what a game that catches anything
 * at all around a connection catches.
 */
public class ConnectionNotFoundException extends IOException {
    public ConnectionNotFoundException() {
        super();
    }

    public ConnectionNotFoundException(String message) {
        super(message);
    }
}

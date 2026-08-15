import java.io.IOException;
import javax.microedition.io.ConnectionNotFoundException;
import javax.microedition.io.Connector;
import javax.microedition.midlet.MIDlet;

/**
 * Exercises the Generic Connection Framework the way a game reaches it. Every
 * entry point is expected to refuse, and the last checks are the ones that
 * matter most: the refusal arrives as an IOException a game already catches,
 * and the state machine around it moves on instead of waiting.
 */
public final class ConnectorMIDlet extends MIDlet {
    private static final String HTTP = "http://ranking.invalid/submit";
    private static final String SOCKET = "socket://ranking.invalid:9000";

    private static int flags;
    private static String failure;

    // The offline path a game takes when its connection is refused.
    private static final int STATE_CONNECTING = 1;
    private static final int STATE_ONLINE = 2;
    private static final int STATE_OFFLINE = 3;

    private static int state;

    protected void startApp() {
    }

    protected void pauseApp() {
    }

    protected void destroyApp(boolean unconditional) {
    }

    public static int run() {
        flags = 0;
        check(0, refusedByOpen(HTTP));
        check(1, refusedByOpen(SOCKET));
        check(2, refusedByOpenWithMode());
        check(3, refusedByOpenWithTimeouts());
        check(4, refusedByInputStream());
        check(5, refusedByDataInputStream());
        check(6, refusedByOutputStream());
        check(7, refusedByDataOutputStream());
        check(8, caughtAsIOException());
        check(9, refusalNamesTarget());
        check(10, stateMachineReachesOffline());
        return flags;
    }

    public static String failure() {
        return failure == null ? "" : failure;
    }

    private static void check(int bit, boolean passed) {
        if (passed) {
            flags |= 1 << bit;
        } else if (failure == null) {
            failure = "check " + bit + " failed";
        }
    }

    private static boolean refusedByOpen(String name) {
        try {
            Connector.open(name);
            return false;
        } catch (ConnectionNotFoundException expected) {
            return true;
        } catch (IOException other) {
            return false;
        }
    }

    private static boolean refusedByOpenWithMode() {
        try {
            Connector.open(HTTP, Connector.READ);
            return false;
        } catch (ConnectionNotFoundException expected) {
            return true;
        } catch (IOException other) {
            return false;
        }
    }

    private static boolean refusedByOpenWithTimeouts() {
        try {
            Connector.open(HTTP, Connector.READ_WRITE, true);
            return false;
        } catch (ConnectionNotFoundException expected) {
            return true;
        } catch (IOException other) {
            return false;
        }
    }

    private static boolean refusedByInputStream() {
        try {
            Connector.openInputStream(HTTP);
            return false;
        } catch (ConnectionNotFoundException expected) {
            return true;
        } catch (IOException other) {
            return false;
        }
    }

    private static boolean refusedByDataInputStream() {
        try {
            Connector.openDataInputStream(HTTP);
            return false;
        } catch (ConnectionNotFoundException expected) {
            return true;
        } catch (IOException other) {
            return false;
        }
    }

    private static boolean refusedByOutputStream() {
        try {
            Connector.openOutputStream(HTTP);
            return false;
        } catch (ConnectionNotFoundException expected) {
            return true;
        } catch (IOException other) {
            return false;
        }
    }

    private static boolean refusedByDataOutputStream() {
        try {
            Connector.openDataOutputStream(HTTP);
            return false;
        } catch (ConnectionNotFoundException expected) {
            return true;
        } catch (IOException other) {
            return false;
        }
    }

    /**
     * Most games never name ConnectionNotFoundException; they wrap the whole
     * attempt in one catch of IOException, so that is the catch that has to
     * see the refusal.
     */
    private static boolean caughtAsIOException() {
        try {
            Connector.open(HTTP);
            return false;
        } catch (IOException expected) {
            return true;
        }
    }

    private static boolean refusalNamesTarget() {
        try {
            Connector.open(SOCKET);
            return false;
        } catch (IOException expected) {
            return SOCKET.equals(expected.getMessage());
        }
    }

    /**
     * The property the whole refusal exists for: a game that dials, is
     * refused, and carries on. A fabricated connection would leave this
     * sitting in STATE_CONNECTING forever.
     */
    private static boolean stateMachineReachesOffline() {
        state = STATE_CONNECTING;
        try {
            Connector.open(HTTP);
            state = STATE_ONLINE;
        } catch (IOException refused) {
            state = STATE_OFFLINE;
        }
        return state == STATE_OFFLINE;
    }
}

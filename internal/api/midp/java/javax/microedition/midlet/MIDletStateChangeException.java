package javax.microedition.midlet;

/**
 * Signals a transient MIDlet lifecycle refusal as defined by MIDP 2.0.
 */
public class MIDletStateChangeException extends Exception {
    public MIDletStateChangeException() {
        super();
    }

    public MIDletStateChangeException(String message) {
        super(message);
    }
}

package javax.microedition.rms;

/**
 * Raised when an operation is attempted on a closed record store.
 */
public class RecordStoreNotOpenException extends RecordStoreException {
    public RecordStoreNotOpenException() {
        super();
    }

    public RecordStoreNotOpenException(String message) {
        super(message);
    }
}

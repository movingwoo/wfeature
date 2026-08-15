package javax.microedition.rms;

/**
 * Raised when the record store has no room for more data.
 */
public class RecordStoreFullException extends RecordStoreException {
    public RecordStoreFullException() {
        super();
    }

    public RecordStoreFullException(String message) {
        super(message);
    }
}

package javax.microedition.rms;

/**
 * Raised when a named record store does not exist.
 */
public class RecordStoreNotFoundException extends RecordStoreException {
    public RecordStoreNotFoundException() {
        super();
    }

    public RecordStoreNotFoundException(String message) {
        super(message);
    }
}

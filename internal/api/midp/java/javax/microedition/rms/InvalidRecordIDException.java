package javax.microedition.rms;

/**
 * Raised when a record id names no record in the store.
 */
public class InvalidRecordIDException extends RecordStoreException {
    public InvalidRecordIDException() {
        super();
    }

    public InvalidRecordIDException(String message) {
        super(message);
    }
}

package javax.microedition.rms;

/**
 * Base of the checked failures the record store reports.
 */
public class RecordStoreException extends Exception {
    public RecordStoreException() {
        super();
    }

    public RecordStoreException(String message) {
        super(message);
    }
}

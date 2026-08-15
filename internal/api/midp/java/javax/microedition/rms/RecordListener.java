package javax.microedition.rms;

/**
 * Receives notifications about changes to a record store.
 */
public interface RecordListener {
    void recordAdded(RecordStore recordStore, int recordId);

    void recordChanged(RecordStore recordStore, int recordId);

    void recordDeleted(RecordStore recordStore, int recordId);
}

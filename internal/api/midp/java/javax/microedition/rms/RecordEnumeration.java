package javax.microedition.rms;

/**
 * A bidirectional cursor over the record ids selected from a record store.
 */
public interface RecordEnumeration {
    int numRecords();

    byte[] nextRecord() throws InvalidRecordIDException, RecordStoreNotOpenException, RecordStoreException;

    int nextRecordId() throws InvalidRecordIDException;

    byte[] previousRecord() throws InvalidRecordIDException, RecordStoreNotOpenException, RecordStoreException;

    int previousRecordId() throws InvalidRecordIDException;

    boolean hasNextElement();

    boolean hasPreviousElement();

    void reset();

    void rebuild();

    void keepUpdated(boolean keepUpdated);

    boolean isKeptUpdated();

    void destroy();
}

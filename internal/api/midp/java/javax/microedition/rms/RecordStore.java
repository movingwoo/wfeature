package javax.microedition.rms;

/**
 * Runtime-owned MIDP record store. Record data lives on the Go side, which
 * persists it through the Host save boundary; the methods here are the guest's
 * view of it.
 *
 * The class is never constructed by an application: openRecordStore hands back
 * an instance the runtime made, so the store handle cannot be forged.
 */
public class RecordStore {
    public static final int AUTHMODE_PRIVATE = 0;
    public static final int AUTHMODE_ANY = 1;

    private RecordStore() {
    }

    public static native String[] listRecordStores();

    public static native RecordStore openRecordStore(String recordStoreName, boolean createIfNecessary)
            throws RecordStoreException, RecordStoreNotFoundException, RecordStoreFullException;

    public static RecordStore openRecordStore(String recordStoreName, boolean createIfNecessary, int authmode,
            boolean writable) throws RecordStoreException, RecordStoreNotFoundException, RecordStoreFullException {
        // Only one MIDlet suite runs at a time, so every store is already
        // private to it and the sharing mode changes nothing observable.
        return openRecordStore(recordStoreName, createIfNecessary);
    }

    public static RecordStore openRecordStore(String recordStoreName, String vendorName, String suiteName)
            throws RecordStoreException, RecordStoreNotFoundException {
        return openRecordStore(recordStoreName, false);
    }

    public static native void deleteRecordStore(String recordStoreName)
            throws RecordStoreException, RecordStoreNotFoundException;

    public void setMode(int authmode, boolean writable) throws RecordStoreException {
        checkOpen();
    }

    public native void closeRecordStore() throws RecordStoreException, RecordStoreNotOpenException;

    public native String getName() throws RecordStoreNotOpenException;

    public native int getVersion() throws RecordStoreNotOpenException;

    public native int getNumRecords() throws RecordStoreNotOpenException;

    public native int getSize() throws RecordStoreNotOpenException;

    public native int getSizeAvailable() throws RecordStoreNotOpenException;

    public native long getLastModified() throws RecordStoreNotOpenException;

    public native int getNextRecordID() throws RecordStoreNotOpenException, RecordStoreException;

    public native int addRecord(byte[] data, int offset, int numBytes)
            throws RecordStoreNotOpenException, RecordStoreException, RecordStoreFullException;

    public native void deleteRecord(int recordId)
            throws RecordStoreNotOpenException, InvalidRecordIDException, RecordStoreException;

    public native int getRecordSize(int recordId)
            throws RecordStoreNotOpenException, InvalidRecordIDException, RecordStoreException;

    public native int getRecord(int recordId, byte[] buffer, int offset)
            throws RecordStoreNotOpenException, InvalidRecordIDException, RecordStoreException;

    public native byte[] getRecord(int recordId)
            throws RecordStoreNotOpenException, InvalidRecordIDException, RecordStoreException;

    public native void setRecord(int recordId, byte[] newData, int offset, int numBytes)
            throws RecordStoreNotOpenException, InvalidRecordIDException, RecordStoreException,
            RecordStoreFullException;

    public native void addRecordListener(RecordListener listener);

    public native void removeRecordListener(RecordListener listener);

    public RecordEnumeration enumerateRecords(RecordFilter filter, RecordComparator comparator, boolean keepUpdated)
            throws RecordStoreNotOpenException {
        checkOpen();
        return new RecordSet(this, filter, comparator, keepUpdated);
    }

    /** recordIds lists the live record ids in ascending order. */
    native int[] recordIds() throws RecordStoreNotOpenException;

    /** checkOpen raises RecordStoreNotOpenException for a closed store. */
    private native void checkOpen() throws RecordStoreNotOpenException;
}

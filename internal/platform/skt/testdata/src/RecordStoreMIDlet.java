import javax.microedition.midlet.MIDlet;
import javax.microedition.rms.InvalidRecordIDException;
import javax.microedition.rms.RecordComparator;
import javax.microedition.rms.RecordEnumeration;
import javax.microedition.rms.RecordFilter;
import javax.microedition.rms.RecordListener;
import javax.microedition.rms.RecordStore;
import javax.microedition.rms.RecordStoreException;
import javax.microedition.rms.RecordStoreNotFoundException;
import javax.microedition.rms.RecordStoreNotOpenException;

/**
 * Exercises the record store surface a real J2ME game saves through. Each
 * check sets one bit so a single int reports which parts of RMS behaved.
 */
public final class RecordStoreMIDlet extends MIDlet implements RecordListener {
    private static int flags;
    private static int failureLine;
    private static String failure;
    private static int added;
    private static int changed;
    private static int deleted;
    private static String enumerationOrder = "";

    protected void startApp() {
    }

    protected void pauseApp() {
    }

    protected void destroyApp(boolean unconditional) {
    }

    public void recordAdded(RecordStore store, int recordId) {
        added++;
    }

    public void recordChanged(RecordStore store, int recordId) {
        changed++;
    }

    public void recordDeleted(RecordStore store, int recordId) {
        deleted++;
    }

    public static int flags() {
        return flags;
    }

    public static String failure() {
        return failure == null ? "" : failure;
    }

    public static String enumerationOrder() {
        return enumerationOrder;
    }

    private static void check(boolean condition, int bit, int line) {
        if (condition) {
            flags |= bit;
        } else if (failure == null) {
            failure = "check at line " + line + " failed";
            failureLine = line;
        }
    }

    /** run writes a store from scratch and reports what worked. */
    public static int run() {
        flags = 0;
        failure = null;
        added = 0;
        changed = 0;
        deleted = 0;
        try {
            body();
        } catch (Throwable thrown) {
            if (failure == null) {
                failure = thrown.getClass().getName() + ": " + thrown.getMessage();
            }
        }
        return flags;
    }

    private static void body() throws RecordStoreException {
        RecordStore missing = null;
        try {
            missing = RecordStore.openRecordStore("absent", false);
        } catch (RecordStoreNotFoundException expected) {
            flags |= 1;
        }
        check(missing == null, 2, 90);

        RecordStore store = RecordStore.openRecordStore("save", true);
        check(store.getName().equals("save"), 4, 94);
        check(store.getNumRecords() == 0, 8, 95);
        check(store.getNextRecordID() == 1, 16, 96);

        RecordStoreMIDlet listener = new RecordStoreMIDlet();
        store.addRecordListener(listener);

        byte[] first = new byte[] { 3, 1, 2 };
        byte[] second = new byte[] { 1, 9 };
        byte[] third = new byte[] { 2, 7, 7, 7 };
        int firstId = store.addRecord(first, 0, first.length);
        int secondId = store.addRecord(second, 0, second.length);
        int thirdId = store.addRecord(third, 0, third.length);
        check(firstId == 1 && secondId == 2 && thirdId == 3, 32, 108);
        check(store.getNumRecords() == 3, 64, 109);
        check(store.getSize() == 9, 128, 110);
        check(store.getSizeAvailable() > 0, 256, 111);
        check(added == 3, 512, 112);

        byte[] read = store.getRecord(secondId);
        check(read.length == 2 && read[0] == 1 && read[1] == 9, 1024, 115);
        check(store.getRecordSize(thirdId) == 4, 2048, 116);

        byte[] buffer = new byte[8];
        int copied = store.getRecord(thirdId, buffer, 2);
        check(copied == 4 && buffer[2] == 2 && buffer[5] == 7 && buffer[0] == 0, 4096, 120);

        // A partial range writes only the selected bytes.
        byte[] replacement = new byte[] { 0, 4, 4, 0 };
        store.setRecord(secondId, replacement, 1, 2);
        byte[] updated = store.getRecord(secondId);
        check(updated.length == 2 && updated[0] == 4 && updated[1] == 4, 8192, 126);
        check(changed == 1, 16384, 127);

        int versionAfterWrites = store.getVersion();
        check(versionAfterWrites >= 4, 32768, 130);

        store.deleteRecord(firstId);
        check(store.getNumRecords() == 2, 65536, 133);
        check(deleted == 1, 131072, 134);
        // Ids are never reused, so the next id still counts the deleted one.
        check(store.getNextRecordID() == 4, 262144, 136);
        try {
            store.getRecord(firstId);
            check(false, 0, 139);
        } catch (InvalidRecordIDException expected) {
            flags |= 524288;
        }

        // Sort the surviving records by their first byte, descending, and keep
        // only records longer than two bytes.
        RecordEnumeration enumeration = store.enumerateRecords(new RecordFilter() {
            public boolean matches(byte[] candidate) {
                return candidate.length > 2;
            }
        }, new RecordComparator() {
            public int compare(byte[] left, byte[] right) {
                if (left[0] == right[0]) {
                    return RecordComparator.EQUIVALENT;
                }
                return left[0] < right[0] ? RecordComparator.FOLLOWS : RecordComparator.PRECEDES;
            }
        }, true);
        check(enumeration.numRecords() == 1, 1048576, 158);
        StringBuffer order = new StringBuffer();
        while (enumeration.hasNextElement()) {
            order.append(enumeration.nextRecordId());
            order.append(',');
        }
        enumerationOrder = order.toString();
        check(enumerationOrder.equals("3,"), 2097152, 165);
        check(!enumeration.hasNextElement() && enumeration.hasPreviousElement(), 4194304, 166);
        enumeration.reset();
        check(enumeration.hasNextElement(), 8388608, 168);

        // A kept-updated enumeration follows a later write without rebuild().
        byte[] fourth = new byte[] { 9, 9, 9 };
        store.addRecord(fourth, 0, fourth.length);
        check(enumeration.numRecords() == 2, 16777216, 173);
        check(enumeration.nextRecordId() == 4, 33554432, 174);
        enumeration.destroy();

        String[] names = RecordStore.listRecordStores();
        boolean listed = false;
        for (int index = 0; index < names.length; index++) {
            if (names[index].equals("save")) {
                listed = true;
            }
        }
        check(listed, 67108864, 183);

        store.removeRecordListener(listener);
        store.closeRecordStore();
        try {
            store.getNumRecords();
            check(false, 0, 189);
        } catch (RecordStoreNotOpenException expected) {
            flags |= 134217728;
        }
    }

    /** reopen reports the record count and payload a later session sees. */
    public static int reopen() {
        try {
            RecordStore store = RecordStore.openRecordStore("save", false);
            int count = store.getNumRecords();
            byte[] record = store.getRecord(2);
            store.closeRecordStore();
            if (record.length == 2 && record[0] == 4 && record[1] == 4) {
                return count;
            }
            return -2;
        } catch (RecordStoreException failed) {
            failure = failed.getClass().getName() + ": " + failed.getMessage();
            return -1;
        }
    }

    /** deleteStore removes the store and reports whether it is gone. */
    public static boolean deleteStore() {
        try {
            RecordStore.deleteRecordStore("save");
        } catch (RecordStoreException failed) {
            failure = failed.getClass().getName() + ": " + failed.getMessage();
            return false;
        }
        try {
            RecordStore.openRecordStore("save", false);
            return false;
        } catch (RecordStoreNotFoundException expected) {
            return true;
        } catch (RecordStoreException other) {
            return false;
        }
    }
}

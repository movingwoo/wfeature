package javax.microedition.rms;

/**
 * The runtime's RecordEnumeration. It is written in Java rather than as a
 * native service on purpose: a filter and a comparator are application
 * objects, and calling them from here keeps them ordinary guest calls instead
 * of Host reentry into the interpreter.
 *
 * The cursor sits between records: position is the number of records before
 * it, so nextRecord returns ids[position++] and previousRecord returns
 * ids[--position].
 */
class RecordSet implements RecordEnumeration, RecordListener {
    private RecordStore store;
    private RecordFilter filter;
    private RecordComparator comparator;
    private boolean updated;
    private boolean destroyed;
    private int[] ids;
    private int position;

    RecordSet(RecordStore store, RecordFilter filter, RecordComparator comparator, boolean keepUpdated) {
        this.store = store;
        this.filter = filter;
        this.comparator = comparator;
        this.ids = new int[0];
        rebuild();
        if (keepUpdated) {
            keepUpdated(true);
        }
    }

    public int numRecords() {
        return ids.length;
    }

    public byte[] nextRecord() throws InvalidRecordIDException, RecordStoreNotOpenException, RecordStoreException {
        return store.getRecord(nextRecordId());
    }

    public int nextRecordId() throws InvalidRecordIDException {
        checkAlive();
        if (position >= ids.length) {
            throw new InvalidRecordIDException("enumeration is past the last record");
        }
        int id = ids[position];
        position++;
        return id;
    }

    public byte[] previousRecord() throws InvalidRecordIDException, RecordStoreNotOpenException, RecordStoreException {
        return store.getRecord(previousRecordId());
    }

    public int previousRecordId() throws InvalidRecordIDException {
        checkAlive();
        if (position <= 0) {
            throw new InvalidRecordIDException("enumeration is before the first record");
        }
        position--;
        return ids[position];
    }

    public boolean hasNextElement() {
        return !destroyed && position < ids.length;
    }

    public boolean hasPreviousElement() {
        return !destroyed && position > 0;
    }

    public void reset() {
        position = 0;
    }

    public void rebuild() {
        if (destroyed) {
            return;
        }
        int[] all;
        try {
            all = store.recordIds();
        } catch (RecordStoreNotOpenException closed) {
            ids = new int[0];
            position = 0;
            return;
        }
        byte[][] data = new byte[all.length][];
        int kept = 0;
        for (int index = 0; index < all.length; index++) {
            byte[] record;
            try {
                record = store.getRecord(all[index]);
            } catch (RecordStoreException missing) {
                // A record deleted between listing and reading simply is not
                // part of this enumeration.
                continue;
            }
            if (filter != null && !filter.matches(record)) {
                continue;
            }
            all[kept] = all[index];
            data[kept] = record;
            kept++;
        }
        int[] selected = new int[kept];
        byte[][] selectedData = new byte[kept][];
        for (int index = 0; index < kept; index++) {
            selected[index] = all[index];
            selectedData[index] = data[index];
        }
        if (comparator != null) {
            sort(selected, selectedData);
        }
        ids = selected;
        position = 0;
    }

    /**
     * Insertion sort keeps the comparator's answer for equal records in the
     * order the store listed them, which is the order a game that sorts by one
     * field expects for records sharing it.
     */
    private void sort(int[] order, byte[][] data) {
        for (int index = 1; index < order.length; index++) {
            int id = order[index];
            byte[] record = data[index];
            int scan = index - 1;
            while (scan >= 0 && comparator.compare(data[scan], record) == RecordComparator.FOLLOWS) {
                order[scan + 1] = order[scan];
                data[scan + 1] = data[scan];
                scan--;
            }
            order[scan + 1] = id;
            data[scan + 1] = record;
        }
    }

    public void keepUpdated(boolean keepUpdated) {
        if (destroyed || updated == keepUpdated) {
            return;
        }
        updated = keepUpdated;
        if (keepUpdated) {
            store.addRecordListener(this);
            rebuild();
        } else {
            store.removeRecordListener(this);
        }
    }

    public boolean isKeptUpdated() {
        return updated;
    }

    public void destroy() {
        if (destroyed) {
            return;
        }
        if (updated) {
            store.removeRecordListener(this);
            updated = false;
        }
        destroyed = true;
        ids = new int[0];
        position = 0;
    }

    public void recordAdded(RecordStore recordStore, int recordId) {
        rebuild();
    }

    public void recordChanged(RecordStore recordStore, int recordId) {
        rebuild();
    }

    public void recordDeleted(RecordStore recordStore, int recordId) {
        rebuild();
    }

    private void checkAlive() throws InvalidRecordIDException {
        if (destroyed) {
            throw new InvalidRecordIDException("enumeration is destroyed");
        }
    }
}

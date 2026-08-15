package javax.microedition.rms;

/**
 * Orders the records an enumeration returns.
 */
public interface RecordComparator {
    int EQUIVALENT = 0;
    int FOLLOWS = 1;
    int PRECEDES = -1;

    int compare(byte[] rec1, byte[] rec2);
}

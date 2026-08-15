package javax.microedition.rms;

/**
 * Selects which records an enumeration includes.
 */
public interface RecordFilter {
    boolean matches(byte[] candidate);
}

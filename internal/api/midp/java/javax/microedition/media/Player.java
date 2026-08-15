package javax.microedition.media;

/**
 * A single sound the runtime decodes and plays through the Host audio sink.
 * The state machine is the JSR-135 one; see docs/audio.md for what the sink
 * actually does with the events.
 */
public class Player {
    public static final int UNREALIZED = 100;
    public static final int REALIZED = 200;
    public static final int PREFETCHED = 300;
    public static final int STARTED = 400;
    public static final int CLOSED = 0;

    public static final long TIME_UNKNOWN = -1;

    Player() {
    }

    public native void realize() throws MediaException;

    public native void prefetch() throws MediaException;

    public native void start() throws MediaException;

    public native void stop() throws MediaException;

    public native void deallocate();

    public native void close();

    public native int getState();

    public native long getDuration();

    public native long getMediaTime();

    public native long setMediaTime(long now) throws MediaException;

    public native void setLoopCount(int count);

    public native String getContentType();

    public native void addPlayerListener(PlayerListener playerListener);

    public native void removePlayerListener(PlayerListener playerListener);
}

import javax.microedition.midlet.MIDlet;

public final class NativeCheckpointMIDlet extends MIDlet {
    public static int starts;
    public static int result;
    public static int heapResult;

    protected void startApp() {
        starts++;
        if (starts == 1) HeapCheckpointProbe.prepare();
        heapResult = HeapCheckpointProbe.tick();
        if (starts > 1) {
            result = NativeCheckpointProbe.run();
        }
    }

    protected void pauseApp() {}
    protected void destroyApp(boolean unconditional) {}
}

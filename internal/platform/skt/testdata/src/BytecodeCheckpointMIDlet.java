import javax.microedition.midlet.MIDlet;

public final class BytecodeCheckpointMIDlet extends MIDlet {
    public static int starts;
    public static int result;

    protected void startApp() {
        starts++;
        if (starts > 1) result = BytecodeCheckpointProbe.run(0);
    }

    protected void pauseApp() {}
    protected void destroyApp(boolean unconditional) {}
}

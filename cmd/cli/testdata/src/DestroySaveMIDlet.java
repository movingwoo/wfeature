import javax.microedition.midlet.MIDlet;
import javax.microedition.rms.RecordStore;

/*
 * Writes its record store only from destroyApp, as a title that saves on exit
 * does, so a run that never destroys the MIDlet leaves the store empty.
 *
 * Built against the library signatures (docs/history/testing.md, "Compiling a
 * Java fixture") from the repository root:
 *
 *   javac -source 1.8 -target 1.8 -nowarn -g:none -classpath "$stub_dir/classes" \
 *     -d "$fixture_dir" cmd/cli/testdata/src/DestroySaveMIDlet.java
 *   jar cfm cmd/cli/testdata/destroy-save.jar cmd/cli/testdata/DESTROY_SAVE.MF \
 *     -C "$fixture_dir" .
 */
public final class DestroySaveMIDlet extends MIDlet {
    protected void startApp() {}
    protected void pauseApp() {}
    protected void destroyApp(boolean unconditional) {
        try {
            RecordStore store = RecordStore.openRecordStore("progress", true);
            byte[] data = "written-by-destroyApp".getBytes();
            store.addRecord(data, 0, data.length);
            store.closeRecordStore();
        } catch (Exception error) {}
    }
}

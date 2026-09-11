import javax.microedition.midlet.MIDlet;
import javax.microedition.rms.RecordStore;

/** An authored license gate with observable initialization and ordinary saves. */
public final class LicenseMIDlet extends MIDlet {
    private static int progress;
    private static boolean accepted;

    protected void startApp() {
        accepted = LicenseCheck.valid(this);
        if (accepted) {
            try {
                RecordStore store = RecordStore.openRecordStore("progress", true);
                if (store.getNumRecords() != 0) progress = store.getRecord(1)[0];
                store.closeRecordStore();
            } catch (Exception failure) {
                throw new RuntimeException("fixture load failed");
            }
        }
    }

    protected void pauseApp() {}
    protected void destroyApp(boolean unconditional) {}

    public static boolean accepted() { return accepted; }
    public static int progress() { return progress; }
    public static int digestCalls() { return LicenseCheck.calls; }
    public static boolean ordinaryEquals() { return "one".equals("two"); }

    public static void advanceAndSave() throws Exception {
        if (!accepted) throw new RuntimeException("fixture gate closed");
        byte[] data = new byte[] { (byte) ++progress };
        RecordStore store = RecordStore.openRecordStore("progress", true);
        if (store.getNumRecords() == 0) store.addRecord(data, 0, data.length);
        else store.setRecord(1, data, 0, data.length);
        store.closeRecordStore();
    }
}

/** The digest is deliberately synthetic; no external license code is bundled. */
final class LicenseCheck {
    public static int calls;

    private static String number() { return System.getProperty("MIN"); }
    public void update(byte[] data, int offset, int length) { calls++; }
    public void finish(byte[] output) { output[0] = 42; }
    private String hex(byte[] output) { return "fixture-digest"; }

    public static boolean valid(MIDlet midlet) {
        String number = number();
        String url = midlet.getAppProperty("MIDlet-Jar-URL");
        int index = url.indexOf("SERVICE_ID=");
        String service = url.substring(index + 16, index + 26);
        String key = midlet.getAppProperty("MIDlet-Key").trim();
        LicenseCheck digest = new LicenseCheck();
        byte[] input = new StringBuffer().append(number).append(service)
            .append("a0a535ef35b").toString().getBytes();
        digest.update(input, 0, input.length);
        byte[] output = new byte[16];
        digest.finish(output);
        String calculated = digest.hex(output);
        digest = null;
        return key.equals(calculated);
    }
}

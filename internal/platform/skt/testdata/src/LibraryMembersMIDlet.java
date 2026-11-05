import javax.microedition.midlet.MIDlet;

/**
 * Runs LibraryMembers, the JVM fixture of the CLDC members KTF and SKT share,
 * from the MIDlet's own startApp: the members are linked from a JAR, under
 * this platform's own charset, the way a title reaches them.
 */
public final class LibraryMembersMIDlet extends MIDlet {
    private static String report = "";

    protected void startApp() {
        try {
            report = LibraryMembers.report();
        } catch (Throwable failure) {
            report = "failed: " + failure;
        }
    }

    protected void pauseApp() {
    }

    protected void destroyApp(boolean unconditional) {
    }

    /** Every line LibraryMembers answered, or what stopped it. */
    public static String report() {
        return report;
    }

    /** Leaves through Runtime.exit, as a quit menu of this era might. */
    public static void quit() {
        Runtime.getRuntime().exit(3);
    }
}

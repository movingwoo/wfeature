// Authored fixture for literal, constructed and explicitly interned strings.
public final class StringIdentity {
    public static final String CONSTANT = "started";
    public static String retained;
    public static String copy;

    public static String literal() { return "started"; }
    public static String peerLiteral() { return Peer.literal(); }

    public static void retain() {
        retained = literal();
        copy = new String(retained);
    }

    public static boolean retainedIdentity() {
        return retained == literal() && retained == peerLiteral() &&
               copy != retained && copy.equals(retained);
    }

    public static boolean internLiteral() {
        String value = new String("started");
        return value != literal() && value.intern() == literal();
    }

    public static boolean internBeforeLiteral() {
        String value = new String(new char[] {'r', 'u', 'n'});
        return value.intern() == value && value == "run";
    }

    public static final class Peer {
        public static String literal() { return "started"; }
    }
}

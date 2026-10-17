/**
 * A string constant holding one half of a surrogate pair. The class file
 * format encodes each UTF-16 unit on its own, so such a constant is legal and
 * the class has to load; the half itself reads back as a replacement
 * character, because this runtime holds strings as Unicode text.
 */
public final class LoneSurrogate {
    public static String text() {
        return "a\uD800b";
    }

    public static int length() {
        return text().length();
    }
}

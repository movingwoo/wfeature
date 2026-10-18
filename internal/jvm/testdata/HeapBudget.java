/**
 * The VM-wide heap budget as a guest meets it: an allocation the budget cannot
 * hold raises OutOfMemoryError, which the guest catches, and what the guest
 * dropped is collected and can be allocated again. Every loop that would grow
 * without end is bounded here as well, so a VM without the budget answers -1
 * instead of running the host out of memory.
 */
public final class HeapBudget {
    /** One array is a whole chunk of the budget; it holds 16,384 elements. */
    static final int CHUNK = 16384;

    /** Arrays a checkpoint carries, kept in a static so the heap state holds them. */
    static Object[] kept;

    static final class Node {
        Node next;
        int value;
    }

    /** Keeps arrays until the budget refuses one, then drops them all. */
    public static int retainArrays() {
        Object[] held = new Object[256];
        int count = 0;
        try {
            while (count < held.length) {
                held[count] = new int[CHUNK];
                count++;
            }
            return -1;
        } catch (OutOfMemoryError e) {
            return count;
        }
    }

    /** Keeps small objects in a chain until the budget refuses one. */
    public static int retainObjects() {
        Node head = null;
        int count = 0;
        try {
            while (count < 100000) {
                Node node = new Node();
                node.next = head;
                node.value = count;
                head = node;
                count++;
            }
            return -1;
        } catch (OutOfMemoryError e) {
            return count;
        }
    }

    /** Grows one StringBuffer until the budget refuses its characters. */
    public static int growBuffer() {
        StringBuffer buffer = new StringBuffer();
        try {
            while (buffer.length() < 4000000) {
                buffer.append("0123456789abcdef");
            }
            return -1;
        } catch (OutOfMemoryError e) {
            return buffer.length();
        }
    }

    /** Allocates many times the budget and keeps none of it. */
    public static int churn(int rounds) {
        int sum = 0;
        for (int round = 0; round < rounds; round++) {
            int[] garbage = new int[CHUNK];
            garbage[round % CHUNK] = round;
            sum += garbage[round % CHUNK] == round ? 1 : 0;
        }
        return sum;
    }

    /** One array larger than the whole budget, refused before it is made. */
    public static String refuseOne() {
        try {
            int[] huge = new int[12000000];
            return "allocated " + huge.length;
        } catch (OutOfMemoryError e) {
            return e.getMessage();
        }
    }

    /** Keeps count arrays in a static for a checkpoint to carry. */
    public static void keep(int count) {
        kept = new Object[count];
        for (int index = 0; index < count; index++) {
            kept[index] = new int[CHUNK];
        }
    }

    /** Answers how many arrays the static holds. */
    public static int keptCount() {
        int count = 0;
        for (int index = 0; index < kept.length; index++) {
            if (kept[index] != null) {
                count++;
            }
        }
        return count;
    }

    /**
     * Keeps small objects through every refusal, as a title does that catches
     * OutOfMemoryError and goes on allocating, and answers how many it kept.
     */
    public static int retainThroughRefusals(int attempts) {
        Node head = null;
        int kept = 0;
        for (int attempt = 0; attempt < attempts; attempt++) {
            try {
                Node node = new Node();
                node.next = head;
                node.value = attempt;
                head = node;
                kept++;
            } catch (OutOfMemoryError e) {
                // Goes on, as such a title does.
            }
        }
        return kept;
    }

    /**
     * Keeps Strings built from one text of length characters until one is
     * refused: from a char array (kind 0), a byte array (1), a StringBuffer
     * (2) or a byte range in a named encoding (3).
     */
    public static int retainCopies(int kind, int length) {
        char[] chars = new char[length];
        byte[] bytes = new byte[length];
        for (int index = 0; index < length; index++) {
            chars[index] = (char) ('a' + index % 26);
            bytes[index] = (byte) chars[index];
        }
        StringBuffer buffer = new StringBuffer(length);
        buffer.append(chars);
        String[] copies = new String[400];
        int count = 0;
        try {
            while (count < copies.length) {
                switch (kind) {
                case 0:
                    copies[count] = new String(chars);
                    break;
                case 1:
                    copies[count] = new String(bytes);
                    break;
                case 2:
                    copies[count] = new String(buffer);
                    break;
                default:
                    copies[count] = new String(bytes, 0, length, "UTF-8");
                    break;
                }
                count++;
            }
            return -1;
        } catch (OutOfMemoryError e) {
            return count;
        } catch (java.io.UnsupportedEncodingException e) {
            return -2;
        }
    }

    /**
     * Fills the budget with arrays it keeps, then asks attempts times for a
     * small array it would also keep, and answers how many were refused.
     */
    public static int refusedWhileFull(int attempts) {
        Object[] held = new Object[1024];
        int count = 0;
        try {
            while (count < 256) {
                held[count] = new int[CHUNK];
                count++;
            }
        } catch (OutOfMemoryError e) {
            // Full.
        }
        int refused = 0;
        for (int attempt = 0; attempt < attempts; attempt++) {
            try {
                int[] small = new int[1024];
                if (count < held.length) {
                    held[count] = small;
                    count++;
                }
            } catch (OutOfMemoryError e) {
                refused++;
            }
        }
        return held[0] == null ? -1 : refused;
    }

    /** A StringBuffer cut short and lengthened again reads zeros past the cut. */
    public static String refill() {
        StringBuffer buffer = new StringBuffer("abc");
        buffer.setLength(1);
        buffer.setLength(3);
        buffer.insert(1, "xy");
        return buffer.toString();
    }
}

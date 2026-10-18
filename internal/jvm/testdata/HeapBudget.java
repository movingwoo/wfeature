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

    /** A StringBuffer cut short and lengthened again reads zeros past the cut. */
    public static String refill() {
        StringBuffer buffer = new StringBuffer("abc");
        buffer.setLength(1);
        buffer.setLength(3);
        buffer.insert(1, "xy");
        return buffer.toString();
    }
}

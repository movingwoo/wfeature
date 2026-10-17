/**
 * Class initialization the way getstatic and putstatic trigger it: the class or
 * interface that declares the field, and nothing else. A compiler names a field
 * reference after the type the source expression had, so an interface constant
 * read inside an implementing class says the class's name, and an inherited
 * static read through a subclass says the subclass's.
 */
public final class StaticInit {
    static int initialized;

    static int mark(int bit) {
        initialized |= bit;
        return bit;
    }

    interface Table {
        int[] VALUES = {7, 8, 9};
        int MARK = StaticInit.mark(1);
    }

    static final class Reader implements Table {
        static {
            mark(2);
        }

        static int first() {
            return VALUES[0];
        }
    }

    static class Base {
        static int shared = 5;

        static {
            mark(4);
        }
    }

    static final class Derived extends Base {
        static {
            mark(8);
        }
    }

    public static int interfaceTable() {
        return Reader.first();
    }

    public static int inheritedRead() {
        return Derived.shared;
    }

    public static int inheritedWrite() {
        Derived.shared = 6;
        return Base.shared;
    }

    public static int initialized() {
        return initialized;
    }
}

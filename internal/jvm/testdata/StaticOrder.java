/**
 * When a class's statics are prepared (JVMS 5.4.2 and 5.5, steps 6 and 7):
 * before its superclass and its default-method superinterfaces are
 * initialized. A value one of their initializers stores into a static of the
 * class whose initialization started the chain is still there once that
 * class's own initialization has finished.
 */
public final class StaticOrder {
    static class Base {
        static {
            Derived.fromBase = 5;
        }
    }

    static final class Derived extends Base {
        static int fromBase;
        static int own = 7;
    }

    interface Defaults {
        int MARK = StaticOrder.storeFromInterface();

        default int mark() {
            return MARK;
        }
    }

    static final class Implementor implements Defaults {
        static int fromInterface;
    }

    static int storeFromInterface() {
        Implementor.fromInterface = 6;
        return 1;
    }

    /** Reads the subclass first, so its initialization starts the chain. */
    public static int fromSuperclass() {
        return Derived.fromBase;
    }

    /** The subclass's own initializer still runs after its superclass's. */
    public static int own() {
        return Derived.own;
    }

    /** Reads the implementing class first, for the same reason. */
    public static int fromSuperinterface() {
        return Implementor.fromInterface;
    }
}

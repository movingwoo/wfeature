/**
 * Class initialization and the interfaces a class implements (JVMS 5.5, step
 * 7): the superclass first, then every superinterface that declares a default
 * method, each one's own superinterfaces before it, then the class. An
 * interface whose methods are all abstract or static is not initialized by a
 * class that implements it. Each initializer appends one letter to the order.
 */
public final class SuperinterfaceInit {
    static String order = "";

    static int log(String name) {
        order = order + name;
        return order.length();
    }

    /** Abstract methods only: not initialized. */
    interface Plain {
        int MARK = SuperinterfaceInit.log("P");

        int plain();
    }

    /** A static method is not a default method: not initialized. */
    interface Statics {
        int MARK = SuperinterfaceInit.log("S");

        static int helper() {
            return 3;
        }
    }

    /** Declares a default method: initialized. */
    interface Root {
        int MARK = SuperinterfaceInit.log("R");

        default int root() {
            return 1;
        }
    }

    /** Inherits Root's default method and declares none of its own. */
    interface Left extends Root {
        int MARK = SuperinterfaceInit.log("L");
    }

    /** Declares a default method above an interface that does not. */
    interface Right extends Plain, Root {
        int MARK = SuperinterfaceInit.log("G");

        default int right() {
            return 2;
        }
    }

    static class Base {
        static {
            log("B");
        }
    }

    static final class Implementation extends Base implements Left, Right, Statics {
        static {
            log("C");
        }

        public int plain() {
            return 0;
        }

        static int touch() {
            return 0;
        }
    }

    /** Initializes Implementation and answers the order the initializers ran in. */
    public static String order() {
        Implementation.touch();
        return order;
    }
}

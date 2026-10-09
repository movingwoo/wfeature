// Authored fixture for transitive interface identity and interpreted dispatch.
public final class InterfaceIdentity {
    public interface Root { int value(); }
    public interface Left extends Root { }
    public interface Right extends Root { }
    public interface Diamond extends Left, Right { }
    public interface Other { int other(); }

    public static class Base implements Diamond {
        public int value() { return 7; }
    }

    public static final class Child extends Base {
        public int value() { return 11; }
    }

    public static boolean transitiveInstance() {
        Object value = new Base();
        return value instanceof Root && value instanceof Left &&
               value instanceof Right && value instanceof Diamond && !(value instanceof Other);
    }

    public static boolean inheritedInstance() {
        Object value = new Child();
        return value instanceof Root && value instanceof Left && value instanceof Right &&
               value instanceof Diamond && value instanceof Base && value instanceof Child;
    }

    public static int castAndDispatch() {
        Object value = new Child();
        Root root = (Root) value;
        Left left = (Left) value;
        Right right = (Right) value;
        Diamond diamond = (Diamond) value;
        return root.value() + left.value() + right.value() + diamond.value();
    }

    public static int inheritedDispatch() {
        Diamond value = new Child();
        return value.value();
    }

    public static boolean rejectsUnrelatedCast() {
        Object value = new Child();
        if (value instanceof Other) return false;
        try {
            Other unrelated = (Other) value;
            return unrelated == null;
        } catch (ClassCastException expected) {
            return true;
        }
    }

    public static boolean nullRelations() {
        Object value = null;
        return !(value instanceof Root) && (Root) value == null;
    }
}

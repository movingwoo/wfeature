/**
 * A field written by the class that declares it and read by a subclass. A
 * compiler names the field reference after the type the expression had, so the
 * write says `InheritedBase.value` and the read says `Inherited.value` — two
 * names for one field, which is what field resolution exists to reconcile.
 */
public final class Inherited extends InheritedBase {
    public Inherited(int value) {
        super(value);
    }

    public int read() {
        return value + shared;
    }
}

class InheritedBase {
    protected int value;
    protected static int shared;

    InheritedBase(int value) {
        this.value = value;
        shared = value * 2;
    }
}

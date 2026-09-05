// CallLoop is the interpreter's call path with the Host out of the way.
//
// A benchmark that enters through InvokeStatic measures one *Host* call, and a
// Host call starts a fresh execution: anything an execution carries across the
// calls it makes — its frames, its step budget — is cold every time. A title
// does not run that way. One guest thread is one execution for the whole
// session and makes millions of calls inside it, which is what this loops.
public final class CallLoop {
    private final int base;

    private CallLoop(int base) {
        this.base = base;
    }

    private int add(int value) {
        return base + value;
    }

    private static int twice(int value) {
        return value * 2;
    }

    // instanceCalls makes `count` virtual calls on one receiver.
    public static int instanceCalls(int count) {
        CallLoop target = new CallLoop(1);
        int sum = 0;
        for (int index = 0; index < count; index++) {
            sum = target.add(sum);
        }
        return sum;
    }

    // staticCalls makes `count` static calls, which take the same frame path
    // without a receiver to dispatch on.
    public static int staticCalls(int count) {
        int sum = 1;
        for (int index = 0; index < count; index++) {
            sum = twice(sum) - sum;
        }
        return sum;
    }

    // allocatingCalls makes `count` objects as well, which is the shape of a
    // title's frame loop rather than of a tight arithmetic one.
    public static int allocatingCalls(int count) {
        int sum = 0;
        for (int index = 0; index < count; index++) {
            sum = new CallLoop(index).add(sum);
        }
        return sum;
    }
}

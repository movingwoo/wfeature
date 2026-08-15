public final class Arithmetic {
    private interface Operation {
        int apply(int value);
    }

    private static final class AddOperation implements Operation {
        public int apply(int value) {
            return value + 1;
        }
    }

    private static int seed = 7;
    private static long longSeed = 5L;
    private static int syncCounter;
    private static int threadResult;

    private static final class SetResult implements Runnable {
        public void run() {
            threadResult = 42;
        }
    }

    static {
        seed *= 3;
    }

    private final int base;

    private Arithmetic(int base) {
        this.base = base;
    }

    public static int sumTwice(int count) {
        int sum = 0;
        for (int index = 0; index < count; index++) {
            sum += index;
        }
        return twice(sum);
    }

    private static int twice(int value) {
        return value * 2;
    }

    public static int denseSwitch(int value) {
        switch (value) {
            case 1:
                return 10;
            case 2:
                return 20;
            case 3:
                return 30;
            default:
                return -1;
        }
    }

    public static int sparseSwitch(int value) {
        switch (value) {
            case -100:
                return 1;
            case 7:
                return 2;
            case 1000:
                return 3;
            default:
                return -1;
        }
    }

    public static long longMath(long left, long right) {
        return (left + right) * 2L;
    }

    public int add(int value) {
        return base + value;
    }

    public static int objectMath(int base, int value) {
        return new Arithmetic(base).add(value);
    }

    public static int plainObjectMath() {
        Object first = new Object();
        Object second = new Object();
        return first != second && first.equals(first) && !first.equals(second)
            && first.hashCode() == first.hashCode() ? 1 : 0;
    }

    public static void startThread() {
        new Thread(new SetResult()).start();
    }

    public static int threadResult() {
        return threadResult;
    }

    public static int arraySum(int count) {
        int[] values = new int[count];
        for (int index = 0; index < values.length; index++) {
            values[index] = index + 1;
        }
        int sum = 0;
        for (int index = 0; index < values.length; index++) {
            sum += values[index];
        }
        return sum;
    }

    public static int nextSeed() {
        return seed++;
    }

    public static long nextLongSeed() {
        return longSeed++;
    }

    public static int safeDivide(int left, int right) {
        try {
            return left / right;
        } catch (ArithmeticException exception) {
            return -1;
        }
    }

    public static int typeMath(int value) {
        Object object = new Arithmetic(value);
        if (object instanceof Arithmetic) {
            return ((Arithmetic) object).base;
        }
        return -1;
    }

    public static int matrixMath() {
        int[][] matrix = new int[2][3];
        matrix[1][2] = 42;
        return matrix.length + matrix[1].length + matrix[1][2];
    }

    public static synchronized int syncIncrement() {
        return ++syncCounter;
    }

    public static int synchronizedBlock(int value) {
        synchronized (new Arithmetic(value)) {
            return value + 1;
        }
    }

    public static int nativeMath(int value) {
        return Math.max(Math.abs(value), 10);
    }

    public static int copyMath() {
        int[] source = new int[]{1, 2, 3, 4};
        int[] target = new int[4];
        System.arraycopy(source, 1, target, 0, 3);
        return target[0] + target[1] + target[2];
    }

    public static int stringMath() {
        return "A\uD83D\uDE00".length();
    }

    public static int commonLibraryMath() {
        java.util.Hashtable table = new java.util.Hashtable();
        table.put("key", "value");
        java.util.Vector values = new java.util.Vector(1);
        values.addElement(new String(new byte[]{'O', 'K'}));
        values.insertElementAt("first", 0);
        String built = new StringBuffer("v").append(42).append('!').toString();
        return table.containsKey("key") && table.get("key").equals("value")
            && values.size() == 2 && values.elementAt(1).equals("OK")
            && built.equals("v42!") && Integer.parseInt("42") == 42 ? 1 : 0;
    }

    public static long clock() {
        return System.currentTimeMillis();
    }

    public static int interfaceMath(int value) {
        Operation operation = new AddOperation();
        return operation.apply(value);
    }
}

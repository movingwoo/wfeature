import java.io.ByteArrayInputStream;
import java.io.DataInputStream;
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStreamReader;

/**
 * Class-library contracts a title's own error handling is written against:
 * which exception a short read raises, what a null argument raises, which
 * numbers a parse refuses, what an empty message reads back as, and that the
 * same class is the same object. Each method answers a number or an object the
 * test compares, so a divergence names the contract it broke.
 */
public final class LibraryContracts {
    public static int readIntPastEnd() {
        DataInputStream in = new DataInputStream(new ByteArrayInputStream(new byte[] {1}));
        try {
            in.readInt();
            return 0;
        } catch (EOFException e) {
            return 1;
        } catch (IOException e) {
            return 2;
        }
    }

    public static int readRecordsUntilEnd() {
        DataInputStream in = new DataInputStream(new ByteArrayInputStream(new byte[] {0, 3, 0, 4, 9}));
        int sum = 0;
        try {
            while (true) {
                sum += in.readShort();
            }
        } catch (EOFException e) {
            return sum;
        } catch (IOException e) {
            return -1;
        }
    }

    public static int readUTFPastEnd() {
        DataInputStream in = new DataInputStream(new ByteArrayInputStream(new byte[] {0, 4, 'a'}));
        try {
            in.readUTF();
            return 0;
        } catch (EOFException e) {
            return 1;
        } catch (IOException e) {
            return 2;
        }
    }

    public static boolean classIdentity() {
        Object first = new LibraryContracts();
        Object second = new LibraryContracts();
        try {
            return first.getClass() == second.getClass()
                && first.getClass() == LibraryContracts.class
                && Class.forName("LibraryContracts") == LibraryContracts.class
                && "a".getClass() == String.class;
        } catch (ClassNotFoundException e) {
            return false;
        }
    }

    public static boolean equalByClass() {
        return new Key(3).equals(new Key(3));
    }

    static final class Key {
        private final int value;

        Key(int value) {
            this.value = value;
        }

        public boolean equals(Object other) {
            if (other == null || getClass() != other.getClass()) {
                return false;
            }
            return ((Key) other).value == value;
        }

        public int hashCode() {
            return value;
        }
    }

    public static void waitWithNanos() throws InterruptedException {
        Object lock = new Object();
        synchronized (lock) {
            lock.wait(0, 500000);
        }
    }

    public static int nullArguments() {
        String text = "abc";
        String none = null;
        int raised = 0;
        try {
            text.compareTo(none);
        } catch (NullPointerException e) {
            raised |= 1;
        }
        try {
            text.concat(none);
        } catch (NullPointerException e) {
            raised |= 2;
        }
        try {
            text.startsWith(none);
        } catch (NullPointerException e) {
            raised |= 4;
        }
        try {
            text.endsWith(none);
        } catch (NullPointerException e) {
            raised |= 8;
        }
        try {
            text.indexOf(none);
        } catch (NullPointerException e) {
            raised |= 16;
        }
        if (!text.equalsIgnoreCase(none)) {
            raised |= 32;
        }
        try {
            Class.forName(none);
        } catch (NullPointerException e) {
            raised |= 64;
        } catch (ClassNotFoundException e) {
            raised |= 0;
        }
        return raised;
    }

    public static int setHugeLength() {
        StringBuffer buffer = new StringBuffer();
        buffer.setLength(Integer.MAX_VALUE);
        return buffer.length();
    }

    public static int sharedBuffer() throws InterruptedException {
        final StringBuffer buffer = new StringBuffer("0");
        Thread writer = new Thread(new Runnable() {
            public void run() {
                for (int i = 0; i < 2000; i++) {
                    buffer.append('x');
                    if (buffer.length() > 64) {
                        buffer.setLength(1);
                    }
                }
            }
        });
        writer.start();
        int seen = 0;
        for (int i = 0; i < 2000; i++) {
            buffer.setCharAt(0, '1');
            seen += buffer.charAt(0) == '1' ? 1 : 0;
            seen += new String(buffer).length() > 0 ? 0 : 1;
        }
        writer.join();
        return seen;
    }

    public static char[] readerUnits(byte[] data) throws IOException {
        InputStreamReader reader = new InputStreamReader(new ByteArrayInputStream(data));
        char[] units = new char[data.length + 1];
        int count = 0;
        int value;
        while ((value = reader.read()) >= 0) {
            units[count++] = (char) value;
        }
        char[] result = new char[count];
        System.arraycopy(units, 0, result, 0, count);
        return result;
    }

    public static int emptyMessageLength() {
        String message = new Exception("").getMessage();
        return message == null ? -1 : message.length();
    }

    public static String emptyMessageText() {
        return new Exception("").toString();
    }

    public static int parseStrictly() {
        int refused = 0;
        try {
            Integer.parseInt("+5");
        } catch (NumberFormatException e) {
            refused |= 1;
        }
        try {
            Long.parseLong(" 12 ");
        } catch (NumberFormatException e) {
            refused |= 2;
        }
        try {
            Byte.parseByte(" 1");
        } catch (NumberFormatException e) {
            refused |= 4;
        }
        try {
            Short.parseShort("1 ");
        } catch (NumberFormatException e) {
            refused |= 8;
        }
        try {
            Integer.valueOf("+1");
        } catch (NumberFormatException e) {
            refused |= 16;
        }
        if (Integer.parseInt("-5") == -5 && Long.parseLong("-12") == -12L) {
            refused |= 32;
        }
        return refused;
    }

    public static int[][] grid(int rows, int columns) {
        return new int[rows][columns];
    }

    static volatile int interrupts = -1;

    public static void interruptSelfThenSleepTwice() {
        Thread thread = new Thread(new Runnable() {
            public void run() {
                int caught = 0;
                Thread.currentThread().interrupt();
                for (int i = 0; i < 2; i++) {
                    try {
                        Thread.sleep(10);
                    } catch (InterruptedException e) {
                        caught++;
                    }
                }
                interrupts = caught;
            }
        });
        thread.start();
    }

    public static int interrupts() {
        return interrupts;
    }

    static volatile int sleeper = 0;

    public static Thread sleepForever() {
        Thread thread = new Thread(new Runnable() {
            public void run() {
                try {
                    Thread.sleep(Long.MAX_VALUE);
                    sleeper = 1;
                } catch (InterruptedException e) {
                    sleeper = 2;
                }
            }
        });
        thread.start();
        return thread;
    }

    public static int sleeper() {
        return sleeper;
    }
}

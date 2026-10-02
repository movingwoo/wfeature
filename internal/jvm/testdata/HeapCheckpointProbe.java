public final class HeapCheckpointProbe {
    public static int initialized = 1;
    public static int ticks;
    public static Object[] left;
    public static Object[] right;
    public static StringBuffer text;
    public static java.util.Calendar calendar;

    public static void prepareCalendar(long millis) {
        calendar = java.util.Calendar.getInstance(java.util.TimeZone.getTimeZone("GMT"));
        calendar.setTime(new java.util.Date(millis));
    }

    public static int calendarMinute(long millis) {
        calendar.setTime(new java.util.Date(millis));
        return calendar.get(java.util.Calendar.HOUR_OF_DAY) * 60
            + calendar.get(java.util.Calendar.MINUTE);
    }

    public static void prepare() {
        left = new Object[2];
        left[0] = left;
        left[1] = new int[] { 40 };
        right = left;
        text = new StringBuffer("a");
        initialized = 7;
    }

    public static int tick() {
        if (left != right || left[0] != left) return -1;
        ticks++;
        text.append('x');
        int[] values = (int[]) left[1];
        values[0]++;
        return values[0] + ticks + text.length() + initialized;
    }
}

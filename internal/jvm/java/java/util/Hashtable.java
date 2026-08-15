package java.util;

public class Hashtable {
    private Object[] keys = new Object[8];
    private Object[] values = new Object[8];
    private int size;

    public Hashtable() {
    }

    public synchronized boolean containsKey(Object key) {
        return find(key) >= 0;
    }

    public synchronized Object get(Object key) {
        int index = find(key);
        return index < 0 ? null : values[index];
    }

    public synchronized boolean isEmpty() {
        return size == 0;
    }

    public synchronized Object put(Object key, Object value) {
        if (key == null || value == null) {
            throw new NullPointerException();
        }
        int index = find(key);
        if (index >= 0) {
            Object previous = values[index];
            values[index] = value;
            return previous;
        }
        ensureCapacity();
        keys[size] = key;
        values[size] = value;
        size++;
        return null;
    }

    public synchronized Object remove(Object key) {
        int index = find(key);
        if (index < 0) {
            return null;
        }
        Object previous = values[index];
        int moved = size - index - 1;
        if (moved > 0) {
            System.arraycopy(keys, index + 1, keys, index, moved);
            System.arraycopy(values, index + 1, values, index, moved);
        }
        size--;
        keys[size] = null;
        values[size] = null;
        return previous;
    }

    private int find(Object key) {
        if (key == null) {
            throw new NullPointerException();
        }
        for (int index = 0; index < size; index++) {
            if (keys[index].equals(key)) {
                return index;
            }
        }
        return -1;
    }

    private void ensureCapacity() {
        if (size < keys.length) {
            return;
        }
        int capacity = keys.length * 2 + 1;
        Object[] nextKeys = new Object[capacity];
        Object[] nextValues = new Object[capacity];
        System.arraycopy(keys, 0, nextKeys, 0, size);
        System.arraycopy(values, 0, nextValues, 0, size);
        keys = nextKeys;
        values = nextValues;
    }
}

package java.util;

public class Vector {
    private Object[] elements;
    private int size;
    private int capacityIncrement;

    public Vector() {
        this(10);
    }

    public Vector(int capacity) {
        this(capacity, 0);
    }

    /**
     * The capacity increment is not a hint. A vector that is full grows by
     * exactly this many slots, and doubles only when the increment is zero,
     * which is what decides what capacity() answers afterwards. One title
     * walks a vector with capacity() as the bound and elementAt as the body,
     * so a vector that grew by more than it was told reads past its elements.
     */
    public Vector(int capacity, int capacityIncrement) {
        if (capacity < 0) {
            throw new IllegalArgumentException();
        }
        elements = new Object[capacity];
        this.capacityIncrement = capacityIncrement;
    }

    public synchronized void addElement(Object value) {
        insertElementAt(value, size);
    }

    public synchronized Object elementAt(int index) {
        checkIndex(index);
        return elements[index];
    }

    public synchronized int indexOf(Object value) {
        for (int index = 0; index < size; index++) {
            if (value == null ? elements[index] == null : value.equals(elements[index])) {
                return index;
            }
        }
        return -1;
    }

    public synchronized void insertElementAt(Object value, int index) {
        if (index < 0 || index > size) {
            throw new ArrayIndexOutOfBoundsException();
        }
        ensureCapacity();
        System.arraycopy(elements, index, elements, index + 1, size - index);
        elements[index] = value;
        size++;
    }

    public synchronized boolean isEmpty() {
        return size == 0;
    }

    public synchronized boolean contains(Object value) {
        return indexOf(value) >= 0;
    }

    public synchronized int lastIndexOf(Object value) {
        for (int index = size - 1; index >= 0; index--) {
            if (value == null ? elements[index] == null : value.equals(elements[index])) {
                return index;
            }
        }
        return -1;
    }

    public synchronized void copyInto(Object[] destination) {
        System.arraycopy(elements, 0, destination, 0, size);
    }

    public synchronized int capacity() {
        return elements.length;
    }

    public synchronized void ensureCapacity(int minimum) {
        if (minimum > elements.length) {
            int capacity;
            if (capacityIncrement > 0) {
                capacity = elements.length + capacityIncrement;
            } else {
                capacity = elements.length * 2 + 1;
            }
            if (capacity < minimum) {
                capacity = minimum;
            }
            Object[] next = new Object[capacity];
            System.arraycopy(elements, 0, next, 0, size);
            elements = next;
        }
    }

    public synchronized void trimToSize() {
        if (size < elements.length) {
            Object[] next = new Object[size];
            System.arraycopy(elements, 0, next, 0, size);
            elements = next;
        }
    }

    public synchronized void setSize(int next) {
        if (next < 0) {
            throw new ArrayIndexOutOfBoundsException();
        }
        ensureCapacity(next);
        for (int index = next; index < size; index++) {
            elements[index] = null;
        }
        size = next;
    }

    public synchronized Object firstElement() {
        checkIndex(0);
        return elements[0];
    }

    public synchronized Object lastElement() {
        checkIndex(size - 1);
        return elements[size - 1];
    }

    public synchronized void setElementAt(Object value, int index) {
        checkIndex(index);
        elements[index] = value;
    }

    public synchronized boolean removeElement(Object value) {
        int index = indexOf(value);
        if (index < 0) {
            return false;
        }
        removeElementAt(index);
        return true;
    }

    public synchronized void removeAllElements() {
        for (int index = 0; index < size; index++) {
            elements[index] = null;
        }
        size = 0;
    }

    public synchronized void removeElementAt(int index) {
        checkIndex(index);
        int moved = size - index - 1;
        if (moved > 0) {
            System.arraycopy(elements, index + 1, elements, index, moved);
        }
        elements[--size] = null;
    }

    public synchronized int size() {
        return size;
    }

    private void checkIndex(int index) {
        if (index < 0 || index >= size) {
            // The index and the size are what say whether the caller counted
            // wrongly or the vector was never filled, and an exception with
            // neither leaves that unanswerable from a log.
            throw new ArrayIndexOutOfBoundsException(index + " of " + size);
        }
    }

    private void ensureCapacity() {
        if (size < elements.length) {
            return;
        }
        ensureCapacity(size + 1);
    }
}

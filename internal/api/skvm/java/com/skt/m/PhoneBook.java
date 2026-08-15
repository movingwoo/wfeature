package com.skt.m;

/**
 * There is no phone book behind this; it reads as empty.
 */
public class PhoneBook {
    public static final int FIELD_NAME = 0;
    public static final int FIELD_NUMBER = 1;

    private PhoneBook() {
    }

    public static native void first();
    public static native int next();
    public static native void findRecord(int field, String value);
    public static native String getField(int recordId, int field);
    public static native String[] getGroupNames();
    public static native int getMaxRecordID();
    public static native String[] getRecord(int recordId);
    public static native boolean isUsed(int recordId);
}

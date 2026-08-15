package com.skt.m;

public class SMSMessage {
    public static final int TYPE_TEXT = 0;
    public static final int TYPE_CALLBACK = 1;
    public static final int TYPE_URL = 2;

    public SMSMessage() {
        init(null, null);
    }

    public SMSMessage(byte[] shortMessage, String sender) {
        init(shortMessage, sender);
    }

    private native void init(byte[] shortMessage, String sender);

    public native byte[] getShortMessage();
    public native byte[] getAppData();
    public native String getSender();
    public native String getName();
    public native String getCName();
    public native String getComment();
    public native String getURL();
    public native byte getServiceOption();
    public native int getType();
}

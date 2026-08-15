package com.skt.m;

/**
 * There is no radio behind this. Every read answers "no message" and send
 * fails, because reporting a delivered message a game can never receive a
 * reply to is worse than reporting none.
 */
public class SMS {
    private SMS() {
    }

    public static native SMSMessage get(int index);
    public static native boolean get(int index, SMSMessage message);
    public static native boolean send(String address, SMSMessage message);
    public static native SMSListener getSMSListener();
    public static native void setSMSListener(SMSListener listener);
}

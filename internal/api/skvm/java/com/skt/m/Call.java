package com.skt.m;

/**
 * There is no telephony behind this; placing a call is refused.
 */
public class Call {
    private Call() {
    }

    public static native boolean call(String number);
}

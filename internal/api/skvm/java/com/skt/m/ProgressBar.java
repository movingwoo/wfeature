package com.skt.m;

public class ProgressBar {
    public ProgressBar(String title) {
        init(title);
    }

    private native void init(String title);

    public native int getValue();
    public native void setValue(int value);
    public native int getMaxValue();
    public native void setMaxValue(int maxValue);
}

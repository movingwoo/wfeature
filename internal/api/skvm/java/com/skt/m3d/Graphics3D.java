package com.skt.m3d;

/**
 * The 3D pipeline's render-state switches. This runtime keeps the state and
 * reports it truthfully; there is no rasterizer behind it, which is what
 * docs/skvm.md records.
 */
public class Graphics3D {
    public Graphics3D() {
    }

    public native void clearZBuffer();
    public native void destroyZBuffer();
    public native boolean isZBufferEnabled();
    public native void setZBufferEnabled(boolean enabled);
    public native boolean isBackfaceCulled();
    public native void setBackfaceCulled(boolean culled);
}

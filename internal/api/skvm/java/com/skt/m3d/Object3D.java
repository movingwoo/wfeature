package com.skt.m3d;

/**
 * A named mesh with a transform. Coordinates are the same fixed-point scale
 * MathFP uses, and the matrix rows are what a game reads back after a
 * transform to do its own projection.
 */
public class Object3D {
    public Object3D(String name) {
        init(name);
    }

    private native void init(String name);

    public native String getName();
    public native void setName(String name);

    public native void addVertex(int x, int y, int z);
    public native void addTriangle(int a, int b, int c, int color);
    public native void setVertices(int[] x, int[] y, int[] z);
    public native void setTriangles(int[] a, int[] b, int[] c, int[] color);

    public native void translate(int x, int y, int z);
    public native void rotate(int x, int y, int z);
    public native void scale(int x, int y, int z);

    public native int[] getMatrixRow0();
    public native int[] getMatrixRow1();
    public native int[] getMatrixRow2();
}

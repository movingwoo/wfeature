package lgt

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestJavaCalendarFieldSlotsAndDateSnapshot(t *testing.T) {
	client := fixtureClient(t)
	calendar, err := javaCalendarGetInstance(client, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dateClass, err := client.preparePlatformJavaClass(javaDateClass)
	if err != nil {
		t.Fatal(err)
	}
	date, err := client.allocateJavaObject(dateClass)
	if err != nil {
		t.Fatal(err)
	}
	initial := time.Date(2024, time.January, 31, 15, 4, 5, 6000000, time.Local)
	client.javaRun.dates[date] = initial.UnixMilli()
	if _, err := javaCalendarSetTime(client, nil, nil, []uint32{calendar, date}); err != nil {
		t.Fatal(err)
	}
	call := func(slot uint32, args ...uint32) uint32 {
		t.Helper()
		for register, value := range append([]uint32{calendar}, args...) {
			if err := client.thread.SetRegister(register, value); err != nil {
				t.Fatal(err)
			}
		}
		served, err := client.callJavaPlatformVirtual(context.Background(), client.thread, javaVirtualSlot(javaCalendarClass, slot))
		if err != nil || !served {
			t.Fatalf("Calendar slot %d: served=%v error=%v", slot, served, err)
		}
		value, err := client.thread.Register(0)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	// A temporarily invalid February 31 must not normalize before DATE is set.
	call(28, javaCalendarMonth, 1)
	call(28, javaCalendarDate, 29)
	call(28, javaCalendarHour, 0)
	snapshot := call(22)
	want := time.Date(2024, time.February, 29, 12, 4, 5, 6000000, time.Local).UnixMilli()
	if got := client.javaRun.dates[snapshot]; got != want {
		t.Fatalf("date = %d, want %d", got, want)
	}
	if got := call(19, javaCalendarMonth); got != 1 {
		t.Fatalf("month = %d", got)
	}
	call(28, javaCalendarDate, 30)
	later := call(22)
	if later == snapshot || client.javaRun.dates[snapshot] != want {
		t.Fatal("getTime did not return independent Date snapshots")
	}
	if got := call(14, javaCalendarMonth); got != 2 {
		t.Fatalf("normalized month = %d", got)
	}
	call(28, javaCalendarYear, 2010)
	if _, err := javaCalendarSetTime(client, nil, nil, []uint32{calendar, date}); err != nil {
		t.Fatal(err)
	}
	if got := client.javaRun.dates[call(22)]; got != initial.UnixMilli() {
		t.Fatal("setTime retained pending field changes")
	}
	if err := client.thread.SetRegister(0, calendar); err != nil {
		t.Fatal(err)
	}
	if err := client.thread.SetRegister(1, 99); err != nil {
		t.Fatal(err)
	}
	_, err = client.callJavaPlatformVirtual(context.Background(), client.thread, javaVirtualSlot(javaCalendarClass, 28))
	if err == nil || !strings.Contains(err.Error(), javaThrowArrayClass) {
		t.Fatalf("invalid field error = %v", err)
	}
}

package lgt

import (
	"context"
	"fmt"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `java/util/Calendar`, which is how a Java title reads the date rather than
// the clock. What it reads it from is the same clock every other call on this
// platform reads — see java_api.go's currentTimeMillis — so a title that
// stamps a save with one and shows the other agrees with itself.
//
// A calendar's own state is this platform's, keyed by the object the module
// allocated, exactly as a String's characters and a Vector's contents are.

const javaCalendarClass = "java/util/Calendar"

type javaCalendar struct {
	millis int64
	// Field assignments normalize when the calendar is read, so successive
	// month/day assignments do not normalize an intermediate invalid date.
	pending *javaCalendarFields
}

type javaCalendarFields struct {
	year, month, day, hour, minute, second, millisecond int
	zone                                                *time.Location
}

// The field numbers the specification fixes. They are `static final`, so the
// compiler has already put them in the caller's instruction stream and this is
// only what the other side of that has to mean.
const (
	javaCalendarYear        = 1
	javaCalendarMonth       = 2
	javaCalendarDate        = 5
	javaCalendarDayOfWeek   = 7
	javaCalendarAMPM        = 9
	javaCalendarHour        = 10
	javaCalendarHourOfDay   = 11
	javaCalendarMinute      = 12
	javaCalendarSecond      = 13
	javaCalendarMillisecond = 14
)

// javaCalendarGetInstance is `Calendar.getInstance()`: the date now. The
// object is this platform's own — a static entry has no receiver to take
// delivery of — so it is allocated here.
func javaCalendarGetInstance(
	client *Client, _ context.Context, _ *armcore.Thread, _ []uint32,
) (uint32, error) {
	class, err := client.preparePlatformJavaClass(javaCalendarClass)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().calendars[object] = javaCalendar{millis: client.clock.unixMillis()}
	return object, nil
}

// javaCalendarOf answers the instant a calendar object stands for.
func (client *Client) javaCalendarOf(object uint32) (time.Time, error) {
	calendar, ok := client.javaRuntimeState().calendars[object]
	if !ok {
		return time.Time{}, fmt.Errorf("the object at %#x is not a calendar this platform built", object)
	}
	if fields := calendar.pending; fields != nil {
		moment := time.Date(fields.year, time.Month(fields.month), fields.day, fields.hour,
			fields.minute, fields.second+fields.millisecond/1000,
			fields.millisecond%1000*int(time.Millisecond), fields.zone)
		calendar.millis, calendar.pending = moment.UnixMilli(), nil
		client.javaRuntimeState().calendars[object] = calendar
	}
	return time.UnixMilli(calendar.millis), nil
}

func javaCalendarSet(client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32) (uint32, error) {
	calendar, ok := client.javaRuntimeState().calendars[arguments[0]]
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a calendar this platform built", arguments[0])
	}
	fields := calendar.pending
	if fields == nil {
		moment := time.UnixMilli(calendar.millis)
		fields = &javaCalendarFields{moment.Year(), int(moment.Month()), moment.Day(), moment.Hour(),
			moment.Minute(), moment.Second(), moment.Nanosecond() / int(time.Millisecond), moment.Location()}
	}
	value := int(int32(arguments[2]))
	switch int32(arguments[1]) {
	case javaCalendarYear:
		fields.year = value
	case javaCalendarMonth:
		fields.month = value + 1
	case javaCalendarDate:
		fields.day = value
	case javaCalendarDayOfWeek:
		moment := time.Date(fields.year, time.Month(fields.month), fields.day, 0, 0, 0, 0, fields.zone)
		fields.day += value - (int(moment.Weekday()) + 1)
	case javaCalendarAMPM:
		fields.hour = fields.hour%12 + value*12
	case javaCalendarHour:
		fields.hour = fields.hour/12*12 + value
	case javaCalendarHourOfDay:
		fields.hour = value
	case javaCalendarMinute:
		fields.minute = value
	case javaCalendarSecond:
		fields.second = value
	case javaCalendarMillisecond:
		fields.millisecond = value
	default:
		return 0, client.throwJavaPlatform(thread, javaThrowArrayClass, "invalid Calendar field")
	}
	calendar.pending = fields
	client.javaRun.calendars[arguments[0]] = calendar
	return 0, nil
}

func javaCalendarGetTime(client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32) (uint32, error) {
	moment, err := client.javaCalendarOf(arguments[0])
	if err != nil {
		return 0, err
	}
	class, err := client.preparePlatformJavaClass(javaDateClass)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	client.javaRun.dates[object] = moment.UnixMilli()
	return object, nil
}

// javaCalendarGet is `get(int)`, slots 14 and 19: one component of the date. The
// components are read in the handset's own zone, which is the zone the clock
// behind them runs in — a title showing a date shows the one on the handset.
func javaCalendarGet(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	moment, err := client.javaCalendarOf(arguments[0])
	if err != nil {
		return 0, err
	}
	var value int
	switch int32(arguments[1]) {
	case javaCalendarYear:
		value = moment.Year()
	case javaCalendarMonth:
		// January is zero, which is the one field the specification numbers
		// differently from the way it reads.
		value = int(moment.Month()) - 1
	case javaCalendarDate:
		value = moment.Day()
	case javaCalendarDayOfWeek:
		// Sunday is one.
		value = int(moment.Weekday()) + 1
	case javaCalendarAMPM:
		value = moment.Hour() / 12
	case javaCalendarHour:
		value = moment.Hour() % 12
	case javaCalendarHourOfDay:
		value = moment.Hour()
	case javaCalendarMinute:
		value = moment.Minute()
	case javaCalendarSecond:
		value = moment.Second()
	case javaCalendarMillisecond:
		value = moment.Nanosecond() / int(time.Millisecond)
	default:
		return 0, fmt.Errorf("Calendar field %d", int32(arguments[1]))
	}
	return uint32(int32(value)), nil
}

// `java/util/Date`, the instant behind a calendar. A title reaches it directly
// when what it wants is a number to store rather than fields to show.

const (
	javaDateClass     = "java/util/Date"
	javaTimeZoneClass = "java/util/TimeZone"
)

// javaDateNow is `Date()`: the instant now, read off the same clock everything
// else on this platform reads.
func javaDateNow(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaRuntimeState().dates[arguments[0]] = client.clock.unixMillis()
	return 0, nil
}

// javaDateAt is `Date(long)`: an instant a title has kept.
func javaDateAt(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	client.javaRuntimeState().dates[arguments[0]] = int64(arguments[2])<<32 | int64(arguments[1])
	return 0, nil
}

// javaDateTime is `getTime()`, slot 10: the instant as the number a title
// stores. The answer is sixty-four bits, which this platform returns the way
// every other long-valued call does.
func javaDateTime(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	millis, ok := client.javaRuntimeState().dates[arguments[0]]
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a date this platform built", arguments[0])
	}
	// A long comes back in two registers, the same way currentTimeMillis
	// hands one over.
	if err := thread.SetRegister(1, uint32(uint64(millis)>>32)); err != nil {
		return 0, err
	}
	return uint32(uint64(millis)), nil
}

// javaTimeZoneOffset is `TimeZone.getRawOffset()`, slot 11: milliseconds east
// of GMT. It is read at the instant the clock stands at rather than fixed,
// because a zone that observes daylight saving has two of them.
func javaTimeZoneOffset(
	client *Client, _ context.Context, _ *armcore.Thread, _ []uint32,
) (uint32, error) {
	_, seconds := time.UnixMilli(client.clock.unixMillis()).Zone()
	return uint32(int32(seconds) * 1000), nil
}

// javaCalendarSetTime is `Calendar.setTime(Date)`: the calendar is moved to the
// instant the Date stands for, and every component read off it afterwards is
// read from there.
func javaCalendarSetTime(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	runtime := client.javaRuntimeState()
	if _, ok := runtime.calendars[arguments[0]]; !ok {
		return 0, fmt.Errorf("the object at %#x is not a calendar this platform built", arguments[0])
	}
	millis, ok := runtime.dates[arguments[1]]
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a date this platform built", arguments[1])
	}
	runtime.calendars[arguments[0]] = javaCalendar{millis: millis}
	return 0, nil
}

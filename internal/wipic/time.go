package wipic

import "time"

// TimeRecordSize is a C library's struct tm: nine ints.
const TimeRecordSize = 36

// TimeFields are the nine members of the struct tm a C library answers for
// moment, in declaration order: seconds, minutes, hours, the day of the month,
// the month counted from zero, the year counted from 1900, the day of the
// week, the day of the year counted from zero, and the daylight-saving flag.
//
// The WIPI specification lists localtime among the ANSI-C functions a platform
// supports without restating its interface, so the contract it defers to is
// ANSI C's. C89 does not fix the members' order, but every ARM toolchain a
// title here was built with lays them out in declaration order. No daylight
// saving is reported: the handsets these titles ran on kept Korean time, which
// has none to report.
func TimeFields(moment time.Time) [9]int32 {
	return [9]int32{
		int32(moment.Second()), int32(moment.Minute()), int32(moment.Hour()),
		int32(moment.Day()), int32(moment.Month()) - 1, int32(moment.Year() - 1900),
		int32(moment.Weekday()), int32(moment.YearDay() - 1), 0,
	}
}

// TimeRecord is TimeFields as the little-endian record a guest reads.
func TimeRecord(moment time.Time) []byte {
	record := make([]byte, TimeRecordSize)
	for index, value := range TimeFields(moment) {
		record[index*4] = byte(value)
		record[index*4+1] = byte(value >> 8)
		record[index*4+2] = byte(value >> 16)
		record[index*4+3] = byte(value >> 24)
	}
	return record
}

package wipi

// PlayListener event identifiers from the WIPI Java media contract.
const (
	PlayEventError      int32 = -1
	PlayEventEndOfData  int32 = 1
	PlayEventStart      int32 = 2
	PlayEventStop       int32 = 3
	PlayEventPause      int32 = 4
	PlayEventResume     int32 = 5
	PlayEventRecord     int32 = 6
	PlayEventFullOfData int32 = 7
)

package backend

// FrameUpdate owns its RGBA pixels. Scale is an optional Host-side presentation
// factor; execution never applies the filter itself.
type FrameUpdate struct {
	RGBA          []byte
	Width, Height int
	Scale         int
	// Force asks the Host to present even an unchanged picture, for example
	// after reconnecting or changing display settings. It is not guest state.
	Force bool
}

// FrameSink offers owned frames without waiting for the Host. The Host supplies
// a bounded channel and must not close it until the runtime has stopped.
type FrameSink struct {
	Output chan<- FrameUpdate
	Scale  int
}

func (sink FrameSink) Offer(frame FrameUpdate) bool {
	frame.Scale = sink.Scale
	select {
	case sink.Output <- frame:
		return true
	default:
		return false
	}
}

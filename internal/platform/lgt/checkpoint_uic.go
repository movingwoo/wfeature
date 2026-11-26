package lgt

import (
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
)

// Sessions without a live date component still use their earlier record.
// A hole in Dates belongs to a destroyed handle and survives a quick load.
const sessionDateCheckpointVersion = 9

type sessionDateCheckpointState struct {
	Version uint32
	Image   sessionImageCheckpointState
	Dates   []*uicDate
}

func encodeLGTDateRecord(image sessionImageCheckpointState, dates []*uicDate) ([]byte, error) {
	if len(dates) == 0 {
		return image.encode()
	}
	image.Version = sessionImageCheckpointVersion
	return backend.EncodeCheckpointRecord(sessionDateCheckpointState{sessionDateCheckpointVersion, image, dates})
}

func decodeLGTDateRecord(data []byte) (sessionImageCheckpointState, []*uicDate, error) {
	if version, ok := leadingRecordVersion(data); !ok || version != sessionDateCheckpointVersion {
		image, err := decodeLGTImageRecord(data)
		return image, nil, err
	}
	var saved sessionDateCheckpointState
	if err := backend.DecodeCheckpointRecord(data, &saved); err != nil {
		return sessionImageCheckpointState{}, nil, err
	}
	if n := len(saved.Dates); n == 0 || n > maxDateComponents || saved.Dates[n-1] == nil {
		return sessionImageCheckpointState{}, nil, fmt.Errorf("LGT checkpoint has an invalid date component table")
	}
	if saved.Image.Version != sessionImageCheckpointVersion || len(saved.Image.Sources) > len(saved.Image.Session.Client.Framebuffers) {
		return sessionImageCheckpointState{}, nil, fmt.Errorf("LGT checkpoint has an invalid image record")
	}
	return saved.Image, saved.Dates, nil
}

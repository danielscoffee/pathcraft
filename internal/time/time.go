package time

import (
	"fmt"
	"strconv"
	"strings"
)

// Time represents a GTFS-compatible time value expressed
// as seconds since midnight. It supports values greater
// than 24 hours (e.g. 25:30:00), as defined by the GTFS spec.
type Time int

type Seconds float64

func ParseTime(s string) (Time, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid time format: %q", s)
	}

	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid hours: %w", err)
	}
	if hours < 0 {
		return 0, fmt.Errorf("invalid hours: must be non-negative")
	}

	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid minutes: %w", err)
	}
	if minutes < 0 || minutes > 59 {
		return 0, fmt.Errorf("invalid minutes: must be between 0 and 59")
	}

	seconds, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, fmt.Errorf("invalid seconds: %w", err)
	}
	if seconds < 0 || seconds > 59 {
		return 0, fmt.Errorf("invalid seconds: must be between 0 and 59")
	}

	return Time(hours*SecondsPerHour + minutes*SecondsPerMinute + seconds), nil
}

func (t Time) String() string {
	hours := int(t) / SecondsPerHour
	minutes := (int(t) % SecondsPerHour) / SecondsPerMinute
	seconds := int(t) % SecondsPerMinute
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

package codex

import "math"

// Prefer the owner's recorded elapsed time.
// App-server timestamps are seconds; never substitute receipt time or sum tools.
func turnDuration(duration, started, completed *int64) *int64 {
	if duration != nil && *duration >= 0 {
		return duration
	}
	if started == nil || completed == nil || *started <= 0 || *completed < *started {
		return nil
	}
	seconds := *completed - *started
	if seconds > math.MaxInt64/1000 {
		return nil
	}
	return new(seconds * 1000)
}

package hubui

import "time"

func (s *State) localTime(t time.Time) time.Time {
	if s.timezone != nil {
		return t.In(s.timezone)
	}
	return platformLocalTime(t)
}
func controllerTimezone(name string, offsetSeconds int) *time.Location {
	if name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.FixedZone(name, offsetSeconds)
}

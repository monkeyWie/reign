//go:build !js

package hubui

import (
	"runtime"
	"time"
)

func platformLocalTime(t time.Time) time.Time { return t.Local() }
func (s *State) refreshTimezone() {
	if runtime.GOOS != "android" && runtime.GOOS != "ios" {
		return
	}
	s.cx.CallHost("ras.timezone", nil, func(v any, err error) {
		if err != nil || s.ctx.Err() != nil {
			return
		}
		zone := object(v)
		s.timezone = controllerTimezone(str(zone, "name"), int(number(zone, "offsetSeconds")))
		s.changed()
	})
}

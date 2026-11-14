package hubui

import (
	"testing"
	"time"
	_ "time/tzdata"
)

func TestMessageTimesUseControllerTimezone(t *testing.T) {
	instant := time.Date(2026, 11, 14, 22, 57, 0, 0, time.UTC)
	for _, tc := range []struct {
		zone   string
		offset int
		want   string
	}{
		{"Asia/Shanghai", 8 * 3600, "2026-11-15 06:57"},
		{"America/New_York", -5 * 3600, "2026-11-14 17:57"},
		{"Unknown/OEM", 19800, "2026-11-15 04:27"},
	} {
		s := &State{timezone: controllerTimezone(tc.zone, tc.offset)}
		if got := s.localTime(instant).Format("2006-01-02 15:04"); got != tc.want {
			t.Fatalf("%s: %s", tc.zone, got)
		}
	}
	// Use the zone's offset at the message instant, not today's DST offset.
	s := &State{timezone: controllerTimezone("America/New_York", -5*3600)}
	summer := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	if got := s.localTime(summer).Format("15:04"); got != "08:00" {
		t.Fatal(got)
	}
}

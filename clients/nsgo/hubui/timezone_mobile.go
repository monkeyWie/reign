//go:build android || ios

package hubui

// Mobile sandboxes do not necessarily expose the system zoneinfo database.
import _ "time/tzdata"

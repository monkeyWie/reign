package provider

// TimestampMillis normalizes Agent timestamps at the adapter boundary. Zero
// remains unknown; clients never substitute a changing activity time for birth.
func TimestampMillis(value int64) int64 {
	if value > 0 && value < 100_000_000_000 {
		return value * 1000
	}
	return value
}

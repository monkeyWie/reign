package provider

// ConversationRecency excludes tool progress, assistant chunks and read clocks.
// Providers record successful completion on the turn, and submission on user items.
func ConversationRecency(turns []Turn) int64 {
	var latest int64
	for _, turn := range turns {
		latest = max(latest, turn.RecencyAt)
		for _, item := range turn.Items {
			if item.Role == "user" && item.TimestampMS != nil && item.Status != "failed" && item.Status != "rejected" {
				latest = max(latest, *item.TimestampMS)
			}
		}
	}
	return latest
}

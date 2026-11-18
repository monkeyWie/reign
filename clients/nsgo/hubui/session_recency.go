package hubui

func transcriptRecency(transcript Object) int64 {
	latest := number(object(transcript["session"]), "recencyAt")
	for _, turn := range objects(transcript["turns"]) {
		latest = max(latest, number(turn, "recencyAt"))
		for _, item := range objects(turn["items"]) {
			if str(item, "role") == "user" && str(item, "status") != "failed" && str(item, "status") != "rejected" {
				latest = max(latest, number(item, "timestampMs"))
			}
		}
	}
	return latest
}

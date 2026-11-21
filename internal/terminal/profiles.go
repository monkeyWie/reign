package terminal

import (
	"context"
	"errors"
)

// Only the ID and display name cross the endpoint. The host resolves the
// executable again on open so removing a shell cannot launch a different one.
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	path string
}

func profilePath(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	profiles, err := Profiles(ctx)
	if err != nil {
		return "", err
	}
	for _, profile := range profiles {
		if profile.ID == id {
			return profile.path, nil
		}
	}
	return "", errors.New("此终端已不可用，请重新选择")
}

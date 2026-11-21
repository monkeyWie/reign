//go:build !windows && (!darwin || ios) && (!linux || android)

package terminal

import "context"

const SupportsProfiles = false

func Profiles(context.Context) ([]Profile, error) { return []Profile{}, nil }

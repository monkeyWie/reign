package webapi

import (
	"context"

	"errors"
	"github.com/monkeyWie/reignhub/pkg/persistence"
)

// Read IndexedDB directly even when a live connection exists. No resolve,
// registration or peer RPC is performed on this path.
func (a *API) cached(target string, command Object) (any, error) {
	if str(command, "method") != "" {
		switch str(command, "method") {
		case "providers", "sessions", "models", "transcript", "approvals":
		default:
			return nil, errors.New("only read snapshots can be loaded from cache")
		}
		if str(command, "method") == "transcript" && str(command, "cursor") == "" {
			page, err := a.store.ReadPage(context.Background(), persistence.Session{Env: target, Provider: str(command, "provider"), ID: str(command, "session")}, persistence.PageQuery{})
			v := page.View()
			if err != nil {
				return nil, err
			}
			if v != nil {
				return v, nil
			}
		}
		rows, e := a.store.LoadList(context.Background(), persistence.Session{Env: target, Provider: str(command, "provider"), ID: str(command, "session")}, persistence.ReadKind(str(command, "method")))
		if rows == nil {
			return nil, e
		}
		return rows, e
	}
	providers, err := a.cached(target, Object{"method": "providers", "provider": "", "session": ""})
	if err != nil {
		return nil, err
	}
	sessions, err := a.store.ListSessions(context.Background(), persistence.DirectoryKey{Env: target})
	if err != nil {
		return nil, err
	}
	return Object{"providers": providers, "sessions": sessions}, nil
}

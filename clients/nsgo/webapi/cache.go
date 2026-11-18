package webapi

import (
	"encoding/json"
	"errors"
	"strings"
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
			v, err := a.store.Get(snapshotKey(target, str(command, "provider"), str(command, "session")))
			if err != nil || v != nil {
				return v, err
			}
		}
		b, err := json.Marshal(command)
		if err != nil {
			return nil, err
		}
		return a.store.Get("rpc:" + target + ":" + string(b))
	}
	providers, err := a.cached(target, Object{"method": "providers", "provider": "", "session": ""})
	if err != nil {
		return nil, err
	}
	keys, err := a.store.Keys()
	if err != nil {
		return nil, err
	}
	sessions := []Object{}
	directories := map[string]bool{}
	for _, key := range keys {
		if !strings.HasPrefix(key, "directory:"+target+":") {
			continue
		}
		v, err := a.store.Get(key)
		if err != nil {
			return nil, err
		}
		directories[key] = true
		for _, session := range obj(obj(v)["items"]) {
			sessions = append(sessions, obj(session))
		}
	}
	for _, key := range keys {
		prefix := "rpc:" + target + ":"
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		var request Object
		if json.Unmarshal([]byte(strings.TrimPrefix(key, prefix)), &request) != nil || str(request, "method") != "sessions" || str(request, "provider") == "" || str(request, "session") != "" || str(request, "cursor") != "" || directories["directory:"+target+":"+str(request, "provider")] {
			continue
		}
		page, err := a.store.Get(key)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, arr(obj(page)["sessions"])...)
	}

	return Object{"providers": providers, "sessions": sessions}, nil
}

func snapshotKey(target, provider, session string) string {
	b, _ := json.Marshal([]string{provider, session})
	return "snapshot:" + target + ":" + string(b)
}

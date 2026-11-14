package device

import (
	"context"
	"sync"

	"github.com/monkeyWie/reignhub/internal/connectivity"
)

type PresenceTarget struct {
	ID, Service, Host, Token string
}

type DeviceStatus struct {
	ID        string `json:"id"`
	Host      string `json:"host"`
	Name      string `json:"name,omitempty"`
	Online    *bool  `json:"online"`
	CheckedAt int64  `json:"checkedAt,omitempty"`
}

// Presence queries the directory only. It never opens a control connection or
// renews pairing. Failed/unauthorized lookups remain unknown, never offline.
func Presence(ctx context.Context, targets []PresenceTarget) []DeviceStatus {
	out := make([]DeviceStatus, len(targets))
	groups := map[string][]int{}
	for i, t := range targets {
		out[i] = DeviceStatus{ID: t.ID, Host: t.Host}
		if t.Token != "" && t.Host != "" {
			groups[t.Service] = append(groups[t.Service], i)
		}
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for service, indices := range groups {
		for start := 0; start < len(indices); start += 50 {
			batch := indices[start:min(start+50, len(indices))]
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case slots <- struct{}{}:
					defer func() { <-slots }()
				case <-ctx.Done():
					return
				}
				hosts := make([]string, len(batch))
				for i, index := range batch {
					hosts[i] = targets[index].Host
				}
				var result struct {
					Devices []DeviceStatus `json:"devices"`
				}
				call, cancel := context.WithTimeout(ctx, connectivity.StageTimeout)
				defer cancel()
				if Post(call, service, targets[batch[0]].Token, "/p2p/devices/status", map[string]any{"hosts": hosts}, &result) != nil {
					return
				}
				byHost := map[string]DeviceStatus{}
				for _, status := range result.Devices {
					byHost[status.Host] = status
				}
				for _, index := range batch {
					if status, ok := byHost[targets[index].Host]; ok {
						status.ID = targets[index].ID
						out[index] = status
					}
				}
			}()
		}
	}
	wg.Wait()
	return out
}

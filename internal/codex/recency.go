package codex

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/internal/provider"
)

func recordedTurnRecency(status string, started, completed, startedMS, completedMS, duration *int64) int64 {
	timestamp := func(seconds, millis *int64) int64 {
		if millis != nil && *millis > 0 {
			return *millis
		}
		if seconds != nil && *seconds > 0 && *seconds <= math.MaxInt64/1000 {
			return *seconds * 1000
		}
		return 0
	}
	start := timestamp(started, startedMS)
	if status != "completed" {
		return start
	}
	end := timestamp(completed, completedMS)
	// IPC supplies the recorded start and elapsed duration, including for
	// canonical history. Never substitute a polling or streaming receipt time.
	if end == 0 && start > 0 && duration != nil && *duration >= 0 && *duration <= math.MaxInt64-start {
		end = start + *duration
	}
	return max(start, end)
}

type sessionRecencyEntry struct {
	updated, recent int64
	status          string
	checked         bool
	touched         uint64
}

func (p *Provider) cacheSessionRecency(id string, entry sessionRecencyEntry) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.recencies == nil {
		p.recencies = map[string]sessionRecencyEntry{}
	}
	entry.recent = max(entry.recent, p.recencies[id].recent)
	p.recencySequence++
	entry.touched = p.recencySequence
	p.recencies[id] = entry
	if len(p.recencies) > 2048 {
		oldest, age := "", p.recencySequence
		for key, candidate := range p.recencies {
			if candidate.touched < age {
				oldest, age = key, candidate.touched
			}
		}
		delete(p.recencies, oldest)
	}
	return entry.recent
}

func (p *Provider) rememberSessionRecency(id string, recent int64) {
	if recent <= 0 {
		return
	}
	p.mu.Lock()
	entry := p.recencies[id]
	p.mu.Unlock()
	entry.recent = max(entry.recent, recent)
	p.cacheSessionRecency(id, entry)
}

// Historical thread/list.updatedAt is not a conversation boundary. Fetch only
// the latest turn's metadata (no items), with bounded concurrency and caching.
// Live App snapshots already carry these boundaries and need no extra RPC.
func (p *Provider) enrichSessionRecency(ctx context.Context, sessions []provider.Session) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	slots := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i := range sessions {
		ss := &sessions[i]
		p.mu.Lock()
		cached := p.recencies[ss.ID]
		p.mu.Unlock()
		entry := sessionRecencyEntry{updated: ss.UpdatedAt, status: ss.Status, recent: max(cached.recent, ss.RecencyAt)}
		if ss.RecencyAt > 0 || cached.checked && cached.updated == ss.UpdatedAt && cached.status == ss.Status {
			entry.checked = true
			ss.RecencyAt = p.cacheSessionRecency(ss.ID, entry)
			continue
		}
		ss.RecencyAt = cached.recent
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			var out struct{ Data []json.RawMessage }
			if err := p.call(ctx, "thread/turns/list", map[string]any{"threadId": ss.ID, "limit": 1, "sortDirection": "desc", "itemsView": "notLoaded"}, &out); err != nil {
				return // Unknown timing cannot turn a poll into new activity.
			}
			for _, raw := range out.Data {
				entry.recent = max(entry.recent, normalizeTurn(raw).RecencyAt)
			}
			entry.checked = true
			ss.RecencyAt = p.cacheSessionRecency(ss.ID, entry)
		}()
	}
	wg.Wait()
}

package hubui

import (
	"context"
	"encoding/json"
	"time"
)

// All presentation state belongs to the UI loop. Watch, snapshots and approval
// reads use separate requests so a slow history transfer cannot hold up text.
type sessionStream struct {
	s                                     *State
	ctx                                   context.Context
	target, provider, session, key, epoch string
	cursor                                int64
	baseline, snapshotInFlight, invalid   bool
	snapshotAgain                         bool
	journal                               []Object
	journalBytes                          int
	syncVersion                           uint64
	approvals                             chan struct{}
}

func (w *sessionStream) current() bool {
	return w.ctx.Err() == nil && w.s.ctx.Err() == nil && w.s.stream == w && w.s.key() == w.key
}
func (w *sessionStream) syncing(value bool) {
	if !w.current() {
		return
	}
	s := w.s
	if s.transcriptSyncing == value {
		return
	}
	w.syncVersion++
	version := w.syncVersion
	s.transcriptSyncing = value
	s.syncVisible = false
	if value {
		time.AfterFunc(300*time.Millisecond, func() {
			s.cx.Post(func() {
				if w.current() && s.transcriptSyncing && w.syncVersion == version {
					s.syncVisible = true
					s.changed()
				}
			})
		})
	}
	s.changed()
}
func (w *sessionStream) checkpoint() {
	if w.baseline && !w.snapshotInFlight {
		w.s.streamKey, w.s.streamEpoch, w.s.streamCursor = w.key, w.epoch, w.cursor
	}
}
func (w *sessionStream) refreshSnapshot() {
	if w.snapshotInFlight {
		w.snapshotAgain = true
		return
	}
	w.startSnapshot()
}
func (w *sessionStream) startSnapshot() {
	if !w.current() || w.snapshotInFlight {
		return
	}
	w.snapshotInFlight = true
	w.snapshotAgain = false
	w.invalid = false
	w.journal = nil
	w.journalBytes = 0
	w.syncing(true)
	started, epoch := time.Now(), w.epoch
	go func() {
		v, err := rpc(w.ctx, w.s.api, w.target, w.provider, w.session, "transcript", Object{"fresh": true})
		w.s.cx.Post(func() {
			if !w.current() {
				return
			}
			w.snapshotInFlight = false
			w.s.setConnectionIssue(w.target, "transcript", err)
			if err != nil {
				w.journal = nil
				w.syncing(false)
				time.AfterFunc(time.Second, func() {
					w.s.cx.Post(func() {
						if w.current() {
							w.startSnapshot()
						}
					})
				})
				return
			}
			t := object(v)
			if w.invalid || epoch != w.epoch || str(t, "streamEpoch") != "" && str(t, "streamEpoch") != w.epoch {
				w.startSnapshot()
				return
			}
			// A completed history response may predate live text already on screen.
			// Replay everything received since the request began; UTF-16 offsets make
			// overlapping snapshot/delta content idempotent.
			replay := w.journal
			if str(t, "streamEpoch") != "" {
				replay = nil
				for _, event := range w.journal {
					if number(event, "id") > number(t, "streamCursor") {
						replay = append(replay, event)
					}
				}
			}
			var refresh bool
			t, refresh = applyEvents(t, replay)
			w.snapshotAgain = w.snapshotAgain || refresh
			w.journal = nil
			w.baseline = true
			w.s.setTranscript(t)
			w.s.reconcileOutgoing(started)
			w.s.rebuildMessages()
			w.s.trackNativeTask(w.target, w.provider, w.session, t)
			if !w.snapshotAgain {
				w.checkpoint()
			}
			w.syncing(false)
			if w.snapshotAgain {
				w.startSnapshot()
			}
		})
	}()
}
func (w *sessionStream) batch(b Object) {
	if !w.current() {
		return
	}
	events := objects(b["events"])
	if flag(b, "reset") {
		w.baseline = false
		w.s.streamEpoch, w.s.streamCursor = "", 0
		if w.snapshotInFlight {
			w.invalid = true
		}
	}
	w.epoch, w.cursor = str(b, "epoch"), number(b, "cursor")
	if !w.baseline && !w.snapshotInFlight {
		w.startSnapshot()
	}
	if w.snapshotInFlight && !w.invalid {
		for _, event := range events {
			raw, _ := json.Marshal(event)
			w.journalBytes += len(raw)
			if len(w.journal) >= 1024 || w.journalBytes > 2<<20 {
				w.invalid = true
				w.journal = nil
				break
			}
			w.journal = append(w.journal, event)
		}
	}
	if len(events) > 0 {
		s := w.s
		if w.baseline && !s.transcriptSyncing {
			if s.animateText == nil {
				s.animateText = map[string]bool{}
			}
			for _, event := range events {
				if str(event, "kind") == "text" {
					s.animateText[str(event, "turnId")+"|"+str(event, "itemId")] = true
				}
			}
		}
		next, refresh := applyEvents(s.transcript, events)
		s.transcript = next
		s.trackNativeTask(w.target, w.provider, w.session, next)
		s.reconcileOutgoing(time.Time{})
		s.rebuildMessages()
		s.scheduleSnapshot()
		s.changed()
		if refresh {
			w.startSnapshot()
		}
	}
	w.checkpoint()
	select {
	case w.approvals <- struct{}{}:
	default:
	}
}
func (w *sessionStream) run(epoch string, cursor int64) {
	go w.pollApprovals()
	probe := true
	for w.ctx.Err() == nil {
		v, err := rpc(w.ctx, w.s.api, w.target, w.provider, w.session, "watch", Object{"epoch": epoch, "after": cursor, "probe": probe})
		if w.ctx.Err() != nil {
			return
		}
		applied := make(chan struct{}, 1)
		w.s.cx.Post(func() {
			if w.current() {
				w.s.setConnectionIssue(w.target, "watch", err)
				if err == nil {
					w.batch(object(v))
				} else {
					w.syncing(false)
				}
				w.s.changed()
			}
			applied <- struct{}{}
		})
		select {
		case <-w.ctx.Done():
			return
		case <-applied:
		}
		if err != nil {
			select {
			case <-w.ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		b := object(v)
		epoch, cursor = str(b, "epoch"), number(b, "cursor")
		probe = false
		// Prevent a broken/unsupported adapter from spinning on empty replies.
		select {
		case <-w.ctx.Done():
			return
		case <-time.After(16 * time.Millisecond):
		}
	}
}
func (w *sessionStream) pollApprovals() {
	timer := time.NewTicker(5 * time.Second)
	defer timer.Stop()
	for w.ctx.Err() == nil {
		v, err := rpc(w.ctx, w.s.api, w.target, w.provider, w.session, "approvals", nil)
		w.s.cx.Post(func() {
			if w.current() && err == nil {
				w.s.approvals = objects(v)
				w.s.changed()
			}
		})
		select {
		case <-w.ctx.Done():
			return
		case <-timer.C:
		case <-w.approvals:
		}
		// Coalesce event bursts to at most one approval read per second.
		select {
		case <-w.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

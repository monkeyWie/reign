package hubui

import (
	"context"
	"time"
)

type nativeTaskMonitor struct{ cancel context.CancelFunc }

// A running task must still finish its notification after its chat is closed
// or another device is selected. Reuse the Provider's existing event protocol.
func (s *State) syncNativeTaskMonitors() {
	if s.nativeTaskMonitors == nil {
		s.nativeTaskMonitors = map[string]*nativeTaskMonitor{}
	}
	for key, task := range s.nativeTasks {
		target := str(task, "target")
		connected := false
		for _, device := range s.devices {
			if str(device, "id") == target && flag(device, "connected") {
				connected = true
			}
		}
		selected := s.key() == target+"|"+str(task, "providerId")+"|"+str(task, "id")
		needed := connected && !selected && !s.suspended && str(task, "status") == "inProgress"
		monitor := s.nativeTaskMonitors[key]
		if !needed && monitor != nil {
			monitor.cancel()
			delete(s.nativeTaskMonitors, key)
		}
		if needed && monitor == nil {
			ctx, cancel := context.WithCancel(s.ctx)
			monitor = &nativeTaskMonitor{cancel: cancel}
			s.nativeTaskMonitors[key] = monitor
			go s.watchNativeTask(ctx, key, copyObject(task), monitor)
		}
	}
}

func (s *State) watchNativeTask(ctx context.Context, key string, task Object, monitor *nativeTaskMonitor) {
	target, provider, session := str(task, "target"), str(task, "providerId"), str(task, "id")
	post := func(turn Object) bool {
		status := str(turn, "status")
		if str(turn, "id") != str(task, "turnId") || status == "" {
			return false
		}
		s.cx.Post(func() {
			if ctx.Err() == nil && s.nativeTaskMonitors[key] == monitor {
				s.trackNativeTask(target, provider, session, Object{"session": task, "turns": []Object{turn}})
				s.changed()
			}
		})
		return status != "inProgress"
	}
	snapshot := func() bool {
		value, err := rpc(ctx, s.api, target, provider, session, "transcript", nil)
		if err == nil {
			for _, turn := range objects(object(value)["turns"]) {
				if post(turn) {
					return true
				}
			}
		}
		return false
	}
	if snapshot() {
		return
	}
	epoch, cursor := "", int64(0)
	for ctx.Err() == nil {
		value, err := rpc(ctx, s.api, target, provider, session, "watch", Object{"epoch": epoch, "after": cursor})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		batch := object(value)
		for _, event := range objects(batch["events"]) {
			if str(event, "kind") == "turn" && post(Object{"id": event["turnId"], "status": event["status"]}) {
				return
			}
		}
		if (flag(batch, "reset") || flag(batch, "heartbeat")) && snapshot() {
			return
		}
		epoch, cursor = str(batch, "epoch"), number(batch, "cursor")
	}
}

package hubui

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// The native host requests Android foreground-service protection. A denied or
// stopped service does not cancel Go synchronization while execution continues.
func (s *State) observeNativeBackground() {
	if isWeb() || s.cx.Env().OS() != core.OSAndroid {
		return
	}
	s.cx.Effect(func() {
		s.read()
		s.syncNativeTaskMonitors()
		status := s.nativeBackgroundStatus()
		encoded, _ := json.Marshal(status)
		if s.nativeBackgroundState == string(encoded) {
			return
		}
		s.nativeBackgroundState = string(encoded)
		s.cx.CallHost("ras.background", status, func(v any, err error) {
			if s.ctx.Err() != nil || s.nativeBackgroundState != string(encoded) {
				return
			}
			s.keepAlive = err == nil && flag(object(v), "running")
			if s.keepAlive && !s.notificationAsked {
				s.notificationAsked = true
				s.cx.RequestNotificationPermission(func(core.NotificationPermission) {
					s.nativeBackgroundState = ""
					s.changed()
				})
			}
		})
	})
	s.cx.OnHostEvent("ras.background-state", func(v any) {
		s.keepAlive = flag(object(v), "running")
	})
	s.cx.OnHostEvent("ras.network-change", func(any) {
		// A network change ends the recovery pause, never a healthy data path.
		for id := range s.retrying {
			delete(s.retrying, id)
		}
		s.directoryChecked = time.Time{}
		s.presenceGeneration++
		s.presencePolling = false
		s.pollPresence()
	})
	s.cx.OnHostEvent("ras.open-session", func(v any) {
		session := object(v)
		if str(session, "target") == "" || str(session, "id") == "" {
			return
		}
		s.pendingNativeSession = session
		s.openNativeSession()
	})
}

func (s *State) openNativeSession() {
	session := s.pendingNativeSession
	if session == nil {
		return
	}
	for _, device := range s.devices {
		if str(device, "id") == str(session, "target") {
			s.pendingNativeSession = nil
			s.openDevice(str(device, "id"))
			s.selectSession(session)
			return
		}
	}
}

func (s *State) nativeBackgroundStatus() Object {
	connected, connecting := 0, 0
	for _, d := range s.devices {
		id := str(d, "id")
		if flag(d, "connected") {
			connected++
		} else if h := s.connections[id]; h != nil && (h.reconnecting || h.established && flag(d, "autoReconnect")) {
			connecting++
		}
	}
	if s.connectAttempt != nil && s.connectAttempt.active && connecting == 0 {
		connecting++
	}
	title, body := "ReignHub", ""
	if connected > 0 {
		title = fmt.Sprintf("ReignHub · 已连接 %d 台设备", connected)
		body = "后台连接保持中"
	} else if connecting > 0 {
		body = "正在连接设备…"
	}
	if task := s.lastTask; task != nil {
		body = str(task, "title") + " · " + str(task, "label")
		if connected == 0 {
			body += " · 等待恢复连接"
		}
	}
	keys := make([]string, 0, len(s.nativeTasks))
	for key := range s.nativeTasks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tasks := make([]Object, 0, len(keys))
	for _, key := range keys {
		task := copyObject(s.nativeTasks[key])
		for _, device := range s.devices {
			if str(device, "id") == str(task, "target") {
				task["connected"] = flag(device, "connected")
			}
		}
		tasks = append(tasks, task)
	}
	return Object{"enabled": connected+connecting > 0, "title": title, "body": body, "tasks": tasks}
}

// Only live snapshots/deltas enter here. Reading a historical cached answer
// must never generate a completion notification or a fictitious running task.
func (s *State) trackNativeTask(target, provider, session string, transcript Object) {
	if isWeb() || s.cx.Env().OS() != core.OSAndroid {
		return
	}
	turns := objects(transcript["turns"])
	for index, turn := range turns {
		key := target + "|" + provider + "|" + session + "|" + str(turn, "id")
		// Old turns may contain historical inProgress flags. Discover only
		// the latest turn; earlier known runs still receive terminal updates.
		if index != len(turns)-1 && s.nativeTasks[key] == nil {
			continue
		}
		s.trackNativeTurn(target, provider, session, object(transcript["session"]), turn)
	}
}

func (s *State) trackNativeTurn(target, provider, session string, info, turn Object) {
	key := target + "|" + provider + "|" + session + "|" + str(turn, "id")
	status := str(turn, "status")
	known := s.nativeTasks[key]
	if status != "inProgress" && known == nil || status == "inProgress" && known != nil && str(known, "status") != "inProgress" {
		return
	}
	label := "正在运行"
	switch status {
	case "completed":
		label = "已完成"
	case "failed":
		label = "运行失败"
	case "interrupted":
		label = "已停止"
	case "inProgress":
	default:
		return
	}
	title := str(info, "title")
	if title == "" {
		title = "Agent 会话"
	}
	if runes := []rune(title); len(runes) > 120 {
		title = string(runes[:120]) + "…"
	}
	if known != nil && str(known, "status") == status {
		return
	}
	s.lastTask = Object{"key": key, "turnId": str(turn, "id"), "target": target, "providerId": provider, "id": session, "title": title, "status": status, "label": label}
	if s.nativeTasks == nil {
		s.nativeTasks = map[string]Object{}
	}
	s.nativeTasks[key] = s.lastTask
	for old, task := range s.nativeTasks {
		if len(s.nativeTasks) <= 128 {
			break
		}
		if old != key && str(task, "status") != "inProgress" {
			delete(s.nativeTasks, old)
			if monitor := s.nativeTaskMonitors[old]; monitor != nil {
				monitor.cancel()
				delete(s.nativeTaskMonitors, old)
			}
		}
	}
}
